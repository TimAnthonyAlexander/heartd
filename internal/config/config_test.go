package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Server.Port != 9300 {
		t.Errorf("Port = %d, want 9300", cfg.Server.Port)
	}
	if cfg.Server.MetricsInterval.Std() != 30*time.Second {
		t.Errorf("MetricsInterval = %v, want 30s", cfg.Server.MetricsInterval.Std())
	}
	if cfg.Server.Retention.Std() != 7*24*time.Hour {
		t.Errorf("Retention = %v, want 168h", cfg.Server.Retention.Std())
	}
	if cfg.Server.DBPath != "heartd.db" {
		t.Errorf("DBPath = %q, want heartd.db", cfg.Server.DBPath)
	}
	if cfg.Server.Name == "" {
		t.Error("Name should not be empty")
	}
	if cfg.Thresholds.CPUPercent != 90 || cfg.Thresholds.MemPercent != 90 || cfg.Thresholds.DiskPercent != 90 {
		t.Errorf("thresholds = %+v, want all 90", cfg.Thresholds)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Default() must be valid, got %v", err)
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"30s", 30 * time.Second, false},
		{"1m", time.Minute, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"0.5d", 12 * time.Hour, false},
		{"90m", 90 * time.Minute, false},
		{"", 0, false},
		{"bogus", 0, true},
		{"5x", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseDuration(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseDuration(%q) expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDuration(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got.Std() != tt.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tt.in, got.Std(), tt.want)
		}
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "heartd.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	yaml := `
server:
  name: web-1
  port: 8080
  metrics_interval: 15s
  retention: 3d
  db_path: /var/lib/heartd.db
  basic_auth:
    username: admin
    password: secret
peers:
  - name: db-1
    url: http://db-1:9300
    secret: shared
checks:
  - name: api
    type: http
    interval: 1m
    url: https://example.com/health
  - name: postgres
    type: tcp
    interval: 30s
    host: localhost
    port: 5432
  - name: nginx
    type: process
    interval: 1m
    process: nginx
  - name: backup
    type: shell
    interval: 5m
    command: /usr/bin/check-backup
thresholds:
  cpu_percent: 80
  mem_percent: 85
  disk_percent: 95
notify:
  email:
    smtp_host: smtp.example.com
    smtp_port: 587
    from: alerts@example.com
    to:
      - ops@example.com
    subject_prefix: "[heartd] "
  webhook:
    url: https://hooks.example.com/x
`
	path := writeTemp(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Name != "web-1" {
		t.Errorf("Name = %q, want web-1", cfg.Server.Name)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Server.MetricsInterval.Std() != 15*time.Second {
		t.Errorf("MetricsInterval = %v, want 15s", cfg.Server.MetricsInterval.Std())
	}
	if cfg.Server.Retention.Std() != 3*24*time.Hour {
		t.Errorf("Retention = %v, want 72h", cfg.Server.Retention.Std())
	}
	if cfg.Server.BasicAuth == nil || cfg.Server.BasicAuth.Username != "admin" {
		t.Errorf("BasicAuth not parsed: %+v", cfg.Server.BasicAuth)
	}
	if len(cfg.Peers) != 1 || cfg.Peers[0].Name != "db-1" {
		t.Errorf("Peers not parsed: %+v", cfg.Peers)
	}
	if len(cfg.Checks) != 4 {
		t.Fatalf("Checks len = %d, want 4", len(cfg.Checks))
	}
	// http check should default Method to GET
	if cfg.Checks[0].Method != "GET" {
		t.Errorf("http check Method = %q, want GET", cfg.Checks[0].Method)
	}
	// timeout should default to 10s
	if cfg.Checks[0].Timeout.Std() != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", cfg.Checks[0].Timeout.Std())
	}
	if cfg.Checks[1].PortNum != 5432 {
		t.Errorf("tcp PortNum = %d, want 5432", cfg.Checks[1].PortNum)
	}
	if cfg.Notify.Email == nil || cfg.Notify.Email.SMTPPort != 587 {
		t.Errorf("Email notify not parsed: %+v", cfg.Notify.Email)
	}
	if cfg.Notify.Webhook == nil || cfg.Notify.Webhook.URL == "" {
		t.Errorf("Webhook notify not parsed: %+v", cfg.Notify.Webhook)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() of missing file should not error, got %v", err)
	}
	if cfg.Server.Port != 9300 {
		t.Errorf("missing file should yield defaults, Port = %d", cfg.Server.Port)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("defaults from missing file must validate, got %v", err)
	}
}

func TestLoadEmptyFileReturnsDefaults(t *testing.T) {
	for _, content := range []string{"", "   \n\t\n", "# just a comment\n"} {
		path := writeTemp(t, content)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() of empty file %q should not error, got %v", content, err)
		}
		if cfg.Server.Port != 9300 {
			t.Errorf("empty file should yield defaults, Port = %d", cfg.Server.Port)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("defaults from empty file must validate, got %v", err)
		}
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	path := writeTemp(t, "server: : : bad\n  port: nope\n")
	if _, err := Load(path); err == nil {
		t.Error("Load() of malformed YAML should error")
	}
}

func TestValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "bad port",
			yaml: "server:\n  port: 70000\n",
		},
		{
			name: "threshold out of range",
			yaml: "thresholds:\n  cpu_percent: 150\n",
		},
		{
			name: "http check missing url",
			yaml: "checks:\n  - name: api\n    type: http\n    interval: 1m\n",
		},
		{
			name: "tcp check missing host",
			yaml: "checks:\n  - name: db\n    type: tcp\n    interval: 1m\n    port: 5432\n",
		},
		{
			name: "unknown check type",
			yaml: "checks:\n  - name: x\n    type: ping\n    interval: 1m\n",
		},
		{
			name: "check missing name",
			yaml: "checks:\n  - type: shell\n    interval: 1m\n    command: ls\n",
		},
		{
			name: "basic auth missing password",
			yaml: "server:\n  basic_auth:\n    username: admin\n",
		},
		{
			name: "zero interval check",
			yaml: "checks:\n  - name: x\n    type: shell\n    command: ls\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTemp(t, tt.yaml)
			if _, err := Load(path); err == nil {
				t.Errorf("expected validation error for %q", tt.name)
			}
		})
	}
}

func TestDurationIntegerFallback(t *testing.T) {
	// A bare integer should be interpreted as nanoseconds.
	path := writeTemp(t, "server:\n  metrics_interval: 5000000000\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.MetricsInterval.Std() != 5*time.Second {
		t.Errorf("integer duration = %v, want 5s", cfg.Server.MetricsInterval.Std())
	}
}
