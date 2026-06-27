package storage

import (
	"testing"
	"time"
)

func diskUsageAt(node, mount string, used, total uint64, at time.Time) DiskUsageSample {
	return DiskUsageSample{
		Node:    node,
		Mount:   mount,
		Used:    used,
		Total:   total,
		Percent: float64(used) / float64(total) * 100,
		At:      at,
	}
}

func TestInsertAndDiskUsageWindowOrdering(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	// Insert out of chronological order to verify the query re-orders ascending.
	times := []time.Time{base.Add(2 * time.Minute), base, base.Add(time.Minute)}
	for i, at := range times {
		if err := db.InsertDiskUsageSample(diskUsageAt("a", "/", uint64(10+i)<<30, 100<<30, at)); err != nil {
			t.Fatalf("InsertDiskUsageSample: %v", err)
		}
	}

	win, err := db.DiskUsageWindow("a", "/", base.Add(-time.Hour), base.Add(time.Hour), 500)
	if err != nil {
		t.Fatalf("DiskUsageWindow: %v", err)
	}
	if len(win) != 3 {
		t.Fatalf("DiskUsageWindow: got %d rows, want 3", len(win))
	}
	for i := 1; i < len(win); i++ {
		if win[i].At.Before(win[i-1].At) {
			t.Fatalf("DiskUsageWindow not ascending at %d: %v before %v", i, win[i].At, win[i-1].At)
		}
	}
	if !win[0].At.Equal(base) || !win[2].At.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("DiskUsageWindow ordering: first=%v last=%v", win[0].At, win[2].At)
	}
	if win[0].At.Location() != time.UTC {
		t.Fatalf("At not UTC: %v", win[0].At.Location())
	}
}

func TestRecentDiskUsage(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i < 4; i++ {
		if err := db.InsertDiskUsageSample(diskUsageAt("a", "/", uint64(50+i)<<30, 100<<30, base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("InsertDiskUsageSample: %v", err)
		}
	}

	// since after the second sample drops the first two.
	pts, err := db.RecentDiskUsage("a", "/", base.Add(90*time.Second))
	if err != nil {
		t.Fatalf("RecentDiskUsage: %v", err)
	}
	if len(pts) != 2 {
		t.Fatalf("RecentDiskUsage: got %d rows, want 2", len(pts))
	}
	if !pts[0].At.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("RecentDiskUsage first = %v, want %v", pts[0].At, base.Add(2*time.Minute))
	}
	if pts[0].Used != 52<<30 {
		t.Fatalf("RecentDiskUsage first used = %d, want %d", pts[0].Used, uint64(52)<<30)
	}
}

func TestDiskUsageCrossNodeMountIsolation(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	must := func(s DiskUsageSample) {
		if err := db.InsertDiskUsageSample(s); err != nil {
			t.Fatalf("InsertDiskUsageSample: %v", err)
		}
	}
	must(diskUsageAt("a", "/", 10<<30, 100<<30, base))
	must(diskUsageAt("a", "/data", 20<<30, 100<<30, base))
	must(diskUsageAt("b", "/", 30<<30, 100<<30, base))

	// node "a" mount "/" sees only its own row, not /data or node b.
	pts, err := db.RecentDiskUsage("a", "/", base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("RecentDiskUsage: %v", err)
	}
	if len(pts) != 1 || pts[0].Used != 10<<30 {
		t.Fatalf("isolation: got %+v, want single 10 GiB row", pts)
	}

	// Window for node b mount "/" sees only b's row.
	win, err := db.DiskUsageWindow("b", "/", base.Add(-time.Hour), base.Add(time.Hour), 500)
	if err != nil {
		t.Fatalf("DiskUsageWindow: %v", err)
	}
	if len(win) != 1 || win[0].Used != 30<<30 {
		t.Fatalf("isolation: got %+v, want single 30 GiB row", win)
	}
}

func TestPruneDiskUsage(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	if err := db.InsertDiskUsageSample(diskUsageAt("a", "/", 10<<30, 100<<30, base)); err != nil {
		t.Fatalf("InsertDiskUsageSample old: %v", err)
	}
	if err := db.InsertDiskUsageSample(diskUsageAt("a", "/", 20<<30, 100<<30, base.Add(time.Hour))); err != nil {
		t.Fatalf("InsertDiskUsageSample new: %v", err)
	}

	n, err := db.PruneDiskUsage(base.Add(30 * time.Minute))
	if err != nil {
		t.Fatalf("PruneDiskUsage: %v", err)
	}
	if n != 1 {
		t.Fatalf("PruneDiskUsage deleted %d rows, want 1", n)
	}
	remaining, err := db.RecentDiskUsage("a", "/", base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("RecentDiskUsage: %v", err)
	}
	if len(remaining) != 1 || !remaining[0].At.Equal(base.Add(time.Hour)) {
		t.Fatalf("after prune: got %d rows %+v", len(remaining), remaining)
	}
}
