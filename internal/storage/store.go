package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TradeRecord stores permanent execution history for a completed scalping trade.
type TradeRecord struct {
	Ticket    string        `json:"ticket"`
	Symbol    string        `json:"symbol"`
	Side      string        `json:"side"`
	Lots      float64       `json:"lots"`
	Entry     float64       `json:"entry"`
	Exit      float64       `json:"exit"`
	NetPnL    float64       `json:"net_pnl"`
	Pips      float64       `json:"pips"`
	Duration  time.Duration `json:"duration"`
	CloseTime time.Time     `json:"close_time"`
	MFEUSD    float64       `json:"mfe_usd,omitempty"`
	MAEUSD    float64       `json:"mae_usd,omitempty"`
	MFEPips   float64       `json:"mfe_pips,omitempty"`
	MAEPips   float64       `json:"mae_pips,omitempty"`
}

// DailyState stores today's realized profit, target progress, and circuit status across bot restarts.
type DailyState struct {
	Date                string    `json:"date"` // YYYY-MM-DD
	DailyPnL            float64   `json:"daily_pnl"`
	ProfitTargetReached bool      `json:"profit_target_reached"`
	CircuitOpen         bool      `json:"circuit_open"`
	ActiveLotSize       float64   `json:"active_lot_size,omitempty"`
	LastUpdated         time.Time `json:"last_updated"`
}

// Store handles thread-safe disk persistence for trades and daily risk state.
type Store struct {
	dataDir    string
	tradesFile string
	stateFile  string
	mu         sync.RWMutex
}

// NewStore creates a new storage instance in the specified data directory.
func NewStore(dataDir string) *Store {
	if dataDir == "" {
		dataDir = "data"
	}
	_ = os.MkdirAll(dataDir, 0755)
	return &Store{
		dataDir:    dataDir,
		tradesFile: filepath.Join(dataDir, "trade_history.json"),
		stateFile:  filepath.Join(dataDir, "daily_state.json"),
	}
}

// LoadTrades loads all saved completed trades from disk, keeping only the last 7 days.
func (s *Store) LoadTrades() ([]TradeRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.tradesFile)
	if os.IsNotExist(err) {
		return []TradeRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	var trades []TradeRecord
	if err := json.Unmarshal(data, &trades); err != nil {
		return []TradeRecord{}, nil
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	var recent []TradeRecord
	for _, t := range trades {
		if t.CloseTime.After(cutoff) {
			recent = append(recent, t)
		}
	}
	return recent, nil
}

// SaveTrade appends a single completed trade and persists to disk.
func (s *Store) SaveTrade(tr TradeRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var trades []TradeRecord
	data, err := os.ReadFile(s.tradesFile)
	if err == nil {
		_ = json.Unmarshal(data, &trades)
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	var pruned []TradeRecord
	for _, existing := range trades {
		if existing.Ticket == tr.Ticket {
			return nil
		}
		if existing.CloseTime.After(cutoff) {
			pruned = append(pruned, existing)
		}
	}

	pruned = append(pruned, tr)
	if len(pruned) > 500 {
		pruned = pruned[len(pruned)-500:]
	}

	marshaled, err := json.MarshalIndent(pruned, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.tradesFile, marshaled, 0644)
}

// LoadDailyState loads the daily profit and risk state for today.
func (s *Store) LoadDailyState() (DailyState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	today := time.Now().Format("2006-01-02")
	data, err := os.ReadFile(s.stateFile)
	if os.IsNotExist(err) {
		return DailyState{Date: today}, nil
	}
	if err != nil {
		return DailyState{Date: today}, err
	}
	var state DailyState
	if err := json.Unmarshal(data, &state); err != nil {
		return DailyState{Date: today}, nil
	}

	// If calendar date has changed, reset daily state for fresh day
	if state.Date != today {
		return DailyState{Date: today}, nil
	}
	return state, nil
}

// SaveDailyState persists the daily state to disk.
func (s *Store) SaveDailyState(state DailyState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state.LastUpdated = time.Now()
	if state.Date == "" {
		state.Date = time.Now().Format("2006-01-02")
	}

	// Preserve existing active lot size if caller did not explicitly supply one
	if state.ActiveLotSize <= 0 {
		if prev, err := os.ReadFile(s.stateFile); err == nil {
			var prevState DailyState
			if err := json.Unmarshal(prev, &prevState); err == nil && prevState.ActiveLotSize > 0 {
				state.ActiveLotSize = prevState.ActiveLotSize
			}
		}
	}

	marshaled, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.stateFile, marshaled, 0644)
}
