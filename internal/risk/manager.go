// Package risk implements the risk management guardrail that sits between
// strategy signals and order execution. NO signal reaches the executor
// without passing through this filter.
package risk

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// Sentinel errors for risk rejection reasons.
var (
	ErrSpreadTooWide           = errors.New("risk: spread exceeds maximum threshold")
	ErrDailyDrawdownHit        = errors.New("risk: daily drawdown circuit breaker triggered")
	ErrDailyProfitTargetReached = errors.New("risk: daily profit target reached — capital secured (anti-greed mode)")
	ErrMaxPositionsOpen        = errors.New("risk: maximum open positions reached")
	ErrInvalidSignal           = errors.New("risk: signal is not actionable")
	ErrInsufficientATR         = errors.New("risk: ATR too low for lot sizing")
	ErrZeroEquity              = errors.New("risk: equity is zero or negative")
	ErrCircuitOpen             = errors.New("risk: circuit breaker is open — trading halted for the day")
	ErrFilterRejected          = errors.New("risk: market filter rejected")
)

// ManagerConfig holds the configuration for the risk manager.
type ManagerConfig struct {
	InitialEquity         float64 // Starting account equity
	RiskPerTrade          float64 // Risk per trade as fraction of equity (e.g., 0.01 = 1%)
	MaxDailyDrawdown      float64 // Max daily loss as fraction of equity (e.g., 0.05 = 5%)
	DailyProfitTargetPct  float64 // Daily profit target as fraction of equity (e.g. 0.05 = 5% auto-lock)
	DailyProfitTargetUSD  float64 // Daily profit target in nominal account currency (e.g. 50.0 USC)
	MaxSpreadPips         float64 // Maximum allowed spread in pips
	MaxOpenPositions      int     // Maximum concurrent open positions
	MaxPositionsPerSymbol int     // Maximum concurrent open positions per symbol (default 1)
	MinLotSize            float64 // Minimum allowed lot size (e.g., 0.01)
	MaxLotSize            float64 // Maximum lot size per trade (e.g., 1.0)
}

// DefaultManagerConfig returns conservative risk defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		InitialEquity:         10000.0,
		RiskPerTrade:          0.01, // 1% per trade
		MaxDailyDrawdown:      0.05, // 5% daily max drawdown
		DailyProfitTargetPct:  0.05, // 5% daily profit target
		DailyProfitTargetUSD:  0.0,  // 0 = use percentage
		MaxSpreadPips:         60.0, // 60 pips ($0.60) for Gold
		MaxOpenPositions:      1,
		MaxPositionsPerSymbol: 1,
		MinLotSize:            0.01,
		MaxLotSize:            0.05,
	}
}

// Manager is the central risk management component.
// Thread-safe: all mutable state protected by sync.RWMutex.
type Manager struct {
	cfg                  ManagerConfig
	equity               float64 // Current account equity
	dailyPnL             float64 // Accumulated P&L today
	openPositions        int     // Current open position count
	symbolPositions      map[string]int // Position count per clean symbol
	circuitOpen          bool    // True if daily drawdown breaker has tripped
	profitTargetReached  bool    // True if daily profit target has been achieved
	lastResetDay         int     // Day-of-year of last daily reset
	filters              []MarketFilter // Pre-trade market condition filters
	mu                   sync.RWMutex
}

// NewManager creates a risk manager with the given configuration.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.MaxPositionsPerSymbol <= 0 {
		cfg.MaxPositionsPerSymbol = 1
	}
	return &Manager{
		cfg:             cfg,
		equity:          cfg.InitialEquity,
		symbolPositions: make(map[string]int),
	}
}

