package storage

import "fmt"

// A node's display name ("alias") is a single value keyed by the node's real
// name (the identity key everywhere — data tables, peer protocol, routing). The
// real name is never replaced; the alias is just the label shown in the UI.
//
// There is exactly ONE name per node, and it is owned by that node. A node sets
// its own row (via a local rename, a remote rename pushed over the peer link, or
// the config display_name seeded on first run). Every other node learns that
// name by polling the node's /api/peer/identity and caching it in its own row
// for that node. So a rename made anywhere converges to the same label on every
// dashboard — there is no per-dashboard override to diverge.
//
// The legacy advertised_alias column still exists in the table for backward
// compatibility but is unused; new writes only touch alias and let the column
// keep its '' default.

// SetNodeAlias sets a node's display name. Upserts. Used both when a node names
// itself and when the poller caches a peer's self-advertised name.
func (db *DB) SetNodeAlias(node, alias string) error {
	const q = `
INSERT INTO node_alias (node, alias) VALUES (?, ?)
ON CONFLICT(node) DO UPDATE SET alias = excluded.alias;`
	if _, err := db.conn.Exec(q, node, alias); err != nil {
		return fmt.Errorf("storage: set node alias for %q: %w", node, err)
	}
	return nil
}

// DeleteNodeAlias clears a node's display name, reverting the UI to its real
// name. No-op (nil error) when none is set.
func (db *DB) DeleteNodeAlias(node string) error {
	if _, err := db.conn.Exec(`DELETE FROM node_alias WHERE node = ?;`, node); err != nil {
		return fmt.Errorf("storage: delete node alias for %q: %w", node, err)
	}
	return nil
}

// NodeAliases returns each node's display name keyed by real name. Nodes without
// a name are omitted from the map.
func (db *DB) NodeAliases() (map[string]string, error) {
	rows, err := db.conn.Query(`SELECT node, alias FROM node_alias WHERE alias != '';`)
	if err != nil {
		return nil, fmt.Errorf("storage: query node aliases: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var node, alias string
		if err := rows.Scan(&node, &alias); err != nil {
			return nil, fmt.Errorf("storage: scan node alias: %w", err)
		}
		out[node] = alias
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate node aliases: %w", err)
	}
	return out, nil
}
