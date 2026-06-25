package metrics

import (
	"context"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
	gnet "github.com/shirou/gopsutil/v4/net"
)

// DiskUsage is usage for one mount point.
type DiskUsage struct {
	Mount   string  `json:"mount"`
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

// minDiskTotal excludes tiny pseudo/system volumes (< 1 GiB) from disk reporting.
const minDiskTotal = 1 << 30

// pseudoFstypes are filesystems that don't represent real storage and would
// otherwise pollute disk reporting (and falsely trip the disk threshold — e.g.
// macOS devfs reports 100% used).
var pseudoFstypes = map[string]bool{
	"devfs": true, "devtmpfs": true, "tmpfs": true, "ramfs": true,
	"autofs": true, "proc": true, "sysfs": true, "cgroup": true,
	"cgroup2": true, "overlay": true, "squashfs": true, "fdescfs": true,
	"nullfs": true, "none": true,
}

// excludedMountPrefixes are OS-internal mount namespaces (macOS firmlinks,
// cryptex, simulator volumes) that duplicate or hide the real root volume.
var excludedMountPrefixes = []string{
	"/System/Volumes/",
	"/private/var/run/",
}

func excludedMount(fstype, mount string) bool {
	if pseudoFstypes[strings.ToLower(fstype)] {
		return true
	}
	for _, prefix := range excludedMountPrefixes {
		if strings.HasPrefix(mount, prefix) {
			return true
		}
	}
	// Xcode simulator disk images.
	return strings.Contains(mount, "/CoreSimulator/")
}

// Disks returns usage for the machine's real mount points. Pseudo filesystems,
// OS-internal mount namespaces, mounts that cannot be stat'd, and volumes
// smaller than 1 GiB are excluded so the result reflects actual storage.
func Disks(ctx context.Context) ([]DiskUsage, error) {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, err
	}

	usages := make([]DiskUsage, 0, len(partitions))
	seen := make(map[string]bool) // dedup by device
	for _, p := range partitions {
		if excludedMount(p.Fstype, p.Mountpoint) {
			continue
		}
		if p.Device != "" {
			if seen[p.Device] {
				continue
			}
			seen[p.Device] = true
		}

		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			continue
		}
		if u.Total < minDiskTotal {
			continue
		}
		usages = append(usages, DiskUsage{
			Mount:   p.Mountpoint,
			Used:    u.Used,
			Total:   u.Total,
			Percent: round2(u.UsedPercent),
		})
	}

	return usages, nil
}

// NetCounters is aggregate cumulative network IO since boot.
type NetCounters struct {
	RecvBytes uint64 `json:"recv_bytes"`
	SentBytes uint64 `json:"sent_bytes"`
}

// ReadNetCounters returns aggregate bytes received/sent across all interfaces.
func ReadNetCounters(ctx context.Context) (NetCounters, error) {
	counters, err := gnet.IOCountersWithContext(ctx, false)
	if err != nil {
		return NetCounters{}, err
	}

	if len(counters) == 0 {
		return NetCounters{}, nil
	}

	return NetCounters{
		RecvBytes: counters[0].BytesRecv,
		SentBytes: counters[0].BytesSent,
	}, nil
}
