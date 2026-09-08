package strategy

import (
	"math"
	"sync"

	"github.com/pompbot/scalpbot/internal/indicator"
	"github.com/pompbot/scalpbot/internal/model"
)

// MomentumScalperConfig holds tunable parameters for the momentum scalper.
type MomentumScalperConfig struct {
	FastEMAPeriod int     // Fast EMA period (e.g., 5)
	SlowEMAPeriod int     // Slow EMA period (e.g., 13)
	RSIPeriod     int     // RSI period (e.g., 14)
	ATRPeriod     int     // ATR period for volatility gating (e.g., 14)
	RSIOverbought float64 // RSI upper threshold (e.g., 70)
	RSIOversold   float64 // RSI lower threshold (e.g., 30)
	ATRMinimum          float64 // Minimum ATR for trade entry (volatility gate)
	MinVolRatio         float64 // Minimum ATR / SMA(ATR) ratio to trade (default 0.60, Nyao Scalper VolRatio)
	EnableLimitPullback bool    // Discount entry on breakout pullback (default true)
	PullbackDiscountATR float64 // ATR discount depth for pullback limit (default 0.3)
	Symbol              string  // Target currency pair
}

// DefaultMomentumConfig returns sensible defaults for Gold (XAUUSD) M15 swing momentum.
func DefaultMomentumConfig() MomentumScalperConfig {
	return MomentumScalperConfig{
		FastEMAPeriod:       5,
		SlowEMAPeriod:       13,
		RSIPeriod:           14,
		ATRPeriod:           14,
		RSIOverbought:       70.0,
		RSIOversold:         30.0,
		ATRMinimum:          0.50, // $0.50 minimum ATR for Gold M15
		MinVolRatio:         0.60, // Minimum 60% of 50-bar baseline ATR
		EnableLimitPullback: true, // Discount limit entry on breakout pullback
		PullbackDiscountATR: 0.30, // 0.3x ATR discount from breakout close
		Symbol:              "XAUUSD",
	}
}

// MomentumScalper implements a swing-momentum strategy using:
//   - Dual EMA crossover for trend direction
//   - Multi-Bar EMA Slope Lookback to eliminate flat/fading whipsaws
//   - RSI overbought/oversold filter to avoid chasing
//   - ATR volatility gate to ensure sufficient movement
//
// Signal logic:
//   - BUY:  FastEMA crosses above SlowEMA + FastEMA Slope > 0 + RSI < Overbought + ATR > Minimum
//   - SELL: FastEMA crosses below SlowEMA + FastEMA Slope < 0 + RSI > Oversold + ATR > Minimum
type MomentumScalper struct {
	id   string
	cfg  MomentumScalperConfig
	fast *indicator.EMA
	slow *indicator.EMA
	rsi  *indicator.RSI
	atr  *indicator.ATR

	// Crossover & Multi-Bar Slope tracking
	prevFast     float64
	prevSlow     float64
	hasPrev      bool
	fastHistory  [6]float64     // Circular buffer for Fast EMA values (0 allocs)
	fastHistLen  int           // Count of stored values (capped at 6)
	fastHistIdx  int           // Next write position
	highHistory  [10]float64    // Circular buffer for recent candle highs (structure SL)
	lowHistory   [10]float64    // Circular buffer for recent candle lows (structure SL)
	candleCount  int           // Count of stored candles (capped at 10)
	candleIdx    int           // Next write position for candles
	atrSMA       *indicator.SMA // Baseline ATR over 50 bars
	lastVolRatio float64        // Current ATR / Baseline ATR ratio
	mu           sync.RWMutex
}

// NewMomentumScalper creates a new momentum scalper with the given config.
func NewMomentumScalper(id string, cfg MomentumScalperConfig) *MomentumScalper {
	return &MomentumScalper{
		id:           id,
		cfg:          cfg,
		fast:         indicator.NewEMA(cfg.FastEMAPeriod),
		slow:         indicator.NewEMA(cfg.SlowEMAPeriod),
		rsi:          indicator.NewRSI(cfg.RSIPeriod),
		atr:          indicator.NewATR(cfg.ATRPeriod),
		atrSMA:       indicator.NewSMA(50),
		lastVolRatio: 1.0,
	}
}

// ID returns the unique strategy identifier.
func (m *MomentumScalper) ID() string {
	return m.id
}

// OnTick is a no-op for this candle-based strategy.
// Returns NoSignal immediately to minimize tick-path overhead.
func (m *MomentumScalper) OnTick(_ model.Tick) model.Signal {
	return model.Signal{Type: model.NoSignal}
}

