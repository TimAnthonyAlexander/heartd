// Package storage owns the SQLite schema and persistence of system metric
// samples for heartd. It is self-contained and does not depend on any other
// internal package.
package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection.
type DB struct {
	conn *sql.DB
}

// MetricSample is one persisted reading. Node lets a node store both its own
// and its peers' metrics (forward-looking for multi-node).
type MetricSample struct {
	Node       string
	CPUPercent float64
	MemUsed    uint64
	MemTotal   uint64
	MemPercent float64
	At         time.Time
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS metric_sample (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    node        TEXT NOT NULL,
    cpu_percent REAL NOT NULL,
    mem_used    INTEGER NOT NULL,
    mem_total   INTEGER NOT NULL,
    mem_percent REAL NOT NULL,
    at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metric_sample_node_at ON metric_sample(node, at);
CREATE TABLE IF NOT EXISTS check_status (
    node       TEXT NOT NULL,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL,
    status     TEXT NOT NULL,
    detail     TEXT NOT NULL,
    latency_ms INTEGER NOT NULL,
    at         INTEGER NOT NULL,
    PRIMARY KEY (node, name)
);
CREATE TABLE IF NOT EXISTS peer (
    name       TEXT PRIMARY KEY,
    url        TEXT NOT NULL,
    secret     TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'unknown',
    last_seen  INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT ''
);
`

// Open opens (creating if needed) the SQLite database at path and ensures the
// schema exists. A path of ":memory:" is supported for tests.
func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("storage: open %q: %w", path, err)
	}

	// A single connection avoids surprises with :memory: databases (each
	// connection would otherwise get its own private in-memory database) and
	// keeps WAL/serialization behaviour predictable.
	conn.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
	} {
		if _, err := conn.Exec(pragma); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("storage: exec %q: %w", pragma, err)
		}
	}

	if _, err := conn.Exec(schemaSQL); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("storage: ensure schema: %w", err)
	}

	return &DB{conn: conn}, nil
}

// Close closes the underlying connection.
func (db *DB) Close() error {
	if err := db.conn.Close(); err != nil {
		return fmt.Errorf("storage: close: %w", err)
	}
	return nil
}

// InsertMetric persists one sample.
func (db *DB) InsertMetric(m MetricSample) error {
	const q = `
INSERT INTO metric_sample (node, cpu_percent, mem_used, mem_total, mem_percent, at)
VALUES (?, ?, ?, ?, ?, ?);`
	if _, err := db.conn.Exec(
		q,
		m.Node,
		m.CPUPercent,
		int64(m.MemUsed),
		int64(m.MemTotal),
		m.MemPercent,
		m.At.UTC().Unix(),
	); err != nil {
		return fmt.Errorf("storage: insert metric for node %q: %w", m.Node, err)
	}
	return nil
}

// RecentMetrics returns samples for the given node with At >= since, ordered
// oldest-first, capped at limit rows (limit <= 0 means no cap). When a limit is
// applied, the most-recent limit samples within the window are returned, still
// ordered oldest-first.
func (db *DB) RecentMetrics(node string, since time.Time, limit int) ([]MetricSample, error) {
	sinceUnix := since.UTC().Unix()

	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		// Select the most-recent N rows in the window, then re-order ascending.
		const q = `
SELECT node, cpu_percent, mem_used, mem_total, mem_percent, at FROM (
    SELECT id, node, cpu_percent, mem_used, mem_total, mem_percent, at
    FROM metric_sample
    WHERE node = ? AND at >= ?
    ORDER BY at DESC, id DESC
    LIMIT ?
)
ORDER BY at ASC, id ASC;`
		rows, err = db.conn.Query(q, node, sinceUnix, limit)
	} else {
		const q = `
SELECT node, cpu_percent, mem_used, mem_total, mem_percent, at
FROM metric_sample
WHERE node = ? AND at >= ?
ORDER BY at ASC, id ASC;`
		rows, err = db.conn.Query(q, node, sinceUnix)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: query recent metrics for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan recent metrics for node %q: %w", node, err)
	}
	return samples, nil
}

// LatestMetric returns the single most recent sample for node, or
// (zero, false, nil) if none exists.
func (db *DB) LatestMetric(node string) (MetricSample, bool, error) {
	const q = `
SELECT node, cpu_percent, mem_used, mem_total, mem_percent, at
FROM metric_sample
WHERE node = ?
ORDER BY at DESC, id DESC
LIMIT 1;`

	var (
		m       MetricSample
		memUsed int64
		memTot  int64
		atUnix  int64
	)
	err := db.conn.QueryRow(q, node).Scan(
		&m.Node, &m.CPUPercent, &memUsed, &memTot, &m.MemPercent, &atUnix,
	)
	if err == sql.ErrNoRows {
		return MetricSample{}, false, nil
	}
	if err != nil {
		return MetricSample{}, false, fmt.Errorf("storage: latest metric for node %q: %w", node, err)
	}

	m.MemUsed = uint64(memUsed)
	m.MemTotal = uint64(memTot)
	m.At = time.Unix(atUnix, 0).UTC()
	return m, true, nil
}

// Prune deletes samples with At < before. Returns rows deleted.
func (db *DB) Prune(before time.Time) (int64, error) {
	const q = `DELETE FROM metric_sample WHERE at < ?;`
	res, err := db.conn.Exec(q, before.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("storage: prune before %s: %w", before.UTC(), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: prune rows affected: %w", err)
	}
	return n, nil
}

// scanSamples reads all rows into MetricSample values. Callers own closing rows.
func scanSamples(rows *sql.Rows) ([]MetricSample, error) {
	var out []MetricSample
	for rows.Next() {
		var (
			m       MetricSample
			memUsed int64
			memTot  int64
			atUnix  int64
		)
		if err := rows.Scan(
			&m.Node, &m.CPUPercent, &memUsed, &memTot, &m.MemPercent, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		m.MemUsed = uint64(memUsed)
		m.MemTotal = uint64(memTot)
		m.At = time.Unix(atUnix, 0).UTC()
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
