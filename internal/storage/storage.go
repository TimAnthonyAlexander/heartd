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
	// Load averages over 1/5/15 minutes (0 where the platform has no load avg).
	Load1  float64
	Load5  float64
	Load15 float64
	// Swap usage (SwapTotal is 0 on hosts without swap).
	SwapUsed    uint64
	SwapTotal   uint64
	SwapPercent float64
	At          time.Time
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS metric_sample (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    node         TEXT NOT NULL,
    cpu_percent  REAL NOT NULL,
    mem_used     INTEGER NOT NULL,
    mem_total    INTEGER NOT NULL,
    mem_percent  REAL NOT NULL,
    load1        REAL NOT NULL DEFAULT 0,
    load5        REAL NOT NULL DEFAULT 0,
    load15       REAL NOT NULL DEFAULT 0,
    swap_used    INTEGER NOT NULL DEFAULT 0,
    swap_total   INTEGER NOT NULL DEFAULT 0,
    swap_percent REAL NOT NULL DEFAULT 0,
    at           INTEGER NOT NULL
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
    last_error TEXT NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS disk_status (
    node    TEXT NOT NULL,
    mount   TEXT NOT NULL,
    used    INTEGER NOT NULL,
    total   INTEGER NOT NULL,
    percent REAL NOT NULL,
    at      INTEGER NOT NULL,
    PRIMARY KEY (node, mount)
);
CREATE TABLE IF NOT EXISTS disk_usage_sample (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    node    TEXT NOT NULL,
    mount   TEXT NOT NULL,
    used    INTEGER NOT NULL,
    total   INTEGER NOT NULL,
    percent REAL NOT NULL,
    at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_disk_usage_sample_node_mount_at ON disk_usage_sample(node, mount, at);
CREATE TABLE IF NOT EXISTS net_sample (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    node       TEXT NOT NULL,
    recv_bytes INTEGER NOT NULL,
    sent_bytes INTEGER NOT NULL,
    recv_rate  REAL NOT NULL,
    sent_rate  REAL NOT NULL,
    at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_net_sample_node_at ON net_sample(node, at);
CREATE TABLE IF NOT EXISTS cpu_state_sample (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    node       TEXT NOT NULL,
    user_pct   REAL NOT NULL,
    system_pct REAL NOT NULL,
    nice_pct   REAL NOT NULL,
    iowait_pct REAL NOT NULL,
    irq_pct    REAL NOT NULL,
    steal_pct  REAL NOT NULL,
    idle_pct   REAL NOT NULL,
    at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cpu_state_sample_node_at ON cpu_state_sample(node, at);
CREATE TABLE IF NOT EXISTS disk_io_sample (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    node             TEXT NOT NULL,
    device           TEXT NOT NULL,
    read_bytes_rate  INTEGER NOT NULL,
    write_bytes_rate INTEGER NOT NULL,
    read_ops_rate    INTEGER NOT NULL,
    write_ops_rate   INTEGER NOT NULL,
    at               INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_disk_io_sample_node_at ON disk_io_sample(node, at);
CREATE INDEX IF NOT EXISTS idx_disk_io_sample_node_device_at ON disk_io_sample(node, device, at);
CREATE TABLE IF NOT EXISTS process_top (
    node        TEXT NOT NULL,
    pid         INTEGER NOT NULL,
    name        TEXT NOT NULL,
    command     TEXT NOT NULL,
    cpu_percent REAL NOT NULL,
    mem_percent REAL NOT NULL,
    mem_rss     INTEGER NOT NULL,
    at          INTEGER NOT NULL,
    PRIMARY KEY (node, pid)
);
CREATE TABLE IF NOT EXISTS cpu_core (
    node    TEXT NOT NULL,
    core    INTEGER NOT NULL,
    percent REAL NOT NULL,
    at      INTEGER NOT NULL,
    PRIMARY KEY (node, core)
);
CREATE TABLE IF NOT EXISTS net_interface (
    node       TEXT NOT NULL,
    iface      TEXT NOT NULL,
    recv_rate  INTEGER NOT NULL,
    sent_rate  INTEGER NOT NULL,
    recv_errs  INTEGER NOT NULL,
    sent_errs  INTEGER NOT NULL,
    recv_drops INTEGER NOT NULL,
    sent_drops INTEGER NOT NULL,
    at         INTEGER NOT NULL,
    PRIMARY KEY (node, iface)
);
CREATE TABLE IF NOT EXISTS node_alias (
    node             TEXT PRIMARY KEY,
    alias            TEXT NOT NULL,
    advertised_alias TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS raid_array (
    node           TEXT NOT NULL,
    name           TEXT NOT NULL,
    level          TEXT NOT NULL,
    state          TEXT NOT NULL,
    total_devices  INTEGER NOT NULL,
    active_devices INTEGER NOT NULL,
    resync_percent REAL NOT NULL,
    detail         TEXT NOT NULL,
    at             INTEGER NOT NULL,
    PRIMARY KEY (node, name)
);
CREATE TABLE IF NOT EXISTS smart_disk (
    node              TEXT NOT NULL,
    device            TEXT NOT NULL,
    model             TEXT NOT NULL,
    serial            TEXT NOT NULL,
    health            TEXT NOT NULL,
    reallocated       INTEGER NOT NULL,
    pending           INTEGER NOT NULL,
    uncorrectable     INTEGER NOT NULL,
    crc_errors        INTEGER NOT NULL,
    temp_c            INTEGER NOT NULL,
    power_on_hours    INTEGER NOT NULL,
    power_cycle_count INTEGER NOT NULL,
    source_at         INTEGER NOT NULL,
    at                INTEGER NOT NULL,
    PRIMARY KEY (node, device)
);
CREATE TABLE IF NOT EXISTS user (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS session (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_expires ON session(expires_at);
CREATE TABLE IF NOT EXISTS setting (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS check_config (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    type         TEXT NOT NULL,
    interval_sec INTEGER NOT NULL,
    timeout_sec  INTEGER NOT NULL,
    url          TEXT NOT NULL DEFAULT '',
    method       TEXT NOT NULL DEFAULT '',
    host         TEXT NOT NULL DEFAULT '',
    port         INTEGER NOT NULL DEFAULT 0,
    process      TEXT NOT NULL DEFAULT '',
    command      TEXT NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    accept_any        INTEGER NOT NULL DEFAULT 0,
    accepted_statuses TEXT NOT NULL DEFAULT '',
    user_agent        TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS alert_rule (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    name                  TEXT NOT NULL,
    enabled               INTEGER NOT NULL DEFAULT 1,
    source                TEXT NOT NULL,
    entity                TEXT NOT NULL DEFAULT '',
    comparator            TEXT NOT NULL DEFAULT '>=',
    threshold             REAL NOT NULL DEFAULT 0,
    for_seconds           INTEGER NOT NULL DEFAULT 0,
    recover_grace_seconds INTEGER NOT NULL DEFAULT 0,
    severity              TEXT NOT NULL DEFAULT 'warning',
    created_at            INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS alert_event (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    node        TEXT NOT NULL,
    observer    TEXT NOT NULL DEFAULT '',
    rule_id     TEXT NOT NULL DEFAULT '',
    rule_source TEXT NOT NULL DEFAULT '',
    entity      TEXT NOT NULL DEFAULT '',
    severity    TEXT NOT NULL DEFAULT '',
    state       TEXT NOT NULL,
    subject     TEXT NOT NULL DEFAULT '',
    detail      TEXT NOT NULL DEFAULT '',
    at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_alert_event_node_at ON alert_event(node, at);
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

	// Column additions to existing tables (CREATE TABLE IF NOT EXISTS won't add
	// them to a database created by an earlier version).
	if err := ensureColumns(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return &DB{conn: conn}, nil
}

// ensureColumns applies idempotent ADD COLUMN migrations for columns introduced
// after a table's initial release.
func ensureColumns(conn *sql.DB) error {
	type col struct{ table, name, def string }
	migrations := []col{
		{"peer", "enabled", "INTEGER NOT NULL DEFAULT 1"},
		{"metric_sample", "load1", "REAL NOT NULL DEFAULT 0"},
		{"metric_sample", "load5", "REAL NOT NULL DEFAULT 0"},
		{"metric_sample", "load15", "REAL NOT NULL DEFAULT 0"},
		{"metric_sample", "swap_used", "INTEGER NOT NULL DEFAULT 0"},
		{"metric_sample", "swap_total", "INTEGER NOT NULL DEFAULT 0"},
		{"metric_sample", "swap_percent", "REAL NOT NULL DEFAULT 0"},
		{"node_alias", "advertised_alias", "TEXT NOT NULL DEFAULT ''"},
		{"alert_event", "observer", "TEXT NOT NULL DEFAULT ''"},
		{"check_config", "accept_any", "INTEGER NOT NULL DEFAULT 0"},
		{"check_config", "accepted_statuses", "TEXT NOT NULL DEFAULT ''"},
		{"check_config", "user_agent", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, m := range migrations {
		has, err := hasColumn(conn, m.table, m.name)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := conn.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", m.table, m.name, m.def)); err != nil {
			return fmt.Errorf("storage: add %s.%s: %w", m.table, m.name, err)
		}
	}
	return nil
}

// hasColumn reports whether table has a column named col.
func hasColumn(conn *sql.DB, table, col string) (bool, error) {
	rows, err := conn.Query("PRAGMA table_info(" + table + ");")
	if err != nil {
		return false, fmt.Errorf("storage: table_info(%s): %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
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
INSERT INTO metric_sample (node, cpu_percent, mem_used, mem_total, mem_percent, load1, load5, load15, swap_used, swap_total, swap_percent, at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`
	if _, err := db.conn.Exec(
		q,
		m.Node,
		m.CPUPercent,
		int64(m.MemUsed),
		int64(m.MemTotal),
		m.MemPercent,
		m.Load1,
		m.Load5,
		m.Load15,
		int64(m.SwapUsed),
		int64(m.SwapTotal),
		m.SwapPercent,
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
SELECT node, cpu_percent, mem_used, mem_total, mem_percent, load1, load5, load15, swap_used, swap_total, swap_percent, at FROM (
    SELECT id, node, cpu_percent, mem_used, mem_total, mem_percent, load1, load5, load15, swap_used, swap_total, swap_percent, at
    FROM metric_sample
    WHERE node = ? AND at >= ?
    ORDER BY at DESC, id DESC
    LIMIT ?
)
ORDER BY at ASC, id ASC;`
		rows, err = db.conn.Query(q, node, sinceUnix, limit)
	} else {
		const q = `
SELECT node, cpu_percent, mem_used, mem_total, mem_percent, load1, load5, load15, swap_used, swap_total, swap_percent, at
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

// bucketSize returns the width, in seconds, of each downsample bucket so that a
// [fromUnix, toUnix] window splits into about maxPoints equal buckets. The result
// is always >= 1 so integer bucketing never divides by zero.
func bucketSize(fromUnix, toUnix int64, maxPoints int) int64 {
	if maxPoints < 1 {
		maxPoints = 1
	}
	span := toUnix - fromUnix
	if span < 1 {
		return 1
	}
	if b := span / int64(maxPoints); b > 1 {
		return b
	}
	return 1
}

// MetricsWindow returns samples for node within [from, to] (inclusive),
// downsampled to about maxPoints by time-bucket decimation: the window is split
// into maxPoints equal time buckets and the earliest sample in each bucket is
// kept (so at most maxPoints+1 rows — the inclusive top edge can fall in its own
// bucket). Results are ordered oldest-first. This bounds the row count for long
// (multi-day) ranges so the dashboard never has to render tens of thousands of
// points.
func (db *DB) MetricsWindow(node string, from, to time.Time, maxPoints int) ([]MetricSample, error) {
	fromUnix := from.UTC().Unix()
	toUnix := to.UTC().Unix()
	bucket := bucketSize(fromUnix, toUnix, maxPoints)

	const q = `
SELECT node, cpu_percent, mem_used, mem_total, mem_percent, load1, load5, load15, swap_used, swap_total, swap_percent, at
FROM metric_sample
WHERE node = ? AND at >= ? AND at <= ? AND id IN (
    SELECT MIN(id) FROM metric_sample
    WHERE node = ? AND at >= ? AND at <= ?
    GROUP BY (at - ?) / ?
)
ORDER BY at ASC, id ASC;`
	rows, err := db.conn.Query(q, node, fromUnix, toUnix, node, fromUnix, toUnix, fromUnix, bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: query metrics window for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan metrics window for node %q: %w", node, err)
	}
	return samples, nil
}

// LatestMetric returns the single most recent sample for node, or
// (zero, false, nil) if none exists.
func (db *DB) LatestMetric(node string) (MetricSample, bool, error) {
	const q = `
SELECT node, cpu_percent, mem_used, mem_total, mem_percent, load1, load5, load15, swap_used, swap_total, swap_percent, at
FROM metric_sample
WHERE node = ?
ORDER BY at DESC, id DESC
LIMIT 1;`

	var (
		m        MetricSample
		memUsed  int64
		memTot   int64
		swapUsed int64
		swapTot  int64
		atUnix   int64
	)
	err := db.conn.QueryRow(q, node).Scan(
		&m.Node, &m.CPUPercent, &memUsed, &memTot, &m.MemPercent,
		&m.Load1, &m.Load5, &m.Load15, &swapUsed, &swapTot, &m.SwapPercent, &atUnix,
	)
	if err == sql.ErrNoRows {
		return MetricSample{}, false, nil
	}
	if err != nil {
		return MetricSample{}, false, fmt.Errorf("storage: latest metric for node %q: %w", node, err)
	}

	m.MemUsed = uint64(memUsed)
	m.MemTotal = uint64(memTot)
	m.SwapUsed = uint64(swapUsed)
	m.SwapTotal = uint64(swapTot)
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
			m        MetricSample
			memUsed  int64
			memTot   int64
			swapUsed int64
			swapTot  int64
			atUnix   int64
		)
		if err := rows.Scan(
			&m.Node, &m.CPUPercent, &memUsed, &memTot, &m.MemPercent,
			&m.Load1, &m.Load5, &m.Load15, &swapUsed, &swapTot, &m.SwapPercent, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		m.MemUsed = uint64(memUsed)
		m.MemTotal = uint64(memTot)
		m.SwapUsed = uint64(swapUsed)
		m.SwapTotal = uint64(swapTot)
		m.At = time.Unix(atUnix, 0).UTC()
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
