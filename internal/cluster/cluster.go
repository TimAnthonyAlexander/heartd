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
	CPUPercent  float64 `json:"cpu_percent"`
	MemUsed     uint64  `json:"mem_used"`
	MemTotal    uint64  `json:"mem_total"`
	MemPercent  float64 `json:"mem_percent"`
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	SwapUsed    uint64  `json:"swap_used"`
	SwapTotal   uint64  `json:"swap_total"`
	SwapPercent float64 `json:"swap_percent"`
	CollectedAt string  `json:"collected_at"`
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

// peerCPUState mirrors /api/peer/cpu.
type peerCPUState struct {
	User   float64 `json:"user"`
	System float64 `json:"system"`
	Nice   float64 `json:"nice"`
	Iowait float64 `json:"iowait"`
	Irq    float64 `json:"irq"`
	Steal  float64 `json:"steal"`
	Idle   float64 `json:"idle"`
	At     string  `json:"at"`
}

// peerDiskIO mirrors one element of /api/peer/diskio.
type peerDiskIO struct {
	Device         string `json:"device"`
	ReadBytesRate  uint64 `json:"read_bytes_rate"`
	WriteBytesRate uint64 `json:"write_bytes_rate"`
	ReadOpsRate    uint64 `json:"read_ops_rate"`
	WriteOpsRate   uint64 `json:"write_ops_rate"`
	At             string `json:"at"`
}

// peerProcess mirrors one element of /api/peer/processes.
type peerProcess struct {
	PID        int32   `json:"pid"`
	Name       string  `json:"name"`
	Command    string  `json:"command"`
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float64 `json:"mem_percent"`
	MemRSS     uint64  `json:"mem_rss"`
	At         string  `json:"at"`
}

// peerCore mirrors one element of /api/peer/cpu/cores.
type peerCore struct {
	Core    int     `json:"core"`
	Percent float64 `json:"percent"`
	At      string  `json:"at"`
}

// Poller announces this node to peers and polls them on an interval. The peer
// list is read fresh from storage each cycle, so peers added or removed in the
// dashboard take effect without a restart.
type Poller struct {
	db            *storage.DB
	selfName      string
	advertiseURL  string
	clusterSecret string // explicit shared secret from config; "" = derive from peers
	settings      *settings.Service
	client        *http.Client

	// fallback is the secret presented to peers that carry no per-link secret of
	// their own (gossip-discovered peers). Resolved once per cycle in Run, then
	// read by the per-peer goroutines pollAll spawns; the wg.Wait barrier each
	// cycle orders the write before the next cycle's reads, so no lock is needed.
	fallback string

	// onPeerRemoved, if set, is called with a peer's name after the poller removes
	// it (e.g. self-healing a phantom row that turned out to be this node). Wired
	// to the alert engine's ForgetNode so removed peers don't linger in alert state.
	onPeerRemoved func(name string)
}

const fallbackPollInterval = 15 * time.Second

// New builds a Poller. The peer list lives in storage (seeded once from config
// on first run). Alerting on peer reachability is handled by the alert Runner,
// which reads the persisted peer status.
//
// clusterSecret is the node's configured peer_secret. It is the trust anchor for
// membership gossip: auto-discovered and self-announced peers arrive without a
// per-link secret, so outbound calls to them fall back to a shared secret. When
// clusterSecret is empty the fallback is derived from the existing peer table
// (the secret the cluster already shares), so gossip needs no config at all in a
// cluster that already uses one secret across its links.
func New(db *storage.DB, selfName, advertiseURL, clusterSecret string, set *settings.Service) *Poller {
	return &Poller{
		db:            db,
		selfName:      selfName,
		advertiseURL:  advertiseURL,
		clusterSecret: clusterSecret,
		settings:      set,
		client:        &http.Client{Timeout: 8 * time.Second},
	}
}

// SetOnPeerRemoved registers a callback invoked with a peer's name after the
// poller removes it during self-healing. Optional; nil disables the hook.
func (p *Poller) SetOnPeerRemoved(fn func(name string)) { p.onPeerRemoved = fn }

// refreshFallback resolves the secret presented to secret-less (gossiped) peers
// for this cycle: the explicitly configured cluster secret when set, otherwise
// the secret the existing peer table already shares. Called once at the top of
// each cycle, before any peer goroutines read p.fallback.
func (p *Poller) refreshFallback() {
	if p.clusterSecret != "" {
		p.fallback = p.clusterSecret
		return
	}
	peers, err := p.db.ListPeers()
	if err != nil {
		return // keep the previous value rather than blanking it on a transient error
	}
	p.fallback = storage.CommonSecret(peers)
}

// effectiveSecret returns the secret to present when calling peer: its own
// per-link secret when set, otherwise this cycle's resolved fallback. This is
// what makes gossiped peers (which carry no secret) reachable across a cluster
// that shares one secret.
func (p *Poller) effectiveSecret(peer storage.Peer) string {
	if peer.Secret != "" {
		return peer.Secret
	}
	return p.fallback
}