// Evaluate validates a signal against all risk rules and returns a sized
// OrderRequest if approved. Returns an error describing the rejection reason
// if any guard fails.
//
// This is the ONLY gateway to order execution. Signals that bypass this
// function are a severe architecture violation.
func (m *Manager) Evaluate(signal model.Signal, tick model.Tick, atrValue float64) (model.OrderRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Daily reset check
	m.checkDailyReset()

	empty := model.OrderRequest{}

	// Guard 0: Market filters (session, spread anomaly, news gate)
	for _, f := range m.filters {
		if ok, reason := f.Allow(tick); !ok {
			return empty, fmt.Errorf("%w [%s]: %s", ErrFilterRejected, f.Name(), reason)
		}
	}

	// Guard 1: Signal must be actionable
	if !signal.IsActionable() {
		return empty, ErrInvalidSignal
	}

	// Guard 2: Circuit breaker
	if m.circuitOpen {
		return empty, ErrCircuitOpen
	}

	// Guard 2b: Daily Profit Target Lock (Anti-Greed Capital Security)
	if m.profitTargetReached {
		return empty, ErrDailyProfitTargetReached
	}

	// Guard 3: Daily drawdown check
	drawdownLimit := m.cfg.InitialEquity * m.cfg.MaxDailyDrawdown
	if m.dailyPnL < 0 && math.Abs(m.dailyPnL) >= drawdownLimit {
		m.circuitOpen = true
		return empty, ErrDailyDrawdownHit
	}

	// Guard 3b: Daily profit target trigger
	var targetAmount float64
	if m.cfg.DailyProfitTargetUSD > 0 {
		targetAmount = m.cfg.DailyProfitTargetUSD
	} else if m.cfg.DailyProfitTargetPct > 0 {
		targetAmount = m.cfg.InitialEquity * m.cfg.DailyProfitTargetPct
	}
	if targetAmount > 0 && m.dailyPnL >= targetAmount {
		m.profitTargetReached = true
		return empty, ErrDailyProfitTargetReached
	}

	// Guard 4: Maximum total open positions across portfolio
	if m.openPositions >= m.cfg.MaxOpenPositions {
		return empty, ErrMaxPositionsOpen
	}

	// Guard 4b: Maximum open positions per symbol (Strict Anti-Stacking)
	maxPerSym := m.cfg.MaxPositionsPerSymbol
	if maxPerSym <= 0 {
		maxPerSym = 1
	}
	cleanSym := model.NormalizeSymbol(signal.Symbol)
	if m.symbolPositions != nil && m.symbolPositions[cleanSym] >= maxPerSym {
		return empty, fmt.Errorf("risk rejected: symbol %s already has %d active position (max %d)", signal.Symbol, m.symbolPositions[cleanSym], maxPerSym)
	}

	// Guard 5: Spread guard (Asset-Aware for Forex & Commodities)
	pipMult := model.PipMultiplier(tick.Symbol)
	spreadPips := tick.SpreadPips(pipMult)
	maxSpread := m.cfg.MaxSpreadPips
	symUpper := strings.ToUpper(tick.Symbol)
	if strings.Contains(symUpper, "XAU") || strings.Contains(symUpper, "GOLD") {
		// Gold spread is naturally in cents (e.g. 25-50 cents = 25-50 pips)
		if maxSpread < 60.0 {
			maxSpread = 60.0
		}
	} else if strings.Contains(symUpper, "JPY") {
		if maxSpread < 3.5 {
			maxSpread = 3.5
		}
	}
	if spreadPips > maxSpread {
		return empty, fmt.Errorf("%w: %.2f pips (max %.2f)", ErrSpreadTooWide, spreadPips, maxSpread)
	}

	// Guard 6: Equity check
	if m.equity <= 0 {
		return empty, ErrZeroEquity
	}

	// Dynamic lot sizing: lots = (equity * riskPercent) / (atrPips * pipValue_per_lot)
	lots, err := m.calculateLotSize(tick.Symbol, atrValue, pipMult)
	if err != nil {
		return empty, err
	}

	// Determine order side
	side := model.SideBuy
	if signal.Type == model.Sell {
		side = model.SideSell
	}

	// Determine entry price (buy at ask, sell at bid)
	entryPrice := tick.Ask
	if side == model.SideSell {
		entryPrice = tick.Bid
	}

	order := model.OrderRequest{
		Symbol:      signal.Symbol,
		Side:        side,
		Lots:        lots,
		Price:       entryPrice,
		StopLoss:    signal.StopLoss,
		TakeProfit:  signal.TakeProfit,
		Status:      model.OrderPending,
		SignalID:    signal.StrategyID,
		TimestampNs: time.Now().UnixNano(),
	}

	return order, nil
}

