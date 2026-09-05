package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BotStatusSummary provides a snapshot of the trading bot state for Telegram.
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
}

// BotCallbacks holds the execution hooks for remote commands.
type BotCallbacks struct {
	GetStatus      func() BotStatusSummary
	SetTradingMode func(mode string) error
	SetMarketFocus func(focus string) error
	CloseAllTrades func() (int, error)
}

// TelegramInteractiveBot manages 2-way communication with Telegram.
type TelegramInteractiveBot struct {
	botToken   string
	chatID     string
	client     *http.Client
	callbacks  BotCallbacks
	lastUpdate int64
	running    bool
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
	text := "🚀 <b>SCALPBOT PRO QUANT ACTIVE</b>\n" +
		"<i>Institutional Scalping Engine is now online and connected to MT5.</i>\n\n" +
		"Tekan tombol di bawah untuk mengontrol bot dari HP Anda:"

	keyboard := b.buildControlKeyboard()
	_ = b.sendMessageWithKeyboard(ctx, text, keyboard)
}

// SendTradeOpen sends a rich alert when a position is entered.
func (b *TelegramInteractiveBot) SendTradeOpen(ctx context.Context, ticket, symbol, side string, lots, price, sl, tp float64, regime string, conf float64, reason string) {
	emoji := "🟢"
	if strings.ToUpper(side) == "SELL" {
		emoji = "🔴"
	}

	text := fmt.Sprintf("%s <b>ENTRY EXECUTED: %s %s</b>\n"+
		"━━━━━━━━━━━━━━━━━━━━\n"+
		"• <b>Ticket:</b> <code>%s</code>\n"+
		"• <b>Lots:</b> <code>%.2f</code>\n"+
		"• <b>Entry Price:</b> <code>%.5f</code>\n"+
		"• <b>Stop Loss:</b> <code>%.5f</code>\n"+
		"• <b>Take Profit:</b> <code>%.5f</code>\n"+
		"• <b>AI Regime:</b> <code>%s</code> (Conf: %.1f%%)\n"+
		"• <b>Reason:</b> <i>%s</i>\n"+
		"• <b>Time:</b> <code>%s UTC</code>",
		emoji, strings.ToUpper(side), html.EscapeString(symbol),
		html.EscapeString(ticket), lots, price, sl, tp,
		html.EscapeString(regime), conf*100.0,
		html.EscapeString(reason),
		time.Now().UTC().Format("15:04:05"))

	keyboard := b.buildControlKeyboard()
	_ = b.sendMessageWithKeyboard(ctx, text, keyboard)
}

// SendTradeClose sends a rich alert when a position is closed.
func (b *TelegramInteractiveBot) SendTradeClose(ctx context.Context, ticket, symbol, side string, lots, entryPrice, closePrice, pnl, pips float64, duration time.Duration) {
	emoji := "✅"
	pnlSign := "+"
	if pnl < 0 {
		emoji = "❌"
		pnlSign = ""
	}

	text := fmt.Sprintf("%s <b>POSITION CLOSED: %s %s</b>\n"+
		"━━━━━━━━━━━━━━━━━━━━\n"+
		"• <b>Ticket:</b> <code>%s</code>\n"+
		"• <b>P&L:</b> <b>%s%.2f</b> (%.1f pips)\n"+
		"• <b>Lots:</b> <code>%.2f</code>\n"+
		"• <b>Entry:</b> <code>%.5f</code> ➔ <b>Exit:</b> <code>%.5f</code>\n"+
		"• <b>Hold Time:</b> <code>%s</code>\n"+
		"• <b>Time:</b> <code>%s UTC</code>",
		emoji, strings.ToUpper(side), html.EscapeString(symbol),
		html.EscapeString(ticket),
		pnlSign, pnl, pips,
		lots, entryPrice, closePrice,
		duration.Round(time.Second),
		time.Now().UTC().Format("15:04:05"))

	keyboard := b.buildControlKeyboard()
	_ = b.sendMessageWithKeyboard(ctx, text, keyboard)
}

