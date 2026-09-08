package strategy

import (
	"math"
	"sync"
	"time"

	"github.com/pompbot/scalpbot/internal/indicator"
	"github.com/pompbot/scalpbot/internal/model"
)

// RangeScalperConfig holds tunable parameters for the Mean-Reversion Range Scalper.
type RangeScalperConfig struct {
	BollingerPeriod int     // Bollinger Band window (e.g. 20)
	BollingerStdDev float64 // Bollinger Standard Deviations (e.g. 2.0)
	RSIPeriod       int     // RSI period (e.g. 14)
	ATRPeriod       int     // ATR period (e.g. 14)
	RSIOverbought   float64 // Reversal sell trigger (e.g. 62.0)
	RSIOversold     float64 // Reversal buy trigger (e.g. 38.0)
	BandwidthMax    float64 // Maximum bandwidth to confirm ranging chop (e.g. 0.0035)
	MinTPPoints     float64 // Minimum take profit distance (in price units)
	Symbol          string  // Target symbol
}

// DefaultRangeConfig returns standard defaults for Gold mean-reversion scalping.
func DefaultRangeConfig() RangeScalperConfig {
	return RangeScalperConfig{
		BollingerPeriod: 20,
		BollingerStdDev: 1.8,
		RSIPeriod:       14,
		ATRPeriod:       14,
		RSIOverbought:   54.0,
		RSIOversold:     46.0,
		BandwidthMax:    0.0050,
		MinTPPoints:     1.50,
		Symbol:          "XAUUSD",
	}
}

// RangeScalper implements an institutional mean-reversion strategy for sideways / Asian session.
// Generates:
//   - BUY when price touches/penetrates Lower Band & RSI is oversold
//   - SELL when price touches/penetrates Upper Band & RSI is overbought
type RangeScalper struct {
	id   string
	cfg  RangeScalperConfig
	bb   *indicator.Bollinger
	rsi  *indicator.RSI
	atr  *indicator.ATR

	// State tracking to prevent multi-firing within the same candle
	lastSignalType model.SignalType
	lastSignalTime int64
	mu             sync.RWMutex
}

// NewRangeScalper creates a new mean-reversion range scalper.
func NewRangeScalper(id string, cfg RangeScalperConfig) *RangeScalper {
	return &RangeScalper{
		id:   id,
		cfg:  cfg,
		bb:   indicator.NewBollinger(cfg.BollingerPeriod, cfg.BollingerStdDev),
		rsi:  indicator.NewRSI(cfg.RSIPeriod),
		atr:  indicator.NewATR(cfg.ATRPeriod),
	}
}

// ID returns the unique strategy identifier.
func (r *RangeScalper) ID() string {
	return r.id
}

// OnTick is a no-op for candle-based calculation to keep latency sub-microsecond.
func (r *RangeScalper) OnTick(_ model.Tick) model.Signal {
	return model.Signal{Type: model.NoSignal}
}

// OnCandle processes a completed candle and emits mean-reversion signals.
func (r *RangeScalper) OnCandle(candle model.Candle) model.Signal {
	r.mu.Lock()
	defer r.mu.Unlock()

	bbRes := r.bb.Update(candle.Close)
	rsiVal := r.rsi.Update(candle.Close)
	atrVal := r.atr.Update(candle)

	if !bbRes.Valid || !r.rsi.Ready() || !r.atr.Ready() {
		return model.Signal{Type: model.NoSignal}
	}

	// Bandwidth Guard: Mean-reversion is ONLY valid in narrow/consolidating ranges
	if r.cfg.BandwidthMax > 0 && bbRes.Bandwidth > r.cfg.BandwidthMax {
		return model.Signal{Type: model.NoSignal}
	}

	now := candle.TimestampNs
	if now == 0 {
		now = time.Now().UnixNano()
	}

	// Anti multi-firing: ignore if signal already emitted for this exact candle timestamp
	if r.lastSignalTime == now && r.lastSignalType != model.NoSignal {
		return model.Signal{Type: model.NoSignal}
	}

	// 1. BUY MEAN-REVERSION (Price touches lower band + RSI confirms oversold bounce)
	oversoldThreshold := r.cfg.RSIOversold
	if oversoldThreshold <= 0 {
		oversoldThreshold = 38.0
	}
	if (candle.Low <= bbRes.Lower || bbRes.PercentB <= 0.15) && rsiVal <= oversoldThreshold {
		minTP, maxTP := getSymbolMinMaxTP(r.cfg.Symbol, atrVal)
		slDist := math.Max(atrVal*0.8, minTP*0.5)
		tpDist := math.Max(slDist*1.8, minTP)
		if tpDist > maxTP {
			tpDist = maxTP
		}

		sl := candle.Close - slDist
		tp := candle.Close + tpDist

		// Calculate confidence score
		strength := 0.55 + (50.0-rsiVal)*0.01
		if strength > 0.90 {
			strength = 0.90
		}

		r.lastSignalType = model.Buy
		r.lastSignalTime = now

		return model.Signal{
			Type:        model.Buy,
			Symbol:      r.cfg.Symbol,
			Price:       candle.Close,
			Strength:    strength,
			StopLoss:    sl,
			TakeProfit:  tp,
			TimestampNs: now,
			StrategyID:  r.id,
		}
	}

	// 2. SELL MEAN-REVERSION (Price touches upper band + RSI confirms overbought reversal)
	overboughtThreshold := r.cfg.RSIOverbought
	if overboughtThreshold <= 0 {
		overboughtThreshold = 62.0
	}
	if (candle.High >= bbRes.Upper || bbRes.PercentB >= 0.85) && rsiVal >= overboughtThreshold {
		minTP, maxTP := getSymbolMinMaxTP(r.cfg.Symbol, atrVal)
		slDist := math.Max(atrVal*0.8, minTP*0.5)
		tpDist := math.Max(slDist*1.8, minTP)
		if tpDist > maxTP {
			tpDist = maxTP
		}

		sl := candle.Close + slDist
		tp := candle.Close - tpDist

		// Calculate confidence score
		strength := 0.55 + (rsiVal-50.0)*0.01
		if strength > 0.90 {
			strength = 0.90
		}

		r.lastSignalType = model.Sell
		r.lastSignalTime = now

		return model.Signal{
			Type:        model.Sell,
			Symbol:      r.cfg.Symbol,
			Price:       candle.Close,
			Strength:    strength,
			StopLoss:    sl,
			TakeProfit:  tp,
			TimestampNs: now,
			StrategyID:  r.id,
		}
	}

	return model.Signal{Type: model.NoSignal}
}

func getSymbolMinMaxTP(symbol string, atrVal float64) (minTP, maxTP float64) {
	// Gold (XAUUSD): 1 pip = $0.01. Min TP = $1.50 (150 pips), Max TP = $8.00 (800 pips)
	minTP = math.Max(atrVal*0.8, 1.50)
	maxTP = math.Max(atrVal*3.0, 8.00)
	return minTP, maxTP
}

// GetIndicators returns current indicator state for dashboard telemetry.
func (r *RangeScalper) GetIndicators() (upper, middle, lower, rsi, atr float64, ready bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.rsi.Ready() || !r.atr.Ready() {
		return 0, 0, 0, 0, 0, false
	}
	res := r.bb.Last()
	return res.Upper, res.Middle, res.Lower, r.rsi.Value(), r.atr.Value(), res.Valid
}
