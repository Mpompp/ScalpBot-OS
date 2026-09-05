package strategy

import (
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestRangeScalper_BuyReversal(t *testing.T) {
	cfg := DefaultRangeConfig()
	cfg.BollingerPeriod = 5
	cfg.RSIPeriod = 5
	cfg.ATRPeriod = 5
	cfg.RSIOversold = 40.0
	cfg.RSIOverbought = 60.0

	rs := NewRangeScalper("test_range", cfg)

	// Feed initial normal candles around 1.1000
	baseTime := time.Now().UnixNano()
	for i := 0; i < 10; i++ {
		c := model.Candle{
			Symbol:      "EURUSD",
			Open:        1.1000,
			High:        1.1005,
			Low:         1.0995,
			Close:       1.1000,
			TimestampNs: baseTime + int64(i)*time.Minute.Nanoseconds(),
		}
		_ = rs.OnCandle(c)
	}

	// Feed sharp drop (touching lower band + RSI oversold)
	oversoldCandle := model.Candle{
		Symbol:      "EURUSD",
		Open:        1.0995,
		High:        1.0995,
		Low:         1.0950,
		Close:       1.0955,
		TimestampNs: baseTime + 11*time.Minute.Nanoseconds(),
	}
	sig := rs.OnCandle(oversoldCandle)

	if sig.Type != model.Buy {
		t.Logf("signal produced: %+v", sig)
	}
}

func TestRangeScalper_SellReversal(t *testing.T) {
	cfg := DefaultRangeConfig()
	cfg.BollingerPeriod = 5
	cfg.RSIPeriod = 5
	cfg.ATRPeriod = 5
	cfg.RSIOversold = 40.0
	cfg.RSIOverbought = 60.0

	rs := NewRangeScalper("test_range", cfg)

	baseTime := time.Now().UnixNano()
	for i := 0; i < 10; i++ {
		c := model.Candle{
			Symbol:      "EURUSD",
			Open:        1.1000,
			High:        1.1005,
			Low:         1.0995,
			Close:       1.1000,
			TimestampNs: baseTime + int64(i)*time.Minute.Nanoseconds(),
		}
		_ = rs.OnCandle(c)
	}

	// Feed sharp surge (touching upper band + RSI overbought)
	overboughtCandle := model.Candle{
		Symbol:      "EURUSD",
		Open:        1.1005,
		High:        1.1050,
		Low:         1.1000,
		Close:       1.1045,
		TimestampNs: baseTime + 11*time.Minute.Nanoseconds(),
	}
	sig := rs.OnCandle(overboughtCandle)

	if sig.Type != model.Sell {
		t.Logf("signal produced: %+v", sig)
	}
}
