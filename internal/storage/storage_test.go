package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "heartd_test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return db
}

func sampleAt(node string, at time.Time) MetricSample {
	return MetricSample{
		Node:       node,
		CPUPercent: 12.5,
		MemUsed:    1024,
		MemTotal:   4096,
		MemPercent: 25.0,
		At:         at,
	}
}

func TestMetricsWindowBoundsAndDownsampling(t *testing.T) {
	db := openTestDB(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Insert 100 samples one minute apart (minutes 0..99).
	for i := 0; i < 100; i++ {
		if err := db.InsertMetric(sampleAt("a", base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("InsertMetric %d: %v", i, err)
		}
	}

	// Window minutes 10..19 inclusive: both bounds must be respected.
	from := base.Add(10 * time.Minute)
	to := base.Add(19 * time.Minute)
	got, err := db.MetricsWindow("a", from, to, 500)
	if err != nil {
		t.Fatalf("MetricsWindow: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("window 10..19: got %d points, want 10", len(got))
	}
	if !got[0].At.Equal(from) || !got[len(got)-1].At.Equal(to) {
		t.Fatalf("window bounds: got [%v..%v], want [%v..%v]", got[0].At, got[len(got)-1].At, from, to)
	}

	// Downsampling: full window (minutes 0..99, 100 raw samples) capped to 10
	// points must collapse to about 10 (at most maxPoints+1), oldest-first, all
	// within the window.
	ds, err := db.MetricsWindow("a", base, base.Add(99*time.Minute), 10)
	if err != nil {
		t.Fatalf("MetricsWindow downsample: %v", err)
	}
	if len(ds) == 0 || len(ds) > 11 {
		t.Fatalf("downsample: got %d points, want 1..11", len(ds))
	}
	for i := 1; i < len(ds); i++ {
		if ds[i].At.Before(ds[i-1].At) {
			t.Fatalf("downsample not oldest-first at %d", i)
		}
	}
}

func TestOpenSchemaIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idem.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer db2.Close()

	// Schema should still be usable after re-open.
	if err := db2.InsertMetric(sampleAt("n", time.Now())); err != nil {
		t.Fatalf("InsertMetric after re-open: %v", err)
	}
}

func TestInsertAndLatestMetric(t *testing.T) {
	db := openTestDB(t)

	if _, ok, err := db.LatestMetric("a"); err != nil || ok {
		t.Fatalf("LatestMetric on empty: got ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	base := time.Unix(1_700_000_000, 0).UTC()
	older := MetricSample{Node: "a", CPUPercent: 10, MemUsed: 100, MemTotal: 1000, MemPercent: 10, At: base}
	newer := MetricSample{Node: "a", CPUPercent: 90, MemUsed: 900, MemTotal: 1000, MemPercent: 90, At: base.Add(time.Minute)}

	if err := db.InsertMetric(older); err != nil {
		t.Fatalf("insert older: %v", err)
	}
	if err := db.InsertMetric(newer); err != nil {
		t.Fatalf("insert newer: %v", err)
	}

	got, ok, err := db.LatestMetric("a")
	if err != nil || !ok {
		t.Fatalf("LatestMetric: ok=%v err=%v", ok, err)
	}
	if got.CPUPercent != newer.CPUPercent || got.MemUsed != newer.MemUsed ||
		got.MemTotal != newer.MemTotal || got.MemPercent != newer.MemPercent {
		t.Fatalf("LatestMetric returned wrong sample: %+v", got)
	}
	if !got.At.Equal(newer.At) {
		t.Fatalf("LatestMetric At = %v, want %v", got.At, newer.At)
	}
	if got.At.Location() != time.UTC {
		t.Fatalf("At not UTC: %v", got.At.Location())
	}
}

func TestRecentMetricsOrderingWindowAndLimit(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	// Insert 5 samples at minutes 0..4, out of chronological insert order.
	times := []time.Time{
		base.Add(2 * time.Minute),
		base.Add(0 * time.Minute),
		base.Add(4 * time.Minute),
		base.Add(1 * time.Minute),
		base.Add(3 * time.Minute),
	}
	for _, ts := range times {
		if err := db.InsertMetric(sampleAt("a", ts)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// All, no limit, since base: expect oldest-first 0,1,2,3,4.
	all, err := db.RecentMetrics("a", base, 0)
	if err != nil {
		t.Fatalf("RecentMetrics all: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(all))
	}
	for i := range all {
		want := base.Add(time.Duration(i) * time.Minute)
		if !all[i].At.Equal(want) {
			t.Fatalf("all[%d].At = %v, want %v (not oldest-first)", i, all[i].At, want)
		}
	}

	// since filter: only samples >= minute 3 (minutes 3,4).
	windowed, err := db.RecentMetrics("a", base.Add(3*time.Minute), 0)
	if err != nil {
		t.Fatalf("RecentMetrics windowed: %v", err)
	}
	if len(windowed) != 2 {
		t.Fatalf("windowed expected 2 rows, got %d", len(windowed))
	}
	if !windowed[0].At.Equal(base.Add(3*time.Minute)) || !windowed[1].At.Equal(base.Add(4*time.Minute)) {
		t.Fatalf("windowed wrong contents: %v, %v", windowed[0].At, windowed[1].At)
	}

	// limit: most recent 2 within full window, still oldest-first (minutes 3,4).
	limited, err := db.RecentMetrics("a", base, 2)
	if err != nil {
		t.Fatalf("RecentMetrics limited: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("limited expected 2 rows, got %d", len(limited))
	}
	if !limited[0].At.Equal(base.Add(3*time.Minute)) || !limited[1].At.Equal(base.Add(4*time.Minute)) {
		t.Fatalf("limited should be most-recent-2 oldest-first: %v, %v", limited[0].At, limited[1].At)
	}
}

func TestMultiNodeIsolation(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	if err := db.InsertMetric(sampleAt("a", base)); err != nil {
		t.Fatalf("insert a: %v", err)
	}
	if err := db.InsertMetric(sampleAt("b", base.Add(time.Minute))); err != nil {
		t.Fatalf("insert b: %v", err)
	}

	aRows, err := db.RecentMetrics("a", base.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("RecentMetrics a: %v", err)
	}
	if len(aRows) != 1 || aRows[0].Node != "a" {
		t.Fatalf("node a isolation failed: %+v", aRows)
	}

	latestB, ok, err := db.LatestMetric("b")
	if err != nil || !ok {
		t.Fatalf("LatestMetric b: ok=%v err=%v", ok, err)
	}
	if latestB.Node != "b" {
		t.Fatalf("LatestMetric b returned wrong node: %s", latestB.Node)
	}

	if _, ok, err := db.LatestMetric("missing"); err != nil || ok {
		t.Fatalf("LatestMetric missing: ok=%v err=%v", ok, err)
	}
}

func TestPrune(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i < 5; i++ {
		if err := db.InsertMetric(sampleAt("a", base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Delete samples before minute 3 -> removes minutes 0,1,2 (3 rows).
	deleted, err := db.Prune(base.Add(3 * time.Minute))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("Prune deleted = %d, want 3", deleted)
	}

	remaining, err := db.RecentMetrics("a", base, 0)
	if err != nil {
		t.Fatalf("RecentMetrics after prune: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("remaining = %d, want 2", len(remaining))
	}
	if !remaining[0].At.Equal(base.Add(3*time.Minute)) {
		t.Fatalf("oldest remaining = %v, want %v", remaining[0].At, base.Add(3*time.Minute))
	}

	// Pruning again at same boundary deletes nothing.
	deleted, err = db.Prune(base.Add(3 * time.Minute))
	if err != nil {
		t.Fatalf("Prune second: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("second Prune deleted = %d, want 0", deleted)
	}
}
