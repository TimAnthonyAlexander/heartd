package auth

import (
	"errors"
	"testing"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewService(db)
}

func TestInitializedLifecycle(t *testing.T) {
	s := newService(t)

	if init, err := s.Initialized(); err != nil || init {
		t.Fatalf("fresh service should not be initialized: init=%v err=%v", init, err)
	}

	user, token, err := s.CreateFirstUser("admin", "supersecret")
	if err != nil {
		t.Fatalf("CreateFirstUser: %v", err)
	}
	if user.Username != "admin" || token == "" {
		t.Fatalf("unexpected user/token: %+v %q", user, token)
	}

	if init, err := s.Initialized(); err != nil || !init {
		t.Fatalf("service should be initialized after first user: init=%v err=%v", init, err)
	}

	// The returned token must resolve to the user.
	got, ok, err := s.UserForSession(token)
	if err != nil || !ok || got.Username != "admin" {
		t.Fatalf("session should resolve to admin: %+v ok=%v err=%v", got, ok, err)
	}
}

func TestCreateFirstUserOnlyOnce(t *testing.T) {
	s := newService(t)
	if _, _, err := s.CreateFirstUser("admin", "supersecret"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, _, err := s.CreateFirstUser("second", "supersecret")
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("expected ErrAlreadyInitialized, got %v", err)
	}
}

func TestCreateFirstUserValidation(t *testing.T) {
	s := newService(t)
	if _, _, err := s.CreateFirstUser("admin", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("expected ErrWeakPassword, got %v", err)
	}
	if _, _, err := s.CreateFirstUser("   ", "supersecret"); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("expected ErrInvalidUsername, got %v", err)
	}
}

func TestLogin(t *testing.T) {
	s := newService(t)
	if _, _, err := s.CreateFirstUser("admin", "supersecret"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Wrong password.
	if _, _, err := s.Login("admin", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password should be ErrInvalidCredentials, got %v", err)
	}
	// Unknown user.
	if _, _, err := s.Login("ghost", "supersecret"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user should be ErrInvalidCredentials, got %v", err)
	}
	// Correct.
	user, token, err := s.Login("admin", "supersecret")
	if err != nil || user.Username != "admin" || token == "" {
		t.Fatalf("valid login failed: %+v %q %v", user, token, err)
	}
	if _, ok, _ := s.UserForSession(token); !ok {
		t.Error("login token should resolve to a session")
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	s := newService(t)
	_, token, err := s.CreateFirstUser("admin", "supersecret")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Logout(token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, ok, _ := s.UserForSession(token); ok {
		t.Error("session should be invalid after logout")
	}
}

func TestUserForSessionEmptyAndUnknown(t *testing.T) {
	s := newService(t)
	if _, ok, err := s.UserForSession(""); ok || err != nil {
		t.Errorf("empty token: ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.UserForSession("deadbeef"); ok || err != nil {
		t.Errorf("unknown token: ok=%v err=%v", ok, err)
	}
}
