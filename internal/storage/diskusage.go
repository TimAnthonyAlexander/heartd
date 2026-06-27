package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// DiskUsageSample is one persisted capacity reading for a single mount on a
// node. Unlike disk_status (a replace-on-write snapshot of current usage), these
// rows accumulate over time so a fill-rate forecast can be regressed from them.
type DiskUsageSample struct {
	Node    string
	Mount   string
	Used    uint64
	Total   uint64
	Percent float64
	At      time.Time
}

// DiskUsagePoint is one capacity reading at an instant for a single mount — the
// shape charted in the dashboard's capacity history view and fed to the forecast.
type DiskUsagePoint struct {
	Used    uint64
	Total   uint64
	Percent float64
	At      time.Time
}

// InsertDiskUsageSample persists one capacity sample for a mount.
func (db *DB) InsertDiskUsageSample(s DiskUsageSample) error {
	const q = `
INSERT INTO disk_usage_sample (node, mount, used, total, percent, at)
VALUES (?, ?, ?, ?, ?, ?);`
	if _, err := db.conn.Exec(
		q,
		s.Node,
		s.Mount,
		int64(s.Used),
		int64(s.Total),
		s.Percent,
		s.At.UTC().Unix(),
	); err != nil {
		return fmt.Errorf("storage: insert disk usage sample for node %q mount %q: %w", s.Node, s.Mount, err)
	}
	return nil
}

// DiskUsageWindow returns capacity points for a single mount on node within
// [from, to] (inclusive), downsampled to at most maxPoints by time-bucket
// decimation: the window is split into maxPoints equal time buckets and the
// earliest sample in each bucket is kept. Ordered oldest-first. Mirrors
// DiskIOWindow so long ranges stay bounded.
func (db *DB) DiskUsageWindow(node, mount string, from, to time.Time, maxPoints int) ([]DiskUsagePoint, error) {
	fromUnix := from.UTC().Unix()
	toUnix := to.UTC().Unix()
	bucket := bucketSize(fromUnix, toUnix, maxPoints)

	const q = `
SELECT used, total, percent, at
FROM disk_usage_sample
WHERE node = ? AND mount = ? AND at >= ? AND at <= ? AND id IN (
    SELECT MIN(id) FROM disk_usage_sample
    WHERE node = ? AND mount = ? AND at >= ? AND at <= ?
    GROUP BY (at - ?) / ?
)
ORDER BY at ASC, id ASC;`
	rows, err := db.conn.Query(q, node, mount, fromUnix, toUnix, node, mount, fromUnix, toUnix, fromUnix, bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: query disk usage window for node %q mount %q: %w", node, mount, err)
	}
	defer rows.Close()

	points, err := scanDiskUsagePoints(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan disk usage window for node %q mount %q: %w", node, mount, err)
	}
	return points, nil
}

// RecentDiskUsage returns every capacity point for a single mount on node with
// At >= since, ordered oldest-first and NOT downsampled. It feeds the fill-rate
// regression, so the caller bounds the row count by passing a since within the
// forecast lookback window.
func (db *DB) RecentDiskUsage(node, mount string, since time.Time) ([]DiskUsagePoint, error) {
	const q = `
SELECT used, total, percent, at
FROM disk_usage_sample
WHERE node = ? AND mount = ? AND at >= ?
ORDER BY at ASC, id ASC;`
	rows, err := db.conn.Query(q, node, mount, since.UTC().Unix())
	if err != nil {
		return nil, fmt.Errorf("storage: query recent disk usage for node %q mount %q: %w", node, mount, err)
	}
	defer rows.Close()

	points, err := scanDiskUsagePoints(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan recent disk usage for node %q mount %q: %w", node, mount, err)
	}
	return points, nil
}

// PruneDiskUsage deletes disk_usage_sample rows with At < before. Returns rows
// deleted.
func (db *DB) PruneDiskUsage(before time.Time) (int64, error) {
	const q = `DELETE FROM disk_usage_sample WHERE at < ?;`
	res, err := db.conn.Exec(q, before.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("storage: prune disk usage before %s: %w", before.UTC(), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: prune disk usage rows affected: %w", err)
	}
	return n, nil
}

// scanDiskUsagePoints reads all rows into DiskUsagePoint values. Callers own
// closing rows.
func scanDiskUsagePoints(rows *sql.Rows) ([]DiskUsagePoint, error) {
	var out []DiskUsagePoint
	for rows.Next() {
		var (
			p           DiskUsagePoint
			used, total int64
			atUnix      int64
		)
		if err := rows.Scan(&used, &total, &p.Percent, &atUnix); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		p.Used = uint64(used)
		p.Total = uint64(total)
		p.At = time.Unix(atUnix, 0).UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
