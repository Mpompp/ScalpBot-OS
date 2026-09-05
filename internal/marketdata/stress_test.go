package marketdata

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestRingBuffer_HighConcurrencyMultiPairStress(t *testing.T) {
	symbols := []string{"EURUSD", "GBPUSD", "USDJPY", "XAUUSD"}
	buffers := make(map[string]*TickRingBuffer, len(symbols))
	for _, sym := range symbols {
		buffers[sym] = NewTickRingBuffer(4096)
	}

	var wg sync.WaitGroup
	var totalPushed atomic.Int64
	numWorkers := 40
	ticksPerWorker := 5000

	// 1. Concurrent Producers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		workerID := w
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(workerID*1000) + time.Now().UnixNano()))
			for i := 0; i < ticksPerWorker; i++ {
				sym := symbols[i%len(symbols)]
				price := 1.0850 + (r.Float64()-0.5)*0.01
				tick := model.Tick{
					Symbol:      sym,
					Bid:         price,
					Ask:         price + 0.0002,
					TimestampNs: time.Now().UnixNano(),
				}
				buffers[sym].Push(tick)
				totalPushed.Add(1)
			}
		}()
	}

	// 2. Concurrent Consumers
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < ticksPerWorker; i++ {
				for _, sym := range symbols {
					_, _ = buffers[sym].Last()
					_ = buffers[sym].Latest(50)
				}
			}
		}()
	}

	wg.Wait()
	t.Logf("RingBuffer multi-pair stress test passed (%d total ticks pushed concurrently without panic)", totalPushed.Load())
}

func TestOHLCVAggregator_SameMillisecondAndGapTicks(t *testing.T) {
	agg := NewOHLCVAggregator(5 * time.Second)

	// Tick 1: Base
	c1, closed1 := agg.OnTick(model.Tick{Symbol: "EURUSD", Bid: 1.0850, Ask: 1.0852, TimestampNs: 1000000000})
	if closed1 {
		t.Fatalf("Unexpected candle close on tick 1")
	}

	// Tick 2: Same timestamp (0 elapsed)
	c2, closed2 := agg.OnTick(model.Tick{Symbol: "EURUSD", Bid: 1.0855, Ask: 1.0857, TimestampNs: 1000000000})
	if closed2 {
		t.Fatalf("Unexpected candle close on same timestamp")
	}

	// Tick 3: 6 seconds later (triggers close)
	c3, closed3 := agg.OnTick(model.Tick{Symbol: "EURUSD", Bid: 1.0840, Ask: 1.0842, TimestampNs: 7000000000})
	if !closed3 {
		t.Fatalf("Expected candle close after 6 seconds")
	}
	if c3.High != 1.0856 || c3.Low != 1.0851 {
		t.Errorf("Unexpected candle OHLC: %+v", c3)
	}

	_ = c1
	_ = c2
}
