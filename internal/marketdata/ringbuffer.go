// Package marketdata provides high-performance tick storage and OHLCV aggregation.
// Designed for zero-allocation on the hot path using pre-allocated ring buffers.
package marketdata

import (
	"sync"

	"github.com/pompbot/scalpbot/internal/model"
)

// TickRingBuffer is a fixed-size circular buffer for tick storage.
// It overwrites the oldest entry on overflow, providing O(1) insert with
// zero heap allocation after initialization. Thread-safe via sync.RWMutex.
type TickRingBuffer struct {
	buf   []model.Tick // Pre-allocated fixed-size backing array
	size  int          // Capacity of the buffer
	head  int          // Next write position
	count int          // Number of valid entries (max = size)
	mu    sync.RWMutex
}

// NewTickRingBuffer creates a ring buffer with the given capacity.
// The backing slice is allocated once and never resized.
func NewTickRingBuffer(capacity int) *TickRingBuffer {
	if capacity <= 0 {
		capacity = 1024
	}
	return &TickRingBuffer{
		buf:  make([]model.Tick, capacity),
		size: capacity,
	}
}

// Push inserts a tick into the buffer, overwriting the oldest entry if full.
// O(1) time, zero allocation.
func (rb *TickRingBuffer) Push(tick model.Tick) {
	rb.mu.Lock()
	rb.buf[rb.head] = tick
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
	rb.mu.Unlock()
}

// Last returns the most recent tick and true, or a zero Tick and false if empty.
func (rb *TickRingBuffer) Last() (model.Tick, bool) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return model.Tick{}, false
	}
	idx := (rb.head - 1 + rb.size) % rb.size
	return rb.buf[idx], true
}

// Len returns the number of ticks currently stored.
func (rb *TickRingBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// latestPool reuses slices for Latest() results to avoid per-call allocations.
var latestPool = sync.Pool{
	New: func() interface{} {
		s := make([]model.Tick, 0, 256)
		return &s
	},
}

// Latest returns the last n ticks in chronological order (oldest first).
// The returned slice is borrowed from a sync.Pool — caller must not retain it
// beyond the current scope. Pass it to ReleaseSlice() when done.
// If n > count, returns all available ticks.
func (rb *TickRingBuffer) Latest(n int) []model.Tick {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.count {
		n = rb.count
	}
	if n == 0 {
		return nil
	}

	// Borrow slice from pool
	ptr := latestPool.Get().(*[]model.Tick)
	result := (*ptr)[:0]

	// Ensure capacity
	if cap(result) < n {
		result = make([]model.Tick, 0, n)
	}

	// Read from oldest to newest
	start := (rb.head - n + rb.size) % rb.size
	for i := 0; i < n; i++ {
		idx := (start + i) % rb.size
		result = append(result, rb.buf[idx])
	}

	*ptr = result
	return result
}

// ReleaseSlice returns a slice obtained from Latest() back to the pool.
func ReleaseSlice(s []model.Tick) {
	if s == nil {
		return
	}
	s = s[:0]
	latestPool.Put(&s)
}

// SnapshotLast copies the most recent tick into dst without allocation.
// Returns true if a tick was available, false if buffer is empty.
func (rb *TickRingBuffer) SnapshotLast(dst *model.Tick) bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return false
	}
	idx := (rb.head - 1 + rb.size) % rb.size
	*dst = rb.buf[idx]
	return true
}
