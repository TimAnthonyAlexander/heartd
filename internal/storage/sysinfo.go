package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DiskStatus is the current disk usage of one mount on a node.
type DiskStatus struct {
	Node    string
	Mount   string
	Used    uint64
	Total   uint64
	Percent float64
	At      time.Time
}

// UpsertDiskStatus inserts or updates current usage for (Node, Mount).
func (db *DB) UpsertDiskStatus(d DiskStatus) error {
	const q = `
INSERT INTO disk_status (node, mount, used, total, percent, at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(node, mount) DO UPDATE SET
    used    = excluded.used,
    total   = excluded.total,
    percent = excluded.percent,
    at      = excluded.at;`
	if _, err := db.conn.Exec(
		q,
		d.Node,
		d.Mount,
		int64(d.Used),
		int64(d.Total),
		d.Percent,
		d.At.UTC().Unix(),
	); err != nil {
		return fmt.Errorf("storage: upsert disk status for node %q mount %q: %w", d.Node, d.Mount, err)
	}
	return nil
}

// DeleteDiskStatusesExcept removes stored mounts for a node that are not in
// keep, so disk reporting reflects the currently-present mounts (e.g. after a
// drive is unmounted or filtering changes). An empty keep removes all mounts.
func (db *DB) DeleteDiskStatusesExcept(node string, keep []string) error {
	if len(keep) == 0 {
		if _, err := db.conn.Exec(`DELETE FROM disk_status WHERE node = ?;`, node); err != nil {
			return fmt.Errorf("storage: clear disk statuses for node %q: %w", node, err)
		}
		return nil
	}

	placeholders := make([]string, len(keep))
	args := make([]any, 0, len(keep)+1)
	args = append(args, node)
	for i, m := range keep {
		placeholders[i] = "?"
		args = append(args, m)
	}
	q := fmt.Sprintf(
		`DELETE FROM disk_status WHERE node = ? AND mount NOT IN (%s);`,
		strings.Join(placeholders, ", "),
	)
	if _, err := db.conn.Exec(q, args...); err != nil {
		return fmt.Errorf("storage: prune disk statuses for node %q: %w", node, err)
	}
	return nil
}

// DiskStatuses returns all mounts for a node, ordered by Mount.
func (db *DB) DiskStatuses(node string) ([]DiskStatus, error) {
	const q = `
SELECT node, mount, used, total, percent, at
FROM disk_status
WHERE node = ?
ORDER BY mount ASC;`
	rows, err := db.conn.Query(q, node)
	if err != nil {
		return nil, fmt.Errorf("storage: query disk statuses for node %q: %w", node, err)
	}
	defer rows.Close()

	statuses, err := scanDiskStatuses(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan disk statuses for node %q: %w", node, err)
	}
	return statuses, nil
}

