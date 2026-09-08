// Package main is the entry point for the scalping bot.
// It wires all components together using explicit dependency injection
// and orchestrates the multi-symbol tick-by-tick processing pipeline:
//
//	[Market Feed] -> [Tick RingBuffer (Per Symbol)] -> [Strategy Engine] -> [Signal Channel]
//	-> [AI Signal Filter] -> [Correlation Guard] -> [Risk Guardrail + News Blackout] -> [Dispatcher]
//	                                       |
//	                            [Position Tracker: Trailing SL, Break-Even, Partial TP]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pompbot/scalpbot/config"
	"github.com/pompbot/scalpbot/internal/ai"
	"github.com/pompbot/scalpbot/internal/broker"
	"github.com/pompbot/scalpbot/internal/broker/mt5"
	"github.com/pompbot/scalpbot/internal/executor"
	"github.com/pompbot/scalpbot/internal/indicator"
	"github.com/pompbot/scalpbot/internal/marketdata"
	"github.com/pompbot/scalpbot/internal/model"
	"github.com/pompbot/scalpbot/internal/news"
	"github.com/pompbot/scalpbot/internal/notifier"
	"github.com/pompbot/scalpbot/internal/portfolio"
	"github.com/pompbot/scalpbot/internal/risk"
	"github.com/pompbot/scalpbot/internal/storage"
	"github.com/pompbot/scalpbot/internal/strategy"
	"github.com/pompbot/scalpbot/internal/web"
)

type TradingMode string

const (
	ModeSantai   TradingMode = "SANTAI"   // 🌿 Conservative: Strict AI >= 65%, wide TP (30-50%), max 1-2 pos, max lot 0.05
	ModeBalanced TradingMode = "BALANCED" // ⚖️ Balanced: Moderate AI >= 55%, standard scalping, max 2-3 pos, max lot 0.10
	ModeAgresif  TradingMode = "AGRESIF"  // ⚡ Aggressive: Fast micro-scalp, AI >= 45%, tight TP (10-25%), max 3-4 pos, max lot 0.20
)

var (
	currentTradingMode atomic.Value // stores TradingMode
)

type PriceSnapshot struct {
	Timestamp time.Time
	Price     float64
}

// SymbolPipeline encapsulates the per-symbol analysis pipeline.
type SymbolPipeline struct {
	mu            sync.RWMutex
	Symbol        string
	Buffer        *marketdata.TickRingBuffer
	Strategy      *strategy.MomentumScalper
	RangeStrategy *strategy.RangeScalper
	Aggregator    *marketdata.OHLCVAggregator
	MTFAggregator *marketdata.MultiTimeframeAggregator
	M5FastEMA     *indicator.EMA
	M5SlowEMA     *indicator.EMA
	M5Trend       string // "BULLISH", "BEARISH", "NEUTRAL"
	M15FastEMA    *indicator.EMA
	M15SlowEMA    *indicator.EMA
	M15RSI        *indicator.RSI
	M15ATR        *indicator.ATR
	M15Trend      string // "BULLISH", "BEARISH", "NEUTRAL"
	H1FastEMA     *indicator.EMA
	H1SlowEMA     *indicator.EMA
	H1Trend       string // "BULLISH", "BEARISH", "NEUTRAL"
	AIFilter      *ai.SignalFilter
	SpreadFilter  *risk.SpreadAnomalyFilter
	LastSignal    string
	SignalStatus  string
	SignalReason  string
	SignalTime    string
	High24h       float64
	Low24h        float64
	OpenPrice     float64
	LastTickTime  time.Time
	Snapshots     []PriceSnapshot
	RecentCandles []web.CandleTelemetry
}

func (p *SymbolPipeline) SetSignalStatus(signal, status, reason, timeStr string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if signal != "" {
		p.LastSignal = signal
	}
	p.SignalStatus = status
	p.SignalReason = reason
	p.SignalTime = timeStr
}

func loadCandlesCache(symbol string) []web.CandleTelemetry {
	cleanSym := strings.ToUpper(strings.TrimRight(symbol, "cmCM."))
	cacheFile := filepath.Join("data", fmt.Sprintf("candles_%s.json", cleanSym))
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil
	}
	var candles []web.CandleTelemetry
	if err := json.Unmarshal(data, &candles); err != nil {
		return nil
	}
	return candles
}

func saveCandlesCache(symbol string, candles []web.CandleTelemetry) {
	if len(candles) == 0 {
		return
	}
	cleanSym := strings.ToUpper(strings.TrimRight(symbol, "cmCM."))
	cacheFile := filepath.Join("data", fmt.Sprintf("candles_%s.json", cleanSym))
	data, err := json.Marshal(candles)
	if err == nil {
		_ = os.WriteFile(cacheFile, data, 0644)
	}
}

func (p *SymbolPipeline) AddRecentCandle(c web.CandleTelemetry) {
	p.mu.Lock()
	p.RecentCandles = append(p.RecentCandles, c)
	if len(p.RecentCandles) > 100 {
		p.RecentCandles = p.RecentCandles[len(p.RecentCandles)-100:]
	}
	cachedCopy := make([]web.CandleTelemetry, len(p.RecentCandles))
	copy(cachedCopy, p.RecentCandles)
	p.mu.Unlock()

	saveCandlesCache(p.Symbol, cachedCopy)
}

func (p *SymbolPipeline) GetRecentCandles() []web.CandleTelemetry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	res := make([]web.CandleTelemetry, len(p.RecentCandles))
	copy(res, p.RecentCandles)
	return res
}

type CompletedTrade struct {
	Ticket    string
	Symbol    string
	Side      string
	Lots      float64
	Entry     float64
	Exit      float64
	NetPnL           float64
	Pips             float64
	Duration         time.Duration
	CloseTime        time.Time
	MaxFavorableUSD  float64
	MaxAdverseUSD    float64
	MaxFavorablePips float64
	MaxAdversePips   float64
}

var (
	persistenceStore *storage.Store
	tradeHistoryMu   sync.RWMutex
	completedTrades  []CompletedTrade
	recentEventsMu   sync.RWMutex
	recentEvents     []web.SignalEvent
	pipelineMu       sync.RWMutex
	pipelines        = make(map[string]*SymbolPipeline)
	mfeRecordsMu        sync.RWMutex
	mfeRecords          = make(map[string]struct{ mfeUSD, maeUSD, mfePips, maePips float64 })
	detectedAccountType atomic.Value
	accountCurrency     atomic.Value
)

func recordCompletedTrade(ct CompletedTrade, riskMgr *risk.Manager) {
	tradeHistoryMu.Lock()
	// Deduplication: prevent recording duplicate tickets or duplicate close events within 3s
	for _, existing := range completedTrades {
		if existing.Ticket == ct.Ticket {
			tradeHistoryMu.Unlock()
			return
		}
		if existing.Symbol == ct.Symbol && math.Abs(existing.CloseTime.Sub(ct.CloseTime).Seconds()) < 3.0 && math.Abs(existing.NetPnL-ct.NetPnL) < 0.10 {
			tradeHistoryMu.Unlock()
			return
		}
	}

	completedTrades = append(completedTrades, ct)
	if len(completedTrades) > 500 {
		completedTrades = completedTrades[len(completedTrades)-500:]
	}
	tradeHistoryMu.Unlock()

	if persistenceStore != nil {
		_ = persistenceStore.SaveTrade(storage.TradeRecord{
			Ticket:    ct.Ticket,
			Symbol:    ct.Symbol,
			Side:      ct.Side,
			Lots:      ct.Lots,
			Entry:     ct.Entry,
			Exit:      ct.Exit,
			NetPnL:    ct.NetPnL,
			Pips:      ct.Pips,
			Duration:  ct.Duration,
			CloseTime: ct.CloseTime,
			MFEUSD:    ct.MaxFavorableUSD,
			MAEUSD:    ct.MaxAdverseUSD,
			MFEPips:   ct.MaxFavorablePips,
			MAEPips:   ct.MaxAdversePips,
		})
		if riskMgr != nil {
			_ = persistenceStore.SaveDailyState(storage.DailyState{
				Date:                time.Now().Format("2006-01-02"),
				DailyPnL:            riskMgr.DailyPnL(),
				ProfitTargetReached: riskMgr.IsProfitTargetReached(),
				CircuitOpen:         riskMgr.IsCircuitOpen(),
			})
		}
	}
}

func computePerformanceTelemetry() *web.PerformanceTelemetry {
	tradeHistoryMu.RLock()
	defer tradeHistoryMu.RUnlock()

	total := len(completedTrades)
	if total == 0 {
		return &web.PerformanceTelemetry{
			TotalTrades:     0,
			WinRatePct:      0.0,
			ProfitFactor:    0.00,
			RealizedRRR:     0.00,
			SymbolBreakdown: make(map[string]web.SymbolPerformance),
		}
	}

	wins := 0
	losses := 0
	grossProfit := 0.0
	grossLoss := 0.0
	totalNet := 0.0
	symMap := make(map[string]web.SymbolPerformance)

	for _, t := range completedTrades {
		totalNet += t.NetPnL
		sp := symMap[t.Symbol]
		sp.Symbol = t.Symbol
		sp.Trades++
		sp.NetProfit += t.NetPnL

		if t.NetPnL >= 0 {
			wins++
			grossProfit += t.NetPnL
			sp.Wins++
		} else {
			losses++
			grossLoss += math.Abs(t.NetPnL)
			sp.Losses++
		}
		if sp.Trades > 0 {
			sp.WinRate = float64(sp.Wins) / float64(sp.Trades) * 100.0
		}
		symMap[t.Symbol] = sp
	}

	winRate := float64(wins) / float64(total) * 100.0
	pf := 2.0
	if grossLoss > 0 {
		pf = grossProfit / grossLoss
	} else if grossProfit > 0 {
		pf = 5.0
	}

	avgWin := 0.0
	if wins > 0 {
		avgWin = grossProfit / float64(wins)
	}
	avgLoss := 0.0
	if losses > 0 {
		avgLoss = grossLoss / float64(losses)
	}
	rrr := 2.0
	if avgLoss > 0 {
		rrr = avgWin / avgLoss
	}

	// Sort completedTrades chronologically ascending by CloseTime
	sort.Slice(completedTrades, func(i, j int) bool {
		return completedTrades[i].CloseTime.Before(completedTrades[j].CloseTime)
	})

	histList := make([]web.TradeRecordTelemetry, len(completedTrades))
	for i := range completedTrades {
		// Newest trade first
		idx := len(completedTrades) - 1 - i
		revT := completedTrades[idx]
		histList[i] = web.TradeRecordTelemetry{
			Ticket:    revT.Ticket,
			Symbol:    revT.Symbol,
			Side:      revT.Side,
			Lots:      revT.Lots,
			Entry:     revT.Entry,
			Exit:      revT.Exit,
			NetPnL:    revT.NetPnL,
			Pips:      revT.Pips,
			Duration:  revT.Duration.Round(time.Second).String(),
			CloseTime: revT.CloseTime.Format("15:04:05"),
			Date:      revT.CloseTime.Format("2006-01-02"),
			Timestamp: revT.CloseTime.Unix(),
			MFEUSD:    revT.MaxFavorableUSD,
			MAEUSD:    revT.MaxAdverseUSD,
			MFEPips:   revT.MaxFavorablePips,
			MAEPips:   revT.MaxAdversePips,
		}
	}

	// Guarantee histList is strictly sorted descending by Timestamp (newest first)
	sort.Slice(histList, func(i, j int) bool {
		return histList[i].Timestamp > histList[j].Timestamp
	})

	return &web.PerformanceTelemetry{
		TotalTrades:     total,
		WinningTrades:   wins,
		LosingTrades:    losses,
		WinRatePct:      winRate,
		ProfitFactor:    pf,
		TotalNetProfit:  totalNet,
		GrossProfit:     grossProfit,
		GrossLoss:       grossLoss,
		AverageWin:      avgWin,
		AverageLoss:     avgLoss,
		RealizedRRR:     rrr,
		SymbolBreakdown: symMap,
		TradeHistory:    histList,
	}
}

type MarketFocus string

const (
	FocusGoldOnly MarketFocus = "GOLD_ONLY"
)

var (
	currentMarketFocus atomic.Value // MarketFocus
)

func applyMarketFocus(focus MarketFocus) {
	currentMarketFocus.Store(focus)
	log.Printf("[market-focus] 🪙 Market focus changed to: %s", focus)
}

func applyTradingMode(mode TradingMode, riskMgr *risk.Manager) {
	currentTradingMode.Store(mode)
	log.Printf("[trading-mode] ══════════════════════════════════════════════════")
	log.Printf("[trading-mode] 🔄 TRADING MODE SWITCHED TO: %s", mode)

	pipelineMu.RLock()
	defer pipelineMu.RUnlock()

	for _, p := range pipelines {
		switch mode {
		case ModeSantai:
			p.AIFilter.UpdateConfig(ai.FilterConfig{
				EnableMLFilter:     true,
				MinConfidence:      0.65, // Conservative 65% composite confidence threshold
				RangeMinConfidence: 0.55,
				FilterRangingChop:  true,
				DualModeEnabled:    false,
			})
			log.Printf("[ai-filter] 🌿 SANTAI config applied for %s: MinConf=65%% (Composite), ChopGuard=ON", p.Symbol)
		case ModeBalanced:
			p.AIFilter.UpdateConfig(ai.FilterConfig{
				EnableMLFilter:     true,
				MinConfidence:      0.55, // Standard institutional 55% composite threshold
				RangeMinConfidence: 0.50,
				FilterRangingChop:  true,
				DualModeEnabled:    false,
			})
			log.Printf("[ai-filter] ⚖️ BALANCED config applied for %s: MinConf=55%% (Composite), ChopGuard=ON", p.Symbol)
		case ModeAgresif:
			p.AIFilter.UpdateConfig(ai.FilterConfig{
				EnableMLFilter:     true,
				MinConfidence:      0.45, // Responsive 45% composite threshold for early impulse capture
				RangeMinConfidence: 0.40,
				FilterRangingChop:  false,
				DualModeEnabled:    false,
			})
			log.Printf("[ai-filter] ⚡ AGRESIF config applied for %s: MinConf=45%% (Composite), ChopGuard=OFF", p.Symbol)
		}
	}
	log.Printf("[trading-mode] ══════════════════════════════════════════════════")
}

