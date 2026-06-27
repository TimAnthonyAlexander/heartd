package metrics

import (
	"context"

	"github.com/shirou/gopsutil/v4/cpu"
)

// CPUTimes holds cumulative CPU time (in seconds since boot) broken down by
// state. Percentages are derived by diffing successive readings: each surfaced
// state's share is delta(state) / delta(Total) * 100. Irq folds in Softirq so
// the surfaced set stays small. Total is the sum of ALL components gopsutil
// reports — including ones we don't surface individually (e.g. Guest) — so the
// surfaced states plus Idle sum to ~Total and the percentages are correct.
type CPUTimes struct {
	User   float64
	System float64
	Nice   float64
	Iowait float64
	Irq    float64
	Steal  float64
	Idle   float64
	Total  float64
}

// ReadCPUTimes returns the machine-wide cumulative CPU times. It reads the
// aggregate (per-CPU=false) TimesStat and maps its fields onto CPUTimes,
// combining Irq+Softirq. Total sums every raw component the kernel reports so
// percentages stay correct even where fields we don't surface are non-zero;
// Guest/GuestNice are NOT added into User (the kernel already counts guest time
// within user), they only contribute to Total.
func ReadCPUTimes(ctx context.Context) (CPUTimes, error) {
	stats, err := cpu.TimesWithContext(ctx, false)
	if err != nil {
		return CPUTimes{}, err
	}
	if len(stats) == 0 {
		return CPUTimes{}, nil
	}
	s := stats[0]
	total := s.User + s.System + s.Idle + s.Nice + s.Iowait + s.Irq + s.Softirq + s.Steal + s.Guest + s.GuestNice
	return CPUTimes{
		User:   s.User,
		System: s.System,
		Nice:   s.Nice,
		Iowait: s.Iowait,
		Irq:    s.Irq + s.Softirq,
		Steal:  s.Steal,
		Idle:   s.Idle,
		Total:  total,
	}, nil
}
