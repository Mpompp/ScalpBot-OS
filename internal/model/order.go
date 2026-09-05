package model

// OrderStatus represents the lifecycle state of an order.
type OrderStatus int8

const (
	// OrderPending indicates the order has been submitted but not yet processed.
	OrderPending OrderStatus = iota
	// OrderFilled indicates the order was successfully executed.
	OrderFilled
	// OrderRejected indicates the order was rejected by broker or risk engine.
	OrderRejected
	// OrderCancelled indicates the order was cancelled before fill.
	OrderCancelled
)

// String returns the human-readable name of the order status.
func (s OrderStatus) String() string {
	switch s {
	case OrderPending:
		return "PENDING"
	case OrderFilled:
		return "FILLED"
	case OrderRejected:
		return "REJECTED"
	case OrderCancelled:
		return "CANCELLED"
	default:
		return "UNKNOWN"
	}
}

// OrderSide represents the direction of an order.
type OrderSide int8

const (
	// SideBuy opens a long position.
	SideBuy OrderSide = iota
	// SideSell opens a short position.
	SideSell
)

// String returns the human-readable name of the order side.
func (s OrderSide) String() string {
	switch s {
	case SideBuy:
		return "BUY"
	case SideSell:
		return "SELL"
	default:
		return "UNKNOWN"
	}
}

// OrderRequest represents a validated order ready for broker submission.
// Created by the Risk Engine after evaluating a Signal.
type OrderRequest struct {
	Symbol      string      // Currency pair
	Side        OrderSide   // Buy or Sell
	Lots        float64     // Position size in standard lots
	Price       float64     // Requested execution price
	StopLoss    float64     // Stop-loss price level
	TakeProfit  float64     // Take-profit price level
	Status      OrderStatus // Current lifecycle status
	SignalID    string      // Reference to the originating strategy signal
	TimestampNs int64       // Order creation time as Unix nanoseconds
}

// Position represents an open trading position returned by the broker.
type Position struct {
	OrderID    string    // Unique order identifier from broker
	Symbol     string    // Currency pair
	Side       OrderSide // Long or Short
	Lots       float64   // Position size
	EntryPrice float64   // Actual fill price
	CurrentPnL float64   // Unrealized P&L in account currency
	OpenTimeNs int64     // Position open time as Unix nanoseconds
}
