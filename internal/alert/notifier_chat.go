package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/timanthonyalexander/heartd/internal/config"
)

// discordContentLimit is Discord's hard cap on a message's content field.
const discordContentLimit = 2000

// formatAlertText renders an alert as a short, readable block shared by the chat
// channels (Slack/Discord/Telegram). It carries the same content the Email and
// Webhook channels use: the status label (severity / recovered), the title, the
// detail line, the node, the target entity, and the time.
func formatAlertText(a Alert) string {
	_, label := emailVisual(a)
	when := a.Time.UTC().Format("2006-01-02T15:04:05Z07:00")

	var b strings.Builder
	b.WriteString("[" + label + "] " + a.Title)
	if a.Detail != "" {
		b.WriteString("\n" + a.Detail)
	}
	if a.Node != "" {
		b.WriteString("\nNode: " + a.Node)
	}
	if t := entityLabel(a.Entity); t != "" {
		b.WriteString("\nTarget: " + t)
	}
	b.WriteString("\nTime: " + when + " UTC")
	return b.String()
}

// postJSON2xx marshals payload, POSTs it as JSON to url under ctx, drains the
// response, and treats any non-2xx status as a failure. channel names the
// notifier for error messages. It mirrors WebhookNotifier.Send's transport.
func postJSON2xx(ctx context.Context, client *http.Client, url string, payload any, channel string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", channel, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s request: %w", channel, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", channel, err)
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s returned non-2xx status: %d", channel, resp.StatusCode)
	}
	return nil
}

// clampRunes truncates s to at most max runes (not bytes), so multibyte content
// is never cut mid-character.
func clampRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// SlackNotifier delivers alerts to a Slack incoming webhook.
type SlackNotifier struct {
	url    string
	client *http.Client
}

// NewSlackNotifier builds a SlackNotifier for the configured incoming-webhook URL.
func NewSlackNotifier(cfg config.SlackNotify) *SlackNotifier {
	return &SlackNotifier{url: cfg.WebhookURL, client: &http.Client{Timeout: httpTimeout}}
}

// Name implements Notifier.
func (s *SlackNotifier) Name() string { return "slack" }

// Send POSTs {"text": "<message>"} to the Slack webhook. Non-2xx = failure.
func (s *SlackNotifier) Send(ctx context.Context, a Alert) error {
	payload := map[string]string{"text": formatAlertText(a)}
	return postJSON2xx(ctx, s.client, s.url, payload, "slack")
}

// DiscordNotifier delivers alerts to a Discord webhook.
type DiscordNotifier struct {
	url    string
	client *http.Client
}

// NewDiscordNotifier builds a DiscordNotifier for the configured webhook URL.
func NewDiscordNotifier(cfg config.DiscordNotify) *DiscordNotifier {
	return &DiscordNotifier{url: cfg.WebhookURL, client: &http.Client{Timeout: httpTimeout}}
}

// Name implements Notifier.
func (d *DiscordNotifier) Name() string { return "discord" }

// Send POSTs {"content": "<message>"} to the Discord webhook. Discord rejects an
// empty content and caps it at 2000 chars, so we guard both. Non-2xx = failure
// (a 204 No Content success falls inside the 2xx range).
func (d *DiscordNotifier) Send(ctx context.Context, a Alert) error {
	content := formatAlertText(a)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("discord: refusing to send empty message")
	}
	payload := map[string]string{"content": clampRunes(content, discordContentLimit)}
	return postJSON2xx(ctx, d.client, d.url, payload, "discord")
}

// TelegramNotifier delivers alerts via the Telegram Bot API sendMessage method.
type TelegramNotifier struct {
	token  string
	chatID string
	client *http.Client
}

// NewTelegramNotifier builds a TelegramNotifier for the configured bot token and
// chat ID.
func NewTelegramNotifier(cfg config.TelegramNotify) *TelegramNotifier {
	return &TelegramNotifier{token: cfg.BotToken, chatID: cfg.ChatID, client: &http.Client{Timeout: httpTimeout}}
}

// Name implements Notifier.
func (t *TelegramNotifier) Name() string { return "telegram" }

// Send POSTs the alert to https://api.telegram.org/bot<token>/sendMessage with
// {"chat_id", "text", "parse_mode": "Markdown"}. Non-2xx = failure.
func (t *TelegramNotifier) Send(ctx context.Context, a Alert) error {
	if t.token == "" {
		return fmt.Errorf("telegram: bot token is not set")
	}
	if t.chatID == "" {
		return fmt.Errorf("telegram: chat id is not set")
	}
	url := "https://api.telegram.org/bot" + t.token + "/sendMessage"
	payload := map[string]string{
		"chat_id":    t.chatID,
		"text":       formatAlertText(a),
		"parse_mode": "Markdown",
	}
	return postJSON2xx(ctx, t.client, url, payload, "telegram")
}
