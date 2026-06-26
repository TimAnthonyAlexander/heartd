package alert

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/timanthonyalexander/heartd/internal/config"
)

func testAlert() Alert {
	return Alert{
		Kind:    KindMetric,
		Node:    "web-01",
		Subject: "Test",
		Firing:  true,
		Title:   "heartd test alert",
		Detail:  "test",
		Time:    time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
	}
}

// A port of 0 (the unset default) must fail immediately with a clear message,
// not hang on a dial — this is the "send test stays on TESTING forever" bug.
func TestEmailNotifierRejectsInvalidPort(t *testing.T) {
	n := NewEmailNotifier(config.EmailNotify{
		SMTPHost: "smtp.example.com", SMTPPort: 0,
		From: "a@example.com", To: []string{"b@example.com"},
	})

	done := make(chan error, 1)
	go func() { done <- n.Send(context.Background(), testAlert()) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "port") {
			t.Fatalf("expected an invalid-port error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked on an invalid port instead of failing fast")
	}
}

func TestEmailNotifierRejectsMissingHost(t *testing.T) {
	n := NewEmailNotifier(config.EmailNotify{
		SMTPHost: "  ", SMTPPort: 587,
		From: "a@example.com", To: []string{"b@example.com"},
	})
	if err := n.Send(context.Background(), testAlert()); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected a missing-host error, got %v", err)
	}
}

func TestEmailNotifierRejectsNoRecipient(t *testing.T) {
	n := NewEmailNotifier(config.EmailNotify{
		SMTPHost: "smtp.example.com", SMTPPort: 587, From: "a@example.com", To: nil,
	})
	if err := n.Send(context.Background(), testAlert()); err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("expected a no-recipient error, got %v", err)
	}
}

// An already-cancelled context short-circuits before any network work.
func TestEmailNotifierHonorsCancelledContext(t *testing.T) {
	n := NewEmailNotifier(config.EmailNotify{
		SMTPHost: "smtp.example.com", SMTPPort: 587,
		From: "a@example.com", To: []string{"b@example.com"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := n.Send(ctx, testAlert()); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// A valid target pointed at a dead address must still return promptly once the
// context deadline fires, rather than blocking on the OS TCP connect timeout.
func TestEmailNotifierTimesOutFast(t *testing.T) {
	// 203.0.113.0/24 is TEST-NET-3 (RFC 5737) — guaranteed unroutable, so the
	// dial neither connects nor refuses; it just hangs until our deadline.
	n := NewEmailNotifier(config.EmailNotify{
		SMTPHost: "203.0.113.1", SMTPPort: 587,
		From: "a@example.com", To: []string{"b@example.com"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := n.Send(ctx, testAlert())
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Send took %s — context deadline was not honored", elapsed)
	}
}
