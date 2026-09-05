package notifier

import (
	"context"
	"testing"
	"time"
)

func TestTelegramInteractiveBotInit(t *testing.T) {
	cb := BotCallbacks{
		GetStatus: func() BotStatusSummary {
			return BotStatusSummary{
				Balance:             1005.0,
				Equity:              1005.0,
				DailyPnL:            25.5,
				TradingMode:         "BALANCED",
				MarketFocus:         "ALL",
				AIRegime:            "TRENDING_BULLISH",
				AIConfidence:        0.65,
				ProfitTargetReached: false,
			}
		},
		SetTradingMode: func(mode string) error {
			return nil
		},
		SetMarketFocus: func(focus string) error {
			return nil
		},
		CloseAllTrades: func() (int, error) {
			return 0, nil
		},
	}

	bot := NewTelegramInteractiveBot("mock-token", "123456", cb)
	if bot == nil {
		t.Fatal("expected non-nil bot")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Should start and stop cleanly
	bot.Start(ctx)
}
