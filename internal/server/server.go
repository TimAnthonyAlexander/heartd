// Package server wires the heartd HTTP API and dashboard together.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

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

// node is the dashboard's view of a single heartd instance. In this wave only
// the local node exists; peers arrive in a later wave.
type node struct {
	Name   string `json:"name"`
	Local  bool   `json:"local"`
	Status string `json:"status"`
}

func (s *server) handleNodes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []node{
		{Name: s.cfg.NodeName, Local: true, Status: "ok"},
	})
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
	name := r.PathValue("name")

	stored, err := s.cfg.DB.CheckStatuses(name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
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
	// Include any stored statuses not covered by the configured set (peers, or
	// checks removed from config but still in the DB).
	for _, st := range stored {
		if !seen[st.Name] {
			out = append(out, toCheckDTO(st))
		}
	}

	writeJSON(w, http.StatusOK, out)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
