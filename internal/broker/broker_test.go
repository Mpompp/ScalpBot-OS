package broker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestMockBroker_Execution(t *testing.T) {
	mb := NewMockBroker(MockConfig{
		FillLatency: 1 * time.Millisecond,
		SlippagePct: 0.0001,
		RejectRate:  0.0, // Guaranteed fill
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	order := model.OrderRequest{
		Symbol: "EURUSD",
		Side:   model.SideBuy,
		Lots:   0.1,
		Price:  1.08500,
	}

	pos, err := mb.Execute(ctx, order)
	if err != nil {
		t.Fatalf("expected order to fill, got error: %v", err)
	}

	if pos.OrderID == "" {
		t.Error("expected non-empty OrderID")
	}
	if pos.Lots != 0.1 {
		t.Errorf("expected 0.1 lots, got %f", pos.Lots)
	}
	if pos.EntryPrice == 0 {
		t.Error("expected valid entry price")
	}
}

func TestMockBroker_Close(t *testing.T) {
	mb := NewMockBroker(DefaultMockConfig())
	ctx := context.Background()

	if err := mb.Close(ctx, "MOCK-1"); err != nil {
		t.Fatalf("failed to close position: %v", err)
	}
}

func TestLifecycleManager_StateAndHeartbeat(t *testing.T) {
	mb := NewMockBroker(MockConfig{RejectRate: 0.0})
	var disconnected atomic.Bool

	lm := NewLifecycleManager(mb, LifecycleConfig{
		StaleThreshold: 20 * time.Millisecond,
		OnDisconnect: func(err error) {
			disconnected.Store(true)
		},
	})

	if lm.State() != StateConnected {
		t.Fatalf("expected StateConnected, got %v", lm.State())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lm.Heartbeat(ctx, 5*time.Millisecond)

	// Simulate receiving a tick
	lm.RecordTick(time.Now().UnixNano())

	// Wait past the stale threshold without sending ticks
	time.Sleep(50 * time.Millisecond)

	if !disconnected.Load() {
		t.Error("expected disconnect callback to trigger due to stale data")
	}
	if lm.State() != StateReconnecting {
		t.Errorf("expected StateReconnecting, got %v", lm.State())
	}
}

func TestReconnectPolicy_Backoff(t *testing.T) {
	policy := ReconnectPolicy{
		MaxRetries:        3,
		BaseDelay:         5 * time.Millisecond,
		MaxDelay:          20 * time.Millisecond,
		BackoffMultiplier: 2.0,
	}

	attempts := 0
	err := policy.ExecuteReconnect(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary connection error")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected reconnect to succeed on 3rd attempt, got: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}
