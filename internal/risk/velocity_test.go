package risk

import (
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestTickVelocityFilter_Allow(t *testing.T) {
	window := 2 * time.Second
	minTPS := 2.0
	maxTPS := 10.0
	filter := NewTickVelocityFilter(window, minTPS, maxTPS)

	now := time.Now()
	tick := model.Tick{Symbol: "EURUSD", Bid: 1.1000, Ask: 1.1002, TimestampNs: now.UnixNano()}

	// 1. Initial state (0 or 1 tick) -> TPS is 0, below minTPS=2.0
	filter.OnTick(tick)
	allowed, reason := filter.Allow(tick)
	if allowed {
		t.Errorf("expected rejection due to low liquidity at 1 tick, got allowed")
	}
	if reason == "" {
		t.Errorf("expected non-empty rejection reason")
	}

	// 2. Push 8 ticks in the last 1 second -> TPS = 8 ticks / 2s window = 4.0 TPS (between 2.0 and 10.0)
	for i := 0; i < 8; i++ {
		tTick := model.Tick{
			Symbol:      "EURUSD",
			Bid:         1.1000 + float64(i)*0.00001,
			Ask:         1.1002 + float64(i)*0.00001,
			TimestampNs: now.Add(time.Duration(i*100) * time.Millisecond).UnixNano(),
		}
		filter.OnTick(tTick)
	}

	lastTick := model.Tick{
		Symbol:      "EURUSD",
		Bid:         1.1001,
		Ask:         1.1003,
		TimestampNs: now.Add(900 * time.Millisecond).UnixNano(),
	}
	allowed, _ = filter.Allow(lastTick)
	if !allowed {
		t.Errorf("expected allowed for normal TPS=4.0, got rejected")
	}

	// 3. Push 30 ticks -> TPS = 30 / 2s = 15.0 TPS (> maxTPS=10.0)
	for i := 0; i < 30; i++ {
		filter.OnTick(model.Tick{
			Symbol:      "EURUSD",
			Bid:         1.1000,
			Ask:         1.1002,
			TimestampNs: now.Add(1000*time.Millisecond + time.Duration(i*10)*time.Millisecond).UnixNano(),
		})
	}
	surgeTick := model.Tick{
		Symbol:      "EURUSD",
		Bid:         1.1000,
		Ask:         1.1002,
		TimestampNs: now.Add(1500 * time.Millisecond).UnixNano(),
	}
	allowed, reason = filter.Allow(surgeTick)
	if allowed {
		t.Errorf("expected rejection due to volatility surge (> 10.0 TPS), got allowed")
	}
	if reason == "" {
		t.Errorf("expected rejection reason for volatility surge")
	}
}

func BenchmarkTickVelocityFilter_OnTick(b *testing.B) {
	filter := NewTickVelocityFilter(5*time.Second, 2.0, 50.0)
	tick := model.Tick{
		Symbol:      "EURUSD",
		Bid:         1.1000,
		Ask:         1.1002,
		TimestampNs: time.Now().UnixNano(),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		filter.OnTick(tick)
	}
}

func BenchmarkTickVelocityFilter_Allow(b *testing.B) {
	filter := NewTickVelocityFilter(5*time.Second, 2.0, 50.0)
	now := time.Now().UnixNano()
	for i := 0; i < 20; i++ {
		filter.OnTick(model.Tick{
			Symbol:      "EURUSD",
			TimestampNs: now + int64(i)*100000000,
		})
	}
	tick := model.Tick{
		Symbol:      "EURUSD",
		TimestampNs: now + 2000000000,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		filter.Allow(tick)
	}
}
