// Package config defines the heartd configuration structure plus loading,
// defaulting, and validation logic. All configuration lives in a single
// heartd.yaml file (see docs/SPEC.md).
//
// Durations
//
// YAML duration fields (e.g. metrics_interval: 30s, interval: 1m, retention: 7d)
// are represented by the custom Duration type rather than time.Duration. This is
// because gopkg.in/yaml.v3 cannot natively unmarshal a time.Duration from a
// string. Duration implements yaml.Unmarshaler and parses via time.ParseDuration,
// with an additional "d" (days) suffix that time.ParseDuration does not support
// (e.g. "7d" == 168h). Use Duration.Std() to obtain the underlying time.Duration.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that can be unmarshalled from YAML strings such as
// "30s", "1m", or "7d". The "d" suffix denotes days (24h) and is supported in
// addition to the units understood by time.ParseDuration.
type Duration time.Duration

// Std returns the value as a standard time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// String renders the duration using time.Duration's formatting.
func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalYAML implements yaml.Unmarshaler, parsing duration strings (with
// optional "d" days suffix) as well as plain integers (interpreted as
// nanoseconds, matching time.Duration's underlying representation).
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	// A bare integer is interpreted as a number of nanoseconds, matching
	// time.Duration's underlying representation.
	if value.Tag == "!!int" {
		var n int64
		if err := value.Decode(&n); err != nil {
			return fmt.Errorf("invalid duration %q: %w", value.Value, err)
		}
		*d = Duration(n)
		return nil
	}
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	parsed, err := ParseDuration(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// MarshalYAML renders the duration as a string so round-trips stay readable.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// ParseDuration parses a duration string, supporting an additional "d" (days)
// suffix on top of time.ParseDuration's units.
func ParseDuration(s string) (Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		numPart := strings.TrimSuffix(s, "d")
		days, err := strconv.ParseFloat(numPart, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		return Duration(time.Duration(days * 24 * float64(time.Hour))), nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return Duration(parsed), nil
}

// Config is the root heartd configuration.
type Config struct {
	Server     Server     `yaml:"server"`
	Peers      []Peer     `yaml:"peers"`
	Checks     []Check    `yaml:"checks"`
	Thresholds Thresholds `yaml:"thresholds"`
	Notify     Notify     `yaml:"notify"`
}

// Server holds node-level settings.
type Server struct {
	Name            string     `yaml:"name"`             // node name; default = os.Hostname() or "heartd"
	Port            int        `yaml:"port"`             // default 9300
	BasicAuth       *BasicAuth `yaml:"basic_auth"`       // optional, nil when absent
	MetricsInterval Duration   `yaml:"metrics_interval"` // how often to sample metrics; default 30s
	Retention       Duration   `yaml:"retention"`        // rolling metric retention window; default 7 days (168h)
	DBPath          string     `yaml:"db_path"`          // sqlite file path; default "heartd.db"
	AdvertiseURL    string     `yaml:"advertise_url"`    // how peers should reach this node (optional)
	PeerPollInterval Duration  `yaml:"peer_poll_interval"` // how often to poll peers; default 15s
}

// BasicAuth holds optional HTTP basic auth credentials for the dashboard.
type BasicAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Peer describes another heartd node.
type Peer struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	Secret string `yaml:"secret"` // shared secret
}

// Check describes a single service health check.
type Check struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"` // http | tcp | process | shell
	Interval Duration `yaml:"interval"`
	Timeout  Duration `yaml:"timeout"`
	// type-specific params:
	URL     string `yaml:"url"`     // http
	Method  string `yaml:"method"`  // http (default GET)
	Host    string `yaml:"host"`    // tcp
	PortNum int    `yaml:"port"`    // tcp
	Process string `yaml:"process"` // process
	Command string `yaml:"command"` // shell
}

// Thresholds defines system-metric alert thresholds (percentages).
type Thresholds struct {
	CPUPercent  float64 `yaml:"cpu_percent"`  // default 90
	MemPercent  float64 `yaml:"mem_percent"`  // default 90
	DiskPercent float64 `yaml:"disk_percent"` // default 90
}

// Notify holds optional alert notification channels.
type Notify struct {
	Email   *EmailNotify   `yaml:"email"`
	Webhook *WebhookNotify `yaml:"webhook"`
}

// EmailNotify configures SMTP email alerts.
type EmailNotify struct {
	SMTPHost      string   `yaml:"smtp_host"`
	SMTPPort      int      `yaml:"smtp_port"`
	Username      string   `yaml:"username"`
	Password      string   `yaml:"password"`
	From          string   `yaml:"from"`
	To            []string `yaml:"to"`
	SubjectPrefix string   `yaml:"subject_prefix"`
}

// WebhookNotify configures a webhook alert target.
type WebhookNotify struct {
	URL string `yaml:"url"`
}

// Valid check types.
const (
	CheckHTTP    = "http"
	CheckTCP     = "tcp"
	CheckProcess = "process"
	CheckShell   = "shell"
)

// Defaults.
const (
	defaultPort            = 9300
	defaultMetricsInterval = 30 * time.Second
	defaultPeerPollInterval = 15 * time.Second
	defaultRetention       = 7 * 24 * time.Hour
	defaultDBPath          = "heartd.db"
	defaultCheckTimeout    = 10 * time.Second
	defaultThreshold       = 90.0
	defaultHTTPMethod      = "GET"
	defaultServerName      = "heartd"
)

// Default returns a fully-populated config with all default values applied.
func Default() Config {
	var cfg Config
	cfg.applyDefaults()
	return cfg
}

// Load reads YAML from path, applies defaults for any unset fields, validates,
// and returns the config. A missing file is NOT an error — it returns Default().
// A malformed or invalid file IS an error.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	// A blank (whitespace-only) file is equivalent to no config: use defaults.
	if strings.TrimSpace(string(data)) == "" {
		return Default(), nil
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		// A comment-only file decodes to EOF: also treat as no config.
		if errors.Is(err, io.EOF) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return cfg, nil
}

// applyDefaults fills in any unset fields with their default values.
func (c *Config) applyDefaults() {
	if c.Server.Name == "" {
		c.Server.Name = defaultHostname()
	}
	if c.Server.Port == 0 {
		c.Server.Port = defaultPort
	}
	if c.Server.MetricsInterval == 0 {
		c.Server.MetricsInterval = Duration(defaultMetricsInterval)
	}
	if c.Server.Retention == 0 {
		c.Server.Retention = Duration(defaultRetention)
	}
	if c.Server.DBPath == "" {
		c.Server.DBPath = defaultDBPath
	}
	if c.Server.PeerPollInterval == 0 {
		c.Server.PeerPollInterval = Duration(defaultPeerPollInterval)
	}

	if c.Thresholds.CPUPercent == 0 {
		c.Thresholds.CPUPercent = defaultThreshold
	}
	if c.Thresholds.MemPercent == 0 {
		c.Thresholds.MemPercent = defaultThreshold
	}
	if c.Thresholds.DiskPercent == 0 {
		c.Thresholds.DiskPercent = defaultThreshold
	}

	for i := range c.Checks {
		if c.Checks[i].Timeout == 0 {
			c.Checks[i].Timeout = Duration(defaultCheckTimeout)
		}
		if c.Checks[i].Type == CheckHTTP && c.Checks[i].Method == "" {
			c.Checks[i].Method = defaultHTTPMethod
		}
	}
}

// defaultHostname returns the machine hostname, or "heartd" if it cannot be
// determined.
func defaultHostname() string {
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	return defaultServerName
}

// Validate checks the config for correctness and returns the first error found.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", c.Server.Port)
	}
	if c.Server.MetricsInterval.Std() <= 0 {
		return fmt.Errorf("server.metrics_interval must be greater than 0")
	}
	if c.Server.Retention.Std() <= 0 {
		return fmt.Errorf("server.retention must be greater than 0")
	}
	if c.Server.PeerPollInterval.Std() <= 0 {
		return fmt.Errorf("server.peer_poll_interval must be greater than 0")
	}

	if c.Server.BasicAuth != nil {
		if c.Server.BasicAuth.Username == "" {
			return fmt.Errorf("server.basic_auth.username must not be empty")
		}
		if c.Server.BasicAuth.Password == "" {
			return fmt.Errorf("server.basic_auth.password must not be empty")
		}
	}

	if err := validateThreshold("thresholds.cpu_percent", c.Thresholds.CPUPercent); err != nil {
		return err
	}
	if err := validateThreshold("thresholds.mem_percent", c.Thresholds.MemPercent); err != nil {
		return err
	}
	if err := validateThreshold("thresholds.disk_percent", c.Thresholds.DiskPercent); err != nil {
		return err
	}

	for i := range c.Checks {
		if err := c.Checks[i].validate(i); err != nil {
			return err
		}
	}

	return nil
}

