package ai

import (
	"math"
	"strings"
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

	// 1. If HMM has confirmed Bull or Bear expansion, HMM rules
	if f.lastHMMState == hmm.StateBull {
		return RegimeTrendingBullish, f.lastHMMConfidence
	} else if f.lastHMMState == hmm.StateBear {
		return RegimeTrendingBearish, f.lastHMMConfidence
	}

	// 2. In neutral/idle state, use the 12-feature RegimeClassifier for real-time tick-by-tick telemetry
	if f.regime != nil && f.extractor != nil && tick.MidPrice() > 0 {
		fv := f.extractor.Extract(tick, fastEMA, slowEMA, rsi, atr, tps, candle)
		return f.regime.Classify(fv)
	}

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
	} else if candle.Open > 0 && candle.Close > 0 {
		logReturn = math.Log(candle.Close / candle.Open)
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
	isRangeStrategy := strings.HasPrefix(sig.StrategyID, "range")
	if !f.cfg.DualModeEnabled && isRangeStrategy {
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

	// 2. Component A: Gaussian HMM Market Regime & Expansion Weather (Weight: 50%)
	var hmmScore float64 = 0.50
	if isRangeStrategy {
		switch state {
		case hmm.StateNoise:
			// In neutral / ranging chop, this is the IDEAL regime for mean-reversion!
			hmmScore = 0.50 + 0.35*hmmConf
		case hmm.StateBull:
			if sig.Type == model.Buy {
				hmmScore = 0.40 // buying near top in bull trend
			} else {
				hmmScore = 0.20 // shorting against strong bull trend
			}
		case hmm.StateBear:
			if sig.Type == model.Sell {
				hmmScore = 0.40 // selling near bottom in bear trend
			} else {
				hmmScore = 0.20 // buying against strong bear trend
			}
		}
	} else {
		// Momentum / Trend Scalper
		switch state {
		case hmm.StateBull:
			if sig.Type == model.Buy {
				hmmScore = 0.50 + 0.50*hmmConf
			} else {
				hmmScore = 0.50 - 0.50*hmmConf
			}
		case hmm.StateBear:
			if sig.Type == model.Sell {
				hmmScore = 0.50 + 0.50*hmmConf
			} else {
				hmmScore = 0.50 - 0.50*hmmConf
			}
		default: // hmm.StateNoise / Ranging Chop
			if f.cfg.FilterRangingChop {
				// In neutral weather, penalize HMM score to 0.40.
				// High-conviction GBDT triggers (score >= 0.70) can still achieve composite >= 0.55 and pass,
				// while weak/choppy triggers are filtered naturally by the composite decision gate.
				hmmScore = 0.40
			} else {
				hmmScore = 0.50
			}
		}
	}

	// 3. Component B: GBDT Microstructure & Candlestick Scorer (Weight: 50%)
	var gbdtScore float64 = 0.50
	if f.scorer != nil {
		if isRangeStrategy {
			gbdtScore = f.scorer.PredictRangeConfidence(sig.Type, fv)
		} else {
			gbdtScore = f.scorer.PredictConfidence(sig.Type, fv)
		}
	}

	// 4. Calculate Unified Composite Confidence: 50% HMM Market Weather + 50% GBDT Trigger Quality
	compositeConf := (0.50 * hmmScore) + (0.50 * gbdtScore)
	f.lastConfidence = compositeConf

	// 5. Single Master Decision Gate
	minConf := f.cfg.MinConfidence
	if isRangeStrategy && f.cfg.RangeMinConfidence > 0 {
		minConf = f.cfg.RangeMinConfidence
	}
	if compositeConf < minConf {
		return false, compositeConf, regime, "AI Filter REJECT: composite confidence below threshold"
	}

	return true, compositeConf, regime, "AI Filter ALLOW: composite confidence passed"
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
