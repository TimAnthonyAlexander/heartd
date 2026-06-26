package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// seedNodeData inserts one row in each per-node table so purge-on-delete can be
// verified.
func seedNodeData(t *testing.T, db *storage.DB, node string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.InsertMetric(storage.MetricSample{Node: node, MemTotal: 1, At: now}); err != nil {
		t.Fatalf("insert metric: %v", err)
	}
	if err := db.UpsertCheckStatus(storage.CheckStatus{Node: node, Name: "c", Type: "http", Status: "ok", At: now}); err != nil {
		t.Fatalf("upsert check: %v", err)
	}
	if err := db.UpsertDiskStatus(storage.DiskStatus{Node: node, Mount: "/", Used: 1, Total: 2, Percent: 50, At: now}); err != nil {
		t.Fatalf("upsert disk: %v", err)
	}
	if err := db.InsertNetSample(storage.NetSample{Node: node, At: now}); err != nil {
		t.Fatalf("insert net: %v", err)
	}
}

func TestCreatePeerValidationAndConflict(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))

	call := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.handleCreatePeer(rec, req)
		return rec.Code
	}

	// Missing secret -> 400.
	if code := call(`{"name":"db-01","url":"http://db-01:9300"}`); code != http.StatusBadRequest {
		t.Fatalf("no-secret create = %d, want 400", code)
	}
	// Self name -> 400.
	if code := call(`{"name":"web-01","url":"http://x:9300","secret":"s"}`); code != http.StatusBadRequest {
		t.Fatalf("self-name create = %d, want 400", code)
	}
	// Bad URL -> 400.
	if code := call(`{"name":"db-01","url":"not-a-url","secret":"s"}`); code != http.StatusBadRequest {
		t.Fatalf("bad-url create = %d, want 400", code)
	}
	// Valid -> 200.
	if code := call(`{"name":"db-01","url":"http://db-01:9300","secret":"s"}`); code != http.StatusOK {
		t.Fatalf("valid create = %d, want 200", code)
	}
	// Duplicate -> 409.
	if code := call(`{"name":"db-01","url":"http://db-01:9300","secret":"s"}`); code != http.StatusConflict {
		t.Fatalf("duplicate create = %d, want 409", code)
	}

	// The secret is stored but never returned.
	if body := listPeersBody(t, s); strings.Contains(body, `"secret"`) || !strings.Contains(body, `"has_secret":true`) {
		t.Fatalf("list leaked secret or missing has_secret: %s", body)
	}
}

func TestUpdatePeerKeepsSecretWhenBlank(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	if err := db.UpsertPeer(storage.Peer{Name: "db-01", URL: "http://old:9300", Secret: "orig"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/peers/db-01", strings.NewReader(`{"url":"http://new:9300"}`))
	req.SetPathValue("name", "db-01")
	rec := httptest.NewRecorder()
	s.handleUpdatePeer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d, body %s", rec.Code, rec.Body.String())
	}

	p, _, _ := db.GetPeer("db-01")
	if p.URL != "http://new:9300" {
		t.Fatalf("url = %q, want updated", p.URL)
	}
	if p.Secret != "orig" {
		t.Fatalf("secret = %q, want preserved 'orig'", p.Secret)
	}
}

func TestDeletePeerPurgesData(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	if err := db.UpsertPeer(storage.Peer{Name: "db-01", URL: "http://db-01:9300", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	seedNodeData(t, db, "db-01")

	req := httptest.NewRequest(http.MethodDelete, "/api/peers/db-01", nil)
	req.SetPathValue("name", "db-01")
	rec := httptest.NewRecorder()
	s.handleDeletePeer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, body %s", rec.Code, rec.Body.String())
	}

	if _, ok, _ := db.GetPeer("db-01"); ok {
		t.Fatal("peer row still present after delete")
	}
	if _, ok, _ := db.LatestMetric("db-01"); ok {
		t.Fatal("metric data not purged")
	}
	if st, _ := db.CheckStatuses("db-01"); len(st) != 0 {
		t.Fatalf("check data not purged: %d rows", len(st))
	}
	if d, _ := db.DiskStatuses("db-01"); len(d) != 0 {
		t.Fatalf("disk data not purged: %d rows", len(d))
	}
	if _, ok, _ := db.LatestNetSample("db-01"); ok {
		t.Fatal("net data not purged")
	}
}

func TestDeleteLocalNodeRejected(t *testing.T) {
	db := testDB(t)
	s := localServer("web-01", db, settings.New(db))
	req := httptest.NewRequest(http.MethodDelete, "/api/peers/web-01", nil)
	req.SetPathValue("name", "web-01")
	rec := httptest.NewRecorder()
	s.handleDeletePeer(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete local = %d, want 400", rec.Code)
	}
}

func listPeersBody(t *testing.T, s *server) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	rec := httptest.NewRecorder()
	s.handleListPeers(rec, req)
	return rec.Body.String()
}
