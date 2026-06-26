package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

func openTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(t.TempDir() + "/cluster_test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// nodeServer is a stand-in heartd node: it answers /api/peer/whoami with the
// given canonical name and /api/peer/members with the given membership list.
func nodeServer(t *testing.T, name string, members []Member) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/peer/whoami":
			_ = json.NewEncoder(w).Encode(WhoAmI{Name: name})
		case "/api/peer/members":
			_ = json.NewEncoder(w).Encode(MembersResponse{Members: members})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func peerNames(t *testing.T, db *storage.DB) map[string]bool {
	t.Helper()
	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	names := make(map[string]bool, len(peers))
	for _, p := range peers {
		names[p.Name] = true
	}
	return names
}

// TestGossipDiscoversReachableSkipsSelfAndUnreachable is the core fix: identity
// is resolved by probing a URL's /whoami, NOT by the node's own advertise_url
// (here deliberately empty). So a peer's correct-but-foreign-labelled entry for
// THIS node is recognized as self and skipped — no phantom — while a genuinely
// new reachable node is added under its canonical name, and an unreachable node
// is not added at all.
func TestGossipDiscoversReachableSkipsSelfAndUnreachable(t *testing.T) {
	db := openTestDB(t)
	const shared = "shared-secret"

	self := nodeServer(t, "node-self", nil) // a URL that loops back to us
	nodeC := nodeServer(t, "node-c", nil)   // a genuinely new, reachable node

	// The peer we gossip from lists: itself, US (under a foreign label but correct
	// URL), a new node-c, and an unreachable ghost.
	nodeB := nodeServer(t, "node-b", []Member{
		{Name: "node-b", URL: "http://node-b.example:9300"},
		{Name: "weird-label-for-self", URL: self.URL},
		{Name: "label-for-c", URL: nodeC.URL},
		{Name: "ghost", URL: "http://127.0.0.1:1/"},
	})
	if err := db.UpsertPeer(storage.Peer{Name: "node-b", URL: nodeB.URL, Secret: shared}); err != nil {
		t.Fatal(err)
	}

	// advertise_url is empty on purpose — self-detection must not depend on it.
	p := New(db, "node-self", "", "", settings.New(db))
	p.refreshFallback()
	p.gossip(context.Background())

	names := peerNames(t, db)
	if !names["node-c"] {
		t.Errorf("reachable new node-c should be discovered; have %v", names)
	}
	if names["weird-label-for-self"] || names["node-self"] {
		t.Errorf("this node must not be added as its own peer; have %v", names)
	}
	if names["ghost"] {
		t.Errorf("unreachable node must not be added; have %v", names)
	}
	if len(names) != 2 { // node-b (seeded) + node-c (discovered)
		t.Errorf("want exactly {node-b, node-c}, got %v", names)
	}
}

// TestGossipSelfHealRemovesPhantomSelf verifies the cleanup path: an auto-added
// (secret-less) peer whose URL resolves to this node is removed, the alert hook
// fires, and a legitimately distinct auto-added peer survives.
func TestGossipSelfHealRemovesPhantomSelf(t *testing.T) {
	db := openTestDB(t)
	self := nodeServer(t, "node-self", nil)
	nodeX := nodeServer(t, "node-x", nil)

	// phantom: no secret, URL loops back to us. node-x: no secret, distinct node.
	if err := db.UpsertPeer(storage.Peer{Name: "phantom", URL: self.URL}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertPeer(storage.Peer{Name: "node-x", URL: nodeX.URL}); err != nil {
		t.Fatal(err)
	}

	var removed []string
	p := New(db, "node-self", "", "", settings.New(db))
	p.SetOnPeerRemoved(func(n string) { removed = append(removed, n) })
	p.refreshFallback()
	p.gossip(context.Background())

	names := peerNames(t, db)
	if names["phantom"] {
		t.Errorf("phantom self peer should have been removed; have %v", names)
	}
	if !names["node-x"] {
		t.Errorf("legit peer node-x should survive; have %v", names)
	}
	if len(removed) != 1 || removed[0] != "phantom" {
		t.Errorf("onPeerRemoved = %v, want [phantom]", removed)
	}
}

// TestManuallyAddedPeerNeverSelfHealed guards the safety rule: a peer carrying a
// per-link secret is trusted as distinct and never auto-removed, even if (in a
// misconfig) its URL would resolve to this node.
func TestManuallyAddedPeerNeverSelfHealed(t *testing.T) {
	db := openTestDB(t)
	self := nodeServer(t, "node-self", nil)
	if err := db.UpsertPeer(storage.Peer{Name: "manual", URL: self.URL, Secret: "explicit"}); err != nil {
		t.Fatal(err)
	}

	p := New(db, "node-self", "", "", settings.New(db))
	p.refreshFallback()
	p.gossip(context.Background())

	if !peerNames(t, db)["manual"] {
		t.Errorf("a manually added peer (with a secret) must not be self-healed away")
	}
}

// TestEffectiveSecretAutoDerivesFromPeers is the zero-config path: with no
// peer_secret configured, a node reaches secret-less (gossiped) peers using the
// secret its existing peer table already shares. Peers that carry their own
// per-link secret keep using it.
func TestEffectiveSecretAutoDerivesFromPeers(t *testing.T) {
	db := openTestDB(t)
	const shared = "the-shared-secret"
	if err := db.UpsertPeer(storage.Peer{Name: "node-b", URL: "http://node-b:9300", Secret: shared}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertPeer(storage.Peer{Name: "node-c", URL: "http://node-c:9300", Secret: shared}); err != nil {
		t.Fatal(err)
	}

	p := New(db, "node-self", "http://node-self:9300", "", settings.New(db))
	p.refreshFallback()

	if got := p.effectiveSecret(storage.Peer{Name: "node-d", URL: "http://node-d:9300"}); got != shared {
		t.Errorf("secret-less peer resolved to %q, want derived shared secret %q", got, shared)
	}
	if got := p.effectiveSecret(storage.Peer{Name: "x", Secret: "own"}); got != "own" {
		t.Errorf("peer with own secret resolved to %q, want %q", got, "own")
	}
}
