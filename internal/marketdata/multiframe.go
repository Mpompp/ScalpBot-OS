package marketdata

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// Timeframe represents candle interval duration.
type Timeframe time.Duration

const (
	TimeframeM1 Timeframe = Timeframe(time.Minute)
	TimeframeM5 Timeframe = Timeframe(5 * time.Minute)
	TimeframeH1 Timeframe = Timeframe(time.Hour)
)

// HTFTrend represents higher timeframe trend direction.
type HTFTrend string

const (
	HTFTrendBullish HTFTrend = "BULLISH"
	HTFTrendBearish HTFTrend = "BEARISH"
	HTFTrendNeutral HTFTrend = "NEUTRAL"
)

// SymbolTimeframes tracks multi-timeframe candle streams for a single symbol.
type SymbolTimeframes struct {
	symbol string
	mu     sync.RWMutex

	// Rolling candles
	m1Candles []model.Candle
	m5Candles []model.Candle
	h1Candles []model.Candle

	// Current in-progress candle
	currentM1 *model.Candle
	currentM5 *model.Candle
	currentH1 *model.Candle

	// Max historical bars to retain
	maxHistory int

	// Cached Indicators
	m5FastEMA float64 // M5 20-period EMA
	m5SlowEMA float64 // M5 50-period EMA
	h1EMA     float64 // H1 50-period EMA
	trendM5   HTFTrend
	trendH1   HTFTrend
}

// NewSymbolTimeframes creates a multi-timeframe tracker for a symbol.
func NewSymbolTimeframes(symbol string, maxHistory int) *SymbolTimeframes {
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &SymbolTimeframes{
		symbol:     symbol,
		maxHistory: maxHistory,
		trendM5:    HTFTrendNeutral,
		trendH1:    HTFTrendNeutral,
	}
}

// OnTick processes an incoming tick into M1, M5, and H1 candle aggregators.
func (st *SymbolTimeframes) OnTick(tick model.Tick) {
	st.mu.Lock()
	defer st.mu.Unlock()

	price := tick.MidPrice()
	if price <= 0 {
		return
	}

	t := time.Unix(0, tick.TimestampNs).UTC()

	// 1. Process M1
	st.processCandle(&st.currentM1, &st.m1Candles, price, t, time.Minute)

	// 2. Process M5
	closedM5 := st.processCandle(&st.currentM5, &st.m5Candles, price, t, 5*time.Minute)
	if closedM5 {
		st.recalculateM5Indicators()
	}

	// 3. Process H1
	closedH1 := st.processCandle(&st.currentH1, &st.h1Candles, price, t, time.Hour)
	if closedH1 {
		st.recalculateH1Indicators()
	}
}

func (st *SymbolTimeframes) processCandle(
	current **model.Candle,
	history *[]model.Candle,
	price float64,
	t time.Time,
	interval time.Duration,
) bool {
	periodStart := t.Truncate(interval)
	periodStartNs := periodStart.UnixNano()

	if *current == nil {
		*current = &model.Candle{
			Symbol:      st.symbol,
			Open:        price,
			High:        price,
			Low:         price,
			Close:       price,
			TimestampNs: periodStartNs,
		}
		return false
	}

	c := *current
	if c.TimestampNs == periodStartNs {
		// Update current active candle
		if price > c.High {
			c.High = price
		}
		if price < c.Low {
			c.Low = price
		}
		c.Close = price
		return false
	}

	// Candle period ended: push closed candle to history
	*history = append(*history, *c)
	if len(*history) > st.maxHistory {
		*history = (*history)[len(*history)-st.maxHistory:]
	}

	// Start new candle
	*current = &model.Candle{
		Symbol:      st.symbol,
		Open:        price,
		High:        price,
		Low:         price,
		Close:       price,
		TimestampNs: periodStartNs,
	}
	return true
}

func (st *SymbolTimeframes) recalculateM5Indicators() {
	if len(st.m5Candles) < 5 {
		return
	}

	// Calculate EMA 10 & 25 on M5
	fastK := 2.0 / (10.0 + 1.0)
	slowK := 2.0 / (25.0 + 1.0)

	lastClose := st.m5Candles[len(st.m5Candles)-1].Close

	if st.m5FastEMA == 0 || st.m5SlowEMA == 0 {
		var fast, slow float64
		for i, c := range st.m5Candles {
			if i == 0 {
				fast = c.Close
				slow = c.Close
				continue
			}
			fast = (c.Close * fastK) + (fast * (1.0 - fastK))
			slow = (c.Close * slowK) + (slow * (1.0 - slowK))
		}
		st.m5FastEMA = fast
		st.m5SlowEMA = slow
	} else {
		// Incremental O(1) update on new closed candle
		st.m5FastEMA = (lastClose * fastK) + (st.m5FastEMA * (1.0 - fastK))
		st.m5SlowEMA = (lastClose * slowK) + (st.m5SlowEMA * (1.0 - slowK))
	}

	if st.m5FastEMA > st.m5SlowEMA*1.0002 {
		st.trendM5 = HTFTrendBullish
	} else if st.m5FastEMA < st.m5SlowEMA*0.9998 {
		st.trendM5 = HTFTrendBearish
	} else {
		st.trendM5 = HTFTrendNeutral
	}
}

