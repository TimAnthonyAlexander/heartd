// Package server wires the heartd HTTP API and dashboard together.
package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/timanthonyalexander/heartd/internal/alert"
	"github.com/timanthonyalexander/heartd/internal/auth"
	"github.com/timanthonyalexander/heartd/internal/cluster"
	"github.com/timanthonyalexander/heartd/internal/metrics"
	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
	"github.com/timanthonyalexander/heartd/internal/web"
)

// sessionCookie is the name of the HttpOnly session cookie.
const sessionCookie = "heartd_session"

// Config holds runtime settings for the server.
type Config struct {
	NodeName string
	DB       *storage.DB
	Settings *settings.Service
	Auth     *auth.Service
	// Engine is the alert engine, used to forget a node's state when it is
	// removed. May be nil when alerting is disabled.
	Engine *alert.Engine
	// Coordinator deduplicates peer alerts across nodes; it backs the
	// /api/peer/alert-claim and /api/peer/alert-sent endpoints. May be nil.
	Coordinator *alert.Coordinator
	// Headless serves only /api/health + /api/peer/* (no dashboard / auth / user
	// endpoints) — the node is an agent managed from another node.
	Headless bool
	// ExtraSecrets are accepted node-to-node shared secrets beyond those derived
	// from configured peers (e.g. a headless agent's `peer_secret`).
	ExtraSecrets []string
	// AdvertiseURL is the URL other nodes use to reach this one. It is published
	// in this node's /api/peer/members view so peers-of-peers can discover it.
	// Empty = this node omits itself from membership (it can't be polled).
	AdvertiseURL string
}

// New builds the root HTTP handler: REST API under /api and the embedded
// dashboard for everything else.
func New(cfg Config) http.Handler {
	s := &server{
		cfg:         cfg,
		proxyClient: &http.Client{Timeout: 15 * time.Second},
	}
	mux := http.NewServeMux()

	// Liveness is always public.
	mux.HandleFunc("GET /api/health", s.handleHealth)

	// The dashboard, auth flow, and session-protected endpoints are skipped in
	// headless (agent) mode — only health and the node-to-node /api/peer/* API
	// below are served, so the node is configured remotely from another node.
	if !cfg.Headless {
		s.registerDashboardRoutes(mux)
	}

	// Node-to-node endpoints, protected by the shared secret. Always served (this
	// is the only API surface in headless mode).
	mux.Handle("POST /api/peer/announce", s.requireSecret(http.HandlerFunc(s.handlePeerAnnounce)))
	mux.Handle("GET /api/peer/members", s.requireSecret(http.HandlerFunc(s.handlePeerMembers)))
	mux.Handle("GET /api/peer/whoami", s.requireSecret(http.HandlerFunc(s.handlePeerWhoami)))
	mux.Handle("GET /api/peer/identity", s.requireSecret(http.HandlerFunc(s.handlePeerIdentity)))
	mux.Handle("GET /api/peer/metrics", s.requireSecret(http.HandlerFunc(s.handlePeerMetrics)))
	mux.Handle("GET /api/peer/checks", s.requireSecret(http.HandlerFunc(s.handlePeerChecks)))
	mux.Handle("GET /api/peer/disk", s.requireSecret(http.HandlerFunc(s.handlePeerDisk)))
	mux.Handle("GET /api/peer/network", s.requireSecret(http.HandlerFunc(s.handlePeerNetwork)))
	mux.Handle("GET /api/peer/diskio", s.requireSecret(http.HandlerFunc(s.handlePeerDiskIO)))

	// Cross-node alert dedup: peers claim an incident and announce delivery here
	// so only one node mails about a shared event (e.g. a third node going down).
	mux.Handle("POST /api/peer/alert-claim", s.requireSecret(http.HandlerFunc(s.handleAlertClaim)))
	mux.Handle("POST /api/peer/alert-sent", s.requireSecret(http.HandlerFunc(s.handleAlertSent)))

	// Node-to-node settings: the receive side of the proxy above. Same handlers
	// the local node uses, operating on THIS node's own settings service.
	mux.Handle("GET /api/peer/settings", s.requireSecret(http.HandlerFunc(s.handleGetSettings)))
	mux.Handle("PUT /api/peer/settings/general", s.requireSecret(http.HandlerFunc(s.handlePutGeneral)))
	mux.Handle("PUT /api/peer/settings/notify", s.requireSecret(http.HandlerFunc(s.handlePutNotify)))
	mux.Handle("PUT /api/peer/settings/alias", s.requireSecret(http.HandlerFunc(s.handlePeerSetAlias)))
	mux.Handle("POST /api/peer/settings/notify/test", s.requireSecret(http.HandlerFunc(s.handleTestNotify)))
	mux.Handle("POST /api/peer/settings/checks", s.requireSecret(http.HandlerFunc(s.handleCreateCheck)))
	mux.Handle("PUT /api/peer/settings/checks/{id}", s.requireSecret(http.HandlerFunc(s.handleUpdateCheck)))
	mux.Handle("DELETE /api/peer/settings/checks/{id}", s.requireSecret(http.HandlerFunc(s.handleDeleteCheck)))
	mux.Handle("POST /api/peer/settings/alerts", s.requireSecret(http.HandlerFunc(s.handleCreateAlert)))
	mux.Handle("PUT /api/peer/settings/alerts/{id}", s.requireSecret(http.HandlerFunc(s.handleUpdateAlert)))
	mux.Handle("DELETE /api/peer/settings/alerts/{id}", s.requireSecret(http.HandlerFunc(s.handleDeleteAlert)))

	// Unknown API paths return JSON 404 rather than falling through to the
	// SPA handler (which would serve index.html with a 200).
	mux.HandleFunc("/api/", handleAPINotFound)

	if cfg.Headless {
		// No dashboard in agent mode; a friendly note for humans hitting the port.
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, "heartd — headless agent. No dashboard on this node; manage it from another node.\n")
		})
	} else {
		// Everything not under /api is the dashboard.
		mux.Handle("/", web.Handler())
	}

	return mux
}

