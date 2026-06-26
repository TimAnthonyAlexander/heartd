package collector

import (
	"context"
	"testing"
	"time"

	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// testSettings builds a settings service seeded with the given retention.
func testSettings(t *testing.T, db *storage.DB, retention time.Duration) *settings.Service {
	t.Helper()
	set := settings.New(db)
	if err := set.Load(config.Default()); err != nil {
		t.Fatalf("settings load: %v", err)
	}
	g := set.General()
	g.RetentionSec = int64(retention.Seconds())
	g.MetricsIntervalSec = int64(time.Hour.Seconds())
	if err := set.SetGeneral(g); err != nil {
		t.Fatalf("set general: %v", err)
	}
	return set
}

func TestCollectorSamplesAndPersists(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	c := New(db, "test-node", testSettings(t, db, time.Hour))

	// One immediate sample, then stop before the first tick fires.
	c.sampleOnce(context.Background())

	got, ok, err := db.LatestMetric("test-node")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if !ok {
		t.Fatal("expected a persisted sample, got none")
	}
	if got.Node != "test-node" {
		t.Errorf("node = %q, want test-node", got.Node)
	}
	if got.MemTotal == 0 {
		t.Error("mem_total should be non-zero on a real machine")
	}
}

func TestCollectorPrunesOldSamples(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Insert one old sample well outside a 1h retention window.
	old := storage.MetricSample{
		Node: "test-node", CPUPercent: 1, MemUsed: 1, MemTotal: 2, MemPercent: 50,
		At: time.Now().UTC().Add(-2 * time.Hour),
	}
	if err := db.InsertMetric(old); err != nil {
		t.Fatalf("insert: %v", err)
	}

	c := New(db, "test-node", testSettings(t, db, time.Hour))
	c.prune()

	if _, ok, err := db.LatestMetric("test-node"); err != nil {
		t.Fatalf("latest: %v", err)
	} else if ok {
		t.Error("expected old sample to be pruned")
	}
}

func TestCollectorRunStopsOnContextCancel(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	c := New(db, "test-node", testSettings(t, db, time.Hour))

	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}
