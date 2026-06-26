package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/timanthonyalexander/heartd/internal/config"
)

// httpTimeout bounds a single webhook POST.
const httpTimeout = sendTimeout

// webhookPayload is the stable JSON shape POSTed to a webhook target.
type webhookPayload struct {
	Kind     Kind   `json:"kind"`
	Node     string `json:"node"`
	Entity   string `json:"entity,omitempty"`
	Subject  string `json:"subject"`
	Severity string `json:"severity,omitempty"`
	Firing   bool   `json:"firing"`
	Status   string `json:"status"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Time     string `json:"time"`
}

// WebhookNotifier delivers alerts as an HTTP POST with a JSON body.
type WebhookNotifier struct {
	url    string
	client *http.Client
}

// NewWebhookNotifier builds a WebhookNotifier for the configured URL.
func NewWebhookNotifier(cfg config.WebhookNotify) *WebhookNotifier {
	return &WebhookNotifier{
		url:    cfg.URL,
		client: &http.Client{Timeout: httpTimeout},
	}
}

// Name implements Notifier.
func (w *WebhookNotifier) Name() string { return "webhook" }

// Send POSTs the alert as JSON to the configured URL using the passed context.
// A non-2xx response is treated as a failure.
func (w *WebhookNotifier) Send(ctx context.Context, a Alert) error {
	payload := webhookPayload{
		Kind:     a.Kind,
		Node:     a.Node,
		Entity:   a.Entity,
		Subject:  a.Subject,
		Severity: a.Severity,
		Firing:   a.Firing,
		Status:   a.Status(),
		Title:    a.Title,
		Detail:   a.Detail,
		Time:     a.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook returned non-2xx status: %d", resp.StatusCode)
	}
	return nil
}

// EmailNotifier delivers alerts as a plain-text email over SMTP.
type EmailNotifier struct {
	cfg config.EmailNotify
}

// NewEmailNotifier builds an EmailNotifier for the configured SMTP target.
func NewEmailNotifier(cfg config.EmailNotify) *EmailNotifier {
	return &EmailNotifier{cfg: cfg}
}

// Name implements Notifier.
func (e *EmailNotifier) Name() string { return "email" }

// Send builds an RFC 822 message and sends it via net/smtp.SendMail.
//
// net/smtp does not accept a context, and SendMail's underlying net.Dial has no
// timeout — so against an unreachable host (or a bogus port) it can block on the
// OS TCP connect for over a minute, hanging the caller (e.g. the "send test"
// button) the whole time. We guard that two ways: validate the target up front
// (so a missing host or invalid port fails instantly with a clear message), and
// race SendMail against ctx so the call returns as soon as the deadline fires.
func (e *EmailNotifier) Send(ctx context.Context, a Alert) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(e.cfg.SMTPHost) == "" {
		return fmt.Errorf("SMTP host is not set")
	}
	if e.cfg.SMTPPort <= 0 || e.cfg.SMTPPort > 65535 {
		return fmt.Errorf("SMTP port %d is invalid — set it to your provider's port (e.g. 587 or 465)", e.cfg.SMTPPort)
	}
	if len(e.cfg.To) == 0 {
		return fmt.Errorf("no recipient address set")
	}

	addr := e.cfg.SMTPHost + ":" + strconv.Itoa(e.cfg.SMTPPort)

	var auth smtp.Auth
	if e.cfg.Username != "" {
		auth = smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.SMTPHost)
	}

	msg := buildEmailMessage(e.cfg, a)

	// SendMail blocks and ignores ctx; run it off-goroutine and return when
	// either it finishes or ctx is done. The buffered channel lets a late-
	// finishing send drain without leaking the goroutine.
	done := make(chan error, 1)
	go func() { done <- smtp.SendMail(addr, auth, e.cfg.From, e.cfg.To, msg) }()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("send email: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("send email to %s timed out (check the SMTP host, port, and connectivity): %w", addr, ctx.Err())
	}
}

// buildEmailMessage constructs the raw RFC 822 message bytes for an alert. It is
// pure (no I/O) so it can be unit-tested without a live SMTP server.
func buildEmailMessage(cfg config.EmailNotify, a Alert) []byte {
	subject := strings.TrimSpace(cfg.SubjectPrefix + " " + a.Title)

	var b strings.Builder
	b.WriteString("From: " + cfg.From + "\r\n")
	b.WriteString("To: " + strings.Join(cfg.To, ", ") + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")

	b.WriteString(a.Title + "\r\n")
	if a.Detail != "" {
		b.WriteString("\r\n" + a.Detail + "\r\n")
	}
	b.WriteString("\r\nTime: " + a.Time.UTC().Format("2006-01-02T15:04:05Z07:00") + "\r\n")

	return []byte(b.String())
}
