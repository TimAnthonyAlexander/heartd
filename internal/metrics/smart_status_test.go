package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTemp writes content to a temp file in t.TempDir and returns its path.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// TestReadSmartStatusFallback verifies that when no JSON file exists, ReadSmart
// falls back to the compact ".status" line written by a hand-rolled collector.
func TestReadSmartStatusFallback(t *testing.T) {
	status := writeTemp(t, "smart.status",
		"OK 2026-06-27T19:54:02+02:00 sda=PASSED,re=0,pend=0,unc=0 sdb=PASSED,re=0,pend=0,unc=0\n")
	t.Setenv("HEARTD_SMART_FILE", filepath.Join(t.TempDir(), "absent.json")) // missing JSON
	t.Setenv("HEARTD_SMART_STATUS_FILE", status)

	rep, err := ReadSmart()
	if err != nil {
		t.Fatalf("ReadSmart: %v", err)
	}
	if !rep.Present {
		t.Fatal("expected Present=true from the .status fallback")
	}
	if len(rep.Disks) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(rep.Disks))
	}
	if rep.Disks[0].Device != "/dev/sda" || rep.Disks[0].Health != "PASSED" {
		t.Fatalf("disk 0 = %+v, want /dev/sda PASSED", rep.Disks[0])
	}
	if rep.SourceAt.IsZero() {
		t.Fatal("expected SourceAt parsed from the timestamp")
	}
	// All counters zero → rollup ok.
	if got := RollupHealth(rep.Disks[0].Health, rep.Disks[0].Reallocated, rep.Disks[0].Pending, rep.Disks[0].Uncorrectable, rep.Disks[0].TempC); got != "ok" {
		t.Fatalf("rollup = %q, want ok", got)
	}
}

// TestReadSmartStatusFailingWithFlags verifies a FAIL line with a trailing
// "!flag" annotation parses, the counters are read, and the rollup is fail.
func TestReadSmartStatusFailingWithFlags(t *testing.T) {
	status := writeTemp(t, "smart.status",
		"FAIL 2026-06-27T19:54:02+02:00 sda=FAILED,re=60,pend=2,unc=0 !health!pending sdb=PASSED,re=0,pend=0,unc=0\n")
	t.Setenv("HEARTD_SMART_FILE", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("HEARTD_SMART_STATUS_FILE", status)

	rep, err := ReadSmart()
	if err != nil {
		t.Fatalf("ReadSmart: %v", err)
	}
	if !rep.Present || len(rep.Disks) != 2 {
		t.Fatalf("expected 2 disks present, got present=%v n=%d", rep.Present, len(rep.Disks))
	}
	sda := rep.Disks[0]
	if sda.Device != "/dev/sda" || sda.Health != "FAILED" || sda.Reallocated != 60 || sda.Pending != 2 {
		t.Fatalf("sda = %+v, want /dev/sda FAILED re=60 pend=2", sda)
	}
	if got := RollupHealth(sda.Health, sda.Reallocated, sda.Pending, sda.Uncorrectable, sda.TempC); got != "fail" {
		t.Fatalf("rollup = %q, want fail", got)
	}
}

// TestReadSmartStatusMissing verifies that with neither a JSON nor a status file,
// ReadSmart reports SMART simply absent (Present=false, no error).
func TestReadSmartStatusMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HEARTD_SMART_FILE", filepath.Join(dir, "absent.json"))
	t.Setenv("HEARTD_SMART_STATUS_FILE", filepath.Join(dir, "absent.status"))

	rep, err := ReadSmart()
	if err != nil {
		t.Fatalf("ReadSmart: %v", err)
	}
	if rep.Present {
		t.Fatal("expected Present=false when neither file exists")
	}
}

// TestReadSmartJSONPreferredOverStatus verifies the JSON file wins when both
// exist (the status fallback only applies when the JSON file is absent).
func TestReadSmartJSONPreferredOverStatus(t *testing.T) {
	jsonFile := writeTemp(t, "smart.json",
		`{"generated_at":"2026-06-27T10:00:00Z","disks":[{"device":"/dev/nvme0n1","model":"Demo","health":"PASSED","temp_c":40,"power_on_hours":100}]}`)
	status := writeTemp(t, "smart.status",
		"OK 2026-06-27T19:54:02+02:00 sda=PASSED,re=0,pend=0,unc=0\n")
	t.Setenv("HEARTD_SMART_FILE", jsonFile)
	t.Setenv("HEARTD_SMART_STATUS_FILE", status)

	rep, err := ReadSmart()
	if err != nil {
		t.Fatalf("ReadSmart: %v", err)
	}
	if !rep.Present || len(rep.Disks) != 1 || rep.Disks[0].Device != "/dev/nvme0n1" {
		t.Fatalf("expected the JSON disk to win, got %+v", rep.Disks)
	}
	if rep.Disks[0].TempC != 40 || rep.Disks[0].PowerOnHours != 100 {
		t.Fatalf("expected richer JSON fields, got %+v", rep.Disks[0])
	}
}
