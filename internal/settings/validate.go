package settings

import (
	"fmt"
	"strings"
	"time"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

// Valid check types.
const (
	CheckHTTP    = "http"
	CheckTCP     = "tcp"
	CheckProcess = "process"
	CheckShell   = "shell"
)

const defaultCheckTimeout = 10 * time.Second

func validateGeneral(g General) error {
	if g.MetricsInterval <= 0 {
		return fmt.Errorf("metrics interval must be greater than 0")
	}
	if g.PeerPollInterval <= 0 {
		return fmt.Errorf("peer poll interval must be greater than 0")
	}
	if g.Retention <= 0 {
		return fmt.Errorf("retention must be greater than 0")
	}
	return nil
}

// Alert rule sources.
const (
	SourceCPU          = "cpu"
	SourceMem          = "mem"
	SourceDisk         = "disk"
	SourceCheckStatus  = "check_status"
	SourceCheckLatency = "check_latency"
	SourceNetRecv      = "net_recv"
	SourceNetSent      = "net_sent"
	SourcePeer         = "peer"
	SourceNoData       = "nodata"
)

// numericSources need a comparator + threshold; statusSources are state-based.
var numericSources = map[string]bool{
	SourceCPU: true, SourceMem: true, SourceDisk: true, SourceCheckLatency: true,
	SourceNetRecv: true, SourceNetSent: true, SourceNoData: true,
}
var percentSources = map[string]bool{SourceCPU: true, SourceMem: true, SourceDisk: true}

// entitySources take an entity (mount/check/peer name, "*" = any).
var entitySources = map[string]bool{
	SourceDisk: true, SourceCheckStatus: true, SourceCheckLatency: true,
	SourcePeer: true, SourceNoData: true,
}

var validComparators = map[string]bool{">=": true, ">": true, "<=": true, "<": true}

// normalizeAlertRule trims/defaults a rule before validation or persistence.
func normalizeAlertRule(r storage.AlertRule) storage.AlertRule {
	r.Name = strings.TrimSpace(r.Name)
	r.Source = strings.TrimSpace(strings.ToLower(r.Source))
	r.Entity = strings.TrimSpace(r.Entity)
	r.Severity = strings.TrimSpace(strings.ToLower(r.Severity))
	r.Comparator = strings.TrimSpace(r.Comparator)

	if r.Severity == "" {
		r.Severity = "warning"
	}
	if numericSources[r.Source] {
		if r.Comparator == "" {
			r.Comparator = ">="
		}
	} else {
		// Status sources have no numeric condition.
		r.Comparator = ""
		r.Threshold = 0
	}
	if entitySources[r.Source] {
		if r.Entity == "" {
			r.Entity = "*"
		}
	} else {
		r.Entity = ""
	}
	if r.ForSec < 0 {
		r.ForSec = 0
	}
	if r.RecoverGrace < 0 {
		r.RecoverGrace = 0
	}
	return r
}

func validateAlertRule(r storage.AlertRule) error {
	if r.Name == "" {
		return fmt.Errorf("alert name is required")
	}
	if !numericSources[r.Source] && r.Source != SourceCheckStatus && r.Source != SourcePeer {
		return fmt.Errorf("invalid alert source %q", r.Source)
	}
	if r.Severity != "warning" && r.Severity != "critical" {
		return fmt.Errorf("severity must be warning or critical")
	}
	if numericSources[r.Source] {
		if !validComparators[r.Comparator] {
			return fmt.Errorf("comparator must be one of >=, >, <=, <")
		}
		if percentSources[r.Source] && (r.Threshold < 0 || r.Threshold > 100) {
			return fmt.Errorf("threshold must be between 0 and 100 percent")
		}
		if r.Threshold < 0 {
			return fmt.Errorf("threshold must not be negative")
		}
	}
	return nil
}

func validateNotify(n Notify) error {
	if n.Email.Enabled {
		if n.Email.SMTPHost == "" {
			return fmt.Errorf("email: smtp host is required")
		}
		if n.Email.From == "" {
			return fmt.Errorf("email: from address is required")
		}
		if len(n.Email.To) == 0 {
			return fmt.Errorf("email: at least one recipient is required")
		}
	}
	if n.Webhook.Enabled && n.Webhook.URL == "" {
		return fmt.Errorf("webhook: url is required")
	}
	if n.Slack.Enabled && n.Slack.WebhookURL == "" {
		return fmt.Errorf("slack: webhook url is required")
	}
	if n.Discord.Enabled && n.Discord.WebhookURL == "" {
		return fmt.Errorf("discord: webhook url is required")
	}
	if n.Telegram.Enabled {
		if n.Telegram.BotToken == "" {
			return fmt.Errorf("telegram: bot token is required")
		}
		if n.Telegram.ChatID == "" {
			return fmt.Errorf("telegram: chat id is required")
		}
	}
	return nil
}

// normalizeCheck trims and applies defaults (e.g. timeout, http method).
func normalizeCheck(c Check) Check {
	c.Name = strings.TrimSpace(c.Name)
	c.Type = strings.TrimSpace(strings.ToLower(c.Type))
	if c.Timeout <= 0 {
		c.Timeout = defaultCheckTimeout
	}
	if c.Type == CheckHTTP && c.Method == "" {
		c.Method = "GET"
	}
	c.UserAgent = strings.TrimSpace(c.UserAgent)
	c.AcceptedStatuses = dedupeSortStatusCodes(c.AcceptedStatuses)
	return c
}

func validateCheck(c Check) error {
	if c.Name == "" {
		return fmt.Errorf("check name is required")
	}
	if c.Interval <= 0 {
		return fmt.Errorf("check interval must be greater than 0")
	}
	switch c.Type {
	case CheckHTTP:
		if c.URL == "" {
			return fmt.Errorf("http check requires a url")
		}
		for _, code := range c.AcceptedStatuses {
			if code < 100 || code > 599 {
				return fmt.Errorf("accepted status code %d is out of range (must be 100-599)", code)
			}
		}
	case CheckTCP:
		if c.Host == "" {
			return fmt.Errorf("tcp check requires a host")
		}
		if c.Port < 1 || c.Port > 65535 {
			return fmt.Errorf("tcp check requires a port between 1 and 65535")
		}
	case CheckProcess:
		if c.Process == "" {
			return fmt.Errorf("process check requires a process name")
		}
	case CheckShell:
		if c.Command == "" {
			return fmt.Errorf("shell check requires a command")
		}
	default:
		return fmt.Errorf("invalid check type %q (must be http, tcp, process, or shell)", c.Type)
	}
	return nil
}
