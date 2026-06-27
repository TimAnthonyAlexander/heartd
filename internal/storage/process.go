package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// ProcessSample is one process's CPU/memory usage for a node at the time of the
// last collection. The collector keeps only the top-N processes by CPU per node;
// this table is replaced wholesale each cycle (see ReplaceProcessTop).
type ProcessSample struct {
	Node       string
	PID        int32
	Name       string
	Command    string
	CPUPercent float64 // share of total machine CPU capacity, 0-100
	MemPercent float64 // share of physical memory, 0-100
	MemRSS     uint64  // resident set size in bytes
	At         time.Time
}

// ReplaceProcessTop atomically replaces a node's top-process set: it deletes the
// node's existing rows and inserts samples, all in one transaction so a reader
// never sees a half-updated set. An empty samples slice simply clears the node.
func (db *DB) ReplaceProcessTop(node string, samples []ProcessSample) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("storage: begin replace process top for node %q: %w", node, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM process_top WHERE node = ?;`, node); err != nil {
		return fmt.Errorf("storage: clear process top for node %q: %w", node, err)
	}

	const q = `
INSERT INTO process_top (node, pid, name, command, cpu_percent, mem_percent, mem_rss, at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);`
	for _, s := range samples {
		if _, err := tx.Exec(
			q,
			node,
			int64(s.PID),
			s.Name,
			s.Command,
			s.CPUPercent,
			s.MemPercent,
			int64(s.MemRSS),
			s.At.UTC().Unix(),
		); err != nil {
			return fmt.Errorf("storage: insert process top for node %q pid %d: %w", node, s.PID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit process top for node %q: %w", node, err)
	}
	return nil
}

// TopProcesses returns a node's stored top processes ordered by CPU share
// (descending), ties broken by memory share. Returns an empty slice if none.
func (db *DB) TopProcesses(node string) ([]ProcessSample, error) {
	const q = `
SELECT node, pid, name, command, cpu_percent, mem_percent, mem_rss, at
FROM process_top
WHERE node = ?
ORDER BY cpu_percent DESC, mem_percent DESC;`
	rows, err := db.conn.Query(q, node)
	if err != nil {
		return nil, fmt.Errorf("storage: query top processes for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanProcessSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan top processes for node %q: %w", node, err)
	}
	return samples, nil
}

// scanProcessSamples reads all rows into ProcessSample values. Callers own
// closing rows.
func scanProcessSamples(rows *sql.Rows) ([]ProcessSample, error) {
	var out []ProcessSample
	for rows.Next() {
		var (
			s      ProcessSample
			pid    int64
			memRSS int64
			atUnix int64
		)
		if err := rows.Scan(
			&s.Node, &pid, &s.Name, &s.Command, &s.CPUPercent, &s.MemPercent, &memRSS, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		s.PID = int32(pid)
		s.MemRSS = uint64(memRSS)
		s.At = time.Unix(atUnix, 0).UTC()
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
