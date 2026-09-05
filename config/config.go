// Package config provides YAML-based configuration loading with
// environment variable override support and validation.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration struct for the scalping bot.
type Config struct {
	Bot           BotConfig           `yaml:"bot"`
	MarketData    MarketDataConfig    `yaml:"market_data"`
	Strategy      StrategyConfig      `yaml:"strategy"`
	RangeStrategy RangeStrategyConfig `yaml:"range_strategy"`
	Risk          RiskConfig          `yaml:"risk"`
	Position      PositionConfig      `yaml:"position"`
	Session       SessionConfig       `yaml:"session"`
	Executor      ExecutorConfig      `yaml:"executor"`
	Broker        BrokerConfig        `yaml:"broker"`
	Notifier      NotifierConfig      `yaml:"notifier"`
	AI            AIConfig            `yaml:"ai"`
	Portfolio     PortfolioConfig     `yaml:"portfolio"`
	News          NewsConfig          `yaml:"news"`
	Web           WebConfig           `yaml:"web"`
}

// RangeStrategyConfig holds parameters for the mean-reversion range scalper.
type RangeStrategyConfig struct {
	Enabled         bool    `yaml:"enabled"`
	BollingerPeriod int     `yaml:"bollinger_period"`
	BollingerStdDev float64 `yaml:"bollinger_stddev"`
	RSIPeriod       int     `yaml:"rsi_period"`
	ATRPeriod       int     `yaml:"atr_period"`
	RSIOverbought   float64 `yaml:"rsi_overbought"`
	RSIOversold     float64 `yaml:"rsi_oversold"`
	MinTPPoints     float64 `yaml:"min_tp_points"`
}

// WebConfig holds parameters for the real-time web dashboard.
type WebConfig struct {
	Enabled bool   `yaml:"enabled"` // Master switch for web control dashboard
	Host    string `yaml:"host"`    // Host binding, e.g. "127.0.0.1"
	Port    int    `yaml:"port"`    // Web dashboard port, e.g. 8080
}

// PortfolioConfig holds cross-pair risk and correlation settings.
type PortfolioConfig struct {
	EnableCorrelationGuard bool `yaml:"enable_correlation_guard"`
	MaxCorrelatedPositions int  `yaml:"max_correlated_positions"`
	VolatilityParitySizing bool `yaml:"volatility_parity_sizing"`
}

// NewsConfig holds live economic calendar and blackout parameters.
type NewsConfig struct {
	EnableAutoSync bool          `yaml:"enable_auto_sync"`
	FeedURL        string        `yaml:"feed_url"`
	SyncInterval   time.Duration `yaml:"sync_interval"`
	BlackoutBefore time.Duration `yaml:"blackout_before"`
	BlackoutAfter  time.Duration `yaml:"blackout_after"`
}

// AIConfig holds machine learning and market regime filter settings.
type AIConfig struct {
	EnableMLFilter     bool    `yaml:"enable_ml_filter"`      // Master switch for AI probability filter
	DualModeEnabled    bool    `yaml:"dual_mode_enabled"`     // Master switch for Dual-Mode AI (Trend + Range)
	MinConfidence      float64 `yaml:"min_confidence"`        // Minimum confidence score for Trend (e.g. 0.55 = 55%)
	RangeMinConfidence float64 `yaml:"range_min_confidence"`  // Minimum confidence score for Range (e.g. 0.50 = 50%)
	FilterRangingChop  bool    `yaml:"filter_ranging_chop"`   // Block breakout trades during ranging chop
}

// BotConfig holds general bot parameters.
type BotConfig struct {
	Symbol   string   `yaml:"symbol"`    // Target primary currency pair (fallback)
	Symbols  []string `yaml:"symbols"`   // Multi-symbol portfolio list (e.g. ["EURUSD", "GBPUSD", "USDJPY", "XAUUSD"])
	LogLevel string   `yaml:"log_level"` // Log verbosity: debug, info, warn, error
}

// MarketDataConfig controls market data buffering and aggregation.
type MarketDataConfig struct {
	TickBufferSize int           `yaml:"tick_buffer_size"` // Ring buffer capacity
	CandlePeriod   time.Duration `yaml:"candle_period"`    // OHLCV aggregation period
	TickChannelBuf int           `yaml:"tick_channel_buf"` // Tick channel buffer size
}