// registerDashboardRoutes wires the auth flow, the embedded dashboard's data
// endpoints, and the management API. Skipped in headless mode.
func (s *server) registerDashboardRoutes(mux *http.ServeMux) {
	// Public auth flow.
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/init", s.handleAuthInit)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)

	// Protected: every data endpoint requires a valid session.
	protect := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, s.requireAuth(h))
	}
	protect("GET /api/nodes", s.handleNodes)

	// Cluster topology (peer) management — the local node's own peer list.
	protect("GET /api/peers", s.handleListPeers)
	protect("POST /api/peers", s.handleCreatePeer)
	protect("PUT /api/peers/{name}", s.handleUpdatePeer)
	protect("DELETE /api/peers/{name}", s.handleDeletePeer)

	// User administration (every user is an admin, so these are session-gated).
	protect("GET /api/users", s.handleListUsers)
	protect("POST /api/users", s.handleCreateUser)
	protect("DELETE /api/users/{username}", s.handleDeleteUser)
	protect("PUT /api/users/{username}/password", s.handleSetUserPassword)

	// Display alias (rename) — always applied locally in this node's DB, for the
	// local node or any peer; never proxied (it relabels the node only here).
	protect("PUT /api/nodes/{name}/alias", s.handleSetNodeAlias)

	protect("GET /api/nodes/{name}/metrics", s.handleMetrics)
	protect("GET /api/nodes/{name}/metrics/history", s.handleHistory)
	protect("GET /api/nodes/{name}/checks", s.handleChecks)
	protect("GET /api/nodes/{name}/disk", s.handleDisk)
	protect("GET /api/nodes/{name}/network", s.handleNetwork)
	protect("GET /api/nodes/{name}/network/history", s.handleNetworkHistory)
	protect("GET /api/nodes/{name}/diskio", s.handleDiskIO)
	protect("GET /api/nodes/{name}/diskio/history", s.handleDiskIOHistory)

	// Alert activity for a node: what is firing right now (from the engine's live
	// in-memory state) and the recent firing/recovered history (from storage).
	protect("GET /api/nodes/{name}/alerts/active", s.handleAlertsActive)
	protect("GET /api/nodes/{name}/alerts/history", s.handleAlertsHistory)

	// Runtime configuration, addressed per node. For the local node these operate
	// on its own settings service; for a peer they proxy the same request to that
	// peer's /api/peer/settings/* over the shared-secret link.
	protect("GET /api/nodes/{name}/settings", s.dispatchNode(s.handleGetSettings, "/api/peer/settings"))
	protect("PUT /api/nodes/{name}/settings/general", s.dispatchNode(s.handlePutGeneral, "/api/peer/settings/general"))
	protect("PUT /api/nodes/{name}/settings/notify", s.dispatchNode(s.handlePutNotify, "/api/peer/settings/notify"))
	protect("POST /api/nodes/{name}/settings/notify/test", s.dispatchNode(s.handleTestNotify, "/api/peer/settings/notify/test"))
	protect("POST /api/nodes/{name}/settings/checks", s.dispatchNode(s.handleCreateCheck, "/api/peer/settings/checks"))
	protect("PUT /api/nodes/{name}/settings/checks/{id}", s.dispatchNode(s.handleUpdateCheck, "/api/peer/settings/checks"))
	protect("DELETE /api/nodes/{name}/settings/checks/{id}", s.dispatchNode(s.handleDeleteCheck, "/api/peer/settings/checks"))
	protect("POST /api/nodes/{name}/settings/alerts", s.dispatchNode(s.handleCreateAlert, "/api/peer/settings/alerts"))
	protect("PUT /api/nodes/{name}/settings/alerts/{id}", s.dispatchNode(s.handleUpdateAlert, "/api/peer/settings/alerts"))
	protect("DELETE /api/nodes/{name}/settings/alerts/{id}", s.dispatchNode(s.handleDeleteAlert, "/api/peer/settings/alerts"))
}

