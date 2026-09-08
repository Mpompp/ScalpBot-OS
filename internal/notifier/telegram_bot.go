package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BotStatusSummary provides a comprehensive snapshot of the trading bot state for Telegram.
type BotStatusSummary struct {
	Balance             float64
	Equity              float64
	DailyPnL            float64
	DailyDrawdownPct    float64
	FloatingPnL         float64
	OpenPositions       int
	TradingMode         string
	MarketFocus         string
	AIRegime            string
	AIConfidence        float64
	ProfitTargetReached bool
	ProfitTargetAmount  float64
	ActiveSymbols       []string
	IsPaused            bool
	TotalTicksProcessed int64
	MemoryAllocMB       float64
	GoldPrice           float64
	GoldSpread          float64
}

// BotCallbacks holds the execution hooks for remote commands.
type BotCallbacks struct {
	GetStatus      func() BotStatusSummary
	SetTradingMode func(mode string) error
	SetMarketFocus func(focus string) error
	CloseAllTrades func() (int, error)
	TogglePause    func(pause bool) bool
	GetTodayReport func() string
	GetPriceQuote  func() string
}

// TelegramInteractiveBot manages 2-way communication with Telegram.
type TelegramInteractiveBot struct {
	botToken   string
	chatID     string
	client     *http.Client
	callbacks  BotCallbacks
	lastUpdate int64
	running    bool
	isPaused   bool
	mu         sync.Mutex
}

// NewTelegramInteractiveBot creates a new interactive Telegram bot controller.
func NewTelegramInteractiveBot(botToken, chatID string, cb BotCallbacks) *TelegramInteractiveBot {
	return &TelegramInteractiveBot{
		botToken: botToken,
		chatID:   chatID,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		callbacks: cb,
	}
}

// Start launches the long-polling loop in a background goroutine.
func (b *TelegramInteractiveBot) Start(ctx context.Context) {
	b.mu.Lock()
	if b.running || b.botToken == "" {
		b.mu.Unlock()
		return
	}
	b.running = true
	b.mu.Unlock()

	log.Printf("[telegram-bot] 🤖 Interactive Telegram Bot service started (ChatID: %s)", b.chatID)
	b.SendStartupGreeting(ctx)

	go b.pollLoop(ctx)
}

// SendStartupGreeting sends an introductory message with control buttons on startup.
func (b *TelegramInteractiveBot) SendStartupGreeting(ctx context.Context) {
	text := "🚀 <b>SCALPBOT PRO QUANT ONLINE</b>\n" +
		"<i>Institutional Gold Scalper Engine is active & connected to MT5.</i>\n\n" +
		"Tekan tombol di bawah untuk memantau atau mengontrol bot secara hands-free dari HP:"

	keyboard := b.buildControlKeyboard(false)
	_ = b.sendMessageWithKeyboard(ctx, text, keyboard)
}

// SendTradeOpen sends a rich alert when a position is entered.
func (b *TelegramInteractiveBot) SendTradeOpen(ctx context.Context, ticket, symbol, side string, lots, price, sl, tp float64, regime string, conf float64, reason string) {
	emoji := "🟢"
	if strings.ToUpper(side) == "SELL" {
		emoji = "🔴"
	}

	slDist := math.Abs(price - sl)
	tpDist := math.Abs(tp - price)
	rrr := tpDist / math.Max(0.01, slDist)

	text := fmt.Sprintf("%s <b>ENTRY EXECUTED: %s %s</b>\n"+
		"━━━━━━━━━━━━━━━━━━━━\n"+
		"• <b>Ticket:</b> <code>#%s</code>\n"+
		"• <b>Volume:</b> <code>%.2f lots</code>\n"+
		"• <b>Entry:</b> <code>$%.2f</code>\n"+
		"• <b>Stop Loss:</b> <code>$%.2f</code> (Risk: $%.2f)\n"+
		"• <b>Take Profit:</b> <code>$%.2f</code> (Reward: $%.2f)\n"+
		"• <b>Planned RRR:</b> <code>1 : %.1f</code>\n"+
		"• <b>AI Regime:</b> <code>%s</code> (Conf: %.0f%%)\n"+
		"• <b>Trigger Reason:</b> <i>%s</i>\n"+
		"• <b>Time:</b> <code>%s WIB</code>",
		emoji, strings.ToUpper(side), html.EscapeString(symbol),
		html.EscapeString(ticket), lots, price,
		sl, slDist,
		tp, tpDist,
		rrr,
		html.EscapeString(regime), conf*100.0,
		html.EscapeString(reason),
		time.Now().UTC().Add(7*time.Hour).Format("15:04:05"))

	keyboard := b.buildControlKeyboard(b.isPaused)
	_ = b.sendMessageWithKeyboard(ctx, text, keyboard)
}

