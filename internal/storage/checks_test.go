package storage

import (
	"testing"
	"time"
)

func checkAt(node, name, status, detail string, at time.Time) CheckStatus {
	return CheckStatus{
		Node:      node,
		Name:      name,
		Type:      "http",
		Status:    status,
		Detail:    detail,
		LatencyMS: 42,
		At:        at,
	}
}

func TestUpsertAndCheckStatuses(t *testing.T) {
	db := openTestDB(t)

	if got, err := db.CheckStatuses("a"); err != nil || len(got) != 0 {
		t.Fatalf("CheckStatuses on empty: got %d rows err=%v, want 0/nil", len(got), err)
	}

	base := time.Unix(1_700_000_000, 0).UTC()
	in := checkAt("a", "api", "ok", "200 OK", base)
	if err := db.UpsertCheckStatus(in); err != nil {
		t.Fatalf("UpsertCheckStatus: %v", err)
	}

	got, err := db.CheckStatuses("a")
	if err != nil {
		t.Fatalf("CheckStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	g := got[0]
	if g.Node != in.Node || g.Name != in.Name || g.Type != in.Type ||
		g.Status != in.Status || g.Detail != in.Detail || g.LatencyMS != in.LatencyMS {
		t.Fatalf("CheckStatuses returned wrong status: %+v", g)
	}
	if !g.At.Equal(in.At) {
		t.Fatalf("At = %v, want %v", g.At, in.At)
	}
	if g.At.Location() != time.UTC {
		t.Fatalf("At not UTC: %v", g.At.Location())
	}
}

func TestUpsertUpdatesInPlace(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	if err := db.UpsertCheckStatus(checkAt("a", "api", "ok", "200 OK", base)); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	updated := CheckStatus{
		Node:      "a",
		Name:      "api",
		Type:      "tcp",
		Status:    "failing",
		Detail:    "connection refused",
		LatencyMS: 999,
		At:        base.Add(time.Minute),
	}
	if err := db.UpsertCheckStatus(updated); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := db.CheckStatuses("a")
	if err != nil {
		t.Fatalf("CheckStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row after re-upsert, got %d (upsert created a duplicate)", len(got))
	}
	g := got[0]
	if g.Type != "tcp" || g.Status != "failing" || g.Detail != "connection refused" ||
		g.LatencyMS != 999 || !g.At.Equal(updated.At) {
		t.Fatalf("upsert did not update values: %+v", g)
	}
}

func TestCheckStatusesMultiNodeIsolation(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	if err := db.UpsertCheckStatus(checkAt("a", "api", "ok", "a-detail", base)); err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := db.UpsertCheckStatus(checkAt("b", "api", "failing", "b-detail", base)); err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	aRows, err := db.CheckStatuses("a")
	if err != nil {
		t.Fatalf("CheckStatuses a: %v", err)
	}
	if len(aRows) != 1 || aRows[0].Node != "a" || aRows[0].Detail != "a-detail" {
		t.Fatalf("node a isolation failed: %+v", aRows)
	}

	bRows, err := db.CheckStatuses("b")
	if err != nil {
		t.Fatalf("CheckStatuses b: %v", err)
	}
	if len(bRows) != 1 || bRows[0].Node != "b" || bRows[0].Detail != "b-detail" {
		t.Fatalf("node b isolation failed: %+v", bRows)
	}
}

func TestCheckStatusesOrdering(t *testing.T) {
	db := openTestDB(t)

	base := time.Unix(1_700_000_000, 0).UTC()
	// Insert out of alphabetical order.
	for _, name := range []string{"cache", "api", "db", "worker"} {
		if err := db.UpsertCheckStatus(checkAt("a", name, "ok", "", base)); err != nil {
			t.Fatalf("upsert %q: %v", name, err)
		}
	}

	got, err := db.CheckStatuses("a")
	if err != nil {
		t.Fatalf("CheckStatuses: %v", err)
	}
	want := []string{"api", "cache", "db", "worker"}
	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d", len(want), len(got))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("got[%d].Name = %q, want %q (not ordered by name)", i, got[i].Name, name)
		}
	}
}

func TestCheckStatusTimestampRoundTrip(t *testing.T) {
	db := openTestDB(t)

	at := time.Now()
	if err := db.UpsertCheckStatus(checkAt("a", "api", "ok", "", at)); err != nil {
		t.Fatalf("UpsertCheckStatus: %v", err)
	}

	got, err := db.CheckStatuses("a")
	if err != nil {
		t.Fatalf("CheckStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if diff := got[0].At.Sub(at.UTC()); diff > time.Second || diff < -time.Second {
		t.Fatalf("At round-trip diff = %v, want within 1s", diff)
	}
}
