// Package indicator provides streaming technical indicator calculations.
// All indicators use incremental O(1) computation with zero heap allocation
// per update. No external CGO dependencies — pure Go math only.
package indicator

import "math"

// EMA implements an Exponential Moving Average with streaming computation.
// After the warmup period, each Update() call is O(1) with zero allocation.
type EMA struct {
	period     int     // EMA lookback period
	multiplier float64 // Smoothing factor: 2 / (period + 1)
	current    float64 // Current EMA value
	count      int     // Number of values received
	sum        float64 // Running sum during warmup phase
	ready      bool    // True once warmup is complete
}

// NewEMA creates a new EMA calculator for the given period.
// Returns nil if period < 1.
func NewEMA(period int) *EMA {
	if period < 1 {
		return nil
	}
	return &EMA{
		period:     period,
		multiplier: 2.0 / float64(period+1),
	}
}

// Update feeds a new value and returns the current EMA.
// Returns math.NaN() during the warmup phase (fewer than 'period' values received).
func (e *EMA) Update(value float64) float64 {
	e.count++

	if !e.ready {
		e.sum += value
		if e.count >= e.period {
			// Warmup complete — seed with SMA
			e.current = e.sum / float64(e.period)
			e.ready = true
			return e.current
		}
		return math.NaN()
	}

	// EMA formula: EMA_today = (value - EMA_yesterday) * multiplier + EMA_yesterday
	e.current = (value-e.current)*e.multiplier + e.current
	return e.current
}

// Value returns the current EMA value, or math.NaN() if not yet ready.
func (e *EMA) Value() float64 {
	if !e.ready {
		return math.NaN()
	}
	return e.current
}

// Ready returns true once sufficient data has been received for valid output.
func (e *EMA) Ready() bool {
	return e.ready
}

// Reset clears all state, allowing reuse without reallocation.
func (e *EMA) Reset() {
	e.current = 0
	e.count = 0
	e.sum = 0
	e.ready = false
}

// Period returns the configured period.
func (e *EMA) Period() int {
	return e.period
}
