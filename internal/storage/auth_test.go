package storage

import (
	"errors"
	"testing"
	"time"
)

func TestCreateUserAndUserByUsername(t *testing.T) {
	db := openTestDB(t)

	if n, err := db.UserCount(); err != nil || n != 0 {
		t.Fatalf("UserCount on empty: got %d err=%v, want 0/nil", n, err)
	}

	u, err := db.CreateUser("alice", "hash-abc")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == 0 || u.Username != "alice" || u.PasswordHash != "hash-abc" {
		t.Fatalf("CreateUser returned wrong user: %+v", u)
	}
	if u.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt not UTC: %v", u.CreatedAt.Location())
	}

	if n, err := db.UserCount(); err != nil || n != 1 {
		t.Fatalf("UserCount after create: got %d err=%v, want 1/nil", n, err)
	}

	got, ok, err := db.UserByUsername("alice")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if !ok {
		t.Fatalf("UserByUsername: ok=false, want true")
	}
	if got.ID != u.ID || got.Username != "alice" || got.PasswordHash != "hash-abc" {
		t.Fatalf("UserByUsername returned wrong user: %+v", got)
	}
	if !got.CreatedAt.Equal(u.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, u.CreatedAt)
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.CreateUser("bob", "hash1"); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	_, err := db.CreateUser("bob", "hash2")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate CreateUser: err=%v, want ErrUsernameTaken", err)
	}

	if n, err := db.UserCount(); err != nil || n != 1 {
		t.Fatalf("UserCount after duplicate: got %d err=%v, want 1/nil", n, err)
	}
}

func TestUserByUsernameUnknown(t *testing.T) {
	db := openTestDB(t)

	got, ok, err := db.UserByUsername("nobody")
	if err != nil {
		t.Fatalf("UserByUsername unknown: err=%v, want nil", err)
	}
	if ok {
		t.Fatalf("UserByUsername unknown: ok=true, want false")
	}
	if got != (User{}) {
		t.Fatalf("UserByUsername unknown: got %+v, want zero User", got)
	}
}

func TestSessionUserUnknown(t *testing.T) {
	db := openTestDB(t)

	_, ok, err := db.SessionUser("no-such-token")
	if err != nil {
		t.Fatalf("SessionUser unknown: err=%v, want nil", err)
	}
	if ok {
		t.Fatalf("SessionUser unknown: ok=true, want false")
	}
}

func TestCreateSessionAndSessionUser(t *testing.T) {
	db := openTestDB(t)

	u, err := db.CreateUser("carol", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	expires := time.Now().Add(time.Hour)
	if err := db.CreateSession("tok-valid", u.ID, expires); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, ok, err := db.SessionUser("tok-valid")
	if err != nil {
		t.Fatalf("SessionUser: %v", err)
	}
	if !ok {
		t.Fatalf("SessionUser: ok=false, want true")
	}
	if got.ID != u.ID || got.Username != "carol" || got.PasswordHash != "hash" {
		t.Fatalf("SessionUser returned wrong user: %+v", got)
	}
}

func TestSessionUserExpired(t *testing.T) {
	db := openTestDB(t)

	u, err := db.CreateUser("dave", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := db.CreateSession("tok-expired", u.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, ok, err := db.SessionUser("tok-expired")
	if err != nil {
		t.Fatalf("SessionUser expired: err=%v, want nil", err)
	}
	if ok {
		t.Fatalf("SessionUser expired: ok=true, want false")
	}
}

func TestDeleteSession(t *testing.T) {
	db := openTestDB(t)

	u, err := db.CreateUser("erin", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := db.CreateSession("tok", u.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := db.DeleteSession("tok"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	_, ok, err := db.SessionUser("tok")
	if err != nil {
		t.Fatalf("SessionUser after delete: %v", err)
	}
	if ok {
		t.Fatalf("SessionUser after delete: ok=true, want false")
	}

	// Deleting a missing token is not an error.
	if err := db.DeleteSession("does-not-exist"); err != nil {
		t.Fatalf("DeleteSession on missing token: %v", err)
	}
}

func TestPruneSessions(t *testing.T) {
	db := openTestDB(t)

	u, err := db.CreateUser("frank", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	now := time.Now()
	if err := db.CreateSession("expired-1", u.ID, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("CreateSession expired-1: %v", err)
	}
	if err := db.CreateSession("expired-2", u.ID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("CreateSession expired-2: %v", err)
	}
	if err := db.CreateSession("valid", u.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession valid: %v", err)
	}

	deleted, err := db.PruneSessions(now)
	if err != nil {
		t.Fatalf("PruneSessions: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("PruneSessions deleted = %d, want 2", deleted)
	}

	// The valid session survives.
	if _, ok, err := db.SessionUser("valid"); err != nil || !ok {
		t.Fatalf("valid session after prune: ok=%v err=%v, want true/nil", ok, err)
	}
}

func TestUserCreatedAtRoundTrip(t *testing.T) {
	db := openTestDB(t)

	before := time.Now()
	u, err := db.CreateUser("grace", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if diff := u.CreatedAt.Sub(before.UTC()); diff > time.Second || diff < -time.Second {
		t.Fatalf("CreatedAt round-trip diff = %v, want within 1s", diff)
	}

	got, _, err := db.UserByUsername("grace")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if !got.CreatedAt.Equal(u.CreatedAt) {
		t.Fatalf("CreatedAt mismatch: %v vs %v", got.CreatedAt, u.CreatedAt)
	}
}
