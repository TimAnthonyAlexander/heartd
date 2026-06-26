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

// TestGossipDiscoversNewPeerWithClusterSecret is the end-to-end membership test:
// a peer added with no per-link secret is still reachable via the cluster secret,
// its /members view is fetched, and a previously-unknown node is added — while
// self and already-known nodes are not duplicated.
func TestGossipDiscoversNewPeerWithClusterSecret(t *testing.T) {
	const clusterSecret = "cluster-shared-secret"

	// A peer that authenticates with the cluster secret and advertises a third
	// node (node-c) plus the local node itself (which must NOT be re-added).
	var gotSecret string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get(SecretHeader)
		if r.URL.Path != "/api/peer/members" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(MembersResponse{Members: []Member{
			{Name: "node-self", URL: "http://node-self:9300"}, // us — skip
			{Name: "node-b", URL: peerURLPlaceholder},         // the responder — already known
			{Name: "node-c", URL: "http://node-c:9300"},       // genuinely new — add
		}})
	}))
	defer peer.Close()

	db := openTestDB(t)
	// node-b is known, enabled, but carries NO per-link secret — it must be
	// reached via the cluster secret.
	if err := db.UpsertPeer(storage.Peer{Name: "node-b", URL: peer.URL}); err != nil {
		t.Fatalf("seed peer: %v", err)
	}

	p := New(db, "node-self", "http://node-self:9300", clusterSecret, settings.New(db))
	p.refreshFallback() // Run does this each cycle before gossip/poll
	p.gossip(context.Background())

	if gotSecret != clusterSecret {
		t.Errorf("peer was called with secret %q, want the cluster secret %q", gotSecret, clusterSecret)
	}

	peers, err := db.ListPeers()
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	names := map[string]bool{}
	for _, pr := range peers {
		names[pr.Name] = true
	}
	if !names["node-c"] {
		t.Errorf("expected node-c to be discovered via gossip; have %v", names)
	}
	if names["node-self"] {
		t.Errorf("the local node must not be added as its own peer; have %v", names)
	}
	if len(peers) != 2 { // node-b (seeded) + node-c (discovered)
		t.Errorf("expected exactly 2 peers (node-b, node-c), got %d: %v", len(peers), names)
	}
}

// TestEffectiveSecretAutoDerivesFromPeers is the zero-config path: with no
// peer_secret configured, a node reaches secret-less (gossiped) peers using the
// secret its existing peer table already shares. Peers that carry their own
// per-link secret keep using it.
func TestEffectiveSecretAutoDerivesFromPeers(t *testing.T) {
	db := openTestDB(t)
	const shared = "the-shared-secret"
	// Two existing links that share one secret (the typical setup).
	if err := db.UpsertPeer(storage.Peer{Name: "node-b", URL: "http://node-b:9300", Secret: shared}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertPeer(storage.Peer{Name: "node-c", URL: "http://node-c:9300", Secret: shared}); err != nil {
		t.Fatal(err)
	}

	// No clusterSecret configured — the fallback must be derived from the table.
	p := New(db, "node-self", "http://node-self:9300", "", settings.New(db))
	p.refreshFallback()

	// A gossip-discovered peer (no per-link secret) is reached with the shared one.
	if got := p.effectiveSecret(storage.Peer{Name: "node-d", URL: "http://node-d:9300"}); got != shared {
		t.Errorf("secret-less peer resolved to %q, want derived shared secret %q", got, shared)
	}
	// A peer with its own secret still uses that.
	if got := p.effectiveSecret(storage.Peer{Name: "x", Secret: "own"}); got != "own" {
		t.Errorf("peer with own secret resolved to %q, want %q", got, "own")
	}
}

// peerURLPlaceholder stands in for "the responder's own URL" in the members list.
// node-b is already known by name, so the exact URL value is irrelevant to the
// dedup (name match wins) — any non-empty URL exercises the already-known path.
const peerURLPlaceholder = "http://node-b:9300"
