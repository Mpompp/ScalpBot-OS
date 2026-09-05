package risk

import (
	"fmt"
	"sync"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// TickVelocityFilter measures the rate of tick arrival (Ticks Per Second / TPS)
// within a rolling sliding window to protect against low-liquidity market traps
// and sudden volatility explosions. Zero-allocation on hot-path.
type TickVelocityFilter struct {
	windowDuration int64   // Window size in nanoseconds (e.g. 5 seconds)
	minTPS         float64 // Minimum required ticks/sec to enter
	maxTPS         float64 // Maximum allowed ticks/sec (anti-news surge)

	timestamps []int64 // Circular ring of timestamps
	head       int
	count      int
	capacity   int
	mu         sync.RWMutex
}

// NewTickVelocityFilter creates a new velocity filter.
func NewTickVelocityFilter(window time.Duration, minTPS, maxTPS float64) *TickVelocityFilter {
	if window <= 0 {
		window = 5 * time.Second
	}
	capSize := 256
	return &TickVelocityFilter{
		windowDuration: window.Nanoseconds(),
		minTPS:         minTPS,
		maxTPS:         maxTPS,
		timestamps:     make([]int64, capSize),
		capacity:       capSize,
	}
}

// OnTick records a tick arrival timestamp into the ring buffer.
func (f *TickVelocityFilter) OnTick(tick model.Tick) {
	ts := tick.TimestampNs
	if ts == 0 {
		ts = time.Now().UnixNano()
	}

	f.mu.Lock()
	f.timestamps[f.head] = ts
	f.head = (f.head + 1) % f.capacity
	if f.count < f.capacity {
		f.count++
	}
	f.mu.Unlock()
}

// CurrentTPS calculates the instantaneous ticks-per-second within the rolling window.
func (f *TickVelocityFilter) CurrentTPS(nowNs int64) float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.count < 2 {
		return 0
	}

	cutoff := nowNs - f.windowDuration
	ticksInWindow := 0

	// Walk backward through timestamps
	idx := (f.head - 1 + f.capacity) % f.capacity
	for i := 0; i < f.count; i++ {
		t := f.timestamps[idx]
		if t < cutoff {
			break
		}
		ticksInWindow++
		idx = (idx - 1 + f.capacity) % f.capacity
	}

	windowSeconds := float64(f.windowDuration) / 1e9
	if windowSeconds <= 0 {
		return 0
	}

	return float64(ticksInWindow) / windowSeconds
}

// Allow evaluates whether market tick velocity is within safe liquidity bounds.
func (f *TickVelocityFilter) Allow(tick model.Tick) (bool, string) {
	ts := tick.TimestampNs
	if ts == 0 {
		ts = time.Now().UnixNano()
	}

	tps := f.CurrentTPS(ts)

	// Check Minimum TPS (Liquidity Guard)
	if f.minTPS > 0 && tps < f.minTPS {
		return false, fmt.Sprintf("liquidity too low (TPS=%.1f < min=%.1f)", tps, f.minTPS)
	}

	// Check Maximum TPS (Volatility Surge Guard)
	if f.maxTPS > 0 && tps > f.maxTPS {
		return false, fmt.Sprintf("volatility surge / flash crash risk (TPS=%.1f > max=%.1f)", tps, f.maxTPS)
	}

	return true, ""
}

// Name returns the identifier for this filter.
func (f *TickVelocityFilter) Name() string {
	return "TickVelocityFilter"
}
