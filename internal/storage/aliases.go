package storage

import "fmt"

// SetNodeAlias sets a display alias for a node (local or peer), keyed by the
// node's real name. The real name remains the identity key everywhere (data
// tables, peer protocol, routing); the alias is a UI-only label. Upserts.
func (db *DB) SetNodeAlias(node, alias string) error {
	const q = `
INSERT INTO node_alias (node, alias) VALUES (?, ?)
ON CONFLICT(node) DO UPDATE SET alias = excluded.alias;`
	if _, err := db.conn.Exec(q, node, alias); err != nil {
		return fmt.Errorf("storage: set node alias for %q: %w", node, err)
	}
	return nil
}

// DeleteNodeAlias removes any alias for a node, reverting the UI to its real
// name. No-op (nil error) when none is set.
func (db *DB) DeleteNodeAlias(node string) error {
	if _, err := db.conn.Exec(`DELETE FROM node_alias WHERE node = ?;`, node); err != nil {
		return fmt.Errorf("storage: delete node alias for %q: %w", node, err)
	}
	return nil
}

// NodeAliases returns all display aliases keyed by node real name. Nodes without
// an alias are simply absent from the map.
func (db *DB) NodeAliases() (map[string]string, error) {
	rows, err := db.conn.Query(`SELECT node, alias FROM node_alias;`)
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
