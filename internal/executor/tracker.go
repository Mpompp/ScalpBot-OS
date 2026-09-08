package executor

import (
	"math"
	"sync"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// PositionState represents the lifecycle state of a tracked position.
type PositionState int8

const (
	// PosOpen indicates an actively held position.
	PosOpen PositionState = iota
	// PosModified indicates SL/TP was adjusted (trailing stop, break-even, partial close).
	PosModified
	// PosClosed indicates the position has been closed.
	PosClosed
)

// String returns the human-readable name of the position state.
func (s PositionState) String() string {
	switch s {
	case PosOpen:
		return "OPEN"
	case PosModified:
		return "MODIFIED"
	case PosClosed:
		return "CLOSED"
	default:
		return "UNKNOWN"
	}
}

// TrailingMode determines how trailing stop distance is calculated.
type TrailingMode int8

const (
	// TrailingFixed uses a fixed pip distance for trailing stop.
	TrailingFixed TrailingMode = iota
	// TrailingATR uses ATR multiplier for adaptive trailing stop.
	TrailingATR
)

// TrackerConfig holds configuration for position tracking behavior.
type TrackerConfig struct {
	TrailingMode       TrailingMode  // Fixed pips or ATR-based trailing
	TrailingStopPips   float64       // Distance in pips (for TrailingFixed)
	TrailingATRMult    float64       // ATR multiplier (for TrailingATR, e.g. 1.5)
	BreakEvenPips      float64       // Profit threshold in pips to trigger break-even
	BreakEvenBuffPips  float64       // Buffer above entry for break-even SL (in pips)
	EnableTrailing     bool          // Master switch for trailing stop
	EnableBreakEven    bool          // Master switch for break-even
	EnableProfitLocker bool          // Master switch for 3-Stage Dynamic Profit Locker
	Stage1ATRMult      float64       // Stage 1 (Break-Even): priceGain >= mult * ATR (default 1.0)
	Stage2ATRMult      float64       // Stage 2 (50% Profit Lock): priceGain >= mult * ATR (default 1.8)
	Stage3ATRMult      float64       // Stage 3 (75% Profit Lock): priceGain >= mult * ATR (default 2.5)
	EnablePartialTP    bool          // Enable multi-stage partial take profit
	PartialTPRatio     float64       // Fraction of lot to close at TP1 (e.g. 0.5 = 50%)
	TP1Pips            float64       // Profit target in pips for Partial TP1
	TP2Pips            float64       // Profit target in pips for Final TP2
	TimeStopDuration   time.Duration // Max holding time before emergency exit (0 = disabled)
	AutoRemoveOnClose  bool          // If true, automatically removes from tracker on local SL/TP hit (set false for broker-managed positions like MT5)
}

// DefaultTrackerConfig returns sensible defaults.
func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		TrailingMode:      TrailingFixed,
		TrailingStopPips:  10.0,
		TrailingATRMult:   1.5,
		BreakEvenPips:     5.0,
		BreakEvenBuffPips: 1.0,
		EnableTrailing:    true,
		EnableBreakEven:   true,
		EnableProfitLocker: true,
		Stage1ATRMult:      1.0,
		Stage2ATRMult:      1.8,
		Stage3ATRMult:      2.5,
		EnablePartialTP:   false,
		PartialTPRatio:    0.5,
		TP1Pips:           5.0,
		TP2Pips:           15.0,
		TimeStopDuration:  0, // Disabled by default
		AutoRemoveOnClose: true,
	}
}

// TrackedPosition extends model.Position with trade management state.
// All fields are value types to minimize heap pressure during OnTick.
type TrackedPosition struct {
	model.Position                    // Embedded base position
	State              PositionState  // Current lifecycle state
	StopLoss           float64        // Current stop-loss price
	TakeProfit         float64        // Current take-profit price
	HighWaterMark      float64        // Highest unrealized P&L (for trailing)
	BreakEvenTriggered bool           // Whether break-even has been applied
	PartialTPTriggered bool           // Whether Partial TP1 has been executed
	ProfitStage        int            // Multi-Stage Profit Locker (0=None, 1=BEP, 2=50% Lock, 3=75% Lock)
	RemainingLots      float64        // Remaining active volume
	OpenTimeNs         int64          // Open timestamp in nanoseconds
	LastUpdateNs       int64          // Last update timestamp in nanoseconds
	TrackedAt          time.Time      // Local timestamp when tracking started
	MaxFavorableUSD    float64        // Highest unrealized profit in USD (MFE)
	MaxAdverseUSD      float64        // Deepest unrealized drawdown in USD (MAE)
	MaxFavorablePips   float64        // Highest unrealized profit in pips (MFE)
	MaxAdversePips     float64        // Deepest unrealized drawdown in pips (MAE)
}

