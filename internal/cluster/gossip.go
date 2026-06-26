package cluster

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

// probeTimeout bounds a single /whoami identity probe. Short, because probes run
// inline during the gossip pass and an unreachable candidate must not stall it.
const probeTimeout = 4 * time.Second

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

// WhoAmI is the body returned by GET /api/peer/whoami: a node's canonical name.
// It is the authoritative answer to "which node is reachable at this URL?", used
// to identify a gossiped URL — including one that loops back to the asking node
// itself, whose own advertise_url may be unset or wrong.
type WhoAmI struct {
	Name string `json:"name"`
}

// Identity is the body returned by GET /api/peer/identity: a node's real name
// plus its own effective display name. Pollers fetch it to learn the label a
// node advertises for itself, so a display name set once on a node propagates to
// every dashboard that polls it. DisplayName is empty (or equal to Name) when
// the node advertises no distinct label.
type Identity struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// gossip learns about peers-of-peers and adds any this node can reach but does
// not yet know. Membership converges across the cluster: a node added on ONE
// machine propagates to the others within a cycle or two, with no manual fan-out.
//
// Identity is resolved by ASKING a URL who it is (/whoami), never by trusting a
// node's own advertised URL or a peer's local label for it. A node's peers all
// record it at the same correct URL, so probing that URL reliably (a) confirms
// it is reachable — we only add nodes we can actually monitor — and (b) detects
// a URL that is really THIS node, which is what prevents a node adding itself as
// a phantom peer when its own advertise_url doesn't match.
func (p *Poller) gossip(ctx context.Context) {
	known, err := p.db.ListPeers()
	if err != nil {
		log.Printf("cluster: gossip list peers failed: %v", err)
		return
	}

	// Self-heal first: drop any auto-added row that turns out to be this node, so
	// phantom-self rows from before this fix (or a transient mislabel) clear out.
	known = p.selfHeal(ctx, known)

	for _, peer := range known {
		if !peer.Enabled {
			continue
		}
		var resp MembersResponse
		if err := p.getJSON(ctx, peer, "/api/peer/members", &resp); err != nil {
			continue // peer unreachable or too old to know the endpoint; skip quietly
		}
		for _, m := range resp.Members {
			if added := p.considerMember(ctx, m, known); added != nil {
				// Reflect the new row locally so a node advertised by two peers in the
				// same cycle is only added once.
				known = append(known, *added)
			}
		}
	}
}

// selfHeal removes any auto-added peer (one with no per-link secret) whose URL
// reports, via /whoami, that it is actually this node. Manually added peers
// (which carry a secret) are trusted as distinct and never auto-removed. Returns
// the surviving peer list. Reachability is required to confirm self, so a
// phantom whose URL this node can't reach is left for manual removal rather than
// guessed at.
func (p *Poller) selfHeal(ctx context.Context, known []storage.Peer) []storage.Peer {
	kept := make([]storage.Peer, 0, len(known))
	for _, k := range known {
		if k.Secret == "" && k.URL != "" {
			if name, ok := p.probeName(ctx, k.URL); ok && name == p.selfName {
				p.removePeer(k.Name)
				continue
			}
		}
		kept = append(kept, k)
	}
	return kept
}

// considerMember adds m as a new peer when it is genuinely new, reachable, and
// not this node itself, returning the row it added (or nil). Cheap local checks
// run first; only a candidate that survives them is probed over the network.
func (p *Poller) considerMember(ctx context.Context, m Member, known []storage.Peer) *storage.Peer {
	memAddr, ok := NormalizeAddr(m.URL)
	if m.Name == "" || !ok {
		return nil
	}
	if m.Name == p.selfName {
		return nil // cheap: self by name (when labels happen to be canonical)
	}
	if selfAddr, ok := NormalizeAddr(p.advertiseURL); ok && selfAddr == memAddr {
		return nil // cheap: self by advertised address (when advertise_url is correct)
	}
	for _, k := range known {
		if k.Name == m.Name {
			return nil // already tracked by name
		}
		if kAddr, ok := NormalizeAddr(k.URL); ok && kAddr == memAddr {
			return nil // already tracked at this address under another name
		}
	}

	// Authoritative identity check: ask the URL who it is. This both confirms the
	// candidate is reachable (we only monitor what we can poll) and catches a URL
	// that loops back to us, independent of our own (possibly wrong) advertise_url.
	canonical, ok := p.probeName(ctx, m.URL)
	if !ok || canonical == p.selfName {
		return nil
	}
	// Store under the node's own canonical name so the same node is named
	// consistently cluster-wide regardless of who gossiped it.
	for _, k := range known {
		if k.Name == canonical {
			return nil
		}
	}
	if err := p.db.UpsertPeer(storage.Peer{Name: canonical, URL: m.URL}); err != nil {
		log.Printf("cluster: gossip add %q failed: %v", canonical, err)
		return nil
	}
	log.Printf("cluster: discovered peer %q at %s via gossip", canonical, m.URL)
	return &storage.Peer{Name: canonical, URL: m.URL, Enabled: true}
}

// removePeer deletes a peer row and its stored data, and notifies the optional
// onPeerRemoved hook (wired to the alert engine's ForgetNode).
func (p *Poller) removePeer(name string) {
	_ = p.db.DeleteNodeData(name)
	_ = p.db.DeletePeer(name)
	_ = p.db.DeleteNodeAlias(name)
	if p.onPeerRemoved != nil {
		p.onPeerRemoved(name)
	}
	log.Printf("cluster: removed peer %q — its URL resolves to this node (self), added in error", name)
}

// probeName asks the node at rawURL for its canonical name via /api/peer/whoami,
// authenticating with the cycle's fallback secret. ok is false on any network,
// status, or decode error (treated as "unreachable / unknown").
func (p *Poller) probeName(ctx context.Context, rawURL string) (string, bool) {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodGet, strings.TrimRight(rawURL, "/")+"/api/peer/whoami", nil)
	if err != nil {
		return "", false
	}
	req.Header.Set(SecretHeader, p.fallback)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var who WhoAmI
	if err := json.NewDecoder(resp.Body).Decode(&who); err != nil {
		return "", false
	}
	if who.Name == "" {
		return "", false
	}
	return who.Name, true
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
