package executor

import (
	"math"
	"sync"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// ExecutionRecord captures detailed execution quality metrics for a single trade.
type ExecutionRecord struct {
	OrderID        string
	Symbol         string
	Side           model.OrderSide
	Lots           float64
	RequestedPrice float64
	FillPrice      float64
	SlippagePips   float64
	Latency        time.Duration
	TimestampNs    int64
}

// TelemetryTracker aggregates execution quality, broker slippage, and close reason metrics.
type TelemetryTracker struct {
	records            []ExecutionRecord
	totalFilled        int64
	totalRejected      int64
	totalPartialCloses int64
	totalFullCloses    int64
	totalSlippagePips  float64
	maxSlippagePips    float64
	totalLatencyNs     int64
	closeReasons       map[string]int64
	mu                 sync.RWMutex
}

// NewTelemetryTracker creates a new telemetry instance.
func NewTelemetryTracker() *TelemetryTracker {
	return &TelemetryTracker{
		records:      make([]ExecutionRecord, 0, 128),
		closeReasons: make(map[string]int64, 8),
	}
}

// RecordFill logs an executed order and calculates slippage & latency.
func (t *TelemetryTracker) RecordFill(order model.OrderRequest, fillPos model.Position, latency time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	pipMult := model.PipMultiplier(order.Symbol)
	var slipPips float64

	if order.Price > 0 && fillPos.EntryPrice > 0 {
		if order.Side == model.SideBuy {
			slipPips = (fillPos.EntryPrice - order.Price) * pipMult
		} else {
			slipPips = (order.Price - fillPos.EntryPrice) * pipMult
		}
	}

	t.totalFilled++
	t.totalSlippagePips += slipPips
	if math.Abs(slipPips) > t.maxSlippagePips {
		t.maxSlippagePips = math.Abs(slipPips)
	}
	t.totalLatencyNs += latency.Nanoseconds()

	rec := ExecutionRecord{
		OrderID:        fillPos.OrderID,
		Symbol:         order.Symbol,
		Side:           order.Side,
		Lots:           fillPos.Lots,
		RequestedPrice: order.Price,
		FillPrice:      fillPos.EntryPrice,
		SlippagePips:   slipPips,
		Latency:        latency,
		TimestampNs:    time.Now().UnixNano(),
	}

	if len(t.records) < 1024 {
		t.records = append(t.records, rec)
	}
}

// RecordRejection logs an order rejected by broker or risk engine.
func (t *TelemetryTracker) RecordRejection() {
	t.mu.Lock()
	t.totalRejected++
	t.mu.Unlock()
}

// RecordClose logs a position closure event with its specific reason.
func (t *TelemetryTracker) RecordClose(ce CloseEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if ce.IsPartial {
		t.totalPartialCloses++
	} else {
		t.totalFullCloses++
	}
	t.closeReasons[ce.Reason]++
}

// Snapshot returns summary statistics.
type TelemetrySnapshot struct {
	TotalFilled        int64            `json:"total_filled"`
	TotalRejected      int64            `json:"total_rejected"`
	TotalPartialCloses int64            `json:"total_partial_closes"`
	TotalFullCloses    int64            `json:"total_full_closes"`
	AverageSlippage    float64          `json:"average_slippage_pips"`
	MaxSlippage        float64          `json:"max_slippage_pips"`
	AverageLatencyMs   float64          `json:"average_latency_ms"`
	CloseReasons       map[string]int64 `json:"close_reasons"`
}

// Stats returns a copy of current execution telemetry.
func (t *TelemetryTracker) Stats() TelemetrySnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	avgSlip := 0.0
	avgLatMs := 0.0
	if t.totalFilled > 0 {
		avgSlip = t.totalSlippagePips / float64(t.totalFilled)
		avgLatMs = float64(t.totalLatencyNs) / float64(t.totalFilled) / 1e6
	}

	reasonsCopy := make(map[string]int64, len(t.closeReasons))
	for k, v := range t.closeReasons {
		reasonsCopy[k] = v
	}

	return TelemetrySnapshot{
		TotalFilled:        t.totalFilled,
		TotalRejected:      t.totalRejected,
		TotalPartialCloses: t.totalPartialCloses,
		TotalFullCloses:    t.totalFullCloses,
		AverageSlippage:    avgSlip,
		MaxSlippage:        t.maxSlippagePips,
		AverageLatencyMs:   avgLatMs,
		CloseReasons:       reasonsCopy,
	}
}
