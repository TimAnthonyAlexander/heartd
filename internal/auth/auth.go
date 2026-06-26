// Package auth implements user authentication for heartd: first-run admin
// creation, password login, and session management backed by storage.
//
// Every user is an admin (authentication == authorization). Passwords are
// hashed with bcrypt; sessions are opaque random tokens stored in the database
// and carried in an HttpOnly cookie.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

const (
	// SessionTTL is how long a login session stays valid.
	SessionTTL = 30 * 24 * time.Hour
	// MinPasswordLen is the minimum acceptable password length.
	MinPasswordLen = 8
	// MaxUsernameLen bounds the username to keep things sane.
	MaxUsernameLen = 64
	tokenBytes     = 32
)

// Sentinel errors. Login failures collapse to ErrInvalidCredentials so the API
// does not reveal whether a username exists.
var (
	ErrAlreadyInitialized = errors.New("auth: already initialized")
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrWeakPassword       = errors.New("auth: password too short")
	ErrInvalidUsername    = errors.New("auth: invalid username")
	ErrUsernameTaken      = storage.ErrUsernameTaken
	// ErrUserNotFound is returned when an admin operation targets a username that
	// does not exist.
	ErrUserNotFound = errors.New("auth: user not found")
	// ErrLastUser is returned when deleting a user would leave zero users, which
	// would drop the system back into the open first-run admin-creation flow.
	ErrLastUser = errors.New("auth: cannot delete the last user")
)

// User is the public view of an authenticated user (no password material).
type User struct {
	ID       int64  `json:"-"`
	Username string `json:"username"`
}

// dummyHash is a valid bcrypt hash compared against when a username is not found,
// so login timing does not reveal whether the user exists.
var dummyHash = mustHash("heartd-dummy-password")

// Service provides authentication operations over a storage.DB.
type Service struct {
	db  *storage.DB
	ttl time.Duration
}

// NewService builds an auth Service.
func NewService(db *storage.DB) *Service {
	return &Service{db: db, ttl: SessionTTL}
}

// Initialized reports whether at least one user exists (first-run is complete).
func (s *Service) Initialized() (bool, error) {
	n, err := s.db.UserCount()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateFirstUser creates the initial admin account. It fails with
// ErrAlreadyInitialized if any user already exists. On success it returns the
// user and a fresh session token.
func (s *Service) CreateFirstUser(username, password string) (User, string, error) {
	initialized, err := s.Initialized()
	if err != nil {
		return User{}, "", err
	}
	if initialized {
		return User{}, "", ErrAlreadyInitialized
	}
	return s.createUserAndSession(username, password)
}

// Login verifies credentials and returns the user with a fresh session token.
// Any failure (unknown user or bad password) returns ErrInvalidCredentials.
func (s *Service) Login(username, password string) (User, string, error) {
	username = strings.TrimSpace(username)

	stored, ok, err := s.db.UserByUsername(username)
	if err != nil {
		return User{}, "", err
	}
	if !ok {
		// Compare against a dummy hash to equalize timing, then fail.
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return User{}, "", ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(password)) != nil {
		return User{}, "", ErrInvalidCredentials
	}

	token, err := s.newSession(stored.ID)
	if err != nil {
		return User{}, "", err
	}
	return User{ID: stored.ID, Username: stored.Username}, token, nil
}

// Logout invalidates a session token.
func (s *Service) Logout(token string) error {
	if token == "" {
		return nil
	}
	return s.db.DeleteSession(token)
}

// UserForSession returns the user owning a valid, non-expired session.
func (s *Service) UserForSession(token string) (User, bool, error) {
	if token == "" {
		return User{}, false, nil
	}
	stored, ok, err := s.db.SessionUser(token)
	if err != nil || !ok {
		return User{}, false, err
	}
	return User{ID: stored.ID, Username: stored.Username}, true, nil
}

// PruneExpired removes expired sessions; intended to run periodically.
func (s *Service) PruneExpired() error {
	_, err := s.db.PruneSessions(time.Now().UTC())
	return err
}

// CreateUser creates a new account WITHOUT logging anyone in (used by admins to
// add other users). Validates the username and password, then stores a bcrypt
// hash. Returns ErrUsernameTaken if the username already exists.
func (s *Service) CreateUser(username, password string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > MaxUsernameLen {
		return User{}, ErrInvalidUsername
	}
	if len(password) < MinPasswordLen {
		return User{}, ErrWeakPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	stored, err := s.db.CreateUser(username, string(hash))
	if err != nil {
		return User{}, err
	}
	return User{ID: stored.ID, Username: stored.Username}, nil
}

// ListUsers returns all accounts (no password material), ordered by username.
func (s *Service) ListUsers() ([]User, error) {
	stored, err := s.db.ListUsers()
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(stored))
	for _, u := range stored {
		out = append(out, User{ID: u.ID, Username: u.Username})
	}
	return out, nil
}

// DeleteUser removes the named account and all of its sessions. It refuses to
// delete the last remaining user (ErrLastUser) so the system can never fall back
// to the open first-run admin-creation flow. Returns ErrUserNotFound if unknown.
func (s *Service) DeleteUser(username string) error {
	username = strings.TrimSpace(username)
	stored, ok, err := s.db.UserByUsername(username)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUserNotFound
	}

	count, err := s.db.UserCount()
	if err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastUser
	}

	if err := s.db.DeleteUser(stored.ID); err != nil {
		return err
	}
	return s.db.DeleteSessionsForUser(stored.ID)
}

// SetPassword changes the named user's password after validating its length.
// Existing sessions are left intact. Returns ErrUserNotFound if unknown.
func (s *Service) SetPassword(username, newPassword string) error {
	username = strings.TrimSpace(username)
	stored, ok, err := s.db.UserByUsername(username)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUserNotFound
	}
	if len(newPassword) < MinPasswordLen {
		return ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.UpdateUserPassword(stored.ID, string(hash))
}

func (s *Service) createUserAndSession(username, password string) (User, string, error) {
	user, err := s.CreateUser(username, password)
	if err != nil {
		return User{}, "", err
	}
	token, err := s.newSession(user.ID)
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

func (s *Service) newSession(userID int64) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if err := s.db.CreateSession(token, userID, now.Add(s.ttl)); err != nil {
		return "", err
	}
	return token, nil
}

func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func mustHash(pw string) string {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(h)
}
