// Package relay — WebSocket client lifecycle.
package relay

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// sendBufferSize is the number of messages that can be queued per client.
	sendBufferSize = 256
)

// Client represents a single device's WebSocket connection.
type Client struct {
	DeviceID       string
	PairedDeviceID string // empty if not paired at connect time

	conn *websocket.Conn
	hub  *Hub
	send chan []byte // outbound message queue

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
) *Client {
	return &Client{
		DeviceID:        deviceID,
		PairedDeviceID:  pairedDeviceID,
		conn:            conn,
		hub:             hub,
		send:            make(chan []byte, sendBufferSize),
		log:             log.With(zap.String("device_id", deviceID)),
		maxMessageBytes: maxBytes,
		pingInterval:    pingInterval,
		deadline:        deadline,
	}
}

// Pump starts the read and write goroutines. This call blocks until the client
// disconnects; it unregisters the client from the Hub before returning.
func (c *Client) Pump() {
	defer func() {
		c.hub.Unregister <- c
		c.close()
	}()

	// Start writer goroutine
	go c.writePump()

	// Read pump (runs in the calling goroutine)
	c.readPump()
}

// readPump receives frames from the WebSocket and routes them to the peer.
func (c *Client) readPump() {
	c.conn.SetReadLimit(c.maxMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(c.deadline))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.deadline))
	})

	for {
		_, payload, err := c.conn.ReadMessage()
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

		if c.PairedDeviceID == "" {
			// Device isn't paired — silently discard
			continue
		}

		c.hub.Route(Message{From: c.DeviceID, Payload: payload}, c.PairedDeviceID)
	}
}

// writePump drains the send channel and writes frames to the WebSocket.
func (c *Client) writePump() {
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case payload, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.deadline))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
				c.log.Warn("write error", zap.Error(err))
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

// close tears down the connection idempotently.
func (c *Client) close() {
	c.once.Do(func() {
		_ = c.conn.Close()
		close(c.send)
	})
}
