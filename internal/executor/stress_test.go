package executor

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestTracker_OddAndMinimumLotPartialTP(t *testing.T) {
	cfg := TrackerConfig{
		EnablePartialTP:   true,
		PartialTPRatio:    0.5,
		TP1Pips:           10.0,
		TP2Pips:           25.0,
		BreakEvenBuffPips: 1.0,
	}
	tracker := NewPositionTracker(cfg)

	// Case 1: Minimum lot (0.01 lot) -> 50% is 0.005 lot (below 0.01 min step)
	// Must NOT trigger invalid partial close
	tracker.Add(model.Position{
		OrderID:    "MIN-001",
		Symbol:     "EURUSD",
		Side:       model.SideBuy,
		Lots:       0.01,
		EntryPrice: 1.08500,
	}, 1.08000, 1.09000)

	// Price moves +12 pips (above TP1)
	tick1 := model.Tick{Symbol: "EURUSD", Bid: 1.08620, Ask: 1.08622, TimestampNs: 1000000}
	closes1 := tracker.OnTick(tick1, 0.0010, 10000.0)

	for _, ce := range closes1 {
		if ce.IsPartial && ce.Lots < 0.01 {
			t.Fatalf("Partial TP triggered invalid sub-minimum lot size: %f", ce.Lots)
		}
	}

	// Case 2: Odd lot (0.03 lot) -> 50% is 0.015 -> rounded to 0.01 lot
	tracker.Add(model.Position{
		OrderID:    "ODD-003",
		Symbol:     "EURUSD",
		Side:       model.SideBuy,
		Lots:       0.03,
		EntryPrice: 1.08500,
	}, 1.08000, 1.09000)

	closes2 := tracker.OnTick(tick1, 0.0010, 10000.0)
	var foundPartial bool
	for _, ce := range closes2 {
		if ce.OrderID == "ODD-003" && ce.IsPartial {
			foundPartial = true
			if ce.Lots != 0.01 {
				t.Fatalf("Expected 0.01 lot partial close for 0.03 lot order, got: %f", ce.Lots)
			}
		}
	}
	if !foundPartial {
		t.Fatalf("Expected partial close for ODD-003")
	}
}

func TestTracker_HighConcurrencyStress(t *testing.T) {
	cfg := TrackerConfig{
		EnableTrailing:    true,
		TrailingMode:      TrailingFixed,
		TrailingStopPips:  5.0,
		EnableBreakEven:   true,
		BreakEvenPips:     4.0,
		BreakEvenBuffPips: 1.0,
		EnablePartialTP:   true,
		PartialTPRatio:    0.5,
		TP1Pips:           8.0,
	}
	tracker := NewPositionTracker(cfg)

	var wg sync.WaitGroup
	var opCount atomic.Int64
	numWorkers := 30
	numIterations := 500

	// 1. Concurrent Position Adders
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		workerID := w
		go func() {
			defer wg.Done()
			for i := 0; i < numIterations; i++ {
				posID := fmt.Sprintf("POS-%d-%d", workerID, i)
				tracker.Add(model.Position{
					OrderID:    posID,
					Symbol:     "EURUSD",
					Side:       model.SideBuy,
					Lots:       0.10,
					EntryPrice: 1.08500,
				}, 1.08000, 1.09000)
				opCount.Add(1)

				if i%5 == 0 {
					tracker.Remove(posID)
				}
			}
		}()
	}

	// 2. Concurrent OnTick Streamers
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			for i := 0; i < numIterations; i++ {
				price := 1.08500 + (r.Float64()-0.5)*0.0050
				tick := model.Tick{
					Symbol:      "EURUSD",
					Bid:         price,
					Ask:         price + 0.0002,
					TimestampNs: time.Now().UnixNano(),
				}
				_ = tracker.OnTick(tick, 0.0010, 10000.0)
				_ = tracker.ActivePositions()
				_ = tracker.Count()
				opCount.Add(1)
			}
		}()
	}

	wg.Wait()
	t.Logf("Tracker stress test completed successfully (%d concurrent operations without race/panic)", opCount.Load())
}
