package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSmart(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "smart.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv("HEARTD_SMART_FILE", path)
	return path
}

func TestReadSmartValid(t *testing.T) {
	writeSmart(t, `{
  "generated_at": "2026-06-27T16:00:00Z",
  "disks": [
    { "device": "/dev/sda", "model": "HGST HUH728080ALE600", "serial": "VK1234",
      "health": "PASSED", "reallocated": 0, "pending": 0, "uncorrectable": 0,
      "crc_errors": 0, "temp_c": 38, "power_on_hours": 47798, "power_cycle_count": 20 },
    { "device": "/dev/sdb", "model": "HGST", "serial": "VK5678",
      "health": "PASSED", "reallocated": 4, "pending": 0, "uncorrectable": 0,
      "crc_errors": 1, "temp_c": 40, "power_on_hours": 47000, "power_cycle_count": 19 }
  ]
}`)

	rep, err := ReadSmart()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rep.Present {
		t.Fatalf("expected Present=true")
	}
	if len(rep.Disks) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(rep.Disks))
	}
	want := time.Date(2026, 6, 27, 16, 0, 0, 0, time.UTC)
	if !rep.SourceAt.Equal(want) {
		t.Errorf("expected SourceAt %v, got %v", want, rep.SourceAt)
	}
	d := rep.Disks[0]
	if d.Device != "/dev/sda" || d.Health != "PASSED" || d.PowerOnHours != 47798 || d.TempC != 38 {
		t.Errorf("unexpected first disk: %+v", d)
	}
}

func TestReadSmartMissingFile(t *testing.T) {
	t.Setenv("HEARTD_SMART_FILE", filepath.Join(t.TempDir(), "absent.json"))
	rep, err := ReadSmart()
	if err != nil {
		t.Fatalf("a missing smart file must not error, got %v", err)
	}
	if rep.Present {
		t.Fatalf("expected Present=false for a missing file")
	}
	if len(rep.Disks) != 0 {
		t.Fatalf("expected no disks, got %d", len(rep.Disks))
	}
}

func TestReadSmartMalformed(t *testing.T) {
	writeSmart(t, `{ this is not valid json `)
	rep, err := ReadSmart()
	if err != nil {
		t.Fatalf("a malformed smart file must not error, got %v", err)
	}
	if rep.Present {
		t.Fatalf("expected Present=false for a malformed file")
	}
}

func TestReadSmartFallsBackToMtime(t *testing.T) {
	path := writeSmart(t, `{ "disks": [ { "device": "/dev/sda", "health": "PASSED" } ] }`)
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	rep, err := ReadSmart()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rep.SourceAt.Equal(mtime) {
		t.Errorf("expected SourceAt to fall back to mtime %v, got %v", mtime, rep.SourceAt)
	}
}

func TestRollupHealth(t *testing.T) {
	cases := []struct {
		name          string
		health        string
		realloc       uint64
		pending       uint64
		uncorrectable uint64
		tempC         int
		want          string
	}{
		{"healthy", "PASSED", 0, 0, 0, 38, "ok"},
		{"failed health", "FAILED", 0, 0, 0, 38, "fail"},
		{"pending sectors", "PASSED", 0, 1, 0, 38, "fail"},
		{"uncorrectable", "PASSED", 0, 0, 2, 38, "fail"},
		{"reallocated warns", "PASSED", 5, 0, 0, 38, "warn"},
		{"hot warns", "PASSED", 0, 0, 0, 60, "warn"},
		{"fail beats warn", "PASSED", 5, 1, 0, 60, "fail"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RollupHealth(c.health, c.realloc, c.pending, c.uncorrectable, c.tempC)
			if got != c.want {
				t.Errorf("RollupHealth(%q, %d, %d, %d, %d) = %q, want %q",
					c.health, c.realloc, c.pending, c.uncorrectable, c.tempC, got, c.want)
			}
		})
	}
}
