package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrAlertRuleNotFound is returned by UpdateAlertRule when no rule matches the ID.
var ErrAlertRuleNotFound = errors.New("storage: alert rule not found")

// AlertRule is a persisted, user-configurable alert rule.
type AlertRule struct {
	ID           int64
	Name         string
	Enabled      bool
	Source       string  // cpu|mem|disk|check_status|check_latency|net_recv|net_sent|peer|nodata
	Entity       string  // mount, check name, or peer name ("*" = any); "" for cpu/mem/net
	Comparator   string  // >=|>|<=|<  (numeric sources; ignored for status sources)
	Threshold    float64 // numeric threshold (units depend on source)
	ForSec       int64   // sustained duration before firing
	RecoverGrace int64   // keep-firing-for after recovery (anti-flap)
	Severity     string  // warning|critical
	CreatedAt    time.Time
}

// ListAlertRules returns all rules ordered by id.
func (db *DB) ListAlertRules() ([]AlertRule, error) {
	const q = `
SELECT id, name, enabled, source, entity, comparator, threshold,
       for_seconds, recover_grace_seconds, severity, created_at
FROM alert_rule
ORDER BY id ASC;`
	rows, err := db.conn.Query(q)
	if err != nil {
		return nil, fmt.Errorf("storage: query alert rules: %w", err)
	}
	defer rows.Close()
	return scanAlertRules(rows)
}

// CountAlertRules returns the number of configured alert rules.
func (db *DB) CountAlertRules() (int, error) {
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM alert_rule;`).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count alert rules: %w", err)
	}
	return n, nil
}

// CreateAlertRule inserts a rule and returns it with its new ID.
func (db *DB) CreateAlertRule(r AlertRule) (AlertRule, error) {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	const q = `
INSERT INTO alert_rule (name, enabled, source, entity, comparator, threshold,
                        for_seconds, recover_grace_seconds, severity, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`
	res, err := db.conn.Exec(q,
		r.Name, boolToInt(r.Enabled), r.Source, r.Entity, r.Comparator, r.Threshold,
		r.ForSec, r.RecoverGrace, r.Severity, r.CreatedAt.UTC().Unix(),
	)
	if err != nil {
		return AlertRule{}, fmt.Errorf("storage: create alert rule %q: %w", r.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AlertRule{}, fmt.Errorf("storage: create alert rule %q last insert id: %w", r.Name, err)
	}
	r.ID = id
	return r, nil
}

// UpdateAlertRule updates the rule with r.ID. Returns ErrAlertRuleNotFound if no
// row matches. created_at is not modified.
func (db *DB) UpdateAlertRule(r AlertRule) error {
	const q = `
UPDATE alert_rule SET
    name                  = ?,
    enabled               = ?,
    source                = ?,
    entity                = ?,
    comparator            = ?,
    threshold             = ?,
    for_seconds           = ?,
    recover_grace_seconds = ?,
    severity              = ?
WHERE id = ?;`
	res, err := db.conn.Exec(q,
		r.Name, boolToInt(r.Enabled), r.Source, r.Entity, r.Comparator, r.Threshold,
		r.ForSec, r.RecoverGrace, r.Severity, r.ID,
	)
	if err != nil {
		return fmt.Errorf("storage: update alert rule %d: %w", r.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("storage: update alert rule %d rows affected: %w", r.ID, err)
	}
	if n == 0 {
		return ErrAlertRuleNotFound
	}
	return nil
}

// DeleteAlertRule removes the rule with the given id. Not an error if absent.
func (db *DB) DeleteAlertRule(id int64) error {
	if _, err := db.conn.Exec(`DELETE FROM alert_rule WHERE id = ?;`, id); err != nil {
		return fmt.Errorf("storage: delete alert rule %d: %w", id, err)
	}
	return nil
}

func scanAlertRules(rows *sql.Rows) ([]AlertRule, error) {
	var out []AlertRule
	for rows.Next() {
		var (
			r         AlertRule
			enabled   int64
			createdAt int64
		)
		if err := rows.Scan(
			&r.ID, &r.Name, &enabled, &r.Source, &r.Entity, &r.Comparator, &r.Threshold,
			&r.ForSec, &r.RecoverGrace, &r.Severity, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("storage: scan alert rule: %w", err)
		}
		r.Enabled = enabled != 0
		r.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate alert rules: %w", err)
	}
	return out, nil
}
