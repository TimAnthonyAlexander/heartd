// Package metrics reads live system resource usage via gopsutil.
package metrics

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// Snapshot is a point-in-time reading of core system metrics.
type Snapshot struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemUsed     uint64  `json:"mem_used"`
	MemTotal    uint64  `json:"mem_total"`
	MemPercent  float64 `json:"mem_percent"`
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

	return Snapshot{
		CPUPercent:  round2(cpuPercent),
		MemUsed:     vm.Used,
		MemTotal:    vm.Total,
		MemPercent:  round2(vm.UsedPercent),
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
