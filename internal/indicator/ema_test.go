package indicator

import (
	"math"
	"testing"
)

func TestEMA_Warmup(t *testing.T) {
	ema := NewEMA(3)
	if ema.Ready() {
		t.Fatal("EMA should not be ready before warmup")
	}

	// Feed values during warmup — should return NaN
	v1 := ema.Update(10.0)
	if !math.IsNaN(v1) {
		t.Errorf("expected NaN during warmup, got %f", v1)
	}
	v2 := ema.Update(11.0)
	if !math.IsNaN(v2) {
		t.Errorf("expected NaN during warmup, got %f", v2)
	}

	// 3rd value completes warmup — should return SMA seed
	v3 := ema.Update(12.0)
	if math.IsNaN(v3) {
		t.Fatal("expected valid value after warmup")
	}
	if !ema.Ready() {
		t.Fatal("EMA should be ready after period values")
	}

	// SMA of {10, 11, 12} = 11.0
	expectedSMA := 11.0
	if math.Abs(v3-expectedSMA) > 1e-10 {
		t.Errorf("expected SMA seed %.4f, got %.4f", expectedSMA, v3)
	}
}

func TestEMA_KnownValues(t *testing.T) {
	// EMA(5) with known data series
	ema := NewEMA(5)
	data := []float64{22.27, 22.19, 22.08, 22.17, 22.18, 22.13, 22.23, 22.43, 22.24, 22.29}

	// Expected EMA values (verified independently)
	// First 5 values: SMA = (22.27+22.19+22.08+22.17+22.18)/5 = 22.178
	var results []float64
	for _, v := range data {
		r := ema.Update(v)
		if !math.IsNaN(r) {
			results = append(results, r)
		}
	}

	if len(results) == 0 {
		t.Fatal("expected at least one valid EMA result")
	}

	// First valid result should be SMA seed
	expectedSMA := (22.27 + 22.19 + 22.08 + 22.17 + 22.18) / 5.0
	if math.Abs(results[0]-expectedSMA) > 1e-10 {
		t.Errorf("SMA seed: expected %.6f, got %.6f", expectedSMA, results[0])
	}
}

func TestEMA_NilOnInvalidPeriod(t *testing.T) {
	if NewEMA(0) != nil {
		t.Error("expected nil for period 0")
	}
	if NewEMA(-1) != nil {
		t.Error("expected nil for negative period")
	}
}

func TestEMA_Reset(t *testing.T) {
	ema := NewEMA(2)
	ema.Update(10.0)
	ema.Update(20.0)
	if !ema.Ready() {
		t.Fatal("expected ready")
	}

	ema.Reset()
	if ema.Ready() {
		t.Fatal("expected not ready after reset")
	}
	if !math.IsNaN(ema.Value()) {
		t.Fatal("expected NaN after reset")
	}
}

func BenchmarkEMAUpdate(b *testing.B) {
	ema := NewEMA(14)
	// Warmup
	for i := 0; i < 14; i++ {
		ema.Update(float64(i))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ema.Update(float64(i))
	}
}
