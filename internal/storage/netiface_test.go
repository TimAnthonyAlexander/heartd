package storage

import (
	"testing"
	"time"
)

func TestReplaceNetInterfacesReplacesNodeSet(t *testing.T) {
	db := openTestDB(t)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Seed two nodes so we can confirm a replace only touches its own node.
	if err := db.ReplaceNetInterfaces("node-a", []NetIfaceSample{
		{Node: "node-a", Iface: "eth0", RecvRate: 100, SentRate: 50, RecvErrs: 1, At: at},
		{Node: "node-a", Iface: "eth1", RecvRate: 200, SentRate: 80, SentDrops: 3, At: at},
	}); err != nil {
		t.Fatalf("seed node-a: %v", err)
	}
	if err := db.ReplaceNetInterfaces("node-b", []NetIfaceSample{
		{Node: "node-b", Iface: "en0", RecvRate: 10, SentRate: 5, At: at},
	}); err != nil {
		t.Fatalf("seed node-b: %v", err)
	}

	// Replace node-a with a single, different row.
	if err := db.ReplaceNetInterfaces("node-a", []NetIfaceSample{
		{Node: "node-a", Iface: "wg0", RecvRate: 999, SentRate: 111, RecvDrops: 7, At: at},
	}); err != nil {
		t.Fatalf("replace node-a: %v", err)
	}

	got, err := db.NetInterfaces("node-a")
	if err != nil {
		t.Fatalf("NetInterfaces node-a: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("node-a: want 1 row after replace, got %d", len(got))
	}
	if got[0].Iface != "wg0" || got[0].RecvRate != 999 || got[0].SentRate != 111 || got[0].RecvDrops != 7 {
		t.Fatalf("node-a: unexpected row %+v", got[0])
	}

	// node-b must be untouched by node-a's replace.
	other, err := db.NetInterfaces("node-b")
	if err != nil {
		t.Fatalf("NetInterfaces node-b: %v", err)
	}
	if len(other) != 1 || other[0].Iface != "en0" {
		t.Fatalf("node-b: expected its own untouched row, got %+v", other)
	}
}

func TestNetInterfacesOrdering(t *testing.T) {
	db := openTestDB(t)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := db.ReplaceNetInterfaces("node", []NetIfaceSample{
		{Node: "node", Iface: "eth2", RecvRate: 1, At: at},
		{Node: "node", Iface: "eth0", RecvRate: 2, At: at},
		{Node: "node", Iface: "eth1", RecvRate: 3, At: at},
	}); err != nil {
		t.Fatalf("ReplaceNetInterfaces: %v", err)
	}

	got, err := db.NetInterfaces("node")
	if err != nil {
		t.Fatalf("NetInterfaces: %v", err)
	}
	want := []string{"eth0", "eth1", "eth2"} // iface name ascending
	if len(got) != len(want) {
		t.Fatalf("want %d rows, got %d", len(want), len(got))
	}
	for i, name := range want {
		if got[i].Iface != name {
			t.Fatalf("position %d: want iface %q, got %q (%+v)", i, name, got[i].Iface, got[i])
		}
	}
}

func TestReplaceNetInterfacesEmptyClearsNode(t *testing.T) {
	db := openTestDB(t)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := db.ReplaceNetInterfaces("node", []NetIfaceSample{
		{Node: "node", Iface: "eth0", RecvRate: 1, At: at},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.ReplaceNetInterfaces("node", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, err := db.NetInterfaces("node")
	if err != nil {
		t.Fatalf("NetInterfaces: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty after clearing, got %d rows", len(got))
	}
}
