package storage

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrCheckNotFound is returned by UpdateCheckConfig when no check matches the
// given ID.
var ErrCheckNotFound = errors.New("storage: check not found")

// CheckConfig is a persisted, user-configurable check definition.
type CheckConfig struct {
	ID          int64
	Name        string
	Type        string // http | tcp | process | shell
	IntervalSec int64
	TimeoutSec  int64
	URL         string
	Method      string
	Host        string
	Port        int
	Process     string
	Command     string
	Enabled     bool
}

// GetSetting returns the value for key, or ok=false if absent. Values are
// opaque strings; callers that need structure store JSON.
func (db *DB) GetSetting(key string) (string, bool, error) {
	const q = `SELECT value FROM setting WHERE key = ?;`
	var value string
	err := db.conn.QueryRow(q, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("storage: get setting %q: %w", key, err)
	}
	return value, true, nil
}

// SetSetting inserts or updates a key's value (upsert).
func (db *DB) SetSetting(key, value string) error {
	const q = `
INSERT INTO setting (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET
    value = excluded.value;`
	if _, err := db.conn.Exec(q, key, value); err != nil {
		return fmt.Errorf("storage: set setting %q: %w", key, err)
	}
	return nil
}

// ListCheckConfigs returns all configured checks ordered by id.
func (db *DB) ListCheckConfigs() ([]CheckConfig, error) {
	const q = `
SELECT id, name, type, interval_sec, timeout_sec, url, method, host, port, process, command, enabled
FROM check_config
ORDER BY id ASC;`
	rows, err := db.conn.Query(q)
	if err != nil {
		return nil, fmt.Errorf("storage: query check configs: %w", err)
	}
	defer rows.Close()

	configs, err := scanCheckConfigs(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan check configs: %w", err)
	}
	return configs, nil
}

// CountCheckConfigs returns the number of configured checks.
func (db *DB) CountCheckConfigs() (int, error) {
	const q = `SELECT COUNT(*) FROM check_config;`
	var n int
	if err := db.conn.QueryRow(q).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count check configs: %w", err)
	}
	return n, nil
}

// CreateCheckConfig inserts a check and returns it with its new ID.
func (db *DB) CreateCheckConfig(c CheckConfig) (CheckConfig, error) {
	const q = `
INSERT INTO check_config (name, type, interval_sec, timeout_sec, url, method, host, port, process, command, enabled)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`
	res, err := db.conn.Exec(
		q,
		c.Name,
		c.Type,
		c.IntervalSec,
		c.TimeoutSec,
		c.URL,
		c.Method,
		c.Host,
		c.Port,
		c.Process,
		c.Command,
		boolToInt(c.Enabled),
	)
	if err != nil {
		return CheckConfig{}, fmt.Errorf("storage: create check config %q: %w", c.Name, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return CheckConfig{}, fmt.Errorf("storage: create check config %q last insert id: %w", c.Name, err)
	}

	c.ID = id
	return c, nil
}

// UpdateCheckConfig updates the check with c.ID. It returns ErrCheckNotFound if
// no row matches.
func (db *DB) UpdateCheckConfig(c CheckConfig) error {
	const q = `
UPDATE check_config SET
    name         = ?,
    type         = ?,
    interval_sec = ?,
    timeout_sec  = ?,
    url          = ?,
    method       = ?,
    host         = ?,
    port         = ?,
    process      = ?,
    command      = ?,
    enabled      = ?
WHERE id = ?;`
	res, err := db.conn.Exec(
		q,
		c.Name,
		c.Type,
		c.IntervalSec,
		c.TimeoutSec,
		c.URL,
		c.Method,
		c.Host,
		c.Port,
		c.Process,
		c.Command,
		boolToInt(c.Enabled),
		c.ID,
	)
	if err != nil {
		return fmt.Errorf("storage: update check config %d: %w", c.ID, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("storage: update check config %d rows affected: %w", c.ID, err)
	}
	if n == 0 {
		return ErrCheckNotFound
	}
	return nil
}

// DeleteCheckConfig removes the check with the given id. It is not an error if
// no row matches.
func (db *DB) DeleteCheckConfig(id int64) error {
	const q = `DELETE FROM check_config WHERE id = ?;`
	if _, err := db.conn.Exec(q, id); err != nil {
		return fmt.Errorf("storage: delete check config %d: %w", id, err)
	}
	return nil
}

// scanCheckConfigs reads all rows into CheckConfig values. Callers own closing
// rows.
func scanCheckConfigs(rows *sql.Rows) ([]CheckConfig, error) {
	var out []CheckConfig
	for rows.Next() {
		var (
			c       CheckConfig
			enabled int64
		)
		if err := rows.Scan(
			&c.ID, &c.Name, &c.Type, &c.IntervalSec, &c.TimeoutSec,
			&c.URL, &c.Method, &c.Host, &c.Port, &c.Process, &c.Command, &enabled,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		c.Enabled = enabled != 0
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}

// boolToInt maps a bool to the INTEGER 0/1 representation used in storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
