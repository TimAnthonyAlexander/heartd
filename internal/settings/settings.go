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
	keyInitialized = "initialized"
	keyGeneral     = "general"
	keyNotify      = "notify"
)

// General holds the tunable sampling/threshold settings.
type General struct {
	MetricsInterval  time.Duration `json:"-"`
	PeerPollInterval time.Duration `json:"-"`
	Retention        time.Duration `json:"-"`
	CPUThreshold     float64       `json:"cpu_threshold"`
	MemThreshold     float64       `json:"mem_threshold"`
	DiskThreshold    float64       `json:"disk_threshold"`
	// Seconds fields are what actually serialize to JSON in the DB.
	MetricsIntervalSec  int64 `json:"metrics_interval_sec"`
	PeerPollIntervalSec int64 `json:"peer_poll_interval_sec"`
	RetentionSec        int64 `json:"retention_sec"`
}

// Thresholds returns the alert thresholds as a config.Thresholds.
func (g General) Thresholds() config.Thresholds {
	return config.Thresholds{
		CPUPercent:  g.CPUThreshold,
		MemPercent:  g.MemThreshold,
		DiskPercent: g.DiskThreshold,
	}
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

// Notify holds the notification channel settings.
type Notify struct {
	Email   EmailNotify   `json:"email"`
	Webhook WebhookNotify `json:"webhook"`
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
}

// Service is the cached, thread-safe settings store.
type Service struct {
	db *storage.DB

	mu      sync.RWMutex
	general General
	notify  Notify
	checks  []Check
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
	return s.reload()
}

func (s *Service) seed(cfg config.Config) error {
	g := General{
		MetricsIntervalSec:  int64(cfg.Server.MetricsInterval.Std().Seconds()),
		PeerPollIntervalSec: int64(cfg.Server.PeerPollInterval.Std().Seconds()),
		RetentionSec:        int64(cfg.Server.Retention.Std().Seconds()),
		CPUThreshold:        cfg.Thresholds.CPUPercent,
		MemThreshold:        cfg.Thresholds.MemPercent,
		DiskThreshold:       cfg.Thresholds.DiskPercent,
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
		}
		if _, err := s.db.CreateCheckConfig(cc); err != nil {
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

	s.mu.Lock()
	s.general, s.notify, s.checks = g, n, checks
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
	}
}

func toStorage(c Check) storage.CheckConfig {
	return storage.CheckConfig{
		ID: c.ID, Name: c.Name, Type: c.Type,
		IntervalSec: int64(c.Interval.Seconds()),
		TimeoutSec:  int64(c.Timeout.Seconds()),
		URL:         c.URL, Method: c.Method, Host: c.Host, Port: c.Port,
		Process: c.Process, Command: c.Command, Enabled: c.Enabled,
	}
}
