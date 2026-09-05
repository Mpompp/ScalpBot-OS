package indicator

import (
	"testing"
)

func TestBollingerBands(t *testing.T) {
	bb := NewBollinger(5, 2.0)

	prices := []float64{10, 11, 12, 11, 10}
	var res BollingerResult
	for _, p := range prices {
		res = bb.Update(p)
	}

	if !res.Valid {
		t.Fatalf("expected valid result after 5 samples")
	}

	// Mean of 10, 11, 12, 11, 10 is 10.8
	if res.Middle < 10.79 || res.Middle > 10.81 {
		t.Errorf("expected middle ~10.8, got %f", res.Middle)
	}

	if res.Upper <= res.Middle || res.Lower >= res.Middle {
		t.Errorf("invalid bands: upper=%f, middle=%f, lower=%f", res.Upper, res.Middle, res.Lower)
	}

	// Update with extreme price (15) to check PercentB
	res2 := bb.Update(15.0)
	if res2.PercentB <= 0.5 {
		t.Errorf("expected high PercentB on price spike, got %f", res2.PercentB)
	}
}

func BenchmarkBollingerUpdate(b *testing.B) {
	bb := NewBollinger(20, 2.0)
	for i := 0; i < 20; i++ {
		bb.Update(1.1000 + float64(i)*0.0001)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bb.Update(1.1020)
	}
}
