package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMdstat writes content to a temp file and points HEARTD_MDSTAT_PATH at it.
func writeMdstat(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mdstat")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv("HEARTD_MDSTAT_PATH", path)
}

func byName(arrays []RaidArray) map[string]RaidArray {
	m := make(map[string]RaidArray, len(arrays))
	for _, a := range arrays {
		m[a.Name] = a
	}
	return m
}

func TestReadRaidArraysHealthyThreeMirrors(t *testing.T) {
	writeMdstat(t, `Personalities : [raid1] [raid0] [raid6] [raid5] [raid4] [raid10]
md1 : active raid1 sdb2[0] sda2[1]
      1046528 blocks super 1.2 [2/2] [UU]

md2 : active raid1 sdb3[0] sda3[1]
      7808649536 blocks super 1.2 [2/2] [UU]
      bitmap: 3/59 pages [12KB], 65536KB chunk

md0 : active raid1 sdb1[0] sda1[1]
      4189184 blocks super 1.2 [2/2] [UU]

unused devices: <none>
`)

	arrays, err := ReadRaidArrays()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(arrays) != 3 {
		t.Fatalf("expected 3 arrays, got %d: %+v", len(arrays), arrays)
	}
	for _, a := range arrays {
		if a.State != "clean" {
			t.Errorf("array %s: expected clean, got %q", a.Name, a.State)
		}
		if a.Level != "raid1" {
			t.Errorf("array %s: expected level raid1, got %q", a.Name, a.Level)
		}
		if a.TotalDevices != 2 || a.ActiveDevices != 2 {
			t.Errorf("array %s: expected [2/2], got [%d/%d]", a.Name, a.TotalDevices, a.ActiveDevices)
		}
	}
}

func TestReadRaidArraysDegraded(t *testing.T) {
	writeMdstat(t, `Personalities : [raid1]
md0 : active raid1 sda1[0] sdb1[1]
      4189184 blocks super 1.2 [2/1] [U_]

unused devices: <none>
`)

	arrays, err := ReadRaidArrays()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a := byName(arrays)["md0"]
	if a.State != "degraded" {
		t.Fatalf("expected degraded, got %q (%+v)", a.State, a)
	}
	if a.TotalDevices != 2 || a.ActiveDevices != 1 {
		t.Errorf("expected [2/1], got [%d/%d]", a.TotalDevices, a.ActiveDevices)
	}
}

func TestReadRaidArraysFaultyMemberDegraded(t *testing.T) {
	writeMdstat(t, `Personalities : [raid1]
md0 : active raid1 sda1[0] sdb1[1](F)
      4189184 blocks super 1.2 [2/2] [UU]

unused devices: <none>
`)

	arrays, err := ReadRaidArrays()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a := byName(arrays)["md0"]; a.State != "degraded" {
		t.Fatalf("a faulty (F) member must read degraded, got %q", a.State)
	}
}

func TestReadRaidArraysRebuilding(t *testing.T) {
	writeMdstat(t, `Personalities : [raid1]
md0 : active raid1 sda1[0] sdb1[2]
      4189184 blocks super 1.2 [2/1] [U_]
      [=====>...............]  recovery = 27.3% (1144000/4189184) finish=12.3min speed=45678K/sec

unused devices: <none>
`)

	arrays, err := ReadRaidArrays()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a := byName(arrays)["md0"]
	if a.State != "rebuilding" {
		t.Fatalf("expected rebuilding, got %q (%+v)", a.State, a)
	}
	if a.ResyncPercent != 27.3 {
		t.Errorf("expected 27.3%% resync, got %v", a.ResyncPercent)
	}
	if a.Detail == "" {
		t.Errorf("expected a non-empty detail for a rebuilding array")
	}
}

// TestReadRaidArraysPendingIsClean is the critical false-positive guard: an idle
// array with a queued (PENDING) resync but a healthy [UU]/[2/2] is clean, not
// degraded or rebuilding.
func TestReadRaidArraysPendingIsClean(t *testing.T) {
	writeMdstat(t, `Personalities : [raid1]
md0 : active (auto-read-only) raid1 sda1[0] sdb1[1]
      4189184 blocks super 1.2 [2/2] [UU]
      	resync=PENDING

unused devices: <none>
`)

	arrays, err := ReadRaidArrays()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a := byName(arrays)["md0"]
	if a.State != "clean" {
		t.Fatalf("PENDING with healthy [UU] must be clean, got %q (%+v)", a.State, a)
	}
	if a.Level != "raid1" {
		t.Errorf("level must skip the (auto-read-only) flag, got %q", a.Level)
	}
	if a.ResyncPercent != 0 {
		t.Errorf("PENDING has no percent, expected 0, got %v", a.ResyncPercent)
	}
}

func TestReadRaidArraysMissingFileIsEmpty(t *testing.T) {
	t.Setenv("HEARTD_MDSTAT_PATH", filepath.Join(t.TempDir(), "does-not-exist"))
	arrays, err := ReadRaidArrays()
	if err != nil {
		t.Fatalf("a missing mdstat must not error, got %v", err)
	}
	if len(arrays) != 0 {
		t.Fatalf("expected no arrays, got %d", len(arrays))
	}
}
