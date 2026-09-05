package risk

import (
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func tickAt(hour int) model.Tick {
	t := time.Date(2025, 1, 15, hour, 30, 0, 0, time.UTC) // A Wednesday
	return model.Tick{
		Symbol:      "EURUSD",
		Bid:         1.10000,
		Ask:         1.10010,
		TimestampNs: t.UnixNano(),
	}
}

// --- Session Filter Tests ---

func TestSessionFilter_LondonHours_Allows(t *testing.T) {
	f := NewLondonSession()
	// 10:00 UTC — should be allowed (London 07-16)
	ok, _ := f.Allow(tickAt(10))
	if !ok {
		t.Error("expected London session to allow at 10:00 UTC")
	}
}

func TestSessionFilter_LondonHours_Rejects(t *testing.T) {
	f := NewLondonSession()
	// 03:00 UTC — outside London
	ok, reason := f.Allow(tickAt(3))
	if ok {
		t.Error("expected London session to reject at 03:00 UTC")
	}
	if reason == "" {
		t.Error("expected rejection reason")
	}
}

func TestSessionFilter_NYHours(t *testing.T) {
	f := NewNewYorkSession()
	// 14:00 UTC — NY session (12-21)
	ok, _ := f.Allow(tickAt(14))
	if !ok {
		t.Error("expected NY session to allow at 14:00 UTC")
	}

	// 22:00 UTC — outside NY
	ok, _ = f.Allow(tickAt(22))
	if ok {
		t.Error("expected NY session to reject at 22:00 UTC")
	}
}

func TestSessionFilter_Overlap(t *testing.T) {
	f := NewLondonNYOverlap()
	// 13:00 UTC — overlap (12-16)
	ok, _ := f.Allow(tickAt(13))
	if !ok {
		t.Error("expected overlap to allow at 13:00 UTC")
	}

	// 17:00 UTC — outside overlap
	ok, _ = f.Allow(tickAt(17))
	if ok {
		t.Error("expected overlap to reject at 17:00 UTC")
	}
}

func TestSessionFilter_MidnightWrap(t *testing.T) {
	// Asian session wraps midnight: 23:00 - 08:00
	f := NewSessionFilter([]SessionWindow{
		{Name: "Asian", StartHour: 23, EndHour: 8},
	})

	// 01:00 UTC — inside asian
	ok, _ := f.Allow(tickAt(1))
	if !ok {
		t.Error("expected Asian session to allow at 01:00 UTC")
	}

	// 23:00 UTC — inside asian
	ok, _ = f.Allow(tickAt(23))
	if !ok {
		t.Error("expected Asian session to allow at 23:00 UTC")
	}

	// 12:00 UTC — outside asian
	ok, _ = f.Allow(tickAt(12))
	if ok {
		t.Error("expected Asian session to reject at 12:00 UTC")
	}
}

func TestSessionFilter_MultiSession(t *testing.T) {
	f := NewMultiSession(
		SessionWindow{Name: "London", StartHour: 7, EndHour: 16},
		SessionWindow{Name: "NewYork", StartHour: 12, EndHour: 21},
	)

	// 09:00 — London only
	ok, _ := f.Allow(tickAt(9))
	if !ok {
		t.Error("expected multi-session to allow at 09:00 (London)")
	}

	// 18:00 — NY only
	ok, _ = f.Allow(tickAt(18))
	if !ok {
		t.Error("expected multi-session to allow at 18:00 (NY)")
	}

	// 04:00 — neither
	ok, _ = f.Allow(tickAt(4))
	if ok {
		t.Error("expected multi-session to reject at 04:00")
	}
}

// --- Spread Anomaly Filter Tests ---

func TestSpreadAnomaly_AllowsDuringWarmup(t *testing.T) {
	f := NewSpreadAnomalyFilter(2.0, 100)
	tick := model.Tick{Symbol: "EURUSD", Bid: 1.10000, Ask: 1.10050} // 5 pips spread
	f.OnTick(tick)

	ok, _ := f.Allow(tick)
	if !ok {
		t.Error("expected filter to allow during warmup")
	}
}

func TestSpreadAnomaly_RejectsSpike(t *testing.T) {
	f := NewSpreadAnomalyFilter(2.0, 10) // Short warmup for testing

	// Feed normal spread ticks (~1 pip)
	for i := 0; i < 20; i++ {
		tick := model.Tick{Symbol: "EURUSD", Bid: 1.10000, Ask: 1.10010}
		f.OnTick(tick)
	}

	if !f.Ready() {
		t.Fatal("expected filter to be ready after 20 ticks")
	}

	// Now send a spike (~5 pips = 5x normal)
	spike := model.Tick{Symbol: "EURUSD", Bid: 1.10000, Ask: 1.10050}
	ok, reason := f.Allow(spike)
	if ok {
		t.Error("expected filter to reject spread spike")
	}
	if reason == "" {
		t.Error("expected rejection reason for spike")
	}
}

func TestSpreadAnomaly_AllowsNormal(t *testing.T) {
	f := NewSpreadAnomalyFilter(2.0, 10)

	// Feed consistent ~1 pip spreads
	for i := 0; i < 20; i++ {
		tick := model.Tick{Symbol: "EURUSD", Bid: 1.10000, Ask: 1.10010}
		f.OnTick(tick)
	}

	// Normal spread should pass
	normal := model.Tick{Symbol: "EURUSD", Bid: 1.10000, Ask: 1.10012} // 1.2 pips
	ok, _ := f.Allow(normal)
	if !ok {
		t.Error("expected filter to allow normal spread")
	}
}

// --- News Filter Tests ---

func TestNewsFilter_BlocksHighImpact(t *testing.T) {
	f := NewNewsFilter(15*time.Minute, 10*time.Minute)

	eventTime := time.Now().UTC()
	f.AddEvent(NewsEvent{
		Time:     eventTime,
		Duration: 5 * time.Minute,
		Impact:   "high",
		Title:    "NFP Release",
	})

	// During event — should be blocked
	tick := model.Tick{TimestampNs: eventTime.Add(1 * time.Minute).UnixNano()}
	ok, _ := f.Allow(tick)
	if ok {
		t.Error("expected filter to block during high-impact event")
	}
}

func TestNewsFilter_AllowsLowImpact(t *testing.T) {
	f := NewNewsFilter(15*time.Minute, 10*time.Minute)

	eventTime := time.Now().UTC()
	f.AddEvent(NewsEvent{
		Time:     eventTime,
		Duration: 5 * time.Minute,
		Impact:   "low",
		Title:    "Minor Data",
	})

	// During low-impact event — should be allowed
	tick := model.Tick{TimestampNs: eventTime.Add(1 * time.Minute).UnixNano()}
	ok, _ := f.Allow(tick)
	if !ok {
		t.Error("expected filter to allow during low-impact event")
	}
}

func TestNewsFilter_AllowsOutsideWindow(t *testing.T) {
	f := NewNewsFilter(15*time.Minute, 10*time.Minute)

	eventTime := time.Now().UTC().Add(-1 * time.Hour) // Event was 1 hour ago
	f.AddEvent(NewsEvent{
		Time:     eventTime,
		Duration: 5 * time.Minute,
		Impact:   "high",
		Title:    "Old NFP",
	})

	// Well after event — should be allowed
	tick := model.Tick{TimestampNs: time.Now().UnixNano()}
	ok, _ := f.Allow(tick)
	if !ok {
		t.Error("expected filter to allow well after event window")
	}
}

func TestNewsFilter_ClearPastEvents(t *testing.T) {
	f := NewNewsFilter(15*time.Minute, 10*time.Minute)

	// Add past event
	f.AddEvent(NewsEvent{
		Time:     time.Now().UTC().Add(-2 * time.Hour),
		Duration: 5 * time.Minute,
		Impact:   "high",
		Title:    "Past Event",
	})

	// Add future event
	f.AddEvent(NewsEvent{
		Time:     time.Now().UTC().Add(1 * time.Hour),
		Duration: 5 * time.Minute,
		Impact:   "high",
		Title:    "Future Event",
	})

	f.ClearPastEvents()

	// Should have only 1 event left
	if f.IsBlocked(time.Now().UTC().Add(-1 * time.Hour)) {
		t.Error("past event should have been cleared")
	}
}