// SendProfitTargetAlert sends a celebration message when daily profit target is hit.
func (b *TelegramInteractiveBot) SendProfitTargetAlert(ctx context.Context, dailyPnL, target, equity float64) {
	text := fmt.Sprintf("🏆 <b>DAILY PROFIT TARGET ACHIEVED!</b> 🏆\n"+
		"━━━━━━━━━━━━━━━━━━━━\n"+
		"• <b>Realized Today:</b> <b>+%.2f</b>\n"+
		"• <b>Target:</b> <code>+%.2f</code>\n"+
		"• <b>Current Equity:</b> <code>%.2f</code>\n\n"+
		"🛡️ <b>Capital Preservation Active:</b>\n"+
		"<i>Bot telah mengunci keuntungan dan istirahat (Auto-Sleep) untuk hari ini. Profit Anda aman!</i>",
		dailyPnL, target, equity)

	keyboard := b.buildControlKeyboard()
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
	case cmd == "/start" || cmd == "/help":
		b.SendStartupGreeting(ctx)
	case cmd == "/status" || cmd == "status":
		b.sendCurrentStatus(ctx)
	case cmd == "/santai":
		b.applyModeChange(ctx, "SANTAI")
	case cmd == "/balanced":
		b.applyModeChange(ctx, "BALANCED")
	case cmd == "/agresif":
		b.applyModeChange(ctx, "AGRESIF")
	case cmd == "/gold":
		b.applyFocusChange(ctx, "GOLD_ONLY")
	case cmd == "/forex":
		b.applyFocusChange(ctx, "FOREX_ONLY")
	case cmd == "/all":
		b.applyFocusChange(ctx, "ALL")
	case cmd == "/closeall":
		b.applyCloseAll(ctx)
	default:
		text := fmt.Sprintf("❓ Perintah tidak dikenal: <code>%s</code>\nSilakan gunakan tombol di bawah:", html.EscapeString(msg.Text))
		_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard())
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
	case "cb_mode_santai":
		b.applyModeChange(ctx, "SANTAI")
	case "cb_mode_balanced":
		b.applyModeChange(ctx, "BALANCED")
	case "cb_mode_agresif":
		b.applyModeChange(ctx, "AGRESIF")
	case "cb_focus_all":
		b.applyFocusChange(ctx, "ALL")
	case "cb_focus_gold":
		b.applyFocusChange(ctx, "GOLD_ONLY")
	case "cb_focus_forex":
		b.applyFocusChange(ctx, "FOREX_ONLY")
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
		targetText = fmt.Sprintf("%.2f target", s.ProfitTargetAmount)
	}

	text := fmt.Sprintf("📊 <b>SCALPBOT LIVE STATUS</b>\n"+
		"━━━━━━━━━━━━━━━━━━━━\n"+
		"• <b>Balance:</b> <code>%.2f</code>\n"+
		"• <b>Equity:</b> <code>%.2f</code>\n"+
		"• <b>Daily P&L:</b> <b>%s%.2f</b>\n"+
		"• <b>Floating P&L:</b> <code>%.2f</code>\n"+
		"• <b>Open Trades:</b> <code>%d</code>\n"+
		"• <b>Trading Mode:</b> <b>%s</b>\n"+
		"• <b>Market Focus:</b> <b>%s</b>\n"+
		"• <b>AI Regime:</b> <code>%s</code> (%.0f%%)\n"+
		"• <b>Target Status:</b> <code>%s</code>\n"+
		"• <b>Time:</b> <code>%s UTC</code>",
		s.Balance, s.Equity,
		pnlSign, s.DailyPnL,
		s.FloatingPnL, s.OpenPositions,
		s.TradingMode, s.MarketFocus,
		s.AIRegime, s.AIConfidence*100.0,
		targetText,
		time.Now().UTC().Format("15:04:05"))

	keyboard := b.buildControlKeyboard()
	_ = b.sendMessageWithKeyboard(ctx, text, keyboard)
}

func (b *TelegramInteractiveBot) applyModeChange(ctx context.Context, mode string) {
	if b.callbacks.SetTradingMode != nil {
		_ = b.callbacks.SetTradingMode(mode)
	}
	text := fmt.Sprintf("🔄 <b>TRADING MODE CHANGED</b>\nMode sekarang aktif: <b>%s</b> 🎯", mode)
	_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard())
}

func (b *TelegramInteractiveBot) applyFocusChange(ctx context.Context, focus string) {
	if b.callbacks.SetMarketFocus != nil {
		_ = b.callbacks.SetMarketFocus(focus)
	}
	text := fmt.Sprintf("🎯 <b>MARKET FOCUS CHANGED</b>\nFokus pasar aktif: <b>%s</b> 🌐", focus)
	_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard())
}

func (b *TelegramInteractiveBot) applyCloseAll(ctx context.Context) {
	if b.callbacks.CloseAllTrades != nil {
		closed, err := b.callbacks.CloseAllTrades()
		if err != nil {
			text := fmt.Sprintf("⚠️ <b>GAGAL MENUTUP SEMUA POSISI:</b> %v", err)
			_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard())
			return
		}
		text := fmt.Sprintf("🚨 <b>EMERGENCY KILL SWITCH EXECUTED!</b>\nBerhasil menutup <b>%d</b> posisi terbuka.", closed)
		_ = b.sendMessageWithKeyboard(ctx, text, b.buildControlKeyboard())
	}
}

func (b *TelegramInteractiveBot) buildControlKeyboard() map[string]interface{} {
	return map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "📊 Status", "callback_data": "cb_status"},
				{"text": "🔄 Refresh", "callback_data": "cb_refresh"},
			},
			{
				{"text": "🌿 Santai", "callback_data": "cb_mode_santai"},
				{"text": "⚖️ Balanced", "callback_data": "cb_mode_balanced"},
				{"text": "⚡ Agresif", "callback_data": "cb_mode_agresif"},
			},
			{
				{"text": "🌐 All Pairs", "callback_data": "cb_focus_all"},
				{"text": "🪙 Gold Only", "callback_data": "cb_focus_gold"},
				{"text": "💵 Forex Only", "callback_data": "cb_focus_forex"},
			},
			{
				{"text": "🛑 CLOSE ALL (EMERGENCY)", "callback_data": "cb_close_all"},
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
