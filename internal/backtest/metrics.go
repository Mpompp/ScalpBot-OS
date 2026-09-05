package backtest

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// TradeRecord stores the complete history of an executed and closed trade.
type TradeRecord struct {
	TradeID     string
	Symbol      string
	Side        model.OrderSide
	Lots        float64
	EntryPrice  float64
	ExitPrice   float64
	EntryTimeNs int64
	ExitTimeNs  int64
	PnL         float64 // Net PnL in currency after commission
	PnLPips     float64 // PnL in pips
	Commission  float64 // Total commission charged
	Reason      string  // "SL_HIT", "TP2_HIT", "PARTIAL_TP1_HIT", "TIME_STOP_HIT", "TRAILING_SL_HIT"
	IsPartial   bool
}

// EquityPoint represents a timestamped point in the account equity curve.
type EquityPoint struct {
	TimestampNs int64   `json:"timestamp_ns"`
	Equity      float64 `json:"equity"`
	DrawdownPct float64 `json:"drawdown_pct"`
}

// PerformanceReport holds all institutional quantitative analytics.
type PerformanceReport struct {
	InitialBalance   float64
	FinalBalance     float64
	NetProfit        float64
	ReturnPct        float64
	TotalTrades      int
	WinningTrades    int
	LosingTrades     int
	WinRate          float64
	LossRate         float64
	GrossProfit      float64
	GrossLoss        float64
	ProfitFactor     float64
	AverageWin       float64
	AverageLoss      float64
	PayoffRatio      float64
	Expectancy       float64
	MaxDrawdownUSD   float64
	MaxDrawdownPct   float64
	SharpeRatio      float64
	SortinoRatio     float64
	CalmarRatio      float64
	AvgTradeDuration time.Duration
	CloseReasons     map[string]int
	EquityCurve      []EquityPoint
}

// CalculateMetrics aggregates trade records into a comprehensive performance report.
func CalculateMetrics(initialBalance float64, trades []TradeRecord, equityCurve []EquityPoint) PerformanceReport {
	report := PerformanceReport{
		InitialBalance: initialBalance,
		FinalBalance:   initialBalance,
		CloseReasons:   make(map[string]int),
		EquityCurve:    equityCurve,
	}

	if len(trades) == 0 {
		return report
	}

	var totalWinUSD, totalLossUSD float64
	var returns []float64
	var totalDurationNs int64

	for _, tr := range trades {
		report.NetProfit += tr.PnL
		report.CloseReasons[tr.Reason]++

		if tr.ExitTimeNs > tr.EntryTimeNs {
			totalDurationNs += (tr.ExitTimeNs - tr.EntryTimeNs)
		}

		if tr.PnL > 0 {
			report.WinningTrades++
			report.GrossProfit += tr.PnL
			totalWinUSD += tr.PnL
		} else if tr.PnL < 0 {
			report.LosingTrades++
			report.GrossLoss += math.Abs(tr.PnL)
			totalLossUSD += math.Abs(tr.PnL)
		}

		// Fractional return on trade
		if initialBalance > 0 {
			returns = append(returns, tr.PnL/initialBalance)
		}
	}

	report.TotalTrades = len(trades)
	report.FinalBalance = initialBalance + report.NetProfit
	if initialBalance > 0 {
		report.ReturnPct = (report.NetProfit / initialBalance) * 100.0
	}

	// Win / Loss Rates
	if report.TotalTrades > 0 {
		report.WinRate = (float64(report.WinningTrades) / float64(report.TotalTrades)) * 100.0
		report.LossRate = (float64(report.LosingTrades) / float64(report.TotalTrades)) * 100.0
		report.AvgTradeDuration = time.Duration(totalDurationNs / int64(report.TotalTrades))
	}

	// Profit Factor
	if report.GrossLoss > 0 {
		report.ProfitFactor = report.GrossProfit / report.GrossLoss
	} else if report.GrossProfit > 0 {
		report.ProfitFactor = 999.99 // Undefined / infinite profit factor
	}

	// Average Win / Loss & Payoff Ratio
	if report.WinningTrades > 0 {
		report.AverageWin = totalWinUSD / float64(report.WinningTrades)
	}
	if report.LosingTrades > 0 {
		report.AverageLoss = totalLossUSD / float64(report.LosingTrades)
	}
	if report.AverageLoss > 0 {
		report.PayoffRatio = report.AverageWin / report.AverageLoss
	}

	// Mathematical Expectancy per trade
	pWin := report.WinRate / 100.0
	pLoss := report.LossRate / 100.0
	report.Expectancy = (pWin * report.AverageWin) - (pLoss * report.AverageLoss)

	// --- Maximum Drawdown Calculation ---
	peakEquity := initialBalance
	maxDDUSD := 0.0
	maxDDPct := 0.0

	for _, pt := range equityCurve {
		if pt.Equity > peakEquity {
			peakEquity = pt.Equity
		}
		ddUSD := peakEquity - pt.Equity
		if ddUSD > maxDDUSD {
			maxDDUSD = ddUSD
		}
		if peakEquity > 0 {
			ddPct := (ddUSD / peakEquity) * 100.0
			if ddPct > maxDDPct {
				maxDDPct = ddPct
			}
		}
	}
	report.MaxDrawdownUSD = maxDDUSD
	report.MaxDrawdownPct = maxDDPct

	// --- Sharpe & Sortino Ratio Calculation ---
	if len(returns) > 1 {
		// Mean trade return
		var sumRet float64
		for _, r := range returns {
			sumRet += r
		}
		meanRet := sumRet / float64(len(returns))

		// Standard Deviation
		var varSum, downVarSum float64
		for _, r := range returns {
			diff := r - meanRet
			varSum += diff * diff
			if r < 0 {
				downVarSum += r * r
			}
		}

		stdDev := math.Sqrt(varSum / float64(len(returns)-1))
		downDev := math.Sqrt(downVarSum / float64(len(returns)))

		// Annualized multiplier (assuming ~250 trading days, 50 trades/day for scalping)
		annualFactor := math.Sqrt(250.0 * 20.0)

		if stdDev > 0 {
			report.SharpeRatio = (meanRet / stdDev) * annualFactor
		}
		if downDev > 0 {
			report.SortinoRatio = (meanRet / downDev) * annualFactor
		}
	}

	// Calmar Ratio (Annualized Return / Max Drawdown %)
	if report.MaxDrawdownPct > 0 {
		report.CalmarRatio = report.ReturnPct / report.MaxDrawdownPct
	}

	return report
}

