package indicator

import (
	"math"
	"testing"
)

func TestRSI_Warmup(t *testing.T) {
	rsi := NewRSI(14)
	if rsi.Ready() {
		t.Fatal("RSI should not be ready before warmup")
	}

	// Need 15 values (14 changes) to complete warmup
	for i := 0; i < 14; i++ {
		v := rsi.Update(float64(40 + i))
		if !math.IsNaN(v) {
			t.Errorf("expected NaN at step %d during warmup, got %f", i, v)
		}
	}

	// 15th value should produce valid RSI
	v := rsi.Update(55.0)
	if math.IsNaN(v) {
		t.Fatal("expected valid RSI after warmup")
	}
	if !rsi.Ready() {
		t.Fatal("RSI should be ready after warmup")
	}
}

func TestRSI_AllGains(t *testing.T) {
	rsi := NewRSI(5)
	// Feed strictly increasing prices
	for i := 0; i < 20; i++ {
		rsi.Update(float64(10 + i))
	}
	val := rsi.Value()
	if val != 100.0 {
		t.Errorf("expected RSI=100 for all gains, got %.2f", val)
	}
}

func TestRSI_AllLosses(t *testing.T) {
	rsi := NewRSI(5)
	// Feed strictly decreasing prices
	for i := 0; i < 20; i++ {
		rsi.Update(float64(100 - i))
	}
	val := rsi.Value()
	if val != 0.0 {
		t.Errorf("expected RSI=0 for all losses, got %.2f", val)
	}
}

func TestRSI_NoMovement(t *testing.T) {
	rsi := NewRSI(5)
	for i := 0; i < 20; i++ {
		rsi.Update(50.0) // Flat price
	}
	val := rsi.Value()
	if val != 50.0 {
		t.Errorf("expected RSI=50 for no movement, got %.2f", val)
	}
}

func TestRSI_Range(t *testing.T) {
	rsi := NewRSI(14)
	// Random-ish data
	data := []float64{44, 44.34, 44.09, 43.61, 44.33, 44.83, 45.10, 45.42, 45.84, 46.08,
		45.89, 46.03, 45.61, 46.28, 46.28, 46.00, 46.03, 46.41, 46.22, 45.64}
	for _, v := range data {
		rsi.Update(v)
	}
	val := rsi.Value()
	if val < 0 || val > 100 {
		t.Errorf("RSI out of range [0,100]: got %.2f", val)
	}
}

func TestRSI_NilOnInvalidPeriod(t *testing.T) {
	if NewRSI(0) != nil {
		t.Error("expected nil for period 0")
	}
}

func BenchmarkRSIUpdate(b *testing.B) {
	rsi := NewRSI(14)
	for i := 0; i < 15; i++ {
		rsi.Update(float64(40 + i))
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rsi.Update(float64(50 + i%10))
	}
}
