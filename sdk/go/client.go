// Package relayly provides a client library for connecting to a Relayly relay server.
//
// Relayly is a lightweight, self-hosted WebSocket relay server for local-first applications.
// It enables secure, end-to-end encrypted communication between a user's own devices
// (phone, laptop, desktop, etc.) without any third-party infrastructure having access
// to message contents.
//
// # Quick Start
//
//	key, err := relayly.GenerateKey()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	client, err := relayly.Connect(context.Background(), "wss://relay.example.com", relayly.Options{
//	    DeviceID:   "my-laptop",
//	    PrivateKey: key,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	// Request a pairing code to share with another device
//	code, err := client.RequestPairCode(context.Background())
//	fmt.Println("Share this code:", code.Short)
//	fmt.Println("Or scan QR:", code.QRCodeURL("wss://relay.example.com"))
//
//	// Wait for pairing to complete
//	peer, err := code.Wait(context.Background())
//
//	// Send an encrypted message
//	err = client.Send(context.Background(), peer.ID, []byte("hello!"))
//
//	// Receive messages
//	for msg := range client.Messages() {
//	    fmt.Printf("[%s] %s\n", msg.From, msg.Payload)
//	}
package relayly

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// DefaultPingInterval is how often the client sends keepalive pings.
	DefaultPingInterval = 30 * time.Second

	// DefaultReconnectDelay is the initial delay before attempting to reconnect.
	DefaultReconnectDelay = 1 * time.Second

	// DefaultMaxReconnectDelay is the maximum delay between reconnect attempts.
	DefaultMaxReconnectDelay = 60 * time.Second

	// DefaultWriteTimeout is the timeout for WebSocket write operations.
	DefaultWriteTimeout = 10 * time.Second
)

// Client is a connected Relayly client. Use Connect to create one.
type Client struct {
	opts      Options
	serverURL string

	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool

	// peers holds paired remote devices (populated after pairing).
	peers   []Peer
	peersMu sync.RWMutex

	messages chan Message
	pairs    chan PairResult
	sends    chan wireMessage

	// inflight pair requests: code -> channel waiting for pair_complete
	pairWaiters   map[string]chan PairResult
	pairWaitersMu sync.Mutex

	done chan struct{}
}

// Connect dials a Relayly server and authenticates. It returns a ready-to-use Client.
//
//	client, err := relayly.Connect(ctx, "wss://relay.example.com", relayly.Options{
//	    DeviceID:   "my-laptop",
//	    PrivateKey: key,
//	})
func Connect(ctx context.Context, serverURL string, opts Options) (*Client, error) {
	if err := opts.validate(); err != nil {
		return nil, fmt.Errorf("relayly: invalid options: %w", err)
	}

	// Normalize the URL
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("relayly: invalid server URL: %w", err)
	}
	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else if u.Scheme == "https" {
		u.Scheme = "wss"
	}

	c := &Client{
		opts:       opts,
		serverURL:  u.String(),
		messages:   make(chan Message, 64),
		pairs:      make(chan PairResult, 8),
		sends:      make(chan wireMessage, 64),
		pairWaiters: make(map[string]chan PairResult),
		done:       make(chan struct{}),
	}

	if err := c.dial(ctx); err != nil {
		return nil, err
	}

	go c.readLoop()
	go c.writeLoop()
	go c.pingLoop()

	return c, nil
}

// dial establishes the WebSocket connection and performs authentication.
func (c *Client) dial(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, c.serverURL, nil)
	if err != nil {
		return fmt.Errorf("relayly: failed to connect to %s: %w", c.serverURL, err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Send authentication frame
	pubKey, err := c.opts.PrivateKey.PublicKey()
	if err != nil {
		conn.Close()
		return fmt.Errorf("relayly: failed to derive public key: %w", err)
	}

	authMsg := wireMessage{
		Type:      msgTypeAuth,
		DeviceID:  c.opts.DeviceID,
		PublicKey: pubKey.Base64(),
	}

	if err := c.writeJSON(conn, authMsg); err != nil {
		conn.Close()
		return fmt.Errorf("relayly: failed to send auth: %w", err)
	}

	// Wait for auth_ok
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("relayly: failed to read auth response: %w", err)
	}

	var resp wireMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		conn.Close()
		return fmt.Errorf("relayly: invalid auth response: %w", err)
	}

	if resp.Type == msgTypeError {
		conn.Close()
		return fmt.Errorf("relayly: authentication failed: %s (%s)", resp.Message, resp.Code)
	}
	if resp.Type != msgTypeAuthOK {
		conn.Close()
		return fmt.Errorf("relayly: unexpected auth response type: %s", resp.Type)
	}

	return nil
}

