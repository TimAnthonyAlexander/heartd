package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/timanthonyalexander/heartd/internal/alert"
	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/settings"
	"github.com/timanthonyalexander/heartd/internal/storage"
)

// checkInput is the JSON shape for a configurable check (durations as seconds,
// since settings.Check carries them as time.Duration).
type checkInput struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	IntervalSec int64  `json:"interval_sec"`
	TimeoutSec  int64  `json:"timeout_sec"`
	URL         string `json:"url"`
	Method      string `json:"method"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Process     string `json:"process"`
	Command     string `json:"command"`
	Enabled     bool   `json:"enabled"`
}

func (c checkInput) toSettings() settings.Check {
	return settings.Check{
		ID: c.ID, Name: c.Name, Type: c.Type,
		Interval: time.Duration(c.IntervalSec) * time.Second,
		Timeout:  time.Duration(c.TimeoutSec) * time.Second,
		URL:      c.URL, Method: c.Method, Host: c.Host, Port: c.Port,
		Process: c.Process, Command: c.Command, Enabled: c.Enabled,
	}
}

func checkToOutput(c settings.Check) checkInput {
	return checkInput{
		ID: c.ID, Name: c.Name, Type: c.Type,
		IntervalSec: int64(c.Interval.Seconds()),
		TimeoutSec:  int64(c.Timeout.Seconds()),
		URL:         c.URL, Method: c.Method, Host: c.Host, Port: c.Port,
		Process: c.Process, Command: c.Command, Enabled: c.Enabled,
	}
}

// alertRuleInput is the JSON shape for a configurable alert rule.
type alertRuleInput struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Enabled      bool    `json:"enabled"`
	Source       string  `json:"source"`
	Entity       string  `json:"entity"`
	Comparator   string  `json:"comparator"`
	Threshold    float64 `json:"threshold"`
	ForSec       int64   `json:"for_seconds"`
	RecoverGrace int64   `json:"recover_grace_seconds"`
	Severity     string  `json:"severity"`
}

func (a alertRuleInput) toStorage() storage.AlertRule {
	return storage.AlertRule{
		ID: a.ID, Name: a.Name, Enabled: a.Enabled, Source: a.Source, Entity: a.Entity,
		Comparator: a.Comparator, Threshold: a.Threshold, ForSec: a.ForSec,
		RecoverGrace: a.RecoverGrace, Severity: a.Severity,
	}
}

func alertToOutput(a storage.AlertRule) alertRuleInput {
	return alertRuleInput{
		ID: a.ID, Name: a.Name, Enabled: a.Enabled, Source: a.Source, Entity: a.Entity,
		Comparator: a.Comparator, Threshold: a.Threshold, ForSec: a.ForSec,
		RecoverGrace: a.RecoverGrace, Severity: a.Severity,
	}
}

type settingsResp struct {
	General settings.General `json:"general"`
	Notify  settings.Notify  `json:"notify"`
	Checks  []checkInput     `json:"checks"`
	Alerts  []alertRuleInput `json:"alerts"`
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	resp := settingsResp{
		General: s.cfg.Settings.General(),
		Notify:  s.cfg.Settings.Notify(),
		Checks:  []checkInput{},
		Alerts:  []alertRuleInput{},
	}
	for _, c := range s.cfg.Settings.Checks() {
		resp.Checks = append(resp.Checks, checkToOutput(c))
	}
	for _, a := range s.cfg.Settings.AlertRules() {
		resp.Alerts = append(resp.Alerts, alertToOutput(a))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleCreateAlert(w http.ResponseWriter, r *http.Request) {
	var in alertRuleInput
	if !decodeBody(w, r, &in) {
		return
	}
	created, err := s.cfg.Settings.CreateAlertRule(in.toStorage())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, alertToOutput(created))
}

func (s *server) handleUpdateAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var in alertRuleInput
	if !decodeBody(w, r, &in) {
		return
	}
	in.ID = id
	if err := s.cfg.Settings.UpdateAlertRule(in.toStorage()); err != nil {
		if errors.Is(err, storage.ErrAlertRuleNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "alert rule not found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleDeleteAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Settings.DeleteAlertRule(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if s.cfg.Engine != nil {
		s.cfg.Engine.Forget(id)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handlePutGeneral(w http.ResponseWriter, r *http.Request) {
	var g settings.General
	if !decodeBody(w, r, &g) {
		return
	}
	if err := s.cfg.Settings.SetGeneral(g); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.cfg.Settings.General())
}

func (s *server) handlePutNotify(w http.ResponseWriter, r *http.Request) {
	var n settings.Notify
	if !decodeBody(w, r, &n) {
		return
	}
	if err := s.cfg.Settings.SetNotify(n); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.cfg.Settings.Notify())
}

func (s *server) handleCreateCheck(w http.ResponseWriter, r *http.Request) {
	var in checkInput
	if !decodeBody(w, r, &in) {
		return
	}
	created, err := s.cfg.Settings.CreateCheck(in.toSettings())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, checkToOutput(created))
}

func (s *server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var in checkInput
	if !decodeBody(w, r, &in) {
		return
	}
	in.ID = id
	if err := s.cfg.Settings.UpdateCheck(in.toSettings()); err != nil {
		if errors.Is(err, storage.ErrCheckNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "check not found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleDeleteCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.cfg.Settings.DeleteCheck(id, s.cfg.NodeName); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleTestNotify sends a test alert using the notify config in the request
// body (so the user can verify a channel before saving), reporting per-channel
// results synchronously.
func (s *server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	var n settings.Notify
	if !decodeBody(w, r, &n) {
		return
	}

	test := alert.Alert{
		Kind: alert.KindMetric, Node: s.cfg.NodeName, Subject: "Test",
		Firing: true, Title: "heartd test alert",
		Detail: "This is a test notification from heartd.", Time: time.Now().UTC(),
	}

	results := map[string]string{}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	if n.Webhook.Enabled && n.Webhook.URL != "" {
		err := alert.NewWebhookNotifier(config.WebhookNotify{URL: n.Webhook.URL}).Send(ctx, test)
		results["webhook"] = resultString(err)
	}
	if n.Email.Enabled {
		err := alert.NewEmailNotifier(config.EmailNotify{
			SMTPHost: n.Email.SMTPHost, SMTPPort: n.Email.SMTPPort,
			Username: n.Email.Username, Password: n.Email.Password,
			From: n.Email.From, To: n.Email.To, SubjectPrefix: n.Email.SubjectPrefix,
		}).Send(ctx, test)
		results["email"] = resultString(err)
	}
	if len(results) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no channel enabled to test"})
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func resultString(err error) string {
	if err != nil {
		return "failed: " + err.Error()
	}
	return "ok"
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return false
	}
	return true
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return 0, false
	}
	return id, true
}
