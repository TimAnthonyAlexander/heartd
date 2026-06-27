// Package collector runs the periodic system-metrics sampling loop, persisting
// each sample to storage and pruning data past the retention window.
package collector

import (
	"context"
	"log"
	"runtime"
	"sort"
	"time"

	"github.com/timanthonyalexander/heartd/internal/metrics"
	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// topProcessCount is how many processes (by CPU share) are persisted each cycle.
const topProcessCount = 10

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

	// Previous per-process cumulative CPU time (pid -> seconds), for deriving each
	// process's instantaneous CPU share between samples.
	prevProcCPU map[int32]float64
	prevProcAt  time.Time
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
	c.sampleProcesses(ctx, at)
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

// sampleProcesses reads every process's cumulative CPU time and derives each
// one's instantaneous CPU share — its CPU-seconds gained since the previous
// sample, divided by elapsed wall-time and the core count, so the values are a
// share of the whole machine's capacity (and roughly sum toward the headline CPU
// percent). New pids and counter resets yield 0, mirroring sampleNet/sampleDiskIO.
// The top processes by CPU share are persisted, replacing the previous set. The
// first cycle (no previous reading) yields 0% for every process; it fills in on
// the next cycle, exactly like the rate-derived net/disk metrics.
func (c *Collector) sampleProcesses(ctx context.Context, at time.Time) {
	procs, err := metrics.ReadProcesses(ctx)
	if err != nil {
		log.Printf("collector: process sample failed: %v", err)
		return
	}

	var secs float64
	if !c.prevProcAt.IsZero() {
		secs = at.Sub(c.prevProcAt).Seconds()
	}
	cores := float64(runtime.NumCPU())

	curCPU := make(map[int32]float64, len(procs))
	samples := make([]storage.ProcessSample, 0, len(procs))
	for _, p := range procs {
		curCPU[p.PID] = p.CPUTime

		var cpuPct float64
		if prev, hadPrev := c.prevProcCPU[p.PID]; hadPrev && secs > 0 && cores > 0 {
			// Guard against a counter reset (pid reuse) yielding a negative spike.
			if delta := p.CPUTime - prev; delta > 0 {
				cpuPct = round2(delta / secs * 100 / cores)
			}
		}
		samples = append(samples, storage.ProcessSample{
			Node:       c.node,
			PID:        p.PID,
			Name:       p.Name,
			Command:    p.Command,
			CPUPercent: cpuPct,
			MemPercent: round2(p.MemPercent),
			MemRSS:     p.MemRSS,
			At:         at,
		})
	}

	// Top-N by CPU share; ties broken by memory share then pid for a stable order.
	sort.SliceStable(samples, func(i, j int) bool {
		a, b := samples[i], samples[j]
		if a.CPUPercent != b.CPUPercent {
			return a.CPUPercent > b.CPUPercent
		}
		if a.MemPercent != b.MemPercent {
			return a.MemPercent > b.MemPercent
		}
		return a.PID < b.PID
	})
	if len(samples) > topProcessCount {
		samples = samples[:topProcessCount]
	}

	if err := c.db.ReplaceProcessTop(c.node, samples); err != nil {
		log.Printf("collector: process persist failed: %v", err)
	}
	c.prevProcCPU = curCPU
	c.prevProcAt = at
}

// round2 rounds to two decimal places, matching the metrics package's rounding so
// derived percentages don't carry float noise into storage.
func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
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