func validateThreshold(name string, v float64) error {
	if v < 0 || v > 100 {
		return fmt.Errorf("%s must be between 0 and 100, got %v", name, v)
	}
	return nil
}

func (ch *Check) validate(index int) error {
	label := fmt.Sprintf("checks[%d]", index)
	if ch.Name == "" {
		return fmt.Errorf("%s.name must not be empty", label)
	}
	label = fmt.Sprintf("checks[%d] (%s)", index, ch.Name)

	if ch.Interval.Std() <= 0 {
		return fmt.Errorf("%s.interval must be greater than 0", label)
	}

	switch ch.Type {
	case CheckHTTP:
		if ch.URL == "" {
			return fmt.Errorf("%s: http check requires url", label)
		}
	case CheckTCP:
		if ch.Host == "" {
			return fmt.Errorf("%s: tcp check requires host", label)
		}
		if ch.PortNum < 1 || ch.PortNum > 65535 {
			return fmt.Errorf("%s: tcp check requires port between 1 and 65535, got %d", label, ch.PortNum)
		}
	case CheckProcess:
		if ch.Process == "" {
			return fmt.Errorf("%s: process check requires process", label)
		}
	case CheckShell:
		if ch.Command == "" {
			return fmt.Errorf("%s: shell check requires command", label)
		}
	case "":
		return fmt.Errorf("%s.type must not be empty", label)
	default:
		return fmt.Errorf("%s.type %q is invalid (must be http, tcp, process, or shell)", label, ch.Type)
	}

	return nil
}
