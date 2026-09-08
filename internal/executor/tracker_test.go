package executor

import (
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func makeTestPosition(id string, side model.OrderSide, entry float64) model.Position {
	return model.Position{
		OrderID:    id,
		Symbol:     "EURUSD",
		Side:       side,
		Lots:       0.1,
		EntryPrice: entry,
		OpenTimeNs: time.Now().UnixNano(),
	}
}

func makeTick(bid, ask float64) model.Tick {
	return model.Tick{
		Symbol:      "EURUSD",
		Bid:         bid,
		Ask:         ask,
		TimestampNs: time.Now().UnixNano(),
	}
}

func TestTracker_AddAndCount(t *testing.T) {
	tr := NewPositionTracker(DefaultTrackerConfig())
	pos := makeTestPosition("ORD-1", model.SideBuy, 1.10000)
	tr.Add(pos, 1.09900, 1.10200)

	if tr.Count() != 1 {
		t.Errorf("expected count 1, got %d", tr.Count())
	}
}

func TestTracker_Remove(t *testing.T) {
	tr := NewPositionTracker(DefaultTrackerConfig())
	pos := makeTestPosition("ORD-1", model.SideBuy, 1.10000)
	tr.Add(pos, 1.09900, 1.10200)
	tr.Remove("ORD-1")

	if tr.Count() != 0 {
		t.Errorf("expected count 0 after remove, got %d", tr.Count())
	}
}

func TestTrailingStop_MovesForward_Buy(t *testing.T) {
	cfg := DefaultTrackerConfig()
	cfg.TrailingStopPips = 5.0
	cfg.EnableBreakEven = false
	tr := NewPositionTracker(cfg)

	pos := makeTestPosition("ORD-1", model.SideBuy, 1.10000)
	tr.Add(pos, 1.09950, 0) // SL at entry - 5 pips

	pipMult := 10000.0

	// Price moves up 10 pips — trailing should advance SL
	tick := makeTick(1.10100, 1.10102)
	tr.OnTick(tick, 0, pipMult)

	positions := tr.ActivePositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}

	// SL should have moved forward (above initial 1.09950)
	if positions[0].StopLoss <= 1.09950 {
		t.Errorf("expected SL to advance beyond 1.09950, got %.5f", positions[0].StopLoss)
	}
}

func TestTrailingStop_NeverMovesBack_Buy(t *testing.T) {
	cfg := DefaultTrackerConfig()
	cfg.TrailingStopPips = 5.0
	cfg.EnableBreakEven = false
	tr := NewPositionTracker(cfg)

	pos := makeTestPosition("ORD-1", model.SideBuy, 1.10000)
	tr.Add(pos, 1.09950, 0)

	pipMult := 10000.0

	// Price up 10 pips
	tr.OnTick(makeTick(1.10100, 1.10102), 0, pipMult)
	positions := tr.ActivePositions()
	slAfterUp := positions[0].StopLoss

	// Price drops back 3 pips — SL should NOT move back
	tr.OnTick(makeTick(1.10070, 1.10072), 0, pipMult)
	positions = tr.ActivePositions()
	slAfterDown := positions[0].StopLoss

	if slAfterDown < slAfterUp {
		t.Errorf("trailing SL moved backward: %.5f -> %.5f", slAfterUp, slAfterDown)
	}
}

func TestBreakEven_TriggersAtThreshold(t *testing.T) {
	cfg := DefaultTrackerConfig()
	cfg.BreakEvenPips = 5.0
	cfg.BreakEvenBuffPips = 1.0
	cfg.EnableTrailing = false
	tr := NewPositionTracker(cfg)

	pos := makeTestPosition("ORD-1", model.SideBuy, 1.10000)
	tr.Add(pos, 1.09900, 0)

	pipMult := 10000.0

	// Price up 4 pips — below BE threshold
	tr.OnTick(makeTick(1.10040, 1.10042), 0, pipMult)
	positions := tr.ActivePositions()
	if positions[0].BreakEvenTriggered {
		t.Errorf("BE triggered prematurely at 4 pips")
	}

	// Price up 6 pips — hits BE threshold
	tr.OnTick(makeTick(1.10060, 1.10062), 0, pipMult)
	positions = tr.ActivePositions()
	if !positions[0].BreakEvenTriggered {
		t.Errorf("expected BE to trigger at 6 pips")
	}

	// SL should now be entry + buffer = 1.10000 + 0.00010 = 1.10010
	expectedSL := 1.10000 + (1.0 / pipMult)
	if positions[0].StopLoss < expectedSL-0.00001 || positions[0].StopLoss > expectedSL+0.00001 {
		t.Errorf("expected SL at %.5f, got %.5f", expectedSL, positions[0].StopLoss)
	}
}

