package ai

import (
	"math"
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func makeTestTick(bid, ask float64) model.Tick {
	return model.Tick{
		Symbol:      "EURUSD",
		Bid:         bid,
		Ask:         ask,
		TimestampNs: time.Now().UnixNano(),
	}
}

func TestFeatureExtractor_Extract(t *testing.T) {
	fe := NewFeatureExtractor()
	// Fill some history
	for i := 0; i < 25; i++ {
		fe.Update(1.1000 + float64(i)*0.00005)
	}

	tick := makeTestTick(1.1012, 1.1014)
	candle := model.Candle{
		Open:  1.1005,
		High:  1.1015,
		Low:   1.1002,
		Close: 1.1013,
	}

	fv := fe.Extract(tick, 1.1013, 1.1008, 62.5, 0.0004, 5.0, candle)

	// Verify all features are valid non-NaN numbers
	for i := 0; i < NumFeatures; i++ {
		if math.IsNaN(fv[i]) || math.IsInf(fv[i], 0) {
			t.Errorf("feature[%d] produced NaN or Inf: %.5f", i, fv[i])
		}
	}

	// Verify EMA delta is positive (1.1013 > 1.1008)
	if fv[FeatEMADelta] <= 0 {
		t.Errorf("expected positive EMA delta, got %.5f", fv[FeatEMADelta])
	}

	// Verify RSI norm (62.5 -> (62.5-50)/50 = 0.25)
	if fv[FeatRSINorm] < 0.24 || fv[FeatRSINorm] > 0.26 {
		t.Errorf("expected RSI norm ~0.25, got %.5f", fv[FeatRSINorm])
	}
}

func TestRegimeClassifier_Regimes(t *testing.T) {
	rc := NewRegimeClassifier()

	// 1. Trending Bullish Feature Vector
	var fvBullish FeatureVector
	fvBullish[FeatEMADelta] = 1.5
	fvBullish[FeatRSINorm] = 0.35
	fvBullish[FeatVolRatio] = 1.1
	fvBullish[FeatSpreadRatio] = 0.2

	regime, conf := rc.Classify(fvBullish)
	if regime != RegimeTrendingBullish {
		t.Errorf("expected RegimeTrendingBullish, got %s", regime)
	}
	if conf < 0.5 {
		t.Errorf("expected confidence >= 0.5, got %.2f", conf)
	}

	// 2. Trending Bearish Feature Vector
	var fvBearish FeatureVector
	fvBearish[FeatEMADelta] = -1.5
	fvBearish[FeatRSINorm] = -0.35
	fvBearish[FeatVolRatio] = 1.1
	fvBearish[FeatSpreadRatio] = 0.2

	regime, conf = rc.Classify(fvBearish)
	if regime != RegimeTrendingBearish {
		t.Errorf("expected RegimeTrendingBearish, got %s", regime)
	}

	// 3. Ranging Chop Feature Vector (flat EMA delta, RSI near 50)
	var fvChop FeatureVector
	fvChop[FeatEMADelta] = 0.1
	fvChop[FeatRSINorm] = 0.05
	fvChop[FeatVolRatio] = 0.9
	fvChop[FeatSpreadRatio] = 0.4

	regime, _ = rc.Classify(fvChop)
	if regime != RegimeRangingChop {
		t.Errorf("expected RegimeRangingChop, got %s", regime)
	}

	// 4. Volatility Event
	var fvEvent FeatureVector
	fvEvent[FeatVolRatio] = 3.5 // Extreme volatility
	regime, _ = rc.Classify(fvEvent)
	if regime != RegimeHighVolatilityEvent {
		t.Errorf("expected RegimeHighVolatilityEvent, got %s", regime)
	}
}

func TestSignalScorer_Confidence(t *testing.T) {
	scorer := NewSignalScorer()

	// Strong Buy Setup
	var fvStrongBuy FeatureVector
	fvStrongBuy[FeatEMADelta] = 1.2
	fvStrongBuy[FeatRSINorm] = 0.30
	fvStrongBuy[FeatSpreadRatio] = 0.2
	fvStrongBuy[FeatTickVelocity] = 0.6
	fvStrongBuy[FeatPriceMomShort] = 0.5
	fvStrongBuy[FeatPriceMomMedium] = 1.0
	fvStrongBuy[FeatVolRatio] = 1.2
	fvStrongBuy[FeatBodyRatio] = 0.70
	fvStrongBuy[FeatUpperWickRatio] = 0.10
	fvStrongBuy[FeatDirectionalConsistency] = 0.6

	buyProb := scorer.PredictConfidence(model.Buy, fvStrongBuy)
	if buyProb < 0.70 {
		t.Errorf("expected high BUY confidence >= 0.70, got %.2f", buyProb)
	}

	// Counter-trend Sell on the same bullish setup should have low score
	sellProb := scorer.PredictConfidence(model.Sell, fvStrongBuy)
	if sellProb > 0.40 {
		t.Errorf("expected low SELL confidence <= 0.40 on bullish setup, got %.2f", sellProb)
	}
}

func TestSignalFilter_EndToEnd(t *testing.T) {
	cfg := DefaultFilterConfig()
	cfg.MinConfidence = 0.70
	filter := NewSignalFilter(cfg)

	// Feed history
	for i := 0; i < 20; i++ {
		filter.OnTick(makeTestTick(1.1000+float64(i)*0.00005, 1.1002+float64(i)*0.00005))
	}

	tick := makeTestTick(1.1010, 1.1012)
	candle := model.Candle{
		Open:      1.1000,
		High:      1.1012,
		Low:       1.0998,
		Close:     1.1010,
		BuyerVol:  45,
		SellerVol: 10,
	}

	// Feed Bullish expansion observation into HMM
	for i := 0; i < 4; i++ {
		c := model.Candle{
			Close:     1.1000 + float64(i)*0.0010,
			BuyerVol:  50,
			SellerVol: 10,
		}
		filter.UpdateHMM(c, 0.0010)
	}

	sig := model.Signal{
		Symbol: "EURUSD",
		Type:   model.Buy,
	}

	// 1. Strong Bullish Expansion -> Should Allow
	allowed, conf, regime, reason := filter.EvaluateSignal(
		sig, tick, 1.1012, 1.1006, 60.0, 0.0005, 4.0, candle,
	)
	if !allowed {
		t.Fatalf("expected allowed for strong bullish signal, rejected with: %s (conf=%.2f, regime=%s)",
			reason, conf, regime)
	}

	// 2. Feed Noise / Flat market -> Should Reject
	for i := 0; i < 8; i++ {
		c := model.Candle{
			Close:     1.1040,
			BuyerVol:  20,
			SellerVol: 20,
		}
		filter.UpdateHMM(c, 0.0004)
	}
	allowed2, _, regime2, _ := filter.EvaluateSignal(
		sig, tick, 1.10101, 1.10100, 50.5, 0.0005, 1.0, candle,
	)
	if allowed2 {
		t.Errorf("expected rejection during ranging chop, got allowed (regime=%s)", regime2)
	}
}

func BenchmarkFeatureExtraction(b *testing.B) {
	fe := NewFeatureExtractor()
	for i := 0; i < 30; i++ {
		fe.Update(1.1000 + float64(i)*0.00002)
	}

	tick := makeTestTick(1.1006, 1.1008)
	candle := model.Candle{Open: 1.1002, High: 1.1007, Low: 1.1001, Close: 1.1006}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = fe.Extract(tick, 1.1006, 1.1003, 58.0, 0.0004, 3.5, candle)
	}
}

func BenchmarkSignalScorer(b *testing.B) {
	scorer := NewSignalScorer()
	var fv FeatureVector
	fv[FeatEMADelta] = 1.0
	fv[FeatRSINorm] = 0.25
	fv[FeatBodyRatio] = 0.65
	fv[FeatVolRatio] = 1.1

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = scorer.PredictConfidence(model.Buy, fv)
	}
}

func BenchmarkAIFilter_EvaluateSignal(b *testing.B) {
	filter := NewSignalFilter(DefaultFilterConfig())
	for i := 0; i < 20; i++ {
		filter.OnTick(makeTestTick(1.1000, 1.1002))
	}

	tick := makeTestTick(1.1008, 1.1010)
	candle := model.Candle{Open: 1.1002, High: 1.1009, Low: 1.1001, Close: 1.1008}
	sig := model.Signal{Symbol: "EURUSD", Type: model.Buy}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = filter.EvaluateSignal(sig, tick, 1.1008, 1.1004, 58.0, 0.0004, 3.5, candle)
	}
}
