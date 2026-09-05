package executor

import (
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestTelemetryTracker(t *testing.T) {
	tracker := NewTelemetryTracker()

	order := model.OrderRequest{
		Symbol: "EURUSD",
		Side:   model.SideBuy,
		Lots:   0.10,
		Price:  1.10000,
	}
	fill := model.Position{
		OrderID:    "ORD-1",
		Symbol:     "EURUSD",
		Side:       model.SideBuy,
		Lots:       0.10,
		EntryPrice: 1.10002, // 0.2 pips slippage
	}

	tracker.RecordFill(order, fill, 12*time.Millisecond)
	tracker.RecordClose(CloseEvent{OrderID: "ORD-1", Reason: "PARTIAL_TP1_HIT", IsPartial: true, Lots: 0.05})
	tracker.RecordClose(CloseEvent{OrderID: "ORD-1", Reason: "TP2_HIT", IsPartial: false, Lots: 0.05})
	tracker.RecordRejection()

	stats := tracker.Stats()
	if stats.TotalFilled != 1 {
		t.Errorf("expected TotalFilled 1, got %d", stats.TotalFilled)
	}
	if stats.TotalRejected != 1 {
		t.Errorf("expected TotalRejected 1, got %d", stats.TotalRejected)
	}
	if stats.TotalPartialCloses != 1 {
		t.Errorf("expected TotalPartialCloses 1, got %d", stats.TotalPartialCloses)
	}
	if stats.TotalFullCloses != 1 {
		t.Errorf("expected TotalFullCloses 1, got %d", stats.TotalFullCloses)
	}
	if stats.AverageLatencyMs < 11.0 || stats.AverageLatencyMs > 13.0 {
		t.Errorf("expected ~12ms latency, got %.2f", stats.AverageLatencyMs)
	}
	if stats.CloseReasons["PARTIAL_TP1_HIT"] != 1 {
		t.Errorf("expected PARTIAL_TP1_HIT count 1, got %d", stats.CloseReasons["PARTIAL_TP1_HIT"])
	}
	if stats.CloseReasons["TP2_HIT"] != 1 {
		t.Errorf("expected TP2_HIT count 1, got %d", stats.CloseReasons["TP2_HIT"])
	}
}