// SendTradeClose sends a rich alert when a position is closed.
func (b *TelegramInteractiveBot) SendTradeClose(ctx context.Context, ticket, symbol, side string, lots, entryPrice, closePrice, pnl, pips float64, duration time.Duration) {
	emoji := "🏆"
	badge := "PROFIT HIT"
	pnlSign := "+"
	if pnl < 0 {
		emoji = "🛑"
		badge = "STOP LOSS HIT"
		pnlSign = ""
	} else if pnl <= 0.50 {
		emoji = "🛡️"
		badge = "BREAK-EVEN (CAPITAL PROTECTED)"
	}

	text := fmt.Sprintf("%s <b>POSITION CLOSED: %s</b>\n"+
		"━━━━━━━━━━━━━━━━━━━━\n"+
		"• <b>Symbol:</b> <code>%s (%s)</code>\n"+
		"• <b>Ticket:</b> <code>#%s</code>\n"+
		"• <b>Net P&L:</b> <b>%s$%.2f</b> (%.1f pips)\n"+
		"• <b>Volume:</b> <code>%.2f lots</code>\n"+
		"• <b>Entry ➔ Exit:</b> <code>$%.2f ➔ $%.2f</code>\n"+
		"• <b>Hold Time:</b> <code>%s</code>\n"+
		"• <b>Time:</b> <code>%s WIB</code>",
		emoji, badge,
		html.EscapeString(symbol), strings.ToUpper(side),
		html.EscapeString(ticket),
		pnlSign, pnl, pips,
		lots, entryPrice, closePrice,
		duration.Round(time.Second),
		time.Now().UTC().Add(7*time.Hour).Format("15:04:05"))

	keyboard := b.buildControlKeyboard(b.isPaused)
	_ = b.sendMessageWithKeyboard(ctx, text, keyboard)
}

// SendProfitTargetAlert sends a celebration message when daily profit target is hit.
func (b *TelegramInteractiveBot) SendProfitTargetAlert(ctx context.Context, dailyPnL, target, equity float64) {
	text := fmt.Sprintf("🏆 <b>DAILY PROFIT TARGET ACHIEVED!</b> 🏆\n"+
		"━━━━━━━━━━━━━━━━━━━━\n"+
		"• <b>Realized Today:</b> <b>+$%.2f</b>\n"+
		"• <b>Target Goal:</b> <code>+$%.2f</code>\n"+
		"• <b>Current Equity:</b> <code>$%.2f</code>\n\n"+
		"🛡️ <b>Capital Preservation Active:</b>\n"+
		"<i>Bot telah mengamankan modal dan istirahat (Auto-Lock) untuk hari ini. Profit Anda terlindungi!</i>",
		dailyPnL, target, equity)

	keyboard := b.buildControlKeyboard(b.isPaused)
	_ = b.sendMessageWithKeyboard(ctx, text, keyboard)
}

// pollLoop continually checks for incoming messages and button callbacks.
func (b *TelegramInteractiveBot) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.fetchUpdates(ctx)
		}
	}
}

type tgUpdate struct {
	UpdateID      int64            `json:"update_id"`
	Message       *tgMessage       `json:"message,omitempty"`
	CallbackQuery *tgCallbackQuery `json:"callback_query,omitempty"`
}

