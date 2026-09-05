package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// WebhookNotifier sends alerts as JSON payloads via HTTP POST.
// Non-blocking: fires in a goroutine with a 5-second timeout.
type WebhookNotifier struct {
	url    string
	client *http.Client
}

// webhookPayload is the JSON body sent to the webhook endpoint.
type webhookPayload struct {
	Level     string            `json:"level"`
	Title     string            `json:"title"`
	Message   string            `json:"message"`
	Timestamp string            `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// NewWebhookNotifier creates a webhook notifier for the given URL.
func NewWebhookNotifier(url string) *WebhookNotifier {
	return &WebhookNotifier{
		url: url,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Name returns the notifier identifier.
func (w *WebhookNotifier) Name() string {
	return "Webhook"
}

// Send posts the alert as JSON to the webhook URL.
// Fire-and-forget: errors are returned but should not block the pipeline.
func (w *WebhookNotifier) Send(ctx context.Context, alert Alert) error {
	payload := webhookPayload{
		Level:     alert.Level.String(),
		Title:     alert.Title,
		Message:   alert.Message,
		Timestamp: alert.Timestamp.UTC().Format(time.RFC3339),
		Metadata:  alert.Metadata,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook: marshal error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: send error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook: server returned %d", resp.StatusCode)
	}

	return nil
}
