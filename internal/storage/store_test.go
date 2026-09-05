package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SaveAndLoad(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "scalpbot_test_storage")
	defer os.RemoveAll(tempDir)

	store := NewStore(tempDir)

	// Test Trade Persistence
	tr := TradeRecord{
		Ticket:    "12345",
		Symbol:    "XAUUSDc",
		Side:      "BUY",
		Lots:      0.01,
		Entry:     2450.0,
		Exit:      2452.5,
		NetPnL:    2.50,
		Pips:      25.0,
		Duration:  45 * time.Second,
		CloseTime: time.Now(),
	}

	if err := store.SaveTrade(tr); err != nil {
		t.Fatalf("failed to save trade: %v", err)
	}

	trades, err := store.LoadTrades()
	if err != nil {
		t.Fatalf("failed to load trades: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].Ticket != "12345" {
		t.Errorf("expected ticket 12345, got %s", trades[0].Ticket)
	}

	// Test Daily State Persistence
	ds := DailyState{
		Date:                time.Now().Format("2006-01-02"),
		DailyPnL:            15.50,
		ProfitTargetReached: false,
		CircuitOpen:         false,
	}
	if err := store.SaveDailyState(ds); err != nil {
		t.Fatalf("failed to save daily state: %v", err)
	}

	loadedState, err := store.LoadDailyState()
	if err != nil {
		t.Fatalf("failed to load daily state: %v", err)
	}
	if loadedState.DailyPnL != 15.50 {
		t.Errorf("expected daily PnL 15.50, got %.2f", loadedState.DailyPnL)
	}
}
