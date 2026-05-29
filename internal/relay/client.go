// Package relay — WebSocket client lifecycle.
package relay

import (
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	flnoise "github.com/flynn/noise"
	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/noise"
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

	conn      *websocket.Conn
	hub       *Hub
	db        *database.DB
	serverKey *noise.Keypair
	send      chan []byte // outbound message queue

	once sync.Once // ensures close() is idempotent
	log  *zap.Logger

	maxMessageBytes int64
	pingInterval    time.Duration
	deadline        time.Duration

	// Cipher states derived from the Noise XX handshake.
	// decCS = client -> server (decrypts)
	// encCS = server -> client (encrypts)
	decCS *flnoise.CipherState
	encCS *flnoise.CipherState
}

// NewClient constructs a Client. Call Pump() to start I/O goroutines.
func NewClient(
	deviceID, pairedDeviceID string,
	conn *websocket.Conn,
	hub *Hub,
	log *zap.Logger,
	maxBytes int64,
	pingInterval, deadline time.Duration,
	serverKey *noise.Keypair,
	db *database.DB,
) *Client {
	return &Client{
		DeviceID:        deviceID,
		PairedDeviceID:  pairedDeviceID,
		conn:            conn,
		hub:             hub,
		db:              db,
		serverKey:       serverKey,
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

	// Perform Noise XX handshake
	if err := c.handshake(); err != nil {
		c.log.Warn("noise handshake failed", zap.Error(err))
		return
	}

	// Start writer goroutine
	go c.writePump()

	// Read pump (runs in the calling goroutine)
	c.readPump()
}

// handshake performs the three-message Noise XX exchange as responder.
func (c *Client) handshake() error {
	hs, err := noise.NewServerHandshake(c.serverKey)
	if err != nil {
		return err
	}

	_ = c.conn.SetReadDeadline(time.Now().Add(c.deadline))
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.deadline))

	// Message 1: ← client
	_, msg1, err := c.conn.ReadMessage()
	if err != nil {
		return err
	}
	if _, _, _, err := hs.ReadMessage(nil, msg1); err != nil {
		return fmt.Errorf("reading client handshake msg1: %w", err)
	}

	// Message 2: → client
	msg2, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return err
	}
	if err := c.conn.WriteMessage(websocket.BinaryMessage, msg2); err != nil {
		return err
	}

	// Message 3: ← client
	_, msg3, err := c.conn.ReadMessage()
	if err != nil {
		return err
	}
	_, cs1, cs2, err := hs.ReadMessage(nil, msg3)
	if err != nil {
		return fmt.Errorf("reading client handshake msg3: %w", err)
	}

	// Handshake complete — verify/store client public key
	remotePub := hs.PeerStatic()
	pubHex := hex.EncodeToString(remotePub)

	device, err := c.db.GetDeviceByID(c.DeviceID)
	if err == nil {
		if device.PublicKey == "" {
			// First handshake: persist the public key
			_ = c.db.UpdatePublicKey(c.DeviceID, pubHex)
			c.log.Info("locked device to public key", zap.String("pub", pubHex))
		} else if device.PublicKey != pubHex {
			// Subsequent handshake: key must match
			return fmt.Errorf("public key mismatch: expected %s, got %s", device.PublicKey, pubHex)
		}
	}

	// Store cipher states for transport
	// Noise XX responder: cs1 = initiator->responder, cs2 = responder->initiator
	c.decCS = cs1
	c.encCS = cs2

	return nil
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

		// Decrypt the payload using the client-to-server cipher state.
		plaintext, err := c.decCS.Decrypt(nil, nil, payload)
		if err != nil {
			c.log.Warn("decryption failed", zap.Error(err))
			continue
		}

		c.hub.Route(Message{From: c.DeviceID, Payload: plaintext}, c.PairedDeviceID)
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

			// Encrypt the payload using the server-to-client cipher state.
			ciphertext, err := c.encCS.Encrypt(nil, nil, payload)
			if err != nil {
				c.log.Error("encryption failed", zap.Error(err))
				continue
			}

			if err := c.conn.WriteMessage(websocket.BinaryMessage, ciphertext); err != nil {
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
