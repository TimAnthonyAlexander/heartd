package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/timanthonyalexander/heartd/internal/auth"
	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/settings"
)

func TestHeadlessServesOnlyPeerAndHealth(t *testing.T) {
	db := testDB(t)
	set := settings.New(db)
	if err := set.Load(config.Default()); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	srv := httptest.NewServer(New(Config{
		NodeName: "agent", DB: db, Settings: set, Auth: auth.NewService(db),
		Headless: true, ExtraSecrets: []string{"sek"},
	}))
	defer srv.Close()

	get := func(path, secret string) int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if secret != "" {
			req.Header.Set("X-Heartd-Secret", secret)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	// Health + the headless landing page are public.
	if c := get("/api/health", ""); c != 200 {
		t.Errorf("/api/health = %d, want 200", c)
	}
	if c := get("/", ""); c != 200 {
		t.Errorf("/ = %d, want 200 (agent note)", c)
	}

	// Dashboard / auth / management endpoints are NOT registered → 404.
	for _, p := range []string{"/api/auth/status", "/api/nodes", "/api/users", "/api/peers"} {
		if c := get(p, ""); c != 404 {
			t.Errorf("%s = %d, want 404 in headless mode", p, c)
		}
	}

	// The peer API IS served, gated by the shared secret.
	if c := get("/api/peer/settings", ""); c != 403 {
		t.Errorf("/api/peer/settings without secret = %d, want 403", c)
	}
	if c := get("/api/peer/settings", "sek"); c != 200 {
		t.Errorf("/api/peer/settings with secret = %d, want 200", c)
	}
}
