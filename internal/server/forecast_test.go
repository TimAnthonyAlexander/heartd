package server

import (
	"testing"
	"time"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

// usageSeries builds a capacity series for one mount: starting at startUsed and
// growing by perStep bytes each step, totalling total, one sample per minute.
func usageSeries(startUsed, perStep, total uint64, steps int) []storage.DiskUsagePoint {
	base := time.Unix(1_700_000_000, 0).UTC()
	out := make([]storage.DiskUsagePoint, 0, steps)
	for i := 0; i < steps; i++ {
		used := startUsed + perStep*uint64(i)
		out = append(out, storage.DiskUsagePoint{
			Used:    used,
			Total:   total,
			Percent: float64(used) / float64(total) * 100,
			At:      base.Add(time.Duration(i) * time.Minute),
		})
	}
	return out
}

func TestFillForecastFilling(t *testing.T) {
	// Fills 1 GiB/min from 90 GiB toward a 100 GiB disk: 10 GiB headroom remains
	// at the last point, so ETA ~= 10 minutes = 600s.
	const gib = uint64(1) << 30
	points := usageSeries(90*gib, gib, 100*gib, 6)

	eta, ok := fillForecast(points)
	if !ok {
		t.Fatalf("fillForecast: ok=false, want a forecast for a clearly-filling series")
	}
	// 5 GiB grown over 5 steps -> latest used = 95 GiB, 5 GiB headroom, slope
	// 1 GiB/60s -> eta ~= 300s. Allow a small rounding band.
	if eta < 280 || eta > 320 {
		t.Fatalf("fillForecast eta = %ds, want ~300s", eta)
	}
}

func TestFillForecastStable(t *testing.T) {
	const gib = uint64(1) << 30
	// Flat usage: no fill, no forecast.
	flat := usageSeries(50*gib, 0, 100*gib, 6)
	if eta, ok := fillForecast(flat); ok {
		t.Fatalf("fillForecast on flat series: ok=true eta=%d, want false", eta)
	}

	// Shrinking usage: negative slope, no forecast.
	base := time.Unix(1_700_000_000, 0).UTC()
	shrink := make([]storage.DiskUsagePoint, 0, 5)
	for i := 0; i < 5; i++ {
		used := 80*gib - gib*uint64(i)
		shrink = append(shrink, storage.DiskUsagePoint{
			Used: used, Total: 100 * gib, At: base.Add(time.Duration(i) * time.Minute),
		})
	}
	if eta, ok := fillForecast(shrink); ok {
		t.Fatalf("fillForecast on shrinking series: ok=true eta=%d, want false", eta)
	}
}

func TestFillForecastTooFewPoints(t *testing.T) {
	const gib = uint64(1) << 30
	points := usageSeries(90*gib, gib, 100*gib, 2)
	if eta, ok := fillForecast(points); ok {
		t.Fatalf("fillForecast with 2 points: ok=true eta=%d, want false", eta)
	}
}

func TestFillForecastBeyondHorizon(t *testing.T) {
	const gib = uint64(1) << 30
	// Grows 1 byte/min with ~10 GiB headroom: ETA is millennia, beyond the
	// 365-day horizon, so it reads as stable.
	points := usageSeries(90*gib, 1, 100*gib, 5)
	if eta, ok := fillForecast(points); ok {
		t.Fatalf("fillForecast far beyond horizon: ok=true eta=%d, want false", eta)
	}
}