type server struct {
	cfg         Config
	proxyClient *http.Client
}

// dispatchNode routes a per-node settings request: if {name} is the local node,
// the local handler runs against this node's own settings service; otherwise the
// request is proxied to that peer's corresponding /api/peer/settings path. When a
// {id} path value is present (check update/delete) it is appended to peerPath.
func (s *server) dispatchNode(local http.HandlerFunc, peerPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("name") == s.cfg.NodeName {
			local(w, r)
			return
		}
		path := peerPath
		if id := r.PathValue("id"); id != "" {
			path = peerPath + "/" + id
		}
		s.proxyToPeer(w, r, path)
	}
}

// proxyToPeer forwards the current request (method + body) to the named peer's
// path, attaching the shared secret, and streams the peer's response back. The
// peer name comes from the {name} path value.
func (s *server) proxyToPeer(w http.ResponseWriter, r *http.Request, path string) {
	name := r.PathValue("name")
	peer, ok, err := s.peerByName(name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown node"})
		return
	}
	secret := s.outboundSecret(peer)
	if peer.URL == "" || secret == "" {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "peer is not configured for remote edits (missing url or shared secret)"})
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 14*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, peer.URL+path, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(cluster.SecretHeader, secret)

	resp, err := s.proxyClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "peer unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// peerByName looks up a configured peer's connection details by name.
func (s *server) peerByName(name string) (storage.Peer, bool, error) {
	peers, err := s.cfg.DB.ListPeers()
	if err != nil {
		return storage.Peer{}, false, err
	}
	for _, p := range peers {
		if p.Name == name {
			return p, true, nil
		}
	}
	return storage.Peer{}, false, nil
}

// outboundSecret returns the secret to present to a peer on node-to-node calls:
// the peer's own per-link secret, else the configured cluster secret, else the
// secret the peer table already shares. Mirrors the poller's fallback so a
// gossip-discovered peer (which carries no per-link secret) can still be renamed
// and remotely managed.
func (s *server) outboundSecret(peer storage.Peer) string {
	if peer.Secret != "" {
		return peer.Secret
	}
	for _, sec := range s.cfg.ExtraSecrets {
		if sec != "" {
			return sec
		}
	}
	if peers, err := s.cfg.DB.ListPeers(); err == nil {
		return storage.CommonSecret(peers)
	}
	return ""
}