// calculateLotSize determines position size based on risk parameters.
func (m *Manager) calculateLotSize(symbol string, atrValue float64, pipMult float64) (float64, error) {
	if atrValue <= 0 {
		return 0, ErrInsufficientATR
	}

	// Convert ATR to pips
	atrPips := atrValue * pipMult

	// Risk amount in account currency
	riskAmount := m.equity * m.cfg.RiskPerTrade

	// Pip value for 1 standard lot
	pipValuePerLot := model.PipValue(symbol, 1.0)

	// lots = riskAmount / (atrPips * pipValuePerLot)
	lots := riskAmount / (atrPips * pipValuePerLot)

	// Clamp to configured bounds
	if lots < m.cfg.MinLotSize {
		lots = m.cfg.MinLotSize
	}
	if lots > m.cfg.MaxLotSize {
		lots = m.cfg.MaxLotSize
	}

	// Round to 2 decimal places (standard lot precision)
	lots = math.Round(lots*100) / 100

	return lots, nil
}

// RecordFill updates internal state when an order is filled.
func (m *Manager) RecordFill() {
	m.mu.Lock()
	m.openPositions++
	m.mu.Unlock()
}

// RecordFillForSymbol updates internal state when an order for a specific symbol is filled.
func (m *Manager) RecordFillForSymbol(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openPositions++
	if m.symbolPositions == nil {
		m.symbolPositions = make(map[string]int)
	}
	cleanSym := model.NormalizeSymbol(symbol)
	m.symbolPositions[cleanSym]++
}

// RecordClose updates internal state when a position is closed.
// pnl is the realized profit/loss for the position.
func (m *Manager) RecordClose(pnl float64) {
	m.RecordCloseForSymbol("", pnl)
}

// RecordCloseForSymbol updates internal state when a position for a specific symbol is closed.
func (m *Manager) RecordCloseForSymbol(symbol string, pnl float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.openPositions > 0 {
		m.openPositions--
	}
	if symbol != "" && m.symbolPositions != nil {
		cleanSym := model.NormalizeSymbol(symbol)
		if m.symbolPositions[cleanSym] > 0 {
			m.symbolPositions[cleanSym]--
		}
	}
	m.dailyPnL += pnl
	m.equity += pnl

	// Check if circuit breaker should trip
	drawdownLimit := m.cfg.InitialEquity * m.cfg.MaxDailyDrawdown
	if m.dailyPnL < 0 && math.Abs(m.dailyPnL) >= drawdownLimit {
		m.circuitOpen = true
	}

	// Check if daily profit target is reached
	var targetAmount float64
	if m.cfg.DailyProfitTargetUSD > 0 {
		targetAmount = m.cfg.DailyProfitTargetUSD
	} else if m.cfg.DailyProfitTargetPct > 0 {
		targetAmount = m.cfg.InitialEquity * m.cfg.DailyProfitTargetPct
	}
	if targetAmount > 0 && m.dailyPnL >= targetAmount {
		m.profitTargetReached = true
	}
}

// checkDailyReset resets daily PnL tracking at the start of a new trading day.
// Must be called with mu held.
func (m *Manager) checkDailyReset() {
	today := time.Now().YearDay()
	if today != m.lastResetDay {
		m.dailyPnL = 0
		m.circuitOpen = false
		m.profitTargetReached = false
		m.lastResetDay = today
	}
}

// RestoreDailyState restores persisted daily risk manager state across process restarts.
func (m *Manager) RestoreDailyState(dailyPnL float64, targetReached, circuitOpen bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dailyPnL = dailyPnL
	m.profitTargetReached = targetReached
	m.circuitOpen = circuitOpen
	m.lastResetDay = time.Now().YearDay()
}

// IsProfitTargetReached returns true if daily profit target is achieved (thread-safe).
func (m *Manager) IsProfitTargetReached() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.profitTargetReached
}

// SetProfitTarget sets the daily profit target dynamically (thread-safe).
func (m *Manager) SetProfitTarget(pct, usd float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.DailyProfitTargetPct = pct
	m.cfg.DailyProfitTargetUSD = usd
}

