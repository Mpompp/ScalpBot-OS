package notifier

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// MultiNotifier fans out alerts to multiple notifier backends.
// One backend failure does not block others.
type MultiNotifier struct {
	notifiers []Notifier
}

// NewMultiNotifier creates a notifier that sends to all given backends.
func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

// Name returns a combined name of all backends.
func (m *MultiNotifier) Name() string {
	names := make([]string, len(m.notifiers))
	for i, n := range m.notifiers {
		names[i] = n.Name()
	}
	return "Multi[" + strings.Join(names, ",") + "]"
}

// Send delivers the alert to all backends. Errors are aggregated;
// individual failures are logged but don't prevent delivery to other backends.
func (m *MultiNotifier) Send(ctx context.Context, alert Alert) error {
	var errs []string

	for _, n := range m.notifiers {
		if err := n.Send(ctx, alert); err != nil {
			log.Printf("[notifier] %s failed: %v", n.Name(), err)
			errs = append(errs, fmt.Sprintf("%s: %v", n.Name(), err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("multi-notifier: %d/%d failed: %s",
			len(errs), len(m.notifiers), strings.Join(errs, "; "))
	}

	return nil
}

// LogNotifier is a simple notifier that writes alerts to the Go log.
// Useful as a fallback or in development.
type LogNotifier struct{}

// NewLogNotifier creates a log-based notifier.
func NewLogNotifier() *LogNotifier {
	return &LogNotifier{}
}

// Name returns the notifier identifier.
func (l *LogNotifier) Name() string {
	return "Log"
}

// Send writes the alert to the standard logger.
func (l *LogNotifier) Send(_ context.Context, alert Alert) error {
	log.Printf("[ALERT][%s] %s: %s", alert.Level, alert.Title, alert.Message)
	return nil
}