func TestPartialTakeProfit_Buy(t *testing.T) {
	cfg := DefaultTrackerConfig()
	cfg.EnablePartialTP = true
	cfg.PartialTPRatio = 0.5
	cfg.TP1Pips = 5.0
	cfg.TP2Pips = 15.0
	cfg.BreakEvenBuffPips = 1.0
	cfg.EnableTrailing = false
	tr := NewPositionTracker(cfg)

	pos := makeTestPosition("ORD-1", model.SideBuy, 1.10000)
	pos.Lots = 0.10
	tr.Add(pos, 1.09900, 0)

	pipMult := 10000.0

	// Price rises 6 pips (above TP1 threshold of 5 pips)
	tick := makeTick(1.10060, 1.10062)
	closes := tr.OnTick(tick, 0, pipMult)

	if len(closes) != 1 {
		t.Fatalf("expected 1 close event for Partial TP1, got %d", len(closes))
	}
	if closes[0].Reason != "PARTIAL_TP1_HIT" {
		t.Errorf("expected reason PARTIAL_TP1_HIT, got %s", closes[0].Reason)
	}
	if !closes[0].IsPartial {
		t.Errorf("expected IsPartial true")
	}
	if closes[0].Lots != 0.05 {
		t.Errorf("expected partial close 0.05 lots, got %.2f", closes[0].Lots)
	}

	// Position must still remain tracked with remaining lots = 0.05
	positions := tr.ActivePositions()
	if len(positions) != 1 {
		t.Fatalf("expected 1 active position remaining, got %d", len(positions))
	}
	if positions[0].RemainingLots != 0.05 {
		t.Errorf("expected remaining lots 0.05, got %.2f", positions[0].RemainingLots)
	}
	if !positions[0].BreakEvenTriggered {
		t.Errorf("expected break-even to be triggered upon TP1")
	}

	// Now price rises to 16 pips (above TP2 of 15 pips)
	tick2 := makeTick(1.10160, 1.10162)
	closes2 := tr.OnTick(tick2, 0, pipMult)
	if len(closes2) != 1 {
		t.Fatalf("expected 1 close event for TP2, got %d", len(closes2))
	}
	if closes2[0].Reason != "TP2_HIT" {
		t.Errorf("expected reason TP2_HIT, got %s", closes2[0].Reason)
	}
	if closes2[0].IsPartial {
		t.Errorf("expected TP2 to be full close, got IsPartial true")
	}
	if tr.Count() != 0 {
		t.Errorf("expected 0 positions remaining after TP2, got %d", tr.Count())
	}
}

func TestTimeStop_HoldingDurationExceeded(t *testing.T) {
	cfg := DefaultTrackerConfig()
	cfg.TimeStopDuration = 10 * time.Minute
	tr := NewPositionTracker(cfg)

	now := time.Now()
	pos := makeTestPosition("ORD-1", model.SideBuy, 1.10000)
	pos.OpenTimeNs = now.Add(-15 * time.Minute).UnixNano() // 15 mins ago
	tr.Add(pos, 1.09900, 1.10200)

	pipMult := 10000.0
	tick := model.Tick{
		Symbol:      "EURUSD",
		Bid:         1.10010,
		Ask:         1.10012,
		TimestampNs: now.UnixNano(),
	}

	closes := tr.OnTick(tick, 0, pipMult)
	if len(closes) != 1 {
		t.Fatalf("expected 1 close event for Time-Stop, got %d", len(closes))
	}
	if closes[0].Reason != "TIME_STOP_HIT" {
		t.Errorf("expected reason TIME_STOP_HIT, got %s", closes[0].Reason)
	}
	if tr.Count() != 0 {
		t.Errorf("expected position to be closed and removed, count=%d", tr.Count())
	}
}

func TestTracker_SL_Hit(t *testing.T) {
	cfg := DefaultTrackerConfig()
	cfg.EnableBreakEven = false
	cfg.EnableTrailing = false
	tr := NewPositionTracker(cfg)

	pos := makeTestPosition("ORD-1", model.SideBuy, 1.10000)
	tr.Add(pos, 1.09900, 0) // SL at 1.09900

	pipMult := 10000.0

	// Price drops below SL
	closes := tr.OnTick(makeTick(1.09890, 1.09892), 0, pipMult)
	if len(closes) != 1 {
		t.Fatalf("expected 1 close event, got %d", len(closes))
	}
	if closes[0].Reason != "SL_HIT" {
		t.Errorf("expected reason SL_HIT, got %s", closes[0].Reason)
	}
	if tr.Count() != 0 {
		t.Errorf("expected position to be removed after SL hit, count=%d", tr.Count())
	}
}

