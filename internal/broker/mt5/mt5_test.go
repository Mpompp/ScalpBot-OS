package mt5

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// mockMT5Client simulates MT5_Bridge connecting to the Go ScalpBot server.
type mockMT5Client struct {
	cmdConn    net.Conn
	streamConn net.Conn
	ticketSeq  atomic.Int64
	closed     atomic.Bool
}

func startMockMT5Client(t testing.TB, cmdAddr, streamAddr string) *mockMT5Client {
	var d net.Dialer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmdConn, err := d.DialContext(ctx, "tcp", cmdAddr)
	if err != nil {
		t.Fatalf("mock MT5 client failed to connect to command server: %v", err)
	}

	streamConn, err := d.DialContext(ctx, "tcp", streamAddr)
	if err != nil {
		_ = cmdConn.Close()
		t.Fatalf("mock MT5 client failed to connect to stream server: %v", err)
	}

	c := &mockMT5Client{
		cmdConn:    cmdConn,
		streamConn: streamConn,
	}

	// Handle command requests from Go Server
	go c.handleCommandLoop()

	return c
}

func (c *mockMT5Client) handleCommandLoop() {
	defer func() {
		_ = c.cmdConn.Close()
	}()

	reader := bufio.NewReaderSize(c.cmdConn, 65536)
	writer := bufio.NewWriterSize(c.cmdConn, 65536)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 {
				var req TradeRequest
				if err := json.Unmarshal(trimmed, &req); err == nil {
					var resp TradeResponse
					resp.RequestID = req.RequestID
					resp.TimestampNs = time.Now().UnixNano()

					switch req.Action {
					case ActionPing:
						resp.Success = true

					case ActionBuy:
						seq := c.ticketSeq.Add(1) + 1000
						resp.Success = true
						resp.RetCode = 10009
						resp.Ticket = fmt.Sprintf("%d", seq)
						resp.FillPrice = 1.08500
						resp.Lots = req.Lots

					case ActionSell:
						seq := c.ticketSeq.Add(1) + 1000
						resp.Success = true
						resp.RetCode = 10009
						resp.Ticket = fmt.Sprintf("%d", seq)
						resp.FillPrice = 1.08490
						resp.Lots = req.Lots

					case ActionClose:
						resp.Success = true
						resp.RetCode = 10009
						resp.Ticket = req.Ticket

					case ActionPositions:
						resp.Success = true
						resp.Positions = []PositionDTO{
							{
								Ticket:     "1001",
								Symbol:     "EURUSD",
								Side:       "BUY",
								Lots:       0.5,
								OpenPrice:  1.08500,
								StopLoss:   1.08000,
								TakeProfit: 1.09000,
								Profit:     25.50,
								OpenTimeNs: time.Now().UnixNano(),
							},
						}

					case ActionAccount:
						resp.Success = true
						resp.Account = &AccountDTO{
							Balance:     10000.0,
							Equity:      10025.50,
							Margin:      200.0,
							FreeMargin:  9825.50,
							MarginLevel: 5012.75,
							Leverage:    100,
							Currency:    "USD",
						}

					default:
						resp.Success = false
						resp.ErrorMsg = "unknown action"
					}

					data, _ := json.Marshal(resp)
					data = append(data, '\n')
					_, _ = writer.Write(data)
					_ = writer.Flush()
				}
			}
		}
		if err != nil {
			break
		}
	}
}

func (c *mockMT5Client) pushTick(symbol string, bid, ask float64) {
	writer := bufio.NewWriter(c.streamConn)
	dto := TickDTO{
		Symbol:      symbol,
		Bid:         bid,
		Ask:         ask,
		TimestampNs: time.Now().UnixNano(),
	}
	data, _ := json.Marshal(dto)
	writer.Write(data)
	writer.WriteByte('\n')
	writer.Flush()
}

func (c *mockMT5Client) close() {
	if c.closed.CompareAndSwap(false, true) {
		if c.cmdConn != nil {
			_ = c.cmdConn.Close()
		}
		if c.streamConn != nil {
			_ = c.streamConn.Close()
		}
	}
}

// --- Tests ---

