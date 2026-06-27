package storage

import (
	"testing"
	"time"
)

func TestReplaceProcessTopReplacesNodeSet(t *testing.T) {
	db := openTestDB(t)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Seed two nodes so we can confirm a replace only touches its own node.
	if err := db.ReplaceProcessTop("node-a", []ProcessSample{
		{Node: "node-a", PID: 1, Name: "init", Command: "/sbin/init", CPUPercent: 5, MemPercent: 1, MemRSS: 1024, At: at},
		{Node: "node-a", PID: 2, Name: "old", Command: "old", CPUPercent: 3, MemPercent: 2, MemRSS: 2048, At: at},
	}); err != nil {
		t.Fatalf("seed node-a: %v", err)
	}
	if err := db.ReplaceProcessTop("node-b", []ProcessSample{
		{Node: "node-b", PID: 9, Name: "other", Command: "other", CPUPercent: 1, MemPercent: 1, MemRSS: 512, At: at},
	}); err != nil {
		t.Fatalf("seed node-b: %v", err)
	}

	// Replace node-a with a single, different row.
	if err := db.ReplaceProcessTop("node-a", []ProcessSample{
		{Node: "node-a", PID: 42, Name: "fresh", Command: "/usr/bin/fresh", CPUPercent: 9, MemPercent: 4, MemRSS: 4096, At: at},
	}); err != nil {
		t.Fatalf("replace node-a: %v", err)
	}

	got, err := db.TopProcesses("node-a")
	if err != nil {
		t.Fatalf("TopProcesses node-a: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("node-a: want 1 row after replace, got %d", len(got))
	}
	if got[0].PID != 42 || got[0].Name != "fresh" || got[0].MemRSS != 4096 {
		t.Fatalf("node-a: unexpected row %+v", got[0])
	}

	// node-b must be untouched by node-a's replace.
	other, err := db.TopProcesses("node-b")
	if err != nil {
		t.Fatalf("TopProcesses node-b: %v", err)
	}
	if len(other) != 1 || other[0].PID != 9 {
		t.Fatalf("node-b: expected its own untouched row, got %+v", other)
	}
}

func TestTopProcessesOrdering(t *testing.T) {
	db := openTestDB(t)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := db.ReplaceProcessTop("node", []ProcessSample{
		{Node: "node", PID: 1, Name: "low", CPUPercent: 1, MemPercent: 9, At: at},
		{Node: "node", PID: 2, Name: "high", CPUPercent: 50, MemPercent: 1, At: at},
		// Same CPU as PID 2 but lower memory: the tie-break puts it second.
		{Node: "node", PID: 3, Name: "tie", CPUPercent: 50, MemPercent: 0.5, At: at},
		{Node: "node", PID: 4, Name: "mid", CPUPercent: 10, MemPercent: 1, At: at},
	}); err != nil {
		t.Fatalf("ReplaceProcessTop: %v", err)
	}

	got, err := db.TopProcesses("node")
	if err != nil {
		t.Fatalf("TopProcesses: %v", err)
	}
	wantPIDs := []int32{2, 3, 4, 1} // CPU desc, then mem desc
	if len(got) != len(wantPIDs) {
		t.Fatalf("want %d rows, got %d", len(wantPIDs), len(got))
	}
	for i, want := range wantPIDs {
		if got[i].PID != want {
			t.Fatalf("position %d: want pid %d, got %d (%+v)", i, want, got[i].PID, got[i])
		}
	}
}

func TestReplaceProcessTopEmptyClearsNode(t *testing.T) {
	db := openTestDB(t)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := db.ReplaceProcessTop("node", []ProcessSample{
		{Node: "node", PID: 1, Name: "p", CPUPercent: 1, At: at},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.ReplaceProcessTop("node", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, err := db.TopProcesses("node")
	if err != nil {
		t.Fatalf("TopProcesses: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty after clearing, got %d rows", len(got))
	}
}
