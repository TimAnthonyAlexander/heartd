package server

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

// peerDTO is the dashboard's view of a managed peer. The shared secret is never
// returned; only whether one is set (so the UI can flag links that can't be
// polled yet).
type peerDTO struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	LastSeen  string `json:"last_seen"` // RFC3339, empty if never seen
	LastError string `json:"last_error"`
	HasSecret bool   `json:"has_secret"`
	Muted     bool   `json:"muted"`
}

func toPeerDTO(p storage.Peer) peerDTO {
	status := p.Status
	if status == "" {
		status = "unknown"
	}
	lastSeen := ""
	if !p.LastSeen.IsZero() {
		lastSeen = p.LastSeen.UTC().Format(time.RFC3339)
	}
	return peerDTO{
		Name:      p.Name,
		URL:       p.URL,
		Status:    status,
		LastSeen:  lastSeen,
		LastError: p.LastError,
		HasSecret: p.Secret != "",
		Muted:     !p.Enabled,
	}
}

// peerInput is the JSON body for creating or updating a peer. On update, an empty
// Secret leaves the stored secret unchanged. Muted=true mutes the peer from this
// node's perspective (not polled, not alerted on, grayed out).
type peerInput struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Secret string `json:"secret"`
	Muted  bool   `json:"muted"`
}

func (s *server) handleListPeers(w http.ResponseWriter, r *http.Request) {
	peers, err := s.cfg.DB.ListPeers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]peerDTO, 0, len(peers))
	for _, p := range peers {
		out = append(out, toPeerDTO(p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleCreatePeer(w http.ResponseWriter, r *http.Request) {
	var in peerInput
	if !decodeBody(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.URL = strings.TrimSpace(in.URL)

	if msg := validatePeer(in, s.cfg.NodeName, true); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	if _, exists, err := s.cfg.DB.GetPeer(in.Name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	} else if exists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a node named " + in.Name + " already exists"})
		return
	}

	if err := s.cfg.DB.UpsertPeer(storage.Peer{Name: in.Name, URL: in.URL, Secret: in.Secret}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.cfg.DB.SetPeerEnabled(in.Name, !in.Muted); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	peer, _, _ := s.cfg.DB.GetPeer(in.Name)
	writeJSON(w, http.StatusOK, toPeerDTO(peer))
}

func (s *server) handleUpdatePeer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	existing, ok, err := s.cfg.DB.GetPeer(name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown node"})
		return
	}

	var in peerInput
	if !decodeBody(w, r, &in) {
		return
	}
	in.Name = name // the name is the identity key and is not editable
	in.URL = strings.TrimSpace(in.URL)

	// Secret is optional on update; blank keeps the existing one.
	if msg := validatePeer(in, s.cfg.NodeName, in.Secret != "" || existing.Secret == ""); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}

	// UpsertPeer leaves the secret unchanged when the incoming secret is empty.
	if err := s.cfg.DB.UpsertPeer(storage.Peer{Name: name, URL: in.URL, Secret: in.Secret}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.cfg.DB.SetPeerEnabled(name, !in.Muted); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	peer, _, _ := s.cfg.DB.GetPeer(name)
	writeJSON(w, http.StatusOK, toPeerDTO(peer))
}

func (s *server) handleDeletePeer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == s.cfg.NodeName {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove the local node"})
		return
	}
	if _, ok, err := s.cfg.DB.GetPeer(name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	} else if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown node"})
		return
	}

	// Purge the node's stored history, then the peer row. Order matters only for
	// tidiness — both are addressed by the same name.
	if err := s.cfg.DB.DeleteNodeData(name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.cfg.DB.DeletePeer(name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.cfg.DB.DeleteNodeAlias(name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if s.cfg.Engine != nil {
		s.cfg.Engine.ForgetNode(name)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// aliasInput is the JSON body for setting a node's UI display alias.
type aliasInput struct {
	Alias string `json:"alias"`
}

// handleSetNodeAlias sets or clears a node's display alias. The node's real name
// remains the identity key everywhere (storage, routing, peer protocol); the
// alias only changes how this dashboard labels it. Works for the local node and
// for any known peer. A blank alias (or one equal to the real name) clears it.
func (s *server) handleSetNodeAlias(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != s.cfg.NodeName {
		if _, ok, err := s.cfg.DB.GetPeer(name); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		} else if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown node"})
			return
		}
	}

	var in aliasInput
	if !decodeBody(w, r, &in) {
		return
	}
	alias := strings.TrimSpace(in.Alias)
	if len([]rune(alias)) > 64 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "display name must be 64 characters or fewer"})
		return
	}

	if alias == "" || alias == name {
		if err := s.cfg.DB.DeleteNodeAlias(name); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"alias": ""})
		return
	}
	if err := s.cfg.DB.SetNodeAlias(name, alias); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"alias": alias})
}

// validatePeer returns an error message, or "" when the input is valid.
// requireSecret enforces a non-empty secret (used on create, and on update only
// when no secret is stored yet and none was supplied).
func validatePeer(in peerInput, localName string, requireSecret bool) string {
	if in.Name == "" {
		return "node name is required"
	}
	if in.Name == localName {
		return "name collides with the local node"
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "url must be a valid http(s) URL, e.g. https://host:9300"
	}
	if requireSecret && in.Secret == "" {
		return "shared secret is required"
	}
	return ""
}
