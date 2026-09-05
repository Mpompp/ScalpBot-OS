// Package executor provides a worker pool for dispatching orders to the broker.
// Orders flow through a buffered channel with backpressure protection.
package executor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/pompbot/scalpbot/internal/broker"
	"github.com/pompbot/scalpbot/internal/model"
)

// FillCallback is called after a successful order fill.
// Used by the pipeline to notify the risk manager of new positions.
type FillCallback func(pos model.Position, originalOrder model.OrderRequest)

// RejectCallback is called when an order fails execution.
type RejectCallback func(order model.OrderRequest, err error)

// Dispatcher manages a pool of worker goroutines that consume orders
// from a channel and execute them through the broker interface.
type Dispatcher struct {
	orderCh         chan model.OrderRequest
	brk             broker.Broker
	onFill          FillCallback
	onReject        RejectCallback
	workers         int
	wg              sync.WaitGroup
	inFlightMu      sync.Mutex
	inFlightSymbols map[string]time.Time
}

// NewDispatcher creates a dispatcher with the given channel buffer size.
// The broker, callbacks, and worker count are configured before Start().
func NewDispatcher(bufferSize int, brk broker.Broker, workers int) *Dispatcher {
	if bufferSize <= 0 {
		bufferSize = 32
	}
	if workers <= 0 {
		workers = 2
	}
	return &Dispatcher{
		orderCh:         make(chan model.OrderRequest, bufferSize),
		brk:             brk,
		workers:         workers,
		inFlightSymbols: make(map[string]time.Time),
	}
}

// SetCallbacks configures the fill and reject callbacks.
// Must be called before Start().
func (d *Dispatcher) SetCallbacks(onFill FillCallback, onReject RejectCallback) {
	d.onFill = onFill
	d.onReject = onReject
}

// Start launches the worker pool. Workers run until ctx is cancelled.
// After cancellation, workers drain remaining orders before exiting.
func (d *Dispatcher) Start(ctx context.Context) {
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		go d.worker(ctx, i)
	}
	log.Printf("[executor] started %d workers, buffer=%d", d.workers, cap(d.orderCh))
}

// Submit sends an order to the dispatch channel.
// Prevents in-flight duplication on the same symbol during order execution.
// Returns an error if an order for this symbol is already in flight or if the channel is full.
func (d *Dispatcher) Submit(order model.OrderRequest) error {
	symKey := strings.ToUpper(strings.TrimSpace(order.Symbol))

	d.inFlightMu.Lock()
	if lastTime, exists := d.inFlightSymbols[symKey]; exists {
		if time.Since(lastTime) < 5*time.Second {
			d.inFlightMu.Unlock()
			return fmt.Errorf("executor: order for %s is already in flight, skipping duplicate submission", symKey)
		}
	}
	d.inFlightSymbols[symKey] = time.Now()
	d.inFlightMu.Unlock()

	select {
	case d.orderCh <- order:
		return nil
	default:
		d.inFlightMu.Lock()
		delete(d.inFlightSymbols, symKey)
		d.inFlightMu.Unlock()
		return fmt.Errorf("executor: order channel full (cap=%d), dropping order for %s",
			cap(d.orderCh), order.Symbol)
	}
}

// Stop signals shutdown and waits for all workers to finish processing
// in-flight orders. Must be called after context cancellation.
func (d *Dispatcher) Stop() {
	close(d.orderCh)
	d.wg.Wait()
	log.Println("[executor] all workers stopped")
}

// Pending returns the number of orders waiting in the channel.
func (d *Dispatcher) Pending() int {
	return len(d.orderCh)
}

// worker is the main loop for each worker goroutine.
func (d *Dispatcher) worker(ctx context.Context, id int) {
	defer d.wg.Done()

	for {
		select {
		case order, ok := <-d.orderCh:
			if !ok {
				// Channel closed — drain complete
				log.Printf("[executor] worker %d: channel closed, exiting", id)
				return
			}
			d.executeOrder(ctx, id, order)

		case <-ctx.Done():
			// Context cancelled — drain remaining orders
			log.Printf("[executor] worker %d: context cancelled, draining queue", id)
			for order := range d.orderCh {
				d.executeOrder(ctx, id, order)
			}
			return
		}
	}
}

// executeOrder handles a single order execution through the broker.
func (d *Dispatcher) executeOrder(ctx context.Context, workerID int, order model.OrderRequest) {
	symKey := strings.ToUpper(strings.TrimSpace(order.Symbol))
	defer func() {
		d.inFlightMu.Lock()
		delete(d.inFlightSymbols, symKey)
		d.inFlightMu.Unlock()
	}()

	pos, err := d.brk.Execute(ctx, order)
	if err != nil {
		log.Printf("[executor] worker %d: order REJECTED %s %s: %v",
			workerID, order.Side, order.Symbol, err)
		if d.onReject != nil {
			d.onReject(order, err)
		}
		return
	}

	log.Printf("[executor] worker %d: order FILLED %s %s @ %.5f, lots=%.2f",
		workerID, order.Side, order.Symbol, pos.EntryPrice, pos.Lots)
	if d.onFill != nil {
		d.onFill(pos, order)
	}
}
