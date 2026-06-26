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

	"github.com/timanthonyalexander/heartd/internal/settings"
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

// peerDisk mirrors one element of /api/peer/disk.
type peerDisk struct {
	Mount   string  `json:"mount"`
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
	At      string  `json:"at"`
}

// peerNet mirrors /api/peer/network.
type peerNet struct {
	RecvBytes uint64  `json:"recv_bytes"`
	SentBytes uint64  `json:"sent_bytes"`
	RecvRate  float64 `json:"recv_rate"`
	SentRate  float64 `json:"sent_rate"`
	At        string  `json:"at"`
}

// Poller announces this node to peers and polls them on an interval. The peer
// list is read fresh from storage each cycle, so peers added or removed in the
// dashboard take effect without a restart.
type Poller struct {
	db           *storage.DB
	selfName     string
	advertiseURL string
	settings     *settings.Service
	client       *http.Client
}

const fallbackPollInterval = 15 * time.Second

// New builds a Poller. The peer list lives in storage (seeded once from config
// on first run). Alerting on peer reachability is handled by the alert Runner,
// which reads the persisted peer status.
func New(db *storage.DB, selfName, advertiseURL string, set *settings.Service) *Poller {
	return &Poller{
		db:           db,
		selfName:     selfName,
		advertiseURL: advertiseURL,
		settings:     set,
		client:       &http.Client{Timeout: 8 * time.Second},
	}
}

// Run announces this node to its peers, then polls all peers once immediately and
// once per current interval until ctx is cancelled. The peer list is re-read from
// storage each cycle.
func (p *Poller) Run(ctx context.Context) {
	p.announceAll(ctx)

	for {
		p.pollAll(ctx)

		interval := p.settings.General().PeerPollInterval
		if interval <= 0 {
			interval = fallbackPollInterval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// peers returns the current ENABLED peer list from storage. Muted peers are
// skipped for polling and announcing (they remain in the DB so their shared
// secret still authenticates inbound node-to-node requests).
func (p *Poller) peers() []storage.Peer {
	peers, err := p.db.ListPeers()
	if err != nil {
		log.Printf("cluster: list peers failed: %v", err)
		return nil
	}
	out := peers[:0]
	for _, peer := range peers {
		if peer.Enabled {
			out = append(out, peer)
		}
	}
	return out
}

// announceAll tells each known peer about this node (best-effort).
func (p *Poller) announceAll(ctx context.Context) {
	if p.advertiseURL == "" {
		return // nothing useful to advertise
	}
	body, _ := json.Marshal(AnnounceRequest{Name: p.selfName, URL: p.advertiseURL})
	for _, peer := range p.peers() {
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

// pollAll polls every known peer concurrently.
func (p *Poller) pollAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, peer := range p.peers() {
		wg.Add(1)
		go func(peer storage.Peer) {
			defer wg.Done()
			p.pollPeer(ctx, peer)
		}(peer)
	}
	wg.Wait()
}

// pollPeer fetches a peer's metrics and checks, storing them under the peer's
// name, and records reachability. A metrics-fetch failure marks the peer down.
func (p *Poller) pollPeer(ctx context.Context, peer storage.Peer) {
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

	p.storePeerDisk(ctx, peer)
	p.storePeerNet(ctx, peer)

	_ = p.db.SetPeerStatus(peer.Name, "ok", time.Now().UTC(), "")
}

// storePeerDisk fetches a peer's disk usage and records it under the peer's name.
func (p *Poller) storePeerDisk(ctx context.Context, peer storage.Peer) {
	var disks []peerDisk
	if err := p.getJSON(ctx, peer, "/api/peer/disk", &disks); err != nil {
		return
	}
	mounts := make([]string, 0, len(disks))
	for _, d := range disks {
		mounts = append(mounts, d.Mount)
		at, perr := time.Parse(time.RFC3339, d.At)
		if perr != nil {
			at = time.Now().UTC()
		}
		_ = p.db.UpsertDiskStatus(storage.DiskStatus{
			Node: peer.Name, Mount: d.Mount, Used: d.Used, Total: d.Total, Percent: d.Percent, At: at,
		})
	}
	// Drop stale peer mounts no longer reported.
	_ = p.db.DeleteDiskStatusesExcept(peer.Name, mounts)
}

// storePeerNet fetches a peer's latest network sample and records it under the
// peer's name.
func (p *Poller) storePeerNet(ctx context.Context, peer storage.Peer) {
	var n peerNet
	if err := p.getJSON(ctx, peer, "/api/peer/network", &n); err != nil {
		return
	}
	if n.At == "" {
		return // peer has no sample yet
	}
	at, perr := time.Parse(time.RFC3339, n.At)
	if perr != nil {
		at = time.Now().UTC()
	}
	_ = p.db.InsertNetSample(storage.NetSample{
		Node: peer.Name, RecvBytes: n.RecvBytes, SentBytes: n.SentBytes,
		RecvRate: n.RecvRate, SentRate: n.SentRate, At: at,
	})
}

func (p *Poller) fetchMetrics(ctx context.Context, peer storage.Peer) (peerMetrics, error) {
	var m peerMetrics
	if err := p.getJSON(ctx, peer, "/api/peer/metrics", &m); err != nil {
		return peerMetrics{}, err
	}
	return m, nil
}

func (p *Poller) fetchChecks(ctx context.Context, peer storage.Peer) ([]peerCheck, error) {
	var c []peerCheck
	if err := p.getJSON(ctx, peer, "/api/peer/checks", &c); err != nil {
		return nil, err
	}
	return c, nil
}

func (p *Poller) getJSON(ctx context.Context, peer storage.Peer, path string, out any) error {
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
