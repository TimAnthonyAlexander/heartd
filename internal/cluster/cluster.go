// Package cluster implements heartd's multi-node behavior: announcing this node
// to its configured peers, and periodically polling those peers for their
// metrics and check statuses (which are stored locally under the peer's name so
// the dashboard can render every node uniformly).
package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// SecretHeader carries the shared secret on node-to-node requests.
const SecretHeader = "X-Heartd-Secret"

// AnnounceRequest is the body a node POSTs to a peer's /api/peer/announce.
type AnnounceRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// peerMetrics mirrors the JSON returned by /api/peer/metrics.
type peerMetrics struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsed    uint64  `json:"mem_used"`
	MemTotal   uint64  `json:"mem_total"`
	MemPercent float64 `json:"mem_percent"`
	CollectedAt string `json:"collected_at"`
}

// peerCheck mirrors one element of the JSON returned by /api/peer/checks.
type peerCheck struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	LatencyMS   int64  `json:"latency_ms"`
	LastChecked string `json:"last_checked"`
}

// Poller announces this node to peers and polls them on an interval.
type Poller struct {
	db           *storage.DB
	selfName     string
	advertiseURL string
	interval     time.Duration
	peers        []config.Peer
	client       *http.Client
}

// New builds a Poller for the configured peers.
func New(db *storage.DB, selfName, advertiseURL string, interval time.Duration, peers []config.Peer) *Poller {
	return &Poller{
		db:           db,
		selfName:     selfName,
		advertiseURL: advertiseURL,
		interval:     interval,
		peers:        peers,
		client:       &http.Client{Timeout: 8 * time.Second},
	}
}

// Run seeds peers into storage, announces this node to them, then polls all
// peers once immediately and once per interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.seedPeers()
	p.announceAll(ctx)
	p.pollAll(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollAll(ctx)
		}
	}
}

// seedPeers writes the configured peers (with their secrets) into storage.
func (p *Poller) seedPeers() {
	for _, peer := range p.peers {
		if err := p.db.UpsertPeer(storage.Peer{Name: peer.Name, URL: peer.URL, Secret: peer.Secret}); err != nil {
			log.Printf("cluster: seed peer %q failed: %v", peer.Name, err)
		}
	}
}

// announceAll tells each configured peer about this node (best-effort).
func (p *Poller) announceAll(ctx context.Context) {
	if p.advertiseURL == "" {
		return // nothing useful to advertise
	}
	body, _ := json.Marshal(AnnounceRequest{Name: p.selfName, URL: p.advertiseURL})
	for _, peer := range p.peers {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, peer.URL+"/api/peer/announce", bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(SecretHeader, peer.Secret)
		resp, err := p.client.Do(req)
		if err != nil {
			log.Printf("cluster: announce to %q failed: %v", peer.Name, err)
			continue
		}
		_ = resp.Body.Close()
	}
}

// pollAll polls every configured peer concurrently.
func (p *Poller) pollAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, peer := range p.peers {
		wg.Add(1)
		go func(peer config.Peer) {
			defer wg.Done()
			p.pollPeer(ctx, peer)
		}(peer)
	}
	wg.Wait()
}

// pollPeer fetches a peer's metrics and checks, storing them under the peer's
// name, and records reachability. A metrics-fetch failure marks the peer down.
func (p *Poller) pollPeer(ctx context.Context, peer config.Peer) {
	m, err := p.fetchMetrics(ctx, peer)
	if err != nil {
		// Preserve last-seen (zero time) and record the error as "down".
		_ = p.db.SetPeerStatus(peer.Name, "down", time.Time{}, err.Error())
		return
	}

	at, perr := time.Parse(time.RFC3339, m.CollectedAt)
	if perr != nil {
		at = time.Now().UTC()
	}
	if err := p.db.InsertMetric(storage.MetricSample{
		Node:       peer.Name,
		CPUPercent: m.CPUPercent,
		MemUsed:    m.MemUsed,
		MemTotal:   m.MemTotal,
		MemPercent: m.MemPercent,
		At:         at,
	}); err != nil {
		log.Printf("cluster: store peer %q metrics failed: %v", peer.Name, err)
	}

	// Checks are best-effort; a failure here does not mark the node down since
	// metrics already succeeded (the node is reachable).
	if checks, err := p.fetchChecks(ctx, peer); err == nil {
		for _, c := range checks {
			at, perr := time.Parse(time.RFC3339, c.LastChecked)
			if perr != nil {
				at = time.Now().UTC()
			}
			_ = p.db.UpsertCheckStatus(storage.CheckStatus{
				Node:      peer.Name,
				Name:      c.Name,
				Type:      c.Type,
				Status:    c.Status,
				Detail:    c.Detail,
				LatencyMS: c.LatencyMS,
				At:        at,
			})
		}
	}

	_ = p.db.SetPeerStatus(peer.Name, "ok", time.Now().UTC(), "")
}

func (p *Poller) fetchMetrics(ctx context.Context, peer config.Peer) (peerMetrics, error) {
	var m peerMetrics
	if err := p.getJSON(ctx, peer, "/api/peer/metrics", &m); err != nil {
		return peerMetrics{}, err
	}
	return m, nil
}

func (p *Poller) fetchChecks(ctx context.Context, peer config.Peer) ([]peerCheck, error) {
	var c []peerCheck
	if err := p.getJSON(ctx, peer, "/api/peer/checks", &c); err != nil {
		return nil, err
	}
	return c, nil
}

func (p *Poller) getJSON(ctx context.Context, peer config.Peer, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peer.URL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set(SecretHeader, peer.Secret)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("peer returned %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
