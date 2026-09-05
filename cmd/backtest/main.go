package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/pompbot/scalpbot/config"
	"github.com/pompbot/scalpbot/internal/ai"
	"github.com/pompbot/scalpbot/internal/backtest"
	"github.com/pompbot/scalpbot/internal/executor"
	"github.com/pompbot/scalpbot/internal/model"
	"github.com/pompbot/scalpbot/internal/risk"
	"github.com/pompbot/scalpbot/internal/strategy"
)

func main() {
	dataFile := flag.String("data", "", "Path to historical tick CSV file")
	csvFile := flag.String("csv", "", "Path to historical tick CSV file (alias for -data)")
	synthetic := flag.Bool("synthetic", true, "Generate synthetic GBM tick data")
	tickCount := flag.Int("ticks", 100000, "Number of synthetic ticks to generate")
	equity := flag.Float64("equity", 10000.0, "Initial account equity ($)")
	riskPerTrade := flag.Float64("risk", 0.01, "Risk fraction per trade (e.g. 0.01 = 1%)")
	configFile := flag.String("config", "", "Path to config YAML (optional)")
	exportCSV := flag.String("export", "", "Export trade log to CSV file (optional)")
	symbol := flag.String("symbol", "EURUSD", "Trading symbol")
	flag.Parse()

	log.Println("=================================================================")
	log.Println("           ⚡ SCALPBOT HIGH-SPEED QUANTITATIVE BACKTESTER        ")
	log.Println("=================================================================")

	// 1. Prepare Engine Configuration
	engineCfg := backtest.DefaultEngineConfig()
	engineCfg.Symbol = *symbol
	engineCfg.InitialBalance = *equity
	engineCfg.RiskConfig.InitialEquity = *equity
	engineCfg.RiskConfig.RiskPerTrade = *riskPerTrade

	if *configFile != "" {
		if cfg, err := config.LoadConfig(*configFile); err == nil {
			engineCfg.InitialBalance = cfg.Risk.InitialEquity
			engineCfg.Symbol = cfg.Bot.Symbol
			engineCfg.CandlePeriod = cfg.MarketData.CandlePeriod
			engineCfg.StrategyConfig = strategy.MomentumScalperConfig{
				FastEMAPeriod: cfg.Strategy.FastEMAPeriod,
				SlowEMAPeriod: cfg.Strategy.SlowEMAPeriod,
				RSIPeriod:     cfg.Strategy.RSIPeriod,
				ATRPeriod:     cfg.Strategy.ATRPeriod,
				RSIOverbought: cfg.Strategy.RSIOverbought,
				RSIOversold:   cfg.Strategy.RSIOversold,
				ATRMinimum:    cfg.Strategy.ATRMinimum,
			}
			engineCfg.RiskConfig = risk.ManagerConfig{
				InitialEquity:    cfg.Risk.InitialEquity,
				RiskPerTrade:     cfg.Risk.RiskPerTrade,
				MaxDailyDrawdown: cfg.Risk.MaxDailyDrawdown,
				MaxSpreadPips:    cfg.Risk.MaxSpreadPips,
				MaxOpenPositions: cfg.Risk.MaxOpenPositions,
				MinLotSize:       cfg.Risk.MinLotSize,
				MaxLotSize:       cfg.Risk.MaxLotSize,
			}
			trailingMode := executor.TrailingFixed
			if cfg.Position.TrailingMode == "atr" {
				trailingMode = executor.TrailingATR
			}
			engineCfg.TrackerConfig = executor.TrackerConfig{
				TrailingMode:      trailingMode,
				TrailingStopPips:  cfg.Position.TrailingStopPips,
				TrailingATRMult:   cfg.Position.TrailingATRMult,
				BreakEvenPips:     cfg.Position.BreakEvenPips,
				BreakEvenBuffPips: cfg.Position.BreakEvenBuffPips,
				EnableTrailing:    cfg.Position.EnableTrailing,
				EnableBreakEven:   cfg.Position.EnableBreakEven,
				EnablePartialTP:   cfg.Position.EnablePartialTP,
				PartialTPRatio:    cfg.Position.PartialTPRatio,
				TP1Pips:           cfg.Position.TP1Pips,
				TP2Pips:           cfg.Position.TP2Pips,
				TimeStopDuration:  cfg.Position.TimeStopDuration,
			}
			engineCfg.AIConfig = ai.FilterConfig{
				EnableMLFilter:    cfg.AI.EnableMLFilter,
				MinConfidence:     cfg.AI.MinConfidence,
				FilterRangingChop: cfg.AI.FilterRangingChop,
			}
			log.Printf("[backtest] loaded strategy & AI config from %s", *configFile)
		} else {
			log.Printf("[backtest] warning: failed to load %s (%v), using defaults", *configFile, err)
		}
	}

	// 2. Load or Generate Tick Data
	var ticks []model.Tick
	startLoad := time.Now()

	csvTarget := *dataFile
	if csvTarget == "" && *csvFile != "" {
		csvTarget = *csvFile
	}

	if csvTarget != "" {
		log.Printf("[backtest] loading ticks from CSV: %s ...", csvTarget)
		var err error
		ticks, err = backtest.LoadTicksFromCSV(csvTarget, *symbol)
		if err != nil {
			log.Fatalf("[backtest] FATAL: failed to load tick data: %v", err)
		}
	} else if *synthetic {
		log.Printf("[backtest] generating %d synthetic GBM market ticks for %s ...", *tickCount, *symbol)
		synthCfg := backtest.DefaultSyntheticConfig()
		ticks = backtest.GenerateSyntheticTicks(*tickCount, *symbol, synthCfg)
	} else {
		log.Fatalf("[backtest] please specify either -csv <file.csv> or -synthetic")
	}

	loadDuration := time.Since(startLoad)
	log.Printf("[backtest] loaded %d ticks in %s", len(ticks), loadDuration)

	// 3. Execute Backtest Simulation
	engine := backtest.NewEngine(engineCfg)
	log.Println("[backtest] running deterministic simulation...")

	startSim := time.Now()
	report, trades, err := engine.Run(ticks)
	simDuration := time.Since(startSim)

	if err != nil {
		log.Fatalf("[backtest] simulation failed: %v", err)
	}

	ticksPerSec := float64(len(ticks)) / simDuration.Seconds()
	log.Printf("[backtest] simulation completed in %s (%.0f ticks/sec throughput)",
		simDuration, ticksPerSec)

	// 4. Print Institutional Report
	fmt.Println(backtest.FormatReport(*report))

	// 5. Optional Export to CSV
	if *exportCSV != "" && len(trades) > 0 {
		if err := exportTradesToCSV(*exportCSV, trades); err != nil {
			log.Printf("[backtest] failed to export trades to %s: %v", *exportCSV, err)
		} else {
			log.Printf("[backtest] trade log exported successfully to %s", *exportCSV)
		}
	}
}

func exportTradesToCSV(filePath string, trades []backtest.TradeRecord) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Header
	_ = writer.Write([]string{
		"TradeID", "Symbol", "Side", "Lots", "EntryPrice", "ExitPrice",
		"PnL_USD", "PnL_Pips", "Commission", "Reason", "IsPartial",
	})

	for _, t := range trades {
		_ = writer.Write([]string{
			t.TradeID,
			t.Symbol,
			t.Side.String(),
			fmt.Sprintf("%.2f", t.Lots),
			fmt.Sprintf("%.5f", t.EntryPrice),
			fmt.Sprintf("%.5f", t.ExitPrice),
			fmt.Sprintf("%.2f", t.PnL),
			fmt.Sprintf("%.1f", t.PnLPips),
			fmt.Sprintf("%.2f", t.Commission),
			t.Reason,
			strconv.FormatBool(t.IsPartial),
		})
	}

	return nil
}
