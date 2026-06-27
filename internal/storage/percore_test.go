package storage

import (
	"testing"
	"time"
)

func TestReplacePerCoreReplacesNodeSet(t *testing.T) {
	db := openTestDB(t)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Seed two nodes so we can confirm a replace only touches its own node.
	if err := db.ReplacePerCore("node-a", []CoreSample{
		{Node: "node-a", Core: 0, Percent: 12, At: at},
		{Node: "node-a", Core: 1, Percent: 80, At: at},
	}); err != nil {
		t.Fatalf("seed node-a: %v", err)
	}
	if err := db.ReplacePerCore("node-b", []CoreSample{
		{Node: "node-b", Core: 0, Percent: 5, At: at},
	}); err != nil {
		t.Fatalf("seed node-b: %v", err)
	}

	// Replace node-a with a single, different row.
	if err := db.ReplacePerCore("node-a", []CoreSample{
		{Node: "node-a", Core: 0, Percent: 99, At: at},
	}); err != nil {
		t.Fatalf("replace node-a: %v", err)
	}

	got, err := db.PerCore("node-a")
	if err != nil {
		t.Fatalf("PerCore node-a: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("node-a: want 1 row after replace, got %d", len(got))
	}
	if got[0].Core != 0 || got[0].Percent != 99 {
		t.Fatalf("node-a: unexpected row %+v", got[0])
	}

	// node-b must be untouched by node-a's replace.
	other, err := db.PerCore("node-b")
	if err != nil {
		t.Fatalf("PerCore node-b: %v", err)
	}
	if len(other) != 1 || other[0].Core != 0 || other[0].Percent != 5 {
		t.Fatalf("node-b: expected its own untouched row, got %+v", other)
	}
}

func TestPerCoreOrdering(t *testing.T) {
	db := openTestDB(t)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Insert out of order; PerCore must return them by core index ascending.
	if err := db.ReplacePerCore("node", []CoreSample{
		{Node: "node", Core: 3, Percent: 30, At: at},
		{Node: "node", Core: 0, Percent: 0, At: at},
		{Node: "node", Core: 2, Percent: 20, At: at},
		{Node: "node", Core: 1, Percent: 10, At: at},
	}); err != nil {
		t.Fatalf("ReplacePerCore: %v", err)
	}

	got, err := db.PerCore("node")
	if err != nil {
		t.Fatalf("PerCore: %v", err)
	}
	wantCores := []int{0, 1, 2, 3}
	if len(got) != len(wantCores) {
		t.Fatalf("want %d rows, got %d", len(wantCores), len(got))
	}
	for i, want := range wantCores {
		if got[i].Core != want {
			t.Fatalf("position %d: want core %d, got %d (%+v)", i, want, got[i].Core, got[i])
		}
	}
}

func TestReplacePerCoreEmptyClearsNode(t *testing.T) {
	db := openTestDB(t)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := db.ReplacePerCore("node", []CoreSample{
		{Node: "node", Core: 0, Percent: 50, At: at},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.ReplacePerCore("node", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, err := db.PerCore("node")
	if err != nil {
		t.Fatalf("PerCore: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty after clearing, got %d rows", len(got))
	}
}