func (st *SymbolTimeframes) recalculateH1Indicators() {
	if len(st.h1Candles) < 3 {
		return
	}

	lastCandle := st.h1Candles[len(st.h1Candles)-1]
	prevCandle := st.h1Candles[len(st.h1Candles)-2]

	if lastCandle.Close > prevCandle.High {
		st.trendH1 = HTFTrendBullish
	} else if lastCandle.Close < prevCandle.Low {
		st.trendH1 = HTFTrendBearish
	} else {
		st.trendH1 = HTFTrendNeutral
	}
}

// EvaluateConfluence checks if a micro-scalp signal is aligned with Higher Timeframe trend.
func (st *SymbolTimeframes) EvaluateConfluence(sigType model.SignalType) (allowed bool, score float64, reason string) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	// If warm-up data is limited (< 5 M5 bars), approve with standard neutral edge
	if len(st.m5Candles) < 3 {
		return true, 0.55, "HTF Warming Up (Neutral Alignment)"
	}

	m5Trend := st.trendM5
	h1Trend := st.trendH1

	switch sigType {
	case model.Buy:
		// Hard conflict: M5 and H1 are both Bearish
		if m5Trend == HTFTrendBearish && h1Trend == HTFTrendBearish {
			return false, 0.20, "HTF Conflict (M5 & H1 Bearish Downtrend)"
		}
		// Strong alignment: M5 is Bullish
		if m5Trend == HTFTrendBullish {
			score = 0.85
			if h1Trend == HTFTrendBullish {
				score = 0.95
			}
			return true, score, fmt.Sprintf("HTF Confluence Aligned (M5=%s, H1=%s)", m5Trend, h1Trend)
		}
		return true, 0.60, fmt.Sprintf("HTF Moderate Alignment (M5=%s)", m5Trend)

	case model.Sell:
		// Hard conflict: M5 and H1 are both Bullish
		if m5Trend == HTFTrendBullish && h1Trend == HTFTrendBullish {
			return false, 0.20, "HTF Conflict (M5 & H1 Bullish Uptrend)"
		}
		// Strong alignment: M5 is Bearish
		if m5Trend == HTFTrendBearish {
			score = 0.85
			if h1Trend == HTFTrendBearish {
				score = 0.95
			}
			return true, score, fmt.Sprintf("HTF Confluence Aligned (M5=%s, H1=%s)", m5Trend, h1Trend)
		}
		return true, 0.60, fmt.Sprintf("HTF Moderate Alignment (M5=%s)", m5Trend)
	}

	return true, 0.50, "Neutral Signal"
}

// MultiTimeframeManager coordinates multi-timeframe aggregators for all active symbols.
type MultiTimeframeManager struct {
	mu        sync.RWMutex
	trackers  map[string]*SymbolTimeframes
	maxBars   int
}

// NewMultiTimeframeManager creates a manager for multi-timeframe data.
func NewMultiTimeframeManager(maxBars int) *MultiTimeframeManager {
	return &MultiTimeframeManager{
		trackers: make(map[string]*SymbolTimeframes),
		maxBars:  maxBars,
	}
}

// OnTick dispatches tick to symbol's timeframe tracker.
func (m *MultiTimeframeManager) OnTick(tick model.Tick) {
	symKey := strings.ToUpper(strings.TrimSpace(tick.Symbol))
	m.mu.Lock()
	tracker, exists := m.trackers[symKey]
	if !exists {
		tracker = NewSymbolTimeframes(symKey, m.maxBars)
		m.trackers[symKey] = tracker
	}
	m.mu.Unlock()

	tracker.OnTick(tick)
}

// CheckConfluence verifies if a signal is supported by higher timeframes.
func (m *MultiTimeframeManager) CheckConfluence(symbol string, sigType model.SignalType) (bool, float64, string) {
	symKey := strings.ToUpper(strings.TrimSpace(symbol))
	m.mu.RLock()
	tracker, exists := m.trackers[symKey]
	m.mu.RUnlock()

	if !exists || tracker == nil {
		return true, 0.55, "HTF Warm-Up Initializing"
	}

	return tracker.EvaluateConfluence(sigType)
}
