package metrics

import (
	"context"

	"github.com/shirou/gopsutil/v4/process"
)

// maxCommandRunes bounds the stored command line so a pathological argv (e.g. a
// JVM with hundreds of flags) can't bloat a sample.
const maxCommandRunes = 200

// ProcInfo is a single process's identity and resource usage at one instant.
// CPUTime is cumulative (user+system) CPU seconds since the process started; the
// collector diffs it between samples to derive an instantaneous CPU percentage,
// mirroring how disk/net counters are turned into rates.
type ProcInfo struct {
	PID        int32
	Name       string
	Command    string
	CPUTime    float64 // cumulative user+system CPU seconds since process start
	MemRSS     uint64  // resident set size in bytes
	MemPercent float64 // share of physical memory, 0-100
}

// ReadProcesses returns every running process with its PID, cumulative CPU time,
// memory, name, and command line populated. It is deliberately resilient: a field
// that can't be read for a given process is left at its zero value rather than
// failing the whole call, and a process that yields no CPU time at all (e.g. it
// exited mid-scan) is skipped. Sorting and top-N selection are the caller's job.
func ReadProcesses(ctx context.Context) ([]ProcInfo, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]ProcInfo, 0, len(procs))
	for _, p := range procs {
		// CPU time is the one field we require: it anchors the rate derivation. A
		// process we can't read it for is skipped entirely.
		times, err := p.TimesWithContext(ctx)
		if err != nil {
			continue
		}
		info := ProcInfo{
			PID:     p.Pid,
			CPUTime: times.User + times.System,
		}
		if name, err := p.NameWithContext(ctx); err == nil {
			info.Name = name
		}
		if cmd, err := p.CmdlineWithContext(ctx); err == nil {
			info.Command = truncateRunes(cmd, maxCommandRunes)
		}
		if mem, err := p.MemoryInfoWithContext(ctx); err == nil && mem != nil {
			info.MemRSS = mem.RSS
		}
		if pct, err := p.MemoryPercentWithContext(ctx); err == nil {
			info.MemPercent = float64(pct)
		}
		out = append(out, info)
	}
	return out, nil
}

// truncateRunes shortens s to at most max runes (not bytes), so multibyte command
// lines are cut on a rune boundary.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