// pushAliasToPeer writes a node's display name on the node itself, over the
// shared-secret link, so the rename is authoritative and propagates to the whole
// cluster from its owner. Returns an error string suitable for the API response,
// or "" on success.
func (s *server) pushAliasToPeer(peer storage.Peer, alias string) string {
	secret := s.outboundSecret(peer)
	if peer.URL == "" || secret == "" {
		return "peer is not configured for remote edits (missing url or shared secret)"
	}
	body, _ := json.Marshal(aliasInput{Alias: alias})
	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, peer.URL+"/api/peer/settings/alias", bytes.NewReader(body))
	if err != nil {
		return err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(cluster.SecretHeader, secret)
	resp, err := s.proxyClient.Do(req)
	if err != nil {
		return "peer unreachable: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("peer rejected rename (status %d)", resp.StatusCode)
	}
	return ""
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// node is the dashboard's view of a single heartd instance (local or peer).
type node struct {
	Name   string `json:"name"`
	Alias  string `json:"alias,omitempty"` // UI display name; empty = use Name
	Local  bool   `json:"local"`
	Status string `json:"status"` // ok | down | unknown
	Muted  bool   `json:"muted"`  // peer muted from this node's perspective
}

// handleNodes returns the local node plus all known peers with their current
// reachability, so each node's dashboard is a full cluster view.
func (s *server) handleNodes(w http.ResponseWriter, r *http.Request) {
	aliases, err := s.cfg.DB.NodeAliases()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := []node{{Name: s.cfg.NodeName, Alias: aliases[s.cfg.NodeName], Local: true, Status: "ok"}}

	peers, err := s.cfg.DB.ListPeers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, p := range peers {
		status := p.Status
		if status == "" {
			status = "unknown"
		}
		out = append(out, node{Name: p.Name, Alias: aliases[p.Name], Local: false, Status: status, Muted: !p.Enabled})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMetrics returns the most recent sample for the named node. It reads the
// latest persisted sample (written by the collector loop) so the headline value
// matches the sparkline and is consistent across dashboard polls. If no sample
// exists yet (fresh start, local node only), it falls back to a live read.
func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	latest, ok, err := s.cfg.DB.LatestMetric(name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if ok {
		writeJSON(w, http.StatusOK, metrics.Snapshot{
			CPUPercent:  latest.CPUPercent,
			MemUsed:     latest.MemUsed,
			MemTotal:    latest.MemTotal,
			MemPercent:  latest.MemPercent,
			Load1:       latest.Load1,
			Load5:       latest.Load5,
			Load15:      latest.Load15,
			SwapUsed:    latest.SwapUsed,
			SwapTotal:   latest.SwapTotal,
			SwapPercent: latest.SwapPercent,
			CollectedAt: latest.At.UTC().Format(time.RFC3339),
		})
		return
	}

	// No persisted sample yet — only the local node can be sampled live.
	if name != s.cfg.NodeName {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown node"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snap, err := metrics.Collect(ctx, 500*time.Millisecond)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// historyPoint is one persisted sample, trimmed to what the sparklines need.
type historyPoint struct {
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
	At          string  `json:"at"`
}

// handleHistory returns persisted samples for a node within a recent window.
// Query params: minutes (default 60), limit (default 200).
func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var (
		samples []storage.MetricSample
		err     error
	)
	if from, to, ok := historyWindow(r); ok {
		samples, err = s.cfg.DB.MetricsWindow(name, from, to, maxHistoryPoints)
	} else {
		minutes := queryInt(r, "minutes", 60)
		limit := queryInt(r, "limit", 200)
		since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
		samples, err = s.cfg.DB.RecentMetrics(name, since, limit)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	points := make([]historyPoint, 0, len(samples))
	for _, m := range samples {
		points = append(points, historyPoint{
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
			At:          m.At.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, points)
}

// checkDTO is the dashboard's view of one service check's current status.
type checkDTO struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"` // ok | failing | unknown
	Detail      string `json:"detail"`
	LatencyMS   int64  `json:"latency_ms"`
	LastChecked string `json:"last_checked"` // RFC3339, empty if never run
}

// handleChecks returns the current status of each check for a node. For the
// local node the configured check set is merged with stored status so checks
// that haven't run yet appear as "unknown". For other nodes (peers) it returns
// whatever statuses have been stored.
func (s *server) handleChecks(w http.ResponseWriter, r *http.Request) {
	out, err := s.checksForNode(r.PathValue("name"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// checksForNode builds the check DTOs for a node. For the local node it merges
// the configured check set with stored status (so not-yet-run checks show
// "unknown"); for other nodes it returns the stored statuses.
func (s *server) checksForNode(name string) ([]checkDTO, error) {
	stored, err := s.cfg.DB.CheckStatuses(name)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]storage.CheckStatus, len(stored))
	for _, st := range stored {
		byName[st.Name] = st
	}

	out := make([]checkDTO, 0)
	seen := make(map[string]bool)

	if name == s.cfg.NodeName {
		for _, c := range s.cfg.Settings.Checks() {
			seen[c.Name] = true
			if st, ok := byName[c.Name]; ok {
				out = append(out, toCheckDTO(st))
			} else {
				out = append(out, checkDTO{Name: c.Name, Type: c.Type, Status: "unknown"})
			}
		}
	}
	// Include any stored statuses not covered by the configured set.
	for _, st := range stored {
		if !seen[st.Name] {
			out = append(out, toCheckDTO(st))
		}
	}
	return out, nil
}

func toCheckDTO(st storage.CheckStatus) checkDTO {
	return checkDTO{
		Name:        st.Name,
		Type:        st.Type,
		Status:      st.Status,
		Detail:      st.Detail,
		LatencyMS:   st.LatencyMS,
		LastChecked: st.At.UTC().Format(time.RFC3339),
	}
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func queryInt64(r *http.Request, key string) (int64, bool) {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

const (
	// maxHistoryPoints caps the number of points a windowed history query returns,
	// via time-bucket downsampling, so a 7-day range stays renderable.
	maxHistoryPoints = 500
	// maxHistoryWindow is the furthest back a windowed query may reach (the
	// retention horizon), clamping the lower bound of a client-supplied window.
	maxHistoryWindow = 7 * 24 * time.Hour
)

// historyWindow parses optional from/to (unix epoch seconds) query params shared
// by the metrics/network/diskio history endpoints. It returns the [from, to]
// window and true only when both are present and to > from; the lower bound is
// clamped to the retention horizon so a stale client can't request an unbounded
// span. When absent the caller keeps its legacy minutes/limit behavior.
func historyWindow(r *http.Request) (time.Time, time.Time, bool) {
	fromSec, okF := queryInt64(r, "from")
	toSec, okT := queryInt64(r, "to")
	if !okF || !okT || toSec <= fromSec {
		return time.Time{}, time.Time{}, false
	}
	from := time.Unix(fromSec, 0).UTC()
	to := time.Unix(toSec, 0).UTC()
	if earliest := time.Now().UTC().Add(-maxHistoryWindow); from.Before(earliest) {
		from = earliest
	}
	return from, to, true
}

// diskDTO is one mount's current usage.
type diskDTO struct {
	Mount   string  `json:"mount"`
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
	At      string  `json:"at"`
}

// netDTO is a node's latest network throughput.
type netDTO struct {
	RecvBytes uint64  `json:"recv_bytes"`
	SentBytes uint64  `json:"sent_bytes"`
	RecvRate  float64 `json:"recv_rate"`
	SentRate  float64 `json:"sent_rate"`
	At        string  `json:"at"`
}

type netHistoryPoint struct {
	RecvRate float64 `json:"recv_rate"`
	SentRate float64 `json:"sent_rate"`
	At       string  `json:"at"`
}

// diskIODTO is one physical device's latest disk throughput/IOPS.
type diskIODTO struct {
	Device         string `json:"device"`
	ReadBytesRate  uint64 `json:"read_bytes_rate"`
	WriteBytesRate uint64 `json:"write_bytes_rate"`
	ReadOpsRate    uint64 `json:"read_ops_rate"`
	WriteOpsRate   uint64 `json:"write_ops_rate"`
	At             string `json:"at"`
}

// diskIOHistoryPoint is throughput/IOPS aggregated across devices at one instant.
type diskIOHistoryPoint struct {
	ReadBytesRate  uint64 `json:"read_bytes_rate"`
	WriteBytesRate uint64 `json:"write_bytes_rate"`
	ReadOpsRate    uint64 `json:"read_ops_rate"`
	WriteOpsRate   uint64 `json:"write_ops_rate"`
	At             string `json:"at"`
}

func (s *server) diskForNode(name string) ([]diskDTO, error) {
	rows, err := s.cfg.DB.DiskStatuses(name)
	if err != nil {
		return nil, err
	}
	out := make([]diskDTO, 0, len(rows))
	for _, d := range rows {
		out = append(out, diskDTO{
			Mount: d.Mount, Used: d.Used, Total: d.Total, Percent: d.Percent,
			At: d.At.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

func (s *server) netForNode(name string) (netDTO, bool, error) {
	n, ok, err := s.cfg.DB.LatestNetSample(name)
	if err != nil || !ok {
		return netDTO{}, ok, err
	}
	return netDTO{
		RecvBytes: n.RecvBytes, SentBytes: n.SentBytes,
		RecvRate: n.RecvRate, SentRate: n.SentRate,
		At: n.At.UTC().Format(time.RFC3339),
	}, true, nil
}

func (s *server) handleDisk(w http.ResponseWriter, r *http.Request) {
	out, err := s.diskForNode(r.PathValue("name"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	n, ok, err := s.netForNode(r.PathValue("name"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *server) handleNetworkHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var (
		samples []storage.NetSample
		err     error
	)
	if from, to, ok := historyWindow(r); ok {
		samples, err = s.cfg.DB.NetSamplesWindow(name, from, to, maxHistoryPoints)
	} else {
		minutes := queryInt(r, "minutes", 60)
		limit := queryInt(r, "limit", 200)
		since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
		samples, err = s.cfg.DB.RecentNetSamples(name, since, limit)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	points := make([]netHistoryPoint, 0, len(samples))
	for _, n := range samples {
		points = append(points, netHistoryPoint{
			RecvRate: n.RecvRate, SentRate: n.SentRate,
			At: n.At.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *server) handlePeerDisk(w http.ResponseWriter, r *http.Request) {
	out, err := s.diskForNode(s.cfg.NodeName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handlePeerNetwork(w http.ResponseWriter, r *http.Request) {
	n, ok, err := s.netForNode(s.cfg.NodeName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *server) diskIOForNode(name string) ([]diskIODTO, error) {
	rows, err := s.cfg.DB.LatestDiskIOSamples(name)
	if err != nil {
		return nil, err
	}
	out := make([]diskIODTO, 0, len(rows))
	for _, d := range rows {
		out = append(out, diskIODTO{
			Device:         d.Device,
			ReadBytesRate:  d.ReadBytesRate,
			WriteBytesRate: d.WriteBytesRate,
			ReadOpsRate:    d.ReadOpsRate,
			WriteOpsRate:   d.WriteOpsRate,
			At:             d.At.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

func (s *server) handleDiskIO(w http.ResponseWriter, r *http.Request) {
	out, err := s.diskIOForNode(r.PathValue("name"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleDiskIOHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var (
		points []storage.DiskIOPoint
		err    error
	)
	if from, to, ok := historyWindow(r); ok {
		points, err = s.cfg.DB.DiskIOWindow(name, from, to, maxHistoryPoints)
	} else {
		minutes := queryInt(r, "minutes", 60)
		limit := queryInt(r, "limit", 200)
		since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
		points, err = s.cfg.DB.DiskIOHistory(name, since, limit)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]diskIOHistoryPoint, 0, len(points))
	for _, p := range points {
		out = append(out, diskIOHistoryPoint{
			ReadBytesRate:  p.ReadBytesRate,
			WriteBytesRate: p.WriteBytesRate,
			ReadOpsRate:    p.ReadOpsRate,
			WriteOpsRate:   p.WriteOpsRate,
			At:             p.At.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handlePeerDiskIO(w http.ResponseWriter, r *http.Request) {
	out, err := s.diskIOForNode(s.cfg.NodeName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Alert activity ---

// activeAlertDTO is the dashboard's view of one alert that is firing right now.
type activeAlertDTO struct {
	Entity   string `json:"entity"`
	Source   string `json:"source"`
	Subject  string `json:"subject"`
	Severity string `json:"severity"` // warning | critical
	Detail   string `json:"detail"`
	Since    string `json:"since"` // RFC3339, when it started firing ("" if unknown)
}

// alertEventDTO is the dashboard's view of one past firing/recovered transition.
type alertEventDTO struct {
	Node       string `json:"node"`
	RuleID     string `json:"rule_id"`
	RuleSource string `json:"rule_source"`
	Entity     string `json:"entity"`
	Severity   string `json:"severity"`
	State      string `json:"state"` // firing | recovered
	Subject    string `json:"subject"`
	Detail     string `json:"detail"`
	At         string `json:"at"` // RFC3339
}

// handleAlertsActive returns the alerts currently firing for a node, as tracked
// live in the engine's in-memory state. An empty list (or absent engine) yields
// an empty array, not an error — "nothing firing" is the steady state.
func (s *server) handleAlertsActive(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	out := make([]activeAlertDTO, 0)
	if s.cfg.Engine != nil {
		for _, a := range s.cfg.Engine.ActiveAlertsForNode(name) {
			since := ""
			if !a.BreachSince.IsZero() {
				since = a.BreachSince.UTC().Format(time.RFC3339)
			}
			out = append(out, activeAlertDTO{
				Entity:   a.Entity,
				Source:   a.Source,
				Subject:  a.Subject,
				Severity: a.Severity,
				Detail:   a.Detail,
				Since:    since,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAlertsHistory returns past firing/recovered events for a node, most
// recent first. Query params: minutes (default 1440 = 24h), limit (default 100).
func (s *server) handleAlertsHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	minutes := queryInt(r, "minutes", 1440)
	limit := queryInt(r, "limit", 100)

	since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
	events, err := s.cfg.DB.AlertEventHistory(name, since, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]alertEventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, alertEventDTO{
			Node:       e.Node,
			RuleID:     e.RuleID,
			RuleSource: e.RuleSource,
			Entity:     e.Entity,
			Severity:   e.Severity,
			State:      e.State,
			Subject:    e.Subject,
			Detail:     e.Detail,
			At:         e.At.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Authentication ---

type authStatusResp struct {
	Initialized   bool   `json:"initialized"`
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.cfg.Auth.Initialized()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := authStatusResp{Initialized: initialized}
	if user, ok := s.currentUser(r); ok {
		resp.Authenticated = true
		resp.Username = user.Username
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleAuthInit(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}
	user, token, err := s.cfg.Auth.CreateFirstUser(creds.Username, creds.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAlreadyInitialized):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already initialized"})
		case errors.Is(err, auth.ErrWeakPassword):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		case errors.Is(err, auth.ErrInvalidUsername):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create user"})
		}
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]string{"username": user.Username})
}

func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}
	user, token, err := s.cfg.Auth.Login(creds.Username, creds.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]string{"username": user.Username})
}

func (s *server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.cfg.Auth.Logout(c.Value)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// requireAuth wraps a handler so it only runs for a request with a valid
// session. Otherwise it returns 401 and reveals nothing.
func (s *server) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.currentUser(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// currentUser resolves the session cookie to a user, if valid.
func (s *server) currentUser(r *http.Request) (auth.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.User{}, false
	}
	user, ok, err := s.cfg.Auth.UserForSession(c.Value)
	if err != nil || !ok {
		return auth.User{}, false
	}
	return user, true
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	var creds credentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&creds); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return credentials{}, false
	}
	return creds, true
}

func (s *server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL / time.Second),
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// requireSecret wraps a handler so it only runs when the request carries a
// valid shared secret in the X-Heartd-Secret header. The comparison is
// constant-time. If no secrets are configured, all node-to-node requests are
// rejected.
func (s *server) requireSecret(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented := r.Header.Get(cluster.SecretHeader)
		if presented == "" || !s.validSecret(presented) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid or missing secret"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validSecret reports whether the presented secret matches any known peer's
// shared secret. Secrets are read fresh from storage so a peer added or edited in
// the dashboard is accepted immediately, without a restart. The comparison is
// constant-time and runs against every peer to avoid early-exit timing leaks.
func (s *server) validSecret(presented string) bool {
	peers, err := s.cfg.DB.ListPeers()
	if err != nil {
		return false
	}
	ok := false
	for _, peer := range peers {
		if peer.Secret == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(presented), []byte(peer.Secret)) == 1 {
			ok = true
		}
	}
	// Also accept any statically-configured secrets (e.g. a headless agent's
	// peer_secret), so an agent can be polled without listing a peer it polls.
	for _, secret := range s.cfg.ExtraSecrets {
		if secret == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(presented), []byte(secret)) == 1 {
			ok = true
		}
	}
	return ok
}

// handlePeerAnnounce records a peer that announced itself to this node.
//
// Announces auto-create nodes (so a genuinely new peer shows up without manual
// entry), but a node announces under its OWN config name + advertise_url — which
// may differ from how you added it here. To avoid duplicates, a NEW name is only
// created when no node already lives at the same address: e.g. a laptop whose
// config name is "web-01" advertising http://localhost:9300 must not spawn a
// "web-01" twin when you already added that exact address as "macbook".
func (s *server) handlePeerAnnounce(w http.ResponseWriter, r *http.Request) {
	var req cluster.AnnounceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Name == "" || req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and url are required"})
		return
	}

	// Refresh a peer we already track by this name; otherwise it's a candidate
	// new node — and we only create it if its address isn't already represented.
	if _, known, err := s.cfg.DB.GetPeer(req.Name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	} else if !known {
		dup, err := s.peerAtSameAddr(req.URL)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if dup != "" {
			// Already tracked under another name — ignore, don't create a twin.
			log.Printf("server: ignoring announce of %q at %s — already tracked as %q", req.Name, req.URL, dup)
			writeJSON(w, http.StatusOK, map[string]string{"name": s.cfg.NodeName})
			return
		}
	}

	// Don't grant a secret from an announce; preserve any existing one.
	if err := s.cfg.DB.UpsertPeer(storage.Peer{Name: req.Name, URL: req.URL}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": s.cfg.NodeName})
}

// handlePeerWhoami returns this node's canonical name. It is the authoritative
// answer to "which node is reachable at this URL?", letting a peer identify a
// gossiped URL — including one that loops back to the asking node itself — without
// relying on names (which default to hostnames) or a possibly-wrong advertise_url.
func (s *server) handlePeerWhoami(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, cluster.WhoAmI{Name: s.cfg.NodeName})
}

// handlePeerIdentity returns this node's real name plus its own effective
// display name (the local alias or config display_name; advertised is irrelevant
// for the node itself). A polling peer stores the display name as this node's
// advertised alias so the label propagates across the cluster. DisplayName falls
// back to the real name when no distinct label is set.
func (s *server) handlePeerIdentity(w http.ResponseWriter, r *http.Request) {
	// DisplayName is the node's OWN alias, or "" when it has none. It must NEVER
	// fall back to the real name: a poller caches a non-empty DisplayName as this
	// node's alias, so returning the hostname here would overwrite every
	// dashboard's label with the hostname.
	display := ""
	if aliases, err := s.cfg.DB.NodeAliases(); err == nil {
		display = aliases[s.cfg.NodeName]
	}
	writeJSON(w, http.StatusOK, cluster.Identity{Name: s.cfg.NodeName, DisplayName: display})
}

// handlePeerMembers serves this node's view of cluster membership: itself (when
// it has an advertise URL others can reach) plus the peers it currently polls.
// Peers fetch this to discover nodes they were never told about directly, so a
// node added on one machine propagates to the whole cluster. Muted peers are
// omitted — a node this one has chosen to ignore is not pushed onto others.
func (s *server) handlePeerMembers(w http.ResponseWriter, r *http.Request) {
	members := make([]cluster.Member, 0)
	if s.cfg.AdvertiseURL != "" {
		members = append(members, cluster.Member{Name: s.cfg.NodeName, URL: s.cfg.AdvertiseURL})
	}
	peers, err := s.cfg.DB.ListPeers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, p := range peers {
		if p.Enabled && p.URL != "" {
			members = append(members, cluster.Member{Name: p.Name, URL: p.URL})
		}
	}
	writeJSON(w, http.StatusOK, cluster.MembersResponse{Members: members})
}

// peerAtSameAddr returns the name of an existing peer whose URL points at the
// same host:port as rawURL, or "" if none. Used to suppress duplicate announces
// from a node that advertises under a different name than the one you gave it.
// An unparseable rawURL returns "" (no match) so the caller proceeds normally.
func (s *server) peerAtSameAddr(rawURL string) (string, error) {
	want, ok := normalizeAddr(rawURL)
	if !ok {
		return "", nil
	}
	peers, err := s.cfg.DB.ListPeers()
	if err != nil {
		return "", err
	}
	for _, p := range peers {
		if got, ok := normalizeAddr(p.URL); ok && got == want {
			return p.Name, nil
		}
	}
	return "", nil
}

// normalizeAddr reduces a URL to a comparable "host:port". It delegates to
// cluster.NormalizeAddr so the server's announce dedup and the poller's gossip
// dedup share one definition of "same address".
func normalizeAddr(rawURL string) (string, bool) {
	return cluster.NormalizeAddr(rawURL)
}

// alertCoordMsg is the body for the alert-claim / alert-sent peer endpoints.
type alertCoordMsg struct {
	Key  string `json:"key"`
	Node string `json:"node"`
}

// handleAlertClaim records a peer's claim on an incident and reports the current
// owner (smallest-named claimant) and whether anyone has already sent it.
func (s *server) handleAlertClaim(w http.ResponseWriter, r *http.Request) {
	var in alertCoordMsg
	if !decodeBody(w, r, &in) {
		return
	}
	if s.cfg.Coordinator == nil {
		writeJSON(w, http.StatusOK, map[string]any{"owner": in.Node, "sent": false})
		return
	}
	owner, sent := s.cfg.Coordinator.HandleClaim(in.Node, in.Key, time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]any{"owner": owner, "sent": sent})
}

// handleAlertSent records that a peer delivered an incident, so this node
// suppresses its own copy.
func (s *server) handleAlertSent(w http.ResponseWriter, r *http.Request) {
	var in alertCoordMsg
	if !decodeBody(w, r, &in) {
		return
	}
	if s.cfg.Coordinator != nil {
		s.cfg.Coordinator.HandleSent(in.Node, in.Key, time.Now().UTC())
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePeerMetrics returns this node's own current metrics to a peer.
func (s *server) handlePeerMetrics(w http.ResponseWriter, r *http.Request) {
	latest, ok, err := s.cfg.DB.LatestMetric(s.cfg.NodeName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		snap, err := metrics.Collect(ctx, 500*time.Millisecond)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, snap)
		return
	}
	writeJSON(w, http.StatusOK, metrics.Snapshot{
		CPUPercent:  latest.CPUPercent,
		MemUsed:     latest.MemUsed,
		MemTotal:    latest.MemTotal,
		MemPercent:  latest.MemPercent,
		Load1:       latest.Load1,
		Load5:       latest.Load5,
		Load15:      latest.Load15,
		SwapUsed:    latest.SwapUsed,
		SwapTotal:   latest.SwapTotal,
		SwapPercent: latest.SwapPercent,
		CollectedAt: latest.At.UTC().Format(time.RFC3339),
	})
}

// handlePeerChecks returns this node's own check statuses to a peer.
func (s *server) handlePeerChecks(w http.ResponseWriter, r *http.Request) {
	out, err := s.checksForNode(s.cfg.NodeName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
