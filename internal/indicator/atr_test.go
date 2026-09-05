package indicator

import (
	"math"
	"testing"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestATR_Warmup(t *testing.T) {
	atr := NewATR(3)
	if atr.Ready() {
		t.Fatal("ATR should not be ready before warmup")
	}

	candles := []model.Candle{
		{High: 1.1010, Low: 1.0990, Close: 1.1000}, // TR = 0.0020
		{High: 1.1030, Low: 1.1000, Close: 1.1020}, // TR = max(0.0030, 0.0030, 0.0000) = 0.0030
		{High: 1.1050, Low: 1.1010, Close: 1.1040}, // TR = max(0.0040, 0.0030, 0.0010) = 0.0040
	}

	for i, c := range candles {
		v := atr.Update(c)
		if i < 2 {
			if !math.IsNaN(v) {
				t.Errorf("expected NaN at candle %d, got %f", i, v)
			}
		}
	}

	if !atr.Ready() {
		t.Fatal("ATR should be ready after 3 candles")
	}
}

func TestATR_KnownValue(t *testing.T) {
	atr := NewATR(3)

	candles := []model.Candle{
		{High: 1.1010, Low: 1.0990, Close: 1.1000},
		{High: 1.1030, Low: 1.1000, Close: 1.1020},
		{High: 1.1050, Low: 1.1010, Close: 1.1040},
	}

	var result float64
	for _, c := range candles {
		result = atr.Update(c)
	}

	// TR values: 0.0020, 0.0030, 0.0040
	// Average = (0.0020 + 0.0030 + 0.0040) / 3 = 0.003
	expected := 0.003
	if math.Abs(result-expected) > 1e-10 {
		t.Errorf("expected ATR %.6f, got %.6f", expected, result)
	}
}

func TestATR_NilOnInvalidPeriod(t *testing.T) {
	if NewATR(0) != nil {
		t.Error("expected nil for period 0")
	}
}

func TestATR_SinglePeriod(t *testing.T) {
	atr := NewATR(1)
	c := model.Candle{High: 1.1010, Low: 1.0990, Close: 1.1000}
	v := atr.Update(c)

	if math.IsNaN(v) {
		t.Fatal("ATR(1) should be ready after 1 candle")
	}
	expected := 0.0020
	if math.Abs(v-expected) > 1e-10 {
		t.Errorf("expected %.6f, got %.6f", expected, v)
	}
}

func BenchmarkATRUpdate(b *testing.B) {
	atr := NewATR(14)
	// Warmup
	for i := 0; i < 14; i++ {
		atr.Update(model.Candle{
			High:  1.1 + float64(i)*0.001,
			Low:   1.09 + float64(i)*0.001,
			Close: 1.095 + float64(i)*0.001,
		})
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		atr.Update(model.Candle{
			High:  1.1 + float64(i%100)*0.0001,
			Low:   1.09 + float64(i%100)*0.0001,
			Close: 1.095 + float64(i%100)*0.0001,
		})
	}
}
