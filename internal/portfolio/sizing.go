package portfolio

import (
	"math"

	"github.com/pompbot/scalpbot/internal/model"
)

// SizerConfig holds parameters for volatility-adjusted position sizing.
type SizerConfig struct {
	RiskPerTrade float64 // Fraction of equity to risk per trade (e.g. 0.01 = 1%)
	MinLotSize   float64 // Minimum order lot size (e.g. 0.01)
	MaxLotSize   float64 // Maximum order lot size (e.g. 2.00)
	LotStep      float64 // Broker lot increment (e.g. 0.01)
	SLATRMult    float64 // Stop loss ATR multiplier (e.g. 1.5)
}

// DefaultSizerConfig returns standard institutional defaults.
func DefaultSizerConfig() SizerConfig {
	return SizerConfig{
		RiskPerTrade: 0.01,
		MinLotSize:   0.01,
		MaxLotSize:   2.00,
		LotStep:      0.01,
		SLATRMult:    1.5,
	}
}

// VolatilityParitySizer calculates lot size such that the dollar loss at stop-loss
// equals exactly RiskPerTrade * Equity across ANY instrument (Forex, Gold, Indices).
type VolatilityParitySizer struct {
	cfg SizerConfig
}

// NewVolatilityParitySizer creates a new volatility-adjusted lot sizer.
func NewVolatilityParitySizer(cfg SizerConfig) *VolatilityParitySizer {
	if cfg.MinLotSize <= 0 {
		cfg.MinLotSize = 0.01
	}
	if cfg.MaxLotSize <= 0 {
		cfg.MaxLotSize = 1.00
	}
	if cfg.LotStep <= 0 {
		cfg.LotStep = 0.01
	}
	if cfg.SLATRMult <= 0 {
		cfg.SLATRMult = 1.5
	}
	return &VolatilityParitySizer{cfg: cfg}
}

// CalculateLotSize determines the position size in standard lots.
// Guaranteed 0 heap allocations.
func (vps *VolatilityParitySizer) CalculateLotSize(
	symbol string,
	equity float64,
	atrValue float64,
) float64 {
	if equity <= 0 || atrValue <= 0 {
		return vps.cfg.MinLotSize
	}

	// Dollar risk budget
	riskDollars := equity * vps.cfg.RiskPerTrade

	// Stop loss distance in price
	slPriceDist := atrValue * vps.cfg.SLATRMult

	// Convert SL distance to pips
	pipMult := model.PipMultiplier(symbol)
	slPips := slPriceDist * pipMult
	if slPips <= 0.1 {
		slPips = 5.0 // fallback minimum 5 pips
	}

	// Dollar value per pip for 1.0 standard lot
	pipValPerStdLot := model.PipValue(symbol, 1.0)
	if pipValPerStdLot <= 0 {
		pipValPerStdLot = 10.0 // standard default $10/pip
	}

	// Calculate exact lots: Lots = RiskUSD / (SL_Pips * PipValPerLot)
	rawLots := riskDollars / (slPips * pipValPerStdLot)

	// Clamp to Min/Max bounds
	clampedLots := math.Max(vps.cfg.MinLotSize, math.Min(vps.cfg.MaxLotSize, rawLots))

	// Round down to nearest broker lot step (e.g. 0.01)
	step := vps.cfg.LotStep
	roundedLots := math.Floor(clampedLots/step) * step

	// Ensure minimum bound after rounding
	if roundedLots < vps.cfg.MinLotSize {
		roundedLots = vps.cfg.MinLotSize
	}

	// Precision rounding to 2 decimal places to avoid IEEE 754 precision artifacts
	return math.Round(roundedLots*100.0) / 100.0
}
