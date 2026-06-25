// Package collector runs the periodic system-metrics sampling loop, persisting
// each sample to storage and pruning data past the retention window.
package collector

import (
	"context"
	"log"
	"time"

	"github.com/timanthonyalexander/heartd/internal/alert"
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
	engine    *alert.Engine // optional; nil when alerting is disabled

	// Previous network counters, for deriving throughput rates between samples.
	prevNet   metrics.NetCounters
	prevNetAt time.Time
}

// New builds a Collector for the local node. engine may be nil.
func New(db *storage.DB, node string, interval, retention time.Duration, engine *alert.Engine) *Collector {
	return &Collector{db: db, node: node, interval: interval, retention: retention, engine: engine}
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

	diskMax := c.sampleDisks(ctx, at)
	c.sampleNet(ctx, at)

	if c.engine != nil {
		c.engine.ObserveMetric(c.node, snap.CPUPercent, snap.MemPercent, diskMax)
	}
}

// sampleDisks records current usage per mount and returns the highest usage
// percentage across mounts (for threshold alerting).
func (c *Collector) sampleDisks(ctx context.Context, at time.Time) float64 {
	disks, err := metrics.Disks(ctx)
	if err != nil {
		log.Printf("collector: disk sample failed: %v", err)
		return 0
	}
	var max float64
	mounts := make([]string, 0, len(disks))
	for _, d := range disks {
		mounts = append(mounts, d.Mount)
		if d.Percent > max {
			max = d.Percent
		}
		if err := c.db.UpsertDiskStatus(storage.DiskStatus{
			Node:    c.node,
			Mount:   d.Mount,
			Used:    d.Used,
			Total:   d.Total,
			Percent: d.Percent,
			At:      at,
		}); err != nil {
			log.Printf("collector: disk persist failed: %v", err)
		}
	}
	// Drop any mounts that are no longer present (or now filtered out).
	if err := c.db.DeleteDiskStatusesExcept(c.node, mounts); err != nil {
		log.Printf("collector: disk cleanup failed: %v", err)
	}
	return max
}

// sampleNet reads cumulative network counters and stores a sample with the rate
// derived from the previous reading.
func (c *Collector) sampleNet(ctx context.Context, at time.Time) {
	nc, err := metrics.ReadNetCounters(ctx)
	if err != nil {
		log.Printf("collector: net sample failed: %v", err)
		return
	}

	var recvRate, sentRate float64
	if !c.prevNetAt.IsZero() {
		secs := at.Sub(c.prevNetAt).Seconds()
		if secs > 0 {
			// Guard against counter resets (e.g. reboot) yielding negatives.
			if nc.RecvBytes >= c.prevNet.RecvBytes {
				recvRate = float64(nc.RecvBytes-c.prevNet.RecvBytes) / secs
			}
			if nc.SentBytes >= c.prevNet.SentBytes {
				sentRate = float64(nc.SentBytes-c.prevNet.SentBytes) / secs
			}
		}
	}
	c.prevNet = nc
	c.prevNetAt = at

	if err := c.db.InsertNetSample(storage.NetSample{
		Node:      c.node,
		RecvBytes: nc.RecvBytes,
		SentBytes: nc.SentBytes,
		RecvRate:  recvRate,
		SentRate:  sentRate,
		At:        at,
	}); err != nil {
		log.Printf("collector: net persist failed: %v", err)
	}
}

// prune removes samples older than the retention window.
func (c *Collector) prune() {
	before := time.Now().UTC().Add(-c.retention)
	if _, err := c.db.Prune(before); err != nil {
		log.Printf("collector: prune failed: %v", err)
	}
	if _, err := c.db.PruneNet(before); err != nil {
		log.Printf("collector: prune net failed: %v", err)
	}
}
