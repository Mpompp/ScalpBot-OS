package risk

import (
	"fmt"
	"sync"

	"github.com/pompbot/scalpbot/internal/model"
)

// SpreadAnomalyFilter detects spread spikes by comparing current spread
// against a rolling EMA average. Rejects trading when spread exceeds
// a configurable multiplier of the average (indicating illiquidity or news).
type SpreadAnomalyFilter struct {
	avgSpread  float64 // EMA of spread in pips
	multiplier float64 // Spike threshold multiplier (e.g. 2.0)
	emaPeriod  int     // EMA lookback for average spread
	emaAlpha   float64 // EMA smoothing factor
	count      int     // Number of updates received
	ready      bool    // True once warmup is complete
	mu         sync.RWMutex
}

// NewSpreadAnomalyFilter creates a spread spike detector.
// multiplier: reject if current spread > multiplier × average (e.g. 2.0 = 2× normal)
// emaPeriod: lookback for rolling average calculation (e.g. 100)
func NewSpreadAnomalyFilter(multiplier float64, emaPeriod int) *SpreadAnomalyFilter {
	if multiplier <= 1.0 {
		multiplier = 2.0
	}
	if emaPeriod <= 0 {
		emaPeriod = 100
	}
	return &SpreadAnomalyFilter{
		multiplier: multiplier,
		emaPeriod:  emaPeriod,
		emaAlpha:   2.0 / float64(emaPeriod+1),
	}
}

// Name returns the filter identifier.
func (f *SpreadAnomalyFilter) Name() string {
	return "SpreadAnomalyFilter"
}

// OnTick updates the rolling spread average. Call on every tick
// regardless of whether trading is active (builds baseline).
func (f *SpreadAnomalyFilter) OnTick(tick model.Tick) {
	spreadPips := tick.SpreadPips(model.PipMultiplier(tick.Symbol))
	
	f.mu.Lock()
	defer f.mu.Unlock()

	f.count++

	if !f.ready {
		// Warmup: use simple running average
		f.avgSpread += (spreadPips - f.avgSpread) / float64(f.count)
		if f.count >= f.emaPeriod {
			f.ready = true
		}
		return
	}

	// EMA update
	f.avgSpread = (spreadPips-f.avgSpread)*f.emaAlpha + f.avgSpread
}

// Allow returns true if the current spread is within normal range.
func (f *SpreadAnomalyFilter) Allow(tick model.Tick) (bool, string) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if !f.ready {
		// During warmup, allow trading (insufficient data to judge)
		return true, ""
	}

	spreadPips := tick.SpreadPips(model.PipMultiplier(tick.Symbol))
	threshold := f.avgSpread * f.multiplier

	if spreadPips > threshold {
		return false, fmt.Sprintf("spread spike: %.2f pips > %.2f (%.1f× avg %.2f)",
			spreadPips, threshold, f.multiplier, f.avgSpread)
	}

	return true, ""
}

// AvgSpread returns the current rolling average spread in pips.
func (f *SpreadAnomalyFilter) AvgSpread() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.avgSpread
}

// Ready returns true once warmup is complete.
func (f *SpreadAnomalyFilter) Ready() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.ready
}