func recordSignalEvent(ev web.SignalEvent) {
	recentEventsMu.Lock()
	defer recentEventsMu.Unlock()
	recentEvents = append([]web.SignalEvent{ev}, recentEvents...)
	if len(recentEvents) > 15 {
		recentEvents = recentEvents[:15]
	}
}

func getRecentEvents() []web.SignalEvent {
	recentEventsMu.RLock()
	defer recentEventsMu.RUnlock()
	copied := make([]web.SignalEvent, len(recentEvents))
	copy(copied, recentEvents)
	return copied
}

func createSymbolPipeline(sym string, cfg *config.Config) *SymbolPipeline {
	cleanSym := normalizeSymbol(sym)
	p := &SymbolPipeline{
		Symbol: cleanSym,
		Buffer: marketdata.NewTickRingBuffer(cfg.MarketData.TickBufferSize),
		Strategy: strategy.NewMomentumScalper(fmt.Sprintf("momentum-%s", cleanSym), strategy.MomentumScalperConfig{
			FastEMAPeriod:       cfg.Strategy.FastEMAPeriod,
			SlowEMAPeriod:       cfg.Strategy.SlowEMAPeriod,
			RSIPeriod:           cfg.Strategy.RSIPeriod,
			ATRPeriod:           cfg.Strategy.ATRPeriod,
			RSIOverbought:       cfg.Strategy.RSIOverbought,
			RSIOversold:         cfg.Strategy.RSIOversold,
			ATRMinimum:          cfg.Strategy.ATRMinimum,
			MinVolRatio:         cfg.Strategy.MinVolRatio,
			EnableLimitPullback: cfg.Strategy.EnableLimitPullback,
			PullbackDiscountATR: cfg.Strategy.PullbackDiscountATR,
			Symbol:              cleanSym,
		}),
		RangeStrategy: strategy.NewRangeScalper(fmt.Sprintf("range-%s", cleanSym), strategy.RangeScalperConfig{
			BollingerPeriod: cfg.RangeStrategy.BollingerPeriod,
			BollingerStdDev: cfg.RangeStrategy.BollingerStdDev,
			RSIPeriod:       cfg.RangeStrategy.RSIPeriod,
			ATRPeriod:       cfg.RangeStrategy.ATRPeriod,
			RSIOverbought:   cfg.RangeStrategy.RSIOverbought,
			RSIOversold:     cfg.RangeStrategy.RSIOversold,
			MinTPPoints:     cfg.RangeStrategy.MinTPPoints,
			Symbol:          cleanSym,
		}),
		Aggregator:    marketdata.NewOHLCVAggregator(cfg.MarketData.CandlePeriod),
		MTFAggregator: marketdata.NewMultiTimeframeAggregator(),
		M5FastEMA:     indicator.NewEMA(20),
		M5SlowEMA:     indicator.NewEMA(50),
		M5Trend:       "NEUTRAL",
		M15FastEMA:    indicator.NewEMA(20),
		M15SlowEMA:    indicator.NewEMA(50),
		M15RSI:        indicator.NewRSI(14),
		M15ATR:        indicator.NewATR(14),
		M15Trend:      "NEUTRAL",
		H1FastEMA:     indicator.NewEMA(20),
		H1SlowEMA:     indicator.NewEMA(50),
		H1Trend:       "NEUTRAL",
		AIFilter: ai.NewSignalFilter(ai.FilterConfig{
			EnableMLFilter:     cfg.AI.EnableMLFilter,
			MinConfidence:      cfg.AI.MinConfidence,
			RangeMinConfidence: cfg.AI.RangeMinConfidence,
			FilterRangingChop:  cfg.AI.FilterRangingChop,
			DualModeEnabled:    cfg.AI.DualModeEnabled,
		}),
		SpreadFilter: risk.NewSpreadAnomalyFilter(1.6, 50),
		LastSignal:   "",
		SignalStatus: "IDLE",
		SignalReason: "Gaussian HMM Decision Layer Active",
		SignalTime:   time.Now().Format("15:04:05"),
		RecentCandles: make([]web.CandleTelemetry, 0, 100),
	}

	if cached := loadCandlesCache(sym); len(cached) > 0 {
		p.RecentCandles = cached
		log.Printf("[pipeline] 📂 Restored %d cached historical candles from disk for %s", len(cached), sym)
		for _, cd := range cached {
			p.Strategy.OnCandle(model.Candle{
				Symbol:      sym,
				Open:        cd.Open,
				High:        cd.High,
				Low:         cd.Low,
				Close:       cd.Close,
				Volume:      cd.Volume,
				TimestampNs: cd.Time * 1e9,
			})
		}
	}

	return p
}

