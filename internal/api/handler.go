// Package api provides REST API endpoints for Relayly.
// All responses include CORS headers to support browser-based clients.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/pairing"
	"github.com/NIKX-Tech/relayly/internal/relay"
	"go.uber.org/zap"
)

// server holds the dependencies for the API handlers.
type server struct {
	db      *database.DB
	hub     *relay.Hub
	log     *zap.Logger
	version string
	startAt time.Time
}

// corsMiddleware adds CORS headers to every response and handles preflight OPTIONS.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeJSON encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ── GET /api/v1/devices ───────────────────────────────────────────────────────

type deviceResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	PairedWith *string    `json:"paired_with"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeen   *time.Time `json:"last_seen"`
}

func (s *server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.db.ListDevices()
	if err != nil {
		s.log.Error("api: list devices", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := make([]deviceResponse, 0, len(devices))
	for _, d := range devices {
		resp = append(resp, deviceResponse{
			ID:         d.ID,
			Name:       d.Name,
			PairedWith: d.PairedWith,
			CreatedAt:  d.CreatedAt,
			LastSeen:   d.LastSeen,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── POST /api/v1/pair ─────────────────────────────────────────────────────────

type pairRequest struct {
	Name string `json:"name"`
}

type pairResponse struct {
	DeviceID  string    `json:"device_id"`
	PairToken string    `json:"pair_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *server) handlePair(w http.ResponseWriter, r *http.Request) {
	var req pairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "body must be JSON with non-empty \"name\" field", http.StatusBadRequest)
		return
	}

	dev, err := pairing.NewDevice(req.Name)
	if err != nil {
		s.log.Error("api: generating device", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := s.db.CreateDevice(dev); err != nil {
		s.log.Error("api: persisting device", zap.Error(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := pairResponse{
		DeviceID:  dev.ID,
		PairToken: dev.PairToken,
		ExpiresAt: *dev.ExpiresAt,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ── GET /api/v1/health ────────────────────────────────────────────────────────

type healthResponse struct {
	Status           string `json:"status"`
	Version          string `json:"version"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	ConnectedDevices int    `json:"connected_devices"`
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(s.startAt)
	resp := healthResponse{
		Status:           "ok",
		Version:          s.version,
		UptimeSeconds:    int64(uptime.Seconds()),
		ConnectedDevices: s.hub.ConnectedCount(),
	}
	writeJSON(w, http.StatusOK, resp)
}
