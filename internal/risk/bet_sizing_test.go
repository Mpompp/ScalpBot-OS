package risk

import (
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestEvaluateWithConfidence_MarcosBetSizing(t *testing.T) {
	cfg := ManagerConfig{
		InitialEquity:    1000.0,
		RiskPerTrade:     0.01,
		MaxDailyDrawdown: 0.05,
		MaxSpreadPips:    50.0,
		MaxOpenPositions: 2,
		MinLotSize:       0.01,
		MaxLotSize:       0.05,
	}
	mgr := NewManager(cfg)

	tick := model.Tick{
		Symbol:      "XAUUSD",
		Bid:         4400.00,
		Ask:         4400.20,
		TimestampNs: time.Now().UnixNano(),
	}

	sig := model.Signal{
		Type:       model.Buy,
		Symbol:     "XAUUSD",
		Price:      4400.20,
		StopLoss:   4395.00,
		TakeProfit: 4412.00,
	}

	atr := 3.00

	// 1. Low/Baseline Confidence (0.50) -> Should yield MinLotSize 0.01
	orderLow, err := mgr.EvaluateWithConfidence(sig, tick, atr, 0.50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orderLow.Lots != 0.01 {
		t.Errorf("expected 0.01 lots for conf 0.50, got %.2f", orderLow.Lots)
	}

	// 2. High Confidence (0.85) -> Should scale up to ~0.04 lots
	mgr.openPositions = 0
	orderHigh, err := mgr.EvaluateWithConfidence(sig, tick, atr, 0.85)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orderHigh.Lots < 0.03 || orderHigh.Lots > 0.05 {
		t.Errorf("expected 0.03 - 0.05 lots for conf 0.85, got %.2f", orderHigh.Lots)
	}

	// 3. User Fixed Lot Override Priority -> Should bypass and strictly use fixed lot
	mgr.openPositions = 0
	mgr.SetFixedLotSize(0.10)
	orderOverride, err := mgr.EvaluateWithConfidence(sig, tick, atr, 0.85)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orderOverride.Lots != 0.10 {
		t.Errorf("expected 0.10 lots under manual override, got %.2f", orderOverride.Lots)
	}
}
