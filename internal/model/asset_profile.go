package model

import "strings"

// AssetClass represents the category of the financial instrument.
type AssetClass string

const (
	AssetClassForex  AssetClass = "FOREX"
	AssetClassGold   AssetClass = "GOLD"
	AssetClassCrypto AssetClass = "CRYPTO"
	AssetClassIndex  AssetClass = "INDEX"
)

// AssetProfile holds the calibrated trading parameters for a specific asset class.
type AssetProfile struct {
	Class            AssetClass
	DisplayName      string
	Icon             string
	PipMultiplier    float64
	MinTPDistance    float64 // Minimum TP in price units
	MaxTPDistance    float64 // Maximum TP in price units
	MinSLDistance    float64 // Minimum SL in price units
	MaxLots          float64 // Hard cap for lot size (especially on small capital)
	DefaultLots      float64
	StagnantMinutes  int     // Minutes before stagnant trade killer triggers
	IsGold           bool
	IsForex          bool
	IsCryptoOrIndex  bool
}

// DetectAssetClass returns the tailored AssetProfile for a symbol.
// Gold (XAUUSD) is the primary first-class commodity for the trading engine.
func DetectAssetClass(symbol string) AssetProfile {
	sym := strings.ToUpper(strings.TrimSpace(symbol))

	// 1. CRYPTO & INDICES (Explicit match)
	if strings.Contains(sym, "BTC") || strings.Contains(sym, "US30") || strings.Contains(sym, "NAS") {
		return AssetProfile{
			Class:           AssetClassCrypto,
			DisplayName:     "Crypto & Index",
			Icon:            "⚡",
			PipMultiplier:   1.0,
			MinTPDistance:   30.0,
			MaxTPDistance:   120.0,
			MinSLDistance:   15.0,
			MaxLots:         0.02,
			DefaultLots:     0.01,
			StagnantMinutes: 15,
			IsCryptoOrIndex: true,
		}
	}

	// 2. FOREX (Explicit match only for legacy/testing tools)
	if strings.Contains(sym, "EUR") || strings.Contains(sym, "GBP") || strings.Contains(sym, "AUD") || strings.Contains(sym, "NZD") || strings.Contains(sym, "CAD") || strings.Contains(sym, "CHF") || strings.HasSuffix(sym, "JPY") {
		return AssetProfile{
			Class:           AssetClassForex,
			DisplayName:     "Forex Major",
			Icon:            "💵",
			PipMultiplier:   10000.0,
			MinTPDistance:   0.0010,
			MaxTPDistance:   0.0035,
			MinSLDistance:   0.0005,
			MaxLots:         0.10,
			DefaultLots:     0.01,
			StagnantMinutes: 25,
			IsForex:         true,
		}
	}

	// 3. GOLD / XAU (Primary Engine Focus & Standard Fallback)
	maxLots := 0.10 // Standard ceiling for Micro/Regular accounts (XAUUSDm)
	if strings.HasSuffix(strings.ToLower(symbol), "c") {
		maxLots = 1.00 // Cent accounts (XAUUSDc) have 100x smaller contract size
	}

	return AssetProfile{
		Class:           AssetClassGold,
		DisplayName:     "Gold Commodity (XAUUSD)",
		Icon:            "🪙",
		PipMultiplier:   100.0, // 1 pip = $0.01 (100 pips = $1.00 move)
		MinTPDistance:   6.00,  // $6.00 TP move (M5 Gold scalping minimum 1:2.0 RRR)
		MaxTPDistance:   25.00, // $25.00 move (H1 macro swing trend target)
		MinSLDistance:   3.00,  // $3.00 SL move (300 pips — M5 volatility buffer)
		MaxLots:         maxLots,
		DefaultLots:     0.01,
		StagnantMinutes: 45,    // 45 min stagnant killer (M5 trade horizon)
		IsGold:          true,
	}
}

// AccountType represents Cent vs Regular/Standard account environment.
type AccountType string

const (
	AccountTypeCent    AccountType = "CENT"
	AccountTypeRegular AccountType = "REGULAR"
)

// DetectAccountType identifies if the account is a Cent account or Regular/Standard account.
// Evaluates broker account currency, symbol suffix, and balance magnitude.
func DetectAccountType(symbol string, currency string, balance float64) AccountType {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	curr := strings.ToUpper(strings.TrimSpace(currency))

	// 1. Explicit Cent Currency from Broker (USC, EUC, CENT, GBC)
	if curr == "USC" || curr == "EUC" || curr == "CENT" || curr == "GBC" {
		return AccountTypeCent
	}

	// 2. Broker Cent Suffix (e.g. XAUUSDc, GOLDc, XAUUSD.c)
	// Note: "m" is Micro/Standard (e.g. XAUUSDm on Exness Standard), "c" is Cent!
	if strings.HasSuffix(sym, "C") || strings.Contains(sym, "CENT") {
		return AccountTypeCent
	}

	// 3. Fallback: Standard / Regular Account (USD, EUR, XAUUSD, XAUUSDm, XAUUSD.pro)
	return AccountTypeRegular
}

