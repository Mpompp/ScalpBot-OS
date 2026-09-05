package broker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// ConnectionState represents the broker connection status.
type ConnectionState int32

const (
	// StateDisconnected indicates no active connection.
	StateDisconnected ConnectionState = iota
	// StateConnecting indicates a connection attempt in progress.
	StateConnecting
	// StateConnected indicates an active, healthy connection.
	StateConnected
	// StateReconnecting indicates automatic reconnection in progress.
	StateReconnecting
)

// String returns the human-readable connection state.
func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "DISCONNECTED"
	case StateConnecting:
		return "CONNECTING"
	case StateConnected:
		return "CONNECTED"
	case StateReconnecting:
		return "RECONNECTING"
	default:
		return "UNKNOWN"
	}
}

// DisconnectCallback is called when a connection loss is detected.
type DisconnectCallback func(err error)

// LifecycleManager wraps a Broker with connection health monitoring,
// stale data detection, and position reconciliation on startup.
type LifecycleManager struct {
	broker          Broker
	state           atomic.Int32 // ConnectionState stored atomically
	lastTickTime    atomic.Int64 // Last tick timestamp (nanos) for stale detection
	staleThreshold  time.Duration
	onDisconnect    DisconnectCallback
	reconnectPolicy ReconnectPolicy
	mu              sync.RWMutex
}

// LifecycleConfig configures the lifecycle manager.
type LifecycleConfig struct {
	StaleThreshold  time.Duration    // Max gap before data is considered stale
	ReconnectPolicy ReconnectPolicy  // Reconnect behavior
	OnDisconnect    DisconnectCallback
}

// DefaultLifecycleConfig returns sensible defaults.
func DefaultLifecycleConfig() LifecycleConfig {
	return LifecycleConfig{
		StaleThreshold:  10 * time.Second,
		ReconnectPolicy: DefaultReconnectPolicy(),
	}
}

// NewLifecycleManager wraps a broker with connection lifecycle management.
func NewLifecycleManager(broker Broker, cfg LifecycleConfig) *LifecycleManager {
	lm := &LifecycleManager{
		broker:          broker,
		staleThreshold:  cfg.StaleThreshold,
		onDisconnect:    cfg.OnDisconnect,
		reconnectPolicy: cfg.ReconnectPolicy,
	}
	lm.state.Store(int32(StateConnected))
	return lm
}

// Execute delegates to the underlying broker, respecting connection state.
func (lm *LifecycleManager) Execute(ctx context.Context, order model.OrderRequest) (model.Position, error) {
	state := ConnectionState(lm.state.Load())
	if state != StateConnected {
		return model.Position{}, fmt.Errorf("broker not connected (state=%s)", state)
	}
	return lm.broker.Execute(ctx, order)
}

// Close delegates to the underlying broker.
func (lm *LifecycleManager) Close(ctx context.Context, positionID string) error {
	return lm.broker.Close(ctx, positionID)
}

// RecordTick updates the last tick timestamp for stale data detection.
// Call this from the tick processing pipeline on every tick.
func (lm *LifecycleManager) RecordTick(timestampNs int64) {
	lm.lastTickTime.Store(timestampNs)
}

// State returns the current connection state.
func (lm *LifecycleManager) State() ConnectionState {
	return ConnectionState(lm.state.Load())
}

// Heartbeat starts a background goroutine that monitors data freshness.
// If no tick is received within staleThreshold, it triggers disconnect handling.
// Runs until ctx is cancelled.
func (lm *LifecycleManager) Heartbeat(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				lastNs := lm.lastTickTime.Load()
				if lastNs == 0 {
					continue // No ticks received yet
				}

				elapsed := time.Since(time.Unix(0, lastNs))
				if elapsed > lm.staleThreshold {
					log.Printf("[lifecycle] stale data detected: no tick for %v (threshold=%v)",
						elapsed.Round(time.Millisecond), lm.staleThreshold)

					lm.state.Store(int32(StateReconnecting))
					if lm.onDisconnect != nil {
						lm.onDisconnect(fmt.Errorf("stale data: %v since last tick", elapsed))
					}
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}

// ReconcilePositions queries the broker for open positions on startup.
// This allows the bot to resume tracking positions after a restart.
// Returns nil slice and nil error if broker doesn't support reconciliation.
func (lm *LifecycleManager) ReconcilePositions(_ context.Context) ([]model.Position, error) {
	// Base Broker interface doesn't support position listing.
	// Real broker adapters can implement a ReconcilableBroker interface.
	type reconcilable interface {
		ListOpenPositions(ctx context.Context) ([]model.Position, error)
	}

	if rb, ok := lm.broker.(reconcilable); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return rb.ListOpenPositions(ctx)
	}

	log.Println("[lifecycle] broker does not support position reconciliation")
	return nil, nil
}

// ReconnectPolicy defines retry behavior for connection recovery.
type ReconnectPolicy struct {
	MaxRetries        int           // Maximum reconnect attempts (0 = unlimited)
	BaseDelay         time.Duration // Initial delay between retries
	MaxDelay          time.Duration // Maximum delay (cap for exponential backoff)
	BackoffMultiplier float64       // Delay multiplier per retry (e.g. 2.0)
}

// DefaultReconnectPolicy returns conservative reconnect defaults.
func DefaultReconnectPolicy() ReconnectPolicy {
	return ReconnectPolicy{
		MaxRetries:        10,
		BaseDelay:         1 * time.Second,
		MaxDelay:          30 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

// ExecuteReconnect runs a reconnection loop with exponential backoff.
// connectFn should attempt to establish the connection and return nil on success.
func (p ReconnectPolicy) ExecuteReconnect(ctx context.Context, connectFn func() error) error {
	delay := p.BaseDelay

	for attempt := 1; p.MaxRetries == 0 || attempt <= p.MaxRetries; attempt++ {
		log.Printf("[reconnect] attempt %d (delay=%v)", attempt, delay)

		if err := connectFn(); err == nil {
			log.Printf("[reconnect] connected on attempt %d", attempt)
			return nil
		}

		// Wait with context awareness
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return fmt.Errorf("reconnect cancelled: %w", ctx.Err())
		}

		// Exponential backoff
		delay = time.Duration(float64(delay) * p.BackoffMultiplier)
		if delay > p.MaxDelay {
			delay = p.MaxDelay
		}
	}

	return errors.New("reconnect: max retries exhausted")
}
