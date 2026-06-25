package storage

import (
	"testing"
	"time"
)

func TestUpsertPeerInsertAndList(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertPeer(Peer{Name: "alpha", URL: "http://a", Secret: "s"}); err != nil {
		t.Fatalf("UpsertPeer: %v", err)
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("len(peers) = %d, want 1", len(peers))
	}
	p := peers[0]
	if p.Name != "alpha" || p.URL != "http://a" {
		t.Fatalf("got name=%q url=%q", p.Name, p.URL)
	}
	if p.Secret != "s" {
		t.Fatalf("secret = %q, want %q", p.Secret, "s")
	}
	if p.Status != "unknown" {
		t.Fatalf("status = %q, want unknown", p.Status)
	}
	if !p.LastSeen.IsZero() {
		t.Fatalf("LastSeen = %v, want zero", p.LastSeen)
	}
	if p.LastError != "" {
		t.Fatalf("LastError = %q, want empty", p.LastError)
	}
}

func TestUpsertPeerEmptySecretDoesNotClobber(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertPeer(Peer{Name: "alpha", URL: "http://a", Secret: "s"}); err != nil {
		t.Fatalf("UpsertPeer insert: %v", err)
	}
	if err := db.UpsertPeer(Peer{Name: "alpha", URL: "http://b", Secret: ""}); err != nil {
		t.Fatalf("UpsertPeer update: %v", err)
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("len(peers) = %d, want 1", len(peers))
	}
	if peers[0].Secret != "s" {
		t.Fatalf("secret = %q, want %q (must not clobber)", peers[0].Secret, "s")
	}
	if peers[0].URL != "http://b" {
		t.Fatalf("url = %q, want %q (should update)", peers[0].URL, "http://b")
	}
}

func TestUpsertPeerNonEmptySecretUpdates(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertPeer(Peer{Name: "alpha", URL: "http://a", Secret: "s"}); err != nil {
		t.Fatalf("UpsertPeer insert: %v", err)
	}
	if err := db.UpsertPeer(Peer{Name: "alpha", URL: "http://a", Secret: "new"}); err != nil {
		t.Fatalf("UpsertPeer update: %v", err)
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if peers[0].Secret != "new" {
		t.Fatalf("secret = %q, want %q", peers[0].Secret, "new")
	}
}

func TestSetPeerStatusWithLastSeen(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertPeer(Peer{Name: "alpha", URL: "http://a"}); err != nil {
		t.Fatalf("UpsertPeer: %v", err)
	}

	now := time.Now()
	if err := db.SetPeerStatus("alpha", "ok", now, ""); err != nil {
		t.Fatalf("SetPeerStatus: %v", err)
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	p := peers[0]
	if p.Status != "ok" {
		t.Fatalf("status = %q, want ok", p.Status)
	}
	if p.LastError != "" {
		t.Fatalf("LastError = %q, want empty", p.LastError)
	}
	if d := p.LastSeen.Sub(now); d > time.Second || d < -time.Second {
		t.Fatalf("LastSeen = %v, want within 1s of %v", p.LastSeen, now)
	}
}

func TestSetPeerStatusZeroLastSeenPreservesLastSeen(t *testing.T) {
	db := openTestDB(t)

	if err := db.UpsertPeer(Peer{Name: "alpha", URL: "http://a"}); err != nil {
		t.Fatalf("UpsertPeer: %v", err)
	}

	now := time.Now()
	if err := db.SetPeerStatus("alpha", "ok", now, ""); err != nil {
		t.Fatalf("SetPeerStatus ok: %v", err)
	}

	peers, _ := db.ListPeers()
	wantSeen := peers[0].LastSeen

	if err := db.SetPeerStatus("alpha", "down", time.Time{}, "boom"); err != nil {
		t.Fatalf("SetPeerStatus down: %v", err)
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	p := peers[0]
	if p.Status != "down" {
		t.Fatalf("status = %q, want down", p.Status)
	}
	if p.LastError != "boom" {
		t.Fatalf("LastError = %q, want boom", p.LastError)
	}
	if !p.LastSeen.Equal(wantSeen) {
		t.Fatalf("LastSeen = %v, want unchanged %v", p.LastSeen, wantSeen)
	}
}

func TestSetPeerStatusMissingPeerNoOp(t *testing.T) {
	db := openTestDB(t)

	if err := db.SetPeerStatus("ghost", "down", time.Time{}, "x"); err != nil {
		t.Fatalf("SetPeerStatus: %v", err)
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("len(peers) = %d, want 0", len(peers))
	}
}

func TestListPeersOrdering(t *testing.T) {
	db := openTestDB(t)

	for _, name := range []string{"charlie", "alpha", "bravo"} {
		if err := db.UpsertPeer(Peer{Name: name, URL: "http://" + name}); err != nil {
			t.Fatalf("UpsertPeer %q: %v", name, err)
		}
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	got := []string{peers[0].Name, peers[1].Name, peers[2].Name}
	want := []string{"alpha", "bravo", "charlie"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ordering = %v, want %v", got, want)
		}
	}
}