type tgMessage struct {
	MessageID int64  `json:"message_id"`
	Chat      tgChat `json:"chat"`
	Text      string `json:"text"`
}

type tgCallbackQuery struct {
	ID      string     `json:"id"`
	From    tgUser     `json:"from"`
	Message *tgMessage `json:"message,omitempty"`
	Data    string     `json:"data"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
}

type tgUpdateResponse struct {
	OK     bool       `json:"ok"`
	Result []tgUpdate `json:"result"`
}

func (b *TelegramInteractiveBot) fetchUpdates(ctx context.Context) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=5", b.botToken, b.lastUpdate+1)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var res tgUpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil || !res.OK {
		return
	}

	for _, upd := range res.Result {
		if upd.UpdateID > b.lastUpdate {
			b.lastUpdate = upd.UpdateID
		}

		if upd.CallbackQuery != nil {
			b.handleCallbackQuery(ctx, upd.CallbackQuery)
		} else if upd.Message != nil && upd.Message.Text != "" {
			b.handleTextMessage(ctx, upd.Message)
		}
	}
}

func (b *TelegramInteractiveBot) handleTextMessage(ctx context.Context, msg *tgMessage) {
	cmd := strings.ToLower(strings.TrimSpace(msg.Text))
	switch {
	case cmd == "/start" || cmd == "/help" || cmd == "help":
		b.SendStartupGreeting(ctx)
	case cmd == "/status" || cmd == "status":
		b.sendCurrentStatus(ctx)
	case cmd == "/today" || cmd == "/report" || cmd == "report":
		b.sendTodayReport(ctx)
	case cmd == "/price" || cmd == "/quote" || cmd == "price":
		b.sendPriceQuote(ctx)
	case cmd == "/pause" || cmd == "pause":
		b.applyPauseToggle(ctx, true)
	case cmd == "/resume" || cmd == "resume":
		b.applyPauseToggle(ctx, false)
	case cmd == "/santai":
		b.applyModeChange(ctx, "SANTAI")
	case cmd == "/balanced":
		b.applyModeChange(ctx, "BALANCED")
	case cmd == "/agresif":
		b.applyModeChange(ctx, "AGRESIF")
	case cmd == "/gold":
		b.applyFocusChange(ctx, "GOLD_ONLY")
	case cmd == "/closeall" || cmd == "closeall":
		b.applyCloseAll(ctx)
	case cmd == "/ping" || cmd == "ping":
		b.sendPingStatus(ctx)
	default:
		text := fmt.Sprintf("❓ Perintah tidak dikenal: <code>%s</code>\nSilakan pilih menu kontrol di bawah:", html.EscapeString(msg.Text))
		_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard(b.isPaused))
	}
}

func (b *TelegramInteractiveBot) handleCallbackQuery(ctx context.Context, cb *tgCallbackQuery) {
	// Acknowledge callback immediately
	ackURL := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", b.botToken)
	ackBody, _ := json.Marshal(map[string]string{"callback_query_id": cb.ID})
	_, _ = b.client.Post(ackURL, "application/json", bytes.NewReader(ackBody))

	switch cb.Data {
	case "cb_status", "cb_refresh":
		b.sendCurrentStatus(ctx)
	case "cb_today":
		b.sendTodayReport(ctx)
	case "cb_quote":
		b.sendPriceQuote(ctx)
	case "cb_pause":
		b.applyPauseToggle(ctx, true)
	case "cb_resume":
		b.applyPauseToggle(ctx, false)
	case "cb_mode_santai":
		b.applyModeChange(ctx, "SANTAI")
	case "cb_mode_balanced":
		b.applyModeChange(ctx, "BALANCED")
	case "cb_mode_agresif":
		b.applyModeChange(ctx, "AGRESIF")
	case "cb_focus_gold":
		b.applyFocusChange(ctx, "GOLD_ONLY")
	case "cb_close_all":
		b.applyCloseAll(ctx)
	}
}

func (b *TelegramInteractiveBot) sendCurrentStatus(ctx context.Context) {
	if b.callbacks.GetStatus == nil {
		return
	}
	s := b.callbacks.GetStatus()

	pnlSign := "+"
	if s.DailyPnL < 0 {
		pnlSign = ""
	}

	targetText := "Running"
	if s.ProfitTargetReached {
		targetText = "🏆 TARGET ACHIEVED (Protected)"
	} else if s.ProfitTargetAmount > 0 {
		targetText = fmt.Sprintf("$%.2f goal", s.ProfitTargetAmount)
	}

	statusState := "🟢 LIVE TRADING"
	if s.IsPaused {
		statusState = "⏸️ PAUSED (Order Freeze)"
	}

	text := fmt.Sprintf("📊 <b>SCALPBOT QUANT DASHBOARD</b>\n"+
		"━━━━━━━━━━━━━━━━━━━━\n"+
		"• <b>Engine State:</b> <b>%s</b>\n"+
		"• <b>Net Equity:</b> <code>$%.2f</code>\n"+
		"• <b>Balance:</b> <code>$%.2f</code>\n"+
		"• <b>Daily Realized P&L:</b> <b>%s$%.2f</b>\n"+
		"• <b>Floating P&L:</b> <code>$%.2f</code> (%d active)\n"+
		"• <b>Gold Price:</b> <code>$%.2f</code> (Spread: %.1fp)\n"+
		"• <b>Trading Mode:</b> <b>%s</b>\n"+
		"• <b>Target Status:</b> <code>%s</code>\n"+
		"• <b>Market Weather:</b> <code>%s</code> (%.0f%%)\n"+
		"• <b>Ticks Processed:</b> <code>%s ticks</code>\n"+
		"• <b>Time:</b> <code>%s WIB</code>",
		statusState,
		s.Equity, s.Balance,
		pnlSign, s.DailyPnL,
		s.FloatingPnL, s.OpenPositions,
		s.GoldPrice, s.GoldSpread,
		s.TradingMode,
		targetText,
		html.EscapeString(s.AIRegime), s.AIConfidence*100.0,
		formatNumber(s.TotalTicksProcessed),
		time.Now().UTC().Add(7*time.Hour).Format("15:04:05"))

	keyboard := b.buildControlKeyboard(s.IsPaused)
	_ = b.sendMessageWithKeyboard(ctx, text, keyboard)
}

func (b *TelegramInteractiveBot) sendTodayReport(ctx context.Context) {
	if b.callbacks.GetTodayReport != nil {
		reportText := b.callbacks.GetTodayReport()
		_ = b.sendMessageWithKeyboard(ctx, reportText, b.buildControlKeyboard(b.isPaused))
		return
	}
	_ = b.sendMessageWithKeyboard(ctx, "📈 Laporan hari ini sedang dimutakhirkan...", b.buildControlKeyboard(b.isPaused))
}

func (b *TelegramInteractiveBot) sendPriceQuote(ctx context.Context) {
	if b.callbacks.GetPriceQuote != nil {
		quoteText := b.callbacks.GetPriceQuote()
		_ = b.sendMessageWithKeyboard(ctx, quoteText, b.buildControlKeyboard(b.isPaused))
		return
	}
	_ = b.sendMessageWithKeyboard(ctx, "🪙 Memuat quote harga live...", b.buildControlKeyboard(b.isPaused))
}

func (b *TelegramInteractiveBot) applyPauseToggle(ctx context.Context, pause bool) {
	b.mu.Lock()
	b.isPaused = pause
	b.mu.Unlock()

	if b.callbacks.TogglePause != nil {
		_ = b.callbacks.TogglePause(pause)
	}

	stateText := "⏸️ <b>BOT PAUSED (ORDER FREEZE)</b>\nSemua sinyal baru dibekukan sementara. Posisi aktif yang sedang berjalan tetap dijaga oleh Trailing Stop / SL."
	if !pause {
		stateText = "▶️ <b>BOT RESUMED (LIVE TRADING)</b>\nSistem kembali aktif penuh mengintai setup trading."
	}

	_ = b.sendMessageWithKeyboard(ctx, stateText, b.buildControlKeyboard(pause))
}

func (b *TelegramInteractiveBot) sendPingStatus(ctx context.Context) {
	text := fmt.Sprintf("🏓 <b>PONG!</b>\n• Socket: <code>Dual TCP 5555/5556 MT5 (ESTABLISHED)</code>\n• Web Engine: <code>Go 1.23 Zero-Alloc (0 B/op)</code>\n• Server Time: <code>%s WIB</code>",
		time.Now().UTC().Add(7*time.Hour).Format("15:04:05"))
	_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard(b.isPaused))
}

func (b *TelegramInteractiveBot) applyModeChange(ctx context.Context, mode string) {
	if b.callbacks.SetTradingMode != nil {
		_ = b.callbacks.SetTradingMode(mode)
	}
	text := fmt.Sprintf("🔄 <b>TRADING MODE CHANGED</b>\nMode sekarang aktif: <b>%s</b> 🎯", mode)
	_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard(b.isPaused))
}

func (b *TelegramInteractiveBot) applyFocusChange(ctx context.Context, focus string) {
	if b.callbacks.SetMarketFocus != nil {
		_ = b.callbacks.SetMarketFocus("GOLD_ONLY")
	}
	text := "🪙 <b>MARKET FOCUS: 100% GOLD EXCLUSIVE</b>\nBot difokuskan secara eksklusif untuk instrumen <b>XAUUSD (Gold)</b>."
	_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard(b.isPaused))
}

func (b *TelegramInteractiveBot) applyCloseAll(ctx context.Context) {
	if b.callbacks.CloseAllTrades != nil {
		closed, err := b.callbacks.CloseAllTrades()
		if err != nil {
			text := fmt.Sprintf("⚠️ <b>GAGAL MENUTUP SEMUA POSISI:</b> %v", err)
			_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard(b.isPaused))
			return
		}
		text := fmt.Sprintf("🚨 <b>EMERGENCY KILL SWITCH EXECUTED!</b>\nBerhasil menutup <b>%d</b> posisi aktif di broker.", closed)
		_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard(b.isPaused))
	}
}

func (b *TelegramInteractiveBot) buildControlKeyboard(isPaused bool) map[string]interface{} {
	pauseBtn := map[string]string{"text": "⏸️ Pause Bot", "callback_data": "cb_pause"}
	if isPaused {
		pauseBtn = map[string]string{"text": "▶️ Resume Bot", "callback_data": "cb_resume"}
	}

	return map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "📊 Status", "callback_data": "cb_status"},
				{"text": "📈 Laporan Hari Ini", "callback_data": "cb_today"},
			},
			{
				{"text": "🪙 Harga Live (Quote)", "callback_data": "cb_quote"},
				{"text": "🔄 Refresh", "callback_data": "cb_refresh"},
			},
			{
				{"text": "🌿 Santai", "callback_data": "cb_mode_santai"},
				{"text": "⚖️ Balanced", "callback_data": "cb_mode_balanced"},
				{"text": "⚡ Agresif", "callback_data": "cb_mode_agresif"},
			},
			{
				pauseBtn,
				{"text": "🚨 TUTUP SEMUA", "callback_data": "cb_close_all"},
			},
		},
	}
}

func (b *TelegramInteractiveBot) sendMessageWithKeyboard(ctx context.Context, text string, keyboard map[string]interface{}) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.botToken)

	payload := map[string]interface{}{
		"chat_id":                  b.chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
		"reply_markup":             keyboard,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func formatNumber(n int64) string {
	in := fmt.Sprintf("%d", n)
	var out []rune
	l := len(in)
	for i, r := range in {
		out = append(out, r)
		rem := l - i - 1
		if rem > 0 && rem%3 == 0 {
			out = append(out, '.')
		}
	}
	return string(out)
}
