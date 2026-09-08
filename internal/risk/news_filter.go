package risk

import (
	"fmt"
	"sync"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// NewsEvent represents a scheduled economic event that may cause
// high volatility and spread spikes.
type NewsEvent struct {
	Time     time.Time // Event timestamp (UTC)
	Duration time.Duration // How long the impact window lasts
	Impact   string    // "high", "medium", "low"
	Title    string    // Event description
}

// NewsFilter blocks trading around high-impact economic events.
// Implements the MarketFilter interface.
type NewsFilter struct {
	events       []NewsEvent
	bufferBefore time.Duration // Blackout period before event
	bufferAfter  time.Duration // Blackout period after event
	mu           sync.RWMutex
}

// NewNewsFilter creates a news gate with configurable buffer periods.
// bufferBefore: how long before event to stop trading (e.g. 15min)
// bufferAfter: how long after event to resume trading (e.g. 10min)
func NewNewsFilter(bufferBefore, bufferAfter time.Duration) *NewsFilter {
	return &NewsFilter{
		bufferBefore: bufferBefore,
		bufferAfter:  bufferAfter,
	}
}

// SetEvents loads a list of upcoming economic events.
// Call this periodically (e.g. daily) to refresh the calendar.
// Thread-safe.
func (f *NewsFilter) SetEvents(events []NewsEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = events
}

// AddEvent adds a single event to the calendar.
// Thread-safe.
func (f *NewsFilter) AddEvent(event NewsEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

// Name returns the filter identifier.
func (f *NewsFilter) Name() string {
	return "NewsFilter"
}

// Allow returns true if the current time is not within any event blackout window.
// Only high-impact events trigger the blackout.
// Thread-safe.
func (f *NewsFilter) Allow(tick model.Tick) (bool, string) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	now := time.Unix(0, tick.TimestampNs).UTC()

	for i := range f.events {
		ev := &f.events[i]

		// Only block for high-impact events
		if ev.Impact != "high" {
			continue
		}

		// Blackout window: [event_time - bufferBefore, event_time + duration + bufferAfter]
		windowStart := ev.Time.Add(-f.bufferBefore)
		windowEnd := ev.Time.Add(ev.Duration + f.bufferAfter)

		if now.After(windowStart) && now.Before(windowEnd) {
			return false, fmt.Sprintf("news blackout: %s (window %s to %s)",
				ev.Title,
				windowStart.Format("15:04"),
				windowEnd.Format("15:04"))
		}
	}

	return true, ""
}

// IsBlocked is a convenience method checking if trading is blocked at time t.
func (f *NewsFilter) IsBlocked(t time.Time) bool {
	tick := model.Tick{TimestampNs: t.UnixNano()}
	allowed, _ := f.Allow(tick)
	return !allowed
}

// ClearPastEvents removes events that have fully passed (including buffer).
// Call periodically to prevent unbounded growth.
func (f *NewsFilter) ClearPastEvents() {
	now := time.Now().UTC()
	kept := f.events[:0] // Reuse backing array

	for i := range f.events {
		endWindow := f.events[i].Time.Add(f.events[i].Duration + f.bufferAfter)
		if endWindow.After(now) {
			kept = append(kept, f.events[i])
		}
	}

	f.events = kept
}
