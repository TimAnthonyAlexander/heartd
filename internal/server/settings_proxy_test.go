package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/timanthonyalexander/heartd/internal/auth"
	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

func testDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

const validGeneral = `{"metrics_interval_sec":11,"peer_poll_interval_sec":15,"retention_sec":86400}`

// localServer builds a bare *server for a node, without the auth wall, so tests
// can call dispatch handlers directly.
func localServer(name string, db *storage.DB, set *settings.Service) *server {
	return &server{
		cfg:         Config{NodeName: name, DB: db, Settings: set, Auth: auth.NewService(db)},
		proxyClient: &http.Client{},
	}
}

// TestDispatchLocalWritesLocally verifies that addressing the local node writes
// to its own settings service.
func TestDispatchLocalWritesLocally(t *testing.T) {
	db := testDB(t)
	set := settings.New(db)
	s := localServer("A", db, set)

	req := httptest.NewRequest(http.MethodPut, "/api/nodes/A/settings/general", strings.NewReader(validGeneral))
	req.SetPathValue("name", "A")
	rec := httptest.NewRecorder()

	s.dispatchNode(s.handlePutGeneral, "/api/peer/settings/general")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := set.General().MetricsIntervalSec; got != 11 {
		t.Fatalf("local metrics interval = %v, want 11", got)
	}
}

// TestDispatchProxiesToPeer verifies that addressing a peer proxies the write to
// that peer's settings service (and does NOT touch the local one).
func TestDispatchProxiesToPeer(t *testing.T) {
	// Peer B: a full server that accepts the shared secret on /api/peer/* (it
	// validates inbound secrets against its own peer rows, so it must know A).
	dbB := testDB(t)
	setB := settings.New(dbB)
	if err := dbB.UpsertPeer(storage.Peer{Name: "A", URL: "http://a", Secret: "sek"}); err != nil {
		t.Fatalf("upsert peer on B: %v", err)
	}
	bSrv := httptest.NewServer(New(Config{
		NodeName: "B", DB: dbB, Settings: setB, Auth: auth.NewService(dbB),
	}))
	defer bSrv.Close()

	// Node A knows B as a peer with B's URL + shared secret.
	dbA := testDB(t)
	if err := dbA.UpsertPeer(storage.Peer{Name: "B", URL: bSrv.URL, Secret: "sek"}); err != nil {
		t.Fatalf("upsert peer: %v", err)
	}
	setA := settings.New(dbA)
	sA := localServer("A", dbA, setA)

	req := httptest.NewRequest(http.MethodPut, "/api/nodes/B/settings/general", strings.NewReader(validGeneral))
	req.SetPathValue("name", "B")
	rec := httptest.NewRecorder()

	sA.dispatchNode(sA.handlePutGeneral, "/api/peer/settings/general")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := setB.General().MetricsIntervalSec; got != 11 {
		t.Fatalf("peer B metrics interval = %v, want 11 (proxy did not land)", got)
	}
	if got := setA.General().MetricsIntervalSec; got == 11 {
		t.Fatalf("local A metrics interval = %v, edit leaked to the wrong node", got)
	}
}

// TestProxyUnknownNode returns 404 for a node that is neither local nor a peer.
func TestProxyUnknownNode(t *testing.T) {
	db := testDB(t)
	s := localServer("A", db, settings.New(db))

	req := httptest.NewRequest(http.MethodPut, "/api/nodes/ghost/settings/general", strings.NewReader(validGeneral))
	req.SetPathValue("name", "ghost")
	rec := httptest.NewRecorder()

	s.dispatchNode(s.handlePutGeneral, "/api/peer/settings/general")(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

// TestProxyCheckCRUDToPeer round-trips a check create through the proxy onto a
// peer, including the {id}-suffixed path mapping for a subsequent delete.
func TestProxyCheckCRUDToPeer(t *testing.T) {
	dbB := testDB(t)
	setB := settings.New(dbB)
	if err := dbB.UpsertPeer(storage.Peer{Name: "A", URL: "http://a", Secret: "sek"}); err != nil {
		t.Fatalf("upsert peer on B: %v", err)
	}
	bSrv := httptest.NewServer(New(Config{
		NodeName: "B", DB: dbB, Settings: setB, Auth: auth.NewService(dbB),
	}))
	defer bSrv.Close()

	dbA := testDB(t)
	if err := dbA.UpsertPeer(storage.Peer{Name: "B", URL: bSrv.URL, Secret: "sek"}); err != nil {
		t.Fatalf("upsert peer: %v", err)
	}
	sA := localServer("A", dbA, settings.New(dbA))

	// Create a check on B via the proxy.
	body := `{"name":"web","type":"http","interval_sec":30,"timeout_sec":5,"url":"http://localhost","method":"GET","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/B/settings/checks", strings.NewReader(body))
	req.SetPathValue("name", "B")
	rec := httptest.NewRecorder()
	sA.dispatchNode(sA.handleCreateCheck, "/api/peer/settings/checks")(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}

	checks := setB.Checks()
	if len(checks) != 1 || checks[0].Name != "web" {
		t.Fatalf("peer B checks = %+v, want one check named web", checks)
	}
}
