package news

import (
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestCalendar_HasUpcomingHighImpact(t *testing.T) {
	cal := NewCalendarClient("")

	now := time.Now()
	nowNs := now.UnixNano()

	// Add high impact event in 10 minutes (USD CPI)
	cal.AddEvent(EconomicEvent{
		ID:          "EV-1",
		Title:       "Consumer Price Index (CPI) YoY",
		Currency:    "USD",
		Impact:      ImpactHigh,
		TimestampNs: now.Add(10 * time.Minute).UnixNano(),
	})

	// Add low impact event in 5 minutes (EUR German Trade Balance)
	cal.AddEvent(EconomicEvent{
		ID:          "EV-2",
		Title:       "Trade Balance",
		Currency:    "EUR",
		Impact:      ImpactLow,
		TimestampNs: now.Add(5 * time.Minute).UnixNano(),
	})

	// Check USD within 15min window -> Should find CPI
	hasNews, ev := cal.HasUpcomingHighImpact("USD", 15*time.Minute, 15*time.Minute, nowNs)
	if !hasNews || ev == nil {
		t.Fatalf("expected USD high impact news found")
	}
	if ev.Title != "Consumer Price Index (CPI) YoY" {
		t.Errorf("unexpected event title: %s", ev.Title)
	}

	// Check EUR within 15min window -> Low impact only -> Should NOT trigger high impact
	hasNewsEUR, _ := cal.HasUpcomingHighImpact("EUR", 15*time.Minute, 15*time.Minute, nowNs)
	if hasNewsEUR {
		t.Errorf("expected EUR low impact event to be ignored")
	}
}

func TestDynamicNewsBlackoutFilter_BlockAndAllow(t *testing.T) {
	cal := NewCalendarClient("")
	now := time.Now()

	// High impact USD event in 8 minutes
	cal.AddEvent(EconomicEvent{
		ID:          "NFP-1",
		Title:       "Non-Farm Payrolls",
		Currency:    "USD",
		Impact:      ImpactHigh,
		TimestampNs: now.Add(8 * time.Minute).UnixNano(),
	})

	filter := NewDynamicNewsBlackoutFilter(DefaultBlackoutConfig(), cal)

	// 1. EURUSD -> contains USD quote -> Must be BLOCKED
	tickEURUSD := model.Tick{
		Symbol:      "EURUSD",
		Bid:         1.1000,
		Ask:         1.1002,
		TimestampNs: now.UnixNano(),
	}
	allowed, reason := filter.Allow(tickEURUSD)
	if allowed {
		t.Fatalf("expected EURUSD blocked during USD NFP window, got allowed")
	}
	if reason == "" {
		t.Errorf("expected blackout reason string")
	}

	// 2. EURGBP -> does NOT contain USD -> Must be ALLOWED
	tickEURGBP := model.Tick{
		Symbol:      "EURGBP",
		Bid:         0.8500,
		Ask:         0.8502,
		TimestampNs: now.UnixNano(),
	}
	allowedGBP, _ := filter.Allow(tickEURGBP)
	if !allowedGBP {
		t.Fatalf("expected EURGBP allowed since it does not involve USD")
	}
}
