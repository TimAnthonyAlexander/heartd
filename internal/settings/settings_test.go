package settings

import (
	"testing"
	"time"

	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

func seedConfig() config.Config {
	cfg := config.Default()
	cfg.Thresholds = config.Thresholds{CPUPercent: 70, MemPercent: 75, DiskPercent: 80}
	cfg.Checks = []config.Check{{
		Name: "Google", Type: "http", URL: "https://google.com",
		Interval: config.Duration(30 * time.Second), Timeout: config.Duration(5 * time.Second),
		Method: "GET",
	}}
	return cfg
}

func TestLoadSeedsFromConfig(t *testing.T) {
	s := newService(t)
	if err := s.Load(seedConfig()); err != nil {
		t.Fatalf("load: %v", err)
	}

	g := s.General()
	if g.MetricsInterval != 30*time.Second {
		t.Errorf("metrics interval = %v, want 30s", g.MetricsInterval)
	}
	if checks := s.Checks(); len(checks) != 1 || checks[0].Name != "Google" {
		t.Errorf("checks not seeded: %+v", checks)
	}
	// The legacy thresholds seed into default alert rules (cpu/mem/disk) plus the
	// always-on check-failing and peer-down rules.
	rules := s.AlertRules()
	var cpu *storage.AlertRule
	for i := range rules {
		if rules[i].Source == "cpu" {
			cpu = &rules[i]
		}
	}
	if cpu == nil || cpu.Threshold != 70 {
		t.Errorf("cpu rule not seeded from threshold: %+v", rules)
	}
}

func TestLoadDoesNotReseed(t *testing.T) {
	s := newService(t)
	if err := s.Load(seedConfig()); err != nil {
		t.Fatalf("load: %v", err)
	}
	// Delete the seeded check, then Load again — it must NOT come back.
	checks := s.Checks()
	if err := s.DeleteCheck(checks[0].ID, "node"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.Load(seedConfig()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := s.Checks(); len(got) != 0 {
		t.Errorf("expected no checks after delete+reload, got %d", len(got))
	}
}

func TestSetGeneralValidation(t *testing.T) {
	s := newService(t)
	if err := s.Load(seedConfig()); err != nil {
		t.Fatalf("load: %v", err)
	}
	g := s.General()
	g.MetricsIntervalSec = 0 // invalid
	if err := s.SetGeneral(g); err == nil {
		t.Error("expected error for zero metrics interval")
	}

	// Alert-rule validation: out-of-range percent threshold is rejected.
	if _, err := s.CreateAlertRule(storage.AlertRule{
		Name: "bad", Source: "cpu", Comparator: ">=", Threshold: 150, Severity: "warning",
	}); err == nil {
		t.Error("expected error for out-of-range cpu threshold")
	}
}

func TestCheckCRUDIsLive(t *testing.T) {
	s := newService(t)
	if err := s.Load(seedConfig()); err != nil {
		t.Fatalf("load: %v", err)
	}

	created, err := s.CreateCheck(Check{
		Name: "Redis", Type: "tcp", Host: "127.0.0.1", Port: 6379,
		Interval: 15 * time.Second, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == 0 || created.Timeout != defaultCheckTimeout {
		t.Errorf("create did not default timeout / set id: %+v", created)
	}
	if len(s.Checks()) != 2 {
		t.Fatalf("expected 2 checks after create, got %d", len(s.Checks()))
	}

	created.Interval = 60 * time.Second
	if err := s.UpdateCheck(created); err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, c := range s.Checks() {
		if c.ID == created.ID && c.Interval != 60*time.Second {
			t.Errorf("update not reflected: %+v", c)
		}
	}

	if err := s.DeleteCheck(created.ID, "node"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(s.Checks()) != 1 {
		t.Errorf("expected 1 check after delete, got %d", len(s.Checks()))
	}
}

func TestCreateCheckValidation(t *testing.T) {
	s := newService(t)
	if err := s.Load(config.Default()); err != nil {
		t.Fatalf("load: %v", err)
	}
	// tcp check without host/port is invalid.
	if _, err := s.CreateCheck(Check{Name: "bad", Type: "tcp", Interval: time.Second}); err == nil {
		t.Error("expected validation error for tcp check without host")
	}
}

func TestCheckAcceptedStatusesRoundTrip(t *testing.T) {
	s := newService(t)
	if err := s.Load(config.Default()); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Codes are normalized (deduped + sorted) on create and survive the
	// comma-joined storage encoding.
	created, err := s.CreateCheck(Check{
		Name: "Login", Type: "http", URL: "https://app.example.com",
		Interval: 30 * time.Second, Enabled: true,
		AcceptedStatuses: []int{403, 200, 401, 200},
		UserAgent:        "  my-agent  ",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := created.AcceptedStatuses; len(got) != 3 || got[0] != 200 || got[1] != 401 || got[2] != 403 {
		t.Errorf("accepted_statuses = %v, want [200 401 403]", got)
	}
	if created.UserAgent != "my-agent" {
		t.Errorf("user_agent = %q, want trimmed %q", created.UserAgent, "my-agent")
	}

	// Reloading from the DB yields the same parsed list.
	if err := s.Load(config.Default()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	var found *Check
	for _, c := range s.Checks() {
		if c.ID == created.ID {
			cc := c
			found = &cc
		}
	}
	if found == nil {
		t.Fatalf("check %d missing after reload", created.ID)
	}
	if got := found.AcceptedStatuses; len(got) != 3 || got[0] != 200 || got[2] != 403 {
		t.Errorf("accepted_statuses after reload = %v, want [200 401 403]", got)
	}
}

func TestCheckAcceptedStatusesValidation(t *testing.T) {
	s := newService(t)
	if err := s.Load(config.Default()); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := s.CreateCheck(Check{
		Name: "bad", Type: "http", URL: "https://x.example.com",
		Interval: time.Second, AcceptedStatuses: []int{99},
	}); err == nil {
		t.Error("expected validation error for out-of-range status code 99")
	}
}

func TestNotifyRoundTrip(t *testing.T) {
	s := newService(t)
	if err := s.Load(config.Default()); err != nil {
		t.Fatalf("load: %v", err)
	}
	n := Notify{Webhook: WebhookNotify{Enabled: true, URL: "https://example.com/hook"}}
	if err := s.SetNotify(n); err != nil {
		t.Fatalf("set notify: %v", err)
	}
	if got := s.Notify(); !got.Webhook.Enabled || got.Webhook.URL != "https://example.com/hook" {
		t.Errorf("notify not persisted: %+v", got)
	}
}
