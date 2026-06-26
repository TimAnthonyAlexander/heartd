package storage

import "testing"

func TestPeerEnabledDefaultsTrueAndToggles(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpsertPeer(Peer{Name: "laptop", URL: "http://x", Secret: "s"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// New peers are enabled (not muted) by default.
	p, ok, err := db.GetPeer("laptop")
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	if !p.Enabled {
		t.Fatal("new peer should be enabled by default")
	}

	// Mute it.
	if err := db.SetPeerEnabled("laptop", false); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	p, _, _ = db.GetPeer("laptop")
	if p.Enabled {
		t.Fatal("peer should be muted after SetPeerEnabled(false)")
	}

	// An announce-style upsert (empty secret) must NOT un-mute it.
	if err := db.UpsertPeer(Peer{Name: "laptop", URL: "http://y"}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	p, _, _ = db.GetPeer("laptop")
	if p.Enabled {
		t.Fatal("upsert must preserve the muted state")
	}
	if p.URL != "http://y" {
		t.Fatalf("url not updated: %q", p.URL)
	}
}
