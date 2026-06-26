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

func newTestEngine() (*Engine, *fakeNotifier) {
	f := &fakeNotifier{}
	return NewEngine(NewDispatcher(f)), f
}

var t0 = time.Unix(1_000_000, 0).UTC()

func TestObserveFiresOnTransition(t *testing.T) {
	e, f := newTestEngine()
	r := RuleView{ID: 1, Name: "CPU high", Severity: "critical"}

	// Not met: nothing.
	e.Observe(r, "web-01", "", "below", false, false, t0)
	stableCount(t, f, 0)

	// Met: one firing alert with severity + envelope.
	e.Observe(r, "web-01", "", "CPU 95% >= 90%", true, false, t0.Add(time.Second))
	waitFor(t, f, 1)
	a := f.snapshot()[0]
	if !a.Firing || a.Severity != "critical" || a.RuleID != 1 || a.Node != "web-01" || a.Kind != KindRule {
		t.Fatalf("unexpected firing alert: %+v", a)
	}
	if !strings.Contains(a.Title, "CPU high") || a.Detail != "CPU 95% >= 90%" {
		t.Fatalf("unexpected title/detail: %+v", a)
	}

	// Still met: dedup, nothing new.
	e.Observe(r, "web-01", "", "CPU 96% >= 90%", true, false, t0.Add(2*time.Second))
	stableCount(t, f, 1)

	// Cleared: one recovery.
	e.Observe(r, "web-01", "", "CPU 40% >= 90%", false, false, t0.Add(3*time.Second))
	waitFor(t, f, 2)
	if f.snapshot()[1].Firing {
		t.Fatalf("expected recovery, got firing")
	}
}

func TestForSecGatesFiring(t *testing.T) {
	e, f := newTestEngine()
	r := RuleView{ID: 2, Name: "Sustained", Severity: "warning", ForSec: 10}

	e.Observe(r, "n", "", "d", true, false, t0) // breach starts, not elapsed
	stableCount(t, f, 0)
	e.Observe(r, "n", "", "d", true, false, t0.Add(5*time.Second))
	stableCount(t, f, 0)
	e.Observe(r, "n", "", "d", true, false, t0.Add(10*time.Second)) // elapsed
	waitFor(t, f, 1)
}

func TestSeedDoesNotDispatch(t *testing.T) {
	e, f := newTestEngine()
	r := RuleView{ID: 3, Name: "x", Severity: "critical"}

	// Seed as already-breaching: primes firing baseline, dispatches nothing.
	e.Observe(r, "n", "", "d", true, true, t0)
	e.Observe(r, "n", "", "d", true, false, t0.Add(time.Second))
	stableCount(t, f, 0)

	// A clear after a seeded-firing baseline still recovers.
	e.Observe(r, "n", "", "d", false, false, t0.Add(2*time.Second))
	waitFor(t, f, 1)
	if f.snapshot()[0].Firing {
		t.Fatalf("expected recovery after seeded-firing -> clear")
	}
}

func TestRecoverGraceDelaysRecovery(t *testing.T) {
	e, f := newTestEngine()
	r := RuleView{ID: 4, Name: "x", Severity: "warning", RecoverGrace: 10}

	e.Observe(r, "n", "", "d", true, false, t0)
	waitFor(t, f, 1)
	e.Observe(r, "n", "", "d", false, false, t0.Add(time.Second)) // grace not elapsed
	stableCount(t, f, 1)
	e.Observe(r, "n", "", "d", false, false, t0.Add(11*time.Second)) // grace elapsed
	waitFor(t, f, 2)
}

func TestForgetResetsState(t *testing.T) {
	e, f := newTestEngine()
	r := RuleView{ID: 5, Name: "x", Severity: "critical"}

	e.Observe(r, "n", "", "d", true, false, t0)
	waitFor(t, f, 1)
	e.Forget(5)
	// After forget the prior firing state is gone, so a met condition is a fresh
	// transition and fires again.
	e.Observe(r, "n", "", "d", true, false, t0.Add(time.Second))
	waitFor(t, f, 2)
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
		Kind:     KindRule,
		Node:     "web-01",
		Subject:  "High CPU",
		Severity: "critical",
		Firing:   true,
		Title:    "High CPU — web-01",
		Detail:   "CPU 95% >= 90%",
		Time:     time.Date(2026, 6, 25, 19, 0, 0, 0, time.UTC),
	}
	if err := n.Send(context.Background(), a); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	select {
	case r := <-got:
		if r.body.Kind != KindRule || r.body.Node != "web-01" || r.body.Subject != "High CPU" {
			t.Errorf("unexpected payload identity: %+v", r.body)
		}
		if r.body.Severity != "critical" {
			t.Errorf("expected severity critical, got %q", r.body.Severity)
		}
		if !r.body.Firing || r.body.Status != "firing" {
			t.Errorf("expected firing/status=firing, got firing=%v status=%q", r.body.Firing, r.body.Status)
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
		Firing:   true,
		Severity: "critical",
		Node:     "web-01",
		Title:    "High CPU — web-01",
		Detail:   "CPU 95% >= 90%",
		Time:     time.Date(2026, 6, 25, 19, 0, 0, 0, time.UTC),
	}
	msg := string(buildEmailMessage(cfg, a))

	for _, want := range []string{
		"From: heartd@example.com",
		"To: ops@example.com, oncall@example.com",
		"Subject: [CRITICAL] [heartd] High CPU — web-01", // firing/critical status tag in subject
		"multipart/alternative",                          // both plain + HTML parts
		"Content-Type: text/plain",
		"Content-Type: text/html",
		"High CPU — web-01",     // title in the plain part
		"CPU 95% >= 90%",        // detail, literal (unescaped) in the plain part
		"Time: 2026-06-25T19:00:00Z",
		"CRITICAL",              // status label in the HTML card
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("email message missing %q\n---\n%s", want, msg)
		}
	}
	// The HTML part must escape the detail's '>' so it can't break markup.
	if !strings.Contains(msg, "CPU 95% &gt;= 90%") {
		t.Errorf("HTML part should HTML-escape the detail:\n%s", msg)
	}
}

func TestBuildEmailMessageRecoveredNoPrefix(t *testing.T) {
	cfg := config.EmailNotify{From: "a@b.c", To: []string{"x@y.z"}}
	a := Alert{Title: "Node peer recovered", Firing: false, Time: time.Now()}
	msg := string(buildEmailMessage(cfg, a))
	if !strings.Contains(msg, "Subject: [RECOVERED] Node peer recovered") {
		t.Errorf("recovered subject should carry the [RECOVERED] tag and no prefix:\n%s", msg)
	}
	if !strings.Contains(msg, "RECOVERED") {
		t.Errorf("recovered alert should show the RECOVERED status label:\n%s", msg)
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
	e, f := newTestEngine()

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			r := RuleView{ID: int64(g), Name: "r", Severity: "warning"}
			base := time.Unix(2_000_000, 0).UTC()
			for i := 0; i < 200; i++ {
				e.Observe(r, "node", "", "d", i%2 == 0, false, base.Add(time.Duration(i)*time.Second))
			}
		}(g)
	}
	wg.Wait()

	time.Sleep(50 * time.Millisecond)
	before := f.count()

	// A final clean transition should still work (engine not deadlocked).
	r := RuleView{ID: 99, Name: "r", Severity: "warning"}
	e.Observe(r, "node", "", "d", true, false, time.Unix(5_000_000, 0).UTC())
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if f.count() > before {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("final transition did not dispatch (count stuck at %d)", f.count())
}
