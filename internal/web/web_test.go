package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebServer_StatusAndControlEndpoints(t *testing.T) {
	killSwitchCalled := false
	closeAllCalled := false
	aiEnabled := true

	callbacks := ServerCallbacks{
		OnKillSwitch: func() error {
			killSwitchCalled = true
			return nil
		},
		OnCloseAll: func() error {
			closeAllCalled = true
			return nil
		},
		OnToggleAI: func(enable bool) bool {
			aiEnabled = enable
			return aiEnabled
		},
		GetTelemetry: func() TelemetryPayload {
			return TelemetryPayload{
				Balance:         10000.0,
				Equity:          10050.0,
				FloatingPnL:     50.0,
				BotStatus:       "RUNNING",
				AIFilterEnabled: aiEnabled,
			}
		},
	}

	server := NewServer("127.0.0.1:0", callbacks)

	// 1. Test /api/status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	wStatus := httptest.NewRecorder()
	server.handleStatus(wStatus, reqStatus)

	if wStatus.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", wStatus.Code)
	}

	var statusPayload TelemetryPayload
	if err := json.NewDecoder(wStatus.Body).Decode(&statusPayload); err != nil {
		t.Fatalf("failed to decode status JSON: %v", err)
	}
	if statusPayload.Balance != 10000.0 || statusPayload.Equity != 10050.0 {
		t.Errorf("unexpected balance/equity in payload: %+v", statusPayload)
	}

	// 2. Test /api/control/kill-switch
	reqKill := httptest.NewRequest(http.MethodPost, "/api/control/kill-switch", nil)
	wKill := httptest.NewRecorder()
	server.handleKillSwitch(wKill, reqKill)

	if wKill.Code != http.StatusOK {
		t.Fatalf("expected kill switch status 200, got %d", wKill.Code)
	}
	if !killSwitchCalled {
		t.Errorf("expected OnKillSwitch callback to be invoked")
	}

	// 3. Test /api/control/close-all
	reqCloseAll := httptest.NewRequest(http.MethodPost, "/api/control/close-all", nil)
	wCloseAll := httptest.NewRecorder()
	server.handleCloseAll(wCloseAll, reqCloseAll)

	if wCloseAll.Code != http.StatusOK {
		t.Fatalf("expected close-all status 200, got %d", wCloseAll.Code)
	}
	if !closeAllCalled {
		t.Errorf("expected OnCloseAll callback to be invoked")
	}

	// 4. Test /api/control/toggle-ai
	reqBody, _ := json.Marshal(map[string]bool{"enable": false})
	reqAI := httptest.NewRequest(http.MethodPost, "/api/control/toggle-ai", bytes.NewReader(reqBody))
	wAI := httptest.NewRecorder()
	server.handleToggleAI(wAI, reqAI)

	if wAI.Code != http.StatusOK {
		t.Fatalf("expected toggle-ai status 200, got %d", wAI.Code)
	}
	if aiEnabled != false {
		t.Errorf("expected AI to be toggled to false")
	}
}

func TestWSFrameEncoding(t *testing.T) {
	data := []byte(`{"status":"ok"}`)
	frame := encodeWSFrame(data)

	if len(frame) != len(data)+2 {
		t.Errorf("expected frame length %d, got %d", len(data)+2, len(frame))
	}
	// First byte: 0x81 (Fin + Text Opcode)
	if frame[0] != 0x81 {
		t.Errorf("expected opcode 0x81, got 0x%X", frame[0])
	}
	// Second byte: Payload length
	if frame[1] != byte(len(data)) {
		t.Errorf("expected payload length %d, got %d", len(data), frame[1])
	}
}

func TestWebServer_StartStop(t *testing.T) {
	server := NewServer("127.0.0.1:0", ServerCallbacks{})
	ctx, cancel := context.WithCancel(context.Background())
	server.Start(ctx)
	cancel()
	_ = server.Stop(context.Background())
}
