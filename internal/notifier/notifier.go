// Package notifier provides alerting interfaces and implementations
// for critical trading events (circuit breaker trips, connection loss,
// consecutive rejections, etc.).
package notifier

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AlertLevel represents the severity of an alert.
type AlertLevel int8

const (
	// AlertInfo is for informational messages (trade opened/closed).
	AlertInfo AlertLevel = iota
	// AlertWarning is for degraded conditions (consecutive rejections).
	AlertWarning
	// AlertCritical is for serious issues (connection loss).
	AlertCritical
	// AlertEmergency is for trading-halting events (circuit breaker).
	AlertEmergency
)

// String returns the human-readable alert level name.
func (l AlertLevel) String() string {
	switch l {
	case AlertInfo:
		return "INFO"
	case AlertWarning:
		return "WARNING"
	case AlertCritical:
		return "CRITICAL"
	case AlertEmergency:
		return "EMERGENCY"
	default:
		return "UNKNOWN"
	}
}

// Emoji returns an emoji indicator for the alert level.
func (l AlertLevel) Emoji() string {
	switch l {
	case AlertInfo:
		return "ℹ️"
	case AlertWarning:
		return "⚠️"
	case AlertCritical:
		return "🔴"
	case AlertEmergency:
		return "🚨"
	default:
		return "❓"
	}
}

// Alert represents a notification event.
type Alert struct {
	Level     AlertLevel        // Severity
	Title     string            // Short summary
	Message   string            // Detailed description
	Timestamp time.Time         // When the alert was generated
	Metadata  map[string]string // Optional key-value context
}

// FormatMarkdown renders the alert as a Markdown message.
func (a Alert) FormatMarkdown() string {
	msg := fmt.Sprintf("%s *%s*\n%s\n_%s_",
		a.Level.Emoji(), a.Title, a.Message,
		a.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC"))

	if len(a.Metadata) > 0 {
		msg += "\n"
		for k, v := range a.Metadata {
			msg += fmt.Sprintf("\n`%s`: %s", k, v)
		}
	}
	return msg
}

// Notifier is the interface for sending alerts to external systems.
type Notifier interface {
	// Send delivers an alert. Implementations should be non-blocking
	// and handle their own timeouts.
	Send(ctx context.Context, alert Alert) error

	// Name returns a human-readable identifier for this notifier.
	Name() string
}

// RateLimitedNotifier wraps a Notifier with per-title rate limiting
// to prevent alert spam during volatile conditions.
type RateLimitedNotifier struct {
	inner    Notifier
	interval time.Duration          // Minimum interval between same-title alerts
	lastSent map[string]time.Time   // Last send time per alert title
	mu       sync.Mutex
}

// NewRateLimited wraps a notifier with rate limiting.
// interval: minimum time between alerts with the same title (e.g. 60s).
func NewRateLimited(inner Notifier, interval time.Duration) *RateLimitedNotifier {
	return &RateLimitedNotifier{
		inner:    inner,
		interval: interval,
		lastSent: make(map[string]time.Time),
	}
}

// Send delivers the alert if it hasn't been sent recently (same title).
func (r *RateLimitedNotifier) Send(ctx context.Context, alert Alert) error {
	r.mu.Lock()
	last, exists := r.lastSent[alert.Title]
	if exists && time.Since(last) < r.interval {
		r.mu.Unlock()
		return nil // Rate limited — silently drop
	}
	r.lastSent[alert.Title] = time.Now()
	r.mu.Unlock()

	return r.inner.Send(ctx, alert)
}

// Name returns the wrapped notifier name.
func (r *RateLimitedNotifier) Name() string {
	return r.inner.Name() + " (rate-limited)"
}
