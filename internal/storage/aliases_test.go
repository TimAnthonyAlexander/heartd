package storage

import "testing"

func TestNodeAliasesEmpty(t *testing.T) {
	db := openTestDB(t)
	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("expected no aliases, got %v", aliases)
	}
}

func TestSetAndListNodeAlias(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetNodeAlias("web-01", "Production web"); err != nil {
		t.Fatalf("SetNodeAlias: %v", err)
	}
	if err := db.SetNodeAlias("db-01", "Primary DB"); err != nil {
		t.Fatalf("SetNodeAlias: %v", err)
	}

	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if got := aliases["web-01"]; got != "Production web" {
		t.Errorf("web-01 alias = %q, want %q", got, "Production web")
	}
	if got := aliases["db-01"]; got != "Primary DB" {
		t.Errorf("db-01 alias = %q, want %q", got, "Primary DB")
	}
}

func TestSetNodeAliasUpdatesInPlace(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetNodeAlias("web-01", "first"); err != nil {
		t.Fatalf("SetNodeAlias: %v", err)
	}
	if err := db.SetNodeAlias("web-01", "second"); err != nil {
		t.Fatalf("SetNodeAlias (update): %v", err)
	}

	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if len(aliases) != 1 {
		t.Fatalf("expected 1 alias after update, got %d (%v)", len(aliases), aliases)
	}
	if got := aliases["web-01"]; got != "second" {
		t.Errorf("alias = %q, want %q", got, "second")
	}
}

func TestDeleteNodeAlias(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetNodeAlias("web-01", "label"); err != nil {
		t.Fatalf("SetNodeAlias: %v", err)
	}
	if err := db.DeleteNodeAlias("web-01"); err != nil {
		t.Fatalf("DeleteNodeAlias: %v", err)
	}

	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if _, ok := aliases["web-01"]; ok {
		t.Errorf("alias still present after delete: %v", aliases)
	}
}

func TestDeleteNodeAliasMissingNoError(t *testing.T) {
	db := openTestDB(t)
	if err := db.DeleteNodeAlias("never-set"); err != nil {
		t.Errorf("DeleteNodeAlias on missing node should be a no-op, got: %v", err)
	}
}

func TestAdvertisedAliasShownWhenNoLocal(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetAdvertisedAlias("web-01", "Advertised name"); err != nil {
		t.Fatalf("SetAdvertisedAlias: %v", err)
	}
	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if got := aliases["web-01"]; got != "Advertised name" {
		t.Errorf("effective = %q, want propagated %q", got, "Advertised name")
	}
}

func TestLocalAliasWinsOverAdvertised(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetAdvertisedAlias("web-01", "Advertised name"); err != nil {
		t.Fatalf("SetAdvertisedAlias: %v", err)
	}
	if err := db.SetNodeAlias("web-01", "Local override"); err != nil {
		t.Fatalf("SetNodeAlias: %v", err)
	}
	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if got := aliases["web-01"]; got != "Local override" {
		t.Errorf("effective = %q, want local %q", got, "Local override")
	}
}

func TestClearingLocalRevealsAdvertised(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetAdvertisedAlias("web-01", "Advertised name"); err != nil {
		t.Fatalf("SetAdvertisedAlias: %v", err)
	}
	if err := db.SetNodeAlias("web-01", "Local override"); err != nil {
		t.Fatalf("SetNodeAlias: %v", err)
	}
	// Clearing the local override must reveal the propagated name, not delete it.
	if err := db.DeleteNodeAlias("web-01"); err != nil {
		t.Fatalf("DeleteNodeAlias: %v", err)
	}
	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if got := aliases["web-01"]; got != "Advertised name" {
		t.Errorf("after clearing local, effective = %q, want advertised %q", got, "Advertised name")
	}
}

func TestSetNodeAliasPreservesAdvertised(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetAdvertisedAlias("web-01", "Advertised name"); err != nil {
		t.Fatalf("SetAdvertisedAlias: %v", err)
	}
	if err := db.SetNodeAlias("web-01", "Local override"); err != nil {
		t.Fatalf("SetNodeAlias: %v", err)
	}
	// A new advertised value (e.g. peer renamed itself) must not disturb the local
	// override, and clearing local then reveals the UPDATED advertised value.
	if err := db.SetAdvertisedAlias("web-01", "Renamed"); err != nil {
		t.Fatalf("SetAdvertisedAlias (update): %v", err)
	}
	if err := db.DeleteNodeAlias("web-01"); err != nil {
		t.Fatalf("DeleteNodeAlias: %v", err)
	}
	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if got := aliases["web-01"]; got != "Renamed" {
		t.Errorf("effective = %q, want updated advertised %q", got, "Renamed")
	}
}

func TestClearAdvertisedRevertsToRealName(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetAdvertisedAlias("web-01", "Advertised name"); err != nil {
		t.Fatalf("SetAdvertisedAlias: %v", err)
	}
	// Peer stopped advertising a label: clearing advertised drops the node from
	// the effective map so the dashboard reverts to the real name.
	if err := db.SetAdvertisedAlias("web-01", ""); err != nil {
		t.Fatalf("SetAdvertisedAlias clear: %v", err)
	}
	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if _, ok := aliases["web-01"]; ok {
		t.Errorf("expected node absent after clearing advertised, got %v", aliases)
	}
}
