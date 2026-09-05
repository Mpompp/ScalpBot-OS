package mt5

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrNotConnected = errors.New("mt5: client not connected to bridge")
	ErrTimeout      = errors.New("mt5: request timed out")
)

// Client handles low-latency TCP communication with the MT5 Bridge EA.
type Client struct {
	addr        string
	conn        net.Conn
	reader      *bufio.Reader
	writer      *bufio.Writer
	mu          sync.Mutex
	reqSeq      atomic.Int64
	timeout     time.Duration
	isConnected atomic.Bool
}

// NewClient creates a new MT5 TCP IPC client.
func NewClient(addr string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &Client{
		addr:    addr,
		timeout: timeout,
	}
}

// Connect establishes the TCP connection to the MT5 Bridge socket.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		_ = c.conn.Close()
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		c.isConnected.Store(false)
		return fmt.Errorf("mt5 connect to %s failed: %w", c.addr, err)
	}

	// Disable Nagle's algorithm for minimal packet latency
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(10 * time.Second)
	}

	c.conn = conn
	c.reader = bufio.NewReaderSize(conn, 65536)
	c.writer = bufio.NewWriterSize(conn, 65536)
	c.isConnected.Store(true)

	return nil
}

// Close disconnects from the MT5 Bridge.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.isConnected.Store(false)
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// IsConnected returns true if the connection is active.
func (c *Client) IsConnected() bool {
	return c.isConnected.Load()
}

// SendCommand sends a TradeRequest and blocks until the TradeResponse is received or timeout occurs.
func (c *Client) SendCommand(ctx context.Context, req TradeRequest) (TradeResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isConnected.Load() || c.conn == nil {
		return TradeResponse{}, ErrNotConnected
	}

	// Generate request ID if empty
	if req.RequestID == "" {
		req.RequestID = fmt.Sprintf("REQ-%d", c.reqSeq.Add(1))
	}

	// Set deadline
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.timeout)
	}
	_ = c.conn.SetDeadline(deadline)

	// Serialize & write NDJSON (Newline Delimited JSON)
	payload, err := json.Marshal(req)
	if err != nil {
		return TradeResponse{}, fmt.Errorf("mt5 marshal error: %w", err)
	}

	if _, err := c.writer.Write(payload); err != nil {
		c.isConnected.Store(false)
		return TradeResponse{}, fmt.Errorf("mt5 write error: %w", err)
	}
	if err := c.writer.WriteByte('\n'); err != nil {
		c.isConnected.Store(false)
		return TradeResponse{}, fmt.Errorf("mt5 write newline error: %w", err)
	}
	if err := c.writer.Flush(); err != nil {
		c.isConnected.Store(false)
		return TradeResponse{}, fmt.Errorf("mt5 flush error: %w", err)
	}

	// Read response line
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		c.isConnected.Store(false)
		return TradeResponse{}, fmt.Errorf("mt5 read response error: %w", err)
	}

	var resp TradeResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return TradeResponse{}, fmt.Errorf("mt5 unmarshal response error: %w (raw=%s)", err, string(line))
	}

	return resp, nil
}

// Ping checks if the MT5 bridge is responsive.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.SendCommand(ctx, TradeRequest{
		Action: ActionPing,
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("ping failed: %s", resp.ErrorMsg)
	}
	return nil
}
