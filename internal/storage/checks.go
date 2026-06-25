package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// CheckStatus is the latest known status of one service check.
type CheckStatus struct {
	Node      string
	Name      string    // check name (unique per node)
	Type      string    // http|tcp|process|shell
	Status    string    // "ok" | "failing" | "unknown"
	Detail    string
	LatencyMS int64
	At        time.Time // when last evaluated (UTC)
}

// DeleteCheckStatus removes the stored status for (node, name). No error if absent.
func (db *DB) DeleteCheckStatus(node, name string) error {
	if _, err := db.conn.Exec(`DELETE FROM check_status WHERE node = ? AND name = ?;`, node, name); err != nil {
		return fmt.Errorf("storage: delete check status for node %q name %q: %w", node, name, err)
	}
	return nil
}

// UpsertCheckStatus inserts or updates the current status for (Node, Name).
func (db *DB) UpsertCheckStatus(s CheckStatus) error {
	const q = `
INSERT INTO check_status (node, name, type, status, detail, latency_ms, at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(node, name) DO UPDATE SET
    type       = excluded.type,
    status     = excluded.status,
    detail     = excluded.detail,
    latency_ms = excluded.latency_ms,
    at         = excluded.at;`
	if _, err := db.conn.Exec(
		q,
		s.Node,
		s.Name,
		s.Type,
		s.Status,
		s.Detail,
		s.LatencyMS,
		s.At.UTC().Unix(),
	); err != nil {
		return fmt.Errorf("storage: upsert check status for node %q name %q: %w", s.Node, s.Name, err)
	}
	return nil
}

// CheckStatuses returns all stored check statuses for a node, ordered by Name.
func (db *DB) CheckStatuses(node string) ([]CheckStatus, error) {
	const q = `
SELECT node, name, type, status, detail, latency_ms, at
FROM check_status
WHERE node = ?
ORDER BY name ASC;`
	rows, err := db.conn.Query(q, node)
	if err != nil {
		return nil, fmt.Errorf("storage: query check statuses for node %q: %w", node, err)
	}
	defer rows.Close()

	statuses, err := scanCheckStatuses(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan check statuses for node %q: %w", node, err)
	}
	return statuses, nil
}

// scanCheckStatuses reads all rows into CheckStatus values. Callers own closing
// rows.
func scanCheckStatuses(rows *sql.Rows) ([]CheckStatus, error) {
	var out []CheckStatus
	for rows.Next() {
		var (
			s      CheckStatus
			atUnix int64
		)
		if err := rows.Scan(
			&s.Node, &s.Name, &s.Type, &s.Status, &s.Detail, &s.LatencyMS, &atUnix,
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
