package backtest

import (
	"fmt"
	"time"

	"github.com/pompbot/scalpbot/internal/ai"
	"github.com/pompbot/scalpbot/internal/executor"
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
	RiskConfig     risk.ManagerConfig
	TrackerConfig  executor.TrackerConfig
	AIConfig       ai.FilterConfig
}

// DefaultEngineConfig returns standard backtest configuration for EURUSD scalping.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		InitialBalance:   10000.0,
		Symbol:           "EURUSD",
		CandlePeriod:     5 * time.Second,
		SlippagePips:     0.1,
		CommissionPerLot: 3.50,
		StrategyConfig: strategy.MomentumScalperConfig{
			FastEMAPeriod: 5,
			SlowEMAPeriod: 13,
			RSIPeriod:     14,
			ATRPeriod:     14,
			RSIOverbought: 70.0,
			RSIOversold:   30.0,
			ATRMinimum:    0.0003,
			Symbol:        "EURUSD",
		},
		RiskConfig: risk.ManagerConfig{
			InitialEquity:    10000.0,
			RiskPerTrade:     0.01,
			MaxDailyDrawdown: 0.05,
			MaxSpreadPips:    2.0,
			MaxOpenPositions: 3,
			MinLotSize:       0.01,
			MaxLotSize:       1.0,
		},
		TrackerConfig: executor.TrackerConfig{
			TrailingMode:      executor.TrailingFixed,
			TrailingStopPips:  10.0,
			TrailingATRMult:   1.5,
			BreakEvenPips:     5.0,
			BreakEvenBuffPips: 1.0,
			EnableTrailing:    true,
			EnableBreakEven:   true,
			EnablePartialTP:   true,
			PartialTPRatio:    0.5,
			TP1Pips:           5.0,
			TP2Pips:           15.0,
			TimeStopDuration:  15 * time.Minute,
		},
		AIConfig: ai.FilterConfig{
			EnableMLFilter:    true,
			MinConfidence:     0.65,
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
	riskMgr := risk.NewManager(e.cfg.RiskConfig)
	tracker := executor.NewPositionTracker(e.cfg.TrackerConfig)
	aggregator := marketdata.NewOHLCVAggregator(e.cfg.CandlePeriod)
	aiFilter := ai.NewSignalFilter(e.cfg.AIConfig)

	// State trackers
	activePositions := make(map[string]SimulatedPosition, 16)
	trades := make([]TradeRecord, 0, 1024)
	equityCurve := make([]EquityPoint, 0, len(ticks)/100+128)

	currentBalance := e.cfg.InitialBalance
	orderSeq := 0

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
			riskMgr.RecordClose(netPnL)

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

		if candle, closed := aggregator.OnTick(tick); closed {
			cSig := strat.OnCandle(candle)
			if cSig.IsActionable() {
				sig = cSig
			}
		}

		// --- C. Evaluate AI Model & Risk Rules ---
		if sig.IsActionable() {
			// AI Filter Check
			fastEMA := strat.FastEMA()
			slowEMA := strat.SlowEMA()
			rsiVal := strat.RSI()
			atrCur := strat.ATR()
			if atrCur <= 0 {
				atrCur = atrVal
			}

			aiAllowed, _, _, _ := aiFilter.EvaluateSignal(sig, tick, fastEMA, slowEMA, rsiVal, atrCur, 5.0, model.Candle{})
			if !aiAllowed {
				continue // Skip signal rejected by AI
			}

			// Risk Engine Check
			order, err := riskMgr.Evaluate(sig, tick, atrVal)
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
				tracker.Add(pos, 0, 0)
				riskMgr.RecordFill()
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
	return &report, trades, nil
}
