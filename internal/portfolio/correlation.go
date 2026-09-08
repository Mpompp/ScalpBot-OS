package portfolio

import (
	"sync"

	"github.com/pompbot/scalpbot/internal/model"
)

// CorrelationThreshold is the minimum correlation coefficient to consider pairs correlated.
const CorrelationThreshold = 0.70

// CorrelationGuard prevents portfolio concentration in correlated currency risk.
// E.g., buying EURUSD and buying GBPUSD simultaneously doubles USD short risk.
type CorrelationGuard struct {
	matrix             map[string]map[string]float64
	maxCorrelatedCount int
	mu                 sync.RWMutex
}

// NewCorrelationGuard creates a correlation guard with standard institutional FX matrix.
func NewCorrelationGuard(maxCorrelated int) *CorrelationGuard {
	if maxCorrelated <= 0 {
		maxCorrelated = 1
	}

	cg := &CorrelationGuard{
		matrix:             make(map[string]map[string]float64),
		maxCorrelatedCount: maxCorrelated,
	}

	// Initialize standard institutional correlation matrix
	cg.setPairCorr("EURUSD", "GBPUSD", 0.82)
	cg.setPairCorr("EURUSD", "USDCHF", -0.88)
	cg.setPairCorr("EURUSD", "XAUUSD", 0.65)
	cg.setPairCorr("EURUSD", "USDJPY", -0.40)
	cg.setPairCorr("GBPUSD", "USDJPY", -0.45)
	cg.setPairCorr("GBPUSD", "XAUUSD", 0.60)
	cg.setPairCorr("USDJPY", "XAUUSD", -0.55)
	cg.setPairCorr("AUDUSD", "NZDUSD", 0.88)
	cg.setPairCorr("EURUSD", "AUDUSD", 0.72)

	return cg
}

func (cg *CorrelationGuard) setPairCorr(sym1, sym2 string, corr float64) {
	s1 := normalizeSymbol(sym1)
	s2 := normalizeSymbol(sym2)

	if cg.matrix[s1] == nil {
		cg.matrix[s1] = make(map[string]float64)
	}
	if cg.matrix[s2] == nil {
		cg.matrix[s2] = make(map[string]float64)
	}

	cg.matrix[s1][s2] = corr
	cg.matrix[s2][s1] = corr
}

// GetCorrelation returns the correlation coefficient between two symbols [-1.0, +1.0].
func (cg *CorrelationGuard) GetCorrelation(sym1, sym2 string) float64 {
	s1 := normalizeSymbol(sym1)
	s2 := normalizeSymbol(sym2)
	if s1 == s2 {
		return 1.0
	}

	cg.mu.RLock()
	defer cg.mu.RUnlock()

	if m, ok := cg.matrix[s1]; ok {
		if c, ok2 := m[s2]; ok2 {
			return c
		}
	}
	return 0.0
}

// CheckCorrelation evaluates if adding a new trade violates portfolio correlation limits.
func (cg *CorrelationGuard) CheckCorrelation(
	newSymbol string,
	newSide model.OrderSide,
	activePositions []model.Position,
) (bool, string) {
	if len(activePositions) == 0 {
		return true, ""
	}

	s1 := normalizeSymbol(newSymbol)

	cg.mu.RLock()
	defer cg.mu.RUnlock()

	correlatedCount := 0

	for i := 0; i < len(activePositions); i++ {
		pos := &activePositions[i]
		s2 := normalizeSymbol(pos.Symbol)
		if s1 == s2 {
			continue // Handled by standard max open positions per symbol
		}

		corr := 0.0
		if m, ok := cg.matrix[s1]; ok {
			corr = m[s2]
		}

		// Positive correlation check: Same side on positively correlated pairs
		if corr >= CorrelationThreshold && pos.Side == newSide {
			correlatedCount++
			if correlatedCount >= cg.maxCorrelatedCount {
				return false, "portfolio correlation limit exceeded"
			}
		}

		// Negative correlation check: Opposite side on negatively correlated pairs
		if corr <= -CorrelationThreshold && pos.Side != newSide {
			correlatedCount++
			if correlatedCount >= cg.maxCorrelatedCount {
				return false, "portfolio inverse correlation limit exceeded"
			}
		}
	}

	return true, ""
}

func normalizeSymbol(sym string) string {
	return model.NormalizeSymbol(sym)
}
