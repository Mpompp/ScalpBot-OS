package web

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// ServerCallbacks hooks the web server actions to the bot runtime.
type ServerCallbacks struct {
	OnKillSwitch    func() error
	OnCloseAll      func() error
	OnClosePosition func(orderID string) error
	OnToggleAI      func(enable bool) bool
	OnSetMode       func(mode string) error
	OnSetFocus      func(focus string) error
	GetTelemetry    func() TelemetryPayload
}

// Server provides HTTP static dashboard serving and REST control API.
type Server struct {
	addr      string
	hub       *WSHub
	callbacks ServerCallbacks
	server    *http.Server
}

// NewServer creates a new dashboard web server.
func NewServer(addr string, callbacks ServerCallbacks) *Server {
	hub := NewWSHub()
	s := &Server{
		addr:      addr,
		hub:       hub,
		callbacks: callbacks,
	}

	mux := http.NewServeMux()

	// 1. WebSocket Endpoint
	mux.HandleFunc("/ws", hub.HandleWebSocket)

	// 2. REST API Endpoints
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/control/kill-switch", s.handleKillSwitch)
	mux.HandleFunc("/api/control/close-all", s.handleCloseAll)
	mux.HandleFunc("/api/control/toggle-ai", s.handleToggleAI)
	mux.HandleFunc("/api/control/set-mode", s.handleSetMode)
	mux.HandleFunc("/api/control/set-focus", s.handleSetFocus)
	mux.HandleFunc("/api/control/close-position", s.handleClosePosition)

	// 3. Static Files (Zero browser caching)
	fileServer := http.FileServer(http.Dir("./web"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		fileServer.ServeHTTP(w, r)
	})

	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return s
}

// Start runs the HTTP server and background telemetry broadcast loop.
func (s *Server) Start(ctx context.Context) {
	// Telemetry broadcast ticker (5 times per second = 200ms)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if s.callbacks.GetTelemetry != nil {
					payload := s.callbacks.GetTelemetry()
					s.hub.Broadcast(payload)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// HTTP listen
	go func() {
		log.Printf("[web-dashboard] Server started at http://%s", s.addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[web-dashboard] HTTP server error: %v", err)
		}
	}()
}

// Stop gracefully shuts down the web server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.callbacks.GetTelemetry != nil {
		_ = json.NewEncoder(w).Encode(s.callbacks.GetTelemetry())
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "running"})
}

func (s *Server) handleKillSwitch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.callbacks.OnKillSwitch != nil {
		if err := s.callbacks.OnKillSwitch(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "Emergency Kill-Switch triggered: all trades halted"})
}

func (s *Server) handleCloseAll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.callbacks.OnCloseAll != nil {
		if err := s.callbacks.OnCloseAll(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": err.Error()})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "Closed all active positions"})
}

func (s *Server) handleToggleAI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Enable bool `json:"enable"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	newVal := req.Enable
	if s.callbacks.OnToggleAI != nil {
		newVal = s.callbacks.OnToggleAI(req.Enable)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "ai_enabled": newVal})
}

func (s *Server) handleSetMode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Mode string `json:"mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Mode == "" {
		req.Mode = r.URL.Query().Get("mode")
	}

	if s.callbacks.OnSetMode != nil {
		if err := s.callbacks.OnSetMode(req.Mode); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "status": "error", "error": err.Error()})
			return
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "status": "ok", "mode": req.Mode})
}

func (s *Server) handleSetFocus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Focus string `json:"focus"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Focus == "" {
		req.Focus = r.URL.Query().Get("focus")
	}

	if s.callbacks.OnSetFocus != nil {
		if err := s.callbacks.OnSetFocus(req.Focus); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "status": "error", "error": err.Error()})
			return
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "status": "ok", "focus": req.Focus})
}

func (s *Server) handleClosePosition(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		http.Error(w, "Invalid order_id", http.StatusBadRequest)
		return
	}

	if s.callbacks.OnClosePosition != nil {
		if err := s.callbacks.OnClosePosition(req.OrderID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "status": "error", "error": err.Error()})
			return
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "status": "ok", "order_id": req.OrderID})
}
