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
	fastHistory  []float64      // Last N Fast EMA values for slope measurement
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
		fastHistory:  make([]float64, 0, 8),
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

	// Update Fast EMA history for multi-bar slope lookback
	m.fastHistory = append(m.fastHistory, fastVal)
	if len(m.fastHistory) > 6 {
		m.fastHistory = m.fastHistory[len(m.fastHistory)-6:]
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

	// Calculate Multi-Bar Fast EMA Slope (Lookback 2 bars)
	var fastSlope float64
	if len(m.fastHistory) >= 3 {
		fastSlope = fastVal - m.fastHistory[len(m.fastHistory)-3]
	} else if len(m.fastHistory) >= 2 {
		fastSlope = fastVal - m.fastHistory[len(m.fastHistory)-2]
	}

	// 1. Bullish crossover: fast crosses above slow
	if m.prevFast <= m.prevSlow && fastVal > slowVal {
		// Multi-Bar Slope Guard: reject flat or declining crossover (whipsaw filter)
		if len(m.fastHistory) >= 2 && fastSlope <= 0 {
			return noSignal
		}
		// RSI filter: not overbought (still room to run)
		if rsiVal < m.cfg.RSIOverbought {
			// Over-extension check: avoid chasing if price is already flying > 1.5x ATR above FastEMA
			if candle.Close-fastVal > atrVal*1.5 {
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

			sl, tp := computeMomentumSLTP(candle.Symbol, entryPrice, atrVal, true)
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
		if len(m.fastHistory) >= 2 && fastSlope >= 0 {
			return noSignal
		}
		// RSI filter: not oversold (still room to fall)
		if rsiVal > m.cfg.RSIOversold {
			// Over-extension check: avoid chasing if price is already plunging > 1.5x ATR below FastEMA
			if fastVal-candle.Close > atrVal*1.5 {
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

			sl, tp := computeMomentumSLTP(candle.Symbol, entryPrice, atrVal, false)
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

	// 3. High-Probability Pullback Re-entry (Dip Buying on Uptrend & Rally Selling on Downtrend)
	if fastVal > slowVal && m.prevFast > m.prevSlow {
		// Established Uptrend: Price tested near FastEMA zone and closed bullish
		if candle.Low <= fastVal+atrVal*0.50 && candle.Close >= fastVal-atrVal*0.20 && candle.Close >= candle.Open {
			if rsiVal >= 38.0 && rsiVal <= 68.0 {
				sl, tp := computeMomentumSLTP(candle.Symbol, candle.Close, atrVal, true)
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
		// Established Downtrend: Price tested near FastEMA zone and closed bearish
		if candle.High >= fastVal-atrVal*0.50 && candle.Close <= fastVal+atrVal*0.20 && candle.Close <= candle.Open {
			if rsiVal <= 62.0 && rsiVal >= 32.0 {
				sl, tp := computeMomentumSLTP(candle.Symbol, candle.Close, atrVal, false)
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

func computeMomentumSLTP(symbol string, price, atrVal float64, isBuy bool) (sl, tp float64) {
	prof := model.DetectAssetClass(symbol)
	slDist := atrVal * 1.5
	tpDist := atrVal * 3.0

	if prof.IsGold {
		// Gold (XAUUSD): 1 pip = $0.01. Minimum SL wide enough to survive M15 swing volatility + spread.
		slDist = math.Max(atrVal*1.5, 3.50) // $3.50 minimum SL (350 pips) to survive healthy retracements
		tpDist = math.Max(slDist*2.5, 8.50)  // $8.50 minimum TP (2.5:1 RRR)
	}

	if isBuy {
		sl = price - slDist
		tp = price + tpDist
	} else {
		sl = price + slDist
		tp = price - tpDist
	}
	return sl, tp
}