// Send encrypts and sends a message to a paired peer device.
//
//	err := client.Send(ctx, peer.ID, []byte("hello!"))
func (c *Client) Send(ctx context.Context, peerID string, payload []byte) error {
	peer, ok := c.findPeer(peerID)
	if !ok {
		return fmt.Errorf("relayly: no paired peer with ID %q — call AcceptPair or RequestPairCode first", peerID)
	}

	ciphertext, nonce, err := c.opts.PrivateKey.Encrypt(payload, peer.PublicKey)
	if err != nil {
		return fmt.Errorf("relayly: encryption failed: %w", err)
	}

	msg := wireMessage{
		Type:    msgTypeSend,
		To:      peerID,
		Payload: encodeBase64(ciphertext),
		Nonce:   encodeBase64(nonce[:]),
	}

	select {
	case c.sends <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return fmt.Errorf("relayly: client is closed")
	}
}

// Messages returns a channel of incoming decrypted messages from paired peers.
// The channel is closed when the client is closed.
//
//	for msg := range client.Messages() {
//	    fmt.Printf("[%s] %s\n", msg.From, msg.Payload)
//	}
func (c *Client) Messages() <-chan Message {
	return c.messages
}

// RequestPairCode asks the server to generate a short pairing code.
// The returned PairCode contains a 6-digit Short code and a QR code URL.
// Call code.Wait(ctx) to block until the other device accepts the pairing.
//
//	code, err := client.RequestPairCode(ctx)
//	fmt.Println("Share this code:", code.Short)
//	peer, err := code.Wait(ctx)
func (c *Client) RequestPairCode(ctx context.Context) (*PairCode, error) {
	// Register a waiter channel before sending the request, so we don't miss the response.
	waiterKey := "__request__"
	ch := make(chan PairResult, 1)

	c.pairWaitersMu.Lock()
	c.pairWaiters[waiterKey] = ch
	c.pairWaitersMu.Unlock()

	msg := wireMessage{Type: msgTypePairRequest}

	select {
	case c.sends <- msg:
	case <-ctx.Done():
		c.removePairWaiter(waiterKey)
		return nil, ctx.Err()
	case <-c.done:
		c.removePairWaiter(waiterKey)
		return nil, fmt.Errorf("relayly: client is closed")
	}

	// Wait for pair_code response
	select {
	case result := <-ch:
		if result.Error != nil {
			return nil, result.Error
		}
		pc := &PairCode{
			Short:     result.Code,
			ExpiresIn: result.ExpiresIn,
			client:    c,
			resultCh:  make(chan PairResult, 1),
		}
		// Register a waiter for the eventual pair_complete
		c.pairWaitersMu.Lock()
		c.pairWaiters[result.Code] = pc.resultCh
		c.pairWaitersMu.Unlock()
		return pc, nil
	case <-ctx.Done():
		c.removePairWaiter(waiterKey)
		return nil, ctx.Err()
	case <-c.done:
		c.removePairWaiter(waiterKey)
		return nil, fmt.Errorf("relayly: client is closed")
	}
}

// AcceptPair uses a 6-digit code from another device to complete the pairing.
//
//	peer, err := client.AcceptPair(ctx, "483921")
func (c *Client) AcceptPair(ctx context.Context, code string) (*Peer, error) {
	ch := make(chan PairResult, 1)

	c.pairWaitersMu.Lock()
	c.pairWaiters[code] = ch
	c.pairWaitersMu.Unlock()

	msg := wireMessage{
		Type: msgTypePairAccept,
		Code: code,
	}

	select {
	case c.sends <- msg:
	case <-ctx.Done():
		c.removePairWaiter(code)
		return nil, ctx.Err()
	case <-c.done:
		c.removePairWaiter(code)
		return nil, fmt.Errorf("relayly: client is closed")
	}

	select {
	case result := <-ch:
		if result.Error != nil {
			return nil, result.Error
		}
		peer := &Peer{ID: result.PeerID, PublicKey: result.PeerPublicKey}
		c.addPeer(peer)
		return peer, nil
	case <-ctx.Done():
		c.removePairWaiter(code)
		return nil, ctx.Err()
	case <-c.done:
		c.removePairWaiter(code)
		return nil, fmt.Errorf("relayly: client is closed")
	}
}

// Close gracefully shuts down the client and closes all channels.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.closed = true
	close(c.done)

	if c.conn != nil {
		c.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		c.conn.Close()
	}
}