// OnCandle evaluates the momentum conditions and emits a signal if criteria are met.
func (m *MomentumScalper) OnCandle(candle model.Candle) model.Signal {
	m.mu.Lock()
	defer m.mu.Unlock()

	price := candle.Close

	// Update all indicators
	fastVal := m.fast.Update(price)
	slowVal := m.slow.Update(price)
	rsiVal := m.rsi.Update(price)
	atrVal := m.atr.Update(candle)
	if !math.IsNaN(atrVal) && atrVal > 0 {
		avgATR := m.atrSMA.Update(atrVal)
		if avgATR > 0 {
			m.lastVolRatio = atrVal / avgATR
		}
	}

	// Check if all indicators are warmed up
	if math.IsNaN(fastVal) || math.IsNaN(slowVal) || math.IsNaN(rsiVal) || math.IsNaN(atrVal) {
		m.prevFast = fastVal
		m.prevSlow = slowVal
		m.hasPrev = !math.IsNaN(fastVal) && !math.IsNaN(slowVal)
		return model.Signal{Type: model.NoSignal}
	}

	// Update Fast EMA history in circular buffer (0 allocs)
	m.fastHistory[m.fastHistIdx] = fastVal
	m.fastHistIdx = (m.fastHistIdx + 1) % 6
	if m.fastHistLen < 6 {
		m.fastHistLen++
	}

	// Update High & Low history in circular buffer (0 allocs)
	m.highHistory[m.candleIdx] = candle.High
	m.lowHistory[m.candleIdx] = candle.Low
	m.candleIdx = (m.candleIdx + 1) % 10
	if m.candleCount < 10 {
		m.candleCount++
	}

	// Need previous EMA values to detect crossover
	if !m.hasPrev {
		m.prevFast = fastVal
		m.prevSlow = slowVal
		m.hasPrev = true
		return model.Signal{Type: model.NoSignal}
	}

	// Detect crossover
	signal := m.evaluateCrossover(fastVal, slowVal, rsiVal, atrVal, candle)

	// Store for next comparison
	m.prevFast = fastVal
	m.prevSlow = slowVal

	return signal
}

