package broker

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// MockBroker simulates order execution for testing and backtesting.
// Configurable fill latency and slippage for realistic simulation.
type MockBroker struct {
	fillLatency time.Duration // Simulated execution latency
	slippagePct float64       // Slippage as fraction (e.g., 0.00005 = 0.5 pips on XAUUSD)
	rejectRate  float64       // Probability of order rejection [0.0, 1.0]
	orderSeq    atomic.Int64  // Atomic counter for unique order IDs
	mu          sync.Mutex
	rng         *rand.Rand    // Local RNG (not global — no contention)
}

// MockConfig holds configuration for the mock broker.
type MockConfig struct {
	FillLatency time.Duration // Simulated fill delay (e.g., 5ms)
	SlippagePct float64       // Max slippage fraction (e.g., 0.0001)
	RejectRate  float64       // Order rejection probability [0, 1]
}

// DefaultMockConfig returns a realistic mock configuration.
func DefaultMockConfig() MockConfig {
	return MockConfig{
		FillLatency: 5 * time.Millisecond,
		SlippagePct: 0.00005, // ~0.5 pips
		RejectRate:  0.02,    // 2% rejection rate
	}
}

// NewMockBroker creates a mock broker with the given configuration.
func NewMockBroker(cfg MockConfig) *MockBroker {
	return &MockBroker{
		fillLatency: cfg.FillLatency,
		slippagePct: cfg.SlippagePct,
		rejectRate:  cfg.RejectRate,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Execute simulates order execution with configurable latency and slippage.
func (mb *MockBroker) Execute(ctx context.Context, order model.OrderRequest) (model.Position, error) {
	// Simulate network/execution latency
	if mb.fillLatency > 0 {
		select {
		case <-time.After(mb.fillLatency):
		case <-ctx.Done():
			return model.Position{}, fmt.Errorf("mock broker: execution cancelled: %w", ctx.Err())
		}
	}

	// Simulate random rejection
	mb.mu.Lock()
	reject := mb.rng.Float64() < mb.rejectRate
	slippage := (mb.rng.Float64()*2 - 1) * mb.slippagePct * order.Price
	mb.mu.Unlock()

	if reject {
		return model.Position{}, fmt.Errorf("mock broker: order rejected (simulated)")
	}

	// Apply slippage to fill price
	fillPrice := order.Price + slippage

	// Generate unique order ID
	seq := mb.orderSeq.Add(1)
	orderID := fmt.Sprintf("MOCK-%d", seq)

	pos := model.Position{
		OrderID:    orderID,
		Symbol:     order.Symbol,
		Side:       order.Side,
		Lots:       order.Lots,
		EntryPrice: fillPrice,
		CurrentPnL: 0,
		OpenTimeNs: time.Now().UnixNano(),
	}

	log.Printf("[mock-broker] filled: %s %s %.2f lots @ %.5f (slip=%.6f)",
		order.Side, order.Symbol, order.Lots, fillPrice, slippage)

	return pos, nil
}

// Close simulates closing a position.
func (mb *MockBroker) Close(_ context.Context, positionID string) error {
	log.Printf("[mock-broker] closed position: %s", positionID)
	return nil
}

// ClosePartial simulates closing a portion of a position.
func (mb *MockBroker) ClosePartial(_ context.Context, positionID string, lots float64) error {
	log.Printf("[mock-broker] partially closed position: %s lots=%.2f", positionID, lots)
	return nil
}

// ModifyPosition simulates updating SL and TP on an active position.
func (mb *MockBroker) ModifyPosition(_ context.Context, positionID string, sl, tp float64) error {
	log.Printf("[mock-broker] modified position %s: SL=%.5f TP=%.5f", positionID, sl, tp)
	return nil
}
