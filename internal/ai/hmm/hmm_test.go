package hmm

import (
	"math"
	"testing"
)

// =============================================================================
// Standar 1: Deteksi Sideways / Flat Market (Noise Filter)
// Input:  LogReturn=0.00002, RelATR=0.0003, VolumeImbalance=0.02
// Target: StateNoise dengan confidence >= 70%
// =============================================================================
func TestStandar1_SidewaysFlat_NoiseDetection(t *testing.T) {
	h := NewGaussianHMM()

	obs := ObservationVector{0.00002, 0.0003, 0.02}

	var st MarketState
	var conf float64
	for i := 0; i < 15; i++ {
		st, conf = h.Update(obs)
		t.Logf("step %2d: state=%-15s conf=%.4f", i, st.String(), conf)
	}

	if st != StateNoise {
		t.Fatalf("STANDAR 1 FAIL: expected StateNoise, got %v (conf=%.4f)", st, conf)
	}
	if conf < 0.70 {
		t.Errorf("STANDAR 1 FAIL: expected Noise conf >= 0.70, got %.4f", conf)
	}
	pN, pB, pBr := h.Posterior()
	t.Logf("✅ STANDAR 1 PASS: state=%s conf=%.4f [N=%.4f B=%.4f Br=%.4f]",
		st.String(), conf, pN, pB, pBr)
}

// =============================================================================
// Standar 2: Deteksi Momentum Bullish M1 Normal (Responsiveness)
// Input:  LogReturn=+0.00045, RelATR=0.0008, VolumeImbalance=+0.30
// Target: StateBull dengan confidence >= 70%
// =============================================================================
func TestStandar2_BullishM1_Responsiveness(t *testing.T) {
	h := NewGaussianHMM()

	obs := ObservationVector{+0.00045, 0.0008, +0.30}

	var st MarketState
	var conf float64
	for i := 0; i < 10; i++ {
		st, conf = h.Update(obs)
		t.Logf("step %2d: state=%-15s conf=%.4f", i, st.String(), conf)
	}

	if st != StateBull {
		t.Fatalf("STANDAR 2 FAIL: expected StateBull, got %v (conf=%.4f)", st, conf)
	}
	if conf < 0.70 {
		t.Errorf("STANDAR 2 FAIL: expected Bull conf >= 0.70, got %.4f", conf)
	}
	pN, pB, pBr := h.Posterior()
	t.Logf("✅ STANDAR 2 PASS: state=%s conf=%.4f [N=%.4f B=%.4f Br=%.4f]",
		st.String(), conf, pN, pB, pBr)
}

// =============================================================================
// Standar 3: Deteksi Momentum Bearish M1 Normal (Responsiveness)
// Input:  LogReturn=-0.00045, RelATR=0.0008, VolumeImbalance=-0.30
// Target: StateBear dengan confidence >= 70%
// =============================================================================
func TestStandar3_BearishM1_Responsiveness(t *testing.T) {
	h := NewGaussianHMM()

	obs := ObservationVector{-0.00045, 0.0008, -0.30}

	var st MarketState
	var conf float64
	for i := 0; i < 10; i++ {
		st, conf = h.Update(obs)
		t.Logf("step %2d: state=%-15s conf=%.4f", i, st.String(), conf)
	}

	if st != StateBear {
		t.Fatalf("STANDAR 3 FAIL: expected StateBear, got %v (conf=%.4f)", st, conf)
	}
	if conf < 0.70 {
		t.Errorf("STANDAR 3 FAIL: expected Bear conf >= 0.70, got %.4f", conf)
	}
	pN, pB, pBr := h.Posterior()
	t.Logf("✅ STANDAR 3 PASS: state=%s conf=%.4f [N=%.4f B=%.4f Br=%.4f]",
		st.String(), conf, pN, pB, pBr)
}

// =============================================================================
// Standar Transisi: Full Lifecycle Bull -> Noise -> Bear
// =============================================================================
func TestStandar_TransitionLifecycle(t *testing.T) {
	h := NewGaussianHMM()

	// Phase 1: Bullish
	var st MarketState
	for i := 0; i < 10; i++ {
		st, _ = h.Update(ObservationVector{+0.00045, 0.0008, +0.30})
	}
	if st != StateBull {
		t.Fatalf("Phase 1: expected StateBull, got %v", st)
	}
	pN, pB, pBr := h.Posterior()
	t.Logf("Phase 1 (Bull): N=%.4f B=%.4f Br=%.4f", pN, pB, pBr)

	// Phase 2: Cooldown to Noise
	for i := 0; i < 12; i++ {
		st, _ = h.Update(ObservationVector{0.00002, 0.0003, 0.02})
	}
	if st != StateNoise {
		t.Fatalf("Phase 2: expected StateNoise, got %v", st)
	}
	pN, pB, pBr = h.Posterior()
	t.Logf("Phase 2 (Noise): N=%.4f B=%.4f Br=%.4f", pN, pB, pBr)

	// Phase 3: Bearish plunge
	for i := 0; i < 10; i++ {
		st, _ = h.Update(ObservationVector{-0.00045, 0.0008, -0.30})
	}
	if st != StateBear {
		t.Fatalf("Phase 3: expected StateBear, got %v", st)
	}
	pN, pB, pBr = h.Posterior()
	t.Logf("Phase 3 (Bear): N=%.4f B=%.4f Br=%.4f", pN, pB, pBr)
}

// =============================================================================
// Anti-Saturation: Extreme volume + flat price must NOT lock to Bull 99%
// =============================================================================
func TestStandar_AntiSaturation(t *testing.T) {
	h := NewGaussianHMM()

	for i := 0; i < 15; i++ {
		obs := ObservationVector{0.00001, 0.0004, 0.95}
		st, conf := h.Update(obs)
		t.Logf("step %2d: state=%-15s conf=%.4f", i, st.String(), conf)
	}

	_, conf := h.Update(ObservationVector{0.00001, 0.0004, 0.95})
	pN, pB, pBr := h.Posterior()
	t.Logf("Final: N=%.4f B=%.4f Br=%.4f (max_conf=%.4f)", pN, pB, pBr, conf)

	if conf > 0.95 {
		t.Errorf("SATURATION BUG: conf=%.4f > 0.95 — extreme volume dominating", conf)
	}
}

// =============================================================================
// Numerical Safety: No NaN/Inf in posteriors, sum always equals 1.0
// =============================================================================
func TestStandar_NumericalSafety(t *testing.T) {
	h := NewGaussianHMM()

	edgeCases := []ObservationVector{
		{0.0, 0.0, 0.0},
		{0.1, 0.1, 0.99},
		{-0.1, 0.1, -0.99},
		{0.0001, 0.00001, 0.5},
		{-0.05, 0.05, 0.0},
	}

	for i, obs := range edgeCases {
		st, conf := h.Update(obs)
		if math.IsNaN(conf) || math.IsInf(conf, 0) {
			t.Fatalf("step %d: NaN/Inf conf for obs=%v (state=%v)", i, obs, st)
		}
		pN, pB, pBr := h.Posterior()
		sum := pN + pB + pBr
		if math.Abs(sum-1.0) > 1e-6 {
			t.Errorf("step %d: posterior sum=%.8f != 1.0", i, sum)
		}
	}
}

// =============================================================================
// Standar 4: Zero-Allocation & Sub-Microsecond Benchmark
// Target: 0 allocs/op, 0 B/op, < 60 ns/op
// =============================================================================
func BenchmarkHMMUpdate(b *testing.B) {
	h := NewGaussianHMM()
	obs := ObservationVector{0.00045, 0.0008, 0.25}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		h.Update(obs)
	}
}
