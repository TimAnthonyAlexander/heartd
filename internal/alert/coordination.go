package alert

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

// secretHeader carries the per-link shared secret on node-to-node requests.
// (Mirrors cluster.SecretHeader; duplicated here to avoid an import cycle.)
const secretHeader = "X-Heartd-Secret"

// PeerLister yields the current peer set. *storage.DB satisfies it.
type PeerLister interface {
	ListPeers() ([]storage.Peer, error)
}

// Coordinator deduplicates alerts across nodes. In heartd every node watches
// every peer independently, so when node X dies, each surviving node would send
// its own "X unreachable" mail. The Coordinator elects a single sender per
// incident without a leader: on an alert edge a node records itself as a
// candidate and asks its reachable peers whether anyone has already claimed or
// sent this incident. The smallest-named candidate sends; once any node reports
// "sent", the rest stand down. If peers can't be reached (a partition), a node
// falls back to sending — duplicates are acceptable, silence is not.
//
// Coordination applies only to alerts ABOUT a peer (a.Node != selfName). A
// node's own metric/check alerts are observed by nobody else, so they always
// send directly and never enter the ledger.
type Coordinator struct {
	selfName string
	peers    PeerLister
	client   *http.Client
	ttl      time.Duration

	mu     sync.Mutex
	ledger map[string]*claimEntry
}

type claimEntry struct {
	owner string // smallest-named node known to be handling this incident
	sent  bool   // some node has already delivered it
	at    time.Time
}

// claimMsg is the body for both /api/peer/alert-claim and /api/peer/alert-sent.
type claimMsg struct {
	Key  string `json:"key"`
	Node string `json:"node"`
}

// claimResponse is the reply to a claim query.
type claimResponse struct {
	Owner string `json:"owner"`
	Sent  bool   `json:"sent"`
}

// NewCoordinator builds a Coordinator for the local node.
func NewCoordinator(selfName string, peers PeerLister) *Coordinator {
	return &Coordinator{
		selfName: selfName,
		peers:    peers,
		client:   &http.Client{Timeout: 3 * time.Second},
		ttl:      10 * time.Minute,
		ledger:   make(map[string]*claimEntry),
	}
}

// incidentKey identifies an incident across observers: same source + dead node +
// entity + firing/recovered state yields the same key on every watcher,
// regardless of the (differently named) rule that produced it. Including the
// source keeps distinct alerts about one peer separate — e.g. "unreachable"
// (peer) and "stale data" (nodata) each get their own mail.
func incidentKey(a Alert) string {
	return a.Source + "|" + a.Node + "|" + a.Entity + "|" + a.Status()
}

// minName returns the lexicographically smaller non-empty name.
func minName(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case a < b:
		return a
	default:
		return b
	}
}

// entryLocked returns the ledger entry for key, creating (or resetting, if past
// TTL) it. Caller holds c.mu.
func (c *Coordinator) entryLocked(key string, now time.Time) *claimEntry {
	e := c.ledger[key]
	if e == nil || now.Sub(e.at) > c.ttl {
		e = &claimEntry{at: now}
		c.ledger[key] = e
	}
	return e
}

// HandleClaim records the asking node as a candidate for key and returns the
// current owner (smallest name seen) and whether the incident was already sent.
func (c *Coordinator) HandleClaim(node, key string, now time.Time) (owner string, sent bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entryLocked(key, now)
	e.owner = minName(e.owner, node)
	e.at = now
	return e.owner, e.sent
}

// HandleSent marks an incident delivered (a peer told us it sent the mail), so
// this node suppresses its own copy.
func (c *Coordinator) HandleSent(node, key string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entryLocked(key, now)
	e.sent = true
	e.owner = minName(e.owner, node)
	e.at = now
}

// ShouldSend reports whether THIS node should deliver alert a. See the type doc
// for the protocol. Safe to call on a nil Coordinator (always true).
func (c *Coordinator) ShouldSend(a Alert) bool {
	if c == nil {
		return true
	}
	// Self incidents aren't observed by other nodes — nothing to coordinate.
	if a.Node == "" || a.Node == c.selfName {
		return true
	}

	now := time.Now().UTC()
	key := incidentKey(a)

	// Record our own candidacy first, so a peer querying us in the same instant
	// already sees us in the race (this is what prevents a double-send tie).
	c.mu.Lock()
	e := c.entryLocked(key, now)
	e.owner = minName(e.owner, c.selfName)
	if e.sent {
		c.mu.Unlock()
		return false
	}
	owner := e.owner
	c.mu.Unlock()

	peers, _ := c.peers.ListPeers()
	sentElsewhere := false
	var agg sync.Mutex
	var wg sync.WaitGroup
	for _, p := range peers {
		if !p.Enabled || p.URL == "" || p.Name == c.selfName || p.Name == a.Node {
			continue // skip self and the incident's (likely-down) subject
		}
		wg.Add(1)
		go func(p storage.Peer) {
			defer wg.Done()
			resp, ok := c.askPeer(p, "/api/peer/alert-claim", key)
			if !ok {
				return
			}
			agg.Lock()
			owner = minName(owner, resp.Owner)
			if resp.Sent {
				sentElsewhere = true
			}
			agg.Unlock()
		}(p)
	}
	wg.Wait()

	if sentElsewhere || owner != c.selfName {
		return false
	}

	// We win the election. Commit locally and tell peers so they stand down.
	c.mu.Lock()
	e2 := c.entryLocked(key, time.Now().UTC())
	racedOut := e2.sent && e2.owner != c.selfName
	e2.sent = true
	e2.owner = c.selfName
	c.mu.Unlock()
	if racedOut {
		return false
	}
	c.broadcastSent(peers, key, a.Node)
	return true
}

// askPeer POSTs a claim/sent message to one peer and decodes the response. ok is
// false on any network/decode error (treated as "peer didn't answer").
func (c *Coordinator) askPeer(p storage.Peer, path, key string) (claimResponse, bool) {
	body, _ := json.Marshal(claimMsg{Key: key, Node: c.selfName})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(p.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return claimResponse{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(secretHeader, p.Secret)
	resp, err := c.client.Do(req)
	if err != nil {
		return claimResponse{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return claimResponse{}, false
	}
	var out claimResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return claimResponse{}, false
	}
	return out, true
}

// broadcastSent tells every reachable peer (except the subject) that this node
// delivered the incident, best-effort and concurrent.
func (c *Coordinator) broadcastSent(peers []storage.Peer, key, subject string) {
	for _, p := range peers {
		if !p.Enabled || p.URL == "" || p.Name == c.selfName || p.Name == subject {
			continue
		}
		go func(p storage.Peer) { _, _ = c.askPeer(p, "/api/peer/alert-sent", key) }(p)
	}
}
