package indicator

import (
	"math"

	"github.com/pompbot/scalpbot/internal/model"
)

// ATR implements the Average True Range indicator with streaming computation.
// Uses Wilder's smoothing method for the true range average.
// Operates on Candle data (requires High, Low, Close).
type ATR struct {
	period    int     // ATR lookback period (typically 14)
	current   float64 // Current ATR value
	prevClose float64 // Previous candle's close price
	count     int     // Number of candles received
	ready     bool    // True once warmup is complete
	trSum     float64 // Running sum during warmup
}

// NewATR creates a new ATR calculator for the given period.
// Returns nil if period < 1.
func NewATR(period int) *ATR {
	if period < 1 {
		return nil
	}
	return &ATR{
		period: period,
	}
}

// Update feeds a new candle and returns the current ATR value.
// Returns math.NaN() during the warmup phase.
func (a *ATR) Update(candle model.Candle) float64 {
	a.count++

	if a.count == 1 {
		// First candle — TR is simply High - Low (no previous close)
		tr := candle.High - candle.Low
		a.trSum = tr
		a.prevClose = candle.Close

		if a.period == 1 {
			a.current = tr
			a.ready = true
			return a.current
		}
		return math.NaN()
	}

	// True Range = max(H-L, |H-prevClose|, |L-prevClose|)
	tr := trueRange(candle.High, candle.Low, a.prevClose)
	a.prevClose = candle.Close

	if !a.ready {
		a.trSum += tr
		if a.count >= a.period {
			// Warmup complete — seed with simple average
			a.current = a.trSum / float64(a.period)
			a.ready = true
			return a.current
		}
		return math.NaN()
	}

	// Wilder's smoothing: ATR = ((prevATR * (period-1)) + TR) / period
	pf := float64(a.period)
	a.current = (a.current*(pf-1) + tr) / pf
	return a.current
}

// trueRange computes the True Range from high, low, and previous close.
func trueRange(high, low, prevClose float64) float64 {
	hl := high - low
	hpc := math.Abs(high - prevClose)
	lpc := math.Abs(low - prevClose)

	tr := hl
	if hpc > tr {
		tr = hpc
	}
	if lpc > tr {
		tr = lpc
	}
	return tr
}

// Value returns the current ATR value, or math.NaN() if not yet ready.
func (a *ATR) Value() float64 {
	if !a.ready {
		return math.NaN()
	}
	return a.current
}

// Ready returns true once sufficient data has been received for valid output.
func (a *ATR) Ready() bool {
	return a.ready
}

// Reset clears all state for reuse.
func (a *ATR) Reset() {
	a.current = 0
	a.prevClose = 0
	a.count = 0
	a.ready = false
	a.trSum = 0
}

// Period returns the configured period.
func (a *ATR) Period() int {
	return a.period
}
