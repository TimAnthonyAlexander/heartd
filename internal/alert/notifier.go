package alert

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

// implicitTLS reports whether a port speaks TLS from the first byte (SMTPS).
// Port 465 is implicit TLS; 587 and 25 are plaintext with optional STARTTLS.
func implicitTLS(port int) bool { return port == 465 }

// Send delivers the alert over SMTP, honoring ctx for both the connect and the
// conversation, and picking the right transport for the port:
//
//   - 465 (SMTPS): the socket is TLS from the first byte. net/smtp.SendMail
//     CANNOT do this — it speaks plaintext then optional STARTTLS — so a 465
//     send with SendMail hangs (the server waits for a TLS handshake that never
//     comes). We wrap the connection in TLS up front instead.
//   - 587 / 25: plaintext, upgraded with STARTTLS when the server offers it.
//
// The target is validated first so a missing host or invalid port (e.g. the
// unset default 0) fails instantly with a clear message.
func (e *EmailNotifier) Send(ctx context.Context, a Alert) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	host := strings.TrimSpace(e.cfg.SMTPHost)
	if host == "" {
		return fmt.Errorf("SMTP host is not set")
	}
	if e.cfg.SMTPPort <= 0 || e.cfg.SMTPPort > 65535 {
		return fmt.Errorf("SMTP port %d is invalid — set it to your provider's port (e.g. 587 or 465)", e.cfg.SMTPPort)
	}
	if len(e.cfg.To) == 0 {
		return fmt.Errorf("no recipient address set")
	}

	addr := host + ":" + strconv.Itoa(e.cfg.SMTPPort)
	tlsConfig := &tls.Config{ServerName: host}

	// Context-aware connect: an unreachable server fails by the deadline rather
	// than blocking on the OS TCP connect timeout.
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("send email to %s: %w", addr, err)
	}
	// Bound the rest of the SMTP conversation by the same deadline.
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if implicitTLS(e.cfg.SMTPPort) {
		conn = tls.Client(conn, tlsConfig)
	}

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("send email to %s: %w", addr, err)
	}
	defer c.Close()

	if !implicitTLS(e.cfg.SMTPPort) {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS with %s: %w", addr, err)
			}
		}
	}

	if e.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, host)); err != nil {
			return fmt.Errorf("SMTP auth failed (check username/password): %w", err)
		}
	}

	if err := c.Mail(e.cfg.From); err != nil {
		return fmt.Errorf("SMTP MAIL FROM %q: %w", e.cfg.From, err)
	}
	for _, rcpt := range e.cfg.To {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("SMTP RCPT %q: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write(buildEmailMessage(e.cfg, a)); err != nil {
		return fmt.Errorf("write message body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close message body: %w", err)
	}
	return c.Quit()
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
