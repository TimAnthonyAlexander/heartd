package metrics

import (
	"context"
	"regexp"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

// DiskIOCounters holds cumulative I/O counters for one physical device since
// boot. Rates are derived by diffing successive readings.
type DiskIOCounters struct {
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
	ReadOps    uint64 `json:"read_ops"`
	WriteOps   uint64 `json:"write_ops"`
}

// physicalDiskPatterns match whole physical block devices, deliberately
// excluding partitions (which would double-count a disk's I/O), as well as
// loop/ram/dm/md pseudo-devices. Keeping only whole disks mirrors the spirit of
// the mount-point filtering in Disks.
var physicalDiskPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(sd|vd|hd|xvd)[a-z]+$`), // sda, vdb, xvda (no trailing partition digit)
	regexp.MustCompile(`^nvme\d+n\d+$`),          // nvme0n1 (not nvme0n1p1)
	regexp.MustCompile(`^mmcblk\d+$`),            // mmcblk0 (not mmcblk0p1)
	regexp.MustCompile(`^disk\d+$`),              // macOS disk0 (not disk0s1)
}

// isPhysicalDisk reports whether name is a whole physical disk worth sampling
// for I/O throughput, filtering out partitions and virtual/pseudo devices.
func isPhysicalDisk(name string) bool {
	n := strings.ToLower(name)
	for _, re := range physicalDiskPatterns {
		if re.MatchString(n) {
			return true
		}
	}
	return false
}

// ReadDiskIOCounters returns cumulative I/O counters keyed by physical device
// name. Partitions and pseudo-devices are excluded so the result reflects real
// disks only.
func ReadDiskIOCounters(ctx context.Context) (map[string]DiskIOCounters, error) {
	stats, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]DiskIOCounters, len(stats))
	for name, s := range stats {
		if !isPhysicalDisk(name) {
			continue
		}
		out[name] = DiskIOCounters{
			ReadBytes:  s.ReadBytes,
			WriteBytes: s.WriteBytes,
			ReadOps:    s.ReadCount,
			WriteOps:   s.WriteCount,
		}
	}
	return out, nil
}
