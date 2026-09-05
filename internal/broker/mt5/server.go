package mt5

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// Server handles listening for MT5 Bridge EA connections and exchanging commands and ticks.
type Server struct {
	cmdAddr     string
	streamAddr  string
	cmdListener net.Listener
	streamListener net.Listener

	cmdConn    net.Conn
	cmdReader  *bufio.Reader
	cmdWriter  *bufio.Writer
	cmdMu      sync.Mutex
	reqSeq     atomic.Int64
	timeout    time.Duration
	isCmdReady atomic.Bool
	closed     atomic.Bool
}

// NewServer creates a new MT5 IPC listener server.
func NewServer(cmdAddr, streamAddr string, timeout time.Duration) *Server {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &Server{
		cmdAddr:    cmdAddr,
		streamAddr: streamAddr,
		timeout:    timeout,
	}
}

// Start listens on the command and stream TCP ports.
func (s *Server) Start(ctx context.Context) error {
	var lc net.ListenConfig

	cmdL, err := lc.Listen(ctx, "tcp", s.cmdAddr)
	if err != nil {
		return fmt.Errorf("mt5 failed to listen on command port %s: %w", s.cmdAddr, err)
	}
	s.cmdListener = cmdL

	streamL, err := lc.Listen(ctx, "tcp", s.streamAddr)
	if err != nil {
		_ = cmdL.Close()
		return fmt.Errorf("mt5 failed to listen on stream port %s: %w", s.streamAddr, err)
	}
	s.streamListener = streamL

	log.Printf("[mt5-server] listening for MT5 Bridge connections on %s (cmd) and %s (stream)",
		s.cmdAddr, s.streamAddr)

	// Accept Command connections in background
	go s.acceptCommandLoop(ctx)

	return nil
}

func (s *Server) acceptCommandLoop(ctx context.Context) {
	for {
		conn, err := s.cmdListener.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}

		if tcpConn, ok := conn.(*net.TCPConn); ok {
			_ = tcpConn.SetNoDelay(true)
			_ = tcpConn.SetKeepAlive(true)
		}

		s.cmdMu.Lock()
		if s.cmdConn != nil {
			_ = s.cmdConn.Close()
		}
		s.cmdConn = conn
		s.cmdReader = bufio.NewReaderSize(conn, 65536)
		s.cmdWriter = bufio.NewWriterSize(conn, 65536)
		s.isCmdReady.Store(true)
		s.cmdMu.Unlock()

		log.Printf("[mt5-server] MT5 Command client connected from %s", conn.RemoteAddr())

		// Start heartbeat for this connection
		go s.heartbeatLoop(ctx, conn)
	}
}

func (s *Server) heartbeatLoop(ctx context.Context, conn net.Conn) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.closed.Load() {
				return
			}
			// Check if connection is still the active one
			s.cmdMu.Lock()
			if s.cmdConn != conn {
				s.cmdMu.Unlock()
				return
			}
			s.cmdMu.Unlock()

			// Send PING command
			req := TradeRequest{Action: ActionPing, RequestID: fmt.Sprintf("PING-%d", time.Now().UnixNano())}
			_, err := s.SendCommand(ctx, req)
			if err != nil {
				log.Printf("[mt5-server] Ping failed, closing connection: %v", err)
				s.cmdMu.Lock()
				if s.cmdConn == conn {
					_ = s.cmdConn.Close()
					s.cmdConn = nil
					s.isCmdReady.Store(false)
				}
				s.cmdMu.Unlock()
				return
			}
		}
	}
}

// IsConnected returns true if MT5 Bridge is connected on the command port.
func (s *Server) IsConnected() bool {
	return s.isCmdReady.Load()
}

// SendCommand sends a command and reads the response synchronously.
func (s *Server) SendCommand(ctx context.Context, req TradeRequest) (TradeResponse, error) {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()

	if !s.isCmdReady.Load() || s.cmdConn == nil {
		return TradeResponse{}, errors.New("mt5: bridge not connected (waiting for MT5 EA connection)")
	}

	if req.RequestID == "" {
		req.RequestID = fmt.Sprintf("REQ-%d", s.reqSeq.Add(1))
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(s.timeout)
	}
	_ = s.cmdConn.SetDeadline(deadline)

	payload, err := json.Marshal(req)
	if err != nil {
		return TradeResponse{}, fmt.Errorf("mt5 marshal error: %w", err)
	}

	if _, err := s.cmdWriter.Write(payload); err != nil {
		s.isCmdReady.Store(false)
		return TradeResponse{}, fmt.Errorf("mt5 write error: %w", err)
	}
	if err := s.cmdWriter.WriteByte('\n'); err != nil {
		s.isCmdReady.Store(false)
		return TradeResponse{}, fmt.Errorf("mt5 write newline error: %w", err)
	}
	if err := s.cmdWriter.Flush(); err != nil {
		s.isCmdReady.Store(false)
		return TradeResponse{}, fmt.Errorf("mt5 flush error: %w", err)
	}

	line, err := s.cmdReader.ReadBytes('\n')
	if err != nil {
		s.isCmdReady.Store(false)
		return TradeResponse{}, fmt.Errorf("mt5 read response error: %w", err)
	}

	var resp TradeResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return TradeResponse{}, fmt.Errorf("mt5 unmarshal response error: %w (raw=%s)", err, string(line))
	}

	return resp, nil
}

// StreamTicks listens on the stream socket and continuously forwards incoming ticks to tickCh.
func (s *Server) StreamTicks(ctx context.Context, tickCh chan<- model.Tick) {
	go func() {
		if s.streamListener == nil {
			log.Println("[mt5-server] stream listener not initialized, exiting stream loop")
			return
		}

		for {
			if s.closed.Load() {
				return
			}

			conn, err := s.streamListener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
					continue
				}
			}

			log.Printf("[mt5-server] MT5 Stream client connected from %s", conn.RemoteAddr())
			if tcpConn, ok := conn.(*net.TCPConn); ok {
				_ = tcpConn.SetNoDelay(true)
			}

			reader := bufio.NewReaderSize(conn, 65536)
			for {
				line, err := reader.ReadBytes('\n')
				if len(line) > 0 {
					trimmed := bytes.TrimSpace(line)
					if len(trimmed) > 0 {
						var dto TickDTO
						if err := json.Unmarshal(trimmed, &dto); err == nil {
							t := model.Tick{
								Symbol:      dto.Symbol,
								Bid:         dto.Bid,
								Ask:         dto.Ask,
								TimestampNs: dto.TimestampNs,
							}
							if t.TimestampNs == 0 {
								t.TimestampNs = time.Now().UnixNano()
							}

							select {
							case tickCh <- t:
							default:
							}
						}
					}
				}
				if err != nil {
					break
				}
			}

			_ = conn.Close()
			log.Println("[mt5-server] MT5 Stream client disconnected, waiting for reconnect...")
		}
	}()
}

// Close terminates the server listeners.
func (s *Server) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.isCmdReady.Store(false)
		if s.cmdConn != nil {
			_ = s.cmdConn.Close()
		}
		if s.cmdListener != nil {
			_ = s.cmdListener.Close()
		}
		if s.streamListener != nil {
			_ = s.streamListener.Close()
		}
	}
	return nil
}
