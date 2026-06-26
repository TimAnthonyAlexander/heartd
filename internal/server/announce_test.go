package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// announce drives handlePeerAnnounce with the given JSON body.
func announce(t *testing.T, s *server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/peer/announce", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handlePeerAnnounce(rec, req)
	return rec
}

func peerNames(t *testing.T, db *storage.DB) []string {
	t.Helper()
	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	out := make([]string, 0, len(peers))
	for _, p := range peers {
		out = append(out, p.Name)
	}
	return out
}

// A node announcing under a new name from an address already tracked under a
// different name must NOT create a duplicate. This is the reported bug: a laptop
// (config name "web-01", advertise http://localhost:9300) re-announcing after the
// same address was added manually as "macbook".
func TestAnnounceDoesNotDuplicateSameAddress(t *testing.T) {
	db := testDB(t)
	s := localServer("hq", db, settings.New(db))
	if err := db.UpsertPeer(storage.Peer{Name: "macbook", URL: "http://localhost:9300", Secret: "s"}); err != nil {
		t.Fatal(err)
	}

	rec := announce(t, s, `{"name":"web-01","url":"http://localhost:9300"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("announce = %d, body %s", rec.Code, rec.Body.String())
	}

	names := peerNames(t, db)
	if len(names) != 1 || names[0] != "macbook" {
		t.Fatalf("expected only [macbook], got %v", names)
	}
}

// Cosmetic URL differences (scheme default port, case, trailing slash) must still
// be recognized as the same address.
func TestAnnounceDedupNormalizesAddress(t *testing.T) {
	db := testDB(t)
	s := localServer("hq", db, settings.New(db))
	if err := db.UpsertPeer(storage.Peer{Name: "macbook", URL: "https://Example.com/", Secret: "s"}); err != nil {
		t.Fatal(err)
	}

	// https default port 443, lowercased host — same address as the stored URL.
	rec := announce(t, s, `{"name":"web-01","url":"https://example.com:443"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("announce = %d, body %s", rec.Code, rec.Body.String())
	}
	if names := peerNames(t, db); len(names) != 1 {
		t.Fatalf("expected no duplicate, got %v", names)
	}
}

// A genuinely new node (different address) is still auto-created — announces must
// keep working for real new peers.
func TestAnnounceCreatesGenuinelyNewNode(t *testing.T) {
	db := testDB(t)
	s := localServer("hq", db, settings.New(db))
	if err := db.UpsertPeer(storage.Peer{Name: "macbook", URL: "http://localhost:9300", Secret: "s"}); err != nil {
		t.Fatal(err)
	}

	rec := announce(t, s, `{"name":"db-01","url":"http://10.0.0.5:9300"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("announce = %d, body %s", rec.Code, rec.Body.String())
	}
	if _, ok, _ := db.GetPeer("db-01"); !ok {
		t.Fatalf("new node db-01 should have been created; peers = %v", peerNames(t, db))
	}
}

// An announce under an EXISTING name refreshes that peer's URL (and does not
// create anything), even though its address matches itself.
func TestAnnounceRefreshesKnownPeerURL(t *testing.T) {
	db := testDB(t)
	s := localServer("hq", db, settings.New(db))
	if err := db.UpsertPeer(storage.Peer{Name: "db-01", URL: "http://old:9300", Secret: "s"}); err != nil {
		t.Fatal(err)
	}

	rec := announce(t, s, `{"name":"db-01","url":"http://new:9300"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("announce = %d, body %s", rec.Code, rec.Body.String())
	}
	p, _, _ := db.GetPeer("db-01")
	if p.URL != "http://new:9300" {
		t.Fatalf("URL = %q, want refreshed to http://new:9300", p.URL)
	}
	if p.Secret != "s" {
		t.Fatalf("secret = %q, want preserved", p.Secret)
	}
	if names := peerNames(t, db); len(names) != 1 {
		t.Fatalf("expected only db-01, got %v", names)
	}
}

// Missing name/url are rejected (unchanged behavior).
func TestAnnounceValidatesBody(t *testing.T) {
	db := testDB(t)
	s := localServer("hq", db, settings.New(db))
	if rec := announce(t, s, `{"name":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing url = %d, want 400", rec.Code)
	}
	if rec := announce(t, s, `{"url":"http://x:9300"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name = %d, want 400", rec.Code)
	}
}
