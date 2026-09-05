// Package mt5 provides a high-performance, low-latency IPC adapter
// connecting Go scalpbot to a MetaTrader 5 (MT5) terminal via TCP sockets.
package mt5

// Action represents the trade or query command sent from Go to MT5.
type Action string

const (
	ActionBuy       Action = "BUY"
	ActionSell      Action = "SELL"
	ActionClose     Action = "CLOSE"
	ActionPositions Action = "POSITIONS"
	ActionAccount   Action = "ACCOUNT"
	ActionHistory   Action = "HISTORY"
	ActionPing      Action = "PING"
)

// TradeRequest is the JSON command payload sent to the MT5 Bridge.
type TradeRequest struct {
	Action      Action  `json:"action"`                 // BUY, SELL, CLOSE, POSITIONS, ACCOUNT, HISTORY, PING
	RequestID   string  `json:"request_id"`             // Correlation ID
	Symbol      string  `json:"symbol,omitempty"`       // Currency pair, e.g. "EURUSD"
	Lots        float64 `json:"lots,omitempty"`         // Volume
	Price       float64 `json:"price,omitempty"`        // Expected entry or limit price
	StopLoss    float64 `json:"stop_loss,omitempty"`    // SL price level
	TakeProfit  float64 `json:"take_profit,omitempty"`  // TP price level
	Slippage    int     `json:"slippage,omitempty"`     // Allowed slippage in points
	MagicNumber int     `json:"magic_number,omitempty"` // EA identifier
	Ticket      string  `json:"ticket,omitempty"`       // Position ticket (for CLOSE)
	Days        float64 `json:"days,omitempty"`         // Days back for HISTORY
}

// TradeResponse is the execution response received from MT5.
type TradeResponse struct {
	RequestID   string           `json:"request_id"`            // Matching RequestID
	Success     bool             `json:"success"`               // True if execution succeeded
	RetCode     int              `json:"retcode"`               // MT5 MqlTradeResult retcode (e.g. 10009 = TRADE_RETCODE_DONE)
	Ticket      string           `json:"ticket,omitempty"`      // Position/Deal ticket
	FillPrice   float64          `json:"fill_price,omitempty"`  // Actual fill price
	Lots        float64          `json:"lots,omitempty"`        // Executed lots
	ErrorMsg    string           `json:"error_msg,omitempty"`   // Description of failure if any
	Positions   []PositionDTO    `json:"positions,omitempty"`   // For POSITIONS action
	Account     *AccountDTO      `json:"account,omitempty"`     // For ACCOUNT action
	History     []HistoryDealDTO `json:"history,omitempty"`     // For HISTORY action
	TimestampNs int64            `json:"timestamp_ns"`          // MT5 response time in nanoseconds
}

// HistoryDealDTO represents an official closed deal reported by MT5 terminal history.
type HistoryDealDTO struct {
	Ticket      string  `json:"ticket"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"` // "BUY" or "SELL"
	Lots        float64 `json:"lots"`
	EntryPrice  float64 `json:"entry"`
	ExitPrice   float64 `json:"exit"`
	NetPnL      float64 `json:"net_pnl"`
	DurationSec int64   `json:"duration_s"`
	CloseTime   int64   `json:"close_time"` // Epoch seconds
}

// PositionDTO represents an open position reported by MT5.
type PositionDTO struct {
	Ticket     string  `json:"ticket"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"` // "BUY" or "SELL"
	Lots       float64 `json:"lots"`
	OpenPrice  float64 `json:"open_price"`
	StopLoss   float64 `json:"stop_loss"`
	TakeProfit float64 `json:"take_profit"`
	Profit     float64 `json:"profit"`
	OpenTimeNs int64   `json:"open_time_ns"`
}

// AccountDTO represents financial status from MT5 AccountInfoDouble/Integer.
type AccountDTO struct {
	Balance     float64 `json:"balance"`
	Equity      float64 `json:"equity"`
	Margin      float64 `json:"margin"`
	FreeMargin  float64 `json:"free_margin"`
	MarginLevel float64 `json:"margin_level"`
	Leverage    int     `json:"leverage"`
	Currency    string  `json:"currency"`
}

// TickDTO represents a tick streamed from MT5 OnTick().
type TickDTO struct {
	Symbol      string  `json:"symbol"`
	Bid         float64 `json:"bid"`
	Ask         float64 `json:"ask"`
	TimestampNs int64   `json:"timestamp_ns"`
}
