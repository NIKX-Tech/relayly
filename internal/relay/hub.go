// Package relay implements the in-memory WebSocket session hub.
// The Hub manages the lifecycle of connected clients, routes messages
// between paired devices, and exposes metrics for the admin UI.
package relay

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Message is an internal relay message: a binary E2E envelope from one device to its
// pair, forwarded verbatim. The server holds no key material and never decrypts or
// inspects Payload (docs/PROTOCOL.md §4, §6).
type Message struct {
	From    string // device ID
	Payload []byte // opaque E2E envelope bytes
}

// Hub is the central in-memory registry of connected WebSocket clients.
// All operations are goroutine-safe.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client // deviceID → *Client

	// register/unregister channels for client lifecycle events
	Register   chan *Client
	Unregister chan *Client

	// pairCodes tracks in-band pairing codes awaiting a pair_accept (docs/PROTOCOL.md §5.3).
	pairCodes *pairCodeRegistry

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
		pairCodes:  newPairCodeRegistry(),
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

			if peerID, paired := client.Peer(); paired {
				h.notifyPeerStatus(peerID, client.DeviceID, true)
			}

		case client := <-h.Unregister:
			h.mu.Lock()
			wasCurrent := false
			if c, ok := h.clients[client.DeviceID]; ok && c == client {
				delete(h.clients, client.DeviceID)
				wasCurrent = true
			}
			h.mu.Unlock()
			h.log.Info("client disconnected", zap.String("device_id", client.DeviceID))

			// Only notify the peer if this was really the device's live connection,
			// not a stale one already evicted by a reconnect (see Register above):
			// otherwise a reconnect would spuriously report the device as offline.
			if wasCurrent {
				if peerID, paired := client.Peer(); paired {
					h.notifyPeerStatus(peerID, client.DeviceID, false)
				}
			}
		}
	}
}

// GetClient returns the currently connected Client for deviceID, if any.
func (h *Hub) GetClient(deviceID string) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.clients[deviceID]
	return c, ok
}

// notifyPeerStatus pushes a peer_status control message to targetDeviceID about
// aboutDeviceID's online state, if targetDeviceID is currently connected. It is a
// no-op (not an error) if the target isn't online to receive it.
func (h *Hub) notifyPeerStatus(targetDeviceID, aboutDeviceID string, online bool) {
	if target, ok := h.GetClient(targetDeviceID); ok {
		target.sendJSON(controlMessage{Type: "peer_status", PeerID: aboutDeviceID, Online: boolPtr(online)})
	}
}

// Route hands a binary E2E envelope from the sender to its paired device's send queue
// (if online), forwarded verbatim: Route does not parse, decrypt, or transform it.
func (h *Hub) Route(msg Message, pairedDeviceID string) {
	h.mu.RLock()
	peer, ok := h.clients[pairedDeviceID]
	h.mu.RUnlock()

	if !ok {
		// Peer is offline — silently drop (client should handle reconnect)
		return
	}

	if !peer.enqueue(wsFrame{kind: websocket.BinaryMessage, data: msg.Payload}) {
		// Peer's send buffer is full (or already closing) — evict to prevent
		// head-of-line blocking.
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
