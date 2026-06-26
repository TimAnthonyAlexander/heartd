package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// setAlias drives handleSetNodeAlias for the given node name and JSON body.
func setAlias(t *testing.T, s *server, name, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/nodes/"+name+"/alias", strings.NewReader(body))
	req.SetPathValue("name", name)
	rec := httptest.NewRecorder()
	s.handleSetNodeAlias(rec, req)
	return rec
}

func TestSetAliasLocalNode(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))

	if rec := setAlias(t, s, "web-01", `{"alias":"Production web"}`); rec.Code != http.StatusOK {
		t.Fatalf("set local alias = %d, body %s", rec.Code, rec.Body.String())
	}
	aliases, _ := db.NodeAliases()
	if got := aliases["web-01"]; got != "Production web" {
		t.Fatalf("alias = %q, want %q", got, "Production web")
	}
}

func TestSetAliasPeerNode(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	if err := db.UpsertPeer(storage.Peer{Name: "db-01", URL: "http://db-01:9300", Secret: "s"}); err != nil {
		t.Fatal(err)
	}

	if rec := setAlias(t, s, "db-01", `{"alias":"Primary DB"}`); rec.Code != http.StatusOK {
		t.Fatalf("set peer alias = %d, body %s", rec.Code, rec.Body.String())
	}
	aliases, _ := db.NodeAliases()
	if got := aliases["db-01"]; got != "Primary DB" {
		t.Fatalf("alias = %q, want %q", got, "Primary DB")
	}
}

func TestSetAliasUnknownNode(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	if rec := setAlias(t, s, "ghost", `{"alias":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("alias for unknown node = %d, want 404", rec.Code)
	}
}

func TestSetAliasBlankClears(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	if err := db.SetNodeAlias("web-01", "old"); err != nil {
		t.Fatal(err)
	}

	if rec := setAlias(t, s, "web-01", `{"alias":"  "}`); rec.Code != http.StatusOK {
		t.Fatalf("clear alias = %d, body %s", rec.Code, rec.Body.String())
	}
	aliases, _ := db.NodeAliases()
	if _, ok := aliases["web-01"]; ok {
		t.Fatalf("alias should be cleared, got %v", aliases)
	}
}

// An alias equal to the real name is a no-op clear, so the API stays idempotent
// and we never store a redundant alias.
func TestSetAliasEqualToNameClears(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	if err := db.SetNodeAlias("web-01", "old"); err != nil {
		t.Fatal(err)
	}

	if rec := setAlias(t, s, "web-01", `{"alias":"web-01"}`); rec.Code != http.StatusOK {
		t.Fatalf("alias==name = %d, body %s", rec.Code, rec.Body.String())
	}
	aliases, _ := db.NodeAliases()
	if _, ok := aliases["web-01"]; ok {
		t.Fatalf("alias should be cleared when equal to name, got %v", aliases)
	}
}

func TestSetAliasTooLong(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	long := strings.Repeat("a", 65)
	if rec := setAlias(t, s, "web-01", `{"alias":"`+long+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("over-long alias = %d, want 400", rec.Code)
	}
}

// handleNodes must surface stored aliases for both the local node and peers.
func TestHandleNodesIncludesAlias(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	if err := db.UpsertPeer(storage.Peer{Name: "db-01", URL: "http://db-01:9300", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetNodeAlias("web-01", "HQ"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetNodeAlias("db-01", "Primary DB"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	rec := httptest.NewRecorder()
	s.handleNodes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nodes = %d, body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"alias":"HQ"`) {
		t.Errorf("local alias missing: %s", body)
	}
	if !strings.Contains(body, `"alias":"Primary DB"`) {
		t.Errorf("peer alias missing: %s", body)
	}
}

// Deleting a peer must also drop its alias so a future node reusing the name
// doesn't inherit a stale label.
func TestDeletePeerClearsAlias(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	if err := db.UpsertPeer(storage.Peer{Name: "db-01", URL: "http://db-01:9300", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetNodeAlias("db-01", "Primary DB"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/peers/db-01", nil)
	req.SetPathValue("name", "db-01")
	rec := httptest.NewRecorder()
	s.handleDeletePeer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, body %s", rec.Code, rec.Body.String())
	}
	aliases, _ := db.NodeAliases()
	if _, ok := aliases["db-01"]; ok {
		t.Fatalf("alias should be removed with the peer, got %v", aliases)
	}
}