func main() {
	// -------------------------------------------------------------------------
	// 1. Parse flags and load configuration
	// -------------------------------------------------------------------------
	configPath := flag.String("config", "config/default.yaml", "Path to YAML config file")
	brokerFlag := flag.String("broker", "", "Override broker type: 'mock' or 'mt5'")
	symbolsFlag := flag.String("symbols", "", "Comma-separated active symbols (e.g. 'XAUUSDc', 'XAUUSD', 'GOLDmicro')")
	portFlag := flag.Int("port", 0, "Override web dashboard port (e.g. 8080)")
	lotFlag := flag.Float64("lots", 0, "Override order lot size (e.g. 0.01, 0.05, 0.10)")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("[main] failed to load config: %v", err)
	}

	if *brokerFlag != "" {
		cfg.Broker.Type = *brokerFlag
	}
	if *portFlag != 0 {
		cfg.Web.Port = *portFlag
	}
	if *symbolsFlag != "" {
		parts := strings.Split(*symbolsFlag, ",")
		var clean []string
		for _, p := range parts {
			if tr := strings.TrimSpace(p); tr != "" {
				clean = append(clean, tr)
			}
		}
		if len(clean) > 0 {
			cfg.Bot.Symbols = clean
		}
	}

	if *lotFlag > 0 {
		cfg.Risk.MinLotSize = *lotFlag
		cfg.Risk.MaxLotSize = *lotFlag
		log.Printf("[main] 🎯 MANUAL LOT SIZE OVERRIDE: %.2f lots per trade", *lotFlag)
	}

	symbols := cfg.Bot.Symbols
	if len(symbols) == 0 {
		symbols = []string{cfg.Bot.Symbol}
	}
	log.Printf("[main] config loaded: broker=%s, active symbols=%v, candle_period=%s",
		cfg.Broker.Type, symbols, cfg.MarketData.CandlePeriod)

	// -------------------------------------------------------------------------
	// 2. Create root context with OS signal cancellation
	// -------------------------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[main] received signal: %v — initiating graceful shutdown", sig)
		cancel()
	}()

	// -------------------------------------------------------------------------
	// 3. Initialize Multi-Symbol Pipelines & Risk Core
	// -------------------------------------------------------------------------
	allowedSymbols := make(map[string]bool, len(symbols))

	for _, sym := range symbols {
		clean := normalizeSymbol(sym)
		allowedSymbols[clean] = true
		pipelines[clean] = createSymbolPipeline(clean, cfg)
	}
	log.Printf("[main] strict symbol whitelist active: %v", symbols)

	riskMgr := risk.NewManager(risk.ManagerConfig{
		InitialEquity:        cfg.Risk.InitialEquity,
		RiskPerTrade:         cfg.Risk.RiskPerTrade,
		MaxDailyDrawdown:     cfg.Risk.MaxDailyDrawdown,
		DailyProfitTargetPct: cfg.Risk.DailyProfitTargetPct,
		DailyProfitTargetUSD: cfg.Risk.DailyProfitTargetUSD,
		MaxSpreadPips:        cfg.Risk.MaxSpreadPips,
		MaxOpenPositions:     cfg.Risk.MaxOpenPositions,
		MinLotSize:           cfg.Risk.MinLotSize,
		MaxLotSize:           cfg.Risk.MaxLotSize,
	})

	firstSym := symbols[0]
	initAccType := model.DetectAccountType(firstSym, "", cfg.Risk.InitialEquity)
	detectedAccountType.Store(string(initAccType))
	accountCurrency.Store("USD")
	riskMgr.ApplyAccountProfile(initAccType)
	log.Printf("[account-detect] 🎯 Initial Account Environment: %s for symbol %s (SpreadLimit: %.1f pips)", initAccType, firstSym, riskMgr.MaxSpreadPips())

	// Initialize Storage Persistence (Trade History & Daily Risk State)
	persistenceStore = storage.NewStore("data")
	if savedTrades, err := persistenceStore.LoadTrades(); err == nil && len(savedTrades) > 0 {
		tradeHistoryMu.Lock()
		for _, st := range savedTrades {
			completedTrades = append(completedTrades, CompletedTrade{
				Ticket:           st.Ticket,
				Symbol:           st.Symbol,
				Side:             st.Side,
				Lots:             st.Lots,
				Entry:            st.Entry,
				Exit:             st.Exit,
				NetPnL:           st.NetPnL,
				Pips:             st.Pips,
				Duration:         st.Duration,
				CloseTime:        st.CloseTime,
				MaxFavorableUSD:  st.MFEUSD,
				MaxAdverseUSD:    st.MAEUSD,
				MaxFavorablePips: st.MFEPips,
				MaxAdversePips:   st.MAEPips,
			})
		}
		tradeHistoryMu.Unlock()
		log.Printf("[storage] 💾 Loaded %d historical completed trades from disk", len(savedTrades))
	}

	var savedLotFromState float64
	if ds, err := persistenceStore.LoadDailyState(); err == nil {
		if ds.ActiveLotSize >= 0.01 {
			savedLotFromState = ds.ActiveLotSize
		}
		if ds.Date == time.Now().Format("2006-01-02") {
			riskMgr.RestoreDailyState(ds.DailyPnL, ds.ProfitTargetReached, ds.CircuitOpen)
			log.Printf("[storage] 💾 Restored daily state for %s: DailyPnL=$%.2f, TargetReached=%v, CircuitOpen=%v",
				ds.Date, ds.DailyPnL, ds.ProfitTargetReached, ds.CircuitOpen)
		}
	}

	// Default trading mode: BALANCED
	currentTradingMode.Store(ModeBalanced)
	applyTradingMode(ModeBalanced, riskMgr)

	// Tick velocity filter (liquidity & anti-surge gate)
	var velocityFilter *risk.TickVelocityFilter
	if cfg.Risk.MinTickVelocityTPS > 0 || cfg.Risk.MaxTickVelocityTPS > 0 {
		velocityFilter = risk.NewTickVelocityFilter(5*time.Second, cfg.Risk.MinTickVelocityTPS, cfg.Risk.MaxTickVelocityTPS)
		riskMgr.AddFilter(velocityFilter)
		log.Printf("[main] tick velocity filter enabled (min=%.1f, max=%.1f TPS)",
			cfg.Risk.MinTickVelocityTPS, cfg.Risk.MaxTickVelocityTPS)
	}

	// Session filter (configurable)
	if cfg.Session.Enabled {
		sessions := buildSessionWindows(cfg.Session.Sessions)
		if len(sessions) > 0 {
			sessionFilter := risk.NewSessionFilter(sessions)
			riskMgr.AddFilter(sessionFilter)
			log.Printf("[main] trading session filter enabled for: %v", cfg.Session.Sessions)
		}
	}

	// Live Economic Calendar & News Blackout Filter
	calendarClient := news.NewCalendarClient(cfg.News.FeedURL)
	if cfg.News.EnableAutoSync && cfg.News.FeedURL != "" {
		calendarClient.StartAutoSync(ctx, cfg.News.SyncInterval)
		log.Printf("[main] live news calendar auto-sync enabled (interval=%s)", cfg.News.SyncInterval)
	}
	newsBlackoutFilter := news.NewDynamicNewsBlackoutFilter(news.BlackoutConfig{
		Enabled:        true,
		BlackoutBefore: cfg.News.BlackoutBefore,
		BlackoutAfter:  cfg.News.BlackoutAfter,
	}, calendarClient)
	riskMgr.AddFilter(newsBlackoutFilter)
	log.Printf("[main] news blackout filter enabled (before=%v, after=%v)",
		cfg.News.BlackoutBefore, cfg.News.BlackoutAfter)

	for _, p := range pipelines {
		if p.SpreadFilter != nil {
			riskMgr.AddFilter(p.SpreadFilter)
		}
	}
	log.Printf("[main] dynamic spread anomaly filter registered in risk core")

	// Cross-symbol portfolio correlation guard
	correlationGuard := portfolio.NewCorrelationGuard(cfg.Portfolio.MaxCorrelatedPositions)
	volatilitySizer := portfolio.NewVolatilityParitySizer(portfolio.SizerConfig{
		RiskPerTrade: cfg.Risk.RiskPerTrade,
		MinLotSize:   cfg.Risk.MinLotSize,
		MaxLotSize:   cfg.Risk.MaxLotSize,
		LotStep:      0.01,
		SLATRMult:    cfg.Position.TrailingATRMult,
	})
	log.Printf("[main] portfolio correlation guard (max_corr=%d) & volatility sizer initialized",
		cfg.Portfolio.MaxCorrelatedPositions)

	// Notifier
	alerter := buildNotifier(cfg.Notifier)
	log.Printf("[main] notifier: %s", alerter.Name())
	_ = alerter.Send(ctx, notifier.Alert{
		Level:     notifier.AlertInfo,
		Title:     "Bot Started (Armed & Ready)",
		Message:   fmt.Sprintf("ScalpBot started with symbols: %v | Broker: %s | AI Gate: >=%.0f%%", symbols, cfg.Broker.Type, cfg.AI.MinConfidence*100),
		Timestamp: time.Now(),
	})

	// -------------------------------------------------------------------------
	// 4. Initialize MT5 Bridge or Mock Broker
	// -------------------------------------------------------------------------
	var activeBroker broker.Broker
	var mt5Adapter *mt5.Adapter

	if strings.ToLower(cfg.Broker.Type) == "mt5" {
		log.Printf("[main] initializing MT5 Broker Adapter (cmd=%s, stream=%s)",
			cfg.Broker.MT5CommandAddr, cfg.Broker.MT5StreamAddr)
		mt5Adapter = mt5.NewAdapter(mt5.AdapterConfig{
			CommandAddr: cfg.Broker.MT5CommandAddr,
			StreamAddr:  cfg.Broker.MT5StreamAddr,
			Timeout:     2 * time.Second,
			MagicNumber: cfg.Broker.MagicNumber,
			Slippage:    cfg.Broker.SlippagePoints,
		})
		activeBroker = mt5Adapter

		if err := mt5Adapter.Connect(ctx); err != nil {
			log.Fatalf("[main] failed to start MT5 Bridge Server: %v", err)
		}
		log.Printf("[main] MT5 Bridge Server ready — listening for MetaTrader 5 connection")
	} else {
		log.Printf("[main] using Mock Broker (latency=%v, slippage=%.3f%%, reject=%.1f%%)",
			cfg.Broker.FillLatency, cfg.Broker.SlippagePct*100, cfg.Broker.RejectRate*100)
		activeBroker = broker.NewMockBroker(broker.MockConfig{
			FillLatency: cfg.Broker.FillLatency,
			SlippagePct: cfg.Broker.SlippagePct,
			RejectRate:  cfg.Broker.RejectRate,
		})
	}

	// -------------------------------------------------------------------------
	// 5. Initialize Execution & Management Components
	// -------------------------------------------------------------------------
	telemetry := executor.NewTelemetryTracker()

	// Position tracker
	trailingMode := executor.TrailingFixed
	if cfg.Position.TrailingMode == "atr" {
		trailingMode = executor.TrailingATR
	}
	tracker := executor.NewPositionTracker(executor.TrackerConfig{
		TrailingMode:      trailingMode,
		TrailingStopPips:  cfg.Position.TrailingStopPips,
		TrailingATRMult:   cfg.Position.TrailingATRMult,
		BreakEvenPips:     cfg.Position.BreakEvenPips,
		BreakEvenBuffPips: cfg.Position.BreakEvenBuffPips,
		EnableTrailing:     cfg.Position.EnableTrailing,
		EnableBreakEven:    cfg.Position.EnableBreakEven,
		EnableProfitLocker: cfg.Position.EnableProfitLocker,
		Stage1ATRMult:      cfg.Position.Stage1ATRMult,
		Stage2ATRMult:      cfg.Position.Stage2ATRMult,
		Stage3ATRMult:      cfg.Position.Stage3ATRMult,
		EnablePartialTP:    cfg.Position.EnablePartialTP,
		PartialTPRatio:    cfg.Position.PartialTPRatio,
		TP1Pips:           cfg.Position.TP1Pips,
		TP2Pips:           cfg.Position.TP2Pips,
		TimeStopDuration:  cfg.Position.TimeStopDuration,
		AutoRemoveOnClose: mt5Adapter == nil, // In MT5 mode, let MT5 broker manage position lifecycle
	})

	// Interactive 2-way Telegram Bot (Mobile Control & Alerts)
	var tgBot *notifier.TelegramInteractiveBot

	type pendingFillInfo struct{ sl, tp float64 }
	var pendingFillMu sync.Mutex
	pendingFills := make(map[string]pendingFillInfo)

	// Order dispatcher
	dispatcher := executor.NewDispatcher(cfg.Executor.QueueSize, activeBroker, cfg.Executor.Workers)
	dispatcher.SetCallbacks(
		func(pos model.Position, orderReq model.OrderRequest) {
			riskMgr.RecordFillForSymbol(pos.Symbol)
			telemetry.RecordFill(model.OrderRequest{Symbol: pos.Symbol, Side: pos.Side, Lots: pos.Lots, Price: pos.EntryPrice}, pos, 0)
			// ponytail: In MT5 mode, do NOT add to tracker here. The reconcile
			// loop adds positions keyed by Position ID (correct). Adding here
			// with Deal ID (resp.Ticket) creates a phantom duplicate that gets
			// SL-killed instantly by spread cost. Non-MT5 mode still adds here.
			if mt5Adapter == nil {
				tracker.Add(pos, orderReq.StopLoss, orderReq.TakeProfit)
			} else {
				// Store SL/TP so reconcile loop can apply them when it discovers the position
				pendingFillMu.Lock()
				pendingFills[model.NormalizeSymbol(pos.Symbol)] = pendingFillInfo{sl: orderReq.StopLoss, tp: orderReq.TakeProfit}
				pendingFillMu.Unlock()
			}
			log.Printf("[main] position opened: %s %s %.2f lots @ %.5f (tracked=%d)",
				pos.Side, pos.Symbol, pos.Lots, pos.EntryPrice, tracker.Count())

			_ = alerter.Send(ctx, notifier.Alert{
				Level:     notifier.AlertInfo,
				Title:     "Position Opened",
				Message:   pos.Side.String() + " " + pos.Symbol,
				Timestamp: time.Now(),
				Metadata:  map[string]string{"lots": fmt.Sprintf("%.2f", pos.Lots)},
			})

			if tgBot != nil {
				tgBot.SendTradeOpen(ctx, pos.OrderID, pos.Symbol, pos.Side.String(), pos.Lots, pos.EntryPrice, orderReq.StopLoss, orderReq.TakeProfit, "M15_H1_ALIGNED", 0.70, orderReq.SignalID)
			}
		},
		func(order model.OrderRequest, err error) {
			telemetry.RecordRejection()
			log.Printf("[main] order rejected: %s %s — %v",
				order.Side, order.Symbol, err)
		},
	)

	// Live account state synced from MT5
	var aiFilterEnabled atomic.Bool
	aiFilterEnabled.Store(cfg.AI.EnableMLFilter)

	var emergencyHalted atomic.Bool
	emergencyHalted.Store(false)

	var liveBalance atomic.Uint64
	var liveEquity atomic.Uint64
	var liveFreeMargin atomic.Uint64
	var totalTicksProcessed atomic.Int64

	setLiveFloat := func(a *atomic.Uint64, v float64) {
		a.Store(math.Float64bits(v))
	}
	getLiveFloat := func(a *atomic.Uint64, defaultVal float64) float64 {
		bits := a.Load()
		if bits == 0 {
			return defaultVal
		}
		return math.Float64frombits(bits)
	}

	setLiveFloat(&liveBalance, cfg.Risk.InitialEquity)
	setLiveFloat(&liveEquity, cfg.Risk.InitialEquity)
	setLiveFloat(&liveFreeMargin, cfg.Risk.InitialEquity)

	var activeUserLot atomic.Uint64
	initialLot := 0.01
	if savedLotFromState >= 0.01 {
		initialLot = savedLotFromState
	} else if cfg.Risk.MinLotSize > 0 {
		initialLot = cfg.Risk.MinLotSize
	}
	if *lotFlag > 0 {
		initialLot = *lotFlag
	}
	setLiveFloat(&activeUserLot, initialLot)
	riskMgr.SetFixedLotSize(initialLot)

	currentMarketFocus.Store(FocusGoldOnly)
	mtfMgr := marketdata.NewMultiTimeframeManager(100)

	// Initialize Interactive 2-way Telegram Bot
	if cfg.Notifier.EnableInteractiveBot && cfg.Notifier.TelegramToken != "" && cfg.Notifier.TelegramChat != "" {
		log.Printf("[telegram-bot] 🤖 Initializing interactive 2-way Telegram Bot for chat %s", cfg.Notifier.TelegramChat)
		tgBot = notifier.NewTelegramInteractiveBot(cfg.Notifier.TelegramToken, cfg.Notifier.TelegramChat, notifier.BotCallbacks{
			GetStatus: func() notifier.BotStatusSummary {
				var symNames []string
				pipelineMu.RLock()
				for s := range pipelines {
					symNames = append(symNames, s)
				}
				regimeStr := "NEUTRAL"
				confVal := 0.0
				goldPrice := 0.0
				goldSpread := 0.0
				for _, p := range pipelines {
					if p.AIFilter != nil {
						regimeStr = p.AIFilter.LastRegime().String()
						confVal = p.AIFilter.LastConfidence()
					}
					if lastTick, hasTick := p.Buffer.Last(); hasTick {
						goldPrice = lastTick.Bid
						pipMult := model.PipMultiplier(lastTick.Symbol)
						sp := lastTick.SpreadPips(pipMult)
						if strings.Contains(strings.ToUpper(lastTick.Symbol), "XAU") {
							goldSpread = sp / 10.0
						} else {
							goldSpread = sp
						}
					}
				}
				pipelineMu.RUnlock()

				curBal := getLiveFloat(&liveBalance, cfg.Risk.InitialEquity)
				curEq := getLiveFloat(&liveEquity, curBal)
				modeStr := "BALANCED"
				if m := currentTradingMode.Load(); m != nil {
					modeStr = string(m.(TradingMode))
				}
				focusStr := "GOLD_ONLY"
				if f := currentMarketFocus.Load(); f != nil {
					focusStr = string(f.(MarketFocus))
				}

				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				memMB := float64(m.Alloc) / (1024.0 * 1024.0)

				return notifier.BotStatusSummary{
					Balance:             curBal,
					Equity:              curEq,
					DailyPnL:            riskMgr.DailyPnL(),
					DailyDrawdownPct:    math.Abs(riskMgr.DailyPnL() / curEq * 100),
					FloatingPnL:         tracker.TotalPnL(),
					OpenPositions:       tracker.Count(),
					TradingMode:         modeStr,
					MarketFocus:         focusStr,
					AIRegime:            regimeStr,
					AIConfidence:        confVal,
					ProfitTargetReached: riskMgr.IsProfitTargetReached(),
					ProfitTargetAmount:  riskMgr.ProfitTargetAmount(),
					ActiveSymbols:       symNames,
					IsPaused:            emergencyHalted.Load(),
					TotalTicksProcessed: totalTicksProcessed.Load(),
					MemoryAllocMB:       memMB,
					GoldPrice:           goldPrice,
					GoldSpread:          goldSpread,
				}
			},
			SetTradingMode: func(mode string) error {
				m := TradingMode(strings.ToUpper(strings.TrimSpace(mode)))
				if m != ModeSantai && m != ModeBalanced && m != ModeAgresif {
					return fmt.Errorf("invalid mode: %s", mode)
				}
				applyTradingMode(m, riskMgr)
				return nil
			},
			SetMarketFocus: func(focus string) error {
				applyMarketFocus(FocusGoldOnly)
				return nil
			},
			CloseAllTrades: func() (int, error) {
				count := 0
				for _, pos := range tracker.ActivePositions() {
					_ = activeBroker.Close(ctx, pos.OrderID)
					count++
				}
				return count, nil
			},
			SetLotSize: func(lots float64) error {
				if lots < 0.01 || lots > 10.0 {
					return fmt.Errorf("lot out of range: %.2f (must be 0.01 - 10.0)", lots)
				}
				setLiveFloat(&activeUserLot, lots)
				riskMgr.SetFixedLotSize(lots)
				if persistenceStore != nil && riskMgr != nil {
					_ = persistenceStore.SaveDailyState(storage.DailyState{
						Date:                time.Now().Format("2006-01-02"),
						DailyPnL:            riskMgr.DailyPnL(),
						ProfitTargetReached: riskMgr.IsProfitTargetReached(),
						CircuitOpen:         riskMgr.IsCircuitOpen(),
						ActiveLotSize:       lots,
						LastUpdated:         time.Now(),
					})
				}
				log.Printf("[telegram-control] 🎯 USER LOT UPDATED TO: %.2f lots", lots)
				return nil
			},
			TogglePause: func(pause bool) bool {
				emergencyHalted.Store(pause)
				log.Printf("[telegram-bot] 🛑 Pause toggled via Telegram: %v", pause)
				return emergencyHalted.Load()
			},
			GetTodayReport: func() string {
				tradeHistoryMu.RLock()
				todayStr := time.Now().Format("2006-01-02")
				var todayList []CompletedTrade
				for _, t := range completedTrades {
					if t.CloseTime.Format("2006-01-02") == todayStr {
						todayList = append(todayList, t)
					}
				}
				tradeHistoryMu.RUnlock()

				totalToday := len(todayList)
				if totalToday == 0 {
					return "📈 <b>LAPORAN TRADING HARI INI</b>\n━━━━━━━━━━━━━━━━━━━━\n<i>Belum ada transaksi yang ditutup hari ini (Sesi Bersih).</i>"
				}

				wins := 0
				losses := 0
				totalPnL := 0.0
				totalPips := 0.0
				for _, t := range todayList {
					totalPnL += t.NetPnL
					totalPips += t.Pips
					if t.NetPnL >= 0 {
						wins++
					} else {
						losses++
					}
				}
				wr := (float64(wins) / float64(totalToday)) * 100.0
				sign := "+"
				if totalPnL < 0 {
					sign = ""
				}

				report := fmt.Sprintf("📈 <b>LAPORAN TRADING HARI INI (%s)</b>\n"+
					"━━━━━━━━━━━━━━━━━━━━\n"+
					"• <b>Total Trades:</b> <code>%d trades</code>\n"+
					"• <b>Win Rate:</b> <b>%.1f%%</b> (%dW / %dL)\n"+
					"• <b>Net P&L:</b> <b>%s$%.2f</b> (%s%.1f pips)\n"+
					"━━━━━━━━━━━━━━━━━━━━\n"+
					"<b>Transaksi Terbaru:</b>\n",
					todayStr, totalToday, wr, wins, losses, sign, totalPnL, sign, totalPips)

				startIdx := 0
				if len(todayList) > 5 {
					startIdx = len(todayList) - 5
				}
				for i := len(todayList) - 1; i >= startIdx; i-- {
					t := todayList[i]
					tSign := "+"
					if t.NetPnL < 0 {
						tSign = ""
					}
					tEmoji := "✅"
					if t.NetPnL < 0 {
						tEmoji = "❌"
					}
					report += fmt.Sprintf("%s #%s %s %.2f @ %.2f ➔ %s$%.2f (%s WIB)\n",
						tEmoji, t.Ticket, t.Side, t.Lots, t.Entry, tSign, t.NetPnL, t.CloseTime.Add(7*time.Hour).Format("15:04"))
				}
				return report
			},
			GetPriceQuote: func() string {
				pipelineMu.RLock()
				var p *SymbolPipeline
				for _, pl := range pipelines {
					p = pl
					break
				}
				pipelineMu.RUnlock()
				if p == nil {
					return "🪙 <i>Data harga belum tersedia.</i>"
				}

				lastTick, hasTick := p.Buffer.Last()
				if !hasTick {
					return "🪙 <i>Menghubungkan ke stream MT5...</i>"
				}

				p.mu.RLock()
				high := p.High24h
				low := p.Low24h
				regime := p.SignalReason
				p.mu.RUnlock()

				pipMult := model.PipMultiplier(lastTick.Symbol)
				spread := lastTick.SpreadPips(pipMult)
				spreadDisplay := spread
				if strings.Contains(strings.ToUpper(lastTick.Symbol), "XAU") {
					spreadDisplay = spread / 10.0
				}

				return fmt.Sprintf("🪙 <b>QUOTE HARGA LIVE (XAUUSD)</b>\n"+
					"━━━━━━━━━━━━━━━━━━━━\n"+
					"• <b>Bid (Sell):</b> <code>$%.2f</code>\n"+
					"• <b>Ask (Buy):</b> <code>$%.2f</code>\n"+
					"• <b>Spread:</b> <code>%.1f pips</code> (Limit: 35.0p)\n"+
					"• <b>24H Range:</b> <code>$%.2f ➔ $%.2f</code>\n"+
					"• <b>Regime Status:</b> <code>%s</code>\n"+
					"• <b>Stream:</b> <code>TCP 5556 LIVE (%s WIB)</code>",
					lastTick.Bid, lastTick.Ask,
					spreadDisplay,
					low, high,
					html.EscapeString(regime),
					time.Now().UTC().Add(7*time.Hour).Format("15:04:05"))
			},
		})
		tgBot.Start(ctx)
	}

	if mt5Adapter != nil {
		go func() {
			syncMT5 := func() {
				// Periodic sync of real balance/equity from MT5 account
				acc, accErr := mt5Adapter.GetAccountState(ctx)
				if accErr == nil && acc != nil && acc.Balance > 0 {
					setLiveFloat(&liveBalance, acc.Balance)
					setLiveFloat(&liveEquity, acc.Equity)
					setLiveFloat(&liveFreeMargin, acc.FreeMargin)
					riskMgr.UpdateEquity(acc.Equity)

					detectedType := model.DetectAccountType(firstSym, acc.Currency, acc.Balance)
					prevType := detectedAccountType.Load()
					if prevType == nil || prevType.(string) != string(detectedType) {
						detectedAccountType.Store(string(detectedType))
						accountCurrency.Store(acc.Currency)
						riskMgr.ApplyAccountProfile(detectedType)
						log.Printf("[account-detect] 🎯 AUTO-DETECTED ACCOUNT: %s (Currency: %s, Symbol: %s, Balance: $%.2f) — Adaptive Parameters Applied! (SpreadLimit: %.1f pips)",
							detectedType, acc.Currency, firstSym, acc.Balance, riskMgr.MaxSpreadPips())
					}
				}

				// Full bi-directional position reconciliation with MT5 using ListOpenPositionsWithDetails
				posDetails, posErr := mt5Adapter.ListOpenPositionsWithDetails(ctx)
				if posErr != nil {
					if !strings.Contains(posErr.Error(), "waiting for MT5 EA connection") {
						log.Printf("[reconcile] ERROR syncing positions with MT5: %v", posErr)
					}
				} else {
					// Synchronize risk manager open position count and per-symbol positions with live MT5 positions
					liveSymbols := make([]string, len(posDetails))
					for i, dto := range posDetails {
						liveSymbols[i] = dto.Symbol
					}
					riskMgr.SyncPositions(liveSymbols)

					mt5LiveIDs := make(map[string]bool, len(posDetails))
					for _, dto := range posDetails {
						mt5LiveIDs[dto.Ticket] = true
					}

					// 1. Process positions closed in MT5 (e.g. TP/SL hit on MT5 server)
					for _, trackedID := range tracker.ActiveOrderIDs() {
						if !mt5LiveIDs[trackedID] {
							if pos, found := tracker.Get(trackedID); found {
								tracker.Remove(trackedID)
								log.Printf("[reconcile] position %s closed in MT5 (final PnL=$%.2f)", trackedID, pos.CurrentPnL)

								pipVal := model.PipValue(pos.Symbol, pos.Lots)
								pips := 0.0
								exitPrice := pos.EntryPrice
								if pipVal > 0 {
									pips = pos.CurrentPnL / (pos.Lots * (pipVal / pos.Lots))
									pipMult := model.PipMultiplier(pos.Symbol)
									if pos.Side == model.SideBuy {
										exitPrice = pos.EntryPrice + (pips / pipMult)
									} else {
										exitPrice = pos.EntryPrice - (pips / pipMult)
									}
								}

								tradeDuration := pos.OpenDuration()
								recordCompletedTrade(CompletedTrade{
									Ticket:    trackedID,
									Symbol:    pos.Symbol,
									Side:      pos.Side.String(),
									Lots:      pos.Lots,
									Entry:     pos.EntryPrice,
									Exit:      exitPrice,
									NetPnL:    pos.CurrentPnL,
									Pips:      pips,
									Duration:  tradeDuration,
									CloseTime: time.Now(),
								}, riskMgr)

								if tgBot != nil {
									tgBot.SendTradeClose(ctx, trackedID, pos.Symbol, pos.Side.String(), pos.Lots, pos.EntryPrice, exitPrice, pos.CurrentPnL, pips, tradeDuration)
									if riskMgr.IsProfitTargetReached() {
										tgBot.SendProfitTargetAlert(ctx, riskMgr.DailyPnL(), riskMgr.ProfitTargetAmount(), riskMgr.Equity())
									}
								}
							} else {
								tracker.Remove(trackedID)
							}
						}
					}

					// 2. Sync SL/TP/P&L from MT5 positions
					for _, dto := range posDetails {
						if tracker.Has(dto.Ticket) {
							tracker.UpdateSLTP(dto.Ticket, dto.StopLoss, dto.TakeProfit)
							tracker.UpdatePnL(dto.Ticket, dto.Profit)
						} else if existingPos, ok := tracker.FindBySymbol(dto.Symbol); ok {
							// Deal ID vs Position ID aliasing: update tracked ticket without creating duplicate position
							tracker.ReplaceTicket(existingPos.OrderID, dto.Ticket)
							tracker.UpdateSLTP(dto.Ticket, dto.StopLoss, dto.TakeProfit)
							tracker.UpdatePnL(dto.Ticket, dto.Profit)
						} else {
							// 3. Add new positions from MT5 not yet tracked
							// Use pending fill SL/TP if available (from our own order, more precise)
							sl, tp := dto.StopLoss, dto.TakeProfit
							normSym := model.NormalizeSymbol(dto.Symbol)
							pendingFillMu.Lock()
							if fi, ok := pendingFills[normSym]; ok {
								sl, tp = fi.sl, fi.tp
								delete(pendingFills, normSym)
							}
							pendingFillMu.Unlock()

							side := model.SideBuy
							if strings.ToUpper(dto.Side) == "SELL" {
								side = model.SideSell
							}
							pos := model.Position{
								OrderID:    dto.Ticket,
								Symbol:     dto.Symbol,
								Side:       side,
								Lots:       dto.Lots,
								EntryPrice: dto.OpenPrice,
								CurrentPnL: dto.Profit,
								OpenTimeNs: dto.OpenTimeNs,
							}
							tracker.Add(pos, sl, tp)
							log.Printf("[reconcile] synced active MT5 position: %s %s %.2f lots @ %.5f (SL=%.5f TP=%.5f)",
								pos.OrderID, pos.Symbol, pos.Lots, pos.EntryPrice, sl, tp)
						}
					}

					// 2.1 Stagnant Trade Killer (Closes trades open > 20m hovering near BEP on small accounts)
					for _, pos := range tracker.ActivePositions() {
						prof := model.DetectAssetClass(pos.Symbol)
						openDuration := pos.OpenDuration()
						stagnantLimit := time.Duration(prof.StagnantMinutes) * time.Minute
						if stagnantLimit <= 0 {
							stagnantLimit = 20 * time.Minute
						}

						if openDuration >= stagnantLimit {
							if pos.CurrentPnL >= -1.50 && pos.CurrentPnL <= 2.50 {
								log.Printf("[stagnant-killer] ⏱️ STAGNANT TRADE AUTO-CLOSED on %s %s (open %v, pnl=$%.2f) to release margin",
									pos.Side, pos.Symbol, openDuration.Round(time.Second), pos.CurrentPnL)
								_ = activeBroker.Close(ctx, pos.OrderID)
							}
						}
					}

					// 2.2 Sync official closed trade history directly from MT5 terminal database
					deals, historyErr := mt5Adapter.FetchHistoryDeals(ctx, 30.0)
					if historyErr == nil && len(deals) > 0 {
						tradeHistoryMu.Lock()
						// Merge deals instead of wiping existing loaded trade history
						existingTickets := make(map[string]int, len(completedTrades))
						for idx, ct := range completedTrades {
							existingTickets[ct.Ticket] = idx
						}

						for _, d := range deals {
							pipMult := model.PipMultiplier(d.Symbol)
							pips := 0.0
							if pipMult > 0 {
								if d.Side == "BUY" {
									pips = (d.ExitPrice - d.EntryPrice) * pipMult
								} else {
									pips = (d.EntryPrice - d.ExitPrice) * pipMult
								}
							}
							closeTime := time.Unix(d.CloseTime, 0)
							oneWeekAgo := time.Now().Add(-7 * 24 * time.Hour)
							if closeTime.Before(oneWeekAgo) {
								continue
							}

							mfeRecordsMu.RLock()
							mfeRec, hasMFE := mfeRecords[d.Ticket]
							mfeRecordsMu.RUnlock()

							ct := CompletedTrade{
								Ticket:    d.Ticket,
								Symbol:    d.Symbol,
								Side:      d.Side,
								Lots:      d.Lots,
								Entry:     d.EntryPrice,
								Exit:      d.ExitPrice,
								NetPnL:    d.NetPnL,
								Pips:      pips,
								Duration:  time.Duration(d.DurationSec) * time.Second,
								CloseTime: closeTime,
							}
							if hasMFE {
								ct.MaxFavorableUSD = mfeRec.mfeUSD
								ct.MaxAdverseUSD = mfeRec.maeUSD
								ct.MaxFavorablePips = mfeRec.mfePips
								ct.MaxAdversePips = mfeRec.maePips
							}

							if existingIdx, found := existingTickets[d.Ticket]; found {
								completedTrades[existingIdx] = ct
							} else {
								completedTrades = append(completedTrades, ct)
							}
						}

						// Guarantee completedTrades is sorted chronologically ascending
						sort.Slice(completedTrades, func(i, j int) bool {
							return completedTrades[i].CloseTime.Before(completedTrades[j].CloseTime)
						})
						// Rebuild existingTickets map
						for idx, tr := range completedTrades {
							existingTickets[tr.Ticket] = idx
						}

						tradeHistoryMu.Unlock()
					}
				}
			}

			// Immediate initial sync
			syncMT5()

			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					syncMT5()
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	dispatcher.Start(ctx)
	log.Println("[main] multi-symbol scalping pipeline started")

	if cfg.Web.Enabled {
		webAddr := fmt.Sprintf("%s:%d", cfg.Web.Host, cfg.Web.Port)
		webServer := web.NewServer(webAddr, web.ServerCallbacks{
			OnKillSwitch: func() error {
				emergencyHalted.Store(true)
				log.Println("[web-control] 🚨 EMERGENCY KILL-SWITCH TRIGGERED FROM DASHBOARD!")
				for _, pos := range tracker.ActivePositions() {
					_ = activeBroker.Close(ctx, pos.OrderID)
				}
				cancel()
				return nil
			},
			OnCloseAll: func() error {
				log.Println("[web-control] 🛑 CLOSE ALL POSITIONS TRIGGERED FROM DASHBOARD!")
				for _, pos := range tracker.ActivePositions() {
					_ = activeBroker.Close(ctx, pos.OrderID)
				}
				return nil
			},
			OnClosePosition: func(orderID string) error {
				log.Printf("[web-control] CLOSE POSITION TRIGGERED FOR %s", orderID)
				return activeBroker.Close(ctx, orderID)
			},
			OnToggleAI: func(enable bool) bool {
				aiFilterEnabled.Store(enable)
				log.Printf("[web-control] AI FILTER ATOMICALLY TOGGLED: %v", enable)
				return enable
			},
			OnSetFocus: func(focus string) error {
				applyMarketFocus(FocusGoldOnly)
				return nil
			},
			OnSetMode: func(mode string) error {
				m := TradingMode(strings.ToUpper(strings.TrimSpace(mode)))
				if m != ModeSantai && m != ModeBalanced && m != ModeAgresif {
					return fmt.Errorf("invalid mode: %s (must be SANTAI, BALANCED, or AGRESIF)", mode)
				}
				applyTradingMode(m, riskMgr)
				_ = alerter.Send(ctx, notifier.Alert{
					Level:     notifier.AlertInfo,
					Title:     "Trading Mode Changed",
					Message:   fmt.Sprintf("ScalpBot switched to %s mode", m),
					Timestamp: time.Now(),
				})
				return nil
			},
			OnSetLot: func(lot float64) error {
				if lot < 0.01 || lot > 10.0 {
					return fmt.Errorf("invalid lot: %.2f (must be 0.01 - 10.0)", lot)
				}
				setLiveFloat(&activeUserLot, lot)
				riskMgr.SetFixedLotSize(lot)
				if persistenceStore != nil && riskMgr != nil {
					_ = persistenceStore.SaveDailyState(storage.DailyState{
						Date:                time.Now().Format("2006-01-02"),
						DailyPnL:            riskMgr.DailyPnL(),
						ProfitTargetReached: riskMgr.IsProfitTargetReached(),
						CircuitOpen:         riskMgr.IsCircuitOpen(),
						ActiveLotSize:       lot,
						LastUpdated:         time.Now(),
					})
				}
				log.Printf("[web-control] 🎯 USER LOT ATOMICALLY UPDATED TO: %.2f lots", lot)
				return nil
			},
			GetTelemetry: func() web.TelemetryPayload {
				tracked := tracker.ActivePositions()
				posList := make([]web.PositionTelemetry, len(tracked))
				totalFloat := 0.0

				// Pre-fetch latest tick prices for each symbol
				pipelineMu.RLock()
				latestPrices := make(map[string]float64)
				for sym, p := range pipelines {
					if t, ok := p.Buffer.Last(); ok && t.Bid > 0 {
						latestPrices[sym] = t.Bid
					}
				}
				pipelineMu.RUnlock()

				nowUnix := time.Now().Unix()
				for i, p := range tracked {
					totalFloat += p.CurrentPnL

					// Get current price from live tick feed
					symKey := normalizeSymbol(p.Symbol)
					curPrice := p.EntryPrice
					if lp, ok := latestPrices[symKey]; ok {
						curPrice = lp
					}

					// Holding time: handle MT5 server time vs local time
					holdSec := nowUnix - (p.OpenTimeNs / 1e9)
					if holdSec < 0 {
						holdSec = -holdSec // MT5 server time ahead of local clock
					}

					// Calculate floating pips
					pipMult := model.PipMultiplier(p.Symbol)
					var floatPips float64
					if p.Side == model.SideBuy {
						floatPips = (curPrice - p.EntryPrice) * pipMult
					} else {
						floatPips = (p.EntryPrice - curPrice) * pipMult
					}

					posList[i] = web.PositionTelemetry{
						OrderID:            p.OrderID,
						Symbol:             p.Symbol,
						Side:               p.Side.String(),
						Lots:               p.Lots,
						EntryPrice:         p.EntryPrice,
						CurrentPrice:       curPrice,
						StopLoss:           p.StopLoss,
						TakeProfit:         p.TakeProfit,
						FloatingPnL:        p.CurrentPnL,
						FloatingPips:       floatPips,
						HoldingTimeSec:     holdSec,
						PartialTPTriggered: p.PartialTPTriggered,
						ProfitStage:        p.ProfitStage,
						MFEUSD:             p.MaxFavorableUSD,
						MAEUSD:             p.MaxAdverseUSD,
						MFEPips:            p.MaxFavorablePips,
						MAEPips:            p.MaxAdversePips,
					}
				}

				symMap := make(map[string]web.SymbolData, len(symbols))
				now := time.Now()
				pipelineMu.RLock()
				for _, rawSym := range symbols {
					sym := normalizeSymbol(rawSym)
					p, exists := pipelines[sym]
					pipMult := model.PipMultiplier(sym)
					if exists {
						if t, ok := p.Buffer.Last(); ok && t.Bid > 0 {
							atrVal := p.Strategy.ATR()
							if atrVal <= 0 {
								atrVal = cfg.Strategy.ATRMinimum
							}
							regime, conf := p.AIFilter.EvaluateCurrent(
								t, p.Strategy.FastEMA(), p.Strategy.SlowEMA(), p.Strategy.RSI(), atrVal, 10.0, model.Candle{},
							)

							// Calculate price changes over 5m, 15m, 1h, 4h, 24h
							curP := t.Bid
							calcChange := func(duration time.Duration) float64 {
								targetTime := now.Add(-duration)
								for _, snap := range p.Snapshots {
									if snap.Timestamp.After(targetTime) {
										if snap.Price > 0 {
											return (curP - snap.Price) / snap.Price * 100.0
										}
										break
									}
								}
								if p.OpenPrice > 0 {
									return (curP - p.OpenPrice) / p.OpenPrice * 100.0
								}
								return 0.0
							}

							timeAgo := int64(0)
							lastTickStr := "--:--:--"
							if !p.LastTickTime.IsZero() {
								timeAgo = int64(now.Sub(p.LastTickTime).Seconds())
								if timeAgo < 0 {
									timeAgo = 0
								}
								lastTickStr = p.LastTickTime.Format("15:04:05")
							}

							high := p.High24h
							if high == 0 {
								high = t.Bid
							}
							low := p.Low24h
							if low == 0 {
								low = t.Bid
							}

							prof := model.DetectAssetClass(sym)
							symMap[sym] = web.SymbolData{
								Bid:          t.Bid,
								Ask:          t.Ask,
								SpreadPip:    t.SpreadPips(pipMult),
								TPS:          12.0,
								AIRegime:     regime.String(),
								AIConf:       conf,
								AssetClass:   string(prof.Class),
								AssetIcon:    prof.Icon,
								LastSignal:   p.LastSignal,
								SignalStatus: p.SignalStatus,
								SignalReason: p.SignalReason,
								SignalTime:   p.SignalTime,
								LastTickTime: lastTickStr,
								TimeAgoSec:   timeAgo,
								High24h:      high,
								Low24h:       low,
								Change24hPct: calcChange(24 * time.Hour),
								Change5mPct:  calcChange(5 * time.Minute),
								Change15mPct: calcChange(15 * time.Minute),
								Change1hPct:  calcChange(1 * time.Hour),
								Change4hPct:  calcChange(4 * time.Hour),
								VolRatio:     p.Strategy.VolRatio(),
							}
							continue
						}
					}
					// Always show ticker card in STANDBY state if tick has not arrived yet
					prof := model.DetectAssetClass(sym)
					symMap[sym] = web.SymbolData{
						Bid:          0.0,
						Ask:          0.0,
						SpreadPip:    0.0,
						TPS:          0.0,
						AIRegime:     "STANDBY",
						AIConf:       0.0,
						AssetClass:   string(prof.Class),
						AssetIcon:    prof.Icon,
						LastSignal:   "",
						SignalStatus: "STANDBY",
						SignalReason: "Waiting for market feed...",
						SignalTime:   "--:--:--",
						LastTickTime: "--:--:--",
						TimeAgoSec:   0,
						High24h:      0.0,
						Low24h:       0.0,
					}
				}
				pipelineMu.RUnlock()

				currBalance := getLiveFloat(&liveBalance, cfg.Risk.InitialEquity)
				currEquity := getLiveFloat(&liveEquity, currBalance+totalFloat)

				status := "LIVE TRADING"
				if emergencyHalted.Load() {
					status = "EMERGENCY HALTED"
				}

				modeStr := "BALANCED"
				if m := currentTradingMode.Load(); m != nil {
					modeStr = string(m.(TradingMode))
				}

				focusStr := "GOLD_ONLY"
				if f := currentMarketFocus.Load(); f != nil {
					focusStr = string(f.(MarketFocus))
				}

				// Candles map for TradingView charts (including forming candles & symbol alias mappings)
				candleMap := make(map[string][]web.CandleTelemetry)
				pipelineMu.RLock()
				for s, p := range pipelines {
					candles := make([]web.CandleTelemetry, 0, len(p.RecentCandles)+1)
					if len(p.RecentCandles) > 0 {
						candles = append(candles, p.RecentCandles...)
					}
					// Include current forming candle if available
					if currCandle, ok := p.Aggregator.Current(); ok && currCandle.Close > 0 {
						candles = append(candles, web.CandleTelemetry{
							Time:   currCandle.TimestampNs / 1e9,
							Open:   currCandle.Open,
							High:   currCandle.High,
							Low:    currCandle.Low,
							Close:  currCandle.Close,
							Volume: currCandle.Volume,
						})
					}
					if len(candles) > 0 {
						clean := strings.ToUpper(strings.TrimRight(s, "cmCM."))
						candleMap[s] = candles
						candleMap[p.Symbol] = candles
						candleMap[clean] = candles
						candleMap[clean+"c"] = candles
						candleMap[clean+"m"] = candles
					}
				}
				pipelineMu.RUnlock()

				perfStats := computePerformanceTelemetry()

				accTypeStr := "REGULAR"
				if at := detectedAccountType.Load(); at != nil {
					accTypeStr = at.(string)
				}
				accCurrStr := "USD"
				if ac := accountCurrency.Load(); ac != nil && ac.(string) != "" {
					accCurrStr = ac.(string)
				}

				nowTimeNs := time.Now().UnixNano()
				var upcomingNews *web.NewsTelemetry
				if ev, countdown, isBlackout, ok := calendarClient.GetUpcomingEvent("USD", cfg.News.BlackoutBefore, cfg.News.BlackoutAfter, nowTimeNs); ok && ev != nil {
					evSec := ev.TimestampNs / 1e9
					endSec := (ev.TimestampNs + cfg.News.BlackoutAfter.Nanoseconds()) / 1e9
					remSec := int64(0)
					if isBlackout {
						remSec = (ev.TimestampNs + cfg.News.BlackoutAfter.Nanoseconds() - nowTimeNs) / 1e9
						if remSec < 0 {
							remSec = 0
						}
					}
					upcomingNews = &web.NewsTelemetry{
						Title:                ev.Title,
						Currency:             ev.Currency,
						Impact:               string(ev.Impact),
						CountdownSec:         countdown,
						IsBlackout:           isBlackout,
						EventTimeSec:         evSec,
						BlackoutEndSec:       endSec,
						BlackoutRemainingSec: remSec,
					}
				}

				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				memAllocMB := float64(m.Alloc) / (1024.0 * 1024.0)

				return web.TelemetryPayload{
					TimestampNs:         nowTimeNs,
					BotStatus:           status,
					CircuitBreaker:      riskMgr.IsCircuitOpen() || emergencyHalted.Load(),
					Balance:             currBalance,
					Equity:              currEquity,
					FloatingPnL:         totalFloat,
					DailyPnL:            riskMgr.DailyPnL(),
					DailyDrawdownPct:    0.0,
					WinRatePct:          perfStats.WinRatePct,
					ProfitFactor:        perfStats.ProfitFactor,
					ProfitTargetReached: riskMgr.IsProfitTargetReached(),
					ProfitTargetAmount:  riskMgr.ProfitTargetAmount(),
					ActivePositions:     posList,
					Symbols:             symMap,
					RecentEvents:        getRecentEvents(),
					UpcomingNews:        upcomingNews,
					AIFilterEnabled:     aiFilterEnabled.Load(),
					TradingMode:         modeStr,
					MarketFocus:         focusStr,
					AccountType:         accTypeStr,
					AccountCurrency:     accCurrStr,
					Performance:         perfStats,
					LiveCandles:         candleMap,
					TotalTicksProcessed: totalTicksProcessed.Load(),
					MemoryAllocMB:       memAllocMB,
					ActiveLotSize:       getLiveFloat(&activeUserLot, 0.01),
				}
			},
		})
		webServer.Start(ctx)
	}

	// -------------------------------------------------------------------------
	// 6. Create pipeline channels & launch goroutines
	// -------------------------------------------------------------------------
	tickCh := make(chan model.Tick, cfg.MarketData.TickChannelBuf)
	signalCh := make(chan model.Signal, 64)

	// Goroutine A: Market feed → tick channel
	if mt5Adapter != nil {
		mt5Adapter.StreamTicks(ctx, tickCh)

		// Goroutine A.2: MT5 Historical M5 Candle Backfill
		go func() {
			time.Sleep(1500 * time.Millisecond) // Let TCP socket complete initial handshake
			log.Printf("[backfill] 🕯️ Requesting historical M5 candle backfill from MT5...")
			for _, sym := range symbols {
				candlesDTO, err := mt5Adapter.FetchCandles(ctx, sym, "M5", 100)
				if err != nil {
					log.Printf("[backfill] ⚠️ Could not fetch M5 candles for %s: %v", sym, err)
					continue
				}
				if len(candlesDTO) > 0 {
					log.Printf("[backfill] 🚀 Successfully fetched %d M5 bars from MT5 for %s!", len(candlesDTO), sym)
					pipelineMu.Lock()
					clean := normalizeSymbol(sym)
					p, exists := pipelines[clean]
					if !exists {
						p = createSymbolPipeline(clean, cfg)
						pipelines[clean] = p
					}
					pipelineMu.Unlock()

					p.mu.Lock()
					p.RecentCandles = make([]web.CandleTelemetry, len(candlesDTO))
					for i, cd := range candlesDTO {
						tele := web.CandleTelemetry{
							Time:   cd.Time,
							Open:   cd.Open,
							High:   cd.High,
							Low:    cd.Low,
							Close:  cd.Close,
							Volume: float64(cd.Volume),
						}
						p.RecentCandles[i] = tele

						// Warm up strategy indicators (FastEMA, SlowEMA, ATR)
						cModel := model.Candle{
							Symbol:      sym,
							Open:        cd.Open,
							High:        cd.High,
							Low:         cd.Low,
							Close:       cd.Close,
							Volume:      float64(cd.Volume),
							TimestampNs: cd.Time * 1e9,
						}
						p.Strategy.OnCandle(cModel)

						// Warm up Macro Confluence indicators & Trend
						fastM5 := p.M5FastEMA.Update(cd.Close)
						slowM5 := p.M5SlowEMA.Update(cd.Close)
						if !math.IsNaN(fastM5) && !math.IsNaN(slowM5) {
							if fastM5 > slowM5*1.0001 {
								p.M5Trend = "BULLISH"
							} else if fastM5 < slowM5*0.9999 {
								p.M5Trend = "BEARISH"
							} else {
								p.M5Trend = "NEUTRAL"
							}
						}
						if i%3 == 0 { // M15 approximation
							fastM15 := p.M15FastEMA.Update(cd.Close)
							slowM15 := p.M15SlowEMA.Update(cd.Close)
							if !math.IsNaN(fastM15) && !math.IsNaN(slowM15) {
								if fastM15 > slowM15*1.0001 {
									p.M15Trend = "BULLISH"
								} else if fastM15 < slowM15*0.9999 {
									p.M15Trend = "BEARISH"
								}
							}
						}
					}
					cachedCopy := make([]web.CandleTelemetry, len(p.RecentCandles))
					copy(cachedCopy, p.RecentCandles)
					p.mu.Unlock()

					saveCandlesCache(sym, cachedCopy)
					saveCandlesCache(clean, cachedCopy)
					log.Printf("[backfill] ✅ Warm-up complete for %s (FastEMA=%.2f, SlowEMA=%.2f)", sym, p.Strategy.FastEMA(), p.Strategy.SlowEMA())
				}
			}
		}()
	} else {
		for _, sym := range symbols {
			go mockMarketFeed(ctx, sym, tickCh)
		}
	}

	var (
		symbolCooldownMu sync.RWMutex
		lastOrderTime    = make(map[string]time.Time)
		lastLossTime     = make(map[string]time.Time)
	)

	// Goroutine B: Tick router & processor → strategy → signal channel
	go func() {
		for {
			select {
			case tick, ok := <-tickCh:
				if !ok {
					return
				}
				totalTicksProcessed.Add(1)

				symKey := normalizeSymbol(tick.Symbol)
				if !strings.Contains(symKey, "XAU") && !strings.Contains(symKey, "GOLD") {
					// STRICT GOLD GUARD: Discard any tick for non-gold symbols
					continue
				}
				if !allowedSymbols[symKey] {
					continue
				}

				pipelineMu.RLock()
				p, exists := pipelines[symKey]
				pipelineMu.RUnlock()

				if !exists {
					pipelineMu.Lock()
					p, exists = pipelines[symKey]
					if !exists {
						p = createSymbolPipeline(tick.Symbol, cfg)
						pipelines[symKey] = p
						log.Printf("[pipeline] dynamically registered symbol pipeline for %s", symKey)
					}
					pipelineMu.Unlock()
				}

				nowTick := time.Now()
				p.mu.Lock()
				p.LastTickTime = nowTick
				if p.High24h == 0 || tick.Bid > p.High24h {
					p.High24h = tick.Bid
				}
				if p.Low24h == 0 || (tick.Bid > 0 && tick.Bid < p.Low24h) {
					p.Low24h = tick.Bid
				}
				if p.OpenPrice == 0 && tick.Bid > 0 {
					p.OpenPrice = tick.Bid
				}
				if len(p.Snapshots) == 0 || nowTick.Sub(p.Snapshots[len(p.Snapshots)-1].Timestamp) >= 30*time.Second {
					p.Snapshots = append(p.Snapshots, PriceSnapshot{Timestamp: nowTick, Price: tick.Bid})
					if len(p.Snapshots) > 2880 {
						p.Snapshots = p.Snapshots[len(p.Snapshots)-2880:]
					}
					// Periodically re-derive true 24h rolling high/low from active snapshots
					if len(p.Snapshots)%10 == 0 && len(p.Snapshots) > 0 {
						minP := p.Snapshots[0].Price
						maxP := p.Snapshots[0].Price
						for _, s := range p.Snapshots {
							if s.Price > maxP {
								maxP = s.Price
							}
							if s.Price > 0 && s.Price < minP {
								minP = s.Price
							}
						}
						if maxP > 0 {
							p.High24h = maxP
						}
						if minP > 0 {
							p.Low24h = minP
						}
					}
				}
				p.mu.Unlock()

				p.Buffer.Push(tick)
				mtfMgr.OnTick(tick)
				p.SpreadFilter.OnTick(tick)
				if velocityFilter != nil {
					velocityFilter.OnTick(tick)
				}
				p.AIFilter.OnTick(tick)

				// Process Multi-Timeframe M5, M15, and H1 candle streams concurrently
				m5Candle, m5Closed, m15Candle, m15Closed, h1Candle, h1Closed := p.MTFAggregator.OnTick(tick)

				if m5Closed {
					fastM5 := p.M5FastEMA.Update(m5Candle.Close)
					slowM5 := p.M5SlowEMA.Update(m5Candle.Close)
					p.mu.Lock()
					if !math.IsNaN(fastM5) && !math.IsNaN(slowM5) {
						if fastM5 > slowM5*1.0001 {
							p.M5Trend = "BULLISH"
						} else if fastM5 < slowM5*0.9999 {
							p.M5Trend = "BEARISH"
						} else {
							p.M5Trend = "NEUTRAL"
						}
					}
					p.mu.Unlock()
				}

				if m15Closed {
					fastM15 := p.M15FastEMA.Update(m15Candle.Close)
					slowM15 := p.M15SlowEMA.Update(m15Candle.Close)
					_ = p.M15RSI.Update(m15Candle.Close)
					atrM15 := p.M15ATR.Update(m15Candle)
					if math.IsNaN(atrM15) || atrM15 <= 0 {
						atrM15 = m15Candle.Range()
						if atrM15 <= 0 {
							atrM15 = cfg.Strategy.ATRMinimum
						}
					}

					p.mu.Lock()
					if !math.IsNaN(fastM15) && !math.IsNaN(slowM15) {
						if fastM15 > slowM15*1.0001 {
							p.M15Trend = "BULLISH"
						} else if fastM15 < slowM15*0.9999 {
							p.M15Trend = "BEARISH"
						} else {
							p.M15Trend = "NEUTRAL"
						}
					}
					p.mu.Unlock()

					// Feed completed M15 candle into Gaussian HMM Engine
					hmmState, hmmConf := p.AIFilter.UpdateHMM(m15Candle, atrM15)
					log.Printf("[ai-hmm] 🧬 %s M15 HMM State: %s (conf=%.1f%%, ATR=%.5f, BuyVol=%.0f, SellVol=%.0f)",
						symKey, hmmState.String(), hmmConf*100.0, atrM15, m15Candle.BuyerVol, m15Candle.SellerVol)
				}

				if h1Closed {
					fastH1 := p.H1FastEMA.Update(h1Candle.Close)
					slowH1 := p.H1SlowEMA.Update(h1Candle.Close)
					p.mu.Lock()
					if !math.IsNaN(fastH1) && !math.IsNaN(slowH1) {
						if fastH1 > slowH1*1.0001 {
							p.H1Trend = "BULLISH"
						} else if fastH1 < slowH1*0.9999 {
							p.H1Trend = "BEARISH"
						} else {
							p.H1Trend = "NEUTRAL"
						}
					}
					p.mu.Unlock()
					log.Printf("[macro-h1] 🏛️ %s H1 Macro Trend: %s", symKey, p.H1Trend)
				}

				atrVal := p.Strategy.ATR()
				if atrVal <= 0 {
					atrVal = cfg.Strategy.ATRMinimum
				}
				pipMult := model.PipMultiplier(tick.Symbol)

				// Position tracker on tick
				closes := tracker.OnTick(tick, atrVal, pipMult)
				for _, ce := range closes {
					telemetry.RecordClose(ce)
					if ce.IsPartial {
						log.Printf("[tracker] partial close: %s lots=%.2f reason=%s", ce.OrderID, ce.Lots, ce.Reason)
						_ = activeBroker.ClosePartial(ctx, ce.OrderID, ce.Lots)
						_ = alerter.Send(ctx, notifier.Alert{
							Level:     notifier.AlertInfo,
							Title:     "Partial TP Executed",
							Message:   fmt.Sprintf("Order %s partially closed %.2f lots (%s)", ce.OrderID, ce.Lots, ce.Reason),
							Timestamp: time.Now(),
						})
					} else {
						mfeRecordsMu.Lock()
						if len(mfeRecords) > 300 {
							mfeRecords = make(map[string]struct{ mfeUSD, maeUSD, mfePips, maePips float64 })
						}
						mfeRecords[ce.OrderID] = struct{ mfeUSD, maeUSD, mfePips, maePips float64 }{
							mfeUSD: ce.MaxFavorableUSD, maeUSD: ce.MaxAdverseUSD, mfePips: ce.MaxFavorablePips, maePips: ce.MaxAdversePips,
						}
						mfeRecordsMu.Unlock()

						if ce.Reason == "TIME_STOP_HIT" {
							log.Printf("[tracker] closing stagnant/time-stop position %s in broker", ce.OrderID)
							_ = activeBroker.Close(ctx, ce.OrderID)
						}

						if mt5Adapter == nil {
							riskMgr.RecordClose(ce.PnL)
							recordCompletedTrade(CompletedTrade{
								Ticket:           ce.OrderID,
								Symbol:           tick.Symbol,
								Side:             ce.Side.String(),
								Lots:             ce.Lots,
								Entry:            ce.EntryPrice,
								Exit:             ce.ExitPrice,
								NetPnL:           ce.PnL,
								Pips:             ce.Pips,
								Duration:         ce.Duration,
								CloseTime:        time.Now(),
								MaxFavorableUSD:  ce.MaxFavorableUSD,
								MaxAdverseUSD:    ce.MaxAdverseUSD,
								MaxFavorablePips: ce.MaxFavorablePips,
								MaxAdversePips:   ce.MaxAdversePips,
							}, riskMgr)

							if tgBot != nil {
								tgBot.SendTradeClose(ctx, ce.OrderID, tick.Symbol, ce.Side.String(), ce.Lots, ce.EntryPrice, ce.ExitPrice, ce.PnL, ce.Pips, ce.Duration)
								if riskMgr.IsProfitTargetReached() {
									tgBot.SendProfitTargetAlert(ctx, riskMgr.DailyPnL(), riskMgr.ProfitTargetAmount(), riskMgr.Equity())
								}
							}
						}

						if ce.PnL < 0 {
							symbolCooldownMu.Lock()
							lastLossTime[symKey] = time.Now()
							symbolCooldownMu.Unlock()
							log.Printf("[anti-revenge] 🛡️ Loss of $%.2f on %s — activated 90s post-loss cooldown pause", ce.PnL, symKey)
						}
						err := activeBroker.Close(ctx, ce.OrderID)
						if err != nil && (strings.Contains(err.Error(), "10015") || strings.Contains(err.Error(), "10029") || strings.Contains(strings.ToLower(err.Error()), "frozen") || strings.Contains(strings.ToLower(err.Error()), "invalid ticket") || strings.Contains(strings.ToLower(err.Error()), "not found")) {
							log.Printf("[tracker] position %s already executing / closed by broker server-side", ce.OrderID)
						} else if err != nil {
							log.Printf("[tracker] position close error for %s: %v", ce.OrderID, err)
						} else {
							log.Printf("[tracker] position closed: %s lots=%.2f reason=%s", ce.OrderID, ce.Lots, ce.Reason)
							_ = alerter.Send(ctx, notifier.Alert{
								Level:     notifier.AlertInfo,
								Title:     "Position Closed",
								Message:   fmt.Sprintf("Order %s closed (%s)", ce.OrderID, ce.Reason),
								Timestamp: time.Now(),
							})
						}
					}
				}

				// Propagate active position SL/TP modifications (Break-Even & Trailing Stop) to broker
				for _, me := range tracker.DrainModifications() {
					log.Printf("[tracker] 🛡️ updating SL/TP in broker for %s: SL=%.5f TP=%.5f", me.OrderID, me.StopLoss, me.TakeProfit)
					if err := activeBroker.ModifyPosition(ctx, me.OrderID, me.StopLoss, me.TakeProfit); err != nil {
						log.Printf("[tracker] warning: failed to modify broker position %s: %v", me.OrderID, err)
					}
				}

				// Tick strategy — ONLY if no candle closes on this tick
				// (prevents duplicate signals when OnTick + OnCandle fire simultaneously)
				sig := p.Strategy.OnTick(tick)
				candleClosedThisTick := false

				// Candle strategies (Dual-Mode: Trend Hunter + Range Scalper)
				if candle, closed := p.Aggregator.OnTick(tick); closed {
					candleClosedThisTick = true
					// Store rolling candles for TradingView Lightweight Charts
					p.AddRecentCandle(web.CandleTelemetry{
						Time:   candle.TimestampNs / 1e9,
						Open:   candle.Open,
						High:   candle.High,
						Low:    candle.Low,
						Close:  candle.Close,
						Volume: candle.Volume,
					})

					// 1. Evaluate Momentum Scalper (Trend Mode)
					sigTrend := p.Strategy.OnCandle(candle)
					if sigTrend.IsActionable() {
						log.Printf("[strategy] 🎯 TREND SIGNAL: %s %s @ %.5f (FastEMA=%.5f SlowEMA=%.5f RSI=%.1f ATR=%.5f)",
							sigTrend.Type, sigTrend.Symbol, candle.Close, p.Strategy.FastEMA(), p.Strategy.SlowEMA(), p.Strategy.RSI(), p.Strategy.ATR())
						select {
						case signalCh <- sigTrend:
						default:
							log.Println("[pipeline] signal channel full, dropping trend signal")
						}
					}

					// 2. Evaluate Mean-Reversion Scalper (Range Mode)
					if p.RangeStrategy != nil && cfg.RangeStrategy.Enabled {
						sigRange := p.RangeStrategy.OnCandle(candle)
						if sigRange.IsActionable() {
							upper, mid, lower, rsiVal, _, _ := p.RangeStrategy.GetIndicators()
							log.Printf("[strategy] 🔄 RANGE SIGNAL: %s %s @ %.5f (Upper=%.5f Mid=%.5f Lower=%.5f RSI=%.1f)",
								sigRange.Type, sigRange.Symbol, candle.Close, upper, mid, lower, rsiVal)
							select {
							case signalCh <- sigRange:
							default:
								log.Println("[pipeline] signal channel full, dropping range signal")
							}
						}
					}
				}

				// Only send tick-based signal if no candle closed on this tick
				// This is the critical anti-duplicate guard
				if !candleClosedThisTick && sig.IsActionable() {
					select {
					case signalCh <- sig:
					default:
						log.Println("[pipeline] signal channel full, dropping tick signal")
					}
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	// Goroutine C: Signal consumer → AI Filter → Correlation Guard → Risk → Dispatcher
	go func() {
		for {
			select {
			case sig, ok := <-signalCh:
				if !ok {
					return
				}

				symKey := normalizeSymbol(sig.Symbol)
				pipelineMu.RLock()
				p, exists := pipelines[symKey]
				pipelineMu.RUnlock()
				if !exists {
					continue
				}

				lastTick, hasTick := p.Buffer.Last()
				if !hasTick {
					continue
				}

				nowStr := time.Now().Format("15:04:05")
				prof := model.DetectAssetClass(sig.Symbol)

				// 0.1 Gate 0: London Open Trap Blackout (14:00 - 14:45 WIB)
				// Empirically proven: 14:00 session generated 80% of historical losses (-$149, 6.7% win rate) due to institutional Judas swings.
				wibTime := time.Now().UTC().Add(7 * time.Hour)
				if wibTime.Hour() == 14 && wibTime.Minute() < 45 {
					londonTrapReason := "Gate 0: London Open Trap Blackout (14:00 - 14:45 WIB pause protects against Judas swings)"
					log.Printf("[session-guard] 🛑 LONDON TRAP BLACKOUT: %s %s — %s", sig.Type.String(), symKey, londonTrapReason)
					p.SetSignalStatus(sig.Type.String(), "REJECTED", londonTrapReason, nowStr)
					recordSignalEvent(web.SignalEvent{
						Time:    nowStr,
						Symbol:  sig.Symbol,
						Type:    sig.Type.String(),
						Price:   lastTick.MidPrice(),
						Status:  "REJECTED",
						Regime:  "LONDON_OPEN_BLACKOUT",
						ConfPct: 0.0,
						Reason:  londonTrapReason,
					})
					continue
				}

				// 0.12 Gate 0.5: Spread Anomaly Spike Guard (Anti-Slippage Freeze)
				if p.SpreadFilter != nil {
					if allowed, reason := p.SpreadFilter.Allow(lastTick); !allowed {
						spreadSpikeReason := fmt.Sprintf("Gate 0.5: Spread Anomaly Spike — %s", reason)
						log.Printf("[risk-guard] 🛑 SPREAD SPIKE: %s %s — %s", sig.Type.String(), symKey, spreadSpikeReason)
						p.SetSignalStatus(sig.Type.String(), "REJECTED", spreadSpikeReason, nowStr)
						recordSignalEvent(web.SignalEvent{
							Time:    nowStr,
							Symbol:  sig.Symbol,
							Type:    sig.Type.String(),
							Price:   lastTick.MidPrice(),
							Status:  "REJECTED",
							Regime:  "SPREAD_SPIKE_LOCK",
							ConfPct: 0.0,
							Reason:  spreadSpikeReason,
						})
						continue
					}
				}

				// 0.15 Gate 1: Hard Macro Trend Lock (M5 / M15 / H1 Hierarchical Confluence)
				// STRICT RULE: No BUY during Bearish Trend, No SELL during Bullish Trend
				p.mu.RLock()
				m5Trend := p.M5Trend
				m15Trend := p.M15Trend
				h1Trend := p.H1Trend
				p.mu.RUnlock()

				effectiveTrend := h1Trend
				if effectiveTrend == "NEUTRAL" {
					effectiveTrend = m15Trend
				}
				if effectiveTrend == "NEUTRAL" {
					effectiveTrend = m5Trend
				}

				if effectiveTrend == "BEARISH" && sig.Type == model.Buy {
					htfReason := fmt.Sprintf("Gate 1: BUY forbidden during %s Bearish Trend (H1=%s, M15=%s, M5=%s)", effectiveTrend, h1Trend, m15Trend, m5Trend)
					log.Printf("[macro-trend] 🛑 COUNTER-TREND REJECTED: %s %s — %s", sig.Type.String(), symKey, htfReason)

					p.SetSignalStatus(sig.Type.String(), "REJECTED", htfReason, nowStr)

					recordSignalEvent(web.SignalEvent{
						Time:    nowStr,
						Symbol:  sig.Symbol,
						Type:    sig.Type.String(),
						Price:   lastTick.MidPrice(),
						Status:  "REJECTED",
						Regime:  "HTF_BEARISH_LOCK",
						ConfPct: 0.0,
						Reason:  htfReason,
					})
					continue
				} else if effectiveTrend == "BULLISH" && sig.Type == model.Sell {
					htfReason := fmt.Sprintf("Gate 1: SELL forbidden during %s Bullish Trend (H1=%s, M15=%s, M5=%s)", effectiveTrend, h1Trend, m15Trend, m5Trend)
					log.Printf("[macro-trend] 🛑 COUNTER-TREND REJECTED: %s %s — %s", sig.Type.String(), symKey, htfReason)

					p.SetSignalStatus(sig.Type.String(), "REJECTED", htfReason, nowStr)

					recordSignalEvent(web.SignalEvent{
						Time:    nowStr,
						Symbol:  sig.Symbol,
						Type:    sig.Type.String(),
						Price:   lastTick.MidPrice(),
						Status:  "REJECTED",
						Regime:  "HTF_BULLISH_LOCK",
						ConfPct: 0.0,
						Reason:  htfReason,
					})
					continue
				} else if effectiveTrend == "NEUTRAL" {
					htfReason := "Gate 1: Waiting for initial trend formation (M5/M15/H1 warming up)"
					log.Printf("[macro-trend] ⏳ WARMING UP: %s %s — %s", sig.Type.String(), symKey, htfReason)
					p.SetSignalStatus(sig.Type.String(), "SKIPPED", htfReason, nowStr)
					continue
				}

				if (h1Trend == "BULLISH" || m15Trend == "BULLISH") && sig.Type == model.Buy {
					log.Printf("[macro-trend] 💎 GOLDEN PULLBACK SETUP: %s BUY dip retest within Bullish trend", symKey)
				} else if (h1Trend == "BEARISH" || m15Trend == "BEARISH") && sig.Type == model.Sell {
					log.Printf("[macro-trend] 💎 GOLDEN PULLBACK SETUP: %s SELL rally retest within Bearish trend", symKey)
				}

				// 0.18 Gate 1.5: Key Level 24H Liquidity Guard (No buying into 24H High, no selling into 24H Low)
				atrVal := p.Strategy.ATR()
				if atrVal <= 0 {
					atrVal = 3.50
				}
				p.mu.RLock()
				high24h := p.High24h
				low24h := p.Low24h
				p.mu.RUnlock()

				if high24h > 0 && low24h > 0 && high24h > low24h {
					if sig.Type == model.Buy && (high24h-lastTick.Ask) < atrVal*0.35 {
						keyReason := fmt.Sprintf("Gate 1.5: Key Level Guard — BUY forbidden within $%.2f (<0.35x ATR) of 24H High ($%.2f)", high24h-lastTick.Ask, high24h)
						log.Printf("[key-level] 🛑 RESISTANCE CEILING: %s %s — %s", sig.Type.String(), symKey, keyReason)
						p.SetSignalStatus(sig.Type.String(), "REJECTED", keyReason, nowStr)
						recordSignalEvent(web.SignalEvent{
							Time:    nowStr,
							Symbol:  sig.Symbol,
							Type:    sig.Type.String(),
							Price:   lastTick.MidPrice(),
							Status:  "REJECTED",
							Regime:  "KEY_LEVEL_RESISTANCE",
							ConfPct: 0.0,
							Reason:  keyReason,
						})
						continue
					} else if sig.Type == model.Sell && (lastTick.Bid-low24h) < atrVal*0.35 {
						keyReason := fmt.Sprintf("Gate 1.5: Key Level Guard — SELL forbidden within $%.2f (<0.35x ATR) of 24H Low ($%.2f)", lastTick.Bid-low24h, low24h)
						log.Printf("[key-level] 🛑 SUPPORT FLOOR: %s %s — %s", sig.Type.String(), symKey, keyReason)
						p.SetSignalStatus(sig.Type.String(), "REJECTED", keyReason, nowStr)
						recordSignalEvent(web.SignalEvent{
							Time:    nowStr,
							Symbol:  sig.Symbol,
							Type:    sig.Type.String(),
							Price:   lastTick.MidPrice(),
							Status:  "REJECTED",
							Regime:  "KEY_LEVEL_SUPPORT",
							ConfPct: 0.0,
							Reason:  keyReason,
						})
						continue
					}
				}

				// 0.2 Multi-Timeframe Confluence Guard (M1 + M5 + H1 Alignment)
				mtfAllowed, mtfScore, mtfReason := mtfMgr.CheckConfluence(sig.Symbol, sig.Type)
				if !mtfAllowed {
					log.Printf("[mtf-confluence] 🛑 HTF CONFLICT: %s %s (score=%.0f%%) — %s",
						sig.Type.String(), sig.Symbol, mtfScore*100.0, mtfReason)

					p.SetSignalStatus(sig.Type.String(), "REJECTED", mtfReason, nowStr)

					recordSignalEvent(web.SignalEvent{
						Time:    nowStr,
						Symbol:  sig.Symbol,
						Type:    sig.Type.String(),
						Price:   lastTick.MidPrice(),
						Status:  "REJECTED",
						Regime:  "HTF_CONFLICT",
						ConfPct: mtfScore * 100.0,
						Reason:  mtfReason,
					})
					continue
				}

				atrVal = p.Strategy.ATR()
				if atrVal <= 0 {
					atrVal = cfg.Strategy.ATRMinimum
				}

				// 0.3 Emergency Kill-Switch Check
				if emergencyHalted.Load() {
					log.Println("[pipeline] order execution halted: emergency kill-switch is active")
					continue
				}

				nowStr = time.Now().Format("15:04:05")
				aiConf := 0.55
				regime := ai.RegimeTrendingBullish

				// 1. AI Machine Learning & Market Regime Guard
				if aiFilterEnabled.Load() {
					var currCandle model.Candle
					if c, ok := p.Aggregator.Current(); ok {
						currCandle = c
					}
					var aiAllowed bool
					var aiReason string
					aiAllowed, aiConf, regime, aiReason = p.AIFilter.EvaluateSignal(
						sig, lastTick, p.Strategy.FastEMA(), p.Strategy.SlowEMA(), p.Strategy.RSI(), atrVal, 5.0, currCandle,
					)
					isRangeSig := strings.Contains(strings.ToLower(sig.StrategyID), "range")

					if !aiAllowed {
						cleanReason := aiReason
						log.Printf("[ai-filter] 🛑 %s %s (regime=%s, conf=%.1f%%) — %s",
							sig.Type.String(), sig.Symbol, regime.String(), aiConf*100.0, cleanReason)

						p.SetSignalStatus(sig.Type.String(), "REJECTED", cleanReason, nowStr)

						recordSignalEvent(web.SignalEvent{
							Time:    nowStr,
							Symbol:  sig.Symbol,
							Type:    sig.Type.String(),
							Price:   lastTick.MidPrice(),
							Status:  "REJECTED",
							Regime:  regime.String(),
							ConfPct: aiConf * 100.0,
							Reason:  cleanReason,
						})
						continue
					}

					stratLabel := "HMM Trend Momentum"
					if isRangeSig {
						stratLabel = "Range Reversion"
					}
					cleanApproved := fmt.Sprintf("%s (HMM Conf: %.0f%%)", stratLabel, aiConf*100.0)

					log.Printf("[ai-filter] ✅ SIGNAL APPROVED: %s %s (%s, regime=%s, confidence=%.1f%%)",
						sig.Type.String(), sig.Symbol, stratLabel, regime.String(), aiConf*100.0)

					p.SetSignalStatus(sig.Type.String(), "APPROVED", cleanApproved, nowStr)

					recordSignalEvent(web.SignalEvent{
						Time:    nowStr,
						Symbol:  sig.Symbol,
						Type:    sig.Type.String(),
						Price:   lastTick.MidPrice(),
						Status:  "APPROVED",
						Regime:  regime.String(),
						ConfPct: aiConf * 100.0,
						Reason:  cleanApproved,
					})
				}

				// 1.8 Anti-Revenge Post-Loss Cooldown & Order Spacing Throttle
				symbolCooldownMu.RLock()
				lastLoss := lastLossTime[symKey]
				lastOrd := lastOrderTime[symKey]
				symbolCooldownMu.RUnlock()

				nowTime := time.Now()
				// A. Post-loss cooldown (300 seconds pause after taking a loss to prevent revenge churn into choppy market)
				if !lastLoss.IsZero() && nowTime.Sub(lastLoss) < 300*time.Second {
					remSec := int(300 - nowTime.Sub(lastLoss).Seconds())
					cooldownReason := fmt.Sprintf("Anti-Revenge Pause: resting 300s after loss (%ds remaining)", remSec)
					log.Printf("[pipeline] 🛡️ COOLDOWN SKIPPED: %s %s", symKey, cooldownReason)

					p.SetSignalStatus("", "SKIPPED", cooldownReason, nowStr)

					recordSignalEvent(web.SignalEvent{
						Time:    nowStr,
						Symbol:  sig.Symbol,
						Type:    sig.Type.String(),
						Price:   lastTick.MidPrice(),
						Status:  "SKIPPED",
						Regime:  regime.String(),
						ConfPct: aiConf * 100.0,
						Reason:  cooldownReason,
					})
					continue
				}

				// B. Order spacing rate limit (minimum 90 seconds between consecutive entries on the same symbol)
				if !lastOrd.IsZero() && nowTime.Sub(lastOrd) < 90*time.Second {
					remSec := int(90 - nowTime.Sub(lastOrd).Seconds())
					spacingReason := fmt.Sprintf("Order Spacing Rate Limit: (%ds remaining)", remSec)
					log.Printf("[pipeline] ⏱️ THROTTLE SKIPPED: %s %s", symKey, spacingReason)

					p.SetSignalStatus("", "SKIPPED", spacingReason, nowStr)

					recordSignalEvent(web.SignalEvent{
						Time:    nowStr,
						Symbol:  sig.Symbol,
						Type:    sig.Type.String(),
						Price:   lastTick.MidPrice(),
						Status:  "SKIPPED",
						Regime:  regime.String(),
						ConfPct: aiConf * 100.0,
						Reason:  spacingReason,
					})
					continue
				}

				// 2. Anti-Duplicate Price-Level Guard (Cluster & Spam Protection)
				// Skips trades only if entry price is identical or too close (e.g. 1544 vs 1544),
				// but ALLOWS new distinct price levels (e.g. 1543 vs 1544) if within max_open_positions.
				trackedNow := tracker.ActivePositions()
				pipMult := model.PipMultiplier(symKey)
				minPipSeparation := 3.0 // Minimum 3.0 pips distance from any existing entry on this symbol
				curPrice := lastTick.MidPrice()

				isPriceDuplicate := false
				var dupEntryPrice float64
				for _, p := range trackedNow {
					if normalizeSymbol(p.Symbol) == symKey {
						pipsDiff := math.Abs(curPrice-p.EntryPrice) * pipMult
						if pipsDiff < minPipSeparation {
							isPriceDuplicate = true
							dupEntryPrice = p.EntryPrice
							break
						}
					}
				}

				if isPriceDuplicate {
					dupReason := fmt.Sprintf("Cluster duplicate @ %.5f (active entry @ %.5f exists, diff < %.1f pips)",
						curPrice, dupEntryPrice, minPipSeparation)
					log.Printf("[pipeline] ⏭️ DUPLICATE SKIPPED: %s %s", symKey, dupReason)

					p.SetSignalStatus("", "SKIPPED", dupReason, nowStr)

					recordSignalEvent(web.SignalEvent{
						Time:    nowStr,
						Symbol:  sig.Symbol,
						Type:    sig.Type.String(),
						Price:   curPrice,
						Status:  "SKIPPED",
						Regime:  regime.String(),
						ConfPct: aiConf * 100.0,
						Reason:  dupReason,
					})
					continue
				}

				// 2.5. Cross-Symbol Portfolio Correlation Guard
				if cfg.Portfolio.EnableCorrelationGuard {
					side := model.SideBuy
					if sig.Type == model.Sell {
						side = model.SideSell
					}
					activePos := make([]model.Position, len(trackedNow))
					for i := range trackedNow {
						activePos[i] = trackedNow[i].Position
					}

					allowed, corrReason := correlationGuard.CheckCorrelation(sig.Symbol, side, activePos)
					if !allowed {
						log.Printf("[portfolio] correlation blocked: %s", corrReason)
						continue
					}
				}

				// 3. Risk Engine Evaluation
				order, err := riskMgr.Evaluate(sig, lastTick, atrVal)
				if err != nil {
					riskReason := fmt.Sprintf("Risk Veto: %v", err)
					if strings.Contains(riskReason, "news-blackout") {
						riskReason = "Blackout Veto: High-impact economic news active (Core CPI/NFP)"
					}
					log.Printf("[risk] signal rejected: %s %s — %s",
						sig.Type, sig.Symbol, riskReason)

					p.SetSignalStatus(sig.Type.String(), "REJECTED", riskReason, nowStr)

					recordSignalEvent(web.SignalEvent{
						Time:    nowStr,
						Symbol:  sig.Symbol,
						Type:    sig.Type.String(),
						Price:   lastTick.MidPrice(),
						Status:  "REJECTED",
						Regime:  "RISK_BLACKOUT",
						ConfPct: 0.0,
						Reason:  riskReason,
					})

					if riskMgr.IsCircuitOpen() {
						_ = alerter.Send(ctx, notifier.Alert{
							Level:     notifier.AlertEmergency,
							Title:     "Circuit Breaker Triggered",
							Message:   "Daily drawdown limit reached — trading halted",
							Timestamp: time.Now(),
						})
					}
					continue
				}

				// 3.1 Direct User Lot Sizing (Manual Priority: whatever user sets in Dashboard/Telegram/Flag)
				userCustomLot := getLiveFloat(&activeUserLot, 0.01)
				if userCustomLot >= 0.01 {
					order.Lots = userCustomLot
				}
				order.Lots = math.Round(order.Lots*100) / 100

				// 3.2 Small-Capital Margin Safety Buffer
				currFreeMargin := getLiveFloat(&liveFreeMargin, getLiveFloat(&liveBalance, cfg.Risk.InitialEquity))
				estimatedMarginReq := order.Lots * 1000.0 // ~$10 margin per 0.01 lot on standard 1:100 leverage
				if currFreeMargin > 0 && currFreeMargin < estimatedMarginReq*1.5 {
					log.Printf("[risk] order rejected on %s: insufficient free margin for safe scalping (free=$%.2f, req=$%.2f)",
						sig.Symbol, currFreeMargin, estimatedMarginReq)
					continue
				}

				// Passthrough & Adaptive StopLoss / TakeProfit scaling per Trading Mode
				mode := ModeBalanced
				if m := currentTradingMode.Load(); m != nil {
					mode = m.(TradingMode)
				}

				execPrice := lastTick.Ask
				if sig.Type == model.Sell {
					execPrice = lastTick.Bid
				}

				if sig.StopLoss > 0 && sig.TakeProfit > 0 {
					// Dynamic Volatility & Structure SL/TP Engine:
					// Preserves structure-based swing invalidation while enforcing
					// volatility floors ($3.50 min) and risk caps ($6.50 max) on Gold.
					structSLDist := math.Abs(execPrice - sig.StopLoss)
					minSL := 3.50
					maxSL := 6.50
					if !prof.IsGold {
						minSL = prof.MinSLDistance
						maxSL = prof.MinSLDistance * 3.0
					}

					// Dynamic mode scaling
					switch mode {
					case ModeSantai:
						minSL *= 1.2
						maxSL *= 1.2
					case ModeAgresif:
						maxSL = math.Min(maxSL, 5.00)
					}

					if structSLDist < minSL {
						structSLDist = minSL
					} else if structSLDist > maxSL {
						structSLDist = maxSL
					}

					if sig.Type == model.Buy {
						order.StopLoss = execPrice - structSLDist
					} else {
						order.StopLoss = execPrice + structSLDist
					}

					// Guarantee Reward-to-Risk ratio >= 2.2x
					structTPDist := math.Abs(sig.TakeProfit - execPrice)
					minTPDist := structSLDist * 2.2
					if prof.IsGold && minTPDist < 8.00 {
						minTPDist = 8.00
					}
					if structTPDist < minTPDist {
						structTPDist = minTPDist
					}
					if prof.IsGold && structTPDist > 25.00 {
						structTPDist = 25.00
					}

					if sig.Type == model.Buy {
						order.TakeProfit = execPrice + structTPDist
					} else {
						order.TakeProfit = execPrice - structTPDist
					}
				} else {
					if sig.StopLoss > 0 {
						order.StopLoss = sig.StopLoss
					}
					if sig.TakeProfit > 0 {
						order.TakeProfit = sig.TakeProfit
					}
				}

				// 4. Dynamic Volatility-Adjusted Lot Sizing (if enabled and user hasn't overridden)
				if cfg.Portfolio.VolatilityParitySizing && userCustomLot < 0.01 {
					order.Lots = volatilitySizer.CalculateLotSize(sig.Symbol, riskMgr.Equity(), atrVal)
				}

				// Submit to dispatcher
				if err := dispatcher.Submit(order); err != nil {
					log.Printf("[pipeline] dispatch failed: %v", err)
				} else {
					symbolCooldownMu.Lock()
					lastOrderTime[symKey] = time.Now()
					symbolCooldownMu.Unlock()
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	// -------------------------------------------------------------------------
	// 7. Status reporter ticker
	// -------------------------------------------------------------------------
	statusTicker := time.NewTicker(30 * time.Second)
	defer statusTicker.Stop()

	for {
		select {
		case <-statusTicker.C:
			pipelineMu.RLock()
			for sym, p := range pipelines {
				if t, ok := p.Buffer.Last(); ok {
					pipMult := model.PipMultiplier(sym)
					log.Printf("[status] %s price=%.5f spread=%.1f pips tracked_pos=%d",
						sym, t.MidPrice(), t.SpreadPips(pipMult), tracker.Count())
				}
			}
			pipelineMu.RUnlock()

		case <-ctx.Done():
			log.Println("[main] shutting down gracefully...")
			dispatcher.Stop()
			log.Printf("[main] session closed — tracked positions: %d", tracker.Count())
			stats := telemetry.Stats()
			log.Printf("[telemetry] fills=%d rejections=%d avg_slippage=%.2f pips avg_latency=%.2fms",
				stats.TotalFilled, stats.TotalRejected, stats.AverageSlippage, stats.AverageLatencyMs)
			return
		}
	}
}

func normalizeSymbol(sym string) string {
	return model.NormalizeSymbol(sym)
}

func buildNotifier(cfg config.NotifierConfig) notifier.Notifier {
	var notifiers []notifier.Notifier

	if cfg.EnableLog {
		notifiers = append(notifiers, notifier.NewLogNotifier())
	}
	if cfg.WebhookURL != "" {
		notifiers = append(notifiers, notifier.NewWebhookNotifier(cfg.WebhookURL))
	}
	if cfg.TelegramToken != "" && cfg.TelegramChat != "" {
		notifiers = append(notifiers, notifier.NewTelegramNotifier(cfg.TelegramToken, cfg.TelegramChat))
	}

	if len(notifiers) == 0 {
		return notifier.NewLogNotifier()
	}
	if len(notifiers) == 1 {
		return notifiers[0]
	}
	return notifier.NewMultiNotifier(notifiers...)
}

func buildSessionWindows(sessions []string) []risk.SessionWindow {
	sessionDefs := map[string]risk.SessionWindow{
		"london":  {Name: "London", StartHour: 7, EndHour: 16},
		"newyork": {Name: "New York", StartHour: 12, EndHour: 21},
		"overlap": {Name: "London/NY Overlap", StartHour: 12, EndHour: 16},
		"asian":   {Name: "Asian", StartHour: 23, EndHour: 8},
	}

	var windows []risk.SessionWindow
	for _, s := range sessions {
		if w, ok := sessionDefs[strings.ToLower(s)]; ok {
			windows = append(windows, w)
		}
	}
	return windows
}

func mockMarketFeed(ctx context.Context, symbol string, tickCh chan<- model.Tick) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	basePrice := 2500.00
	spread := 0.35 // $0.35 spread for Gold cent

	for {
		select {
		case <-ticker.C:
			delta := (rand.Float64() - 0.499) * 0.50
			basePrice += delta
			tick := model.Tick{
				Symbol:      symbol,
				Bid:         math.Round(basePrice*100) / 100,
				Ask:         math.Round((basePrice+spread)*100) / 100,
				TimestampNs: time.Now().UnixNano(),
			}
			select {
			case tickCh <- tick:
			default:
			}
		case <-ctx.Done():
			return
		}
	}
}
