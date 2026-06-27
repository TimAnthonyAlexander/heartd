package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// CPUStateSample is one CPU-state breakdown reading for a node. Each field is a
// percentage (0-100) of CPU time spent in that state over the interval since the
// previous reading; the surfaced states plus Idle sum to ~100.
type CPUStateSample struct {
	Node   string
	User   float64
	System float64
	Nice   float64
	Iowait float64
	Irq    float64
	Steal  float64
	Idle   float64
	At     time.Time
}

// InsertCPUState persists one CPU-state breakdown sample.
func (db *DB) InsertCPUState(s CPUStateSample) error {
	const q = `
INSERT INTO cpu_state_sample (node, user_pct, system_pct, nice_pct, iowait_pct, irq_pct, steal_pct, idle_pct, at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`
	if _, err := db.conn.Exec(
		q,
		s.Node,
		s.User,
		s.System,
		s.Nice,
		s.Iowait,
		s.Irq,
		s.Steal,
		s.Idle,
		s.At.UTC().Unix(),
	); err != nil {
		return fmt.Errorf("storage: insert cpu state sample for node %q: %w", s.Node, err)
	}
	return nil
}

// RecentCPUStates returns samples for the given node with At >= since, ordered
// oldest-first, capped at limit rows (limit <= 0 means no cap). When a limit is
// applied, the most-recent limit samples within the window are returned, still
// ordered oldest-first.
func (db *DB) RecentCPUStates(node string, since time.Time, limit int) ([]CPUStateSample, error) {
	sinceUnix := since.UTC().Unix()

	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		// Select the most-recent N rows in the window, then re-order ascending.
		const q = `
SELECT node, user_pct, system_pct, nice_pct, iowait_pct, irq_pct, steal_pct, idle_pct, at FROM (
    SELECT id, node, user_pct, system_pct, nice_pct, iowait_pct, irq_pct, steal_pct, idle_pct, at
    FROM cpu_state_sample
    WHERE node = ? AND at >= ?
    ORDER BY at DESC, id DESC
    LIMIT ?
)
ORDER BY at ASC, id ASC;`
		rows, err = db.conn.Query(q, node, sinceUnix, limit)
	} else {
		const q = `
SELECT node, user_pct, system_pct, nice_pct, iowait_pct, irq_pct, steal_pct, idle_pct, at
FROM cpu_state_sample
WHERE node = ? AND at >= ?
ORDER BY at ASC, id ASC;`
		rows, err = db.conn.Query(q, node, sinceUnix)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: query recent cpu state samples for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanCPUStateSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan recent cpu state samples for node %q: %w", node, err)
	}
	return samples, nil
}

// CPUStateWindow returns CPU-state samples for node within [from, to]
// (inclusive), downsampled to at most maxPoints by time-bucket decimation
// (earliest sample per bucket kept), ordered oldest-first. Mirrors
// NetSamplesWindow so long ranges stay bounded.
func (db *DB) CPUStateWindow(node string, from, to time.Time, maxPoints int) ([]CPUStateSample, error) {
	fromUnix := from.UTC().Unix()
	toUnix := to.UTC().Unix()
	bucket := bucketSize(fromUnix, toUnix, maxPoints)

	const q = `
SELECT node, user_pct, system_pct, nice_pct, iowait_pct, irq_pct, steal_pct, idle_pct, at
FROM cpu_state_sample
WHERE node = ? AND at >= ? AND at <= ? AND id IN (
    SELECT MIN(id) FROM cpu_state_sample
    WHERE node = ? AND at >= ? AND at <= ?
    GROUP BY (at - ?) / ?
)
ORDER BY at ASC, id ASC;`
	rows, err := db.conn.Query(q, node, fromUnix, toUnix, node, fromUnix, toUnix, fromUnix, bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: query cpu state window for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanCPUStateSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan cpu state window for node %q: %w", node, err)
	}
	return samples, nil
}

// LatestCPUState returns the single most recent sample for node, or
// (zero, false, nil) if none exists.
func (db *DB) LatestCPUState(node string) (CPUStateSample, bool, error) {
	const q = `
SELECT node, user_pct, system_pct, nice_pct, iowait_pct, irq_pct, steal_pct, idle_pct, at
FROM cpu_state_sample
WHERE node = ?
ORDER BY at DESC, id DESC
LIMIT 1;`

	var (
		s      CPUStateSample
		atUnix int64
	)
	err := db.conn.QueryRow(q, node).Scan(
		&s.Node, &s.User, &s.System, &s.Nice, &s.Iowait, &s.Irq, &s.Steal, &s.Idle, &atUnix,
	)
	if err == sql.ErrNoRows {
		return CPUStateSample{}, false, nil
	}
	if err != nil {
		return CPUStateSample{}, false, fmt.Errorf("storage: latest cpu state sample for node %q: %w", node, err)
	}

	s.At = time.Unix(atUnix, 0).UTC()
	return s, true, nil
}

// PruneCPUState deletes cpu_state_sample rows with At < before. Returns rows
// deleted.
func (db *DB) PruneCPUState(before time.Time) (int64, error) {
	const q = `DELETE FROM cpu_state_sample WHERE at < ?;`
	res, err := db.conn.Exec(q, before.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("storage: prune cpu state before %s: %w", before.UTC(), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: prune cpu state rows affected: %w", err)
	}
	return n, nil
}

// scanCPUStateSamples reads all rows into CPUStateSample values. Callers own
// closing rows.
func scanCPUStateSamples(rows *sql.Rows) ([]CPUStateSample, error) {
	var out []CPUStateSample
	for rows.Next() {
		var (
			s      CPUStateSample
			atUnix int64
		)
		if err := rows.Scan(
			&s.Node, &s.User, &s.System, &s.Nice, &s.Iowait, &s.Irq, &s.Steal, &s.Idle, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		s.At = time.Unix(atUnix, 0).UTC()
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
