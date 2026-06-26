package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

func TestAnnounceNeverCreatesButRefreshesKnown(t *testing.T) {
	db := testDB(t)
	s := localServer("hq", db, settings.New(db))

	announce := func(name, url string) {
		req := httptest.NewRequest(http.MethodPost, "/api/peer/announce",
			strings.NewReader(`{"name":"`+name+`","url":"`+url+`"}`))
		rec := httptest.NewRecorder()
		s.handlePeerAnnounce(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("announce %s = %d", name, rec.Code)
		}
	}

	// Unknown announcer must NOT be created (no surprise duplicate node).
	announce("web-01", "http://localhost:9300")
	if _, ok, _ := db.GetPeer("web-01"); ok {
		t.Fatal("announce created an unknown peer; it must not")
	}

	// A peer we already manage gets its advertised URL refreshed (secret kept).
	if err := db.UpsertPeer(storage.Peer{Name: "laptop", URL: "http://old:9300", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	announce("laptop", "http://new:9300")
	p, _, _ := db.GetPeer("laptop")
	if p.URL != "http://new:9300" {
		t.Fatalf("known peer URL = %q, want refreshed", p.URL)
	}
	if p.Secret != "s" {
		t.Fatalf("announce clobbered the secret: %q", p.Secret)
	}
}
