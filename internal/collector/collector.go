// Package collector runs the periodic system-metrics sampling loop, persisting
// each sample to storage and pruning data past the retention window.
package collector

import (
	"context"
	"log"
	"time"

	"github.com/timanthonyalexander/heartd/internal/metrics"
	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// cpuWindow is how long each CPU sample blocks to compute a usage percentage.
const cpuWindow = 500 * time.Millisecond

// fallbackInterval is used if the configured interval is somehow invalid.
const fallbackInterval = 30 * time.Second

// Collector samples metrics for the local node, writing them to the database and
// pruning anything older than the retention window. The interval and retention
// are read fresh from settings each cycle, so edits apply without a restart.
type Collector struct {
	db       *storage.DB
	node     string
	settings *settings.Service

	// Previous network counters, for deriving throughput rates between samples.
	prevNet   metrics.NetCounters
	prevNetAt time.Time

	// Previous per-device disk I/O counters, for deriving throughput/IOPS rates.
	prevDiskIO   map[string]metrics.DiskIOCounters
	prevDiskIOAt time.Time
}

// New builds a Collector for the local node.
func New(db *storage.DB, node string, set *settings.Service) *Collector {
	return &Collector{db: db, node: node, settings: set}
}

// Run samples immediately, then once per current interval until ctx is cancelled.
// It blocks, so callers typically run it in a goroutine.
func (c *Collector) Run(ctx context.Context) {
	for {
		c.sampleOnce(ctx)
		c.prune()

		interval := c.settings.General().MetricsInterval
		if interval <= 0 {
			interval = fallbackInterval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
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
		Node:        c.node,
		CPUPercent:  snap.CPUPercent,
		MemUsed:     snap.MemUsed,
		MemTotal:    snap.MemTotal,
		MemPercent:  snap.MemPercent,
		Load1:       snap.Load1,
		Load5:       snap.Load5,
		Load15:      snap.Load15,
		SwapUsed:    snap.SwapUsed,
		SwapTotal:   snap.SwapTotal,
		SwapPercent: snap.SwapPercent,
		At:          at,
	}
	if err := c.db.InsertMetric(sample); err != nil {
		log.Printf("collector: insert failed: %v", err)
	}

	c.sampleDisks(ctx, at)
	c.sampleNet(ctx, at)
	c.sampleDiskIO(ctx, at)
}

// sampleDisks records current usage per mount and returns the highest usage
// percentage across mounts.
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

// sampleDiskIO reads cumulative per-device disk counters and stores one sample
// per physical device, with throughput/IOPS rates derived from the previous
// reading. Counter resets (reboots/wraps) yield a zero rate rather than a
// negative spike, mirroring sampleNet.
func (c *Collector) sampleDiskIO(ctx context.Context, at time.Time) {
	counters, err := metrics.ReadDiskIOCounters(ctx)
	if err != nil {
		log.Printf("collector: disk io sample failed: %v", err)
		return
	}

	var secs float64
	if !c.prevDiskIOAt.IsZero() {
		secs = at.Sub(c.prevDiskIOAt).Seconds()
	}

	for device, cur := range counters {
		prev, hadPrev := c.prevDiskIO[device]
		var readBytes, writeBytes, readOps, writeOps uint64
		if hadPrev && secs > 0 {
			readBytes = perSecond(cur.ReadBytes, prev.ReadBytes, secs)
			writeBytes = perSecond(cur.WriteBytes, prev.WriteBytes, secs)
			readOps = perSecond(cur.ReadOps, prev.ReadOps, secs)
			writeOps = perSecond(cur.WriteOps, prev.WriteOps, secs)
		}
		if err := c.db.InsertDiskIOSample(storage.DiskIOSample{
			Node:           c.node,
			Device:         device,
			ReadBytesRate:  readBytes,
			WriteBytesRate: writeBytes,
			ReadOpsRate:    readOps,
			WriteOpsRate:   writeOps,
			At:             at,
		}); err != nil {
			log.Printf("collector: disk io persist failed: %v", err)
		}
	}
	c.prevDiskIO = counters
	c.prevDiskIOAt = at
}

// perSecond returns the per-second rate between two cumulative counter values.
// A current value below the previous one indicates a counter reset (reboot or
// wrap) and yields 0 rather than a spurious spike.
func perSecond(cur, prev uint64, secs float64) uint64 {
	if cur < prev || secs <= 0 {
		return 0
	}
	return uint64(float64(cur-prev)/secs + 0.5)
}

// prune removes samples older than the current retention window.
func (c *Collector) prune() {
	retention := c.settings.General().Retention
	if retention <= 0 {
		return
	}
	before := time.Now().UTC().Add(-retention)
	if _, err := c.db.Prune(before); err != nil {
		log.Printf("collector: prune failed: %v", err)
	}
	if _, err := c.db.PruneNet(before); err != nil {
		log.Printf("collector: prune net failed: %v", err)
	}
	if _, err := c.db.PruneDiskIO(before); err != nil {
		log.Printf("collector: prune disk io failed: %v", err)
	}
	if _, err := c.db.PruneAlertEvents(before); err != nil {
		log.Printf("collector: prune alert events failed: %v", err)
	}
}
