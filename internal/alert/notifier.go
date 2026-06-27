package alert

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html"
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
	Observer string `json:"observer,omitempty"`
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
		Observer: a.Observer,
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

// emailBoundary separates the plain-text and HTML parts of the multipart body.
// A fixed token is fine: it's distinctive enough never to appear in alert text.
const emailBoundary = "==heartd_a17c_alt_boundary=="

// emailVisual maps an alert to its presentation: a status label and the accent
// colour used for it. Colours are deep enough to read against a light card and
// stand in for real severity, not decoration — a deep red for critical, ochre
// for warning, forest green for recovered.
func emailVisual(a Alert) (accent, label string) {
	if !a.Firing {
		return "#1f7a45", "RECOVERED"
	}
	if a.Severity == "critical" {
		return "#b3261d", "CRITICAL"
	}
	return "#8a5512", "WARNING"
}

// buildEmailMessage constructs the raw RFC 822 message for an alert as a
// multipart/alternative (plain text + a styled HTML card). Pure (no I/O) so it
// can be unit-tested without a live SMTP server.
func buildEmailMessage(cfg config.EmailNotify, a Alert) []byte {
	_, label := emailVisual(a)
	subject := "[" + label + "] " + strings.TrimSpace(cfg.SubjectPrefix+" "+a.Title)
	when := a.Time.UTC().Format("2006-01-02T15:04:05Z07:00")

	var b strings.Builder
	b.WriteString("From: " + cfg.From + "\r\n")
	b.WriteString("To: " + strings.Join(cfg.To, ", ") + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + emailBoundary + "\"\r\n")
	b.WriteString("\r\n")

	// Plain-text alternative (for clients that don't render HTML).
	b.WriteString("--" + emailBoundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
	b.WriteString("[" + label + "] " + a.Title + "\r\n")
	if a.Detail != "" {
		b.WriteString("\r\n" + a.Detail + "\r\n")
	}
	if a.Node != "" {
		b.WriteString("\r\nNode: " + a.Node + "\r\n")
	}
	if t := entityLabel(a.Entity); t != "" {
		b.WriteString("Target: " + t + "\r\n")
	}
	if a.Observer != "" {
		b.WriteString("Reported by: " + a.Observer + "\r\n")
	}
	b.WriteString("Time: " + when + "\r\n")

	// HTML alternative.
	b.WriteString("\r\n--" + emailBoundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
	b.WriteString(emailHTML(a, when))

	b.WriteString("\r\n--" + emailBoundary + "--\r\n")
	return []byte(b.String())
}

// emailHTML renders the alert as a plain, legible notice: a light card with a
// single status-coloured spine on the left, a small status label, the title,
// and a hairline-ruled table of facts. Styles are inline and the layout is
// table-based for broad email-client compatibility. Colour appears only where
// it carries meaning (the severity), never as decoration.
func emailHTML(a Alert, when string) string {
	accent, label := emailVisual(a)
	esc := html.EscapeString

	metaRow := func(name, value string) string {
		if value == "" {
			return ""
		}
		return `<tr>` +
			`<td style="padding:10px 0;border-top:1px solid #efece5;color:#8c877d;font-size:13px;width:84px;vertical-align:top;">` + esc(name) + `</td>` +
			`<td style="padding:10px 0;border-top:1px solid #efece5;color:#322f2a;font-size:13px;font-weight:500;">` + esc(value) + `</td>` +
			`</tr>`
	}

	detail := ""
	if a.Detail != "" {
		detail = `<tr><td style="padding:12px 32px 0;color:#5f5b53;font-size:14px;line-height:1.6;">` +
			esc(a.Detail) + `</td></tr>`
	}

	return `<!doctype html><html><body style="margin:0;padding:0;background:#f4f2ee;">` +
		`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f4f2ee;padding:32px 16px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">` +
		`<tr><td align="center">` +
		`<table role="presentation" width="560" cellpadding="0" cellspacing="0" style="max-width:560px;width:100%;background:#ffffff;border:1px solid #e7e2d8;border-left:3px solid ` + accent + `;border-radius:4px;">` +
		`<tr><td style="padding:28px 32px 0;color:` + accent + `;font-size:12px;font-weight:700;letter-spacing:.07em;text-transform:uppercase;">` + label + `</td></tr>` +
		`<tr><td style="padding:8px 32px 0;color:#211f1c;font-size:20px;font-weight:600;line-height:1.35;">` + esc(a.Title) + `</td></tr>` +
		detail +
		`<tr><td style="padding:24px 32px 6px;">` +
		`<table role="presentation" width="100%" cellpadding="0" cellspacing="0">` +
		metaRow("Node", a.Node) +
		metaRow("Target", entityLabel(a.Entity)) +
		metaRow("Reported by", a.Observer) +
		metaRow("When", when+" UTC") +
		`</table></td></tr>` +
		`<tr><td style="padding:18px 32px 26px;color:#9b958a;font-size:12px;line-height:1.5;">heartd · automated monitoring alert</td></tr>` +
		`</table></td></tr></table></body></html>`
}

// entityLabel renders the alert's entity for display, blanking the "any" wildcard.
func entityLabel(entity string) string {
	if entity == "" || entity == "*" {
		return ""
	}
	return entity
}