// StrategyConfig holds parameters for the momentum scalper.
type StrategyConfig struct {
	Timeframe        string  `yaml:"timeframe"`
	MacroTimeframe   string  `yaml:"macro_timeframe"`
	FastEMAPeriod    int     `yaml:"fast_ema_period"`
	SlowEMAPeriod    int     `yaml:"slow_ema_period"`
	RSIPeriod        int     `yaml:"rsi_period"`
	ATRPeriod        int     `yaml:"atr_period"`
	RSIOverbought    float64 `yaml:"rsi_overbought"`
	RSIOversold      float64 `yaml:"rsi_oversold"`
	ATRMinimum          float64 `yaml:"atr_minimum"`
	SLATRMultiplier     float64 `yaml:"sl_atr_multiplier"`
	TPATRMultiplier     float64 `yaml:"tp_atr_multiplier"`
	MinVolRatio         float64 `yaml:"min_vol_ratio"`
	EnableLimitPullback bool    `yaml:"enable_limit_pullback"`
	PullbackDiscountATR float64 `yaml:"pullback_discount_atr"`
}

// RiskConfig holds risk management parameters.
type RiskConfig struct {
	InitialEquity        float64 `yaml:"initial_equity"`
	RiskPerTrade         float64 `yaml:"risk_per_trade"`           // Fraction (0.01 = 1%)
	MaxDailyDrawdown     float64 `yaml:"max_daily_drawdown"`       // Fraction (0.05 = 5%)
	DailyProfitTargetPct float64 `yaml:"daily_profit_target_pct"` // Fraction (e.g. 0.05 = 5% daily profit lock)
	DailyProfitTargetUSD float64 `yaml:"daily_profit_target_usd"` // Nominal (e.g. 50.0 USC target lock)
	MaxSpreadPips        float64 `yaml:"max_spread_pips"`
	MaxOpenPositions     int     `yaml:"max_open_positions"`
	MinLotSize           float64 `yaml:"min_lot_size"`
	MaxLotSize           float64 `yaml:"max_lot_size"`
	MinTickVelocityTPS   float64 `yaml:"min_tick_velocity_tps"` // Minimum ticks/sec (liquidity gate)
	MaxTickVelocityTPS   float64 `yaml:"max_tick_velocity_tps"` // Maximum ticks/sec (anti-flash surge)
	IsCentAccount        bool    `yaml:"is_cent_account"`       // Cent account mode
}

// ExecutorConfig controls the order dispatcher.
type ExecutorConfig struct {
	Workers   int `yaml:"workers"`    // Number of worker goroutines
	QueueSize int `yaml:"queue_size"` // Order channel buffer size
}

// BrokerConfig holds broker connection parameters.
type BrokerConfig struct {
	Type           string        `yaml:"type"`             // "mock" or "mt5"
	FillLatency    time.Duration `yaml:"fill_latency"`     // Mock: simulated fill delay
	SlippagePct    float64       `yaml:"slippage_pct"`     // Mock: slippage fraction
	RejectRate     float64       `yaml:"reject_rate"`      // Mock: rejection probability
	MT5CommandAddr string        `yaml:"mt5_command_addr"` // MT5: Command TCP socket, e.g. "127.0.0.1:5555"
	MT5StreamAddr  string        `yaml:"mt5_stream_addr"`  // MT5: Stream TCP socket, e.g. "127.0.0.1:5556"
	MagicNumber    int           `yaml:"magic_number"`     // MT5: EA Magic Number
	SlippagePoints int           `yaml:"slippage_points"`  // MT5: Allowed slippage in points
}

