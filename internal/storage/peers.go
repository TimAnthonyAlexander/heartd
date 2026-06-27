package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Peer is a known peer node.
type Peer struct {
	Name      string
	URL       string
	Secret    string    // shared secret used to authenticate to this peer; "" if unknown
	Status    string    // "ok" | "down" | "unknown"
	LastSeen  time.Time // last successful contact; zero value when never seen
	LastError string    // last poll error; "" when none
	Enabled   bool      // false = muted: not polled, not alerted on, grayed out
}

// UpsertPeer inserts a peer by Name, or updates its URL and (only if the
// incoming Secret is non-empty) its Secret. It does NOT modify status,
// last_seen, or last_error of an existing row. Used for config seeding and for
// announce handling (which passes an empty Secret and must not clobber an
// existing one).
func (db *DB) UpsertPeer(p Peer) error {
	const q = `
INSERT INTO peer (name, url, secret)
VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    url    = excluded.url,
    secret = CASE WHEN excluded.secret <> '' THEN excluded.secret ELSE peer.secret END;`
	if _, err := db.conn.Exec(q, p.Name, p.URL, p.Secret); err != nil {
		return fmt.Errorf("storage: upsert peer %q: %w", p.Name, err)
	}
	return nil
}

// GetPeer returns the named peer, or ok=false if no such peer exists.
func (db *DB) GetPeer(name string) (Peer, bool, error) {
	const q = `
SELECT name, url, secret, status, last_seen, last_error, enabled
FROM peer
WHERE name = ?;`
	rows, err := db.conn.Query(q, name)
	if err != nil {
		return Peer{}, false, fmt.Errorf("storage: query peer %q: %w", name, err)
	}
	defer rows.Close()

	peers, err := scanPeers(rows)
	if err != nil {
		return Peer{}, false, fmt.Errorf("storage: scan peer %q: %w", name, err)
	}
	if len(peers) == 0 {
		return Peer{}, false, nil
	}
	return peers[0], true, nil
}

// DeletePeer removes a peer row by name. It does NOT touch the metric/check/disk/
// net data stored under that node's name — see DeleteNodeData for that.
func (db *DB) DeletePeer(name string) error {
	if _, err := db.conn.Exec(`DELETE FROM peer WHERE name = ?;`, name); err != nil {
		return fmt.Errorf("storage: delete peer %q: %w", name, err)
	}
	return nil
}

// DeleteNodeData purges all metric, check, disk, and network rows stored under a
// node's name. Used when a peer is removed so no orphaned history remains.
func (db *DB) DeleteNodeData(name string) error {
	for _, table := range []string{"metric_sample", "check_status", "disk_status", "net_sample", "process_top"} {
		if _, err := db.conn.Exec("DELETE FROM "+table+" WHERE node = ?;", name); err != nil {
			return fmt.Errorf("storage: delete %s for %q: %w", table, name, err)
		}
	}
	return nil
}

// ListPeers returns all known peers ordered by Name. LastSeen is the zero
// time.Time when the stored last_seen is 0.
func (db *DB) ListPeers() ([]Peer, error) {
	const q = `
SELECT name, url, secret, status, last_seen, last_error, enabled
FROM peer
ORDER BY name ASC;`
	rows, err := db.conn.Query(q)
	if err != nil {
		return nil, fmt.Errorf("storage: query peers: %w", err)
	}
	defer rows.Close()

	peers, err := scanPeers(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan peers: %w", err)
	}
	return peers, nil
}

// SetPeerStatus updates status and last_error for the named peer. If lastSeen is
// non-zero it also updates last_seen; if lastSeen is the zero time.Time, the
// stored last_seen is left UNCHANGED (so a failed poll preserves the last
// successful-contact time). No-op (nil error) if the peer does not exist.
func (db *DB) SetPeerStatus(name, status string, lastSeen time.Time, lastErr string) error {
	if lastSeen.IsZero() {
		const q = `
UPDATE peer SET status = ?, last_error = ?
WHERE name = ?;`
		if _, err := db.conn.Exec(q, status, lastErr, name); err != nil {
			return fmt.Errorf("storage: set peer status for %q: %w", name, err)
		}
		return nil
	}

	const q = `
UPDATE peer SET status = ?, last_seen = ?, last_error = ?
WHERE name = ?;`
	if _, err := db.conn.Exec(q, status, lastSeen.UTC().Unix(), lastErr, name); err != nil {
		return fmt.Errorf("storage: set peer status for %q: %w", name, err)
	}
	return nil
}

// SetPeerEnabled toggles a peer's muted state (enabled=false). No-op if the peer
// does not exist.
func (db *DB) SetPeerEnabled(name string, enabled bool) error {
	if _, err := db.conn.Exec(`UPDATE peer SET enabled = ? WHERE name = ?;`, boolToInt(enabled), name); err != nil {
		return fmt.Errorf("storage: set peer enabled for %q: %w", name, err)
	}
	return nil
}

// CommonSecret returns the most frequently used non-empty peer secret, or "" if
// no peer has a secret. It is the fallback used to authenticate to gossip-
// discovered peers — which arrive with no per-link secret — in the common case
// where a cluster shares one secret across all its links. Ties break on the
// lexicographically smaller secret so the result is deterministic.
func CommonSecret(peers []Peer) string {
	counts := make(map[string]int)
	for _, p := range peers {
		if p.Secret != "" {
			counts[p.Secret]++
		}
	}
	best, bestN := "", 0
	for s, n := range counts {
		if n > bestN || (n == bestN && s < best) {
			best, bestN = s, n
		}
	}
	return best
}

// scanPeers reads all rows into Peer values. Callers own closing rows.
func scanPeers(rows *sql.Rows) ([]Peer, error) {
	var out []Peer
	for rows.Next() {
		var (
			p        Peer
			lastSeen int64
			enabled  int64
		)
		if err := rows.Scan(
			&p.Name, &p.URL, &p.Secret, &p.Status, &lastSeen, &p.LastError, &enabled,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		if lastSeen != 0 {
			p.LastSeen = time.Unix(lastSeen, 0).UTC()
		}
		p.Enabled = enabled != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
