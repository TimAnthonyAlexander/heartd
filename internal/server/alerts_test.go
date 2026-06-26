package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/settings"
)

// alertServer builds a bare server whose settings are loaded (so the default
// alert rules are seeded).
func alertServer(t *testing.T) *server {
	t.Helper()
	db := testDB(t)
	set := settings.New(db)
	if err := set.Load(config.Default()); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return localServer("web-01", db, set)
}

func TestCreateAlertValidation(t *testing.T) {
	s := alertServer(t)

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/nodes/web-01/settings/alerts", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.handleCreateAlert(rec, req)
		return rec.Code
	}

	// Missing name -> 400.
	if c := post(`{"source":"cpu","comparator":">=","threshold":90,"severity":"warning"}`); c != http.StatusBadRequest {
		t.Fatalf("no-name create = %d, want 400", c)
	}
	// Out-of-range percent -> 400.
	if c := post(`{"name":"x","source":"cpu","comparator":">=","threshold":150,"severity":"warning"}`); c != http.StatusBadRequest {
		t.Fatalf("bad threshold create = %d, want 400", c)
	}
	// Valid -> 200.
	if c := post(`{"name":"High CPU","source":"cpu","comparator":">=","threshold":90,"severity":"critical","for_seconds":120}`); c != http.StatusOK {
		t.Fatalf("valid create = %d, want 200", c)
	}
}

func TestAlertCRUDRoundTrip(t *testing.T) {
	s := alertServer(t)

	// Create.
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/web-01/settings/alerts",
		strings.NewReader(`{"name":"Disk full","source":"disk","entity":"/","comparator":">=","threshold":95,"severity":"critical"}`))
	rec := httptest.NewRecorder()
	s.handleCreateAlert(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created alertRuleInput
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Source != "disk" || created.Entity != "/" {
		t.Fatalf("unexpected created rule: %+v", created)
	}

	// Update.
	req = httptest.NewRequest(http.MethodPut, "/api/nodes/web-01/settings/alerts/1",
		strings.NewReader(`{"name":"Disk full","source":"disk","entity":"/","comparator":">=","threshold":80,"severity":"warning"}`))
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	rec = httptest.NewRecorder()
	s.handleUpdateAlert(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body.String())
	}

	// Delete.
	req = httptest.NewRequest(http.MethodDelete, "/api/nodes/web-01/settings/alerts/1", nil)
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	rec = httptest.NewRecorder()
	s.handleDeleteAlert(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body.String())
	}

	// The deleted rule must be gone; the seeded defaults remain.
	for _, a := range s.cfg.Settings.AlertRules() {
		if a.ID == created.ID {
			t.Fatal("rule still present after delete")
		}
	}
}