// evaluateCrossover checks for EMA crossover with multi-bar slope alignment or pullback re-entry.
func (m *MomentumScalper) evaluateCrossover(
	fastVal, slowVal, rsiVal, atrVal float64,
	candle model.Candle,
) model.Signal {
	noSignal := model.Signal{Type: model.NoSignal}

	// ATR volatility gate — skip low-volatility periods
	if atrVal < m.cfg.ATRMinimum {
		return noSignal
	}

	// Dead-Market Volatility Filter (Nyao Scalper v43.0 MinVolRatioToTrade)
	// If current ATR is < 60% of baseline ATR, the market is dormant (spread bleed risk).
	if m.cfg.MinVolRatio > 0 && m.atrSMA.Ready() && m.lastVolRatio < m.cfg.MinVolRatio {
		return noSignal
	}

	// Calculate Multi-Bar Fast EMA Slope (Lookback 2 bars from circular buffer)
	var fastSlope float64
	if m.fastHistLen >= 3 {
		// Index 3 bars ago in circular buffer
		idx := (m.fastHistIdx - 3 + 6) % 6
		fastSlope = fastVal - m.fastHistory[idx]
	} else if m.fastHistLen >= 2 {
		idx := (m.fastHistIdx - 2 + 6) % 6
		fastSlope = fastVal - m.fastHistory[idx]
	}

	// 1. Bullish crossover: fast crosses above slow
	if m.prevFast <= m.prevSlow && fastVal > slowVal {
		// Multi-Bar Slope Guard: reject flat or declining crossover (whipsaw filter)
		if m.fastHistLen >= 2 && fastSlope <= 0 {
			return noSignal
		}
		// RSI filter: not overbought (still room to run)
		if rsiVal < m.cfg.RSIOverbought {
			// Over-extension check: avoid chasing if price is already extended > 0.40x ATR above FastEMA
			// Force extended impulses to wait for healthy pullback re-entry (Condition 3)
			if candle.Close-fastVal > atrVal*0.40 {
				return noSignal
			}

			entryPrice := candle.Close
			if m.cfg.EnableLimitPullback && m.cfg.PullbackDiscountATR > 0 {
				discountPrice := candle.Close - (m.cfg.PullbackDiscountATR * atrVal)
				if discountPrice > fastVal {
					entryPrice = discountPrice
				} else {
					entryPrice = fastVal
				}
			}

			swingLow := m.recentSwingLow(5)
			sl, tp := computeStructureSLTP(candle.Symbol, entryPrice, atrVal, swingLow, true)
			return model.Signal{
				Type:        model.Buy,
				Symbol:      candle.Symbol,
				Price:       entryPrice,
				Strength:    m.computeStrength(fastVal, slowVal, rsiVal),
				StopLoss:    sl,
				TakeProfit:  tp,
				TimestampNs: candle.TimestampNs,
				StrategyID:  m.id,
			}
		}
	}

	// 2. Bearish crossover: fast crosses below slow
	if m.prevFast >= m.prevSlow && fastVal < slowVal {
		// Multi-Bar Slope Guard: reject flat or rising crossover (whipsaw filter)
		if m.fastHistLen >= 2 && fastSlope >= 0 {
			return noSignal
		}
		// RSI filter: not oversold (still room to fall)
		if rsiVal > m.cfg.RSIOversold {
			// Over-extension check: avoid chasing if price is already extended > 0.40x ATR below FastEMA
			// Force extended breakdowns to wait for healthy rally pullback re-entry (Condition 3)
			if fastVal-candle.Close > atrVal*0.40 {
				return noSignal
			}

			entryPrice := candle.Close
			if m.cfg.EnableLimitPullback && m.cfg.PullbackDiscountATR > 0 {
				discountPrice := candle.Close + (m.cfg.PullbackDiscountATR * atrVal)
				if discountPrice < fastVal {
					entryPrice = discountPrice
				} else {
					entryPrice = fastVal
				}
			}

			swingHigh := m.recentSwingHigh(5)
			sl, tp := computeStructureSLTP(candle.Symbol, entryPrice, atrVal, swingHigh, false)
			return model.Signal{
				Type:        model.Sell,
				Symbol:      candle.Symbol,
				Price:       entryPrice,
				Strength:    m.computeStrength(fastVal, slowVal, rsiVal),
				StopLoss:    sl,
				TakeProfit:  tp,
				TimestampNs: candle.TimestampNs,
				StrategyID:  m.id,
			}
		}
	}

	// 3. High-Probability Institutional Pullback Re-entry (Value Area Entry + Rejection Wick)
	cRange := candle.High - candle.Low
	if cRange <= 0 {
		cRange = 0.01
	}

	if fastVal > slowVal && m.prevFast > m.prevSlow {
		// Established Uptrend: Price tested near FastEMA support zone and rejected lower prices
		lowerWick := math.Min(candle.Open, candle.Close) - candle.Low
		testedSupport := candle.Low <= fastVal+atrVal*0.15
		notExtended := candle.Close-fastVal <= atrVal*0.40
		bullishRejection := (lowerWick >= 0.20*cRange) || (candle.Close > candle.Open && candle.Close >= fastVal-atrVal*0.20)

		if testedSupport && notExtended && bullishRejection {
			if rsiVal >= 40.0 && rsiVal <= 68.0 {
				swingLow := m.recentSwingLow(5)
				sl, tp := computeStructureSLTP(candle.Symbol, candle.Close, atrVal, swingLow, true)
				return model.Signal{
					Type:        model.Buy,
					Symbol:      candle.Symbol,
					Price:       candle.Close,
					Strength:    0.85,
					StopLoss:    sl,
					TakeProfit:  tp,
					TimestampNs: candle.TimestampNs,
					StrategyID:  m.id + "-pullback",
				}
			}
		}
	} else if fastVal < slowVal && m.prevFast < m.prevSlow {
		// Established Downtrend: Price rallied into FastEMA resistance zone and rejected higher prices
		upperWick := candle.High - math.Max(candle.Open, candle.Close)
		testedResistance := candle.High >= fastVal-atrVal*0.15
		notExtended := fastVal-candle.Close <= atrVal*0.40
		bearishRejection := (upperWick >= 0.20*cRange) || (candle.Close < candle.Open && candle.Close <= fastVal+atrVal*0.20)

		if testedResistance && notExtended && bearishRejection {
			if rsiVal <= 60.0 && rsiVal >= 32.0 {
				swingHigh := m.recentSwingHigh(5)
				sl, tp := computeStructureSLTP(candle.Symbol, candle.Close, atrVal, swingHigh, false)
				return model.Signal{
					Type:        model.Sell,
					Symbol:      candle.Symbol,
					Price:       candle.Close,
					Strength:    0.85,
					StopLoss:    sl,
					TakeProfit:  tp,
					TimestampNs: candle.TimestampNs,
					StrategyID:  m.id + "-pullback",
				}
			}
		}
	}

	return noSignal
}

