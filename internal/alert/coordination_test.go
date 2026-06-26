package alert

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

type fakePeers struct{ peers []storage.Peer }

func (f *fakePeers) ListPeers() ([]storage.Peer, error) { return f.peers, nil }

// coordServer exposes a Coordinator's claim/sent handlers over HTTP so another
// Coordinator can query it exactly as it would a real peer.
func coordServer(c *Coordinator) *httptest.Server {
	mux := http.NewServeMux()
	decode := func(r *http.Request) claimMsg {
		var m claimMsg
		_ = json.NewDecoder(r.Body).Decode(&m)
		return m
	}
	mux.HandleFunc("/api/peer/alert-claim", func(w http.ResponseWriter, r *http.Request) {
		m := decode(r)
		owner, sent := c.HandleClaim(m.Node, m.Key, time.Now().UTC())
		_ = json.NewEncoder(w).Encode(claimResponse{Owner: owner, Sent: sent})
	})
	mux.HandleFunc("/api/peer/alert-sent", func(w http.ResponseWriter, r *http.Request) {
		m := decode(r)
		c.HandleSent(m.Node, m.Key, time.Now().UTC())
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return httptest.NewServer(mux)
}

func peerDown() Alert {
	return Alert{Kind: KindRule, Node: "x", Firing: true, Severity: "critical", Title: "x unreachable"}
}

func TestCoordinatorSelfAlertAlwaysSends(t *testing.T) {
	c := NewCoordinator("a", &fakePeers{})
	// An alert about this node itself is observed by nobody else.
	if !c.ShouldSend(Alert{Node: "a", Firing: true, Title: "High CPU — a"}) {
		t.Fatal("self alert must always send")
	}
}

func TestCoordinatorSingleNodeSends(t *testing.T) {
	c := NewCoordinator("a", &fakePeers{}) // no peers
	if !c.ShouldSend(peerDown()) {
		t.Fatal("a single node with no peers must send")
	}
}

// Two nodes watching a third that died: exactly one of them sends, regardless of
// which evaluates first.
func TestCoordinatorElectsOneSender(t *testing.T) {
	for _, order := range []string{"a-first", "b-first"} {
		fpA, fpB := &fakePeers{}, &fakePeers{}
		coordA := NewCoordinator("a", fpA)
		coordB := NewCoordinator("b", fpB)
		srvA := coordServer(coordA)
		srvB := coordServer(coordB)
		defer srvA.Close()
		defer srvB.Close()
		// Each watches the other and the (down) subject x.
		fpA.peers = []storage.Peer{
			{Name: "b", URL: srvB.URL, Secret: "s", Enabled: true},
			{Name: "x", URL: "http://127.0.0.1:1", Secret: "s", Enabled: true},
		}
		fpB.peers = []storage.Peer{
			{Name: "a", URL: srvA.URL, Secret: "s", Enabled: true},
			{Name: "x", URL: "http://127.0.0.1:1", Secret: "s", Enabled: true},
		}

		var first, second *Coordinator
		if order == "a-first" {
			first, second = coordA, coordB
		} else {
			first, second = coordB, coordA
		}
		sent1 := first.ShouldSend(peerDown())
		sent2 := second.ShouldSend(peerDown())

		if !sent1 {
			t.Fatalf("[%s] the first evaluator should send", order)
		}
		if sent2 {
			t.Fatalf("[%s] the second evaluator should suppress (duplicate)", order)
		}
	}
}

// With peers unreachable (a partition), a node sends rather than stay silent.
func TestCoordinatorPartitionFallbackSends(t *testing.T) {
	fp := &fakePeers{peers: []storage.Peer{
		{Name: "b", URL: "http://127.0.0.1:1", Secret: "s", Enabled: true}, // unreachable
	}}
	c := NewCoordinator("a", fp)
	if !c.ShouldSend(peerDown()) {
		t.Fatal("with unreachable peers a node must fall back to sending, not stay silent")
	}
}

func TestHandleClaimMinNameAndSent(t *testing.T) {
	c := NewCoordinator("m", &fakePeers{})
	now := time.Now().UTC()

	owner, sent := c.HandleClaim("c", "k", now)
	if owner != "c" || sent {
		t.Fatalf("first claim: owner=%q sent=%v, want c/false", owner, sent)
	}
	// A larger name must not take ownership from a smaller one.
	owner, _ = c.HandleClaim("z", "k", now)
	if owner != "c" {
		t.Fatalf("owner should stay smallest-named c, got %q", owner)
	}
	// A smaller name does.
	owner, _ = c.HandleClaim("a", "k", now)
	if owner != "a" {
		t.Fatalf("owner should drop to a, got %q", owner)
	}
	c.HandleSent("a", "k", now)
	if _, sent := c.HandleClaim("a", "k", now); !sent {
		t.Fatal("claim after HandleSent should report sent=true")
	}
}