// Run drives the cluster loop until ctx is cancelled. Each cycle it (1) announces
// this node to its peers, (2) gossips — learns about peers-of-peers and adds any
// it doesn't yet know — and (3) polls every known peer. The peer list is re-read
// from storage each cycle, so peers added (by hand or by gossip) take effect
// without a restart. Announce runs every cycle (not just at startup) so a peer
// added later is still told about this node, which is what lets a freshly added
// node bootstrap into the mesh.
func (p *Poller) Run(ctx context.Context) {
	for {
		p.refreshFallback()
		p.announceAll(ctx)
		p.gossip(ctx)
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
		req.Header.Set(SecretHeader, p.effectiveSecret(peer))
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
		Node:        peer.Name,
		CPUPercent:  m.CPUPercent,
		MemUsed:     m.MemUsed,
		MemTotal:    m.MemTotal,
		MemPercent:  m.MemPercent,
		Load1:       m.Load1,
		Load5:       m.Load5,
		Load15:      m.Load15,
		SwapUsed:    m.SwapUsed,
		SwapTotal:   m.SwapTotal,
		SwapPercent: m.SwapPercent,
		At:          at,
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
	p.storePeerCPUState(ctx, peer)
	p.storePeerCPUCores(ctx, peer)
	p.storePeerDiskIO(ctx, peer)
	p.storePeerProcesses(ctx, peer)
	p.storePeerIdentity(ctx, peer)

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
		// Accumulate peer capacity history locally so the same fill-rate forecast
		// path runs for peers as for the local node.
		_ = p.db.InsertDiskUsageSample(storage.DiskUsageSample{
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

// storePeerCPUState fetches a peer's latest CPU-state breakdown and records it
// under the peer's name.
func (p *Poller) storePeerCPUState(ctx context.Context, peer storage.Peer) {
	var c peerCPUState
	if err := p.getJSON(ctx, peer, "/api/peer/cpu", &c); err != nil {
		return
	}
	if c.At == "" {
		return // peer has no sample yet
	}
	at, perr := time.Parse(time.RFC3339, c.At)
	if perr != nil {
		at = time.Now().UTC()
	}
	_ = p.db.InsertCPUState(storage.CPUStateSample{
		Node: peer.Name, User: c.User, System: c.System, Nice: c.Nice,
		Iowait: c.Iowait, Irq: c.Irq, Steal: c.Steal, Idle: c.Idle, At: at,
	})
}

// storePeerCPUCores fetches a peer's latest per-core busy snapshot and records it
// under the peer's name, replacing the previous set. Best-effort: a failure does
// not affect the rest of the poll.
func (p *Poller) storePeerCPUCores(ctx context.Context, peer storage.Peer) {
	var cores []peerCore
	if err := p.getJSON(ctx, peer, "/api/peer/cpu/cores", &cores); err != nil {
		return
	}
	samples := make([]storage.CoreSample, 0, len(cores))
	for _, c := range cores {
		at, perr := time.Parse(time.RFC3339, c.At)
		if perr != nil {
			at = time.Now().UTC()
		}
		samples = append(samples, storage.CoreSample{
			Node:    peer.Name,
			Core:    c.Core,
			Percent: c.Percent,
			At:      at,
		})
	}
	_ = p.db.ReplacePerCore(peer.Name, samples)
}

// storePeerDiskIO fetches a peer's latest per-device disk I/O snapshot and
// records each device under the peer's name.
func (p *Poller) storePeerDiskIO(ctx context.Context, peer storage.Peer) {
	var ios []peerDiskIO
	if err := p.getJSON(ctx, peer, "/api/peer/diskio", &ios); err != nil {
		return
	}
	for _, io := range ios {
		at, perr := time.Parse(time.RFC3339, io.At)
		if perr != nil {
			at = time.Now().UTC()
		}
		_ = p.db.InsertDiskIOSample(storage.DiskIOSample{
			Node:           peer.Name,
			Device:         io.Device,
			ReadBytesRate:  io.ReadBytesRate,
			WriteBytesRate: io.WriteBytesRate,
			ReadOpsRate:    io.ReadOpsRate,
			WriteOpsRate:   io.WriteOpsRate,
			At:             at,
		})
	}
}

// storePeerProcesses fetches a peer's latest top-process snapshot and records it
// under the peer's name, replacing the previous set so the dashboard shows the
// peer's current processes. Best-effort: a failure does not affect the rest of
// the poll.
func (p *Poller) storePeerProcesses(ctx context.Context, peer storage.Peer) {
	var procs []peerProcess
	if err := p.getJSON(ctx, peer, "/api/peer/processes", &procs); err != nil {
		return
	}
	samples := make([]storage.ProcessSample, 0, len(procs))
	for _, pr := range procs {
		at, perr := time.Parse(time.RFC3339, pr.At)
		if perr != nil {
			at = time.Now().UTC()
		}
		samples = append(samples, storage.ProcessSample{
			Node:       peer.Name,
			PID:        pr.PID,
			Name:       pr.Name,
			Command:    pr.Command,
			CPUPercent: pr.CPUPercent,
			MemPercent: pr.MemPercent,
			MemRSS:     pr.MemRSS,
			At:         at,
		})
	}
	_ = p.db.ReplaceProcessTop(peer.Name, samples)
}

// storePeerIdentity caches a peer's self-advertised display name under that
// peer's local row name, so a name set once on a node converges to the same
// label on every dashboard. The peer is the single source of truth: a non-empty
// advertised name is cached verbatim, and an empty one clears the cache (the peer
// has no distinct label, so the dashboard shows its real name). The advertised
// value is already "" when no alias is set — it is NEVER the peer's hostname — so
// there is no comparison to make here. Best-effort: a failure does not affect the
// rest of the poll.
func (p *Poller) storePeerIdentity(ctx context.Context, peer storage.Peer) {
	var id Identity
	if err := p.getJSON(ctx, peer, "/api/peer/identity", &id); err != nil {
		return
	}
	if id.DisplayName != "" {
		if err := p.db.SetNodeAlias(peer.Name, id.DisplayName); err != nil {
			log.Printf("cluster: cache peer %q display name failed: %v", peer.Name, err)
		}
		return
	}
	if err := p.db.DeleteNodeAlias(peer.Name); err != nil {
		log.Printf("cluster: clear peer %q display name failed: %v", peer.Name, err)
	}
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
	req.Header.Set(SecretHeader, p.effectiveSecret(peer))

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
