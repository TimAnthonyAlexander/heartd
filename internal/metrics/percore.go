package metrics

import (
	"context"

	"github.com/shirou/gopsutil/v4/cpu"
)

// CoreTime holds one logical core's cumulative CPU time (in seconds since boot).
// Busy is all non-idle time; Total is every component the kernel reports. Per-core
// busy percentages are derived by diffing successive readings: a core's share is
// delta(Busy) / delta(Total) * 100, mirroring how the machine-wide CPU-state
// breakdown is derived in cpustate.go (no second blocking sample window).
type CoreTime struct {
	Busy  float64 // cumulative non-idle seconds (Total - Idle)
	Total float64 // cumulative seconds across all states
}

// ReadPerCoreTimes returns the cumulative CPU times for every logical core, in
// core order — the slice index IS the core number (gopsutil returns cores in
// order). Each core's Total sums all raw components the kernel reports; Busy is
// Total minus Idle. The collector diffs successive readings to derive each core's
// instantaneous busy percentage without a blocking sample window.
func ReadPerCoreTimes(ctx context.Context) ([]CoreTime, error) {
	stats, err := cpu.TimesWithContext(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]CoreTime, 0, len(stats))
	for _, s := range stats {
		total := s.User + s.System + s.Idle + s.Nice + s.Iowait + s.Irq + s.Softirq + s.Steal + s.Guest + s.GuestNice
		out = append(out, CoreTime{
			Busy:  total - s.Idle,
			Total: total,
		})
	}
	return out, nil
}
