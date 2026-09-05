package ai

import (
	"fmt"
	"math"
	"sync"

	"github.com/pompbot/scalpbot/internal/ai/hmm"
	"github.com/pompbot/scalpbot/internal/model"
)

// FilterConfig configures the AI signal filter and Gaussian HMM regime guard.
type FilterConfig struct {
	EnableMLFilter     bool    // Master switch for AI probability scoring
	MinConfidence      float64 // Minimum required confidence for HMM expansion (e.g. 0.70 = 70%)
	RangeMinConfidence float64 // Minimum required confidence for Range setups (e.g. 0.60 = 60%)
	FilterRangingChop  bool    // Block trend breakout trades during ranging chop
	DualModeEnabled    bool    // Enable Dual-Mode AI (Trend + Range)
}

// DefaultFilterConfig returns standard institutional defaults.
func DefaultFilterConfig() FilterConfig {
	return FilterConfig{
		EnableMLFilter:     true,
		MinConfidence:      0.55, // Calibrated composite confidence threshold (55% Balanced mode)
		RangeMinConfidence: 0.50, // 50% minimum confidence for range
		FilterRangingChop:  true,
		DualModeEnabled:    false, // DISABLED: Gold swing momentum mode — trend signals only
	}
}

// SignalFilter is an institutional AI filter that verifies market regime and model confidence.
// Thread-safe and zero-allocation on the hot-path.
type SignalFilter struct {
	cfg               FilterConfig
	extractor         *FeatureExtractor
	regime            *RegimeClassifier
	scorer            *SignalScorer
	hmmEngine         *hmm.GaussianHMM
	lastRegime        MarketRegime
	lastConfidence    float64
	lastHMMState      hmm.MarketState
	lastHMMConfidence float64
	prevCandleClose   float64
	mu                sync.RWMutex
}

// NewSignalFilter creates a new AI signal filter with embedded Gaussian HMM.
func NewSignalFilter(cfg FilterConfig) *SignalFilter {
	return &SignalFilter{
		cfg:               cfg,
		extractor:         NewFeatureExtractor(),
		regime:            NewRegimeClassifier(),
		scorer:            NewSignalScorer(),
		hmmEngine:         hmm.NewGaussianHMM(),
		lastRegime:        RegimeRangingChop,
		lastConfidence:    0.0,
		lastHMMState:      hmm.StateNoise,
		lastHMMConfidence: 0.70,
	}
}

// OnTick updates the feature extractor history with incoming tick price.
func (f *SignalFilter) OnTick(tick model.Tick) {
	f.mu.Lock()
	f.extractor.Update(tick.MidPrice())
	f.mu.Unlock()
}

// LastRegime returns the most recently detected market regime.
func (f *SignalFilter) LastRegime() MarketRegime {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastRegime
}

// LastConfidence returns the most recent AI confidence score.
func (f *SignalFilter) LastConfidence() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastConfidence
}

// LastHMMState returns the most recent HMM market state and confidence.
func (f *SignalFilter) LastHMMState() (hmm.MarketState, float64) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastHMMState, f.lastHMMConfidence
}

// EvaluateCurrent inspects the current market state and returns the latest regime and confidence for telemetry.
func (f *SignalFilter) EvaluateCurrent(tick model.Tick, fastEMA, slowEMA, rsi, atr, tps float64, candle model.Candle) (MarketRegime, float64) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastRegime, f.lastConfidence
}

// MinConfidence returns the active required minimum confidence threshold.
func (f *SignalFilter) MinConfidence() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cfg.MinConfidence
}

// RangeMinConfidence returns the active required range minimum confidence threshold.
func (f *SignalFilter) RangeMinConfidence() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cfg.RangeMinConfidence
}

// UpdateHMM feeds an aggregated M1 candle into the Gaussian HMM engine.
func (f *SignalFilter) UpdateHMM(candle model.Candle, atr float64) (hmm.MarketState, float64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	logReturn := 0.0
	if f.prevCandleClose > 0 && candle.Close > 0 {
		logReturn = math.Log(candle.Close / f.prevCandleClose)
	}
	f.prevCandleClose = candle.Close

	relATR := 0.0004
	if candle.Close > 0 && atr > 0 {
		relATR = atr / candle.Close
	}

	volImbalance := 0.0
	totalVol := candle.BuyerVol + candle.SellerVol
	if totalVol > 1e-9 {
		volImbalance = (candle.BuyerVol - candle.SellerVol) / (totalVol + 1e-9)
	}
	// Strict clamping to [-1, +1]
	if volImbalance > 1.0 {
		volImbalance = 1.0
	} else if volImbalance < -1.0 {
		volImbalance = -1.0
	}

	obs := hmm.ObservationVector{logReturn, relATR, volImbalance}
	state, conf := f.hmmEngine.Update(obs)

	f.lastHMMState = state
	f.lastHMMConfidence = conf
	f.lastConfidence = conf

	switch state {
	case hmm.StateBull:
		f.lastRegime = RegimeTrendingBullish
	case hmm.StateBear:
		f.lastRegime = RegimeTrendingBearish
	default:
		f.lastRegime = RegimeRangingChop
	}

	return state, conf
}

