package risk

import "github.com/pompbot/scalpbot/internal/model"

// MarketFilter is the interface for pre-trade market condition checks.
// Filters are evaluated before risk guards in the Evaluate() pipeline.
// Each filter returns whether trading is allowed and a rejection reason if not.
type MarketFilter interface {
	// Name returns a human-readable identifier for this filter.
	Name() string

	// Allow checks whether current market conditions permit trading.
	// Returns true if the filter passes, or false with a reason string.
	Allow(tick model.Tick) (bool, string)
}
