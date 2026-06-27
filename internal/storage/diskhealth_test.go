package storage

import (
	"testing"
	"time"
)

func TestReplaceRaidArrays(t *testing.T) {
	db := openTestDB(t)
	at := time.Now().UTC()

	first := []RaidArrayRow{
		{Node: "n1", Name: "md2", Level: "raid1", State: "clean", TotalDevices: 2, ActiveDevices: 2, At: at},
		{Node: "n1", Name: "md0", Level: "raid1", State: "degraded", TotalDevices: 2, ActiveDevices: 1, At: at},
	}
	if err := db.ReplaceRaidArrays("n1", first); err != nil {
		t.Fatalf("ReplaceRaidArrays: %v", err)
	}

	got, err := db.RaidArrays("n1")
	if err != nil {
		t.Fatalf("RaidArrays: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 arrays, got %d", len(got))
	}
	// Ordered by name ascending: md0 before md2.
	if got[0].Name != "md0" || got[1].Name != "md2" {
		t.Errorf("expected order md0, md2; got %s, %s", got[0].Name, got[1].Name)
	}

	// Replace with a smaller set: the old rows are wiped, not merged.
	second := []RaidArrayRow{
		{Node: "n1", Name: "md2", Level: "raid1", State: "rebuilding", TotalDevices: 2, ActiveDevices: 1, ResyncPercent: 50, Detail: "recovery = 50%", At: at},
	}
	if err := db.ReplaceRaidArrays("n1", second); err != nil {
		t.Fatalf("ReplaceRaidArrays (second): %v", err)
	}
	got, _ = db.RaidArrays("n1")
	if len(got) != 1 || got[0].Name != "md2" || got[0].State != "rebuilding" || got[0].ResyncPercent != 50 {
		t.Fatalf("replace did not overwrite: %+v", got)
	}
}

func TestReplaceRaidArraysEmptyClearsNode(t *testing.T) {
	db := openTestDB(t)
	at := time.Now().UTC()

	if err := db.ReplaceRaidArrays("n1", []RaidArrayRow{
		{Node: "n1", Name: "md0", Level: "raid1", State: "clean", TotalDevices: 2, ActiveDevices: 2, At: at},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.ReplaceRaidArrays("n1", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ := db.RaidArrays("n1")
	if len(got) != 0 {
		t.Fatalf("expected node cleared, got %d rows", len(got))
	}
}

func TestReplaceSmartDisks(t *testing.T) {
	db := openTestDB(t)
	at := time.Now().UTC()
	src := at.Add(-5 * time.Minute)

	rows := []SmartDiskRow{
		{Node: "n1", Device: "/dev/sdb", Model: "M2", Serial: "S2", Health: "PASSED", Reallocated: 3, TempC: 40, PowerOnHours: 100, SourceAt: src, At: at},
		{Node: "n1", Device: "/dev/sda", Model: "M1", Serial: "S1", Health: "PASSED", Pending: 0, TempC: 38, PowerOnHours: 200, SourceAt: src, At: at},
	}
	if err := db.ReplaceSmartDisks("n1", rows); err != nil {
		t.Fatalf("ReplaceSmartDisks: %v", err)
	}

	got, err := db.SmartDisks("n1")
	if err != nil {
		t.Fatalf("SmartDisks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(got))
	}
	// Ordered by device ascending: /dev/sda before /dev/sdb.
	if got[0].Device != "/dev/sda" || got[1].Device != "/dev/sdb" {
		t.Errorf("expected order sda, sdb; got %s, %s", got[0].Device, got[1].Device)
	}
	if got[1].Reallocated != 3 || got[1].TempC != 40 {
		t.Errorf("unexpected sdb values: %+v", got[1])
	}
	if got[0].SourceAt.Unix() != src.Unix() {
		t.Errorf("expected SourceAt %v, got %v", src, got[0].SourceAt)
	}
}

func TestReplaceSmartDisksEmptyClearsNode(t *testing.T) {
	db := openTestDB(t)
	at := time.Now().UTC()
	if err := db.ReplaceSmartDisks("n1", []SmartDiskRow{
		{Node: "n1", Device: "/dev/sda", Health: "PASSED", SourceAt: at, At: at},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.ReplaceSmartDisks("n1", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ := db.SmartDisks("n1")
	if len(got) != 0 {
		t.Fatalf("expected node cleared, got %d rows", len(got))
	}
}

// TestDiskHealthCrossNodeIsolation verifies that replacing one node's snapshots
// never touches another node's rows — peers and the local node coexist.
func TestDiskHealthCrossNodeIsolation(t *testing.T) {
	db := openTestDB(t)
	at := time.Now().UTC()

	if err := db.ReplaceRaidArrays("local", []RaidArrayRow{
		{Node: "local", Name: "md0", Level: "raid1", State: "clean", TotalDevices: 2, ActiveDevices: 2, At: at},
	}); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	if err := db.ReplaceSmartDisks("peer", []SmartDiskRow{
		{Node: "peer", Device: "/dev/sda", Health: "PASSED", SourceAt: at, At: at},
	}); err != nil {
		t.Fatalf("seed peer: %v", err)
	}

	// Clearing the peer's SMART must leave local's RAID intact.
	if err := db.ReplaceSmartDisks("peer", nil); err != nil {
		t.Fatalf("clear peer smart: %v", err)
	}
	if got, _ := db.RaidArrays("local"); len(got) != 1 {
		t.Fatalf("local RAID must be untouched, got %d rows", len(got))
	}
	if got, _ := db.SmartDisks("peer"); len(got) != 0 {
		t.Fatalf("peer SMART must be cleared, got %d rows", len(got))
	}
}
