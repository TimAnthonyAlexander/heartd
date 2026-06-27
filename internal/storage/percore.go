package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// CoreSample is one logical core's busy percentage for a node at the time of the
// last collection. Per-core utilization is a current-snapshot metric (no history):
// this table is replaced wholesale each cycle (see ReplacePerCore).
type CoreSample struct {
	Node    string
	Core    int
	Percent float64 // busy share of this core, 0-100
	At      time.Time
}

// ReplacePerCore atomically replaces a node's per-core snapshot: it deletes the
// node's existing rows and inserts samples, all in one transaction so a reader
// never sees a half-updated set. An empty samples slice simply clears the node.
func (db *DB) ReplacePerCore(node string, samples []CoreSample) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("storage: begin replace per-core for node %q: %w", node, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM cpu_core WHERE node = ?;`, node); err != nil {
		return fmt.Errorf("storage: clear per-core for node %q: %w", node, err)
	}

	const q = `
INSERT INTO cpu_core (node, core, percent, at)
VALUES (?, ?, ?, ?);`
	for _, s := range samples {
		if _, err := tx.Exec(
			q,
			node,
			s.Core,
			s.Percent,
			s.At.UTC().Unix(),
		); err != nil {
			return fmt.Errorf("storage: insert per-core for node %q core %d: %w", node, s.Core, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit per-core for node %q: %w", node, err)
	}
	return nil
}

// PerCore returns a node's stored per-core busy snapshot ordered by core index
// ascending. Returns an empty slice if none.
func (db *DB) PerCore(node string) ([]CoreSample, error) {
	const q = `
SELECT node, core, percent, at
FROM cpu_core
WHERE node = ?
ORDER BY core ASC;`
	rows, err := db.conn.Query(q, node)
	if err != nil {
		return nil, fmt.Errorf("storage: query per-core for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanCoreSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan per-core for node %q: %w", node, err)
	}
	return samples, nil
}

// scanCoreSamples reads all rows into CoreSample values. Callers own closing rows.
func scanCoreSamples(rows *sql.Rows) ([]CoreSample, error) {
	var out []CoreSample
	for rows.Next() {
		var (
			s      CoreSample
			atUnix int64
		)
		if err := rows.Scan(&s.Node, &s.Core, &s.Percent, &atUnix); err != nil {
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