// EvaluateSignal inspects an actionable strategy signal using the Gaussian HMM and Exhaustion Gates.
// EvaluateSignal inspects an actionable strategy signal using the Unified Composite Ensemble
// combining Gaussian HMM Market Regime, GBDT Microstructure Scorer, and Candlestick Health.
// Returns: (allowed bool, confidence float64, regime MarketRegime, reason string)
func (f *SignalFilter) EvaluateSignal(
	sig model.Signal,
	tick model.Tick,
	fastEMA, slowEMA, rsi, atr, tps float64,
	candle model.Candle,
) (bool, float64, MarketRegime, string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// 0. Safety Guard: Range signals disabled when DualMode is off
	if !f.cfg.DualModeEnabled && sig.StrategyID == "range_scalper" {
		return false, 0.0, f.lastRegime, "AI Filter 🛑 REJECT: Range signals disabled in Trend Momentum Mode"
	}

	if !f.cfg.EnableMLFilter {
		return true, 0.60, f.lastRegime, "AI Filter bypassed (disabled in config)"
	}

	// 1. Extract 12-feature vector for microstructure & candle analysis
	fv := f.extractor.Extract(tick, fastEMA, slowEMA, rsi, atr, tps, candle)

	state := f.lastHMMState
	hmmConf := f.lastHMMConfidence
	regime := f.lastRegime

	// 2. Component A: Gaussian HMM Regime Score (Weight: 45%)
	// Severe Counter-Trend Guard: Reject if HMM is firmly against the trade direction
	if sig.Type == model.Buy && state == hmm.StateBear && hmmConf >= 0.65 {
		return false, hmmConf, regime, fmt.Sprintf(
			"AI Filter 🛑 REJECT: BUY blocked by strong Bearish HMM regime (state=%s, conf=%.1f%%)",
			state.String(), hmmConf*100.0,
		)
	}
	if sig.Type == model.Sell && state == hmm.StateBull && hmmConf >= 0.65 {
		return false, hmmConf, regime, fmt.Sprintf(
			"AI Filter 🛑 REJECT: SELL blocked by strong Bullish HMM regime (state=%s, conf=%.1f%%)",
			state.String(), hmmConf*100.0,
		)
	}

	var hmmScore float64
	switch state {
	case hmm.StateBull:
		if sig.Type == model.Buy {
			hmmScore = hmmConf
		} else {
			hmmScore = 1.0 - hmmConf
		}
	case hmm.StateBear:
		if sig.Type == model.Sell {
			hmmScore = hmmConf
		} else {
			hmmScore = 1.0 - hmmConf
		}
	default: // hmm.StateNoise / Transition
		// Neutral baseline for transitioning/consolidating market
		hmmScore = 0.50
	}

	// 3. Component B: GBDT Microstructure Scorer (Weight: 35%)
	var gbdtScore float64 = 0.50
	if f.scorer != nil {
		gbdtScore = f.scorer.PredictConfidence(sig.Type, fv)
	}

	// 4. Component C: Candlestick Health & Rejection Wick Quality (Weight: 20%)
	upperWick := fv[FeatUpperWickRatio]
	lowerWick := fv[FeatLowerWickRatio]
	rsiNorm := fv[FeatRSINorm] // [-1.0, +1.0], 0 = RSI 50, +0.36 = RSI 68, -0.36 = RSI 32

	var candleScore float64 = 0.60 // baseline healthy candle
	if sig.Type == model.Buy {
		if upperWick <= 0.25 {
			candleScore += 0.25 // Clean breakout body
		} else if upperWick > 0.45 {
			candleScore -= 0.35 // Notable upper resistance wick
		}
		// RSI ceiling check (RSI > 72.5)
		if rsiNorm > 0.45 {
			candleScore -= 0.25
		} else if rsiNorm > 0.10 && rsiNorm < 0.35 {
			candleScore += 0.15 // Prime momentum sweet spot (RSI 55-67)
		}
	} else if sig.Type == model.Sell {
		if lowerWick <= 0.25 {
			candleScore += 0.25 // Clean breakdown body
		} else if lowerWick > 0.45 {
			candleScore -= 0.35 // Notable lower support wick
		}
		// RSI floor check (RSI < 27.5)
		if rsiNorm < -0.45 {
			candleScore -= 0.25
		} else if rsiNorm < -0.10 && rsiNorm > -0.35 {
			candleScore += 0.15 // Prime momentum sweet spot (RSI 33-45)
		}
	}
	// Clamp candleScore to [0.0, 1.0]
	if candleScore < 0.0 {
		candleScore = 0.0
	} else if candleScore > 1.0 {
		candleScore = 1.0
	}

	// 5. Calculate Unified Composite Confidence
	// 45% HMM Regime + 35% GBDT Microstructure + 20% Candlestick Health
	compositeConf := (0.45 * hmmScore) + (0.35 * gbdtScore) + (0.20 * candleScore)
	f.lastConfidence = compositeConf

	// 6. Single Master Decision Gate
	if compositeConf < f.cfg.MinConfidence {
		reason := fmt.Sprintf(
			"AI Filter 🛑 REJECT: %s composite confidence %.1f%% < min %.1f%% (HMM: %.0f%%, Micro: %.0f%%, Candle: %.0f%%)",
			sig.Type.String(), compositeConf*100.0, f.cfg.MinConfidence*100.0,
			hmmScore*100.0, gbdtScore*100.0, candleScore*100.0,
		)
		return false, compositeConf, regime, reason
	}

	reason := fmt.Sprintf(
		"Composite Conf: %.0f%% (HMM: %.0f%%, Micro: %.0f%%, Candle: %.0f%%)",
		compositeConf*100.0, hmmScore*100.0, gbdtScore*100.0, candleScore*100.0,
	)
	return true, compositeConf, regime, reason
}

// Name returns the identifier for this filter.
func (f *SignalFilter) Name() string {
	return "GaussianHMMSignalFilter"
}

// UpdateConfig dynamically updates filter configuration at runtime.
func (f *SignalFilter) UpdateConfig(cfg FilterConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfg = cfg
}

// GetConfig returns the current filter configuration.
func (f *SignalFilter) GetConfig() FilterConfig {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cfg
}
