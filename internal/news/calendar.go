package news

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ImpactLevel represents the severity of an economic event.
type ImpactLevel string

const (
	ImpactHigh   ImpactLevel = "HIGH"
	ImpactMedium ImpactLevel = "MEDIUM"
	ImpactLow    ImpactLevel = "LOW"
)

// EconomicEvent represents a scheduled macroeconomic news release.
type EconomicEvent struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Country     string      `json:"country"`
	Currency    string      `json:"currency"` // USD, EUR, GBP, JPY, etc.
	Impact      ImpactLevel `json:"impact"`   // HIGH, MEDIUM, LOW
	TimestampNs int64       `json:"timestamp_ns"`
	Forecast    string      `json:"forecast,omitempty"`
	Previous    string      `json:"previous,omitempty"`
}

// CalendarClient manages scheduled economic calendar events with live auto-sync.
type CalendarClient struct {
	events     []EconomicEvent
	httpClient *http.Client
	feedURL    string
	mu         sync.RWMutex
}

// NewCalendarClient creates a new economic calendar client.
func NewCalendarClient(feedURL string) *CalendarClient {
	return &CalendarClient{
		events: make([]EconomicEvent, 0, 128),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		feedURL: feedURL,
	}
}

// AddEvent registers a single economic event.
func (c *CalendarClient) AddEvent(event EconomicEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	event.Currency = strings.ToUpper(strings.TrimSpace(event.Currency))
	c.events = append(c.events, event)
}

// SetEvents replaces the current event list.
func (c *CalendarClient) SetEvents(events []EconomicEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range events {
		events[i].Currency = strings.ToUpper(strings.TrimSpace(events[i].Currency))
	}
	c.events = events
}

// Sync fetches the latest calendar data from the configured HTTP feed.
func (c *CalendarClient) Sync(ctx context.Context) error {
	if c.feedURL == "" {
		return nil // No remote URL configured, using local events
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.feedURL, nil)
	if err != nil {
		return fmt.Errorf("news sync: failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("news sync: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("news sync: unexpected status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("news sync: failed to read body: %w", err)
	}

	var events []EconomicEvent
	if err := json.Unmarshal(body, &events); err != nil {
		return fmt.Errorf("news sync: failed to parse JSON: %w", err)
	}

	c.SetEvents(events)
	log.Printf("[news-calendar] successfully synced %d economic events", len(events))
	return nil
}

// StartAutoSync launches a background goroutine to periodically sync calendar data.
func (c *CalendarClient) StartAutoSync(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Hour
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Initial sync
		_ = c.Sync(ctx)

		for {
			select {
			case <-ticker.C:
				if err := c.Sync(ctx); err != nil {
					log.Printf("[news-calendar] auto-sync error: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// HasUpcomingHighImpact checks if any HIGH impact news occurs within the blackout window.
// Window: [nowNs - afterWindow, nowNs + beforeWindow].
func (c *CalendarClient) HasUpcomingHighImpact(
	currency string,
	beforeWindow, afterWindow time.Duration,
	nowNs int64,
) (bool, *EconomicEvent) {
	curr := strings.ToUpper(strings.TrimSpace(currency))

	c.mu.RLock()
	defer c.mu.RUnlock()

	beforeNs := nowNs + beforeWindow.Nanoseconds()
	afterNs := nowNs - afterWindow.Nanoseconds()

	for i := range c.events {
		ev := &c.events[i]
		if ev.Impact != ImpactHigh {
			continue
		}

		if ev.Currency != curr && curr != "" {
			continue
		}

		// Event timestamp falls inside the blackout zone
		if ev.TimestampNs >= afterNs && ev.TimestampNs <= beforeNs {
			return true, ev
		}
	}

	return false, nil
}
