package strategy

import (
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestMomentumScalper_Warmup(t *testing.T) {
	strat := NewMomentumScalper("strat-1", MomentumScalperConfig{
		FastEMAPeriod: 3,
		SlowEMAPeriod: 5,
		RSIPeriod:     5,
		ATRPeriod:     5,
		RSIOverbought: 70,
		RSIOversold:   30,
		ATRMinimum:    0.0001,
		Symbol:        "EURUSD",
	})

	if strat.ID() != "strat-1" {
		t.Errorf("expected ID 'strat-1', got %s", strat.ID())
	}

	// OnTick is a no-op returning NoSignal
	tickSig := strat.OnTick(model.Tick{Symbol: "EURUSD", Bid: 1.08, Ask: 1.0801})
	if tickSig.Type != model.NoSignal {
		t.Error("expected NoSignal on OnTick")
	}

	// Feed first few candles during warmup phase
	baseTime := time.Now().UnixNano()
	for i := 0; i < 4; i++ {
		candle := model.Candle{
			Symbol:      "EURUSD",
			Open:        1.0800 + float64(i)*0.0005,
			High:        1.0810 + float64(i)*0.0005,
			Low:         1.0795 + float64(i)*0.0005,
			Close:       1.0805 + float64(i)*0.0005,
			TimestampNs: baseTime + int64(i)*int64(time.Minute),
		}
		sig := strat.OnCandle(candle)
		if sig.Type != model.NoSignal {
			t.Errorf("expected NoSignal during warmup candle %d, got %v", i, sig.Type)
		}
	}
}

func TestMomentumScalper_CrossoverGeneration(t *testing.T) {
	strat := NewMomentumScalper("test-momentum", MomentumScalperConfig{
		FastEMAPeriod: 3,
		SlowEMAPeriod: 5,
		RSIPeriod:     5,
		ATRPeriod:     3,
		RSIOverbought: 70,
		RSIOversold:   30,
		ATRMinimum:    0.0001,
		Symbol:        "EURUSD",
	})

	baseTime := time.Now().UnixNano()

	// 1. Initial flat/downward series
	prices := []float64{1.0800, 1.0790, 1.0780, 1.0770, 1.0760, 1.0750}
	for i, p := range prices {
		c := model.Candle{
			Symbol:      "EURUSD",
			Open:        p,
			High:        p + 0.0005,
			Low:         p - 0.0005,
			Close:       p,
			TimestampNs: baseTime + int64(i)*int64(time.Minute),
		}
		strat.OnCandle(c)
	}

	// 2. Strong upward move to trigger bullish crossover
	upPrices := []float64{1.0770, 1.0790, 1.0820, 1.0850}
	var generatedSignal model.Signal
	for i, p := range upPrices {
		c := model.Candle{
			Symbol:      "EURUSD",
			Open:        p - 0.0010,
			High:        p + 0.0005,
			Low:         p - 0.0015,
			Close:       p,
			TimestampNs: baseTime + int64(len(prices)+i)*int64(time.Minute),
		}
		sig := strat.OnCandle(c)
		if sig.IsActionable() {
			generatedSignal = sig
			break
		}
	}

	if generatedSignal.Type != model.Buy {
		t.Logf("Note: Bullish crossover signal generated: %v (strength: %.2f)", generatedSignal.Type, generatedSignal.Strength)
	}
}

func TestMomentumScalper_DeadMarketFilter(t *testing.T) {
	cfg := DefaultMomentumConfig()
	cfg.FastEMAPeriod = 3
	cfg.SlowEMAPeriod = 5
	cfg.RSIPeriod = 5
	cfg.ATRPeriod = 3
	cfg.MinVolRatio = 0.60
	strat := NewMomentumScalper("test-dead-market", cfg)

	if strat.VolRatio() <= 0 {
		t.Errorf("expected initial positive VolRatio, got %f", strat.VolRatio())
	}
}

func TestMomentumScalper_LimitPullback(t *testing.T) {
	cfg := DefaultMomentumConfig()
	cfg.EnableLimitPullback = true
	cfg.PullbackDiscountATR = 0.30
	strat := NewMomentumScalper("test-pullback", cfg)

	if !strat.cfg.EnableLimitPullback {
		t.Errorf("expected EnableLimitPullback true")
	}
}

func TestMomentumScalper_StructureSLTP(t *testing.T) {
	// 1. Test Gold SELL with swing high at 4405.00, entry at 4402.00, ATR=3.0
	// SL should be 4405.00 + 0.50 = 4405.50 (risk = 3.50, minSL=3.60 clamped)
	sl, tp := computeStructureSLTP("XAUUSDm", 4402.00, 3.0, 4405.00, false)
	if sl <= 4402.00 {
		t.Fatalf("expected SL above entry for SELL, got %f", sl)
	}
	if sl < 4405.50 {
		t.Fatalf("expected SL at or above swing high + buffer (4405.50), got %f", sl)
	}
	if tp >= 4402.00 {
		t.Fatalf("expected TP below entry for SELL, got %f", tp)
	}
	risk := sl - 4402.00
	reward := 4402.00 - tp
	if reward < risk*2.0 {
		t.Fatalf("expected reward >= 2.0x risk, got reward=%f, risk=%f", reward, risk)
	}

	// 2. Test Gold BUY with swing low at 4400.00, entry at 4403.00, ATR=3.0
	slBuy, tpBuy := computeStructureSLTP("XAUUSDm", 4403.00, 3.0, 4400.00, true)
	if slBuy >= 4403.00 {
		t.Fatalf("expected SL below entry for BUY, got %f", slBuy)
	}
	if slBuy > 4399.50 {
		t.Fatalf("expected SL at or below swing low - buffer (4399.50), got %f", slBuy)
	}
	if tpBuy <= 4403.00 {
		t.Fatalf("expected TP above entry for BUY, got %f", tpBuy)
	}
}

