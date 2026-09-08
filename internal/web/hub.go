package web

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TelemetryPayload is broadcast to all connected WebSocket clients.
type TelemetryPayload struct {
	TimestampNs         int64                        `json:"timestamp_ns"`
	BotStatus           string                       `json:"bot_status"`
	CircuitBreaker      bool                         `json:"circuit_breaker"`
	Balance             float64                      `json:"balance"`
	Equity              float64                      `json:"equity"`
	FloatingPnL         float64                      `json:"floating_pnl"`
	DailyDrawdownPct    float64                      `json:"daily_drawdown_pct"`
	WinRatePct          float64                      `json:"win_rate_pct"`
	ProfitFactor        float64                      `json:"profit_factor"`
	ProfitTargetReached bool                         `json:"profit_target_reached"`
	ProfitTargetAmount  float64                      `json:"profit_target_amount"`
	ActivePositions     []PositionTelemetry          `json:"active_positions"`
	Symbols             map[string]SymbolData        `json:"symbols"`
	RecentEvents        []SignalEvent                `json:"recent_events,omitempty"`
	UpcomingNews        *NewsTelemetry               `json:"upcoming_news,omitempty"`
	AIFilterEnabled     bool                         `json:"ai_filter_enabled"`
	TradingMode         string                       `json:"trading_mode"`
	MarketFocus         string                       `json:"market_focus"`
	AccountType         string                       `json:"account_type"`
	AccountCurrency     string                       `json:"account_currency"`
	Performance         *PerformanceTelemetry        `json:"performance,omitempty"`
	LiveCandles         map[string][]CandleTelemetry `json:"live_candles,omitempty"`
	TotalTicksProcessed int64                        `json:"total_ticks_processed,omitempty"`
	MemoryAllocMB       float64                      `json:"memory_alloc_mb,omitempty"`
	ActiveLotSize       float64                      `json:"active_lot_size,omitempty"`
}

type CandleTelemetry struct {
	Time   int64   `json:"time"` // Unix timestamp in seconds
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type TradeRecordTelemetry struct {
	Ticket    string  `json:"ticket"`
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Lots      float64 `json:"lots"`
	Entry     float64 `json:"entry"`
	Exit      float64 `json:"exit"`
	NetPnL    float64 `json:"pnl"`
	Pips      float64 `json:"pips"`
	Duration  string  `json:"duration"`
	CloseTime string  `json:"time"`
	Date      string  `json:"date,omitempty"`
	Timestamp int64   `json:"timestamp,omitempty"`
	MFEUSD    float64 `json:"mfe_usd,omitempty"`
	MAEUSD    float64 `json:"mae_usd,omitempty"`
	MFEPips   float64 `json:"mfe_pips,omitempty"`
	MAEPips   float64 `json:"mae_pips,omitempty"`
}

type PerformanceTelemetry struct {
	TotalTrades     int                          `json:"total_trades"`
	WinningTrades   int                          `json:"winning_trades"`
	LosingTrades    int                          `json:"losing_trades"`
	WinRatePct      float64                      `json:"win_rate_pct"`
	ProfitFactor    float64                      `json:"profit_factor"`
	TotalNetProfit  float64                      `json:"total_net_profit"`
	GrossProfit     float64                      `json:"gross_profit"`
	GrossLoss       float64                      `json:"gross_loss"`
	AverageWin      float64                      `json:"average_win"`
	AverageLoss     float64                      `json:"average_loss"`
	RealizedRRR     float64                      `json:"realized_rrr"`
	SymbolBreakdown map[string]SymbolPerformance `json:"symbol_breakdown"`
	TradeHistory    []TradeRecordTelemetry       `json:"trade_history,omitempty"`
}

type SymbolPerformance struct {
	Symbol    string  `json:"symbol"`
	Trades    int     `json:"trades"`
	Wins      int     `json:"wins"`
	Losses    int     `json:"losses"`
	NetProfit float64 `json:"net_profit"`
	WinRate   float64 `json:"win_rate"`
}

type PositionTelemetry struct {
	OrderID            string  `json:"order_id"`
	Symbol             string  `json:"symbol"`
	Side               string  `json:"side"`
	Lots               float64 `json:"lots"`
	EntryPrice         float64 `json:"entry_price"`
	CurrentPrice       float64 `json:"current_price"`
	StopLoss           float64 `json:"stop_loss"`
	TakeProfit         float64 `json:"take_profit"`
	FloatingPnL        float64 `json:"floating_pnl"`
	FloatingPips       float64 `json:"floating_pips"`
	HoldingTimeSec     int64   `json:"holding_time_sec"`
	PartialTPTriggered bool    `json:"partial_tp_triggered"`
	ProfitStage        int     `json:"profit_stage,omitempty"`
	MFEUSD             float64 `json:"mfe_usd,omitempty"`
	MAEUSD             float64 `json:"mae_usd,omitempty"`
	MFEPips            float64 `json:"mfe_pips,omitempty"`
	MAEPips            float64 `json:"mae_pips,omitempty"`
}

type SymbolData struct {
	Bid          float64 `json:"bid"`
	Ask          float64 `json:"ask"`
	SpreadPip    float64 `json:"spread_pip"`
	TPS          float64 `json:"tps"`
	AIRegime     string  `json:"ai_regime"`
	AIConf       float64 `json:"ai_conf"`
	AssetClass   string  `json:"asset_class,omitempty"`   // "GOLD"
	AssetIcon    string  `json:"asset_icon,omitempty"`    // "🪙"
	LastSignal   string  `json:"last_signal,omitempty"`   // "BUY" or "SELL"
	SignalStatus string  `json:"signal_status,omitempty"` // "APPROVED", "REJECTED", "IDLE", "SKIPPED"
	SignalReason string  `json:"signal_reason,omitempty"` // "Ranging Chop (0%)", "Conf 45% < 65%", "Score 72%"
	SignalTime   string  `json:"signal_time,omitempty"`   // "08:16:05"
	LastTickTime string  `json:"last_tick_time,omitempty"` // "01:43:55"
	TimeAgoSec   int64   `json:"time_ago_sec"`            // seconds since last tick
	High24h      float64 `json:"high_24h,omitempty"`
	Low24h       float64 `json:"low_24h,omitempty"`
	Change24hPct float64 `json:"change_24h_pct,omitempty"`
	Change5mPct  float64 `json:"change_5m_pct,omitempty"`
	Change15mPct float64 `json:"change_15m_pct,omitempty"`
	Change1hPct  float64 `json:"change_1h_pct,omitempty"`
	Change4hPct  float64 `json:"change_4h_pct,omitempty"`
	VolRatio     float64 `json:"vol_ratio,omitempty"`
}

type SignalEvent struct {
	Time    string  `json:"time"`
	Symbol  string  `json:"symbol"`
	Type    string  `json:"type"`   // "BUY" or "SELL"
	Price   float64 `json:"price"`
	Status  string  `json:"status"` // "APPROVED" or "REJECTED"
	Regime  string  `json:"regime"`
	ConfPct float64 `json:"conf_pct"`
	Reason  string  `json:"reason"`
}

type NewsTelemetry struct {
	Title                string `json:"title"`
	Currency             string `json:"currency"`
	Impact               string `json:"impact"`
	CountdownSec         int64  `json:"countdown_sec"`
	IsBlackout           bool   `json:"is_blackout"`
	EventTimeSec         int64  `json:"event_time_sec,omitempty"`
	BlackoutEndSec       int64  `json:"blackout_end_sec,omitempty"`
	BlackoutRemainingSec int64  `json:"blackout_remaining_sec,omitempty"`
}

// WSClient represents a single connected browser WebSocket with a buffered send channel.
type WSClient struct {
	hub    *WSHub
	conn   net.Conn
	send   chan []byte
	closed bool
	mu     sync.Mutex
}

// WSHub handles WebSocket connections with zero external dependencies (pure Go RFC 6455).
// Broadcasts are completely non-blocking: slow clients are disconnected automatically.
type WSHub struct {
	clients map[*WSClient]struct{}
	mu      sync.RWMutex
}

// NewWSHub creates a new non-blocking WebSocket hub.
func NewWSHub() *WSHub {
	return &WSHub{
		clients: make(map[*WSClient]struct{}, 16),
	}
}

// HandleWebSocket upgrades HTTP connection to WebSocket (RFC 6455) and registers client.
func (h *WSHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if strings.ToLower(r.Header.Get("Upgrade")) != "websocket" {
		http.Error(w, "Expected WebSocket Upgrade", http.StatusBadRequest)
		return
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "Missing Sec-WebSocket-Key", http.StatusBadRequest)
		return
	}

	// Calculate Sec-WebSocket-Accept
	hKey := sha1.New()
	hKey.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	acceptVal := base64.StdEncoding.EncodeToString(hKey.Sum(nil))

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, bufrw, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Send handshake response
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptVal + "\r\n\r\n"
	if _, err := bufrw.WriteString(response); err != nil {
		conn.Close()
		return
	}
	if err := bufrw.Flush(); err != nil {
		conn.Close()
		return
	}

	client := &WSClient{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 32), // 32-message non-blocking buffer
	}

	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	// Dedicated Write Pump (handles non-blocking writing with timeout)
	go client.writePump()

	// Read loop to detect browser close/disconnect
	go client.readPump(bufrw)
}

