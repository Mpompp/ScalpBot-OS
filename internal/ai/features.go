package ai

import (
	"math"

	"github.com/pompbot/scalpbot/internal/model"
)

// Feature indices within the 12-element FeatureVector.
const (
	FeatEMADelta               = 0  // (FastEMA - SlowEMA) / Price * 10000
	FeatRSINorm                = 1  // (RSI - 50) / 50 -> [-1.0, +1.0]
	FeatATRNorm                = 2  // ATR / Price * 10000
	FeatSpreadRatio            = 3  // Spread / (ATR + eps)
	FeatTickVelocity           = 4  // TPS / 10.0
	FeatPriceMomShort          = 5  // Price change over last 5 ticks (pips)
	FeatPriceMomMedium         = 6  // Price change over last 20 ticks (pips)
	FeatVolRatio               = 7  // Short-term variance / Long-term variance
	FeatBodyRatio              = 8  // Candle Body / (High - Low + eps)
	FeatUpperWickRatio         = 9  // Candle Upper Wick / Range
	FeatLowerWickRatio         = 10 // Candle Lower Wick / Range
	FeatDirectionalConsistency = 11 // Positive tick ratio in window [-1.0, +1.0]
	NumFeatures                = 12
)

// FeatureVector is a fixed-size array holding all 12 quantitative features.
// Using a fixed-size value array guarantees 0 heap allocation on the hot-path.
type FeatureVector [NumFeatures]float64

// FeatureExtractor maintains sliding window price history to construct feature vectors.
type FeatureExtractor struct {
	historyPrice [32]float64
	historyHead  int
	historyCount int
}

// NewFeatureExtractor creates a zero-allocation feature extractor.
func NewFeatureExtractor() *FeatureExtractor {
	return &FeatureExtractor{}
}

// Update records a tick's mid price into the circular buffer.
func (fe *FeatureExtractor) Update(price float64) {
	fe.historyPrice[fe.historyHead] = price
	fe.historyHead = (fe.historyHead + 1) % 32
	if fe.historyCount < 32 {
		fe.historyCount++
	}
}

// Extract transforms raw indicators, ticks, and candle state into a FeatureVector.
// Guaranteed 0 heap allocations per call.
func (fe *FeatureExtractor) Extract(
	tick model.Tick,
	fastEMA, slowEMA, rsi, atr, tps float64,
	candle model.Candle,
) FeatureVector {
	var fv FeatureVector

	mid := tick.MidPrice()
	if mid <= 0 {
		return fv
	}

	pipMult := model.PipMultiplier(tick.Symbol)

	// 1. EMA Delta (ATR-Normalized Dimensionless Ratio for Cross-Asset Parity)
	if fastEMA > 0 && slowEMA > 0 && atr > 0 {
		fv[FeatEMADelta] = (fastEMA - slowEMA) / atr
	} else if fastEMA > 0 && slowEMA > 0 {
		fv[FeatEMADelta] = (fastEMA - slowEMA) * pipMult
	}

	// 2. RSI Normalized [-1.0, +1.0]
	if rsi > 0 {
		fv[FeatRSINorm] = (rsi - 50.0) / 50.0
	}

	// 3. ATR in Pips
	if atr > 0 {
		fv[FeatATRNorm] = atr * pipMult
	}

	// 4. Spread Ratio
	spread := tick.Ask - tick.Bid
	if atr > 0 {
		fv[FeatSpreadRatio] = spread / atr
	} else {
		fv[FeatSpreadRatio] = 0.5
	}

	// 5. Tick Velocity (normalized)
	fv[FeatTickVelocity] = tps / 10.0

	// 6 & 7. Price Momentum (ATR-Normalized Dimensionless Ratios)
	if fe.historyCount >= 5 {
		idx5 := (fe.historyHead - 5 + 32) % 32
		p5 := fe.historyPrice[idx5]
		if p5 > 0 {
			if atr > 0 {
				fv[FeatPriceMomShort] = (mid - p5) / atr
			} else {
				fv[FeatPriceMomShort] = (mid - p5) * pipMult
			}
		}
	}
	if fe.historyCount >= 20 {
		idx20 := (fe.historyHead - 20 + 32) % 32
		p20 := fe.historyPrice[idx20]
		if p20 > 0 {
			if atr > 0 {
				fv[FeatPriceMomMedium] = (mid - p20) / atr
			} else {
				fv[FeatPriceMomMedium] = (mid - p20) * pipMult
			}
		}
	}

	// 8. Volatility Ratio (Short 8 ticks vs Long 32 ticks)
	if fe.historyCount >= 16 {
		var sumShort, sumLong float64
		nShort := 8
		nLong := fe.historyCount

		for i := 0; i < nLong; i++ {
			idx := (fe.historyHead - 1 - i + 32) % 32
			p := fe.historyPrice[idx]
			sumLong += p
			if i < nShort {
				sumShort += p
			}
		}
		meanShort := sumShort / float64(nShort)
		meanLong := sumLong / float64(nLong)

		var varShort, varLong float64
		for i := 0; i < nLong; i++ {
			idx := (fe.historyHead - 1 - i + 32) % 32
			p := fe.historyPrice[idx]
			diffL := p - meanLong
			varLong += diffL * diffL
			if i < nShort {
				diffS := p - meanShort
				varShort += diffS * diffS
			}
		}
		varShort /= float64(nShort)
		varLong /= float64(nLong)

		// Minimum variance floor based on 0.02 ATR to avoid division by micro-tick noise
		epsVar := 1e-6
		if atr > 0 {
			epsVar = (0.02 * atr) * (0.02 * atr)
		}
		ratio := math.Sqrt((varShort + epsVar) / (varLong + epsVar))
		if ratio > 4.0 {
			ratio = 4.0
		} else if ratio < 0.25 {
			ratio = 0.25
		}
		fv[FeatVolRatio] = ratio
	} else {
		fv[FeatVolRatio] = 1.0
	}

	// 9, 10, 11. Candle Microstructure Features
	rangeC := candle.High - candle.Low
	if rangeC > 1e-6 {
		body := math.Abs(candle.Close - candle.Open)
		fv[FeatBodyRatio] = body / rangeC

		maxOC := math.Max(candle.Open, candle.Close)
		minOC := math.Min(candle.Open, candle.Close)

		fv[FeatUpperWickRatio] = (candle.High - maxOC) / rangeC
		fv[FeatLowerWickRatio] = (minOC - candle.Low) / rangeC
	} else {
		fv[FeatBodyRatio] = 0.5
		fv[FeatUpperWickRatio] = 0.25
		fv[FeatLowerWickRatio] = 0.25
	}

	// 12. Directional Consistency
	if fe.historyCount >= 10 {
		posCount := 0
		for i := 1; i < 10; i++ {
			currIdx := (fe.historyHead - i + 32) % 32
			prevIdx := (fe.historyHead - i - 1 + 32) % 32
			if fe.historyPrice[currIdx] >= fe.historyPrice[prevIdx] {
				posCount++
			}
		}
		// Scale to [-1.0, +1.0]
		fv[FeatDirectionalConsistency] = (float64(posCount)/9.0)*2.0 - 1.0
	}

	return fv
}