// PositionConfig holds position tracking and trade management parameters.
type PositionConfig struct {
	EnableTrailing    bool          `yaml:"enable_trailing"`     // Enable trailing stop
	TrailingMode      string        `yaml:"trailing_mode"`       // "fixed" or "atr"
	TrailingStopPips  float64       `yaml:"trailing_stop_pips"`  // Distance for fixed trailing
	TrailingATRMult   float64       `yaml:"trailing_atr_mult"`   // ATR multiplier for adaptive trailing
	EnableBreakEven   bool          `yaml:"enable_breakeven"`    // Enable break-even
	BreakEvenPips     float64       `yaml:"breakeven_pips"`      // Profit threshold to trigger
	BreakEvenBuffPips float64       `yaml:"breakeven_buff_pips"` // Buffer above entry for BE SL
	EnablePartialTP   bool          `yaml:"enable_partial_tp"`   // Enable partial take profit (TP1/TP2)
	PartialTPRatio    float64       `yaml:"partial_tp_ratio"`    // Lot fraction to close at TP1 (e.g. 0.5)
	TP1Pips           float64       `yaml:"tp1_pips"`            // TP1 profit target in pips
	TP2Pips           float64       `yaml:"tp2_pips"`            // TP2 final profit target in pips
	TimeStopDuration  time.Duration `yaml:"time_stop_duration"`  // Max position duration (e.g. 15m)
}

// SessionConfig holds trading session filter parameters.
type SessionConfig struct {
	Enabled  bool     `yaml:"enabled"`  // Enable session filter
	Sessions []string `yaml:"sessions"` // Active sessions: "london", "newyork", "overlap", "asian"
}

// NotifierConfig holds alerting configuration.
type NotifierConfig struct {
	EnableLog            bool   `yaml:"enable_log"`              // Enable log-based alerts
	WebhookURL           string `yaml:"webhook_url"`             // Webhook endpoint (empty = disabled)
	TelegramToken        string `yaml:"telegram_token"`          // Bot token (prefer env: SCALP_TG_TOKEN)
	TelegramChat         string `yaml:"telegram_chat"`           // Chat ID (prefer env: SCALP_TG_CHAT)
	EnableInteractiveBot bool   `yaml:"enable_interactive_bot"` // Enable 2-way Telegram polling & remote control
}

// LoadConfig reads configuration from a YAML file.
// Environment variables can override specific fields using the SCALP_ prefix.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: failed to read %s: %w", path, err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: failed to parse %s: %w", path, err)
	}

	// Apply environment variable overrides
	cfg.applyEnvOverrides()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: validation failed: %w", err)
	}

	return cfg, nil
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Bot: BotConfig{
			Symbol:   "EURUSD",
			LogLevel: "info",
		},
		MarketData: MarketDataConfig{
			TickBufferSize: 4096,
			CandlePeriod:   1 * time.Minute,
			TickChannelBuf: 1024,
		},
		Strategy: StrategyConfig{
			Timeframe:           "15m",
			MacroTimeframe:      "1h",
			FastEMAPeriod:       5,
			SlowEMAPeriod:       13,
			RSIPeriod:           14,
			ATRPeriod:           14,
			RSIOverbought:       70.0,
			RSIOversold:         30.0,
			ATRMinimum:          0.50,
			SLATRMultiplier:     1.5,
			TPATRMultiplier:     3.0,
			MinVolRatio:         0.60,
			EnableLimitPullback: true,
			PullbackDiscountATR: 0.30,
		},
		RangeStrategy: RangeStrategyConfig{
			Enabled:         true,
			BollingerPeriod: 20,
			BollingerStdDev: 2.0,
			RSIPeriod:       14,
			ATRPeriod:       14,
			RSIOverbought:   62.0,
			RSIOversold:     38.0,
			MinTPPoints:     0.0003,
		},
		Risk: RiskConfig{
			InitialEquity:        900.0,
			RiskPerTrade:         0.005,
			MaxDailyDrawdown:     0.05,
			DailyProfitTargetUSD: 50.0,
			DailyProfitTargetPct: 0.05,
			MaxSpreadPips:        2.0,
			MaxOpenPositions:     2,
			MinLotSize:           0.01,
			MaxLotSize:           0.01,
			MinTickVelocityTPS:   0.0,
			MaxTickVelocityTPS:   0.0,
			IsCentAccount:        true,
		},
		Executor: ExecutorConfig{
			Workers:   2,
			QueueSize: 32,
		},
		Broker: BrokerConfig{
			Type:           "mock",
			FillLatency:    5 * time.Millisecond,
			SlippagePct:    0.00005,
			RejectRate:     0.02,
			MT5CommandAddr: "127.0.0.1:5555",
			MT5StreamAddr:  "127.0.0.1:5556",
			MagicNumber:    123456,
			SlippagePoints: 10,
		},
		Position: PositionConfig{
			EnableTrailing:    true,
			TrailingMode:      "fixed",
			TrailingStopPips:  12.0,
			TrailingATRMult:   1.5,
			EnableBreakEven:   true,
			BreakEvenPips:     8.0,
			BreakEvenBuffPips: 1.0,
			EnablePartialTP:   false,
			PartialTPRatio:    0.5,
			TP1Pips:           5.0,
			TP2Pips:           15.0,
			TimeStopDuration:  35 * time.Minute, // 35 minutes Stagnant Trade Killer
		},
		Session: SessionConfig{
			Enabled:  false,
			Sessions: []string{"london", "newyork"},
		},
		Notifier: NotifierConfig{
			EnableLog: true,
		},
		AI: AIConfig{
			EnableMLFilter:     true,
			DualModeEnabled:    true,
			MinConfidence:      0.70, // 70% minimum state probability
			RangeMinConfidence: 0.60,
			FilterRangingChop:  true,
		},
		Portfolio: PortfolioConfig{
			EnableCorrelationGuard: true,
			MaxCorrelatedPositions: 1,
			VolatilityParitySizing: true,
		},
		News: NewsConfig{
			EnableAutoSync: false,
			FeedURL:        "",
			SyncInterval:   1 * time.Hour,
			BlackoutBefore: 15 * time.Minute,
			BlackoutAfter:  15 * time.Minute,
		},
		Web: WebConfig{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    8080,
		},
	}
}

