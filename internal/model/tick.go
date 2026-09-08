// Package model defines the core domain entities for the scalping bot.
// All structs use value types and primitive fields to minimize heap allocations
// on the hot execution path.
package model

import "strings"

var knownSymbolSuffixes = [...]string{".PRO", ".M", ".R", "M"}

// NormalizeSymbol strips broker-specific suffixes (e.g. "c", ".m", ".pro", ".r")
// and returns an uppercase canonical symbol name for consistent comparison.
// Use this everywhere symbols are compared, keyed, or counted.
func NormalizeSymbol(sym string) string {
	s := strings.ToUpper(strings.TrimSpace(sym))
	for i := range knownSymbolSuffixes {
		s = strings.TrimSuffix(s, knownSymbolSuffixes[i])
	}
	// Guard: don't strip trailing C from symbols where C is part of the name
	// e.g. "USDCHFC" -> "USDCHF", but "EURUSC" shouldn't become "EURUS"
	// The TrimSuffix("C") is safe for broker cent-account suffixes (XAUUSDc, GOLDmicro)
	if strings.HasSuffix(s, "C") && len(s) > 4 {
		s = strings.TrimSuffix(s, "C")
	}
	return s
}

// Tick represents a single raw market price update.
// TimestampNs stores time as nanoseconds since Unix epoch for zero-allocation
// comparison and arithmetic (avoids time.Time heap escape).
type Tick struct {
	Symbol      string  // Target symbol, e.g. "XAUUSDc", "XAUUSD"
	Bid         float64 // Best bid price
	Ask         float64 // Best ask price
	TimestampNs int64   // Unix nanosecond timestamp
}

// Spread returns the raw spread (ask - bid) in price units.
func (t Tick) Spread() float64 {
	return t.Ask - t.Bid
}

// MidPrice returns the midpoint between bid and ask.
func (t Tick) MidPrice() float64 {
	return (t.Bid + t.Ask) / 2.0
}

// SpreadPips returns the spread expressed in pips for the given pip multiplier.
// Use PipMultiplier() to obtain the correct multiplier for a symbol.
func (t Tick) SpreadPips(pipMultiplier float64) float64 {
	return t.Spread() * pipMultiplier
}

// PipMultiplier returns the pip conversion multiplier for a symbol.
// Primary Focus: Gold (XAUUSD): 1 pip = $0.01 (100 pips = $1.00 move in gold price)
func PipMultiplier(symbol string) float64 {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if strings.Contains(sym, "XAU") || strings.Contains(sym, "GOLD") {
		return 100.0 // 1 pip = $0.01
	}
	if strings.Contains(sym, "BTC") || strings.Contains(sym, "US30") || strings.Contains(sym, "NAS") {
		return 1.0
	}
	if strings.HasSuffix(sym, "JPY") {
		return 100.0
	}
	if strings.Contains(sym, "EUR") || strings.Contains(sym, "GBP") || strings.Contains(sym, "AUD") || strings.Contains(sym, "NZD") || strings.Contains(sym, "CAD") || strings.Contains(sym, "CHF") {
		return 10000.0
	}
	return 100.0 // Gold-centric default fallback (1 pip = $0.01)
}

// PipValue returns the dollar value of a 1-pip price move for the given lot size.
// For Gold (XAUUSD): 1 standard lot = 100 oz. 1 pip ($0.01) = $1.00.
// Value = lots * 100 oz * 0.01 price change = lots * 1.0 USD per pip.
func PipValue(symbol string, lots float64) float64 {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if strings.Contains(sym, "EUR") || strings.Contains(sym, "GBP") || strings.Contains(sym, "AUD") || strings.Contains(sym, "NZD") || strings.Contains(sym, "CAD") || strings.Contains(sym, "CHF") {
		return lots * 10.0 // Forex pairs
	}
	// Gold-centric primary & default fallback: 1 lot = $1.00 per pip
	return lots * 1.0
}

