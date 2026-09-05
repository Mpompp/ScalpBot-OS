// Package strategy defines the strategy interface and concrete implementations.
// Strategies produce Signals only — they are strictly forbidden from executing
// orders directly. All signals must pass through the Risk Engine.
package strategy

import (
	"github.com/pompbot/scalpbot/internal/model"
)

// Strategy is the interface that all trading strategies must implement.
// Implementations must be stateless with respect to order execution —
// they observe market data and emit signals, nothing more.
type Strategy interface {
	// ID returns a unique identifier for this strategy instance.
	ID() string

	// OnTick processes a market tick and optionally emits a signal.
	// Called on every tick — must be extremely fast (sub-microsecond target).
	// Return model.Signal with Type == NoSignal if no action is warranted.
	OnTick(tick model.Tick) model.Signal

	// OnCandle processes a completed candle and optionally emits a signal.
	// Called when the OHLCV aggregator closes a period.
	OnCandle(candle model.Candle) model.Signal
}

// NewStrategy returns the default Momentum Scalper engine (primary strategy).
func NewStrategy(symbol string) Strategy {
	cfg := DefaultMomentumConfig()
	cfg.Symbol = symbol
	return NewMomentumScalper("momentum-"+symbol, cfg)
}
