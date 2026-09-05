package indicator

import "math"

// RSI implements Wilder's Relative Strength Index with streaming computation.
// Uses Wilder's smoothing method (exponential moving average of gains/losses).
// O(1) per update, zero allocation after initialization.
type RSI struct {
	period  int     // RSI lookback period (typically 14)
	avgGain float64 // Wilder-smoothed average gain
	avgLoss float64 // Wilder-smoothed average loss
	prev    float64 // Previous price value
	count   int     // Number of values received
	ready   bool    // True once warmup is complete

	// Warmup accumulators
	gainSum float64
	lossSum float64
}

// NewRSI creates a new RSI calculator for the given period.
// Returns nil if period < 1.
func NewRSI(period int) *RSI {
	if period < 1 {
		return nil
	}
	return &RSI{
		period: period,
	}
}

// Update feeds a new price value and returns the current RSI [0, 100].
// Returns math.NaN() during the warmup phase.
func (r *RSI) Update(value float64) float64 {
	r.count++

	// First value — just store as previous, no change to compute
	if r.count == 1 {
		r.prev = value
		return math.NaN()
	}

	change := value - r.prev
	r.prev = value

	var gain, loss float64
	if change > 0 {
		gain = change
	} else {
		loss = -change
	}

	// Warmup phase: accumulate gains and losses
	if !r.ready {
		r.gainSum += gain
		r.lossSum += loss

		// Need 'period' changes (period+1 values) to complete warmup
		if r.count > r.period {
			r.avgGain = r.gainSum / float64(r.period)
			r.avgLoss = r.lossSum / float64(r.period)
			r.ready = true
			return r.computeRSI()
		}
		return math.NaN()
	}

	// Wilder's smoothing
	pf := float64(r.period)
	r.avgGain = (r.avgGain*(pf-1) + gain) / pf
	r.avgLoss = (r.avgLoss*(pf-1) + loss) / pf

	return r.computeRSI()
}

// computeRSI calculates RSI from current average gain/loss.
func (r *RSI) computeRSI() float64 {
	if r.avgLoss == 0 {
		if r.avgGain == 0 {
			return 50.0 // No movement — neutral
		}
		return 100.0 // All gains, no losses
	}
	rs := r.avgGain / r.avgLoss
	return 100.0 - (100.0 / (1.0 + rs))
}

// Value returns the current RSI value, or math.NaN() if not yet ready.
func (r *RSI) Value() float64 {
	if !r.ready {
		return math.NaN()
	}
	return r.computeRSI()
}

// Ready returns true once sufficient data has been received for valid output.
func (r *RSI) Ready() bool {
	return r.ready
}

// Reset clears all state for reuse.
func (r *RSI) Reset() {
	r.avgGain = 0
	r.avgLoss = 0
	r.prev = 0
	r.count = 0
	r.ready = false
	r.gainSum = 0
	r.lossSum = 0
}

// Period returns the configured period.
func (r *RSI) Period() int {
	return r.period
}
