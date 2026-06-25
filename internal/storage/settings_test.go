package storage

import (
	"errors"
	"testing"
)

func TestGetSettingMissingKey(t *testing.T) {
	db := openTestDB(t)

	value, ok, err := db.GetSetting("nope")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for missing key, got value %q", value)
	}
}

func TestSetSettingThenGet(t *testing.T) {
	db := openTestDB(t)

	if err := db.SetSetting("theme", `{"mode":"dark"}`); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	value, ok, err := db.GetSetting("theme")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true after SetSetting")
	}
	if value != `{"mode":"dark"}` {
		t.Fatalf("value = %q, want %q", value, `{"mode":"dark"}`)
	}
}

func TestSetSettingUpdatesInPlace(t *testing.T) {
	db := openTestDB(t)

	if err := db.SetSetting("k", "first"); err != nil {
		t.Fatalf("SetSetting first: %v", err)
	}
	if err := db.SetSetting("k", "second"); err != nil {
		t.Fatalf("SetSetting second: %v", err)
	}

	value, ok, err := db.GetSetting("k")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if value != "second" {
		t.Fatalf("value = %q, want %q", value, "second")
	}

	// Ensure no duplicate row was created.
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM setting WHERE key = ?;`, "k").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("row count for key = %d, want 1", n)
	}
}

func sampleCheckConfig() CheckConfig {
	return CheckConfig{
		Name:        "api",
		Type:        "http",
		IntervalSec: 30,
		TimeoutSec:  5,
		URL:         "https://example.com/health",
		Method:      "GET",
		Host:        "example.com",
		Port:        443,
		Process:     "nginx",
		Command:     "curl -sf https://example.com",
		Enabled:     true,
	}
}

func TestCreateAndListCheckConfig(t *testing.T) {
	db := openTestDB(t)

	in := sampleCheckConfig()
	created, err := db.CreateCheckConfig(in)
	if err != nil {
		t.Fatalf("CreateCheckConfig: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected non-zero ID, got 0")
	}

	configs, err := db.ListCheckConfigs()
	if err != nil {
		t.Fatalf("ListCheckConfigs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}

	got := configs[0]
	if got.ID != created.ID {
		t.Fatalf("ID = %d, want %d", got.ID, created.ID)
	}
	assertCheckConfigFields(t, got, in)
}

func TestCheckConfigRoundTripEnabledFalse(t *testing.T) {
	db := openTestDB(t)

	in := sampleCheckConfig()
	in.Name = "disabled-check"
	in.Enabled = false

	created, err := db.CreateCheckConfig(in)
	if err != nil {
		t.Fatalf("CreateCheckConfig: %v", err)
	}

	configs, err := db.ListCheckConfigs()
	if err != nil {
		t.Fatalf("ListCheckConfigs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Enabled {
		t.Fatalf("expected Enabled=false to round-trip")
	}
	in.ID = created.ID
	assertCheckConfigFields(t, configs[0], in)
}

func TestCountCheckConfigs(t *testing.T) {
	db := openTestDB(t)

	n, err := db.CountCheckConfigs()
	if err != nil {
		t.Fatalf("CountCheckConfigs: %v", err)
	}
	if n != 0 {
		t.Fatalf("initial count = %d, want 0", n)
	}

	for i := 0; i < 3; i++ {
		if _, err := db.CreateCheckConfig(sampleCheckConfig()); err != nil {
			t.Fatalf("CreateCheckConfig %d: %v", i, err)
		}
	}

	n, err = db.CountCheckConfigs()
	if err != nil {
		t.Fatalf("CountCheckConfigs: %v", err)
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
}

func TestUpdateCheckConfig(t *testing.T) {
	db := openTestDB(t)

	created, err := db.CreateCheckConfig(sampleCheckConfig())
	if err != nil {
		t.Fatalf("CreateCheckConfig: %v", err)
	}

	updated := CheckConfig{
		ID:          created.ID,
		Name:        "renamed",
		Type:        "tcp",
		IntervalSec: 60,
		TimeoutSec:  10,
		URL:         "",
		Method:      "",
		Host:        "db.internal",
		Port:        5432,
		Process:     "",
		Command:     "",
		Enabled:     false,
	}
	if err := db.UpdateCheckConfig(updated); err != nil {
		t.Fatalf("UpdateCheckConfig: %v", err)
	}

	configs, err := db.ListCheckConfigs()
	if err != nil {
		t.Fatalf("ListCheckConfigs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	assertCheckConfigFields(t, configs[0], updated)
}

func TestUpdateCheckConfigNotFound(t *testing.T) {
	db := openTestDB(t)

	c := sampleCheckConfig()
	c.ID = 999
	err := db.UpdateCheckConfig(c)
	if !errors.Is(err, ErrCheckNotFound) {
		t.Fatalf("err = %v, want ErrCheckNotFound", err)
	}
}

func TestDeleteCheckConfig(t *testing.T) {
	db := openTestDB(t)

	created, err := db.CreateCheckConfig(sampleCheckConfig())
	if err != nil {
		t.Fatalf("CreateCheckConfig: %v", err)
	}

	if err := db.DeleteCheckConfig(created.ID); err != nil {
		t.Fatalf("DeleteCheckConfig: %v", err)
	}

	n, err := db.CountCheckConfigs()
	if err != nil {
		t.Fatalf("CountCheckConfigs: %v", err)
	}
	if n != 0 {
		t.Fatalf("count after delete = %d, want 0", n)
	}
}

func TestDeleteCheckConfigMissingNoError(t *testing.T) {
	db := openTestDB(t)

	if err := db.DeleteCheckConfig(12345); err != nil {
		t.Fatalf("DeleteCheckConfig missing id: %v", err)
	}
}

func assertCheckConfigFields(t *testing.T, got, want CheckConfig) {
	t.Helper()
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.Type != want.Type {
		t.Errorf("Type = %q, want %q", got.Type, want.Type)
	}
	if got.IntervalSec != want.IntervalSec {
		t.Errorf("IntervalSec = %d, want %d", got.IntervalSec, want.IntervalSec)
	}
	if got.TimeoutSec != want.TimeoutSec {
		t.Errorf("TimeoutSec = %d, want %d", got.TimeoutSec, want.TimeoutSec)
	}
	if got.URL != want.URL {
		t.Errorf("URL = %q, want %q", got.URL, want.URL)
	}
	if got.Method != want.Method {
		t.Errorf("Method = %q, want %q", got.Method, want.Method)
	}
	if got.Host != want.Host {
		t.Errorf("Host = %q, want %q", got.Host, want.Host)
	}
	if got.Port != want.Port {
		t.Errorf("Port = %d, want %d", got.Port, want.Port)
	}
	if got.Process != want.Process {
		t.Errorf("Process = %q, want %q", got.Process, want.Process)
	}
	if got.Command != want.Command {
		t.Errorf("Command = %q, want %q", got.Command, want.Command)
	}
	if got.Enabled != want.Enabled {
		t.Errorf("Enabled = %v, want %v", got.Enabled, want.Enabled)
	}
}
