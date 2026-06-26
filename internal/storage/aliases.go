package storage

import "fmt"

// A node's effective display name resolves in two layers, both keyed by the
// node's real name (the identity key everywhere — data tables, peer protocol,
// routing). The real name is never replaced; the alias is a UI-only label:
//
//   - alias            — the LOCAL override, set on this dashboard. Wins.
//   - advertised_alias — the name PROPAGATED from the node itself (its own
//     configured display name), learned by the poller. Used when no local
//     override is set.
//
// Precedence: local override → advertised name → (caller falls back to the real
// name when both are empty, which NodeAliases signals by omitting the row).

// SetNodeAlias sets the LOCAL display override for a node (local or peer). It
// touches only the alias column, leaving any propagated (advertised) name
// intact. Upserts.
func (db *DB) SetNodeAlias(node, alias string) error {
	const q = `
INSERT INTO node_alias (node, alias, advertised_alias) VALUES (?, ?, '')
ON CONFLICT(node) DO UPDATE SET alias = excluded.alias;`
	if _, err := db.conn.Exec(q, node, alias); err != nil {
		return fmt.Errorf("storage: set node alias for %q: %w", node, err)
	}
	return nil
}

// SetAdvertisedAlias sets the PROPAGATED display name for a node, learned from
// the node itself. It touches only the advertised column, never the local
// override. An empty alias clears the advertised name (set to ''), which is how
// the poller revokes a name a peer stopped advertising. Upserts.
func (db *DB) SetAdvertisedAlias(node, alias string) error {
	const q = `
INSERT INTO node_alias (node, alias, advertised_alias) VALUES (?, '', ?)
ON CONFLICT(node) DO UPDATE SET advertised_alias = excluded.advertised_alias;`
	if _, err := db.conn.Exec(q, node, alias); err != nil {
		return fmt.Errorf("storage: set advertised alias for %q: %w", node, err)
	}
	return nil
}

// DeleteNodeAlias clears the LOCAL display override for a node, reverting this
// dashboard to the propagated name (if any) or the real name. It only blanks the
// alias column so a propagated name survives the clear; a row left fully empty is
// then deleted to keep the table tidy. No-op (nil error) when none is set.
func (db *DB) DeleteNodeAlias(node string) error {
	if _, err := db.conn.Exec(`UPDATE node_alias SET alias = '' WHERE node = ?;`, node); err != nil {
		return fmt.Errorf("storage: clear node alias for %q: %w", node, err)
	}
	if _, err := db.conn.Exec(
		`DELETE FROM node_alias WHERE node = ? AND alias = '' AND advertised_alias = '';`, node,
	); err != nil {
		return fmt.Errorf("storage: prune empty node alias for %q: %w", node, err)
	}
	return nil
}

// NodeAliases returns each node's EFFECTIVE display name keyed by real name: the
// local override when set, else the propagated (advertised) name. Nodes with
// neither are omitted from the map.
func (db *DB) NodeAliases() (map[string]string, error) {
	rows, err := db.conn.Query(`SELECT node, alias, advertised_alias FROM node_alias;`)
	if err != nil {
		return nil, fmt.Errorf("storage: query node aliases: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var node, alias, advertised string
		if err := rows.Scan(&node, &alias, &advertised); err != nil {
			return nil, fmt.Errorf("storage: scan node alias: %w", err)
		}
		effective := alias
		if effective == "" {
			effective = advertised
		}
		if effective == "" {
			continue
		}
		out[node] = effective
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate node aliases: %w", err)
	}
	return out, nil
}
