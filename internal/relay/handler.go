// Package relay — HTTP → WebSocket upgrade handler.
package relay

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nikx-one/relayly/internal/config"
	"github.com/nikx-one/relayly/internal/database"
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
func Handler(hub *Hub, db *database.DB, cfg *config.Config, log *zap.Logger) http.HandlerFunc {
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

// StatusHandler returns a simple JSON health endpoint for the relay.
func StatusHandler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uptime := hub.Uptime().Round(time.Second)
		connected := hub.ConnectedCount()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","connected":` +
			itoa(connected) + `,"uptime":"` + uptime.String() + `"}`))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
