package marketdata

import (
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// OHLCVAggregator builds candles from a stream of ticks in memory.
// It maintains a single partial candle and emits a completed candle
// when the configured period elapses. Zero allocation during OnTick
// after the initial struct creation.
type OHLCVAggregator struct {
	periodNs      int64        // Candle period in nanoseconds
	current       model.Candle // Partial candle being built
	hasData       bool         // Whether current candle has any ticks
	periodEnd     int64        // Timestamp when current period closes
	lastMid       float64      // Previous tick mid price for volume direction
	lastDirection int          // +1 = last uptick, -1 = last downtick, 0 = unknown
}

// NewOHLCVAggregator creates an aggregator with the given candle period.
// Common periods: 1*time.Minute (M1), 5*time.Minute (M5), 15*time.Minute (M15), etc.
func NewOHLCVAggregator(period time.Duration) *OHLCVAggregator {
	return &OHLCVAggregator{
		periodNs: period.Nanoseconds(),
	}
}

// OnTick processes an incoming tick and returns a completed candle if the
// period has closed. The bool return indicates whether a candle was emitted.
func (a *OHLCVAggregator) OnTick(tick model.Tick) (model.Candle, bool) {
	mid := tick.MidPrice()

	// First tick ever — initialize period boundaries
	if !a.hasData {
		a.startNewCandle(tick.Symbol, mid, tick.TimestampNs)
		a.lastMid = mid
		return model.Candle{}, false
	}

	// Check if this tick falls into a new period
	if tick.TimestampNs >= a.periodEnd {
		// Close current candle
		completed := a.current
		completed.Volume = float64(completed.TickCount)
		if completed.BuyerVol+completed.SellerVol == 0 {
			completed.BuyerVol = completed.Volume * 0.5
			completed.SellerVol = completed.Volume * 0.5
		}

		// Start new candle with this tick
		a.startNewCandle(tick.Symbol, mid, tick.TimestampNs)
		a.lastMid = mid

		return completed, true
	}

	// Tick-Rule Volume Classification:
	// Up-tick → BuyVol, Down-tick → SellVol, Flat-tick → follow last valid direction
	if mid > a.lastMid {
		a.current.BuyerVol++
		a.lastDirection = 1
	} else if mid < a.lastMid {
		a.current.SellerVol++
		a.lastDirection = -1
	} else {
		// Flat tick: split evenly or follow last valid direction
		if a.lastDirection > 0 {
			a.current.BuyerVol += 0.5
			a.current.SellerVol += 0.5
		} else if a.lastDirection < 0 {
			a.current.BuyerVol += 0.5
			a.current.SellerVol += 0.5
		} else {
			a.current.BuyerVol += 0.5
			a.current.SellerVol += 0.5
		}
	}
	a.lastMid = mid

	// Update current candle with this tick
	a.current.Close = mid
	if mid > a.current.High {
		a.current.High = mid
	}
	if mid < a.current.Low {
		a.current.Low = mid
	}
	a.current.TickCount++

	return model.Candle{}, false
}

// Current returns the partial candle currently being built.
// Returns zero candle and false if no data has been received yet.
func (a *OHLCVAggregator) Current() (model.Candle, bool) {
	if !a.hasData {
		return model.Candle{}, false
	}
	return a.current, true
}

// startNewCandle initializes a fresh candle period.
func (a *OHLCVAggregator) startNewCandle(symbol string, price float64, timestampNs int64) {
	// Align to period boundary
	periodStart := (timestampNs / a.periodNs) * a.periodNs

	a.current = model.Candle{
		Symbol:      symbol,
		Open:        price,
		High:        price,
		Low:         price,
		Close:       price,
		Volume:      1,
		BuyerVol:    0,
		SellerVol:   0,
		TickCount:   1,
		TimestampNs: periodStart,
	}
	a.periodEnd = periodStart + a.periodNs
	a.hasData = true
}

// Reset clears the aggregator state, discarding any partial candle.
func (a *OHLCVAggregator) Reset() {
	a.hasData = false
	a.periodEnd = 0
	a.current = model.Candle{}
	a.lastMid = 0
	a.lastDirection = 0
}

// MultiTimeframeAggregator concurrently aggregates M5, M15, and H1 bars from a single tick stream.
type MultiTimeframeAggregator struct {
	M5  *OHLCVAggregator
	M15 *OHLCVAggregator
	H1  *OHLCVAggregator
}

// NewMultiTimeframeAggregator creates a new MTF aggregator for M5, M15, and H1.
func NewMultiTimeframeAggregator() *MultiTimeframeAggregator {
	return &MultiTimeframeAggregator{
		M5:  NewOHLCVAggregator(5 * time.Minute),
		M15: NewOHLCVAggregator(15 * time.Minute),
		H1:  NewOHLCVAggregator(60 * time.Minute),
	}
}

// OnTick processes an incoming tick across all 3 timeframes without heap allocations.
func (m *MultiTimeframeAggregator) OnTick(tick model.Tick) (
	c5 model.Candle, closed5 bool,
	c15 model.Candle, closed15 bool,
	ch1 model.Candle, closedh1 bool,
) {
	c5, closed5 = m.M5.OnTick(tick)
	c15, closed15 = m.M15.OnTick(tick)
	ch1, closedh1 = m.H1.OnTick(tick)
	return
}
