package backtest

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pompbot/scalpbot/internal/ai"
	"github.com/pompbot/scalpbot/internal/ai/hmm"
	"github.com/pompbot/scalpbot/internal/executor"
	"github.com/pompbot/scalpbot/internal/indicator"
	"github.com/pompbot/scalpbot/internal/marketdata"
	"github.com/pompbot/scalpbot/internal/model"
	"github.com/pompbot/scalpbot/internal/risk"
	"github.com/pompbot/scalpbot/internal/strategy"
)

// EngineConfig holds all configuration parameters for the backtesting engine.
type EngineConfig struct {
	InitialBalance   float64
	Symbol           string
	CandlePeriod     time.Duration
	SlippagePips     float64
	CommissionPerLot float64 // Commission per round-turn lot (e.g. $3.50)

	StrategyConfig strategy.MomentumScalperConfig
	RangeConfig    strategy.RangeScalperConfig
	EnableDualMode bool
	RiskConfig     risk.ManagerConfig
	TrackerConfig  executor.TrackerConfig
	AIConfig       ai.FilterConfig
	SessionFilter  *risk.SessionFilter
}

// DefaultEngineConfig returns standard backtest configuration for Gold (XAUUSD) scalping.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		InitialBalance:   10000.0,
		Symbol:           "XAUUSD",
		CandlePeriod:     5 * time.Minute, // M5 Execution (realistic for small accounts)
		SlippagePips:     1.0,
		CommissionPerLot: 3.50,
		StrategyConfig: strategy.MomentumScalperConfig{
			FastEMAPeriod: 5,
			SlowEMAPeriod: 13,
			RSIPeriod:     14,
			ATRPeriod:     14,
			RSIOverbought: 70.0,
			RSIOversold:   30.0,
			ATRMinimum:    0.35, // Scaled for M5 Gold
			Symbol:        "XAUUSD",
		},
		RiskConfig: risk.ManagerConfig{
			InitialEquity:        10000.0,
			RiskPerTrade:         0.01,
			MaxDailyDrawdown:     0.05,
			MaxSpreadPips:        60.0,
			MaxOpenPositions:     2,
			MaxPositionsPerSymbol: 1,
			MinLotSize:           0.01,
			MaxLotSize:           0.01, // Fixed safe 0.01 lot
		},
		TrackerConfig: executor.TrackerConfig{
			TrailingMode:       executor.TrailingATR,
			TrailingStopPips:   150.0, // $1.50 trailing room
			TrailingATRMult:    1.5,
			BreakEvenPips:      200.0, // $2.00 profit before BE
			BreakEvenBuffPips:  20.0,  // $0.20 buffer above entry
			EnableTrailing:     true,
			EnableBreakEven:    true,
			EnableProfitLocker: true,
			Stage1ATRMult:      1.0,
			Stage2ATRMult:      1.8,
			Stage3ATRMult:      2.5,
			EnablePartialTP:    true,
			PartialTPRatio:     0.5,
			TP1Pips:            300.0, // $3.00 Partial TP1
			TP2Pips:            600.0, // $6.00 Final TP2
			TimeStopDuration:   45 * time.Minute,
			AutoRemoveOnClose:  true,
		},
		AIConfig: ai.FilterConfig{
			EnableMLFilter:    true,
			MinConfidence:     0.55,
			FilterRangingChop: true,
		},
	}
}

// SimulatedPosition maps an active position in the backtester.
type SimulatedPosition struct {
	model.Position
	Commission float64
}

// Engine executes historical tick data through the full quant strategy pipeline.
type Engine struct {
	cfg EngineConfig
}

// NewEngine creates a new backtesting engine instance.
func NewEngine(cfg EngineConfig) *Engine {
	return &Engine{cfg: cfg}
}

