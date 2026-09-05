package indicator

import (
	"math"
	"testing"
)

func TestSMA(t *testing.T) {
	sma := NewSMA(3)
	if sma.Ready() {
		t.Errorf("expected not ready initially")
	}

	v1 := sma.Update(10.0)
	if math.Abs(v1-10.0) > 1e-6 {
		t.Errorf("expected 10.0, got %f", v1)
	}

	v2 := sma.Update(20.0)
	if math.Abs(v2-15.0) > 1e-6 {
		t.Errorf("expected 15.0, got %f", v2)
	}

	v3 := sma.Update(30.0)
	if !sma.Ready() {
		t.Errorf("expected ready after 3 values")
	}
	if math.Abs(v3-20.0) > 1e-6 {
		t.Errorf("expected 20.0, got %f", v3)
	}

	v4 := sma.Update(40.0) // window: [20, 30, 40] -> avg = 30.0
	if math.Abs(v4-30.0) > 1e-6 {
		t.Errorf("expected 30.0, got %f", v4)
	}
}
