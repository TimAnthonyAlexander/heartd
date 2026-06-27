// Package settings is the runtime-configurable source of truth for heartd:
// metric intervals, retention, alert thresholds, notification channels, and the
// service-check list. It is backed by storage (SQLite) and seeded once from the
// YAML config on first run. Values are cached in memory and updated on write, so
// the runtime can read current settings cheaply on every cycle and pick up edits
// without a restart.
package settings

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

const (
	keyInitialized  = "initialized"
	keyGeneral      = "general"
	keyNotify       = "notify"
	keyAlertsSeeded = "alerts_seeded"
)

// General holds the tunable sampling/retention settings. Alert thresholds moved
// out of here into configurable alert rules (see AlertRule).
type General struct {
	MetricsInterval  time.Duration `json:"-"`
	PeerPollInterval time.Duration `json:"-"`
	Retention        time.Duration `json:"-"`
	// Seconds fields are what actually serialize to JSON in the DB.
	MetricsIntervalSec  int64 `json:"metrics_interval_sec"`
	PeerPollIntervalSec int64 `json:"peer_poll_interval_sec"`
	RetentionSec        int64 `json:"retention_sec"`
}

// EmailNotify configures SMTP alerts.
type EmailNotify struct {
	Enabled       bool     `json:"enabled"`
	SMTPHost      string   `json:"smtp_host"`
	SMTPPort      int      `json:"smtp_port"`
	Username      string   `json:"username"`
	Password      string   `json:"password"`
	From          string   `json:"from"`
	To            []string `json:"to"`
	SubjectPrefix string   `json:"subject_prefix"`
}

// WebhookNotify configures a webhook alert target.
type WebhookNotify struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

// SlackNotify configures a Slack incoming-webhook alert target.
type SlackNotify struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
}

// DiscordNotify configures a Discord webhook alert target.
type DiscordNotify struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
}

// TelegramNotify configures a Telegram bot alert target. BotToken is a secret and
// is handled exactly like the SMTP password (persisted and echoed back as-is).
type TelegramNotify struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// Notify holds the notification channel settings.
type Notify struct {
	Email    EmailNotify    `json:"email"`
	Webhook  WebhookNotify  `json:"webhook"`
	Slack    SlackNotify    `json:"slack"`
	Discord  DiscordNotify  `json:"discord"`
	Telegram TelegramNotify `json:"telegram"`
}

// Check is a configurable service check.
type Check struct {
	ID       int64         `json:"id"`
	Name     string        `json:"name"`
	Type     string        `json:"type"`
	Interval time.Duration `json:"-"`
	Timeout  time.Duration `json:"-"`
	URL      string        `json:"url"`
	Method   string        `json:"method"`
	Host     string        `json:"host"`
	Port     int           `json:"port"`
	Process  string        `json:"process"`
	Command  string        `json:"command"`
	Enabled  bool          `json:"enabled"`
	// AcceptAny (http only): treat any HTTP response as healthy. Takes
	// precedence over AcceptedStatuses.
	AcceptAny bool `json:"accept_any"`
	// AcceptedStatuses (http only): explicit list of healthy status codes.
	// Empty means the 2xx default.
	AcceptedStatuses []int `json:"accepted_statuses"`
	// UserAgent (http only): override the default health-check User-Agent.
	UserAgent string `json:"user_agent"`
}

// Service is the cached, thread-safe settings store.
type Service struct {
	db *storage.DB

	mu      sync.RWMutex
	general General
	notify  Notify
	checks  []Check
	alerts  []storage.AlertRule
}

// New builds a settings Service over the database.
func New(db *storage.DB) *Service {
	return &Service{db: db}
}

// Load seeds the database from the YAML config on first run, then loads the
// current settings into the in-memory cache.
func (s *Service) Load(cfg config.Config) error {
	if _, ok, err := s.db.GetSetting(keyInitialized); err != nil {
		return err
	} else if !ok {
		if err := s.seed(cfg); err != nil {
			return fmt.Errorf("seed settings: %w", err)
		}
		if err := s.db.SetSetting(keyInitialized, "1"); err != nil {
			return err
		}
	}
	// Seed the default alert rules once. This is gated separately from the main
	// init flag so existing databases (initialized before alert rules existed)
	// still get the defaults on upgrade, while a user who later deletes all rules
	// won't have them resurrected on the next restart.
	if err := s.seedAlertRulesOnce(cfg); err != nil {
		return fmt.Errorf("seed alert rules: %w", err)
	}
	return s.reload()
}

