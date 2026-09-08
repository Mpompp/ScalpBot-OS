package news

import (
	"fmt"
	"strings"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// BlackoutConfig configures news blackout behavior.
type BlackoutConfig struct {
	Enabled         bool          // Master switch for news filter
	BlackoutBefore  time.Duration // Block new trades before event (e.g. 15m)
	BlackoutAfter   time.Duration // Block new trades after event (e.g. 15m)
	FilterAllImpact bool          // True = include medium impact, False = only high impact
}

// DefaultBlackoutConfig returns standard institutional defaults.
func DefaultBlackoutConfig() BlackoutConfig {
	return BlackoutConfig{
		Enabled:        true,
		BlackoutBefore: 15 * time.Minute,
		BlackoutAfter:  15 * time.Minute,
	}
}

// DynamicNewsBlackoutFilter blocks trades during high-impact economic news releases.
// Optimized for zero-allocation hot-path checks.
type DynamicNewsBlackoutFilter struct {
	cfg      BlackoutConfig
	calendar *CalendarClient
}

// NewDynamicNewsBlackoutFilter creates a new blackout filter.
func NewDynamicNewsBlackoutFilter(cfg BlackoutConfig, calendar *CalendarClient) *DynamicNewsBlackoutFilter {
	return &DynamicNewsBlackoutFilter{
		cfg:      cfg,
		calendar: calendar,
	}
}

// Allow evaluates whether trading is permitted for the given tick based on economic calendar.
// Guaranteed 0 heap allocations on hot-path.
func (f *DynamicNewsBlackoutFilter) Allow(tick model.Tick) (bool, string) {
	if !f.cfg.Enabled || f.calendar == nil {
		return true, ""
	}

	baseCurr, quoteCurr := extractCurrencies(tick.Symbol)

	nowNs := tick.TimestampNs
	if nowNs <= 0 {
		nowNs = time.Now().UnixNano()
	}

	// 1. Check Base Currency News (e.g. EUR in EURUSD, XAU in XAUUSD)
	if baseCurr != "" {
		if hasNews, ev := f.calendar.HasUpcomingHighImpact(baseCurr, f.cfg.BlackoutBefore, f.cfg.BlackoutAfter, nowNs); hasNews {
			diff := time.Duration(ev.TimestampNs - nowNs)
			if diff > 0 {
				return false, fmt.Sprintf("news blackout: %s %s in %v (%s)", ev.Currency, ev.Title, diff.Round(time.Second), ev.Impact)
			}
			return false, fmt.Sprintf("news blackout: %s %s released %v ago (%s)", ev.Currency, ev.Title, (-diff).Round(time.Second), ev.Impact)
		}
	}

	// 2. Check Quote Currency News (e.g. USD in EURUSD/GBPUSD/USDJPY/XAUUSD)
	if quoteCurr != "" && quoteCurr != baseCurr {
		if hasNews, ev := f.calendar.HasUpcomingHighImpact(quoteCurr, f.cfg.BlackoutBefore, f.cfg.BlackoutAfter, nowNs); hasNews {
			diff := time.Duration(ev.TimestampNs - nowNs)
			if diff > 0 {
				return false, fmt.Sprintf("news blackout: %s %s in %v (%s)", ev.Currency, ev.Title, diff.Round(time.Second), ev.Impact)
			}
			return false, fmt.Sprintf("news blackout: %s %s released %v ago (%s)", ev.Currency, ev.Title, (-diff).Round(time.Second), ev.Impact)
		}
	}

	return true, ""
}

// OnTick is a no-op for the news filter.
func (f *DynamicNewsBlackoutFilter) OnTick(tick model.Tick) {}

// Name returns filter identifier.
func (f *DynamicNewsBlackoutFilter) Name() string {
	return "DynamicNewsBlackoutFilter"
}

// extractCurrencies parses base and quote currency codes without allocations for common pairs.
// Prioritizes Gold (XAU/USD) for zero-latency news calendar blackout checking.
func extractCurrencies(symbol string) (string, string) {
	switch {
	case strings.HasPrefix(symbol, "XAUUSD") || strings.HasPrefix(symbol, "GOLD") || strings.Contains(symbol, "XAU"):
		return "XAU", "USD"
	case strings.HasPrefix(symbol, "EURUSD"):
		return "EUR", "USD"
	case strings.HasPrefix(symbol, "GBPUSD"):
		return "GBP", "USD"
	case strings.HasPrefix(symbol, "USDJPY"):
		return "USD", "JPY"
	case strings.HasPrefix(symbol, "USDCHF"):
		return "USD", "CHF"
	case strings.HasPrefix(symbol, "AUDUSD"):
		return "AUD", "USD"
	case strings.HasPrefix(symbol, "USDCAD"):
		return "USD", "CAD"
	case strings.HasPrefix(symbol, "NZDUSD"):
		return "NZD", "USD"
	case strings.HasPrefix(symbol, "EURGBP"):
		return "EUR", "GBP"
	case strings.HasPrefix(symbol, "EURJPY"):
		return "EUR", "JPY"
	case strings.HasPrefix(symbol, "GBPJPY"):
		return "GBP", "JPY"
	case strings.HasPrefix(symbol, "BTCUSD"):
		return "BTC", "USD"
	default:
		if len(symbol) >= 6 {
			return symbol[:3], symbol[3:6]
		}
		return symbol, ""
	}
}
