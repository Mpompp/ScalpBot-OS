package news

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
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
	c := &CalendarClient{
		events: make([]EconomicEvent, 0, 128),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		feedURL: feedURL,
	}
	c.seedDefaultSchedule()
	return c
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

// GetUpcomingEvent finds the next upcoming or active economic event for the given currency.
// Returns the event pointer, countdown in seconds (negative if already active), blackout flag, and whether an event was found.
func (c *CalendarClient) GetUpcomingEvent(
	currency string,
	beforeWindow, afterWindow time.Duration,
	nowNs int64,
) (ev *EconomicEvent, countdownSec int64, isBlackout bool, found bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	curr := strings.ToUpper(strings.TrimSpace(currency))
	var nearestEv *EconomicEvent
	var minDiffNs int64 = math.MaxInt64

	for i := range c.events {
		e := &c.events[i]
		if curr != "" && e.Currency != curr {
			continue
		}
		if e.Impact != ImpactHigh && e.Impact != ImpactMedium {
			continue
		}

		diffNs := e.TimestampNs - nowNs

		// Check if inside blackout window: [-afterWindow, +beforeWindow]
		inBlackout := (diffNs >= -afterWindow.Nanoseconds() && diffNs <= beforeWindow.Nanoseconds())
		if inBlackout {
			return e, diffNs / 1e9, true, true
		}

		// Otherwise look for the closest future event
		if diffNs > 0 && diffNs < minDiffNs {
			minDiffNs = diffNs
			nearestEv = e
		}
	}

	if nearestEv != nil {
		return nearestEv, minDiffNs / 1e9, false, true
	}

	return nil, 0, false, false
}

// seedDefaultSchedule generates standard recurring macroeconomic releases anchored around the current week.
func (c *CalendarClient) seedDefaultSchedule() {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	type templateEvent struct {
		dayOffset int
		hour      int
		minute    int
		title     string
		currency  string
		impact    ImpactLevel
	}

	templates := []templateEvent{
		{dayOffset: 0, hour: 13, minute: 30, title: "Core CPI m/m", currency: "USD", impact: ImpactHigh},
		{dayOffset: 0, hour: 15, minute: 0, title: "ISM Manufacturing PMI", currency: "USD", impact: ImpactHigh},
		{dayOffset: 1, hour: 13, minute: 30, title: "Producer Price Index (PPI) m/m", currency: "USD", impact: ImpactHigh},
		{dayOffset: 1, hour: 18, minute: 0, title: "FOMC Economic Projections & Rate", currency: "USD", impact: ImpactHigh},
		{dayOffset: 2, hour: 13, minute: 30, title: "Initial Jobless Claims", currency: "USD", impact: ImpactMedium},
		{dayOffset: 2, hour: 15, minute: 0, title: "Existing Home Sales", currency: "USD", impact: ImpactMedium},
		{dayOffset: 3, hour: 13, minute: 30, title: "Non-Farm Employment Change (NFP)", currency: "USD", impact: ImpactHigh},
		{dayOffset: 3, hour: 13, minute: 30, title: "Unemployment Rate", currency: "USD", impact: ImpactHigh},
		{dayOffset: 4, hour: 14, minute: 0, title: "Fed Chair Speaks", currency: "USD", impact: ImpactHigh},
	}

	for i, t := range templates {
		eventTime := today.AddDate(0, 0, t.dayOffset).Add(time.Duration(t.hour)*time.Hour + time.Duration(t.minute)*time.Minute)
		c.events = append(c.events, EconomicEvent{
			ID:          fmt.Sprintf("SCHED-%d", i+1),
			Title:       t.title,
			Country:     t.currency,
			Currency:    t.currency,
			Impact:      t.impact,
			TimestampNs: eventTime.UnixNano(),
		})
	}
}