// seedAlertRulesOnce installs the default alert rules the first time it runs
// (guarded by keyAlertsSeeded), mirroring the legacy fixed thresholds plus
// check-failing and peer-down alerts so behavior is preserved.
func (s *Service) seedAlertRulesOnce(cfg config.Config) error {
	if _, ok, err := s.db.GetSetting(keyAlertsSeeded); err != nil {
		return err
	} else if ok {
		return nil
	}

	defaults := []storage.AlertRule{
		{Name: "Service check failing", Enabled: true, Source: "check_status", Entity: "*", Severity: "critical"},
		{Name: "Node unreachable", Enabled: true, Source: "peer", Entity: "*", Severity: "critical"},
	}
	if cfg.Thresholds.CPUPercent > 0 {
		defaults = append(defaults, storage.AlertRule{Name: "High CPU usage", Enabled: true, Source: "cpu", Comparator: ">=", Threshold: cfg.Thresholds.CPUPercent, Severity: "critical"})
	}
	if cfg.Thresholds.MemPercent > 0 {
		defaults = append(defaults, storage.AlertRule{Name: "High memory usage", Enabled: true, Source: "mem", Comparator: ">=", Threshold: cfg.Thresholds.MemPercent, Severity: "critical"})
	}
	if cfg.Thresholds.DiskPercent > 0 {
		defaults = append(defaults, storage.AlertRule{Name: "Disk almost full", Enabled: true, Source: "disk", Entity: "*", Comparator: ">=", Threshold: cfg.Thresholds.DiskPercent, Severity: "critical"})
	}
	for _, r := range defaults {
		if _, err := s.db.CreateAlertRule(normalizeAlertRule(r)); err != nil {
			return err
		}
	}
	return s.db.SetSetting(keyAlertsSeeded, "1")
}

func (s *Service) seed(cfg config.Config) error {
	g := General{
		MetricsIntervalSec:  int64(cfg.Server.MetricsInterval.Std().Seconds()),
		PeerPollIntervalSec: int64(cfg.Server.PeerPollInterval.Std().Seconds()),
		RetentionSec:        int64(cfg.Server.Retention.Std().Seconds()),
	}
	if err := s.writeJSON(keyGeneral, g); err != nil {
		return err
	}

	var n Notify
	if cfg.Notify.Email != nil {
		e := cfg.Notify.Email
		n.Email = EmailNotify{
			Enabled: true, SMTPHost: e.SMTPHost, SMTPPort: e.SMTPPort,
			Username: e.Username, Password: e.Password, From: e.From,
			To: e.To, SubjectPrefix: e.SubjectPrefix,
		}
	}
	if cfg.Notify.Webhook != nil {
		n.Webhook = WebhookNotify{Enabled: true, URL: cfg.Notify.Webhook.URL}
	}
	if cfg.Notify.Slack != nil {
		n.Slack = SlackNotify{Enabled: true, WebhookURL: cfg.Notify.Slack.WebhookURL}
	}
	if cfg.Notify.Discord != nil {
		n.Discord = DiscordNotify{Enabled: true, WebhookURL: cfg.Notify.Discord.WebhookURL}
	}
	if cfg.Notify.Telegram != nil {
		n.Telegram = TelegramNotify{
			Enabled:  true,
			BotToken: cfg.Notify.Telegram.BotToken,
			ChatID:   cfg.Notify.Telegram.ChatID,
		}
	}
	if err := s.writeJSON(keyNotify, n); err != nil {
		return err
	}

	for _, c := range cfg.Checks {
		cc := storage.CheckConfig{
			Name: c.Name, Type: c.Type,
			IntervalSec: int64(c.Interval.Std().Seconds()),
			TimeoutSec:  int64(c.Timeout.Std().Seconds()),
			URL:         c.URL, Method: c.Method, Host: c.Host, Port: c.PortNum,
			Process: c.Process, Command: c.Command, Enabled: true,
			AcceptAny:        c.AcceptAny,
			AcceptedStatuses: formatStatusCodes(c.AcceptedStatuses),
			UserAgent:        c.UserAgent,
		}
		if _, err := s.db.CreateCheckConfig(cc); err != nil {
			return err
		}
	}

	// Seed the peer list from config ONCE. After first run the peer table is the
	// source of truth and is managed live from the dashboard, so peers removed in
	// the UI are not resurrected from YAML on the next restart.
	for _, p := range cfg.Peers {
		if err := s.db.UpsertPeer(storage.Peer{Name: p.Name, URL: p.URL, Secret: p.Secret}); err != nil {
			return err
		}
	}

	// Seed this node's own display name as its LOCAL alias ONCE, so an operator
	// can label each node in its own YAML and have that name propagate to every
	// dashboard that polls it. Gated by the same first-run flag as the rest of
	// seed(), so a later dashboard rename is never clobbered on restart.
	if cfg.Server.DisplayName != "" {
		if err := s.db.SetNodeAlias(cfg.Server.Name, cfg.Server.DisplayName); err != nil {
			return err
		}
	}
	return nil
}

