// Package client provides a reference Go client for connecting to a Relayly
// relay server and performing end-to-end encrypted communication.
//
// # Protocol Summary
//
//  1. Client connects to ws://<relay>/ws?device_id=<id>&token=<token>
//  2. Client performs a Noise XX handshake over WebSocket binary frames:
//     Frame 1: client → server (32-byte ephemeral pub)
//     Frame 2: server → client (encrypted server static pub + ephemeral)
//     Frame 3: client → server (encrypted client static pub)
//  3. After handshake, the relay forwards all subsequent binary frames
//     verbatim to the paired device.
//  4. The client uses the Noise transport CipherState to encrypt/decrypt
//     application payloads before sending/after receiving.
//
// # Usage
//
//	kp, _ := noise.GenerateKeypair()
//	c, _ := client.New(client.Options{
//	    ServerURL:  "ws://localhost:8080/ws",
//	    DeviceID:   "...",
//	    Token:      "...",
//	    Keypair:    kp,
//	})
//	c.Connect(ctx)
//	c.Send([]byte("hello"))
//	msg := <-c.Recv()
package client

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	fnoise "github.com/flynn/noise"
	"github.com/gorilla/websocket"
	"github.com/NIKX-Tech/relayly/internal/noise"
)

// Options configures a Relayly client connection.
type Options struct {
	// ServerURL is the WebSocket relay URL, e.g. "ws://localhost:8080/ws"
	ServerURL string

	// DeviceID is the UUID assigned by `relayly pair`
	DeviceID string

	// Token is the pairing token assigned by `relayly pair`
	Token string

	// Keypair is this client's Noise static keypair
	Keypair *noise.Keypair

	// ServerPublicKey is the server's static Noise public key (optional, for pinning)
	ServerPublicKey []byte

	// RecvBufferSize sets the size of the inbound message channel (default 128)
	RecvBufferSize int
}

// Client manages a single Relayly WebSocket connection with Noise E2EE.
type Client struct {
	opts Options
	conn *websocket.Conn

	send chan []byte
	recv chan []byte

	// Noise transport cipher states (established after handshake)
	encCS *fnoise.CipherState // local → remote
	decCS *fnoise.CipherState // remote → local

	once   sync.Once
	closed chan struct{}
}

// New creates a Client. Call Connect() to establish the connection.
func New(opts Options) (*Client, error) {
	if opts.RecvBufferSize == 0 {
		opts.RecvBufferSize = 128
	}
	if opts.DeviceID == "" || opts.Token == "" {
		return nil, fmt.Errorf("DeviceID and Token are required")
	}
	if opts.Keypair == nil {
		return nil, fmt.Errorf("Keypair is required")
	}
	return &Client{
		opts:   opts,
		send:   make(chan []byte, 256),
		recv:   make(chan []byte, opts.RecvBufferSize),
		closed: make(chan struct{}),
	}, nil
}

// Connect dials the relay, performs the Noise handshake, and starts I/O loops.
// It blocks until ctx is cancelled or the connection drops.
func (c *Client) Connect(ctx context.Context) error {
	u, err := url.Parse(c.opts.ServerURL)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}
	q := u.Query()
	q.Set("device_id", c.opts.DeviceID)
	q.Set("token", c.opts.Token)
	u.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("dialing relay: %w", err)
	}
	c.conn = conn

	// Perform Noise XX handshake
	if err := c.handshake(); err != nil {
		_ = conn.Close()
		return fmt.Errorf("noise handshake: %w", err)
	}

	// Start I/O goroutines
	go c.readLoop(ctx)
	go c.writeLoop(ctx)

	// Wait for context or connection close
	select {
	case <-ctx.Done():
	case <-c.closed:
	}
	return nil
}

// Send queues an application payload for encrypted delivery to the paired device.
func (c *Client) Send(payload []byte) error {
	select {
	case c.send <- payload:
		return nil
	case <-c.closed:
		return fmt.Errorf("connection closed")
	}
}

// Recv returns the channel on which decrypted messages from the peer arrive.
func (c *Client) Recv() <-chan []byte {
	return c.recv
}

// Close terminates the connection.
func (c *Client) Close() {
	c.once.Do(func() {
		_ = c.conn.Close()
		close(c.closed)
	})
}

// ── Internal ──────────────────────────────────────────────────────────────────

// handshake performs the three-message Noise XX exchange as initiator.
func (c *Client) handshake() error {
	hs, err := noise.NewClientHandshake(c.opts.Keypair, c.opts.ServerPublicKey)
	if err != nil {
		return err
	}

	// Message 1: → server
	msg1, _, _, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return err
	}
	if err := c.conn.WriteMessage(websocket.BinaryMessage, msg1); err != nil {
		return err
	}

	// Message 2: ← server
	_, msg2, err := c.conn.ReadMessage()
	if err != nil {
		return err
	}
	if _, _, _, err := hs.ReadMessage(nil, msg2); err != nil {
		return fmt.Errorf("reading server handshake msg: %w", err)
	}

	// Message 3: → server
	msg3, cs1, cs2, err := hs.WriteMessage(nil, nil)
	if err != nil {
		return err
	}
	if err := c.conn.WriteMessage(websocket.BinaryMessage, msg3); err != nil {
		return err
	}

	// cs1 = initiator send / responder recv
	// cs2 = initiator recv / responder send
	c.encCS = cs1
	c.decCS = cs2

	return nil
}

func (c *Client) readLoop(ctx context.Context) {
	defer c.Close()
	for {
		_, frame, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		// Decrypt
		plaintext, err := c.decCS.Decrypt(nil, nil, frame)
		if err != nil {
			// Decryption failure — drop frame (do not expose to application)
			continue
		}
		select {
		case c.recv <- plaintext:
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		}
	}
}

func (c *Client) writeLoop(ctx context.Context) {
	defer c.Close()
	for {
		select {
		case payload := <-c.send:
			// Encrypt
			ciphertext, err := c.encCS.Encrypt(nil, nil, payload)
			if err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, ciphertext); err != nil {
				return
			}
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		}
	}
}
