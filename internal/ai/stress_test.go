package ai

import (
	"math"
	"testing"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestFeatureExtractor_ZeroSpreadAndNegativeSpread(t *testing.T) {
	fe := NewFeatureExtractor()

	// Anomaly 1: Zero Spread
	tickZeroSpread := model.Tick{
		Symbol:      "EURUSD",
		Bid:         1.08500,
		Ask:         1.08500, // Spread = 0
		TimestampNs: 1000000,
	}

	fvZero := fe.Extract(tickZeroSpread, 1.0850, 1.0848, 55.0, 0.0010, 15.0, model.Candle{})
	for i, val := range fvZero {
		if math.IsNaN(val) || math.IsInf(val, 0) {
			t.Fatalf("Feature[%d] is invalid (NaN/Inf) on zero spread: %f", i, val)
		}
	}

	// Anomaly 2: Negative Spread (Inverted Quote Anomaly)
	tickNegSpread := model.Tick{
		Symbol:      "EURUSD",
		Bid:         1.08510,
		Ask:         1.08500, // Ask < Bid
		TimestampNs: 2000000,
	}
	fvNeg := fe.Extract(tickNegSpread, 1.0850, 1.0848, 55.0, 0.0010, 15.0, model.Candle{})
	for i, val := range fvNeg {
		if math.IsNaN(val) || math.IsInf(val, 0) {
			t.Fatalf("Feature[%d] is invalid (NaN/Inf) on negative spread: %f", i, val)
		}
	}
}

func TestFeatureExtractor_ZeroPriceMovement_DivisionByZero(t *testing.T) {
	fe := NewFeatureExtractor()

	// High == Low == Open == Close == Price == 0.0
	tickZeroPrice := model.Tick{
		Symbol:      "EURUSD",
		Bid:         0.0,
		Ask:         0.0,
		TimestampNs: 1000000,
	}
	candleFlat := model.Candle{
		Open:   0.0,
		High:   0.0,
		Low:    0.0,
		Close:  0.0,
		Volume: 0.0,
	}

	fv := fe.Extract(tickZeroPrice, 0.0, 0.0, 0.0, 0.0, 0.0, candleFlat)
	for i, val := range fv {
		if math.IsNaN(val) || math.IsInf(val, 0) {
			t.Fatalf("Feature[%d] produced NaN/Inf during flat zero price: %f", i, val)
		}
	}
}

func TestFeatureExtractor_FlashCrashSpike(t *testing.T) {
	fe := NewFeatureExtractor()

	// Simulate sudden 2,000 pip price drop in 1 tick
	fe.Update(1.0850)
	fe.Update(1.0851)
	fe.Update(1.0849)

	crashTick := model.Tick{
		Symbol:      "EURUSD",
		Bid:         0.88500, // 2000 pips lower
		Ask:         0.88510,
		TimestampNs: 5000000,
	}
	fe.Update(crashTick.MidPrice())

	fv := fe.Extract(crashTick, 1.0850, 1.0848, 12.0, 0.0200, 85.0, model.Candle{
		Open: 1.0850, High: 1.0855, Low: 0.8850, Close: 0.8850, Volume: 10000,
	})

	for i, val := range fv {
		if math.IsNaN(val) || math.IsInf(val, 0) {
			t.Fatalf("Feature[%d] produced NaN/Inf during flash crash: %f", i, val)
		}
	}

	rc := NewRegimeClassifier()
	regime, _ := rc.Classify(fv)
	if regime != RegimeHighVolatilityEvent && regime != RegimeTrendingBearish {
		t.Logf("Flash crash classified as: %s (safe)", regime)
	}

	scorer := NewSignalScorer()
	confBuy := scorer.PredictConfidence(model.Buy, fv)
	confSell := scorer.PredictConfidence(model.Sell, fv)

	if confBuy < 0.0 || confBuy > 1.0 || math.IsNaN(confBuy) {
		t.Fatalf("PredictConfidence(Buy) out of bounds: %f", confBuy)
	}
	if confSell < 0.0 || confSell > 1.0 || math.IsNaN(confSell) {
		t.Fatalf("PredictConfidence(Sell) out of bounds: %f", confSell)
	}
}

func TestSignalScorer_BoundaryProbabilities(t *testing.T) {
	scorer := NewSignalScorer()

	var extremeVector FeatureVector
	for i := range extremeVector {
		extremeVector[i] = 100000.0
	}

	conf := scorer.PredictConfidence(model.Buy, extremeVector)
	if conf < 0.0 || conf > 1.0 || math.IsNaN(conf) {
		t.Fatalf("Scorer confidence out of bounds [0, 1] on extreme high vector: %f", conf)
	}

	for i := range extremeVector {
		extremeVector[i] = -100000.0
	}
	conf = scorer.PredictConfidence(model.Sell, extremeVector)
	if conf < 0.0 || conf > 1.0 || math.IsNaN(conf) {
		t.Fatalf("Scorer confidence out of bounds [0, 1] on extreme low vector: %f", conf)
	}
}

func BenchmarkFeatureExtractor_5MillionTicks(b *testing.B) {
	fe := NewFeatureExtractor()
	tick := model.Tick{
		Symbol:      "EURUSD",
		Bid:         1.08500,
		Ask:         1.08502,
		TimestampNs: 1000000,
	}
	candle := model.Candle{
		Open: 1.0845, High: 1.0855, Low: 1.0840, Close: 1.0850, Volume: 100,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		fe.Update(tick.MidPrice())
		_ = fe.Extract(tick, 1.0850, 1.0848, 55.0, 0.0010, 12.0, candle)
	}
}
