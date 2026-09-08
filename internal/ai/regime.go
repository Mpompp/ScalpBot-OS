package ai

import (
	"math"
)

// MarketRegime represents the macro/micro state of the market.
type MarketRegime int8

const (
	// RegimeTrendingBullish indicates strong upward directional momentum.
	RegimeTrendingBullish MarketRegime = iota
	// RegimeTrendingBearish indicates strong downward directional momentum.
	RegimeTrendingBearish
	// RegimeRangingChop indicates directionless, low-edge, whipsaw market conditions.
	RegimeRangingChop
	// RegimeHighVolatilityEvent indicates abnormal risk (news release, flash crash, wide spread).
	RegimeHighVolatilityEvent
	// RegimeWallExhaustion indicates extreme momentum exhaustion or price hitting a major round wall.
	RegimeWallExhaustion
)

// String returns human-readable name of the market regime.
func (r MarketRegime) String() string {
	switch r {
	case RegimeTrendingBullish:
		return "TRENDING_BULLISH"
	case RegimeTrendingBearish:
		return "TRENDING_BEARISH"
	case RegimeRangingChop:
		return "RANGING_CHOP"
	case RegimeHighVolatilityEvent:
		return "HIGH_VOLATILITY_EVENT"
	case RegimeWallExhaustion:
		return "WALL_EXHAUSTION"
	default:
		return "UNKNOWN"
	}
}

// RegimeClassifier classifies market conditions using a multi-feature decision rule matrix.
type RegimeClassifier struct {
	chopSpreadThreshold float64 // Max allowed EMA delta to consider market as ranging (pips)
	chopRSIBand         float64 // RSI distance from 50 (e.g. 8.0 = 42 to 58 is chop)
	volRatioThreshold   float64 // Volatility explosion threshold (e.g. 2.5)
}

// NewRegimeClassifier creates a new regime classifier.
func NewRegimeClassifier() *RegimeClassifier {
	return &RegimeClassifier{
		chopSpreadThreshold: 0.20, // Less than 0.20 ATR divergence indicates chop
		chopRSIBand:         0.16, // RSI normalized between -0.16 and +0.16 (RSI 42-58)
		volRatioThreshold:   2.4,  // Volatility ratio > 2.4 indicates surge
	}
}

// Classify determines the current MarketRegime from the FeatureVector.
// Guaranteed 0 heap allocations, executes in < 20 nanoseconds.
func (rc *RegimeClassifier) Classify(fv FeatureVector) (MarketRegime, float64) {
	emaDelta := fv[FeatEMADelta]
	rsiNorm := fv[FeatRSINorm]
	volRatio := fv[FeatVolRatio]
	spreadRatio := fv[FeatSpreadRatio]
	tps := fv[FeatTickVelocity] * 10.0

	// 1. Check High Volatility / Abnormal Event
	if volRatio >= rc.volRatioThreshold || spreadRatio >= 1.2 || tps > 50.0 {
		conf := math.Min(1.0, volRatio/3.0)
		return RegimeHighVolatilityEvent, conf
	}

	// 2. Check Wall / Momentum Exhaustion (RSI Extreme > 74 or < 26 with high volatility ratio)
	if (rsiNorm > 0.24 || rsiNorm < -0.24) && volRatio > 1.3 {
		return RegimeWallExhaustion, 0.0
	}

	// 3. Check Ranging / Chop (Indecision / Flat EMAs / Neutral RSI)
	absEMADelta := math.Abs(emaDelta)
	absRSINorm := math.Abs(rsiNorm)

	if absEMADelta < rc.chopSpreadThreshold && absRSINorm < rc.chopRSIBand {
		chopScore := (1.0 - (absEMADelta / rc.chopSpreadThreshold)) * 0.5 +
			(1.0 - (absRSINorm / rc.chopRSIBand)) * 0.5
		return RegimeRangingChop, math.Min(1.0, math.Max(0.5, chopScore))
	}

	// 4. Trending Bullish (Requires genuine directional divergence > 0.20 ATR)
	if emaDelta >= rc.chopSpreadThreshold && rsiNorm > rc.chopRSIBand {
		conf := math.Min(1.0, (emaDelta/1.5)*0.5+(rsiNorm)*0.5)
		if conf < 0.55 {
			conf = 0.55
		}
		return RegimeTrendingBullish, conf
	}

	// 5. Trending Bearish (Requires genuine directional divergence < -0.20 ATR)
	if emaDelta <= -rc.chopSpreadThreshold && rsiNorm < -rc.chopRSIBand {
		conf := math.Min(1.0, (math.Abs(emaDelta)/1.5)*0.5+(math.Abs(rsiNorm))*0.5)
		if conf < 0.55 {
			conf = 0.55
		}
		return RegimeTrendingBearish, conf
	}

	// Mixed signal -> default to Ranging Chop for safety
	return RegimeRangingChop, 0.60
}