func setupTestEnvironment(t testing.TB) (*Adapter, *mockMT5Client, func()) {
	// Random available ports
	cmdL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get random port: %v", err)
	}
	cmdAddr := cmdL.Addr().String()
	_ = cmdL.Close()

	streamL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get random stream port: %v", err)
	}
	streamAddr := streamL.Addr().String()
	_ = streamL.Close()

	adapter := NewAdapter(AdapterConfig{
		CommandAddr: cmdAddr,
		StreamAddr:  streamAddr,
		Timeout:     1 * time.Second,
		MagicNumber: 123456,
		Slippage:    10,
	})

	ctx := context.Background()
	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("adapter failed to start server: %v", err)
	}

	client := startMockMT5Client(t, cmdAddr, streamAddr)
	time.Sleep(50 * time.Millisecond) // Wait for server to accept

	cleanup := func() {
		client.close()
		_ = adapter.CloseConnection()
	}

	return adapter, client, cleanup
}

func TestMT5Adapter_Execute_BuyAndSell(t *testing.T) {
	adapter, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	ctx := context.Background()

	// Test BUY
	buyOrder := model.OrderRequest{
		Symbol: "EURUSD",
		Side:   model.SideBuy,
		Lots:   0.10,
		Price:  1.08500,
	}

	buyPos, err := adapter.Execute(ctx, buyOrder)
	if err != nil {
		t.Fatalf("buy execute failed: %v", err)
	}
	if buyPos.OrderID == "" {
		t.Error("expected non-empty ticket ID")
	}
	if buyPos.Lots != 0.10 {
		t.Errorf("expected 0.10 lots, got %f", buyPos.Lots)
	}

	// Test SELL
	sellOrder := model.OrderRequest{
		Symbol: "EURUSD",
		Side:   model.SideSell,
		Lots:   0.20,
		Price:  1.08490,
	}

	sellPos, err := adapter.Execute(ctx, sellOrder)
	if err != nil {
		t.Fatalf("sell execute failed: %v", err)
	}
	if sellPos.OrderID == "" {
		t.Error("expected non-empty ticket ID")
	}
	if sellPos.Lots != 0.20 {
		t.Errorf("expected 0.20 lots, got %f", sellPos.Lots)
	}
}

func TestMT5Adapter_ClosePosition(t *testing.T) {
	adapter, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	ctx := context.Background()
	if err := adapter.Close(ctx, "1001"); err != nil {
		t.Errorf("failed to close position: %v", err)
	}
}

func TestMT5Adapter_ListOpenPositions(t *testing.T) {
	adapter, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	ctx := context.Background()
	positions, err := adapter.ListOpenPositions(ctx)
	if err != nil {
		t.Fatalf("failed to list positions: %v", err)
	}

	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	if positions[0].OrderID != "1001" {
		t.Errorf("expected ticket 1001, got %s", positions[0].OrderID)
	}
}

func TestMT5Adapter_GetAccountState(t *testing.T) {
	adapter, _, cleanup := setupTestEnvironment(t)
	defer cleanup()

	ctx := context.Background()
	acc, err := adapter.GetAccountState(ctx)
	if err != nil {
		t.Fatalf("failed to get account: %v", err)
	}

	if acc.Balance != 10000.0 {
		t.Errorf("expected 10000 balance, got %f", acc.Balance)
	}
}

func TestMT5Adapter_StreamTicks(t *testing.T) {
	adapter, client, cleanup := setupTestEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tickCh := make(chan model.Tick, 10)
	adapter.StreamTicks(ctx, tickCh)

	// Send ticks
	go func() {
		for i := 0; i < 5; i++ {
			client.pushTick("EURUSD", 1.08500+float64(i)*0.0001, 1.08510+float64(i)*0.0001)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	received := 0
	for received < 3 {
		select {
		case tick := <-tickCh:
			if tick.Symbol != "EURUSD" {
				t.Errorf("expected EURUSD, got %s", tick.Symbol)
			}
			received++
		case <-time.After(1 * time.Second):
			t.Fatalf("timeout waiting for stream ticks, received %d", received)
		}
	}
}

func BenchmarkMT5_CommandRoundTripLatency(b *testing.B) {
	adapter, _, cleanup := setupTestEnvironment(b)
	defer cleanup()

	ctx := context.Background()
	order := model.OrderRequest{
		Symbol: "EURUSD",
		Side:   model.SideBuy,
		Lots:   0.1,
		Price:  1.08500,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := adapter.Execute(ctx, order)
		if err != nil {
			b.Fatalf("execute failed: %v", err)
		}
	}
}
