package mt5

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// AdapterConfig holds settings for the MT5 adapter.
type AdapterConfig struct {
	CommandAddr string        // Address to listen for Trade/Command channel, e.g. "127.0.0.1:5555"
	StreamAddr  string        // Address to listen for Tick Stream channel, e.g. "127.0.0.1:5556"
	Timeout     time.Duration // Command timeout
	MagicNumber int           // Expert Advisor Magic Number
	Slippage    int           // Slippage tolerance in points
}

// DefaultAdapterConfig returns default connection settings for MT5.
func DefaultAdapterConfig() AdapterConfig {
	return AdapterConfig{
		CommandAddr: "127.0.0.1:5555",
		StreamAddr:  "127.0.0.1:5556",
		Timeout:     2 * time.Second,
		MagicNumber: 123456,
		Slippage:    10,
	}
}

// Adapter implements the broker.Broker interface, bridging scalpbot to MT5.
type Adapter struct {
	server *Server
	cfg    AdapterConfig
}

// NewAdapter creates a new MT5 broker adapter.
func NewAdapter(cfg AdapterConfig) *Adapter {
	return &Adapter{
		server: NewServer(cfg.CommandAddr, cfg.StreamAddr, cfg.Timeout),
		cfg:    cfg,
	}
}

// Connect starts listening for MT5 connections.
func (a *Adapter) Connect(ctx context.Context) error {
	return a.server.Start(ctx)
}

// CloseConnection shuts down the adapter server.
func (a *Adapter) CloseConnection() error {
	return a.server.Close()
}

// IsConnected returns true if MT5 EA has connected.
func (a *Adapter) IsConnected() bool {
	return a.server.IsConnected()
}

// Execute submits an OrderRequest to MT5 and waits for the execution confirmation.
func (a *Adapter) Execute(ctx context.Context, order model.OrderRequest) (model.Position, error) {
	action := ActionBuy
	if order.Side == model.SideSell {
		action = ActionSell
	}

	req := TradeRequest{
		Action:      action,
		Symbol:      order.Symbol,
		Lots:        order.Lots,
		Price:       order.Price,
		StopLoss:    order.StopLoss,
		TakeProfit:  order.TakeProfit,
		Slippage:    a.cfg.Slippage,
		MagicNumber: a.cfg.MagicNumber,
	}

	resp, err := a.server.SendCommand(ctx, req)
	if err != nil {
		return model.Position{}, fmt.Errorf("mt5 execute error: %w", err)
	}

	if !resp.Success {
		return model.Position{}, fmt.Errorf("mt5 execution rejected: %s (retcode=%d)", resp.ErrorMsg, resp.RetCode)
	}

	pos := model.Position{
		OrderID:    resp.Ticket,
		Symbol:     order.Symbol,
		Side:       order.Side,
		Lots:       resp.Lots,
		EntryPrice: resp.FillPrice,
		CurrentPnL: 0,
		OpenTimeNs: resp.TimestampNs,
	}

	if pos.OpenTimeNs == 0 {
		pos.OpenTimeNs = time.Now().UnixNano()
	}

	return pos, nil
}

// Close closes an open position in MT5 by its order/ticket ID.
func (a *Adapter) Close(ctx context.Context, positionID string) error {
	req := TradeRequest{
		Action:      ActionClose,
		Ticket:      positionID,
		Slippage:    a.cfg.Slippage,
		MagicNumber: a.cfg.MagicNumber,
	}

	resp, err := a.server.SendCommand(ctx, req)
	if err != nil {
		return fmt.Errorf("mt5 close error: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("mt5 close failed: %s (retcode=%d)", resp.ErrorMsg, resp.RetCode)
	}

	return nil
}

// ListOpenPositions queries MT5 for all active positions for state reconciliation.
func (a *Adapter) ListOpenPositions(ctx context.Context) ([]model.Position, error) {
	req := TradeRequest{
		Action:      ActionPositions,
		MagicNumber: a.cfg.MagicNumber,
	}

	resp, err := a.server.SendCommand(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("mt5 list positions error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("mt5 list positions failed: %s", resp.ErrorMsg)
	}

	positions := make([]model.Position, 0, len(resp.Positions))
	for _, p := range resp.Positions {
		side := model.SideBuy
		if strings.ToUpper(p.Side) == "SELL" {
			side = model.SideSell
		}

		positions = append(positions, model.Position{
			OrderID:    p.Ticket,
			Symbol:     p.Symbol,
			Side:       side,
			Lots:       p.Lots,
			EntryPrice: p.OpenPrice,
			CurrentPnL: p.Profit,
			OpenTimeNs: p.OpenTimeNs,
		})
	}

	return positions, nil
}

// ListOpenPositionsWithDetails returns raw PositionDTOs including SL/TP/Profit for tracker synchronization.
func (a *Adapter) ListOpenPositionsWithDetails(ctx context.Context) ([]PositionDTO, error) {
	req := TradeRequest{
		Action:      ActionPositions,
		MagicNumber: a.cfg.MagicNumber,
	}

	resp, err := a.server.SendCommand(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("mt5 list positions detail error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("mt5 list positions detail failed: %s", resp.ErrorMsg)
	}

	return resp.Positions, nil
}

// GetAccountState queries the current equity, balance, and margin from MT5.
func (a *Adapter) GetAccountState(ctx context.Context) (*AccountDTO, error) {
	req := TradeRequest{
		Action: ActionAccount,
	}

	resp, err := a.server.SendCommand(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("mt5 get account error: %w", err)
	}

	if !resp.Success || resp.Account == nil {
		return nil, fmt.Errorf("mt5 get account failed: %s", resp.ErrorMsg)
	}

	return resp.Account, nil
}

// FetchHistoryDeals queries MT5 for official completed history deals.
func (a *Adapter) FetchHistoryDeals(ctx context.Context, days float64) ([]HistoryDealDTO, error) {
	req := TradeRequest{
		Action:      ActionHistory,
		MagicNumber: a.cfg.MagicNumber,
		Days:        days,
	}

	resp, err := a.server.SendCommand(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("mt5 get history error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("mt5 get history failed: %s", resp.ErrorMsg)
	}

	return resp.History, nil
}

// StreamTicks listens on the stream socket and pipes ticks into tickCh.
func (a *Adapter) StreamTicks(ctx context.Context, tickCh chan<- model.Tick) {
	a.server.StreamTicks(ctx, tickCh)
}

// SubscribeTicks is an alias for StreamTicks.
func (a *Adapter) SubscribeTicks(ctx context.Context, tickCh chan<- model.Tick) {
	a.StreamTicks(ctx, tickCh)
}