// readLoop receives frames from the server and dispatches them.
func (c *Client) readLoop() {
	defer close(c.messages)

	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		_, data, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-c.done:
				return // normal shutdown
			default:
				// TODO: reconnect logic could go here
				return
			}
		}

		var frame wireMessage
		if err := json.Unmarshal(data, &frame); err != nil {
			continue // skip malformed frames
		}

		c.dispatch(frame)
	}
}

// writeLoop serialises outgoing messages onto the WebSocket.
func (c *Client) writeLoop() {
	for {
		select {
		case msg := <-c.sends:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			_ = c.writeJSON(conn, msg)

		case <-c.done:
			return
		}
	}
}

// pingLoop sends periodic pings to keep the connection alive.
func (c *Client) pingLoop() {
	interval := c.opts.PingInterval
	if interval == 0 {
		interval = DefaultPingInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			c.sends <- wireMessage{Type: msgTypePing}
		case <-c.done:
			return
		}
	}
}

// dispatch routes an incoming server frame to the appropriate handler.
func (c *Client) dispatch(frame wireMessage) {
	switch frame.Type {
	case msgTypeMessage:
		c.handleIncomingMessage(frame)

	case msgTypePairCode:
		// Response to pair_request: give the code to the waiter
		c.pairWaitersMu.Lock()
		ch, ok := c.pairWaiters["__request__"]
		if ok {
			delete(c.pairWaiters, "__request__")
		}
		c.pairWaitersMu.Unlock()
		if ok {
			ch <- PairResult{Code: frame.Code, ExpiresIn: frame.ExpiresIn}
		}

	case msgTypePairComplete:
		pubKey, _ := publicKeyFromBase64(frame.PeerPublicKey)
		result := PairResult{PeerID: frame.PeerID, PeerPublicKey: pubKey}

		c.pairWaitersMu.Lock()
		ch, ok := c.pairWaiters[frame.Code]
		if ok {
			delete(c.pairWaiters, frame.Code)
		}
		c.pairWaitersMu.Unlock()

		if ok {
			peer := &Peer{ID: frame.PeerID, PublicKey: pubKey}
			c.addPeer(peer)
			ch <- result
		}

	case msgTypeError:
		// Try to deliver errors to any waiting pair channels
		c.pairWaitersMu.Lock()
		for code, ch := range c.pairWaiters {
			ch <- PairResult{Error: fmt.Errorf("%s: %s", frame.Code, frame.Message)}
			delete(c.pairWaiters, code)
		}
		c.pairWaitersMu.Unlock()
	}
}

// handleIncomingMessage decrypts a message and delivers it to the Messages() channel.
func (c *Client) handleIncomingMessage(frame wireMessage) {
	peer, ok := c.findPeer(frame.From)
	if !ok {
		return // unknown sender, drop
	}

	ciphertext, err := decodeBase64(frame.Payload)
	if err != nil {
		return
	}
	nonceBytes, err := decodeBase64(frame.Nonce)
	if err != nil {
		return
	}

	plaintext, err := c.opts.PrivateKey.Decrypt(ciphertext, nonceBytes, peer.PublicKey)
	if err != nil {
		return // decryption failed, drop
	}

	msg := Message{
		From:      frame.From,
		Payload:   plaintext,
		Timestamp: frame.Timestamp,
	}

	select {
	case c.messages <- msg:
	default:
		// channel full, drop (caller should read promptly)
	}
}

func (c *Client) writeJSON(conn *websocket.Conn, v any) error {
	conn.SetWriteDeadline(time.Now().Add(DefaultWriteTimeout))
	return conn.WriteJSON(v)
}

func (c *Client) removePairWaiter(key string) {
	c.pairWaitersMu.Lock()
	delete(c.pairWaiters, key)
	c.pairWaitersMu.Unlock()
}

// addPeer adds or replaces a peer in the client's peer list.
func (c *Client) addPeer(p *Peer) {
	c.peersMu.Lock()
	defer c.peersMu.Unlock()
	for i, existing := range c.peers {
		if existing.ID == p.ID {
			c.peers[i] = *p
			return
		}
	}
	c.peers = append(c.peers, *p)
}

// findPeer looks up a peer by device ID.
func (c *Client) findPeer(id string) (Peer, bool) {
	c.peersMu.RLock()
	defer c.peersMu.RUnlock()
	for _, p := range c.peers {
		if p.ID == id {
			return p, true
		}
	}
	return Peer{}, false
}
