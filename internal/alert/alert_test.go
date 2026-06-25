package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/timanthonyalexander/heartd/internal/config"
)

// fakeNotifier records every Alert it receives. Safe for concurrent use.
type fakeNotifier struct {
	mu       sync.Mutex
	alerts   []Alert
	failNext bool
}

func (f *fakeNotifier) Name() string { return "fake" }

func (f *fakeNotifier) Send(_ context.Context, a Alert) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, a)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.alerts)
}

func (f *fakeNotifier) snapshot() []Alert {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Alert, len(f.alerts))
	copy(out, f.alerts)
	return out
}

// waitFor polls until the fake notifier has received exactly n alerts or the
// timeout elapses. Dispatch is async, so tests must wait.
func waitFor(t *testing.T, f *fakeNotifier, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if f.count() == n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d alerts, got %d", n, f.count())
}

// stableCount waits a short while and asserts the count never exceeds n. Used to
// assert that NO additional alert fires (dedup).
func stableCount(t *testing.T, f *fakeNotifier, n int) {
	t.Helper()
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if c := f.count(); c != n {
			t.Fatalf("expected count to stay at %d, got %d", n, c)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newTestEngine(t *testing.T) (*Engine, *fakeNotifier) {
	t.Helper()
	f := &fakeNotifier{}
	d := NewDispatcher(f)
	th := config.Thresholds{CPUPercent: 90, MemPercent: 80, DiskPercent: 90}
	return NewEngine(d, th), f
}

func TestObserveCheckTransitions(t *testing.T) {
	e, f := newTestEngine(t)

	// ok -> failing fires one firing alert.
	e.ObserveCheck("web-01", "Google", "http", statusOK, "")
	stableCount(t, f, 0) // ok with no prior (unknown->ok) fires nothing

	e.ObserveCheck("web-01", "Google", "http", statusFailing, "HTTP 500")
	waitFor(t, f, 1)
	got := f.snapshot()[0]
	if !got.Firing {
		t.Errorf("expected firing alert, got recovered")
	}
	if got.Title != `Check "Google" is failing on web-01` {
		t.Errorf("unexpected title: %q", got.Title)
	}
	if got.Detail != "HTTP 500" {
		t.Errorf("unexpected detail: %q", got.Detail)
	}
	if got.Kind != KindCheck || got.Node != "web-01" || got.Subject != "Google" {
		t.Errorf("unexpected envelope: %+v", got)
	}

	// failing -> failing fires NOTHING (dedup).
	e.ObserveCheck("web-01", "Google", "http", statusFailing, "HTTP 503")
	stableCount(t, f, 1)

	// failing -> ok fires one recovered alert.
	e.ObserveCheck("web-01", "Google", "http", statusOK, "HTTP 200")
	waitFor(t, f, 2)
	rec := f.snapshot()[1]
	if rec.Firing {
		t.Errorf("expected recovered alert, got firing")
	}
	if rec.Title != `Check "Google" recovered on web-01` {
		t.Errorf("unexpected recovery title: %q", rec.Title)
	}
}

func TestObserveCheckUnknownToOK(t *testing.T) {
	e, f := newTestEngine(t)
	// No prior state: unknown -> ok must NOT fire.
	e.ObserveCheck("n1", "c1", "tcp", statusOK, "")
	stableCount(t, f, 0)
}

func TestSeedCheckPreventsReAlert(t *testing.T) {
	e, f := newTestEngine(t)
	// Restart scenario: seed as failing, then observe the same failing status.
	e.SeedCheck("web-01", "Google", statusFailing)
	e.ObserveCheck("web-01", "Google", "http", statusFailing, "still down")
	stableCount(t, f, 0)

	// But a transition to ok after seed still fires recovery.
	e.ObserveCheck("web-01", "Google", "http", statusOK, "")
	waitFor(t, f, 1)
	if f.snapshot()[0].Firing {
		t.Errorf("expected recovery after seeded-failing -> ok")
	}
}

func TestObservePeerTransitions(t *testing.T) {
	e, f := newTestEngine(t)

	// unknown -> ok: nothing.
	e.ObservePeer("peer-a", statusOK)
	stableCount(t, f, 0)

	// ok -> down: fires.
	e.ObservePeer("peer-a", statusDown)
	waitFor(t, f, 1)
	a := f.snapshot()[0]
	if !a.Firing || a.Title != "Node peer-a is unreachable" || a.Kind != KindPeer {
		t.Errorf("unexpected down alert: %+v", a)
	}

	// down -> down: nothing.
	e.ObservePeer("peer-a", statusDown)
	stableCount(t, f, 1)

	// down -> ok: recovered.
	e.ObservePeer("peer-a", statusOK)
	waitFor(t, f, 2)
	r := f.snapshot()[1]
	if r.Firing || r.Title != "Node peer-a recovered" {
		t.Errorf("unexpected recovery alert: %+v", r)
	}
}

func TestSeedPeerPreventsReAlert(t *testing.T) {
	e, f := newTestEngine(t)
	e.SeedPeer("peer-b", statusDown)
	e.ObservePeer("peer-b", statusDown)
	stableCount(t, f, 0)
}

func TestObserveMetricTransitions(t *testing.T) {
	e, f := newTestEngine(t) // CPU threshold 90, Mem 80

	// below -> below: nothing.
	e.ObserveMetric("web-01", 10, 10, 10)
	stableCount(t, f, 0)

	// below -> above (CPU): fires once.
	e.ObserveMetric("web-01", 95, 10, 10)
	waitFor(t, f, 1)
	a := f.snapshot()[0]
	if !a.Firing || a.Subject != "CPU" || a.Kind != KindMetric {
		t.Errorf("unexpected cpu alert: %+v", a)
	}
	if !strings.Contains(a.Title, "CPU on web-01 is high") {
		t.Errorf("unexpected cpu title: %q", a.Title)
	}

	// above -> above: nothing.
	e.ObserveMetric("web-01", 96, 10, 10)
	stableCount(t, f, 1)

	// above -> below: recovered.
	e.ObserveMetric("web-01", 50, 10, 10)
	waitFor(t, f, 2)
	r := f.snapshot()[1]
	if r.Firing || !strings.Contains(r.Title, "CPU on web-01 recovered") {
		t.Errorf("unexpected cpu recovery: %+v", r)
	}
}

func TestObserveMetricThresholdDisabled(t *testing.T) {
	f := &fakeNotifier{}
	d := NewDispatcher(f)
	// CPU disabled (0), Mem enabled at 80.
	e := NewEngine(d, config.Thresholds{CPUPercent: 0, MemPercent: 80})

	// Huge CPU must never fire because threshold <= 0.
	e.ObserveMetric("web-01", 100, 10, 10)
	stableCount(t, f, 0)

	// Mem crossing still fires.
	e.ObserveMetric("web-01", 100, 95, 10)
	waitFor(t, f, 1)
	if f.snapshot()[0].Subject != "Memory" {
		t.Errorf("expected only a Memory alert, got %+v", f.snapshot())
	}
}

func TestObserveMetricDiskTransitions(t *testing.T) {
	f := &fakeNotifier{}
	d := NewDispatcher(f)
	e := NewEngine(d, config.Thresholds{CPUPercent: 90, MemPercent: 90, DiskPercent: 90})

	// Disk below threshold: nothing (CPU/mem also below).
	e.ObserveMetric("web-01", 10, 10, 80)
	stableCount(t, f, 0)

	// Disk crosses above: one firing alert about Disk.
	e.ObserveMetric("web-01", 10, 10, 95)
	waitFor(t, f, 1)
	a := f.snapshot()[0]
	if !a.Firing || a.Subject != "Disk" || a.Kind != KindMetric {
		t.Errorf("unexpected disk alert: %+v", a)
	}

	// Still above: dedup, nothing new.
	e.ObserveMetric("web-01", 10, 10, 97)
	stableCount(t, f, 1)

	// Back below: recovery.
	e.ObserveMetric("web-01", 10, 10, 40)
	waitFor(t, f, 2)
	if f.snapshot()[1].Firing {
		t.Errorf("expected disk recovery, got %+v", f.snapshot()[1])
	}
}

func TestWebhookNotifierSendsJSON(t *testing.T) {
	type received struct {
		body webhookPayload
		ct   string
	}
	got := make(chan received, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p webhookPayload
		_ = json.NewDecoder(r.Body).Decode(&p)
		got <- received{body: p, ct: r.Header.Get("Content-Type")}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(config.WebhookNotify{URL: srv.URL})
	a := Alert{
		Kind:    KindCheck,
		Node:    "web-01",
		Subject: "Google",
		Firing:  true,
		Title:   `Check "Google" is failing on web-01`,
		Detail:  "HTTP 500",
		Time:    time.Date(2026, 6, 25, 19, 0, 0, 0, time.UTC),
	}
	if err := n.Send(context.Background(), a); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	select {
	case r := <-got:
		if r.body.Kind != KindCheck || r.body.Node != "web-01" || r.body.Subject != "Google" {
			t.Errorf("unexpected payload identity: %+v", r.body)
		}
		if !r.body.Firing || r.body.Status != "firing" {
			t.Errorf("expected firing/status=firing, got firing=%v status=%q", r.body.Firing, r.body.Status)
		}
		if r.body.Title != a.Title {
			t.Errorf("unexpected title: %q", r.body.Title)
		}
		if r.body.Time != "2026-06-25T19:00:00Z" {
			t.Errorf("unexpected time: %q", r.body.Time)
		}
		if r.ct != "application/json" {
			t.Errorf("unexpected content-type: %q", r.ct)
		}
	case <-time.After(time.Second):
		t.Fatal("webhook server did not receive request")
	}
}

func TestWebhookNotifierStatusRecovered(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(config.WebhookNotify{URL: srv.URL})
	err := n.Send(context.Background(), Alert{Firing: false, Time: time.Now()})
	if err != nil {
		t.Fatalf("204 should be success, got %v", err)
	}
	select {
	case body := <-got:
		if !strings.Contains(body, `"status":"recovered"`) {
			t.Errorf("expected status recovered, got %s", body)
		}
	case <-time.After(time.Second):
		t.Fatal("no request received")
	}
}

func TestWebhookNotifierNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(config.WebhookNotify{URL: srv.URL})
	if err := n.Send(context.Background(), Alert{Time: time.Now()}); err == nil {
		t.Fatal("expected error on non-2xx response, got nil")
	}
}

func TestBuildEmailMessage(t *testing.T) {
	cfg := config.EmailNotify{
		From:          "heartd@example.com",
		To:            []string{"ops@example.com", "oncall@example.com"},
		SubjectPrefix: "[heartd]",
	}
	a := Alert{
		Title:  `Check "Google" is failing on web-01`,
		Detail: "HTTP 500 in 45ms",
		Time:   time.Date(2026, 6, 25, 19, 0, 0, 0, time.UTC),
	}
	msg := string(buildEmailMessage(cfg, a))

	for _, want := range []string{
		"From: heartd@example.com",
		"To: ops@example.com, oncall@example.com",
		`Subject: [heartd] Check "Google" is failing on web-01`,
		`Check "Google" is failing on web-01`,
		"HTTP 500 in 45ms",
		"Time: 2026-06-25T19:00:00Z",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("email message missing %q\n---\n%s", want, msg)
		}
	}
}

func TestBuildEmailMessageNoPrefix(t *testing.T) {
	cfg := config.EmailNotify{From: "a@b.c", To: []string{"x@y.z"}}
	a := Alert{Title: "Node peer recovered", Time: time.Now()}
	msg := string(buildEmailMessage(cfg, a))
	if !strings.Contains(msg, "Subject: Node peer recovered") {
		t.Errorf("subject should be trimmed with no prefix:\n%s", msg)
	}
}

func TestDispatcherEmpty(t *testing.T) {
	if !NewDispatcher().Empty() {
		t.Error("expected empty dispatcher")
	}
	if NewDispatcher(nil, nil).Empty() == false {
		t.Error("nil notifiers should be filtered, leaving it empty")
	}
	if NewDispatcher(&fakeNotifier{}).Empty() {
		t.Error("dispatcher with one notifier should not be empty")
	}
}

// TestConcurrentObserveNoDeadlock fires many overlapping observations from
// multiple goroutines. Run with -race to detect data races.
func TestConcurrentObserveNoDeadlock(t *testing.T) {
	e, f := newTestEngine(t)

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			name := "check"
			node := "node"
			for i := 0; i < 200; i++ {
				status := statusOK
				if i%2 == 0 {
					status = statusFailing
				}
				e.ObserveCheck(node, name, "http", status, "d")
				e.ObservePeer("peer", status)
				e.ObserveMetric(node, float64(i%100), float64(i%100), float64(i%100))
			}
		}(g)
	}
	wg.Wait()

	// Drain any in-flight async dispatches; just assert we didn't deadlock and
	// the engine remains usable.
	time.Sleep(50 * time.Millisecond)
	_ = f.count()

	// A final clean transition should still work (engine not deadlocked).
	e.SeedCheck("node", "check", statusOK)
	before := f.count()
	e.ObserveCheck("node", "check", "http", statusFailing, "final")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if f.count() > before {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("final transition did not dispatch (count stuck at %d)", f.count())
}
