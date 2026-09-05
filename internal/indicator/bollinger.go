// Package indicator provides low-latency technical indicators for quantitative analysis.
package indicator

import (
	"math"
)

// Bollinger holds state for computing Bollinger Bands over a sliding window.
// Zero heap allocations after initialization.
type Bollinger struct {
	period     int
	k          float64
	buf        []float64
	pos        int
	count      int
	sum        float64
	lastResult BollingerResult
}

// BollingerResult holds the computed Bollinger Band values.
type BollingerResult struct {
	Upper     float64 // Upper band (SMA + k*stdDev)
	Middle    float64 // Middle band (SMA)
	Lower     float64 // Lower band (SMA - k*stdDev)
	Bandwidth float64 // (Upper - Lower) / Middle
	PercentB  float64 // (Price - Lower) / (Upper - Lower)
	Valid     bool    // True when enough samples (period) have been processed
}

// NewBollinger creates a new Bollinger Bands indicator.
func NewBollinger(period int, k float64) *Bollinger {
	if period < 2 {
		period = 20
	}
	if k <= 0 {
		k = 2.0
	}
	return &Bollinger{
		period: period,
		k:      k,
		buf:    make([]float64, period),
	}
}

// Update adds a new price sample and returns the updated Bollinger Bands.
func (b *Bollinger) Update(price float64) BollingerResult {
	if b.count < b.period {
		b.buf[b.pos] = price
		b.sum += price
		b.pos = (b.pos + 1) % b.period
		b.count++
		if b.count < b.period {
			b.lastResult = BollingerResult{Valid: false}
			return b.lastResult
		}
	} else {
		old := b.buf[b.pos]
		b.sum = b.sum - old + price
		b.buf[b.pos] = price
		b.pos = (b.pos + 1) % b.period
	}

	mean := b.sum / float64(b.period)

	var varianceSum float64
	for i := 0; i < b.period; i++ {
		diff := b.buf[i] - mean
		varianceSum += diff * diff
	}
	stdDev := math.Sqrt(varianceSum / float64(b.period))

	upper := mean + (b.k * stdDev)
	lower := mean - (b.k * stdDev)

	var bandwidth float64
	if mean > 0 {
		bandwidth = (upper - lower) / mean
	}

	var percentB float64
	bandRange := upper - lower
	if bandRange > 0 {
		percentB = (price - lower) / bandRange
	} else {
		percentB = 0.5
	}

	b.lastResult = BollingerResult{
		Upper:     upper,
		Middle:    mean,
		Lower:     lower,
		Bandwidth: bandwidth,
		PercentB:  percentB,
		Valid:     true,
	}
	return b.lastResult
}

// Last returns the most recently computed Bollinger Bands without mutating state.
func (b *Bollinger) Last() BollingerResult {
	return b.lastResult
}

// Reset clears the indicator state.
func (b *Bollinger) Reset() {
	b.pos = 0
	b.count = 0
	b.sum = 0
	b.lastResult = BollingerResult{}
	for i := range b.buf {
		b.buf[i] = 0
	}
}
