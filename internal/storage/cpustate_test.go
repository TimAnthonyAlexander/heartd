package storage

import (
	"testing"
	"time"
)

func cpuStateAt(node string, user, system, idle float64, at time.Time) CPUStateSample {
	return CPUStateSample{
		Node:   node,
		User:   user,
		System: system,
		Nice:   0,
		Iowait: 0,
		Irq:    0,
		Steal:  0,
		Idle:   idle,
		At:     at,
	}
}

func TestInsertAndLatestCPUState(t *testing.T) {
	db := openTestDB(t)

	if _, ok, err := db.LatestCPUState("a"); err != nil || ok {
		t.Fatalf("LatestCPUState on empty: ok=%v err=%v, want false/nil", ok, err)
	}

	base := time.Unix(1_700_000_000, 0).UTC()
	in := cpuStateAt("a", 20.0, 5.0, 75.0, base)
	if err := db.InsertCPUState(in); err != nil {
		t.Fatalf("InsertCPUState: %v", err)
	}
	// A second, later sample should be the one Latest returns.
	in2 := cpuStateAt("a", 40.0, 10.0, 50.0, base.Add(time.Minute))
	if err := db.InsertCPUState(in2); err != nil {
		t.Fatalf("InsertCPUState second: %v", err)
	}

	got, ok, err := db.LatestCPUState("a")
	if err != nil || !ok {
		t.Fatalf("LatestCPUState: ok=%v err=%v", ok, err)
	}
	if got.User != in2.User || got.System != in2.System || got.Idle != in2.Idle {
		t.Fatalf("LatestCPUState returned wrong sample: %+v", got)
	}
	if !got.At.Equal(in2.At) {
		t.Fatalf("At = %v, want %v", got.At, in2.At)
	}
	if got.At.Location() != time.UTC {
		t.Fatalf("At not UTC: %v", got.At.Location())
	}
}

func TestCPUStateHistoryOrdering(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	// Insert out of chronological order to verify the query re-orders ascending.
	times := []time.Time{base.Add(2 * time.Minute), base, base.Add(time.Minute)}
	for i, at := range times {
		if err := db.InsertCPUState(cpuStateAt("a", float64(10*(i+1)), 1.0, 50.0, at)); err != nil {
			t.Fatalf("InsertCPUState: %v", err)
		}
	}
	// A sample for another node must not leak into node "a"'s history.
	if err := db.InsertCPUState(cpuStateAt("b", 99.0, 1.0, 0.0, base)); err != nil {
		t.Fatalf("InsertCPUState other node: %v", err)
	}

	// RecentCPUStates returns oldest-first within the window.
	recent, err := db.RecentCPUStates("a", base.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("RecentCPUStates: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("RecentCPUStates: got %d rows, want 3", len(recent))
	}
	for i := 1; i < len(recent); i++ {
		if recent[i].At.Before(recent[i-1].At) {
			t.Fatalf("RecentCPUStates not ascending at %d: %v before %v", i, recent[i].At, recent[i-1].At)
		}
	}

	// CPUStateWindow over the full span returns the same node's samples ascending.
	win, err := db.CPUStateWindow("a", base.Add(-time.Hour), base.Add(time.Hour), 500)
	if err != nil {
		t.Fatalf("CPUStateWindow: %v", err)
	}
	if len(win) != 3 {
		t.Fatalf("CPUStateWindow: got %d rows, want 3", len(win))
	}
	if !win[0].At.Equal(base) || !win[2].At.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("CPUStateWindow ordering: first=%v last=%v", win[0].At, win[2].At)
	}
}

func TestPruneCPUState(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	if err := db.InsertCPUState(cpuStateAt("a", 10, 1, 89, base)); err != nil {
		t.Fatalf("InsertCPUState old: %v", err)
	}
	if err := db.InsertCPUState(cpuStateAt("a", 20, 2, 78, base.Add(time.Hour))); err != nil {
		t.Fatalf("InsertCPUState new: %v", err)
	}

	n, err := db.PruneCPUState(base.Add(30 * time.Minute))
	if err != nil {
		t.Fatalf("PruneCPUState: %v", err)
	}
	if n != 1 {
		t.Fatalf("PruneCPUState deleted %d rows, want 1", n)
	}

	remaining, err := db.RecentCPUStates("a", base.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("RecentCPUStates: %v", err)
	}
	if len(remaining) != 1 || !remaining[0].At.Equal(base.Add(time.Hour)) {
		t.Fatalf("after prune: got %d rows %+v", len(remaining), remaining)
	}
}