// FormatReport generates an institutional-grade ASCII performance table.
func FormatReport(r PerformanceReport) string {
	var sb strings.Builder

	sep := strings.Repeat("═", 64)
	subSep := strings.Repeat("─", 64)

	sb.WriteString("\n" + sep + "\n")
	sb.WriteString("        📊 SCALPBOT QUANTITATIVE PERFORMANCE REPORT\n")
	sb.WriteString(sep + "\n")

	sb.WriteString(fmt.Sprintf(" Initial Balance   : $%-12.2f  Final Balance  : $%.2f\n", r.InitialBalance, r.FinalBalance))
	sb.WriteString(fmt.Sprintf(" Net Profit        : $%-12.2f  Total Return   : %.2f%%\n", r.NetProfit, r.ReturnPct))
	sb.WriteString(subSep + "\n")

	sb.WriteString(fmt.Sprintf(" Total Trades      : %-13d  Win Rate       : %.1f%% (%d wins)\n", r.TotalTrades, r.WinRate, r.WinningTrades))
	sb.WriteString(fmt.Sprintf(" Losing Trades     : %-13d  Loss Rate      : %.1f%% (%d losses)\n", r.LosingTrades, r.LossRate, r.LosingTrades))
	sb.WriteString(fmt.Sprintf(" Gross Profit      : $%-12.2f  Gross Loss     : $%.2f\n", r.GrossProfit, r.GrossLoss))
	sb.WriteString(fmt.Sprintf(" Profit Factor     : %-13.2f  Payoff Ratio   : %.2f\n", r.ProfitFactor, r.PayoffRatio))
	sb.WriteString(fmt.Sprintf(" Average Win       : $%-12.2f  Average Loss   : $%.2f\n", r.AverageWin, r.AverageLoss))
	sb.WriteString(fmt.Sprintf(" Trade Expectancy  : $%-12.2f  Avg Duration   : %s\n", r.Expectancy, r.AvgTradeDuration.Round(time.Millisecond)))
	sb.WriteString(subSep + "\n")

	sb.WriteString(fmt.Sprintf(" Max Drawdown ($)  : $%-12.2f  Max Drawdown (%%): %.2f%%\n", r.MaxDrawdownUSD, r.MaxDrawdownPct))
	sb.WriteString(fmt.Sprintf(" Sharpe Ratio      : %-13.2f  Sortino Ratio  : %.2f\n", r.SharpeRatio, r.SortinoRatio))
	sb.WriteString(fmt.Sprintf(" Calmar Ratio      : %-13.2f\n", r.CalmarRatio))
	sb.WriteString(subSep + "\n")

	sb.WriteString(" Exit Reason Distribution:\n")
	for reason, count := range r.CloseReasons {
		pct := 0.0
		if r.TotalTrades > 0 {
			pct = (float64(count) / float64(r.TotalTrades)) * 100.0
		}
		sb.WriteString(fmt.Sprintf("   • %-18s: %4d trades (%.1f%%)\n", reason, count, pct))
	}
	sb.WriteString(sep + "\n")

	return sb.String()
}
