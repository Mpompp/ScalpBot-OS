package portfolio

import (
	"testing"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestCorrelationGuard_AllowDifferentUncorrelated(t *testing.T) {
	cg := NewCorrelationGuard(1)

	// Active position: BUY EURUSD
	active := []model.Position{
		{
			Symbol: "EURUSD",
			Side:   model.SideBuy,
			Lots:   0.10,
		},
	}

	// 1. BUY USDJPY -> Uncorrelated (corr = -0.40) -> Allowed
	allowed, reason := cg.CheckCorrelation("USDJPY", model.SideBuy, active)
	if !allowed {
		t.Fatalf("expected USDJPY Buy allowed with EURUSD Buy, rejected: %s", reason)
	}

	// 2. BUY GBPUSD -> Strongly positively correlated (corr = 0.82) -> REJECTED
	allowed, reason = cg.CheckCorrelation("GBPUSD", model.SideBuy, active)
	if allowed {
		t.Fatalf("expected GBPUSD Buy REJECTED due to correlation with EURUSD Buy, got allowed")
	}

	// 3. SELL USDCHF -> Strongly negatively correlated (corr = -0.88) -> REJECTED (same USD short exposure)
	allowed, reason = cg.CheckCorrelation("USDCHF", model.SideSell, active)
	if allowed {
		t.Fatalf("expected USDCHF Sell REJECTED due to inverse correlation with EURUSD Buy, got allowed")
	}
}

func TestVolatilityParitySizer_EqualRisk(t *testing.T) {
	cfg := DefaultSizerConfig()
	cfg.RiskPerTrade = 0.01 // 1% risk = $100 on $10,000 equity
	sizer := NewVolatilityParitySizer(cfg)

	equity := 10000.0

	// 1. EURUSD: ATR = 0.0004 (4 pips) -> SL = 6 pips ($60 per lot) -> Lots ~ 1.66
	lotsEUR := sizer.CalculateLotSize("EURUSD", equity, 0.0004)
	if lotsEUR < 0.10 || lotsEUR > 2.00 {
		t.Errorf("expected reasonable EURUSD lot size, got %.2f", lotsEUR)
	}

	// 2. XAUUSD (Gold): ATR = 3.00 ($3.00 gold move = 300 pips) -> SL = 450 pips -> Lots must be small (~0.02)
	lotsGold := sizer.CalculateLotSize("XAUUSD", equity, 3.00)
	if lotsGold >= lotsEUR {
		t.Errorf("expected Gold lots (%.2f) to be significantly smaller than EURUSD lots (%.2f)", lotsGold, lotsEUR)
	}
	if lotsGold < 0.01 {
		t.Errorf("expected Gold lots >= 0.01, got %.2f", lotsGold)
	}
}

func BenchmarkCorrelationGuard(b *testing.B) {
	cg := NewCorrelationGuard(1)
	active := []model.Position{
		{Symbol: "EURUSD", Side: model.SideBuy, Lots: 0.10},
		{Symbol: "USDJPY", Side: model.SideSell, Lots: 0.10},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = cg.CheckCorrelation("GBPUSD", model.SideBuy, active)
	}
}

func BenchmarkVolatilityParitySizer(b *testing.B) {
	sizer := NewVolatilityParitySizer(DefaultSizerConfig())

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = sizer.CalculateLotSize("EURUSD", 10000.0, 0.0004)
	}
}
