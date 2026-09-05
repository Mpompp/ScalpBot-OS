package marketdata

import (
	"sync"
	"testing"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestRingBuffer_PushAndLast(t *testing.T) {
	rb := NewTickRingBuffer(3)

	if _, ok := rb.Last(); ok {
		t.Fatal("expected empty buffer to return false")
	}

	tick1 := model.Tick{Symbol: "EURUSD", Bid: 1.1000, Ask: 1.1002}
	rb.Push(tick1)

	got, ok := rb.Last()
	if !ok {
		t.Fatal("expected Last to return true after push")
	}
	if got.Bid != 1.1000 {
		t.Errorf("expected bid 1.1000, got %f", got.Bid)
	}
}

func TestRingBuffer_WrapAround(t *testing.T) {
	rb := NewTickRingBuffer(3)

	// Push 5 ticks into a buffer of size 3
	for i := 0; i < 5; i++ {
		rb.Push(model.Tick{Bid: float64(i)})
	}

	if rb.Len() != 3 {
		t.Errorf("expected len 3, got %d", rb.Len())
	}

	// Last should be tick 4 (most recent)
	last, _ := rb.Last()
	if last.Bid != 4.0 {
		t.Errorf("expected last bid 4.0, got %f", last.Bid)
	}

	// Latest(3) should return ticks 2, 3, 4 (chronological order)
	latest := rb.Latest(3)
	defer ReleaseSlice(latest)

	if len(latest) != 3 {
		t.Fatalf("expected 3 ticks, got %d", len(latest))
	}
	if latest[0].Bid != 2.0 {
		t.Errorf("expected oldest bid 2.0, got %f", latest[0].Bid)
	}
	if latest[2].Bid != 4.0 {
		t.Errorf("expected newest bid 4.0, got %f", latest[2].Bid)
	}
}

func TestRingBuffer_LatestMoreThanCount(t *testing.T) {
	rb := NewTickRingBuffer(10)
	rb.Push(model.Tick{Bid: 1.0})
	rb.Push(model.Tick{Bid: 2.0})

	latest := rb.Latest(100) // Request more than available
	defer ReleaseSlice(latest)

	if len(latest) != 2 {
		t.Errorf("expected 2 ticks, got %d", len(latest))
	}
}

func TestRingBuffer_LatestEmpty(t *testing.T) {
	rb := NewTickRingBuffer(10)
	latest := rb.Latest(5)
	if latest != nil {
		t.Errorf("expected nil for empty buffer, got %v", latest)
	}
}

func TestRingBuffer_SnapshotLast(t *testing.T) {
	rb := NewTickRingBuffer(10)

	var dst model.Tick
	if rb.SnapshotLast(&dst) {
		t.Fatal("expected false for empty buffer")
	}

	rb.Push(model.Tick{Symbol: "GBPUSD", Bid: 1.25})
	if !rb.SnapshotLast(&dst) {
		t.Fatal("expected true after push")
	}
	if dst.Symbol != "GBPUSD" {
		t.Errorf("expected GBPUSD, got %s", dst.Symbol)
	}
}

func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	rb := NewTickRingBuffer(100)
	var wg sync.WaitGroup

	// Concurrent writers
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				rb.Push(model.Tick{Bid: float64(id*1000 + i)})
			}
		}(w)
	}

	// Concurrent readers
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				rb.Last()
				s := rb.Latest(10)
				ReleaseSlice(s)
				rb.Len()
			}
		}()
	}

	wg.Wait()

	// Just ensure no panic or deadlock
	if rb.Len() == 0 {
		t.Error("expected non-zero length after concurrent writes")
	}
}

func BenchmarkRingBufferPush(b *testing.B) {
	rb := NewTickRingBuffer(4096)
	tick := model.Tick{Symbol: "EURUSD", Bid: 1.1000, Ask: 1.1002}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rb.Push(tick)
	}
}

func BenchmarkRingBufferLast(b *testing.B) {
	rb := NewTickRingBuffer(4096)
	for i := 0; i < 4096; i++ {
		rb.Push(model.Tick{Bid: float64(i)})
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rb.Last()
	}
}
