package server

import (
	"errors"
	"net/http"

	"github.com/timanthonyalexander/heartd/internal/auth"
)

// userDTO is the dashboard's view of an account (no password material).
type userDTO struct {
	Username string `json:"username"`
	Self     bool   `json:"self"`
}

func (s *server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.cfg.Auth.ListUsers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	current, _ := s.currentUser(r)
	out := make([]userDTO, 0, len(users))
	for _, u := range users {
		out = append(out, userDTO{Username: u.Username, Self: u.Username == current.Username})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}
	user, err := s.cfg.Auth.CreateUser(creds.Username, creds.Password)
	if err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": user.Username})
}

func (s *server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if err := s.cfg.Auth.DeleteUser(username); err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := s.cfg.Auth.SetPassword(r.PathValue("username"), body.Password); err != nil {
		writeUserError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeUserError maps auth errors to appropriate HTTP statuses with a
// user-facing message.
func writeUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrUsernameTaken):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username is already taken"})
	case errors.Is(err, auth.ErrInvalidUsername):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
	case errors.Is(err, auth.ErrWeakPassword):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
	case errors.Is(err, auth.ErrUserNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
	case errors.Is(err, auth.ErrLastUser):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete the last user"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "operation failed"})
	}
}
