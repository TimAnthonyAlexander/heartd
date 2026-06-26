// Package metrics reads live system resource usage via gopsutil.
package metrics

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

// Snapshot is a point-in-time reading of core system metrics.
type Snapshot struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsed    uint64  `json:"mem_used"`
	MemTotal   uint64  `json:"mem_total"`
	MemPercent float64 `json:"mem_percent"`
	// Load averages over 1/5/15 minutes. Zero on platforms where load isn't
	// available (e.g. Windows), where reading the average returns an error.
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
	// Swap usage. SwapTotal is 0 on systems without swap configured.
	SwapUsed    uint64  `json:"swap_used"`
	SwapTotal   uint64  `json:"swap_total"`
	SwapPercent float64 `json:"swap_percent"`
	CollectedAt string  `json:"collected_at"`
}

// Collect samples current CPU and memory usage. The CPU read blocks for the
// given sample window to produce a usage percentage over that interval.
func Collect(ctx context.Context, cpuWindow time.Duration) (Snapshot, error) {
	cpuPercents, err := cpu.PercentWithContext(ctx, cpuWindow, false)
	if err != nil {
		return Snapshot{}, err
	}

	var cpuPercent float64
	if len(cpuPercents) > 0 {
		cpuPercent = cpuPercents[0]
	}

	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	snap := Snapshot{
		CPUPercent:  round2(cpuPercent),
		MemUsed:     vm.Used,
		MemTotal:    vm.Total,
		MemPercent:  round2(vm.UsedPercent),
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Load average is unavailable on some platforms; treat as zero rather than
	// failing the whole sample.
	if avg, err := load.AvgWithContext(ctx); err == nil {
		snap.Load1 = round2(avg.Load1)
		snap.Load5 = round2(avg.Load5)
		snap.Load15 = round2(avg.Load15)
	}

	// Swap is optional; a box with no swap (or where the read fails) reports zero.
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		snap.SwapUsed = sw.Used
		snap.SwapTotal = sw.Total
		snap.SwapPercent = round2(sw.UsedPercent)
	}

	return snap, nil
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
