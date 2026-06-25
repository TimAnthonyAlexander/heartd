package metrics

import (
	"context"
	"testing"
)

func TestDisks(t *testing.T) {
	disks, err := Disks(context.Background())
	if err != nil {
		t.Fatalf("Disks returned error: %v", err)
	}
	if len(disks) == 0 {
		t.Fatal("Disks returned no mounts; expected at least one")
	}

	for _, d := range disks {
		if d.Mount == "" {
			t.Errorf("disk has empty mount: %+v", d)
		}
		if d.Total == 0 {
			t.Errorf("disk %q has Total == 0", d.Mount)
		}
		if d.Used > d.Total {
			t.Errorf("disk %q has Used (%d) > Total (%d)", d.Mount, d.Used, d.Total)
		}
		if d.Percent < 0 || d.Percent > 100 {
			t.Errorf("disk %q has Percent out of range: %f", d.Mount, d.Percent)
		}
	}
}

func TestReadNetCounters(t *testing.T) {
	first, err := ReadNetCounters(context.Background())
	if err != nil {
		t.Fatalf("ReadNetCounters returned error: %v", err)
	}

	second, err := ReadNetCounters(context.Background())
	if err != nil {
		t.Fatalf("ReadNetCounters (second call) returned error: %v", err)
	}

	// Cumulative counters since boot should not decrease between two
	// back-to-back reads.
	if second.RecvBytes < first.RecvBytes {
		t.Errorf("RecvBytes decreased: %d -> %d", first.RecvBytes, second.RecvBytes)
	}
	if second.SentBytes < first.SentBytes {
		t.Errorf("SentBytes decreased: %d -> %d", first.SentBytes, second.SentBytes)
	}
}
