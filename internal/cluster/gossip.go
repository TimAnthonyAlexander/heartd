package cluster

import (
	"context"
	"log"
	"net/url"
	"strings"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

// Member is one node in a cluster's membership view: a name and the URL to reach
// it. It is what /api/peer/members exchanges so nodes can learn about peers they
// were never told about directly (peers-of-peers).
type Member struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// MembersResponse is the body returned by GET /api/peer/members: the responding
// node's own identity plus the peers it currently polls.
type MembersResponse struct {
	Members []Member `json:"members"`
}

// gossip learns about peers-of-peers and adds any this node doesn't yet know.
// For every enabled peer it fetches that peer's membership view and merges in
// unknown nodes. Membership converges across the cluster: a node added on ONE
// machine is propagated to all the others within a cycle or two, with no manual
// fan-out. Merging is ADD-ONLY — it never edits or removes an existing peer, so
// a hand-set URL or secret is never clobbered, and a peer you deliberately
// removed is not silently resurrected by the same node within the cycle.
func (p *Poller) gossip(ctx context.Context) {
	peers := p.peers()
	if len(peers) == 0 {
		return
	}

	// Snapshot the full local view once (all peers, not just enabled) so we don't
	// re-create a muted peer or an address we already track under another name.
	known, err := p.db.ListPeers()
	if err != nil {
		log.Printf("cluster: gossip list peers failed: %v", err)
		return
	}

	for _, peer := range peers {
		var resp MembersResponse
		if err := p.getJSON(ctx, peer, "/api/peer/members", &resp); err != nil {
			continue // peer unreachable or too old to know the endpoint; skip quietly
		}
		for _, m := range resp.Members {
			if p.addDiscovered(m, known) {
				// Reflect the new row in our local snapshot so a node advertised by
				// two peers in the same cycle is only added once.
				known = append(known, storage.Peer{Name: m.Name, URL: m.URL, Enabled: true})
			}
		}
	}
}

// addDiscovered adds m as a new peer when it is genuinely new, returning whether
// it did. It skips this node itself (by name or advertised address), any peer
// already tracked by name, and any node already reachable at the same address
// under a different name. New rows carry no secret (outbound falls back to the
// cluster secret) and are enabled by default so they are polled immediately.
func (p *Poller) addDiscovered(m Member, known []storage.Peer) bool {
	if !shouldAddMember(m, p.selfName, p.advertiseURL, known) {
		return false
	}
	if err := p.db.UpsertPeer(storage.Peer{Name: m.Name, URL: m.URL}); err != nil {
		log.Printf("cluster: gossip add %q failed: %v", m.Name, err)
		return false
	}
	log.Printf("cluster: discovered peer %q at %s via gossip", m.Name, m.URL)
	return true
}

// shouldAddMember decides whether a gossiped member is a new node worth adding,
// given this node's identity and its current peer set. Pure (no I/O) so the
// dedup rules are unit-testable.
func shouldAddMember(m Member, selfName, advertiseURL string, known []storage.Peer) bool {
	if m.Name == "" || m.URL == "" {
		return false
	}
	if m.Name == selfName {
		return false // that's us, by name
	}
	memAddr, memOK := NormalizeAddr(m.URL)
	if selfAddr, ok := NormalizeAddr(advertiseURL); ok && memOK && selfAddr == memAddr {
		return false // that's us, by advertised address
	}
	for _, k := range known {
		if k.Name == m.Name {
			return false // already tracked by name
		}
		if kAddr, ok := NormalizeAddr(k.URL); ok && memOK && kAddr == memAddr {
			return false // already tracked at this address under another name
		}
	}
	return true
}

// NormalizeAddr reduces a URL to a lowercase "host:port", filling in the default
// port for the scheme, so two URLs that differ only cosmetically (case, implicit
// port) compare equal. Returns ok=false when the URL has no usable host.
func NormalizeAddr(rawURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host + ":" + port, true
}