// Validate checks configuration for logical errors.
func (c *Config) Validate() error {
	if c.Bot.Symbol == "" {
		return fmt.Errorf("bot.symbol is required")
	}
	if c.MarketData.TickBufferSize <= 0 {
		return fmt.Errorf("market_data.tick_buffer_size must be > 0")
	}
	if c.MarketData.CandlePeriod <= 0 {
		return fmt.Errorf("market_data.candle_period must be > 0")
	}
	if c.Strategy.FastEMAPeriod >= c.Strategy.SlowEMAPeriod {
		return fmt.Errorf("strategy.fast_ema_period must be < slow_ema_period")
	}
	if c.Risk.RiskPerTrade <= 0 || c.Risk.RiskPerTrade > 0.1 {
		return fmt.Errorf("risk.risk_per_trade must be in (0, 0.10]")
	}
	if c.Risk.MaxDailyDrawdown <= 0 || c.Risk.MaxDailyDrawdown > 1.0 {
		return fmt.Errorf("risk.max_daily_drawdown must be in (0, 1.0]")
	}
	if c.Risk.InitialEquity <= 0 {
		return fmt.Errorf("risk.initial_equity must be > 0")
	}
	if c.Executor.Workers <= 0 {
		return fmt.Errorf("executor.workers must be > 0")
	}
	return nil
}

// applyEnvOverrides reads environment variables with SCALP_ prefix
// and overrides corresponding config fields.
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("SCALP_SYMBOL"); v != "" {
		c.Bot.Symbol = v
	}
	if v := os.Getenv("SCALP_LOG_LEVEL"); v != "" {
		c.Bot.LogLevel = v
	}
	if v := os.Getenv("SCALP_BROKER_TYPE"); v != "" {
		c.Broker.Type = v
	}
	if v := os.Getenv("SCALP_MT5_CMD"); v != "" {
		c.Broker.MT5CommandAddr = v
	}
	if v := os.Getenv("SCALP_MT5_STREAM"); v != "" {
		c.Broker.MT5StreamAddr = v
	}
	if v := os.Getenv("SCALP_TG_TOKEN"); v != "" {
		c.Notifier.TelegramToken = v
	}
	if v := os.Getenv("SCALP_TG_CHAT"); v != "" {
		c.Notifier.TelegramChat = v
	}
	if v := os.Getenv("SCALP_WEBHOOK_URL"); v != "" {
		c.Notifier.WebhookURL = v
	}
}
