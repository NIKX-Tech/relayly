// Package relay — HTTP → WebSocket upgrade handler.
package relay

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/NIKX-Tech/relayly/internal/config"
	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/noise"
	"github.com/NIKX-Tech/relayly/pkg/version"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// CheckOrigin: all origins accepted for a self-hosted relay.
	// Operators can restrict this via a reverse proxy or by overriding CheckOrigin.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler returns an http.HandlerFunc that:
//  1. Authenticates the device via ?device_id=&token= query params
//  2. Upgrades the connection to WebSocket
//  3. Registers the client with the Hub and starts I/O pumps
func Handler(hub *Hub, db *database.DB, cfg *config.Config, log *zap.Logger, serverKey *noise.Keypair) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		deviceID := q.Get("device_id")
		token := q.Get("token")

		if deviceID == "" || token == "" {
			http.Error(w, "missing device_id or token", http.StatusBadRequest)
			return
		}

		// Authenticate: verify the token matches the claimed device ID
		device, err := db.GetDeviceByID(deviceID)
		if err != nil {
			log.Warn("device not found", zap.String("device_id", deviceID))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if device.PairToken != token {
			log.Warn("invalid token", zap.String("device_id", deviceID))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Check pairing code expiry
		if device.ExpiresAt != nil && time.Now().After(*device.ExpiresAt) {
			log.Warn("pairing code expired", zap.String("device_id", deviceID))
			http.Error(w, "pairing code expired", http.StatusUnauthorized)
			return
		}

		// Resolve paired device (may be nil if not yet paired)
		pairedDeviceID := ""
		if device.PairedWith != nil {
			pairedDeviceID = *device.PairedWith
		}

		// Upgrade
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("websocket upgrade failed", zap.Error(err))
			return
		}

		wsCfg := cfg.WebSocket
		client := NewClient(
			deviceID, pairedDeviceID, conn, hub, log,
			wsCfg.MaxMessageBytes,
			wsCfg.PingInterval,
			wsCfg.Deadline,
			serverKey,
			db,
		)

		hub.Register <- client

		// Update last_seen asynchronously
		go func() {
			_ = db.TouchLastSeen(deviceID)
		}()

		log.Info("device connected",
			zap.String("device_id", deviceID),
			zap.String("paired_with", pairedDeviceID),
			zap.String("remote", r.RemoteAddr),
		)

		// Pump blocks until disconnect
		client.Pump()
	}
}

// statusResponse is the JSON shape returned by StatusHandler.
type statusResponse struct {
	Status           string `json:"status"`
	Version          string `json:"version"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	ConnectedDevices int    `json:"connected_devices"`
}

// StatusHandler returns a JSON health endpoint for the relay.
func StatusHandler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := statusResponse{
			Status:           "ok",
			Version:          version.Version,
			UptimeSeconds:    int64(hub.Uptime().Seconds()),
			ConnectedDevices: hub.ConnectedCount(),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
