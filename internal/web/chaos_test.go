package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebControl_HighConcurrencyStress(t *testing.T) {
	var killSwitchCount atomic.Int64
	var toggleAICount atomic.Int64
	var aiState atomic.Bool
	aiState.Store(true)

	callbacks := ServerCallbacks{
		OnKillSwitch: func() error {
			killSwitchCount.Add(1)
			return nil
		},
		OnCloseAll: func() error {
			return nil
		},
		OnToggleAI: func(enable bool) bool {
			aiState.Store(enable)
			toggleAICount.Add(1)
			return enable
		},
		GetTelemetry: func() TelemetryPayload {
			return TelemetryPayload{
				Balance:         10000.0,
				Equity:          10000.0,
				BotStatus:       "LIVE TRADING",
				AIFilterEnabled: aiState.Load(),
			}
		},
	}

	server := NewServer("127.0.0.1:0", callbacks)

	var wg sync.WaitGroup
	numWorkers := 40
	iterations := 200

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		workerID := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Concurrent GET /api/status
				reqStatus := httptest.NewRequest(http.MethodGet, "/api/status", nil)
				wStatus := httptest.NewRecorder()
				server.handleStatus(wStatus, reqStatus)
				if wStatus.Code != http.StatusOK {
					t.Errorf("Status code: %d", wStatus.Code)
				}

				// Concurrent POST /api/control/toggle-ai
				if i%10 == 0 {
					body, _ := json.Marshal(map[string]bool{"enable": i%2 == 0})
					reqAI := httptest.NewRequest(http.MethodPost, "/api/control/toggle-ai", bytes.NewReader(body))
					wAI := httptest.NewRecorder()
					server.handleToggleAI(wAI, reqAI)
				}

				// Concurrent POST /api/control/kill-switch
				if workerID == 0 && i%50 == 0 {
					reqKill := httptest.NewRequest(http.MethodPost, "/api/control/kill-switch", nil)
					wKill := httptest.NewRecorder()
					server.handleKillSwitch(wKill, reqKill)
				}
			}
		}()
	}

	wg.Wait()

	if killSwitchCount.Load() == 0 {
		t.Errorf("Expected kill switch to be called at least once")
	}
	if toggleAICount.Load() == 0 {
		t.Errorf("Expected toggle AI to be called")
	}

	t.Logf("Web control concurrency stress passed (%d kill-switch calls, %d toggle-ai calls)",
		killSwitchCount.Load(), toggleAICount.Load())
}

func TestWSHub_SlowReaderNonBlockingStress(t *testing.T) {
	hub := NewWSHub()

	// Simulate clients
	for i := 0; i < 15; i++ {
		client := &WSClient{
			hub:  hub,
			send: make(chan []byte, 4), // Tiny buffer of 4
		}
		hub.mu.Lock()
		hub.clients[client] = struct{}{}
		hub.mu.Unlock()
	}

	// Rapidly broadcast 100 payloads (must NEVER block even though send buffers are completely full!)
	start := time.Now()
	for i := 0; i < 100; i++ {
		payload := TelemetryPayload{
			TimestampNs: time.Now().UnixNano(),
			Balance:     10000.0 + float64(i),
			BotStatus:   "STRESS_TEST",
		}
		hub.Broadcast(payload)
	}
	duration := time.Since(start)

	if duration > 100*time.Millisecond {
		t.Fatalf("Broadcast took too long (%s), slow clients may have blocked!", duration)
	}

	t.Logf("100 broadcasts to full buffer clients completed non-blocking in %s", duration)
}