// scanDiskStatuses reads all rows into DiskStatus values. Callers own closing
// rows.
func scanDiskStatuses(rows *sql.Rows) ([]DiskStatus, error) {
	var out []DiskStatus
	for rows.Next() {
		var (
			d      DiskStatus
			used   int64
			total  int64
			atUnix int64
		)
		if err := rows.Scan(
			&d.Node, &d.Mount, &used, &total, &d.Percent, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		d.Used = uint64(used)
		d.Total = uint64(total)
		d.At = time.Unix(atUnix, 0).UTC()
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}

// NetSample is one network throughput reading for a node.
type NetSample struct {
	Node      string
	RecvBytes uint64  // cumulative since boot
	SentBytes uint64  // cumulative since boot
	RecvRate  float64 // bytes/sec since previous sample
	SentRate  float64 // bytes/sec since previous sample
	At        time.Time
}

// InsertNetSample persists one network throughput sample.
func (db *DB) InsertNetSample(s NetSample) error {
	const q = `
INSERT INTO net_sample (node, recv_bytes, sent_bytes, recv_rate, sent_rate, at)
VALUES (?, ?, ?, ?, ?, ?);`
	if _, err := db.conn.Exec(
		q,
		s.Node,
		int64(s.RecvBytes),
		int64(s.SentBytes),
		s.RecvRate,
		s.SentRate,
		s.At.UTC().Unix(),
	); err != nil {
		return fmt.Errorf("storage: insert net sample for node %q: %w", s.Node, err)
	}
	return nil
}

// RecentNetSamples returns samples for the given node with At >= since, ordered
// oldest-first, capped at limit rows (limit <= 0 means no cap). When a limit is
// applied, the most-recent limit samples within the window are returned, still
// ordered oldest-first.
func (db *DB) RecentNetSamples(node string, since time.Time, limit int) ([]NetSample, error) {
	sinceUnix := since.UTC().Unix()

	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		// Select the most-recent N rows in the window, then re-order ascending.
		const q = `
SELECT node, recv_bytes, sent_bytes, recv_rate, sent_rate, at FROM (
    SELECT id, node, recv_bytes, sent_bytes, recv_rate, sent_rate, at
    FROM net_sample
    WHERE node = ? AND at >= ?
    ORDER BY at DESC, id DESC
    LIMIT ?
)
ORDER BY at ASC, id ASC;`
		rows, err = db.conn.Query(q, node, sinceUnix, limit)
	} else {
		const q = `
SELECT node, recv_bytes, sent_bytes, recv_rate, sent_rate, at
FROM net_sample
WHERE node = ? AND at >= ?
ORDER BY at ASC, id ASC;`
		rows, err = db.conn.Query(q, node, sinceUnix)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: query recent net samples for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanNetSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan recent net samples for node %q: %w", node, err)
	}
	return samples, nil
}

// NetSamplesWindow returns net samples for node within [from, to] (inclusive),
// downsampled to at most maxPoints by time-bucket decimation (earliest sample
// per bucket kept), ordered oldest-first. Mirrors MetricsWindow so long ranges
// stay bounded.
func (db *DB) NetSamplesWindow(node string, from, to time.Time, maxPoints int) ([]NetSample, error) {
	fromUnix := from.UTC().Unix()
	toUnix := to.UTC().Unix()
	bucket := bucketSize(fromUnix, toUnix, maxPoints)

	const q = `
SELECT node, recv_bytes, sent_bytes, recv_rate, sent_rate, at
FROM net_sample
WHERE node = ? AND at >= ? AND at <= ? AND id IN (
    SELECT MIN(id) FROM net_sample
    WHERE node = ? AND at >= ? AND at <= ?
    GROUP BY (at - ?) / ?
)
ORDER BY at ASC, id ASC;`
	rows, err := db.conn.Query(q, node, fromUnix, toUnix, node, fromUnix, toUnix, fromUnix, bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: query net samples window for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanNetSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan net samples window for node %q: %w", node, err)
	}
	return samples, nil
}

// LatestNetSample returns the single most recent sample for node, or
// (zero, false, nil) if none exists.
func (db *DB) LatestNetSample(node string) (NetSample, bool, error) {
	const q = `
SELECT node, recv_bytes, sent_bytes, recv_rate, sent_rate, at
FROM net_sample
WHERE node = ?
ORDER BY at DESC, id DESC
LIMIT 1;`

	var (
		s      NetSample
		recv   int64
		sent   int64
		atUnix int64
	)
	err := db.conn.QueryRow(q, node).Scan(
		&s.Node, &recv, &sent, &s.RecvRate, &s.SentRate, &atUnix,
	)
	if err == sql.ErrNoRows {
		return NetSample{}, false, nil
	}
	if err != nil {
		return NetSample{}, false, fmt.Errorf("storage: latest net sample for node %q: %w", node, err)
	}

	s.RecvBytes = uint64(recv)
	s.SentBytes = uint64(sent)
	s.At = time.Unix(atUnix, 0).UTC()
	return s, true, nil
}

// PruneNet deletes net_sample rows with At < before. Returns rows deleted.
func (db *DB) PruneNet(before time.Time) (int64, error) {
	const q = `DELETE FROM net_sample WHERE at < ?;`
	res, err := db.conn.Exec(q, before.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("storage: prune net before %s: %w", before.UTC(), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: prune net rows affected: %w", err)
	}
	return n, nil
}

// scanNetSamples reads all rows into NetSample values. Callers own closing rows.
func scanNetSamples(rows *sql.Rows) ([]NetSample, error) {
	var out []NetSample
	for rows.Next() {
		var (
			s      NetSample
			recv   int64
			sent   int64
			atUnix int64
		)
		if err := rows.Scan(
			&s.Node, &recv, &sent, &s.RecvRate, &s.SentRate, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		s.RecvBytes = uint64(recv)
		s.SentBytes = uint64(sent)
		s.At = time.Unix(atUnix, 0).UTC()
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