func (c *WSClient) writePump() {
	defer func() {
		c.hub.removeClient(c)
	}()

	for msg := range c.send {
		_ = c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if _, err := c.conn.Write(msg); err != nil {
			return
		}
	}
}

func (c *WSClient) readPump(bufrw *bufio.ReadWriter) {
	defer func() {
		c.hub.removeClient(c)
	}()

	buf := make([]byte, 512)
	for {
		_, err := bufrw.Read(buf)
		if err != nil {
			return
		}
	}
}

func (h *WSHub) removeClient(c *WSClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()

	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.send)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	}
	c.mu.Unlock()
}

// Broadcast sends a JSON payload to all connected WebSocket clients.
// Guaranteed NON-BLOCKING: If a client's send buffer is full (slow reader),
// the client is disconnected immediately so the core bot loop is NEVER delayed.
func (h *WSHub) Broadcast(payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	frame := encodeWSFrame(data)

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		select {
		case client.send <- frame:
			// Sent successfully
		default:
			// Slow reader: buffer full -> schedule disconnect to prevent backpressure on bot
			go h.removeClient(client)
		}
	}
}

// encodeWSFrame formats payload as a single unmasked text WebSocket frame (Opcode 0x1).
func encodeWSFrame(payload []byte) []byte {
	length := len(payload)
	var header []byte

	if length <= 125 {
		header = []byte{0x81, byte(length)}
	} else if length <= 65535 {
		header = []byte{0x81, 126, byte(length >> 8), byte(length & 0xFF)}
	} else {
		header = []byte{0x81, 127,
			0, 0, 0, 0,
			byte((length >> 24) & 0xFF),
			byte((length >> 16) & 0xFF),
			byte((length >> 8) & 0xFF),
			byte(length & 0xFF),
		}
	}

	frame := make([]byte, len(header)+len(payload))
	copy(frame, header)
	copy(frame[len(header):], payload)
	return frame
}
