package mt5

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

func TestMT5Adapter_BrokerErrorCodes(t *testing.T) {
	cmdAddr := "127.0.0.1:59101"
	streamAddr := "127.0.0.1:59102"

	cfg := AdapterConfig{
		CommandAddr: cmdAddr,
		StreamAddr:  streamAddr,
		Timeout:     1 * time.Second,
		MagicNumber: 123456,
		Slippage:    10,
	}

	adapter := NewAdapter(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("failed to start adapter: %v", err)
	}
	defer adapter.CloseConnection()

	// Connect custom mock client that returns specific error codes
	var d net.Dialer
	cmdConn, err := d.DialContext(ctx, "tcp", cmdAddr)
	if err != nil {
		t.Fatalf("failed to dial cmd server: %v", err)
	}
	defer cmdConn.Close()

	// Send an execute command that triggers Error 10019 (No Money)
	go func() {
		buf := make([]byte, 4096)
		n, _ := cmdConn.Read(buf)
		if n > 0 {
			// Respond with RetCode 10019 (No Money)
			resp := `{"request_id":"test-1","success":false,"retcode":10019,"error_msg":"No money on account"}` + "\n"
			_, _ = cmdConn.Write([]byte(resp))
		}
	}()

	time.Sleep(50 * time.Millisecond)

	_, execErr := adapter.Execute(context.Background(), model.OrderRequest{
		Symbol: "EURUSD",
		Side:   model.SideBuy,
		Lots:   100.0, // High lot
		Price:  1.08500,
	})

	if execErr == nil {
		t.Fatalf("Expected error for No Money rejection, got nil")
	}
	if !strings.Contains(execErr.Error(), "10019") {
		t.Errorf("Expected retcode 10019 in error, got: %v", execErr)
	}
}

func TestMT5Adapter_TimeoutAndSocketDrop(t *testing.T) {
	cmdAddr := "127.0.0.1:59103"
	streamAddr := "127.0.0.1:59104"

	cfg := AdapterConfig{
		CommandAddr: cmdAddr,
		StreamAddr:  streamAddr,
		Timeout:     100 * time.Millisecond, // Short timeout
		MagicNumber: 123456,
		Slippage:    10,
	}

	adapter := NewAdapter(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("failed to start adapter: %v", err)
	}
	defer adapter.CloseConnection()

	// Connect client and then immediately close socket
	var d net.Dialer
	cmdConn, err := d.DialContext(ctx, "tcp", cmdAddr)
	if err != nil {
		t.Fatalf("failed to dial cmd server: %v", err)
	}
	_ = cmdConn.Close() // DROP IMMEDIATELY

	time.Sleep(50 * time.Millisecond)

	// Attempt Execute on dropped socket
	_, execErr := adapter.Execute(context.Background(), model.OrderRequest{
		Symbol: "EURUSD",
		Side:   model.SideBuy,
		Lots:   0.10,
		Price:  1.08500,
	})

	if execErr == nil {
		t.Fatalf("Expected execute error on dropped socket, got nil")
	}
	t.Logf("Socket drop handled safely with error: %v", execErr)
}

func TestMT5Adapter_ExtremeSlippage(t *testing.T) {
	cmdAddr := "127.0.0.1:59105"
	streamAddr := "127.0.0.1:59106"

	cfg := AdapterConfig{
		CommandAddr: cmdAddr,
		StreamAddr:  streamAddr,
		Timeout:     1 * time.Second,
		MagicNumber: 123456,
		Slippage:    50, // 50 points slippage tolerance
	}

	adapter := NewAdapter(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("failed to start adapter: %v", err)
	}
	defer adapter.CloseConnection()

	var d net.Dialer
	cmdConn, err := d.DialContext(ctx, "tcp", cmdAddr)
	if err != nil {
		t.Fatalf("failed to dial cmd server: %v", err)
	}
	defer cmdConn.Close()

	// Mock responding with slippage (+5 pips / +50 points)
	go func() {
		buf := make([]byte, 4096)
		n, _ := cmdConn.Read(buf)
		if n > 0 {
			resp := fmt.Sprintf(`{"request_id":"test-slip","success":true,"retcode":10009,"ticket":"99999","fill_price":%.5f,"lots":0.10,"timestamp_ns":%d}`+"\n",
				1.08550, time.Now().UnixNano())
			_, _ = cmdConn.Write([]byte(resp))
		}
	}()

	time.Sleep(50 * time.Millisecond)

	pos, err := adapter.Execute(context.Background(), model.OrderRequest{
		Symbol: "EURUSD",
		Side:   model.SideBuy,
		Lots:   0.10,
		Price:  1.08500,
	})

	if err != nil {
		t.Fatalf("Failed to execute with slippage: %v", err)
	}
	if pos.EntryPrice != 1.08550 {
		t.Errorf("Expected fill price 1.08550, got: %f", pos.EntryPrice)
	}
}
