// Package collector runs the periodic system-metrics sampling loop, persisting
// each sample to storage and pruning data past the retention window.
package collector

import (
	"context"
	"log"
	"time"

	"github.com/timanthonyalexander/heartd/internal/metrics"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// cpuWindow is how long each CPU sample blocks to compute a usage percentage.
const cpuWindow = 500 * time.Millisecond

// Collector samples metrics for the local node on a fixed interval and writes
// them to the database, pruning anything older than the retention window.
type Collector struct {
	db        *storage.DB
	node      string
	interval  time.Duration
	retention time.Duration
}

// New builds a Collector for the local node.
func New(db *storage.DB, node string, interval, retention time.Duration) *Collector {
	return &Collector{db: db, node: node, interval: interval, retention: retention}
}

// Run samples immediately, then once per interval until ctx is cancelled.
// It blocks, so callers typically run it in a goroutine.
func (c *Collector) Run(ctx context.Context) {
	c.sampleOnce(ctx)
	c.prune()

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sampleOnce(ctx)
			c.prune()
		}
	}
}

// sampleOnce reads one metrics snapshot and persists it. Errors are logged but
// never fatal — a single failed sample must not kill the loop.
func (c *Collector) sampleOnce(ctx context.Context) {
	snap, err := metrics.Collect(ctx, cpuWindow)
	if err != nil {
		log.Printf("collector: sample failed: %v", err)
		return
	}

	at, err := time.Parse(time.RFC3339, snap.CollectedAt)
	if err != nil {
		at = time.Now().UTC()
	}

	sample := storage.MetricSample{
		Node:       c.node,
		CPUPercent: snap.CPUPercent,
		MemUsed:    snap.MemUsed,
		MemTotal:   snap.MemTotal,
		MemPercent: snap.MemPercent,
		At:         at,
	}
	if err := c.db.InsertMetric(sample); err != nil {
		log.Printf("collector: insert failed: %v", err)
	}
}

// prune removes samples older than the retention window.
func (c *Collector) prune() {
	before := time.Now().UTC().Add(-c.retention)
	if _, err := c.db.Prune(before); err != nil {
		log.Printf("collector: prune failed: %v", err)
	}
}
