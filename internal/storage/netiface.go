package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// NetIfaceSample is one interface's network state for a node at the time of the
// last collection. Byte throughput is a derived rate (bytes/sec); error/drop
// fields are cumulative running totals since boot. Per-interface network is a
// current-snapshot metric (no history): this table is replaced wholesale each
// cycle (see ReplaceNetInterfaces).
type NetIfaceSample struct {
	Node      string
	Iface     string
	RecvRate  uint64 // bytes received per second
	SentRate  uint64 // bytes sent per second
	RecvErrs  uint64 // cumulative receive errors since boot
	SentErrs  uint64 // cumulative send errors since boot
	RecvDrops uint64 // cumulative receive drops since boot
	SentDrops uint64 // cumulative send drops since boot
	At        time.Time
}

// ReplaceNetInterfaces atomically replaces a node's per-interface snapshot: it
// deletes the node's existing rows and inserts samples, all in one transaction so
// a reader never sees a half-updated set. An empty samples slice clears the node.
func (db *DB) ReplaceNetInterfaces(node string, samples []NetIfaceSample) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("storage: begin replace net interfaces for node %q: %w", node, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM net_interface WHERE node = ?;`, node); err != nil {
		return fmt.Errorf("storage: clear net interfaces for node %q: %w", node, err)
	}

	const q = `
INSERT INTO net_interface (node, iface, recv_rate, sent_rate, recv_errs, sent_errs, recv_drops, sent_drops, at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`
	for _, s := range samples {
		if _, err := tx.Exec(
			q,
			node,
			s.Iface,
			int64(s.RecvRate),
			int64(s.SentRate),
			int64(s.RecvErrs),
			int64(s.SentErrs),
			int64(s.RecvDrops),
			int64(s.SentDrops),
			s.At.UTC().Unix(),
		); err != nil {
			return fmt.Errorf("storage: insert net interface for node %q iface %q: %w", node, s.Iface, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit net interfaces for node %q: %w", node, err)
	}
	return nil
}

// NetInterfaces returns a node's stored per-interface snapshot ordered by
// interface name ascending. Returns an empty slice if none.
func (db *DB) NetInterfaces(node string) ([]NetIfaceSample, error) {
	const q = `
SELECT node, iface, recv_rate, sent_rate, recv_errs, sent_errs, recv_drops, sent_drops, at
FROM net_interface
WHERE node = ?
ORDER BY iface ASC;`
	rows, err := db.conn.Query(q, node)
	if err != nil {
		return nil, fmt.Errorf("storage: query net interfaces for node %q: %w", node, err)
	}
	defer rows.Close()

	samples, err := scanNetIfaceSamples(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan net interfaces for node %q: %w", node, err)
	}
	return samples, nil
}

// scanNetIfaceSamples reads all rows into NetIfaceSample values. Callers own
// closing rows.
func scanNetIfaceSamples(rows *sql.Rows) ([]NetIfaceSample, error) {
	var out []NetIfaceSample
	for rows.Next() {
		var (
			s      NetIfaceSample
			atUnix int64
		)
		if err := rows.Scan(
			&s.Node, &s.Iface, &s.RecvRate, &s.SentRate,
			&s.RecvErrs, &s.SentErrs, &s.RecvDrops, &s.SentDrops, &atUnix,
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
