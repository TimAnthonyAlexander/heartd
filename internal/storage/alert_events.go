package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// AlertEvent is one persisted alert state transition: a row is written when a
// rule starts firing (state "firing") and another when it recovers (state
// "recovered"). Together they form the incident history shown in the dashboard.
// The node is the entity the alert concerns (the local node or a peer), so the
// history is queried the same way as every other per-node series.
type AlertEvent struct {
	ID         int64
	Node       string
	RuleID     string // the producing rule's id, as text ("" if none)
	RuleSource string // rule source (cpu|mem|disk|peer|nodata|...)
	Entity     string // mount / check / peer the rule targets ("" if n/a)
	Severity   string // warning | critical
	State      string // firing | recovered
	Subject    string // rule name, for display
	Detail     string // value-vs-threshold / check detail at the transition
	At         time.Time
}

// InsertAlertEvent persists one alert state transition.
func (db *DB) InsertAlertEvent(e AlertEvent) error {
	const q = `
INSERT INTO alert_event (node, rule_id, rule_source, entity, severity, state, subject, detail, at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`
	if _, err := db.conn.Exec(
		q,
		e.Node,
		e.RuleID,
		e.RuleSource,
		e.Entity,
		e.Severity,
		e.State,
		e.Subject,
		e.Detail,
		e.At.UTC().Unix(),
	); err != nil {
		return fmt.Errorf("storage: insert alert event for node %q: %w", e.Node, err)
	}
	return nil
}

// AlertEventHistory returns alert events for node with At >= since, ordered
// most-recent-first, capped at limit rows (limit <= 0 means no cap).
func (db *DB) AlertEventHistory(node string, since time.Time, limit int) ([]AlertEvent, error) {
	sinceUnix := since.UTC().Unix()

	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		const q = `
SELECT id, node, rule_id, rule_source, entity, severity, state, subject, detail, at
FROM alert_event
WHERE node = ? AND at >= ?
ORDER BY at DESC, id DESC
LIMIT ?;`
		rows, err = db.conn.Query(q, node, sinceUnix, limit)
	} else {
		const q = `
SELECT id, node, rule_id, rule_source, entity, severity, state, subject, detail, at
FROM alert_event
WHERE node = ? AND at >= ?
ORDER BY at DESC, id DESC;`
		rows, err = db.conn.Query(q, node, sinceUnix)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: query alert events for node %q: %w", node, err)
	}
	defer rows.Close()

	events, err := scanAlertEvents(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan alert events for node %q: %w", node, err)
	}
	return events, nil
}

// PruneAlertEvents deletes alert_event rows with At < before. Returns rows deleted.
func (db *DB) PruneAlertEvents(before time.Time) (int64, error) {
	const q = `DELETE FROM alert_event WHERE at < ?;`
	res, err := db.conn.Exec(q, before.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("storage: prune alert events before %s: %w", before.UTC(), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: prune alert events rows affected: %w", err)
	}
	return n, nil
}

// scanAlertEvents reads all rows into AlertEvent values. Callers own closing rows.
func scanAlertEvents(rows *sql.Rows) ([]AlertEvent, error) {
	var out []AlertEvent
	for rows.Next() {
		var (
			e      AlertEvent
			atUnix int64
		)
		if err := rows.Scan(
			&e.ID, &e.Node, &e.RuleID, &e.RuleSource, &e.Entity,
			&e.Severity, &e.State, &e.Subject, &e.Detail, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		e.At = time.Unix(atUnix, 0).UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
