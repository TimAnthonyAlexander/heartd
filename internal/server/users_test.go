package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/timanthonyalexander/heartd/internal/settings"
)

// userServer builds a bare server with a seeded first admin and returns it.
func userServer(t *testing.T) *server {
	t.Helper()
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	if _, _, err := s.cfg.Auth.CreateFirstUser("admin", "password1"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return s
}

func postUser(t *testing.T, s *server, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleCreateUser(rec, req)
	return rec.Code
}

func TestCreateUserValidationAndList(t *testing.T) {
	s := userServer(t)

	if code := postUser(t, s, `{"username":"bob","password":"short"}`); code != http.StatusBadRequest {
		t.Fatalf("weak password = %d, want 400", code)
	}
	if code := postUser(t, s, `{"username":"bob","password":"longenough"}`); code != http.StatusOK {
		t.Fatalf("create = %d, want 200", code)
	}
	if code := postUser(t, s, `{"username":"bob","password":"longenough"}`); code != http.StatusConflict {
		t.Fatalf("duplicate = %d, want 409", code)
	}

	// List should now have admin + bob, and never leak a password field.
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec := httptest.NewRecorder()
	s.handleListUsers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	var users []userDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("user count = %d, want 2", len(users))
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("list leaked a password field: %s", rec.Body.String())
	}
}

func TestDeleteLastUserRejected(t *testing.T) {
	s := userServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/users/admin", nil)
	req.SetPathValue("username", "admin")
	rec := httptest.NewRecorder()
	s.handleDeleteUser(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete last user = %d, want 400", rec.Code)
	}
	// Admin must still exist.
	users, _ := s.cfg.Auth.ListUsers()
	if len(users) != 1 {
		t.Fatalf("user count = %d, want 1 (delete should have been refused)", len(users))
	}
}

func TestDeleteUserSucceedsWhenOthersExist(t *testing.T) {
	s := userServer(t)
	if _, err := s.cfg.Auth.CreateUser("bob", "longenough"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/users/bob", nil)
	req.SetPathValue("username", "bob")
	rec := httptest.NewRecorder()
	s.handleDeleteUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete bob = %d, want 200", rec.Code)
	}
	if _, ok, _ := s.cfg.DB.UserByUsername("bob"); ok {
		t.Fatal("bob still present after delete")
	}
}

func TestSetPasswordTakesEffect(t *testing.T) {
	s := userServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/users/admin/password", strings.NewReader(`{"password":"newpassword"}`))
	req.SetPathValue("username", "admin")
	rec := httptest.NewRecorder()
	s.handleSetUserPassword(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set password = %d, body %s", rec.Code, rec.Body.String())
	}

	if _, _, err := s.cfg.Auth.Login("admin", "newpassword"); err != nil {
		t.Fatalf("login with new password failed: %v", err)
	}
	if _, _, err := s.cfg.Auth.Login("admin", "password1"); err == nil {
		t.Fatal("login with old password should fail")
	}
}
