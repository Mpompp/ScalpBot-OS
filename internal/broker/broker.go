// Package broker defines the interface for order execution adapters
// and provides a mock implementation for testing.
package broker

import (
	"context"

	"github.com/pompbot/scalpbot/internal/model"
)

// Broker is the interface that all broker adapters must implement.
// Real adapters (MT5, cTrader, FIX, etc.) implement this interface
// to bridge the bot to a live trading platform.
type Broker interface {
	// Execute submits an order and returns the resulting position.
	// Returns an error if the order is rejected by the broker.
	// Implementations must respect context cancellation for timeout/shutdown.
	Execute(ctx context.Context, order model.OrderRequest) (model.Position, error)

	// Close closes an open position by its broker-assigned order ID.
	// Returns an error if the position cannot be closed.
	Close(ctx context.Context, positionID string) error
}