// reload refreshes the entire in-memory cache from the database.
func (s *Service) reload() error {
	var g General
	if err := s.readJSON(keyGeneral, &g); err != nil {
		return err
	}
	g = normalizeGeneral(g)

	var n Notify
	if err := s.readJSON(keyNotify, &n); err != nil {
		return err
	}

	checks, err := s.loadChecks()
	if err != nil {
		return err
	}

	alerts, err := s.db.ListAlertRules()
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.general, s.notify, s.checks, s.alerts = g, n, checks, alerts
	s.mu.Unlock()
	return nil
}

func (s *Service) loadChecks() ([]Check, error) {
	rows, err := s.db.ListCheckConfigs()
	if err != nil {
		return nil, err
	}
	checks := make([]Check, 0, len(rows))
	for _, r := range rows {
		checks = append(checks, fromStorage(r))
	}
	return checks, nil
}

// AlertRules returns a copy of the current alert rule list.
func (s *Service) AlertRules() []storage.AlertRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]storage.AlertRule, len(s.alerts))
	copy(out, s.alerts)
	return out
}

// CreateAlertRule validates and inserts a new rule, returning it with its ID.
func (s *Service) CreateAlertRule(r storage.AlertRule) (storage.AlertRule, error) {
	r = normalizeAlertRule(r)
	if err := validateAlertRule(r); err != nil {
		return storage.AlertRule{}, err
	}
	created, err := s.db.CreateAlertRule(r)
	if err != nil {
		return storage.AlertRule{}, err
	}
	if err := s.refreshAlertRules(); err != nil {
		return storage.AlertRule{}, err
	}
	return created, nil
}

// UpdateAlertRule validates and updates an existing rule.
func (s *Service) UpdateAlertRule(r storage.AlertRule) error {
	r = normalizeAlertRule(r)
	if err := validateAlertRule(r); err != nil {
		return err
	}
	if err := s.db.UpdateAlertRule(r); err != nil {
		return err
	}
	return s.refreshAlertRules()
}

// DeleteAlertRule removes a rule by ID.
func (s *Service) DeleteAlertRule(id int64) error {
	if err := s.db.DeleteAlertRule(id); err != nil {
		return err
	}
	return s.refreshAlertRules()
}

func (s *Service) refreshAlertRules() error {
	alerts, err := s.db.ListAlertRules()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.alerts = alerts
	s.mu.Unlock()
	return nil
}

// General returns a copy of the current general settings.
func (s *Service) General() General {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.general
}

// Notify returns a copy of the current notification settings.
func (s *Service) Notify() Notify {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.notify
}

// Checks returns a copy of the current check list.
func (s *Service) Checks() []Check {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Check, len(s.checks))
	copy(out, s.checks)
	return out
}

