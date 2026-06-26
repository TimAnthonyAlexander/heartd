package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrUsernameTaken is returned by CreateUser when the username already exists.
var ErrUsernameTaken = errors.New("storage: username already taken")

// User is a persisted account that can authenticate.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// CreateUser inserts a new user. It returns ErrUsernameTaken if the username
// already exists.
func (db *DB) CreateUser(username, passwordHash string) (User, error) {
	createdAt := time.Now().UTC()
	const q = `
INSERT INTO user (username, password_hash, created_at)
VALUES (?, ?, ?);`
	res, err := db.conn.Exec(q, username, passwordHash, createdAt.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrUsernameTaken
		}
		return User{}, fmt.Errorf("storage: create user %q: %w", username, err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("storage: create user %q last insert id: %w", username, err)
	}

	return User{
		ID:           id,
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    time.Unix(createdAt.Unix(), 0).UTC(),
	}, nil
}

// UserByUsername returns the user with the given username, or ok=false if none.
func (db *DB) UserByUsername(username string) (User, bool, error) {
	const q = `
SELECT id, username, password_hash, created_at
FROM user
WHERE username = ?;`

	var (
		u             User
		createdAtUnix int64
	)
	err := db.conn.QueryRow(q, username).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &createdAtUnix,
	)
	if err == sql.ErrNoRows {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("storage: user by username %q: %w", username, err)
	}

	u.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
	return u, true, nil
}

// ListUsers returns all users ordered by username. PasswordHash is not
// populated (it is never needed by callers that list users).
func (db *DB) ListUsers() ([]User, error) {
	const q = `
SELECT id, username, created_at
FROM user
ORDER BY username ASC;`
	rows, err := db.conn.Query(q)
	if err != nil {
		return nil, fmt.Errorf("storage: list users: %w", err)
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var (
			u             User
			createdAtUnix int64
		)
		if err := rows.Scan(&u.ID, &u.Username, &createdAtUnix); err != nil {
			return nil, fmt.Errorf("storage: scan user: %w", err)
		}
		u.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate users: %w", err)
	}
	return out, nil
}

// DeleteUser removes a user by id. Sessions reference the user, so callers
// should also call DeleteSessionsForUser to avoid orphaned sessions.
func (db *DB) DeleteUser(id int64) error {
	if _, err := db.conn.Exec(`DELETE FROM user WHERE id = ?;`, id); err != nil {
		return fmt.Errorf("storage: delete user %d: %w", id, err)
	}
	return nil
}

// DeleteSessionsForUser removes all sessions belonging to a user (used when the
// user is deleted, or to force re-login).
func (db *DB) DeleteSessionsForUser(id int64) error {
	if _, err := db.conn.Exec(`DELETE FROM session WHERE user_id = ?;`, id); err != nil {
		return fmt.Errorf("storage: delete sessions for user %d: %w", id, err)
	}
	return nil
}

// UpdateUserPassword sets a user's password hash.
func (db *DB) UpdateUserPassword(id int64, passwordHash string) error {
	if _, err := db.conn.Exec(`UPDATE user SET password_hash = ? WHERE id = ?;`, passwordHash, id); err != nil {
		return fmt.Errorf("storage: update password for user %d: %w", id, err)
	}
	return nil
}

// UserCount returns the number of users (used to detect first-run /
// "initialized").
func (db *DB) UserCount() (int, error) {
	const q = `SELECT COUNT(*) FROM user;`
	var n int
	if err := db.conn.QueryRow(q).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: user count: %w", err)
	}
	return n, nil
}

// CreateSession stores a session token for a user with an expiry.
func (db *DB) CreateSession(token string, userID int64, expiresAt time.Time) error {
	const q = `
INSERT INTO session (token, user_id, created_at, expires_at)
VALUES (?, ?, ?, ?);`
	if _, err := db.conn.Exec(
		q,
		token,
		userID,
		time.Now().UTC().Unix(),
		expiresAt.UTC().Unix(),
	); err != nil {
		return fmt.Errorf("storage: create session for user %d: %w", userID, err)
	}
	return nil
}

// SessionUser returns the user owning a non-expired session for token, or
// ok=false if the token is unknown or expired.
func (db *DB) SessionUser(token string) (User, bool, error) {
	const q = `
SELECT u.id, u.username, u.password_hash, u.created_at
FROM session s
JOIN user u ON u.id = s.user_id
WHERE s.token = ? AND s.expires_at > ?;`

	var (
		u             User
		createdAtUnix int64
	)
	err := db.conn.QueryRow(q, token, time.Now().UTC().Unix()).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &createdAtUnix,
	)
	if err == sql.ErrNoRows {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("storage: session user: %w", err)
	}

	u.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
	return u, true, nil
}

// DeleteSession removes a session (logout). It is not an error if the token
// does not exist.
func (db *DB) DeleteSession(token string) error {
	const q = `DELETE FROM session WHERE token = ?;`
	if _, err := db.conn.Exec(q, token); err != nil {
		return fmt.Errorf("storage: delete session: %w", err)
	}
	return nil
}

// PruneSessions deletes sessions with expires_at < now. Returns rows deleted.
func (db *DB) PruneSessions(now time.Time) (int64, error) {
	const q = `DELETE FROM session WHERE expires_at < ?;`
	res, err := db.conn.Exec(q, now.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("storage: prune sessions before %s: %w", now.UTC(), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: prune sessions rows affected: %w", err)
	}
	return n, nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure.
// modernc/sqlite surfaces these as messages containing "UNIQUE constraint
// failed".
func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
