// Package server wires the heartd HTTP API and dashboard together.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/timanthonyalexander/heartd/internal/cluster"
	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/metrics"
	"github.com/timanthonyalexander/heartd/internal/storage"
	"github.com/timanthonyalexander/heartd/internal/web"
)

// Config holds runtime settings for the server.
type Config struct {
	NodeName string
	DB       *storage.DB
	Checks   []config.Check
	// PeerSecrets are the shared secrets this node accepts on node-to-node
	// requests (typically the secrets of its configured peers).
	PeerSecrets []string
}

// New builds the root HTTP handler: REST API under /api and the embedded
// dashboard for everything else.
func New(cfg Config) http.Handler {
	s := &server{cfg: cfg}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/nodes", s.handleNodes)
	mux.HandleFunc("GET /api/nodes/{name}/metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/nodes/{name}/metrics/history", s.handleHistory)
	mux.HandleFunc("GET /api/nodes/{name}/checks", s.handleChecks)
	mux.HandleFunc("GET /api/nodes/{name}/disk", s.handleDisk)
	mux.HandleFunc("GET /api/nodes/{name}/network", s.handleNetwork)
	mux.HandleFunc("GET /api/nodes/{name}/network/history", s.handleNetworkHistory)

	// Node-to-node endpoints, protected by the shared secret.
	mux.Handle("POST /api/peer/announce", s.requireSecret(http.HandlerFunc(s.handlePeerAnnounce)))
	mux.Handle("GET /api/peer/metrics", s.requireSecret(http.HandlerFunc(s.handlePeerMetrics)))
	mux.Handle("GET /api/peer/checks", s.requireSecret(http.HandlerFunc(s.handlePeerChecks)))
	mux.Handle("GET /api/peer/disk", s.requireSecret(http.HandlerFunc(s.handlePeerDisk)))
	mux.Handle("GET /api/peer/network", s.requireSecret(http.HandlerFunc(s.handlePeerNetwork)))

	// Unknown API paths return JSON 404 rather than falling through to the
	// SPA handler (which would serve index.html with a 200).
	mux.HandleFunc("/api/", handleAPINotFound)

	// Everything not under /api is the dashboard.
	mux.Handle("/", web.Handler())

	return mux
}

type server struct {
	cfg Config
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
	Local  bool   `json:"local"`
	Status string `json:"status"` // ok | down | unknown
}

// handleNodes returns the local node plus all known peers with their current
// reachability, so each node's dashboard is a full cluster view.
func (s *server) handleNodes(w http.ResponseWriter, r *http.Request) {
	out := []node{{Name: s.cfg.NodeName, Local: true, Status: "ok"}}

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
		out = append(out, node{Name: p.Name, Local: false, Status: status})
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
	CPUPercent float64 `json:"cpu_percent"`
	MemUsed    uint64  `json:"mem_used"`
	MemTotal   uint64  `json:"mem_total"`
	MemPercent float64 `json:"mem_percent"`
	At         string  `json:"at"`
}

// handleHistory returns persisted samples for a node within a recent window.
// Query params: minutes (default 60), limit (default 200).
func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	minutes := queryInt(r, "minutes", 60)
	limit := queryInt(r, "limit", 200)

	since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
	samples, err := s.cfg.DB.RecentMetrics(name, since, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	points := make([]historyPoint, 0, len(samples))
	for _, m := range samples {
		points = append(points, historyPoint{
			CPUPercent: m.CPUPercent,
			MemUsed:    m.MemUsed,
			MemTotal:   m.MemTotal,
			MemPercent: m.MemPercent,
			At:         m.At.UTC().Format(time.RFC3339),
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
		for _, c := range s.cfg.Checks {
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
	minutes := queryInt(r, "minutes", 60)
	limit := queryInt(r, "limit", 200)

	since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
	samples, err := s.cfg.DB.RecentNetSamples(name, since, limit)
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

func (s *server) validSecret(presented string) bool {
	ok := false
	for _, secret := range s.cfg.PeerSecrets {
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
	// Don't grant a secret from an announce; preserve any existing one.
	if err := s.cfg.DB.UpsertPeer(storage.Peer{Name: req.Name, URL: req.URL}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": s.cfg.NodeName})
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