// SetGeneral validates and persists the general settings.
func (s *Service) SetGeneral(g General) error {
	g = normalizeGeneral(g)
	if err := validateGeneral(g); err != nil {
		return err
	}
	if err := s.writeJSON(keyGeneral, g); err != nil {
		return err
	}
	s.mu.Lock()
	s.general = g
	s.mu.Unlock()
	return nil
}

// SetNotify persists the notification settings.
func (s *Service) SetNotify(n Notify) error {
	if err := validateNotify(n); err != nil {
		return err
	}
	if err := s.writeJSON(keyNotify, n); err != nil {
		return err
	}
	s.mu.Lock()
	s.notify = n
	s.mu.Unlock()
	return nil
}

// CreateCheck validates and inserts a new check, returning it with its ID.
func (s *Service) CreateCheck(c Check) (Check, error) {
	c = normalizeCheck(c)
	if err := validateCheck(c); err != nil {
		return Check{}, err
	}
	created, err := s.db.CreateCheckConfig(toStorage(c))
	if err != nil {
		return Check{}, err
	}
	if err := s.refreshChecks(); err != nil {
		return Check{}, err
	}
	return fromStorage(created), nil
}

// UpdateCheck validates and updates an existing check.
func (s *Service) UpdateCheck(c Check) error {
	c = normalizeCheck(c)
	if err := validateCheck(c); err != nil {
		return err
	}
	if err := s.db.UpdateCheckConfig(toStorage(c)); err != nil {
		return err
	}
	return s.refreshChecks()
}

// DeleteCheck removes a check by ID and clears its stored status for node.
func (s *Service) DeleteCheck(id int64, node string) error {
	// Find the name before deleting so we can clear its status row.
	var name string
	for _, c := range s.Checks() {
		if c.ID == id {
			name = c.Name
			break
		}
	}
	if err := s.db.DeleteCheckConfig(id); err != nil {
		return err
	}
	if name != "" {
		_ = s.db.DeleteCheckStatus(node, name)
	}
	return s.refreshChecks()
}

func (s *Service) refreshChecks() error {
	checks, err := s.loadChecks()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.checks = checks
	s.mu.Unlock()
	return nil
}

func (s *Service) writeJSON(key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.db.SetSetting(key, string(data))
}

func (s *Service) readJSON(key string, v any) error {
	raw, ok, err := s.db.GetSetting(key)
	if err != nil {
		return err
	}
	if !ok {
		return nil // leave v at its zero value
	}
	return json.Unmarshal([]byte(raw), v)
}

func normalizeGeneral(g General) General {
	g.MetricsInterval = time.Duration(g.MetricsIntervalSec) * time.Second
	g.PeerPollInterval = time.Duration(g.PeerPollIntervalSec) * time.Second
	g.Retention = time.Duration(g.RetentionSec) * time.Second
	return g
}

func fromStorage(r storage.CheckConfig) Check {
	return Check{
		ID: r.ID, Name: r.Name, Type: r.Type,
		Interval: time.Duration(r.IntervalSec) * time.Second,
		Timeout:  time.Duration(r.TimeoutSec) * time.Second,
		URL:      r.URL, Method: r.Method, Host: r.Host, Port: r.Port,
		Process: r.Process, Command: r.Command, Enabled: r.Enabled,
		AcceptAny:        r.AcceptAny,
		AcceptedStatuses: parseStatusCodes(r.AcceptedStatuses),
		UserAgent:        r.UserAgent,
	}
}

func toStorage(c Check) storage.CheckConfig {
	return storage.CheckConfig{
		ID: c.ID, Name: c.Name, Type: c.Type,
		IntervalSec: int64(c.Interval.Seconds()),
		TimeoutSec:  int64(c.Timeout.Seconds()),
		URL:         c.URL, Method: c.Method, Host: c.Host, Port: c.Port,
		Process: c.Process, Command: c.Command, Enabled: c.Enabled,
		AcceptAny:        c.AcceptAny,
		AcceptedStatuses: formatStatusCodes(c.AcceptedStatuses),
		UserAgent:        c.UserAgent,
	}
}