// Run executes the simulation over the given slice of ticks.
func (e *Engine) Run(ticks []model.Tick) (*PerformanceReport, []TradeRecord, error) {
	if len(ticks) == 0 {
		return nil, nil, fmt.Errorf("empty tick slice provided")
	}

	symbol := e.cfg.Symbol
	pipMult := model.PipMultiplier(symbol)

	// 1. Initialize Pipeline Components
	strat := strategy.NewMomentumScalper("scalper", e.cfg.StrategyConfig)
	var rangeStrat *strategy.RangeScalper
	if e.cfg.EnableDualMode {
		rangeStrat = strategy.NewRangeScalper("range_scalper", e.cfg.RangeConfig)
	}
	riskMgr := risk.NewManager(e.cfg.RiskConfig)
	if e.cfg.SessionFilter != nil {
		riskMgr.AddFilter(e.cfg.SessionFilter)
	}
	tracker := executor.NewPositionTracker(e.cfg.TrackerConfig)
	aggregator := marketdata.NewOHLCVAggregator(e.cfg.CandlePeriod)
	aiFilter := ai.NewSignalFilter(e.cfg.AIConfig)

	// Gate 1: Macro Trend Aggregators (M15 and H1)
	m15Agg := marketdata.NewOHLCVAggregator(15 * time.Minute)
	h1Agg := marketdata.NewOHLCVAggregator(1 * time.Hour)
	m15Fast := indicator.NewEMA(5)
	m15Slow := indicator.NewEMA(13)
	h1Fast := indicator.NewEMA(5)
	h1Slow := indicator.NewEMA(13)
	h1Trend := "NEUTRAL"
	m15Trend := "NEUTRAL"

	// State trackers
	activePositions := make(map[string]SimulatedPosition, 16)
	trades := make([]TradeRecord, 0, 1024)
	equityCurve := make([]EquityPoint, 0, len(ticks)/100+128)

	currentBalance := e.cfg.InitialBalance
	orderSeq := 0
	var lastClosedCandle model.Candle

	// Funnel diagnostics
	candlesClosed := 0
	signalsGenerated := 0
	macroRejections := 0
	aiRejections := 0
	riskRejections := 0

	// Pre-record initial equity point
	equityCurve = append(equityCurve, EquityPoint{
		TimestampNs: ticks[0].TimestampNs,
		Equity:      currentBalance,
		DrawdownPct: 0,
	})

	atrVal := e.cfg.StrategyConfig.ATRMinimum

	// 2. Core Simulation Loop
	for i := 0; i < len(ticks); i++ {
		tick := ticks[i]
		aiFilter.OnTick(tick)

		// --- A. Update Position Tracker (SL/TP, Trailing, Partial TP, Time-Stop) ---
		closes := tracker.OnTick(tick, atrVal, pipMult)
		for _, ce := range closes {
			simPos, exists := activePositions[ce.OrderID]
			if !exists {
				continue
			}

			// Calculate simulated exit price with slippage
			var exitPrice float64
			var pnlPips float64
			slipPrice := e.cfg.SlippagePips / pipMult

			if simPos.Side == model.SideBuy {
				exitPrice = tick.Bid - slipPrice
				pnlPips = (exitPrice - simPos.EntryPrice) * pipMult
			} else {
				exitPrice = tick.Ask + slipPrice
				pnlPips = (simPos.EntryPrice - exitPrice) * pipMult
			}

			// Calculate PnL in currency
			pipVal := model.PipValue(symbol, ce.Lots)
			grossPnL := pnlPips * pipVal
			comm := ce.Lots * e.cfg.CommissionPerLot
			netPnL := grossPnL - comm

			currentBalance += netPnL
			if !ce.IsPartial {
				riskMgr.RecordCloseForSymbol(symbol, netPnL)
			} else {
				riskMgr.RecordClose(netPnL)
			}

			// Record trade history
			trades = append(trades, TradeRecord{
				TradeID:     ce.OrderID,
				Symbol:      symbol,
				Side:        simPos.Side,
				Lots:        ce.Lots,
				EntryPrice:  simPos.EntryPrice,
				ExitPrice:   exitPrice,
				EntryTimeNs: simPos.OpenTimeNs,
				ExitTimeNs:  tick.TimestampNs,
				PnL:         netPnL,
				PnLPips:     pnlPips,
				Commission:  comm,
				Reason:      ce.Reason,
				IsPartial:   ce.IsPartial,
			})

			if ce.IsPartial {
				simPos.Lots -= ce.Lots
				activePositions[ce.OrderID] = simPos
			} else {
				delete(activePositions, ce.OrderID)
			}
		}

		// --- B. Generate Signals ---
		var sig model.Signal
		sig = strat.OnTick(tick)

		// Update Gate 1 Macro Trend Aggregators (M15 and H1)
		if m15Candle, m15Closed := m15Agg.OnTick(tick); m15Closed {
			f := m15Fast.Update(m15Candle.Close)
			s := m15Slow.Update(m15Candle.Close)
			if !math.IsNaN(f) && !math.IsNaN(s) {
				if f > s*1.0001 {
					m15Trend = "BULLISH"
				} else if f < s*0.9999 {
					m15Trend = "BEARISH"
				} else {
					m15Trend = "NEUTRAL"
				}
			}
		}

		if h1Candle, h1Closed := h1Agg.OnTick(tick); h1Closed {
			f := h1Fast.Update(h1Candle.Close)
			s := h1Slow.Update(h1Candle.Close)
			if !math.IsNaN(f) && !math.IsNaN(s) {
				if f > s*1.0001 {
					h1Trend = "BULLISH"
				} else if f < s*0.9999 {
					h1Trend = "BEARISH"
				} else {
					h1Trend = "NEUTRAL"
				}
			}
		}

		if candle, closed := aggregator.OnTick(tick); closed {
			candlesClosed++
			lastClosedCandle = candle
			atrCur := strat.ATR()
			if atrCur > 0 {
				atrVal = atrCur
			}
			aiFilter.UpdateHMM(candle, atrVal)
			cSig := strat.OnCandle(candle)
			if cSig.IsActionable() {
				signalsGenerated++
				sig = cSig
			} else if e.cfg.EnableDualMode && rangeStrat != nil {
				// Adaptive Regime Guard: Range Scalper is STRICTLY gated to hmm.StateNoise (Consolidation/Ranging)
				if lastHMM, _ := aiFilter.LastHMMState(); lastHMM == hmm.StateNoise {
					rSig := rangeStrat.OnCandle(candle)
					if rSig.IsActionable() {
						signalsGenerated++
						sig = rSig
					}
				}
			}
		}

		// --- C. Evaluate AI Model & Risk Rules ---
		if sig.IsActionable() {
			// Gate 1: Macro Trend Lock (H1 first, then M15)
			effectiveTrend := "NEUTRAL"
			if h1Trend != "NEUTRAL" {
				effectiveTrend = h1Trend
			} else if m15Trend != "NEUTRAL" {
				effectiveTrend = m15Trend
			}

			if effectiveTrend == "BEARISH" && sig.Type == model.Buy {
				macroRejections++
				continue // Forbid buying during macro downtrend
			} else if effectiveTrend == "BULLISH" && sig.Type == model.Sell {
				macroRejections++
				continue // Forbid selling during macro uptrend
			}

			// AI Filter Check
			fastEMA := strat.FastEMA()
			slowEMA := strat.SlowEMA()
			rsiVal := strat.RSI()
			atrCur := strat.ATR()
			if atrCur <= 0 {
				atrCur = atrVal
			}

			aiAllowed, aiConf, _, _ := aiFilter.EvaluateSignal(sig, tick, fastEMA, slowEMA, rsiVal, atrCur, 5.0, lastClosedCandle)
			if !aiAllowed {
				aiRejections++
				continue // Skip signal rejected by AI
			}

			// Risk Engine Check with Marcos López de Prado Dynamic Bet Sizing
			order, err := riskMgr.EvaluateWithConfidence(sig, tick, atrVal, aiConf)
			if err != nil {
				riskRejections++
				continue
			}
			if err == nil {
				orderSeq++
				orderID := fmt.Sprintf("BT-%d", orderSeq)

				// Calculate fill price with slippage
				var fillPrice float64
				slipPrice := e.cfg.SlippagePips / pipMult
				if order.Side == model.SideBuy {
					fillPrice = tick.Ask + slipPrice
				} else {
					fillPrice = tick.Bid - slipPrice
				}

				// Apply Dynamic Volatility & Structure SL/TP Engine
				execPrice := fillPrice
				if sig.StopLoss > 0 && sig.TakeProfit > 0 {
					structSLDist := math.Abs(execPrice - sig.StopLoss)
					minSL := 3.50
					maxSL := 6.50
					isGold := strings.Contains(strings.ToUpper(symbol), "XAU") || strings.Contains(strings.ToUpper(symbol), "GOLD")
					if !isGold {
						minSL = 10.0 / pipMult
						maxSL = 30.0 / pipMult
					}

					if structSLDist < minSL {
						structSLDist = minSL
					} else if structSLDist > maxSL {
						structSLDist = maxSL
					}

					if order.Side == model.SideBuy {
						order.StopLoss = execPrice - structSLDist
					} else {
						order.StopLoss = execPrice + structSLDist
					}

					structTPDist := math.Abs(sig.TakeProfit - execPrice)
					minTPDist := structSLDist * 1.5
					if isGold && minTPDist < 5.00 {
						minTPDist = 5.00
					}
					if structTPDist < minTPDist {
						structTPDist = minTPDist
					}
					if isGold && structTPDist > 12.00 {
						structTPDist = 12.00
					}

					if order.Side == model.SideBuy {
						order.TakeProfit = execPrice + structTPDist
					} else {
						order.TakeProfit = execPrice - structTPDist
					}
				}

				pos := model.Position{
					OrderID:    orderID,
					Symbol:     symbol,
					Side:       order.Side,
					Lots:       order.Lots,
					EntryPrice: fillPrice,
					OpenTimeNs: tick.TimestampNs,
				}

				// Register in active positions & tracker
				comm := order.Lots * e.cfg.CommissionPerLot
				activePositions[orderID] = SimulatedPosition{
					Position:   pos,
					Commission: comm,
				}
				tracker.Add(pos, order.StopLoss, order.TakeProfit)
				riskMgr.RecordFillForSymbol(symbol)
			}
		}

		// --- D. Sample Equity Curve (every 500 ticks or on last tick) ---
		if i%500 == 0 || i == len(ticks)-1 {
			floatingPnL := 0.0
			for _, pos := range tracker.ActivePositions() {
				floatingPnL += pos.CurrentPnL
			}
			equity := currentBalance + floatingPnL
			equityCurve = append(equityCurve, EquityPoint{
				TimestampNs: tick.TimestampNs,
				Equity:      equity,
			})
		}
	}

	// 3. Compute Quantitative Metrics
	report := CalculateMetrics(e.cfg.InitialBalance, trades, equityCurve)
	fmt.Printf("\n[Funnel Diagnostics] Closed Candles: %d | Raw Signals: %d | Gate 1 Macro Vetoes: %d | AI Rejections: %d | Risk Rejections: %d | Executed: %d\n",
		candlesClosed, signalsGenerated, macroRejections, aiRejections, riskRejections, orderSeq)
	return &report, trades, nil
}
