package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// DiskIOSample is one per-device disk throughput reading for a node. Rates are
// stored as whole units/second (bytes/sec, ops/sec) derived from diffing the
// device's cumulative counters between successive samples.
type DiskIOSample struct {
	Node           string
	Device         string
	ReadBytesRate  uint64 // bytes/sec since previous sample
	WriteBytesRate uint64 // bytes/sec since previous sample
	ReadOpsRate    uint64 // ops/sec since previous sample
	WriteOpsRate   uint64 // ops/sec since previous sample
	At             time.Time
}

// DiskIOPoint is an aggregated throughput reading at one instant, summed across
// all of a node's devices — the shape charted in the dashboard's history view.
type DiskIOPoint struct {
	ReadBytesRate  uint64
	WriteBytesRate uint64
	ReadOpsRate    uint64
	WriteOpsRate   uint64
	At             time.Time
}

// InsertDiskIOSample persists one per-device disk throughput sample.
func (db *DB) InsertDiskIOSample(s DiskIOSample) error {
	const q = `
INSERT INTO disk_io_sample (node, device, read_bytes_rate, write_bytes_rate, read_ops_rate, write_ops_rate, at)
VALUES (?, ?, ?, ?, ?, ?, ?);`
	if _, err := db.conn.Exec(
		q,
		s.Node,
		s.Device,
		int64(s.ReadBytesRate),
		int64(s.WriteBytesRate),
		int64(s.ReadOpsRate),
		int64(s.WriteOpsRate),
		s.At.UTC().Unix(),
	); err != nil {
		return fmt.Errorf("storage: insert disk io sample for node %q device %q: %w", s.Node, s.Device, err)
	}
	return nil
}

// LatestDiskIOSamples returns the most recent per-device snapshot for node: the
// rows sharing the single newest timestamp. Returns an empty slice if none.
func (db *DB) LatestDiskIOSamples(node string) ([]DiskIOSample, error) {
	const q = `
SELECT node, device, read_bytes_rate, write_bytes_rate, read_ops_rate, write_ops_rate, at
FROM disk_io_sample
WHERE node = ? AND at = (SELECT MAX(at) FROM disk_io_sample WHERE node = ?)
ORDER BY device ASC;`
	rows, err := db.conn.Query(q, node, node)
	if err != nil {
		return nil, fmt.Errorf("storage: query latest disk io samples for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanDiskIOSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan latest disk io samples for node %q: %w", node, err)
	}
	return samples, nil
}

// DiskIOHistory returns throughput aggregated across all devices, one point per
// timestamp, for node with At >= since, ordered oldest-first. limit caps the
// number of timestamps (limit <= 0 means no cap); when applied, the most-recent
// limit points within the window are returned, still ordered oldest-first.
func (db *DB) DiskIOHistory(node string, since time.Time, limit int) ([]DiskIOPoint, error) {
	sinceUnix := since.UTC().Unix()

	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		const q = `
SELECT read_bytes_rate, write_bytes_rate, read_ops_rate, write_ops_rate, at FROM (
    SELECT
        SUM(read_bytes_rate)  AS read_bytes_rate,
        SUM(write_bytes_rate) AS write_bytes_rate,
        SUM(read_ops_rate)    AS read_ops_rate,
        SUM(write_ops_rate)   AS write_ops_rate,
        at
    FROM disk_io_sample
    WHERE node = ? AND at >= ?
    GROUP BY at
    ORDER BY at DESC
    LIMIT ?
)
ORDER BY at ASC;`
		rows, err = db.conn.Query(q, node, sinceUnix, limit)
	} else {
		const q = `
SELECT
    SUM(read_bytes_rate)  AS read_bytes_rate,
    SUM(write_bytes_rate) AS write_bytes_rate,
    SUM(read_ops_rate)    AS read_ops_rate,
    SUM(write_ops_rate)   AS write_ops_rate,
    at
FROM disk_io_sample
WHERE node = ? AND at >= ?
GROUP BY at
ORDER BY at ASC;`
		rows, err = db.conn.Query(q, node, sinceUnix)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: query disk io history for node %q: %w", node, err)
	}
	defer rows.Close()

	points, err := scanDiskIOPoints(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan disk io history for node %q: %w", node, err)
	}
	return points, nil
}

// PruneDiskIO deletes disk_io_sample rows with At < before. Returns rows deleted.
func (db *DB) PruneDiskIO(before time.Time) (int64, error) {
	const q = `DELETE FROM disk_io_sample WHERE at < ?;`
	res, err := db.conn.Exec(q, before.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("storage: prune disk io before %s: %w", before.UTC(), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: prune disk io rows affected: %w", err)
	}
	return n, nil
}

// scanDiskIOSamples reads all rows into DiskIOSample values. Callers own closing
// rows.
func scanDiskIOSamples(rows *sql.Rows) ([]DiskIOSample, error) {
	var out []DiskIOSample
	for rows.Next() {
		var (
			s                            DiskIOSample
			readB, writeB, readO, writeO int64
			atUnix                       int64
		)
		if err := rows.Scan(
			&s.Node, &s.Device, &readB, &writeB, &readO, &writeO, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		s.ReadBytesRate = uint64(readB)
		s.WriteBytesRate = uint64(writeB)
		s.ReadOpsRate = uint64(readO)
		s.WriteOpsRate = uint64(writeO)
		s.At = time.Unix(atUnix, 0).UTC()
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}

// scanDiskIOPoints reads aggregated history rows into DiskIOPoint values.
// Callers own closing rows.
func scanDiskIOPoints(rows *sql.Rows) ([]DiskIOPoint, error) {
	var out []DiskIOPoint
	for rows.Next() {
		var (
			p                            DiskIOPoint
			readB, writeB, readO, writeO int64
			atUnix                       int64
		)
		if err := rows.Scan(
			&readB, &writeB, &readO, &writeO, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		p.ReadBytesRate = uint64(readB)
		p.WriteBytesRate = uint64(writeB)
		p.ReadOpsRate = uint64(readO)
		p.WriteOpsRate = uint64(writeO)
		p.At = time.Unix(atUnix, 0).UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
