package backtest

import (
	"math"
	"math/rand"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// SyntheticConfig configures the synthetic tick generator.
type SyntheticConfig struct {
	StartPrice    float64
	Drift         float64 // Directional bias per tick (e.g. 0.000001)
	Volatility    float64 // Price standard deviation per tick (e.g. 0.00005)
	BaseSpreadPip float64 // Base spread in pips (e.g. 0.3)
	SpreadNoise   float64 // Spread fluctuation
	Interval      time.Duration
	Seed          int64
}

// DefaultSyntheticConfig returns realistic forex market defaults for EURUSD.
func DefaultSyntheticConfig() SyntheticConfig {
	return SyntheticConfig{
		StartPrice:    1.10000,
		Drift:         0.0000005,
		Volatility:    0.00006,
		BaseSpreadPip: 0.3,
		SpreadNoise:   0.1,
		Interval:      100 * time.Millisecond,
		Seed:          42,
	}
}

// GenerateSyntheticTicks creates a slice of realistic ticks using GBM with regime changes.
func GenerateSyntheticTicks(count int, symbol string, cfg SyntheticConfig) []model.Tick {
	r := rand.New(rand.NewSource(cfg.Seed))
	ticks := make([]model.Tick, count)

	currentPrice := cfg.StartPrice
	pipMult := model.PipMultiplier(symbol)
	now := time.Now().Add(-time.Duration(count) * cfg.Interval)

	currentDrift := cfg.Drift
	regimeCounter := 0
	regimeLength := 500 + r.Intn(1000)

	for i := 0; i < count; i++ {
		// Regime switching: switch between Bullish, Bearish, and Ranging
		regimeCounter++
		if regimeCounter >= regimeLength {
			regimeCounter = 0
			regimeLength = 500 + r.Intn(1500)
			mode := r.Intn(3)
			switch mode {
			case 0: // Bullish trend
				currentDrift = math.Abs(cfg.Drift) * (1.0 + r.Float64()*2.0)
			case 1: // Bearish trend
				currentDrift = -math.Abs(cfg.Drift) * (1.0 + r.Float64()*2.0)
			case 2: // Ranging / sideways
				currentDrift = 0.0
			}
		}

		// Geometric Brownian Motion step
		// z = standard normal random variable using Box-Muller transform
		u1 := r.Float64()
		if u1 < 1e-10 {
			u1 = 1e-10
		}
		u2 := r.Float64()
		z := math.Sqrt(-2.0*math.Log(u1)) * math.Cos(2.0*math.Pi*u2)

		delta := (currentDrift * currentPrice) + (cfg.Volatility * currentPrice * z)
		currentPrice += delta
		if currentPrice < 0.5 {
			currentPrice = 0.5
		}

		// Spread calculation with slight jitter
		spreadPips := cfg.BaseSpreadPip + (r.Float64()-0.5)*cfg.SpreadNoise
		if spreadPips < 0.1 {
			spreadPips = 0.1
		}
		halfSpread := (spreadPips / pipMult) / 2.0

		bid := currentPrice - halfSpread
		ask := currentPrice + halfSpread

		ts := now.Add(time.Duration(i) * cfg.Interval).UnixNano()

		ticks[i] = model.Tick{
			Symbol:      symbol,
			Bid:         bid,
			Ask:         ask,
			TimestampNs: ts,
		}
	}

	return ticks
}
