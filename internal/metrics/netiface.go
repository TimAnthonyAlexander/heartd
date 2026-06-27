package metrics

import (
	"context"
	"regexp"
	"strings"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// NetIfaceCounters holds cumulative network counters for one interface since
// boot. Byte throughput is derived by diffing successive readings; error/drop
// counts are diagnostic running totals reported as-is.
type NetIfaceCounters struct {
	RecvBytes uint64 `json:"recv_bytes"`
	SentBytes uint64 `json:"sent_bytes"`
	RecvErrs  uint64 `json:"recv_errs"`
	SentErrs  uint64 `json:"sent_errs"`
	RecvDrops uint64 `json:"recv_drops"`
	SentDrops uint64 `json:"sent_drops"`
}

// loopbackPattern matches loopback interface names (lo, lo0, lo1, …), which are
// excluded from per-interface reporting — local traffic is not a link to localize.
var loopbackPattern = regexp.MustCompile(`^lo\d*$`)

// ReadNetIfaceCounters returns cumulative per-interface network counters keyed by
// interface name. Loopback interfaces are skipped, as are interfaces that are
// entirely idle (no bytes either direction AND no errors/drops) — those are down
// or unused and would only add noise. An interface carrying any traffic, error,
// or drop is kept.
func ReadNetIfaceCounters(ctx context.Context) (map[string]NetIfaceCounters, error) {
	stats, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return nil, err
	}

	out := make(map[string]NetIfaceCounters, len(stats))
	for _, s := range stats {
		if loopbackPattern.MatchString(strings.ToLower(s.Name)) {
			continue
		}
		// Skip interfaces with no activity at all (down/unused): no bytes in
		// either direction and no recorded errors or drops.
		if s.BytesRecv == 0 && s.BytesSent == 0 &&
			s.Errin == 0 && s.Errout == 0 && s.Dropin == 0 && s.Dropout == 0 {
			continue
		}
		out[s.Name] = NetIfaceCounters{
			RecvBytes: s.BytesRecv,
			SentBytes: s.BytesSent,
			RecvErrs:  s.Errin,
			SentErrs:  s.Errout,
			RecvDrops: s.Dropin,
			SentDrops: s.Dropout,
		}
	}
	return out, nil
}
