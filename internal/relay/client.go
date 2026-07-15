// Package relay — WebSocket client lifecycle.
package relay

import (
	"sync"
	"time"

	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// sendBufferSize is the number of frames that can be queued per client.
	sendBufferSize = 256
)

// wsFrame is an outbound WebSocket frame together with its message type, so writePump
// can tell a JSON control message (text) apart from a relayed E2E envelope (binary).
type wsFrame struct {
	kind       int // websocket.TextMessage or websocket.BinaryMessage
	data       []byte
	closeAfter bool // if true, writePump closes the connection right after writing this frame
}

// Client represents a single device's WebSocket connection.
type Client struct {
	DeviceID string

	conn *websocket.Conn
	hub  *Hub
	db   *database.DB

	// send is written to from multiple goroutines (this client's own control
	// handlers, the Hub pushing peer_status, a peer's Client pushing pair_complete),
	// so sendMu/closed guard it against a send racing close()'s close(c.send): a send
	// holds the read lock (concurrent sends are fine, channel sends are goroutine-safe
	// on their own), close holds the write lock, so a send is never in flight while the
	// channel is being closed.
	sendMu sync.RWMutex
	closed bool
	send   chan wsFrame // outbound frame queue

	// pairedWith is the currently linked peer's device ID, or "" if unpaired.
	// It is set at connect time from the DB and can change at runtime once in-band
	// pairing (docs/PROTOCOL.md §5.3) links this client to a peer, so it is guarded
	// by pairedMu rather than being a plain field read/written across goroutines.
	pairedMu   sync.RWMutex
	pairedWith string

	once sync.Once // ensures close() is idempotent
	log  *zap.Logger

	maxMessageBytes int64
	pingInterval    time.Duration
	deadline        time.Duration
}

// NewClient constructs a Client. Call Pump() to start I/O goroutines.
func NewClient(
	deviceID, pairedDeviceID string,
	conn *websocket.Conn,
	hub *Hub,
	log *zap.Logger,
	maxBytes int64,
	pingInterval, deadline time.Duration,
	db *database.DB,
) *Client {
	return &Client{
		DeviceID:        deviceID,
		pairedWith:      pairedDeviceID,
		conn:            conn,
		hub:             hub,
		db:              db,
		send:            make(chan wsFrame, sendBufferSize),
		log:             log.With(zap.String("device_id", deviceID)),
		maxMessageBytes: maxBytes,
		pingInterval:    pingInterval,
		deadline:        deadline,
	}
}

// Peer returns the currently linked peer's device ID, if any.
func (c *Client) Peer() (string, bool) {
	c.pairedMu.RLock()
	defer c.pairedMu.RUnlock()
	return c.pairedWith, c.pairedWith != ""
}

// setPeer links this client to a peer device ID (or clears it, if id is "").
func (c *Client) setPeer(id string) {
	c.pairedMu.Lock()
	c.pairedWith = id
	c.pairedMu.Unlock()
}

// enqueue safely queues frame for writePump, from any goroutine. It is a no-op once
// the client has been closed, and never blocks: if the buffer is full the frame is
// dropped and enqueue returns false, so callers can react (Hub.Route evicts on this).
func (c *Client) enqueue(frame wsFrame) bool {
	c.sendMu.RLock()
	defer c.sendMu.RUnlock()
	if c.closed {
		return false
	}
	select {
	case c.send <- frame:
		return true
	default:
		return false
	}
}

// Pump starts the read and write goroutines. This call blocks until the client
// disconnects; it unregisters the client from the Hub before returning.
func (c *Client) Pump() {
	defer func() {
		c.hub.Unregister <- c
		c.close()
	}()

	go c.writePump()
	c.readPump()
}

// readPump receives frames from the WebSocket and dispatches them: text frames are
// JSON control messages (§5), binary frames are E2E envelopes relayed verbatim to the
// paired device (§4). The server never parses or decrypts binary frame contents.
func (c *Client) readPump() {
	c.conn.SetReadLimit(c.maxMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(c.deadline))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.deadline))
	})

	for {
		msgType, payload, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived,
			) {
				c.log.Warn("unexpected websocket close", zap.Error(err))
			}
			return
		}

		switch msgType {
		case websocket.TextMessage:
			c.handleControl(payload)

		case websocket.BinaryMessage:
			peerID, ok := c.Peer()
			if !ok {
				continue // not paired yet — silently discard (§8)
			}
			c.hub.Route(Message{From: c.DeviceID, Payload: payload}, peerID)
		}
	}
}

// writePump drains the send channel and writes frames to the WebSocket, preserving
// each frame's message type (text control vs. binary envelope).
func (c *Client) writePump() {
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case frame, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.deadline))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(frame.kind, frame.data); err != nil {
				c.log.Warn("write error", zap.Error(err))
				return
			}
			if frame.closeAfter {
				_ = c.conn.Close()
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.deadline))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// close tears down the connection idempotently. Taking sendMu's write lock here
// ensures no enqueue is ever in flight when the channel closes (see enqueue).
func (c *Client) close() {
	c.once.Do(func() {
		_ = c.conn.Close()
		c.sendMu.Lock()
		c.closed = true
		close(c.send)
		c.sendMu.Unlock()
	})
}
