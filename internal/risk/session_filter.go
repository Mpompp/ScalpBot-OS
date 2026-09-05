package risk

import (
	"fmt"
	"time"

	"github.com/pompbot/scalpbot/internal/model"
)

// SessionWindow defines a UTC trading session by hour range.
type SessionWindow struct {
	Name      string // e.g. "London", "NewYork"
	StartHour int    // UTC start hour (inclusive), 0-23
	EndHour   int    // UTC end hour (exclusive), 0-23
}

// SessionFilter restricts trading to configured market session windows.
// Rejects signals outside active trading hours to avoid low-liquidity periods.
type SessionFilter struct {
	sessions []SessionWindow
}

// NewSessionFilter creates a filter with custom session windows.
func NewSessionFilter(sessions []SessionWindow) *SessionFilter {
	return &SessionFilter{sessions: sessions}
}

// NewLondonSession returns a filter for London session (07:00-16:00 UTC).
func NewLondonSession() *SessionFilter {
	return NewSessionFilter([]SessionWindow{
		{Name: "London", StartHour: 7, EndHour: 16},
	})
}

// NewNewYorkSession returns a filter for New York session (12:00-21:00 UTC).
func NewNewYorkSession() *SessionFilter {
	return NewSessionFilter([]SessionWindow{
		{Name: "NewYork", StartHour: 12, EndHour: 21},
	})
}

// NewLondonNYOverlap returns a filter for the London-New York overlap
// (12:00-16:00 UTC), the highest liquidity period.
func NewLondonNYOverlap() *SessionFilter {
	return NewSessionFilter([]SessionWindow{
		{Name: "LDN-NY Overlap", StartHour: 12, EndHour: 16},
	})
}

// NewMultiSession returns a filter allowing trading during multiple sessions.
func NewMultiSession(sessions ...SessionWindow) *SessionFilter {
	return NewSessionFilter(sessions)
}

// Name returns the filter identifier.
func (f *SessionFilter) Name() string {
	return "SessionFilter"
}

// Allow returns true if the tick timestamp falls within any configured session.
func (f *SessionFilter) Allow(tick model.Tick) (bool, string) {
	t := time.Unix(0, tick.TimestampNs).UTC()
	hour := t.Hour()

	for _, s := range f.sessions {
		if s.StartHour <= s.EndHour {
			// Normal range: e.g. 7-16
			if hour >= s.StartHour && hour < s.EndHour {
				return true, ""
			}
		} else {
			// Wraps midnight: e.g. 22-6 (Asian session)
			if hour >= s.StartHour || hour < s.EndHour {
				return true, ""
			}
		}
	}

	return false, fmt.Sprintf("outside trading sessions (UTC hour=%d)", hour)
}
