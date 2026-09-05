package marketdata_test

import (
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/marketdata"
	"github.com/pompbot/scalpbot/internal/model"
)

func TestMultiTimeframeManager(t *testing.T) {
	mgr := marketdata.NewMultiTimeframeManager(50)

	baseTime := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)

	// Simulate 10 M5 candles trending up
	price := 1.1000
	for i := 0; i < 60; i++ {
		curTime := baseTime.Add(time.Duration(i) * time.Minute)
		price += 0.0002 // Steadily increasing price
		tick := model.Tick{
			Symbol:      "EURUSD",
			Bid:         price,
			Ask:         price + 0.0001,
			TimestampNs: curTime.UnixNano(),
		}
		mgr.OnTick(tick)
	}

	// In an uptrend, Buy should be allowed with high score
	allowedBuy, scoreBuy, reasonBuy := mgr.CheckConfluence("EURUSD", model.Buy)
	if !allowedBuy {
		t.Errorf("expected Buy to be allowed in uptrend, got false (reason: %s)", reasonBuy)
	}
	if scoreBuy < 0.50 {
		t.Errorf("expected Buy score >= 0.50, got %.2f", scoreBuy)
	}

	// In a strong uptrend, Sell should be flagged or have lower score
	_, scoreSell, _ := mgr.CheckConfluence("EURUSD", model.Sell)
	if scoreSell > scoreBuy {
		t.Errorf("expected Sell score (%.2f) to be lower than Buy score (%.2f) in uptrend", scoreSell, scoreBuy)
	}
}
