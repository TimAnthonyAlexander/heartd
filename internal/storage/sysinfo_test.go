package storage

import (
	"testing"
	"time"
)

func diskAt(node, mount string, used, total uint64, percent float64, at time.Time) DiskStatus {
	return DiskStatus{
		Node:    node,
		Mount:   mount,
		Used:    used,
		Total:   total,
		Percent: percent,
		At:      at,
	}
}

func TestUpsertAndDiskStatuses(t *testing.T) {
	db := openTestDB(t)

	if got, err := db.DiskStatuses("a"); err != nil || len(got) != 0 {
		t.Fatalf("DiskStatuses on empty: got %d rows err=%v, want 0/nil", len(got), err)
	}

	base := time.Unix(1_700_000_000, 0).UTC()
	in := diskAt("a", "/", 1024, 4096, 25.0, base)
	if err := db.UpsertDiskStatus(in); err != nil {
		t.Fatalf("UpsertDiskStatus: %v", err)
	}

	got, err := db.DiskStatuses("a")
	if err != nil {
		t.Fatalf("DiskStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	g := got[0]
	if g.Node != in.Node || g.Mount != in.Mount || g.Used != in.Used ||
		g.Total != in.Total || g.Percent != in.Percent {
		t.Fatalf("DiskStatuses returned wrong status: %+v", g)
	}
	if !g.At.Equal(in.At) {
		t.Fatalf("At = %v, want %v", g.At, in.At)
	}
	if g.At.Location() != time.UTC {
		t.Fatalf("At not UTC: %v", g.At.Location())
	}
}

func TestUpsertDiskStatusUpdatesInPlace(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	if err := db.UpsertDiskStatus(diskAt("a", "/", 1024, 4096, 25.0, base)); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	updated := diskAt("a", "/", 3072, 4096, 75.0, base.Add(time.Minute))
	if err := db.UpsertDiskStatus(updated); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := db.DiskStatuses("a")
	if err != nil {
		t.Fatalf("DiskStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row after re-upsert, got %d (upsert created a duplicate)", len(got))
	}
	g := got[0]
	if g.Used != 3072 || g.Total != 4096 || g.Percent != 75.0 || !g.At.Equal(updated.At) {
		t.Fatalf("upsert did not update values: %+v", g)
	}
}

func TestDiskStatusesMultiNodeIsolation(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	if err := db.UpsertDiskStatus(diskAt("a", "/", 1, 10, 10.0, base)); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := db.UpsertDiskStatus(diskAt("b", "/", 9, 10, 90.0, base)); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	aRows, err := db.DiskStatuses("a")
	if err != nil {
		t.Fatalf("DiskStatuses a: %v", err)
	}
	if len(aRows) != 1 || aRows[0].Node != "a" || aRows[0].Percent != 10.0 {
		t.Fatalf("node a isolation failed: %+v", aRows)
	}

	bRows, err := db.DiskStatuses("b")
	if err != nil {
		t.Fatalf("DiskStatuses b: %v", err)
	}
	if len(bRows) != 1 || bRows[0].Node != "b" || bRows[0].Percent != 90.0 {
		t.Fatalf("node b isolation failed: %+v", bRows)
	}
}

func TestDiskStatusesOrdering(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	// Insert out of alphabetical order.
	for _, mount := range []string{"/var", "/", "/home", "/boot"} {
		if err := db.UpsertDiskStatus(diskAt("a", mount, 1, 10, 10.0, base)); err != nil {
			t.Fatalf("upsert %q: %v", mount, err)
		}
	}

	got, err := db.DiskStatuses("a")
	if err != nil {
		t.Fatalf("DiskStatuses: %v", err)
	}
	want := []string{"/", "/boot", "/home", "/var"}
	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d", len(want), len(got))
	}
	for i, mount := range want {
		if got[i].Mount != mount {
			t.Fatalf("got[%d].Mount = %q, want %q (not ordered by mount)", i, got[i].Mount, mount)
		}
	}
}

func TestDiskStatusTimestampRoundTrip(t *testing.T) {
	db := openTestDB(t)

	at := time.Now()
	if err := db.UpsertDiskStatus(diskAt("a", "/", 1, 10, 10.0, at)); err != nil {
		t.Fatalf("UpsertDiskStatus: %v", err)
	}

	got, err := db.DiskStatuses("a")
	if err != nil {
		t.Fatalf("DiskStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if diff := got[0].At.Sub(at.UTC()); diff > time.Second || diff < -time.Second {
		t.Fatalf("At round-trip diff = %v, want within 1s", diff)
	}
}

func netAt(node string, recv, sent uint64, recvRate, sentRate float64, at time.Time) NetSample {
	return NetSample{
		Node:      node,
		RecvBytes: recv,
		SentBytes: sent,
		RecvRate:  recvRate,
		SentRate:  sentRate,
		At:        at,
	}
}

func TestInsertAndLatestNetSample(t *testing.T) {
	db := openTestDB(t)

	if _, ok, err := db.LatestNetSample("a"); err != nil || ok {
		t.Fatalf("LatestNetSample on empty: got ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	base := time.Unix(1_700_000_000, 0).UTC()
	older := netAt("a", 100, 50, 1.0, 0.5, base)
	newer := netAt("a", 900, 450, 9.0, 4.5, base.Add(time.Minute))

	if err := db.InsertNetSample(older); err != nil {
		t.Fatalf("insert older: %v", err)
	}
	if err := db.InsertNetSample(newer); err != nil {
		t.Fatalf("insert newer: %v", err)
	}

	got, ok, err := db.LatestNetSample("a")
	if err != nil || !ok {
		t.Fatalf("LatestNetSample: ok=%v err=%v", ok, err)
	}
	if got.RecvBytes != newer.RecvBytes || got.SentBytes != newer.SentBytes ||
		got.RecvRate != newer.RecvRate || got.SentRate != newer.SentRate {
		t.Fatalf("LatestNetSample returned wrong sample: %+v", got)
	}
	if !got.At.Equal(newer.At) {
		t.Fatalf("LatestNetSample At = %v, want %v", got.At, newer.At)
	}
	if got.At.Location() != time.UTC {
		t.Fatalf("At not UTC: %v", got.At.Location())
	}
}

func TestRecentNetSamplesOrderingWindowAndLimit(t *testing.T) {
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
		if err := db.InsertNetSample(netAt("a", 1, 1, 1.0, 1.0, ts)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// All, no limit, since base: expect oldest-first 0,1,2,3,4.
	all, err := db.RecentNetSamples("a", base, 0)
	if err != nil {
		t.Fatalf("RecentNetSamples all: %v", err)
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
	windowed, err := db.RecentNetSamples("a", base.Add(3*time.Minute), 0)
	if err != nil {
		t.Fatalf("RecentNetSamples windowed: %v", err)
	}
	if len(windowed) != 2 {
		t.Fatalf("windowed expected 2 rows, got %d", len(windowed))
	}
	if !windowed[0].At.Equal(base.Add(3*time.Minute)) || !windowed[1].At.Equal(base.Add(4*time.Minute)) {
		t.Fatalf("windowed wrong contents: %v, %v", windowed[0].At, windowed[1].At)
	}

	// limit: most recent 2 within full window, still oldest-first (minutes 3,4).
	limited, err := db.RecentNetSamples("a", base, 2)
	if err != nil {
		t.Fatalf("RecentNetSamples limited: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("limited expected 2 rows, got %d", len(limited))
	}
	if !limited[0].At.Equal(base.Add(3*time.Minute)) || !limited[1].At.Equal(base.Add(4*time.Minute)) {
		t.Fatalf("limited should be most-recent-2 oldest-first: %v, %v", limited[0].At, limited[1].At)
	}
}

func TestNetSampleMultiNodeIsolation(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	if err := db.InsertNetSample(netAt("a", 1, 1, 1.0, 1.0, base)); err != nil {
		t.Fatalf("insert a: %v", err)
	}
	if err := db.InsertNetSample(netAt("b", 2, 2, 2.0, 2.0, base.Add(time.Minute))); err != nil {
		t.Fatalf("insert b: %v", err)
	}

	aRows, err := db.RecentNetSamples("a", base.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("RecentNetSamples a: %v", err)
	}
	if len(aRows) != 1 || aRows[0].Node != "a" {
		t.Fatalf("node a isolation failed: %+v", aRows)
	}

	latestB, ok, err := db.LatestNetSample("b")
	if err != nil || !ok {
		t.Fatalf("LatestNetSample b: ok=%v err=%v", ok, err)
	}
	if latestB.Node != "b" {
		t.Fatalf("LatestNetSample b returned wrong node: %s", latestB.Node)
	}

	if _, ok, err := db.LatestNetSample("missing"); err != nil || ok {
		t.Fatalf("LatestNetSample missing: ok=%v err=%v", ok, err)
	}
}

func TestPruneNet(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i < 5; i++ {
		if err := db.InsertNetSample(netAt("a", 1, 1, 1.0, 1.0, base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Delete samples before minute 3 -> removes minutes 0,1,2 (3 rows).
	deleted, err := db.PruneNet(base.Add(3 * time.Minute))
	if err != nil {
		t.Fatalf("PruneNet: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("PruneNet deleted = %d, want 3", deleted)
	}

	remaining, err := db.RecentNetSamples("a", base, 0)
	if err != nil {
		t.Fatalf("RecentNetSamples after prune: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("remaining = %d, want 2", len(remaining))
	}
	if !remaining[0].At.Equal(base.Add(3 * time.Minute)) {
		t.Fatalf("oldest remaining = %v, want %v", remaining[0].At, base.Add(3*time.Minute))
	}

	// Pruning again at same boundary deletes nothing.
	deleted, err = db.PruneNet(base.Add(3 * time.Minute))
	if err != nil {
		t.Fatalf("PruneNet second: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("second PruneNet deleted = %d, want 0", deleted)
	}
}
