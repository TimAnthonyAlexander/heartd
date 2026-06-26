// Package server wires the heartd HTTP API and dashboard together.
package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
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
}

// New builds the root HTTP handler: REST API under /api and the embedded
// dashboard for everything else.
func New(cfg Config) http.Handler {
	s := &server{
		cfg:         cfg,
		proxyClient: &http.Client{Timeout: 15 * time.Second},
	}
	mux := http.NewServeMux()

	// Public: liveness and the auth flow (you must reach these unauthenticated).
	mux.HandleFunc("GET /api/health", s.handleHealth)
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

	protect("GET /api/nodes/{name}/metrics", s.handleMetrics)
	protect("GET /api/nodes/{name}/metrics/history", s.handleHistory)
	protect("GET /api/nodes/{name}/checks", s.handleChecks)
	protect("GET /api/nodes/{name}/disk", s.handleDisk)
	protect("GET /api/nodes/{name}/network", s.handleNetwork)
	protect("GET /api/nodes/{name}/network/history", s.handleNetworkHistory)

	// Runtime configuration, addressed per node. For the local node these operate
	// on its own settings service; for a peer they proxy the same request to that
	// peer's /api/peer/settings/* over the shared-secret link, so each node owns
	// its own config but is editable from any dashboard.
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

	// Node-to-node endpoints, protected by the shared secret.
	mux.Handle("POST /api/peer/announce", s.requireSecret(http.HandlerFunc(s.handlePeerAnnounce)))
	mux.Handle("GET /api/peer/metrics", s.requireSecret(http.HandlerFunc(s.handlePeerMetrics)))
	mux.Handle("GET /api/peer/checks", s.requireSecret(http.HandlerFunc(s.handlePeerChecks)))
	mux.Handle("GET /api/peer/disk", s.requireSecret(http.HandlerFunc(s.handlePeerDisk)))
	mux.Handle("GET /api/peer/network", s.requireSecret(http.HandlerFunc(s.handlePeerNetwork)))

	// Node-to-node settings: the receive side of the proxy above. Same handlers
	// the local node uses, operating on THIS node's own settings service.
	mux.Handle("GET /api/peer/settings", s.requireSecret(http.HandlerFunc(s.handleGetSettings)))
	mux.Handle("PUT /api/peer/settings/general", s.requireSecret(http.HandlerFunc(s.handlePutGeneral)))
	mux.Handle("PUT /api/peer/settings/notify", s.requireSecret(http.HandlerFunc(s.handlePutNotify)))
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

	// Everything not under /api is the dashboard.
	mux.Handle("/", web.Handler())

	return mux
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
	if peer.URL == "" || peer.Secret == "" {
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
	req.Header.Set(cluster.SecretHeader, peer.Secret)

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
