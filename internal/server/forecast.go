package server

import (
	"time"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

// maxForecastHorizon caps how far ahead a fill forecast is reported. A mount
// filling slower than this is treated as effectively stable: the prediction
// carries no useful signal and would only clutter the dashboard.
const maxForecastHorizon = 365 * 24 * time.Hour

// fillForecast estimates when a mount will reach 100% full by least-squares
// linear regression of used bytes (y) over time in seconds (x). It returns the
// estimated seconds-until-full and ok=true only when the data supports a
// forecast: at least three points, a positive variance in x (samples span time),
// and a positive slope (the disk is actually filling). When the slope is flat or
// negative, the spread is degenerate, there are too few points, or the estimate
// exceeds maxForecastHorizon, it returns ok=false — meaning stable, not filling,
// or not enough data. The remaining headroom is measured from the latest point.
func fillForecast(points []storage.DiskUsagePoint) (etaSeconds int64, ok bool) {
	if len(points) < 3 {
		return 0, false
	}

	// Use the first point's time as the x origin to keep the magnitudes small and
	// the arithmetic well-conditioned.
	originUnix := points[0].At.UTC().Unix()
	n := float64(len(points))

	var sumX, sumY, sumXX, sumXY float64
	for _, p := range points {
		x := float64(p.At.UTC().Unix() - originUnix)
		y := float64(p.Used)
		sumX += x
		sumY += y
		sumXX += x * x
		sumXY += x * y
	}

	// varX is proportional to the variance of x (n^2 * Var(x)); covXY likewise to
	// the covariance. A zero (or numerically tiny) varX means every sample shares a
	// timestamp, so no rate can be derived.
	varX := n*sumXX - sumX*sumX
	covXY := n*sumXY - sumX*sumY
	if varX <= 0 {
		return 0, false
	}

	slope := covXY / varX // bytes per second
	if slope <= 0 {
		return 0, false // stable or shrinking — not filling
	}

	latest := points[len(points)-1]
	if latest.Used >= latest.Total {
		return 0, false // already full; nothing to forecast
	}
	remaining := float64(latest.Total - latest.Used)
	eta := remaining / slope
	if eta <= 0 {
		return 0, false
	}

	etaSeconds = int64(eta + 0.5)
	if etaSeconds > int64(maxForecastHorizon/time.Second) {
		return 0, false // filling so slowly it is effectively stable
	}
	return etaSeconds, true
}