// recentSwingHigh finds the maximum candle high in the last lookback bars (0 heap allocs).
func (m *MomentumScalper) recentSwingHigh(lookback int) float64 {
	if m.candleCount == 0 {
		return 0
	}
	n := lookback
	if n > m.candleCount {
		n = m.candleCount
	}
	maxH := -math.MaxFloat64
	for i := 0; i < n; i++ {
		idx := (m.candleIdx - 1 - i + 10) % 10
		if m.highHistory[idx] > maxH {
			maxH = m.highHistory[idx]
		}
	}
	if maxH == -math.MaxFloat64 {
		return 0
	}
	return maxH
}

// recentSwingLow finds the minimum candle low in the last lookback bars (0 heap allocs).
func (m *MomentumScalper) recentSwingLow(lookback int) float64 {
	if m.candleCount == 0 {
		return 0
	}
	n := lookback
	if n > m.candleCount {
		n = m.candleCount
	}
	minL := math.MaxFloat64
	for i := 0; i < n; i++ {
		idx := (m.candleIdx - 1 - i + 10) % 10
		if m.lowHistory[idx] < minL {
			minL = m.lowHistory[idx]
		}
	}
	if minL == math.MaxFloat64 {
		return 0
	}
	return minL
}

// computeStrength returns a signal strength value in [0, 1] based on
// the EMA divergence magnitude and RSI extremity.
func (m *MomentumScalper) computeStrength(fast, slow, rsi float64) float64 {
	// EMA divergence as a fraction of slow EMA
	divergence := math.Abs(fast-slow) / slow
	// Normalize divergence (cap at 0.5 for extreme moves)
	divScore := math.Min(divergence*1000, 0.5)

	// RSI distance from neutral (50)
	rsiScore := math.Abs(rsi-50) / 100.0

	return math.Min(divScore+rsiScore, 1.0)
}

// FastEMA returns the current value of the fast EMA indicator.
func (m *MomentumScalper) FastEMA() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.fast == nil {
		return 0
	}
	return m.fast.Value()
}

// SlowEMA returns the current value of the slow EMA indicator.
func (m *MomentumScalper) SlowEMA() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.slow == nil {
		return 0
	}
	return m.slow.Value()
}

// RSI returns the current value of the RSI indicator.
func (m *MomentumScalper) RSI() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.rsi == nil {
		return 0
	}
	return m.rsi.Value()
}

// ATR returns the current value of the ATR indicator.
func (m *MomentumScalper) ATR() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.atr == nil {
		return 0
	}
	return m.atr.Value()
}

// VolRatio returns current short-term to long-term volatility ratio.
func (m *MomentumScalper) VolRatio() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastVolRatio
}

// computeStructureSLTP calculates a structure-based Stop Loss positioned beyond
// recent swing extremes, plus a Take Profit guaranteeing at least 1:2.2 RRR.
func computeStructureSLTP(symbol string, entryPrice, atrVal, swingExtreme float64, isBuy bool) (sl, tp float64) {
	prof := model.DetectAssetClass(symbol)
	buffer := 0.50 // $0.50 buffer beyond swing extreme on Gold
	if !prof.IsGold {
		buffer = 5.0 / model.PipMultiplier(symbol)
	}

	minSL := math.Max(atrVal*1.2, 3.50) // Minimum $3.50 breathing room on Gold
	maxSL := math.Max(atrVal*2.0, 6.50) // Maximum $6.50 risk cap on Gold

	if isBuy {
		if swingExtreme > 0 && swingExtreme < entryPrice {
			sl = swingExtreme - buffer
		} else {
			sl = entryPrice - minSL
		}
		riskDist := entryPrice - sl
		if riskDist < minSL {
			sl = entryPrice - minSL
			riskDist = minSL
		} else if riskDist > maxSL {
			sl = entryPrice - maxSL
			riskDist = maxSL
		}
		tp = entryPrice + math.Max(riskDist*2.2, 8.00)
	} else {
		if swingExtreme > 0 && swingExtreme > entryPrice {
			sl = swingExtreme + buffer
		} else {
			sl = entryPrice + minSL
		}
		riskDist := sl - entryPrice
		if riskDist < minSL {
			sl = entryPrice + minSL
			riskDist = minSL
		} else if riskDist > maxSL {
			sl = entryPrice + maxSL
			riskDist = maxSL
		}
		tp = entryPrice - math.Max(riskDist*2.2, 8.00)
	}
	return sl, tp
}

func computeMomentumSLTP(symbol string, price, atrVal float64, isBuy bool) (sl, tp float64) {
	return computeStructureSLTP(symbol, price, atrVal, 0, isBuy)
}