// OpenDuration returns the elapsed duration since the position was opened/tracked.
func (p *TrackedPosition) OpenDuration() time.Duration {
	if !p.TrackedAt.IsZero() {
		return time.Since(p.TrackedAt)
	}
	return time.Since(time.Unix(0, p.OpenTimeNs))
}

// ModifyEvent represents an active position whose StopLoss or TakeProfit has been modified.
type ModifyEvent struct {
	OrderID    string
	Symbol     string
	StopLoss   float64
	TakeProfit float64
}

// PositionTracker manages active positions with trailing stop, break-even,
// partial take profit, and time-stop functionality.
// Designed for zero-allocation OnTick processing. Thread-safe via sync.RWMutex.
type PositionTracker struct {
	positions     map[string]*TrackedPosition // keyed by OrderID
	modifications []ModifyEvent
	cfg           TrackerConfig
	mu            sync.RWMutex
}

// NewPositionTracker creates a tracker with the given configuration.
func NewPositionTracker(cfg TrackerConfig) *PositionTracker {
	if cfg.PartialTPRatio <= 0 || cfg.PartialTPRatio >= 1.0 {
		cfg.PartialTPRatio = 0.5
	}
	return &PositionTracker{
		positions: make(map[string]*TrackedPosition, 16),
		cfg:       cfg,
	}
}

// Add registers a newly filled position for tracking.
func (pt *PositionTracker) Add(pos model.Position, sl, tp float64) {
	pt.mu.Lock()
	now := time.Now()
	openTime := pos.OpenTimeNs
	if openTime == 0 {
		openTime = now.UnixNano()
	}
	pos.Symbol = normalizeSymbol(pos.Symbol)
	pt.positions[pos.OrderID] = &TrackedPosition{
		Position:      pos,
		State:         PosOpen,
		StopLoss:      sl,
		TakeProfit:    tp,
		RemainingLots: pos.Lots,
		OpenTimeNs:    openTime,
		LastUpdateNs:  now.UnixNano(),
		TrackedAt:     now,
	}
	pt.mu.Unlock()
}

// Has returns true if the position is already tracked.
func (pt *PositionTracker) Has(orderID string) bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	_, exists := pt.positions[orderID]
	return exists
}

// HasSymbol returns true if there is already an active position for the given symbol.
func (pt *PositionTracker) HasSymbol(symbol string) bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	cleanTarget := normalizeSymbol(symbol)
	for _, p := range pt.positions {
		if normalizeSymbol(p.Symbol) == cleanTarget {
			return true
		}
	}
	return false
}

// FindBySymbol finds the first active tracked position for the given symbol.
func (pt *PositionTracker) FindBySymbol(symbol string) (*TrackedPosition, bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	cleanTarget := normalizeSymbol(symbol)
	for _, p := range pt.positions {
		if normalizeSymbol(p.Symbol) == cleanTarget {
			cp := *p
			return &cp, true
		}
	}
	return nil, false
}

