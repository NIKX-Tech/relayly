// Package relay implements the in-memory WebSocket session hub.
// The Hub manages the lifecycle of connected clients, routes messages
// between paired devices, and exposes metrics for the admin UI.
package relay

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// Message is an internal relay message: a raw frame from one device to its pair.
type Message struct {
	From    string // device ID
	Payload []byte // opaque encrypted bytes — never inspected by the relay
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

// Route forwards a message from the sender to its paired device (if online).
// The payload is forwarded verbatim — the relay is payload-agnostic.
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
