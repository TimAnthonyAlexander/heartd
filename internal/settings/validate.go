package settings

import (
	"fmt"
	"strings"
	"time"
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
	for name, v := range map[string]float64{
		"cpu threshold": g.CPUThreshold, "mem threshold": g.MemThreshold, "disk threshold": g.DiskThreshold,
	} {
		if v < 0 || v > 100 {
			return fmt.Errorf("%s must be between 0 and 100", name)
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