func TestTracker_MFE_MAE_Tracking(t *testing.T) {
	tr := NewPositionTracker(DefaultTrackerConfig())
	pos := makeTestPosition("ORD-MFE", model.SideBuy, 2500.00)
	pos.Symbol = "XAUUSD"
	tr.Add(pos, 2490.00, 2520.00)

	pipMult := 100.0

	// Tick 1: Price drops to 2498 (Floating loss -$2.00 / -20 pips -> MAE updated)
	tr.OnTick(model.Tick{Symbol: "XAUUSD", Bid: 2498.00, Ask: 2498.35}, 1.5, pipMult)
	p, _ := tr.Get("ORD-MFE")
	if p.MaxAdverseUSD >= 0 {
		t.Errorf("expected negative MAE, got %f", p.MaxAdverseUSD)
	}

	// Tick 2: Price surges to 2505 (Floating profit +$5.00 / +50 pips -> MFE updated)
	tr.OnTick(model.Tick{Symbol: "XAUUSD", Bid: 2505.00, Ask: 2505.35}, 1.5, pipMult)
	p, _ = tr.Get("ORD-MFE")
	if p.MaxFavorableUSD <= 0 {
		t.Errorf("expected positive MFE, got %f", p.MaxFavorableUSD)
	}

	// Tick 3: Price drops back to 2489.50 (SL hit)
	closes := tr.OnTick(model.Tick{Symbol: "XAUUSD", Bid: 2489.50, Ask: 2490.00}, 1.5, pipMult)
	if len(closes) != 1 {
		t.Fatalf("expected 1 close event on SL hit")
	}
	if closes[0].MaxFavorableUSD <= 0 {
		t.Errorf("expected CloseEvent to carry MFE > 0, got %f", closes[0].MaxFavorableUSD)
	}
	if closes[0].MaxAdverseUSD >= 0 {
		t.Errorf("expected CloseEvent to carry MAE < 0, got %f", closes[0].MaxAdverseUSD)
	}
}

func TestProfitLocker_Stages(t *testing.T) {
	cfg := DefaultTrackerConfig()
	cfg.EnableProfitLocker = true
	cfg.EnableBreakEven = false
	cfg.EnableTrailing = false
	tr := NewPositionTracker(cfg)

	// Gold SELL position @ 4400.00
	pos := makeTestPosition("GOLD-01", model.SideSell, 4400.00)
	pos.Symbol = "XAUUSDm"
	tr.Add(pos, 4405.00, 4380.00)

	atr := 3.00
	pipMult := 100.0

	// 1. Initial price: 4398.00 (gain $2.00, below Stage 1 $5.00 threshold)
	tick1 := model.Tick{Symbol: "XAUUSDm", Bid: 4397.90, Ask: 4398.00}
	tr.OnTick(tick1, atr, pipMult)
	p := tr.ActivePositions()[0]
	if p.ProfitStage != 0 {
		t.Fatalf("expected ProfitStage 0, got %d", p.ProfitStage)
	}

	// 2. Price drops to 4394.50 (gain $5.50 >= Stage 1 $5.00 threshold)
	tick2 := model.Tick{Symbol: "XAUUSDm", Bid: 4394.40, Ask: 4394.50}
	tr.OnTick(tick2, atr, pipMult)
	p = tr.ActivePositions()[0]
	if p.ProfitStage != 1 {
		t.Fatalf("expected ProfitStage 1 (Break-Even), got %d", p.ProfitStage)
	}
	if p.StopLoss > 4399.85 {
		t.Fatalf("expected SL moved to BEP ~4399.80, got %f", p.StopLoss)
	}

	// 3. Price drops to 4392.00 (gain $8.00 >= Stage 2 $7.50 threshold)
	tick3 := model.Tick{Symbol: "XAUUSDm", Bid: 4391.90, Ask: 4392.00}
	tr.OnTick(tick3, atr, pipMult)
	p = tr.ActivePositions()[0]
	if p.ProfitStage != 2 {
		t.Fatalf("expected ProfitStage 2 (50%% Lock), got %d", p.ProfitStage)
	}
	// Locked gain = 8.00 * 0.5 = 4.00 -> SL should be 4400 - 4.00 = 4396.00
	if p.StopLoss > 4396.25 {
		t.Fatalf("expected SL locked at <= 4396.25, got %f", p.StopLoss)
	}

	// 4. Price drops to 4388.50 (gain $11.50 >= Stage 3 $10.50 threshold)
	tick4 := model.Tick{Symbol: "XAUUSDm", Bid: 4388.40, Ask: 4388.50}
	tr.OnTick(tick4, atr, pipMult)
	p = tr.ActivePositions()[0]
	if p.ProfitStage != 3 {
		t.Fatalf("expected ProfitStage 3 (75%% Lock), got %d", p.ProfitStage)
	}
	// Locked gain = 11.50 * 0.75 = 8.625 -> SL should be 4400 - 8.625 = 4391.375
	if p.StopLoss > 4391.50 {
		t.Fatalf("expected SL locked at <= 4391.50, got %f", p.StopLoss)
	}
}

func BenchmarkTrackerOnTick(b *testing.B) {
	cfg := DefaultTrackerConfig()
	cfg.EnablePartialTP = true
	cfg.TimeStopDuration = 15 * time.Minute
	tr := NewPositionTracker(cfg)

	// Add a few positions
	for i := 0; i < 3; i++ {
		pos := makeTestPosition("ORD-"+string(rune('A'+i)), model.SideBuy, 1.10000)
		tr.Add(pos, 1.09900, 1.10200)
	}

	tick := makeTick(1.10020, 1.10022)
	pipMult := 10000.0
	atr := 0.0005

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tr.OnTick(tick, atr, pipMult)
	}
}
