package backtest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTickLineBytes(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected float64
	}{
		{
			name:     "MT5 Standard Format",
			line:     "2026.08.14 12:00:00.123,1.15704,1.15707,10",
			expected: 1.15704,
		},
		{
			name:     "TimestampNs Format",
			line:     "1723636800000000000,1.15704,1.15707",
			expected: 1.15704,
		},
		{
			name:     "Tab Delimited Format",
			line:     "2026.08.14 12:00:00\t1.15704\t1.15707",
			expected: 1.15704,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tick, err := parseTickLineBytes([]byte(tt.line), "EURUSD")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tick.Bid < tt.expected-0.00001 || tick.Bid > tt.expected+0.00001 {
				t.Errorf("expected bid %.5f, got %.5f", tt.expected, tick.Bid)
			}
		})
	}
}

func TestSyntheticGenerator(t *testing.T) {
	cfg := DefaultSyntheticConfig()
	count := 5000
	ticks := GenerateSyntheticTicks(count, "XAUUSD", cfg)

	if len(ticks) != count {
		t.Fatalf("expected %d ticks, got %d", count, len(ticks))
	}

	for i := 1; i < len(ticks); i++ {
		if ticks[i].TimestampNs <= ticks[i-1].TimestampNs {
			t.Errorf("timestamp not monotonically increasing at index %d", i)
		}
		if ticks[i].Bid >= ticks[i].Ask {
			t.Errorf("bid %.5f >= ask %.5f at index %d", ticks[i].Bid, ticks[i].Ask, i)
		}
	}
}

func TestCalculateMetrics(t *testing.T) {
	trades := []TradeRecord{
		{
			TradeID:    "T1",
			Lots:       0.1,
			EntryPrice: 1.1000,
			ExitPrice:  1.1010,
			PnL:        100.0, // Win $100
			Reason:     "TP_HIT",
		},
		{
			TradeID:    "T2",
			Lots:       0.1,
			EntryPrice: 1.1000,
			ExitPrice:  1.0995,
			PnL:        -50.0, // Loss $50
			Reason:     "SL_HIT",
		},
		{
			TradeID:    "T3",
			Lots:       0.1,
			EntryPrice: 1.1000,
			ExitPrice:  1.1015,
			PnL:        150.0, // Win $150
			Reason:     "TP2_HIT",
		},
	}

	equityCurve := []EquityPoint{
		{TimestampNs: 1, Equity: 10000.0},
		{TimestampNs: 2, Equity: 10100.0},
		{TimestampNs: 3, Equity: 10050.0},
		{TimestampNs: 4, Equity: 10200.0},
	}

	report := CalculateMetrics(10000.0, trades, equityCurve)

	if report.TotalTrades != 3 {
		t.Errorf("expected 3 trades, got %d", report.TotalTrades)
	}
	if report.WinningTrades != 2 || report.LosingTrades != 1 {
		t.Errorf("expected 2 wins / 1 loss, got %d / %d", report.WinningTrades, report.LosingTrades)
	}
	if report.NetProfit != 200.0 {
		t.Errorf("expected net profit $200, got %.2f", report.NetProfit)
	}
	if report.ProfitFactor != 5.0 { // GrossProfit 250 / GrossLoss 50 = 5.0
		t.Errorf("expected Profit Factor 5.0, got %.2f", report.ProfitFactor)
	}
	if report.MaxDrawdownUSD != 50.0 {
		t.Errorf("expected Max Drawdown $50, got %.2f", report.MaxDrawdownUSD)
	}
}

func TestBacktestEngine_Run(t *testing.T) {
	synthCfg := DefaultSyntheticConfig()
	ticks := GenerateSyntheticTicks(10000, "XAUUSD", synthCfg)

	engineCfg := DefaultEngineConfig()
	engine := NewEngine(engineCfg)

	report, trades, err := engine.Run(ticks)
	if err != nil {
		t.Fatalf("simulation failed: %v", err)
	}

	if report == nil {
		t.Fatalf("expected non-nil report")
	}

	t.Logf("Backtest Results: TotalTrades=%d, NetProfit=$%.2f, WinRate=%.1f%%, MDD=%.2f%%",
		report.TotalTrades, report.NetProfit, report.WinRate, report.MaxDrawdownPct)

	if len(trades) != report.TotalTrades {
		t.Errorf("trades slice length %d != report TotalTrades %d", len(trades), report.TotalTrades)
	}
}

func TestCSVLoader_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	csvPath := filepath.Join(tempDir, "test_ticks.csv")

	content := "Date,Bid,Ask,Vol\n2026.08.14 10:00:00.100,1.15000,1.15003,1\n2026.08.14 10:00:00.200,1.15005,1.15008,1\n"
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	ticks, err := LoadTicksFromCSV(csvPath, "EURUSD")
	if err != nil {
		t.Fatalf("failed to load ticks from CSV: %v", err)
	}

	if len(ticks) != 2 {
		t.Fatalf("expected 2 ticks, got %d", len(ticks))
	}
	if ticks[0].Bid != 1.15000 || ticks[1].Bid != 1.15005 {
		t.Errorf("unexpected tick values: %+v", ticks)
	}
}

func BenchmarkCSVParser_ParseLineBytes(b *testing.B) {
	line := []byte("2026.08.14 12:00:00.123,1.15704,1.15707,10")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = parseTickLineBytes(line, "EURUSD")
	}
}

func BenchmarkBacktestEngine_Throughput(b *testing.B) {
	synthCfg := DefaultSyntheticConfig()
	ticks := GenerateSyntheticTicks(50000, "EURUSD", synthCfg)

	engineCfg := DefaultEngineConfig()
	engine := NewEngine(engineCfg)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, _ = engine.Run(ticks)
	}
}
