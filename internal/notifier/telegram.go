package notifier

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"time"
)

// TelegramNotifier sends alerts via the Telegram Bot API.
type TelegramNotifier struct {
	botToken string
	chatID   string
	client   *http.Client
}

// NewTelegramNotifier creates a Telegram notifier.
// botToken: the bot token from @BotFather (e.g. "123456:ABC-DEF...")
// chatID: the target chat/group ID (e.g. "-1001234567890")
func NewTelegramNotifier(botToken, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		botToken: botToken,
		chatID:   chatID,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Name returns the notifier identifier.
func (t *TelegramNotifier) Name() string {
	return "Telegram"
}

// Send delivers the alert as a robust HTML-formatted Telegram message.
func (t *TelegramNotifier) Send(ctx context.Context, alert Alert) error {
	// Build safe HTML message
	title := html.EscapeString(alert.Title)
	message := html.EscapeString(alert.Message)
	timeStr := html.EscapeString(alert.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC"))

	text := fmt.Sprintf("%s <b>%s</b>\n%s\n<i>%s</i>",
		alert.Level.Emoji(), title, message, timeStr)

	if len(alert.Metadata) > 0 {
		text += "\n"
		for k, v := range alert.Metadata {
			text += fmt.Sprintf("\n<code>%s</code>: %s", html.EscapeString(k), html.EscapeString(v))
		}
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)

	params := url.Values{}
	params.Set("chat_id", t.chatID)
	params.Set("text", text)
	params.Set("parse_mode", "HTML")
	params.Set("disable_web_page_preview", "true")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("telegram: request error: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: send error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Fallback to plain text if HTML parsing failed
		params.Del("parse_mode")
		plainReq, pErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"?"+params.Encode(), nil)
		if pErr == nil {
			pResp, pErr2 := t.client.Do(plainReq)
			if pErr2 == nil {
				defer pResp.Body.Close()
				if pResp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		return fmt.Errorf("telegram: API returned %d", resp.StatusCode)
	}

	return nil
}
