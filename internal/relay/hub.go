// Package relay implements the in-memory WebSocket session hub.
// The Hub manages the lifecycle of connected clients, routes messages
// between paired devices, and exposes metrics for the admin UI.
package relay

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// Message is an internal relay message: a decrypted frame from one device to its pair.
type Message struct {
	From string // device ID

	// Payload is plaintext: Client.readPump already decrypted it with the sender's
	// cipher state before handing it to Route. It is not logged or persisted, but it
	// is not opaque to the relay process either (see docs/rfc/000-protocol-reconciliation.md).
	// Route hands it to the peer's Client.writePump, which re-encrypts it with the
	// peer's own cipher state before it goes out on the wire.
	Payload []byte
}

// Hub is the central in-memory registry of connected WebSocket clients.
// All operations are goroutine-safe.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client // deviceID → *Client

	// register/unregister channels for client lifecycle events
	Register   chan *Client
	Unregister chan *Client

	log     *zap.Logger
	startAt time.Time
}

// NewHub creates and returns an initialised Hub. Call Run() to start
// the event loop.
func NewHub(log *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		Register:   make(chan *Client, 64),
		Unregister: make(chan *Client, 64),
		log:        log,
		startAt:    time.Now(),
	}
}

// Run starts the Hub event loop. It blocks until ctx is cancelled.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			// If another connection exists for this device, close it gracefully
			if old, ok := h.clients[client.DeviceID]; ok {
				h.log.Warn("duplicate connection — evicting old client",
					zap.String("device_id", client.DeviceID))
				old.close()
			}
			h.clients[client.DeviceID] = client
			h.mu.Unlock()
			h.log.Info("client connected", zap.String("device_id", client.DeviceID))

		case client := <-h.Unregister:
			h.mu.Lock()
			if c, ok := h.clients[client.DeviceID]; ok && c == client {
				delete(h.clients, client.DeviceID)
			}
			h.mu.Unlock()
			h.log.Info("client disconnected", zap.String("device_id", client.DeviceID))
		}
	}
}

// Route hands a decrypted message from the sender to its paired device's send queue
// (if online); Client.writePump re-encrypts it for that device before it goes out.
// Route itself does not parse or transform the payload beyond that hand-off.
func (h *Hub) Route(msg Message, pairedDeviceID string) {
	h.mu.RLock()
	peer, ok := h.clients[pairedDeviceID]
	h.mu.RUnlock()

	if !ok {
		// Peer is offline — silently drop (client should handle reconnect)
		return
	}

	select {
	case peer.send <- msg.Payload:
	default:
		// Peer's send buffer is full — evict to prevent head-of-line blocking
		h.log.Warn("send buffer full — evicting peer",
			zap.String("peer_id", pairedDeviceID))
		h.Unregister <- peer
	}
}

// ConnectedDevices returns the IDs of all currently connected devices.
func (h *Hub) ConnectedDevices() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// ConnectedCount returns the number of currently connected devices.
func (h *Hub) ConnectedCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Uptime returns how long the hub has been running.
func (h *Hub) Uptime() time.Duration {
	return time.Since(h.startAt)
}