// ProfitTargetAmount returns the active nominal daily profit target (thread-safe).
func (m *Manager) ProfitTargetAmount() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg.DailyProfitTargetUSD > 0 {
		return m.cfg.DailyProfitTargetUSD
	}
	if m.cfg.DailyProfitTargetPct > 0 {
		return m.cfg.InitialEquity * m.cfg.DailyProfitTargetPct
	}
	return 0.0
}

// Equity returns the current account equity (thread-safe).
func (m *Manager) Equity() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.equity
}

// UpdateEquity safely updates the internal equity from external sources.
func (m *Manager) UpdateEquity(equity float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if equity > 0 {
		m.equity = equity
	}
}

// DailyPnL returns the accumulated daily P&L (thread-safe).
func (m *Manager) DailyPnL() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dailyPnL
}

// IsCircuitOpen returns true if the daily drawdown breaker has tripped (thread-safe).
func (m *Manager) IsCircuitOpen() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.circuitOpen
}

// OpenPositions returns the current open position count (thread-safe).
func (m *Manager) OpenPositions() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.openPositions
}

// SetOpenPositions synchronizes the open position count directly from external tracker (thread-safe).
func (m *Manager) SetOpenPositions(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openPositions = n
	if n == 0 {
		m.symbolPositions = make(map[string]int)
	}
}

// SyncPositions synchronizes open position counts and per-symbol tracking directly from live active symbols (thread-safe).
func (m *Manager) SyncPositions(activeSymbols []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openPositions = len(activeSymbols)
	m.symbolPositions = make(map[string]int)
	for _, sym := range activeSymbols {
		cleanSym := model.NormalizeSymbol(sym)
		m.symbolPositions[cleanSym]++
	}
}

// SetMaxOpenPositions dynamically updates the maximum allowed open positions (thread-safe).
func (m *Manager) SetMaxOpenPositions(max int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if max > 0 {
		m.cfg.MaxOpenPositions = max
	}
}

// SetMaxLotSize dynamically updates the maximum allowed lot size (thread-safe).
func (m *Manager) SetMaxLotSize(max float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if max > 0 {
		m.cfg.MaxLotSize = max
	}
}

// AddFilter registers a market condition filter.
// Filters are evaluated in order during Evaluate() before risk guards.
// Must be called before the pipeline starts processing signals.
func (m *Manager) AddFilter(f MarketFilter) {
	m.filters = append(m.filters, f)
}

// RiskInvariant enforces SL/TP requirement and fixed-fractional risk (0.5% default, max 1% hardcap).
// This is the ONLY gate that can reject a signal with SL/TP == 0 or risk > 1%.
func (m *Manager) RiskInvariant(signal model.Signal) error {
	if signal.StopLoss == 0 || signal.TakeProfit == 0 {
		return fmt.Errorf("%w: signal must have both StopLoss and TakeProfit", ErrInvalidSignal)
	}
	if signal.Strength > 0.01 { // Strength field used as proxy for riskPct in model.Signal
		return fmt.Errorf("%w: risk per trade cannot exceed 1%% (hardcap)", ErrInvalidSignal)
	}
	return nil
}

// ApplyAccountProfile dynamically adapts risk parameters according to Cent vs Regular account.
func (m *Manager) ApplyAccountProfile(accType model.AccountType) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if accType == model.AccountTypeCent {
		m.cfg.MaxSpreadPips = 60.0 // Wider spread tolerance for broker cent markup ($0.60)
		m.cfg.MaxLotSize = 0.01    // Strict 0.01 lot cap on Cent accounts
		m.cfg.MaxOpenPositions = 1 // Strict 1 position for margin safety
		m.cfg.MaxPositionsPerSymbol = 1
	} else {
		m.cfg.MaxSpreadPips = 35.0 // Tighter spread tolerance for standard/regular accounts ($0.35)
		m.cfg.MaxLotSize = 0.05    // Dynamic sizing allows up to 0.05 lot on regular accounts
		m.cfg.MaxOpenPositions = 2 // Allow up to 2 concurrent swing positions on standard equity
		m.cfg.MaxPositionsPerSymbol = 1
	}
}

// MaxSpreadPips returns the current spread limit in pips (thread-safe).
func (m *Manager) MaxSpreadPips() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.MaxSpreadPips
}

