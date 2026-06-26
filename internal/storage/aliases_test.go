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

// The poller caches a peer's self-advertised name by overwriting that peer's
// row every cycle. A later value (the peer renamed itself) must replace the
// earlier one with no residue — this is what makes every dashboard converge to
// the same label.
func TestSetNodeAliasConvergesOnOverwrite(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetNodeAlias("db-01", "Old name"); err != nil {
		t.Fatalf("SetNodeAlias: %v", err)
	}
	if err := db.SetNodeAlias("db-01", "New name"); err != nil {
		t.Fatalf("SetNodeAlias (overwrite): %v", err)
	}
	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if got := aliases["db-01"]; got != "New name" {
		t.Errorf("effective = %q, want %q", got, "New name")
	}
}

// When a node stops advertising a distinct label the poller clears the row, so
// the dashboard reverts to the real name (node absent from the effective map).
func TestDeleteNodeAliasRevertsToRealName(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetNodeAlias("db-01", "Some label"); err != nil {
		t.Fatalf("SetNodeAlias: %v", err)
	}
	if err := db.DeleteNodeAlias("db-01"); err != nil {
		t.Fatalf("DeleteNodeAlias: %v", err)
	}
	aliases, err := db.NodeAliases()
	if err != nil {
		t.Fatalf("NodeAliases: %v", err)
	}
	if _, ok := aliases["db-01"]; ok {
		t.Errorf("expected node absent after clear, got %v", aliases)
	}
}