// ReplaceTicket re-keys a tracked position from oldID to newID (for MT5 Deal vs Position ID aliasing).
func (pt *PositionTracker) ReplaceTicket(oldID, newID string) {
	if oldID == newID || oldID == "" || newID == "" {
		return
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if p, exists := pt.positions[oldID]; exists {
		delete(pt.positions, oldID)
		p.OrderID = newID
		pt.positions[newID] = p
	}
}

// Get returns a copy of the tracked position if it exists.
func (pt *PositionTracker) Get(orderID string) (*TrackedPosition, bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	p, exists := pt.positions[orderID]
	if !exists {
		return nil, false
	}
	cp := *p
	return &cp, true
}

// Remove deletes a closed position from tracking.
func (pt *PositionTracker) Remove(orderID string) {
	pt.mu.Lock()
	delete(pt.positions, orderID)
	pt.mu.Unlock()
}

// ActiveOrderIDs returns all tracked order IDs.
func (pt *PositionTracker) ActiveOrderIDs() []string {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	ids := make([]string, 0, len(pt.positions))
	for id := range pt.positions {
		ids = append(ids, id)
	}
	return ids
}

// DrainModifications returns and clears all pending SL/TP modification events.
func (pt *PositionTracker) DrainModifications() []ModifyEvent {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if len(pt.modifications) == 0 {
		return nil
	}
	mods := pt.modifications
	pt.modifications = nil
	return mods
}

// UpdateSLTP updates the StopLoss and TakeProfit of a tracked position.
func (pt *PositionTracker) UpdateSLTP(orderID string, sl, tp float64) {
	pt.mu.Lock()
	if p, ok := pt.positions[orderID]; ok {
		if sl > 0 {
			p.StopLoss = sl
		}
		if tp > 0 {
			p.TakeProfit = tp
		}
	}
	pt.mu.Unlock()
}

// UpdatePnL updates the floating P&L of a tracked position.
func (pt *PositionTracker) UpdatePnL(orderID string, pnl float64) {
	pt.mu.Lock()
	if p, ok := pt.positions[orderID]; ok {
		p.CurrentPnL = pnl
		if pnl > p.MaxFavorableUSD {
			p.MaxFavorableUSD = pnl
		}
		if pnl < p.MaxAdverseUSD {
			p.MaxAdverseUSD = pnl
		}
	}
	pt.mu.Unlock()
}

// CloseEvent represents a position (or portion) that should be closed by the executor.
type CloseEvent struct {
	OrderID    string
	Reason     string          // "SL_HIT", "TP_HIT", "TP2_HIT", "PARTIAL_TP1_HIT", "TIME_STOP_HIT"
	IsPartial  bool            // True if only partial volume should be closed
	Lots       float64         // Volume to close
	PnL        float64         // Realized PnL for the closed volume
	Side       model.OrderSide // Order side (BUY/SELL)
	EntryPrice float64         // Open entry price
	ExitPrice  float64         // Current exit price
	Pips             float64         // Pips gained/lost
	Duration         time.Duration   // Holding duration
	MaxFavorableUSD  float64         // Highest unrealized profit in USD (MFE)
	MaxAdverseUSD    float64         // Deepest unrealized drawdown in USD (MAE)
	MaxFavorablePips float64         // Highest unrealized profit in pips (MFE)
	MaxAdversePips   float64         // Deepest unrealized drawdown in pips (MAE)
}

// OnTick updates all tracked positions with current market price.
// This is called on every tick — zero allocation on hot-path.
// Returns a slice of close events (full or partial).
func (pt *PositionTracker) OnTick(tick model.Tick, atrValue float64, pipMult float64) []CloseEvent {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	var closes []CloseEvent

	nowNs := tick.TimestampNs
	if nowNs == 0 {
		nowNs = time.Now().UnixNano()
	}

	normTickSym := normalizeSymbol(tick.Symbol)
	for _, tp := range pt.positions {
		if tp.Symbol != normTickSym {
			continue
		}

		// Calculate unrealized P&L in pips
		var pnlPips float64
		if tp.Side == model.SideBuy {
			pnlPips = (tick.Bid - tp.EntryPrice) * pipMult
		} else {
			pnlPips = (tp.EntryPrice - tick.Ask) * pipMult
		}

		// Update P&L on position
		pipVal := model.PipValue(tp.Symbol, tp.RemainingLots)
		tp.CurrentPnL = pnlPips * pipVal // convert pips to currency
		tp.LastUpdateNs = tick.TimestampNs

		// Track MFE and MAE (Peak Profit & Deepest Drawdown)
		if tp.CurrentPnL > tp.MaxFavorableUSD {
			tp.MaxFavorableUSD = tp.CurrentPnL
		}
		if tp.CurrentPnL < tp.MaxAdverseUSD {
			tp.MaxAdverseUSD = tp.CurrentPnL
		}
		if pnlPips > tp.MaxFavorablePips {
			tp.MaxFavorablePips = pnlPips
		}
		if pnlPips < tp.MaxAdversePips {
			tp.MaxAdversePips = pnlPips
		}

		// --- 1. Dynamic Time-Stop Check ---
		if pt.cfg.TimeStopDuration > 0 && tick.TimestampNs > 0 && tp.OpenTimeNs > 0 {
			elapsed := time.Duration(tick.TimestampNs - tp.OpenTimeNs)
			if elapsed >= pt.cfg.TimeStopDuration {
				tp.State = PosClosed
				closes = append(closes, CloseEvent{
					OrderID:          tp.OrderID,
					Reason:           "TIME_STOP_HIT",
					IsPartial:        false,
					Lots:             tp.RemainingLots,
					PnL:              tp.CurrentPnL,
					MaxFavorableUSD:  tp.MaxFavorableUSD,
					MaxAdverseUSD:    tp.MaxAdverseUSD,
					MaxFavorablePips: tp.MaxFavorablePips,
					MaxAdversePips:   tp.MaxAdversePips,
				})
				continue
			}
		}

		// --- 2. Multi-Stage Partial Take Profit (TP1) ---
		if pt.cfg.EnablePartialTP && !tp.PartialTPTriggered && pt.cfg.TP1Pips > 0 {
			prof := model.DetectAssetClass(tp.Symbol)
			tp1PipsRequired := pt.cfg.TP1Pips
			if prof.IsGold && tp1PipsRequired < 500.0 {
				tp1PipsRequired = 500.0 // $5.00 minimum breathing space on Gold
			}
			if pnlPips >= tp1PipsRequired {
				closeLots := math.Floor((tp.Lots*pt.cfg.PartialTPRatio)*100) / 100
				if closeLots < 0.01 {
					// Fallback to Break-Even Lock + Full Runner Mode (only after $5.00 gain)
					tp.PartialTPTriggered = true
					bufferPrice := pt.cfg.BreakEvenBuffPips / pipMult
					if prof.IsGold {
						bufferPrice = math.Max(bufferPrice, 0.20)
					}
					if tp.Side == model.SideBuy {
						newSL := tp.EntryPrice + bufferPrice
						if newSL > tp.StopLoss {
							tp.StopLoss = newSL
						}
					} else {
						newSL := tp.EntryPrice - bufferPrice
						if tp.StopLoss == 0 || newSL < tp.StopLoss {
							tp.StopLoss = newSL
						}
					}
					tp.BreakEvenTriggered = true
					tp.ProfitStage = 1
					tp.State = PosModified
					pt.modifications = append(pt.modifications, ModifyEvent{
						OrderID:    tp.OrderID,
						Symbol:     tp.Symbol,
						StopLoss:   tp.StopLoss,
						TakeProfit: tp.TakeProfit,
					})
				} else if closeLots < tp.RemainingLots {
					tp.RemainingLots -= closeLots
					tp.PartialTPTriggered = true

					// Automatically move Stop Loss to Break-Even + Buffer
					bufferPrice := pt.cfg.BreakEvenBuffPips / pipMult
					if tp.Side == model.SideBuy {
						newSL := tp.EntryPrice + bufferPrice
						if newSL > tp.StopLoss {
							tp.StopLoss = newSL
						}
					} else {
						newSL := tp.EntryPrice - bufferPrice
						if tp.StopLoss == 0 || newSL < tp.StopLoss {
							tp.StopLoss = newSL
						}
					}
					tp.BreakEvenTriggered = true
					tp.State = PosModified
					pt.modifications = append(pt.modifications, ModifyEvent{
						OrderID:    tp.OrderID,
						Symbol:     tp.Symbol,
						StopLoss:   tp.StopLoss,
						TakeProfit: tp.TakeProfit,
					})

					// Realized PnL portion
					realizedPnL := (closeLots / tp.Lots) * tp.CurrentPnL

					closes = append(closes, CloseEvent{
						OrderID:    tp.OrderID,
						Reason:     "PARTIAL_TP1_HIT",
						IsPartial:  true,
						Lots:       closeLots,
						PnL:        realizedPnL,
						Side:       tp.Side,
						EntryPrice: tp.EntryPrice,
						ExitPrice:        func() float64 { if tp.Side == model.SideSell { return tick.Ask }; return tick.Bid }(),
						Pips:             pnlPips,
						Duration:         time.Duration(nowNs - tp.OpenTimeNs),
						MaxFavorableUSD:  tp.MaxFavorableUSD,
						MaxAdverseUSD:    tp.MaxAdverseUSD,
						MaxFavorablePips: tp.MaxFavorablePips,
						MaxAdversePips:   tp.MaxAdversePips,
					})
				}
			}
		}

		// --- 3. Multi-Stage Dynamic Profit Locker & Break-Even ---
		prof := model.DetectAssetClass(tp.Symbol)
		var priceGain float64
		if tp.Side == model.SideBuy {
			priceGain = tick.Bid - tp.EntryPrice
		} else {
			priceGain = tp.EntryPrice - tick.Ask
		}

		if pt.cfg.EnableProfitLocker && prof.IsGold {
			// Stage 3: Lock 75% Profit when priceGain >= 2.8x ATR (floor $10.50)
			stage3Threshold := math.Max(atrValue*pt.cfg.Stage3ATRMult, 10.50)
			// Stage 2: Lock 50% Profit when priceGain >= 2.0x ATR (floor $7.50)
			stage2Threshold := math.Max(atrValue*pt.cfg.Stage2ATRMult, 7.50)
			// Stage 1: Break-Even Buffer when priceGain >= 1.5x ATR (floor $5.00)
			stage1Threshold := math.Max(atrValue*pt.cfg.Stage1ATRMult, 5.00)

			if priceGain >= stage3Threshold && tp.ProfitStage < 3 {
				lockedGain := priceGain * 0.75
				if lockedGain < 7.50 {
					lockedGain = 7.50
				}
				var newSL float64
				if tp.Side == model.SideBuy {
					newSL = tp.EntryPrice + lockedGain
				} else {
					newSL = tp.EntryPrice - lockedGain
				}

				shouldUpdate := false
				if tp.Side == model.SideBuy {
					if newSL > tp.StopLoss+0.25 {
						shouldUpdate = true
					}
				} else {
					if tp.StopLoss == 0 || newSL < tp.StopLoss-0.25 {
						shouldUpdate = true
					}
				}

				if shouldUpdate {
					tp.StopLoss = newSL
					tp.ProfitStage = 3
					tp.BreakEvenTriggered = true
					tp.State = PosModified
					pt.modifications = append(pt.modifications, ModifyEvent{
						OrderID:    tp.OrderID,
						Symbol:     tp.Symbol,
						StopLoss:   tp.StopLoss,
						TakeProfit: tp.TakeProfit,
					})
				}
			} else if priceGain >= stage2Threshold && tp.ProfitStage < 2 {
				lockedGain := priceGain * 0.50
				if lockedGain < 3.50 {
					lockedGain = 3.50
				}
				var newSL float64
				if tp.Side == model.SideBuy {
					newSL = tp.EntryPrice + lockedGain
				} else {
					newSL = tp.EntryPrice - lockedGain
				}

				shouldUpdate := false
				if tp.Side == model.SideBuy {
					if newSL > tp.StopLoss+0.25 {
						shouldUpdate = true
					}
				} else {
					if tp.StopLoss == 0 || newSL < tp.StopLoss-0.25 {
						shouldUpdate = true
					}
				}

				if shouldUpdate {
					tp.StopLoss = newSL
					tp.ProfitStage = 2
					tp.BreakEvenTriggered = true
					tp.State = PosModified
					pt.modifications = append(pt.modifications, ModifyEvent{
						OrderID:    tp.OrderID,
						Symbol:     tp.Symbol,
						StopLoss:   tp.StopLoss,
						TakeProfit: tp.TakeProfit,
					})
				}
			} else if priceGain >= stage1Threshold && tp.ProfitStage < 1 {
				bufferPrice := math.Max(pt.cfg.BreakEvenBuffPips/pipMult, 0.20)
				var newSL float64
				if tp.Side == model.SideBuy {
					newSL = tp.EntryPrice + bufferPrice
				} else {
					newSL = tp.EntryPrice - bufferPrice
				}

				shouldUpdate := false
				if tp.Side == model.SideBuy {
					if newSL > tp.StopLoss+0.15 {
						shouldUpdate = true
					}
				} else {
					if tp.StopLoss == 0 || newSL < tp.StopLoss-0.15 {
						shouldUpdate = true
					}
				}

				if shouldUpdate {
					tp.StopLoss = newSL
					tp.ProfitStage = 1
					tp.BreakEvenTriggered = true
					tp.State = PosModified
					pt.modifications = append(pt.modifications, ModifyEvent{
						OrderID:    tp.OrderID,
						Symbol:     tp.Symbol,
						StopLoss:   tp.StopLoss,
						TakeProfit: tp.TakeProfit,
					})
				}
			}
		} else if pt.cfg.EnableBreakEven && !tp.BreakEvenTriggered {
			bePipsRequired := pt.cfg.BreakEvenPips
			if prof.IsGold {
				// Gold requires at least $5.00 profit before locking BEP (never choke on tick noise)
				minGoldBEPPips := math.Max(atrValue*100.0*1.5, 500.0)
				if bePipsRequired < minGoldBEPPips {
					bePipsRequired = minGoldBEPPips
				}
			}

			if pnlPips >= bePipsRequired {
				bufferPrice := pt.cfg.BreakEvenBuffPips / pipMult
				if prof.IsGold {
					bufferPrice = math.Max(bufferPrice, 0.15) // $0.15 buffer for Gold
				}
				if tp.Side == model.SideBuy {
					newSL := tp.EntryPrice + bufferPrice
					if newSL > tp.StopLoss {
						tp.StopLoss = newSL
					}
				} else {
					newSL := tp.EntryPrice - bufferPrice
					if tp.StopLoss == 0 || newSL < tp.StopLoss {
						tp.StopLoss = newSL
					}
				}
				tp.BreakEvenTriggered = true
				tp.ProfitStage = 1
				tp.State = PosModified
				pt.modifications = append(pt.modifications, ModifyEvent{
					OrderID:    tp.OrderID,
					Symbol:     tp.Symbol,
					StopLoss:   tp.StopLoss,
					TakeProfit: tp.TakeProfit,
				})
			}
		}

		// --- 4. Trailing Stop Check (Asset-Aware & Volatility-Proportional) ---
		if pt.cfg.EnableTrailing {
			if pnlPips > tp.HighWaterMark {
				tp.HighWaterMark = pnlPips

				prof := model.DetectAssetClass(tp.Symbol)
				// Calculate trailing distance in price units
				var trailDist float64
				if pt.cfg.TrailingMode == TrailingATR && atrValue > 0 {
					trailDist = atrValue * pt.cfg.TrailingATRMult
				} else {
					trailDist = pt.cfg.TrailingStopPips / pipMult
				}

				// Gold requires at least 1.2x ATR or $1.50 trailing room to reach full 2.0R target
				if prof.IsGold {
					minGoldTrail := math.Max(atrValue*1.2, 1.50)
					if trailDist < minGoldTrail {
						trailDist = minGoldTrail
					}
				}

				minStep := 0.25
				if !prof.IsGold {
					minStep = 5.0 / pipMult
				}

				if tp.Side == model.SideBuy {
					newSL := tick.Bid - trailDist
					if newSL > tp.StopLoss+minStep {
						tp.StopLoss = newSL
						tp.State = PosModified
						pt.modifications = append(pt.modifications, ModifyEvent{
							OrderID:    tp.OrderID,
							Symbol:     tp.Symbol,
							StopLoss:   tp.StopLoss,
							TakeProfit: tp.TakeProfit,
						})
					}
				} else {
					newSL := tick.Ask + trailDist
					if tp.StopLoss != 0 && newSL < tp.StopLoss-minStep {
						tp.StopLoss = newSL
						tp.State = PosModified
						pt.modifications = append(pt.modifications, ModifyEvent{
							OrderID:    tp.OrderID,
							Symbol:     tp.Symbol,
							StopLoss:   tp.StopLoss,
							TakeProfit: tp.TakeProfit,
						})
					}
				}
			}
		}

		// --- 5. Stop Loss Hit Check ---
		if tp.StopLoss > 0 {
			hit := false
			if tp.Side == model.SideBuy && tick.Bid <= tp.StopLoss {
				hit = true
			} else if tp.Side == model.SideSell && tick.Ask >= tp.StopLoss {
				hit = true
			}
			if hit {
				tp.State = PosClosed
				exitPr := tick.Bid
				if tp.Side == model.SideSell {
					exitPr = tick.Ask
				}
				closes = append(closes, CloseEvent{
					OrderID:          tp.OrderID,
					Reason:           "SL_HIT",
					IsPartial:        false,
					Lots:             tp.RemainingLots,
					PnL:              tp.CurrentPnL,
					Side:             tp.Side,
					EntryPrice:       tp.EntryPrice,
					ExitPrice:        exitPr,
					Pips:             pnlPips,
					Duration:         time.Duration(nowNs - tp.OpenTimeNs),
					MaxFavorableUSD:  tp.MaxFavorableUSD,
					MaxAdverseUSD:    tp.MaxAdverseUSD,
					MaxFavorablePips: tp.MaxFavorablePips,
					MaxAdversePips:   tp.MaxAdversePips,
				})
				continue
			}
		}

		// --- 6. Primary Take Profit Target Check ---
		if tp.TakeProfit > 0 {
			hit := false
			if tp.Side == model.SideBuy && tick.Bid >= tp.TakeProfit {
				hit = true
			} else if tp.Side == model.SideSell && tick.Ask <= tp.TakeProfit {
				hit = true
			}
			if hit {
				tp.State = PosClosed
				exitPr := tick.Bid
				if tp.Side == model.SideSell {
					exitPr = tick.Ask
				}
				closes = append(closes, CloseEvent{
					OrderID:          tp.OrderID,
					Reason:           "TP_HIT",
					IsPartial:        false,
					Lots:             tp.RemainingLots,
					PnL:              tp.CurrentPnL,
					Side:             tp.Side,
					EntryPrice:       tp.EntryPrice,
					ExitPrice:        exitPr,
					Pips:             pnlPips,
					Duration:         time.Duration(nowNs - tp.OpenTimeNs),
					MaxFavorableUSD:  tp.MaxFavorableUSD,
					MaxAdverseUSD:    tp.MaxAdverseUSD,
					MaxFavorablePips: tp.MaxFavorablePips,
					MaxAdversePips:   tp.MaxAdversePips,
				})
				continue
			}
		} else if pt.cfg.TP2Pips > 0 {
			// Fallback fixed TP2 check if no structural TP was assigned
			prof := model.DetectAssetClass(tp.Symbol)
			tp2Target := pt.cfg.TP2Pips
			if prof.IsGold && tp2Target < 200.0 {
				tp2Target = 200.0 // Minimum $2.00 target for Gold (200 pips)
			}
			if pnlPips >= tp2Target {
				tp.State = PosClosed
				exitPr := tick.Bid
				if tp.Side == model.SideSell {
					exitPr = tick.Ask
				}
				closes = append(closes, CloseEvent{
					OrderID:          tp.OrderID,
					Reason:           "TP2_HIT",
					IsPartial:        false,
					Lots:             tp.RemainingLots,
					PnL:              tp.CurrentPnL,
					Side:             tp.Side,
					EntryPrice:       tp.EntryPrice,
					ExitPrice:        exitPr,
					Pips:             pnlPips,
					Duration:         time.Duration(nowNs - tp.OpenTimeNs),
					MaxFavorableUSD:  tp.MaxFavorableUSD,
					MaxAdverseUSD:    tp.MaxAdverseUSD,
					MaxFavorablePips: tp.MaxFavorablePips,
					MaxAdversePips:   tp.MaxAdversePips,
				})
				continue
			}
		}
	}

	// Remove only fully closed positions if AutoRemoveOnClose is enabled
	if pt.cfg.AutoRemoveOnClose {
		for _, ce := range closes {
			if !ce.IsPartial {
				delete(pt.positions, ce.OrderID)
			}
		}
	}

	return closes
}

// Count returns the number of actively tracked positions.
func (pt *PositionTracker) Count() int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return len(pt.positions)
}

// TotalPnL returns the sum of unrealized floating PnL across all tracked positions.
func (pt *PositionTracker) TotalPnL() float64 {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	total := 0.0
	for _, p := range pt.positions {
		total += p.CurrentPnL
	}
	return total
}

// ActivePositions returns a snapshot of all tracked positions.
func (pt *PositionTracker) ActivePositions() []TrackedPosition {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	result := make([]TrackedPosition, 0, len(pt.positions))
	for _, p := range pt.positions {
		result = append(result, *p)
	}
	return result
}

// normalizeSymbol delegates to model.NormalizeSymbol for canonical symbol comparison.
func normalizeSymbol(sym string) string {
	return model.NormalizeSymbol(sym)
}
