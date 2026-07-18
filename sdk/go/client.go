// Package relayly provides a client library for connecting to a Relayly relay server.
//
// Relayly is a lightweight, self-hosted WebSocket relay server for local-first applications.
// It enables secure, end-to-end encrypted communication between a user's own devices
// (phone, laptop, desktop, etc.) without any third-party infrastructure having access
// to message contents. Encryption runs device-to-device using Noise XX
// (docs/PROTOCOL.md); the relay itself holds no key material.
//
// # Quick Start
//
//	key, err := relayly.LoadOrGenerateKey("~/.relayly/device.key")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	client, err := relayly.Connect(context.Background(), "wss://relay.example.com", relayly.Options{
//	    DeviceID:    "my-laptop",
//	    DeviceToken: token, // from POST /api/v1/devices or `relayly pair`
//	    PrivateKey:  key,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	// Request a pairing code to share with another device
//	code, err := client.RequestPairCode(context.Background())
//	fmt.Println("Share this code:", code.Short)
//
//	// Wait for pairing to complete (blocks until the Noise handshake is up too)
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

	// handshakeTimeout bounds how long a single Noise handshake attempt (initial
	// pairing or a rekey) is allowed to take before it's considered failed.
	handshakeTimeout = 15 * time.Second
)

// outFrame is an outbound WebSocket frame together with its message type, so
// writeLoop can tell a JSON control message (text) apart from an E2E envelope (binary).
type outFrame struct {
	kind int
	data []byte
}

func controlFrame(msg controlMessage) outFrame {
	data, _ := json.Marshal(msg) // controlMessage has no unmarshalable fields
	return outFrame{kind: websocket.TextMessage, data: data}
}

// pairWaiter is what a pending RequestPairCode/AcceptPair call is waiting on.
type pairWaiter struct {
	ch            chan PairResult
	weAreAcceptor bool // true if this waiter came from AcceptPair (we initiate, §5.3)
}

// Client is a connected Relayly client. Use Connect to create one.
type Client struct {
	opts      Options
	serverURL string

	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool

	peerStore *PeerStore

	peersMu sync.RWMutex
	peers   map[string]*peerConn // device ID -> peer connection (v1: at most one)

	messages chan Message
	sends    chan outFrame

	pairWaitersMu sync.Mutex
	pairWaiters   map[string]pairWaiter

	done chan struct{}
}

// Connect dials a Relayly server and authenticates. It returns a ready-to-use Client.
//
//	client, err := relayly.Connect(ctx, "wss://relay.example.com", relayly.Options{
//	    DeviceID:    "my-laptop",
//	    DeviceToken: token,
//	    PrivateKey:  key,
//	})
func Connect(ctx context.Context, serverURL string, opts Options) (*Client, error) {
	if err := opts.validate(); err != nil {
		return nil, fmt.Errorf("relayly: invalid options: %w", err)
	}

	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("relayly: invalid server URL: %w", err)
	}
	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else if u.Scheme == "https" {
		u.Scheme = "wss"
	}

	peerStore, err := LoadPeerStore(opts.peerStorePath())
	if err != nil {
		return nil, fmt.Errorf("relayly: loading peer store: %w", err)
	}

	c := &Client{
		opts:        opts,
		serverURL:   u.String(),
		peerStore:   peerStore,
		peers:       make(map[string]*peerConn),
		messages:    make(chan Message, 64),
		sends:       make(chan outFrame, 64),
		pairWaiters: make(map[string]pairWaiter),
		done:        make(chan struct{}),
	}

	if err := c.dial(ctx); err != nil {
		return nil, err
	}

	go c.readLoop()
	go c.writeLoop()
	go c.pingLoop()

	return c, nil
}

// dial establishes the WebSocket connection, authenticates at the HTTP layer via
// query params (docs/PROTOCOL.md §3), consumes welcome, and announces our static key.
func (c *Client) dial(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}

	u, err := url.Parse(c.serverURL)
	if err != nil {
		return fmt.Errorf("relayly: invalid server URL: %w", err)
	}
	q := u.Query()
	q.Set("device_id", c.opts.DeviceID)
	q.Set("token", c.opts.DeviceToken)
	u.RawQuery = q.Encode()

	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("relayly: failed to connect to %s: %w", c.serverURL, err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	msgType, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("relayly: failed to read welcome: %w", err)
	}
	if msgType != websocket.TextMessage {
		conn.Close()
		return fmt.Errorf("relayly: expected a text welcome frame")
	}
	var welcome controlMessage
	if err := json.Unmarshal(data, &welcome); err != nil {
		conn.Close()
		return fmt.Errorf("relayly: invalid welcome frame: %w", err)
	}
	if welcome.Type == msgTypeError {
		conn.Close()
		return fmt.Errorf("relayly: authentication failed: %s (%s)", welcome.Message, welcome.Code)
	}
	if welcome.Type != msgTypeWelcome {
		conn.Close()
		return fmt.Errorf("relayly: unexpected first frame type: %s", welcome.Type)
	}
	if welcome.ProtocolVersion != protocolVersion {
		conn.Close()
		return fmt.Errorf("relayly: unsupported protocol_version %d (this SDK implements %d)",
			welcome.ProtocolVersion, protocolVersion)
	}
	c.applyWelcomePeers(welcome.Peers)

	pub, err := c.opts.PrivateKey.PublicKey()
	if err != nil {
		conn.Close()
		return fmt.Errorf("relayly: failed to derive public key: %w", err)
	}
	announce := controlMessage{Type: msgTypeAnnounceKey, StaticKey: pub.Base64()}
	if err := c.writeJSON(conn, announce); err != nil {
		conn.Close()
		return fmt.Errorf("relayly: failed to send announce_key: %w", err)
	}

	return nil
}

// applyWelcomePeers registers any peer(s) welcome reports as already linked, without
// disturbing a peerConn that already exists (a reconnect must not discard a still-healthy
// active session just because the control channel briefly reset).
func (c *Client) applyWelcomePeers(peers []wirePeer) {
	for _, p := range peers {
		if c.getPeerConn(p.ID) == nil {
			c.setPeerConn(p.ID, newPeerConn(p.ID, p.StaticKey))
		}
	}
}

func (c *Client) getPeerConn(id string) *peerConn {
	c.peersMu.RLock()
	defer c.peersMu.RUnlock()
	return c.peers[id]
}

func (c *Client) setPeerConn(id string, p *peerConn) {
	c.peersMu.Lock()
	c.peers[id] = p
	c.peersMu.Unlock()
}

// getSolePeer returns this device's one linked peer, if any (v1 supports exactly one
// linked peer per device, docs/PROTOCOL.md §5.3). Incoming binary envelopes carry no
// sender field — the relay only ever forwards them from the paired device.
func (c *Client) getSolePeer() *peerConn {
	c.peersMu.RLock()
	defer c.peersMu.RUnlock()
	for _, p := range c.peers {
		return p
	}
	return nil
}

// Send encrypts and sends a message to a paired peer.
//
//	err := client.Send(ctx, peer.ID, []byte("hello!"))
func (c *Client) Send(ctx context.Context, peerID string, payload []byte) error {
	peer := c.getPeerConn(peerID)
	if peer == nil {
		return fmt.Errorf("relayly: no paired peer with ID %q — call AcceptPair or RequestPairCode first", peerID)
	}

	ciphertext, err := peer.send(payload)
	if err != nil {
		return err
	}

	frame := outFrame{kind: websocket.BinaryMessage, data: encodeEnvelope(envelopeTransport, ciphertext)}
	select {
	case c.sends <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return fmt.Errorf("relayly: client is closed")
	}
}

// Messages returns a channel of incoming decrypted messages from paired peers.
// The channel is closed when the client is closed.
func (c *Client) Messages() <-chan Message {
	return c.messages
}

// RequestPairCode asks the server to generate a short pairing code.
// Call code.Wait(ctx) to block until the other device accepts the pairing (including
// the resulting Noise handshake completing).
//
//	code, err := client.RequestPairCode(ctx)
//	fmt.Println("Share this code:", code.Short)
//	peer, err := code.Wait(ctx)
func (c *Client) RequestPairCode(ctx context.Context) (*PairCode, error) {
	waiterKey := "__request__"
	ch := make(chan PairResult, 1)

	c.pairWaitersMu.Lock()
	c.pairWaiters[waiterKey] = pairWaiter{ch: ch}
	c.pairWaitersMu.Unlock()

	select {
	case c.sends <- controlFrame(controlMessage{Type: msgTypePairRequest}):
	case <-ctx.Done():
		c.removePairWaiter(waiterKey)
		return nil, ctx.Err()
	case <-c.done:
		c.removePairWaiter(waiterKey)
		return nil, fmt.Errorf("relayly: client is closed")
	}

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
		c.pairWaitersMu.Lock()
		c.pairWaiters[result.Code] = pairWaiter{ch: pc.resultCh}
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

// AcceptPair uses a 6-digit code from another device to complete the pairing. It
// blocks until the resulting Noise handshake completes (docs/PROTOCOL.md §5.3: the
// accepting device is the initiator), so the returned Peer can be Sent to immediately.
//
//	peer, err := client.AcceptPair(ctx, "483921")
func (c *Client) AcceptPair(ctx context.Context, code string) (*Peer, error) {
	ch := make(chan PairResult, 1)

	c.pairWaitersMu.Lock()
	c.pairWaiters[code] = pairWaiter{ch: ch, weAreAcceptor: true}
	c.pairWaitersMu.Unlock()

	select {
	case c.sends <- controlFrame(controlMessage{Type: msgTypePairAccept, Code: code}):
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
		return &Peer{ID: result.PeerID, PublicKey: result.PeerPublicKey}, nil
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

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			select {
			case <-c.done:
				return // normal shutdown
			default:
			}
			if !c.reconnectWithBackoff(err) {
				return
			}
			continue
		}

		switch msgType {
		case websocket.TextMessage:
			var frame controlMessage
			if err := json.Unmarshal(data, &frame); err != nil {
				continue // skip malformed frames
			}
			c.dispatch(frame)

		case websocket.BinaryMessage:
			kind, payload, ok := decodeEnvelope(data)
			if !ok {
				continue
			}
			switch kind {
			case envelopeHandshake:
				c.handleHandshakeEnvelope(payload)
			case envelopeTransport:
				c.handleTransportEnvelope(payload)
			}
		}
	}
}

// reconnectWithBackoff fires OnDisconnect, then retries dialing with exponential backoff.
// Returns true when a connection is re-established, false if shutdown was requested or
// reconnection is disabled (ReconnectDelay < 0).
func (c *Client) reconnectWithBackoff(cause error) bool {
	if c.opts.OnDisconnect != nil {
		c.opts.OnDisconnect(cause)
	}

	delay := c.opts.ReconnectDelay
	if delay < 0 {
		return false
	}
	if delay == 0 {
		delay = DefaultReconnectDelay
	}
	maxDelay := c.opts.MaxReconnectDelay
	if maxDelay == 0 {
		maxDelay = DefaultMaxReconnectDelay
	}

	for {
		select {
		case <-c.done:
			return false
		case <-time.After(delay):
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := c.dial(ctx)
		cancel()
		if err == nil {
			if c.opts.OnReconnect != nil {
				c.opts.OnReconnect()
			}
			return true
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

// writeLoop serialises outgoing frames onto the WebSocket.
func (c *Client) writeLoop() {
	for {
		select {
		case frame := <-c.sends:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			_ = c.writeFrame(conn, frame)

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
			c.sends <- controlFrame(controlMessage{Type: msgTypePing})
		case <-c.done:
			return
		}
	}
}

// dispatch routes an incoming server control frame to the appropriate handler.
// welcome is handled inline by dial(), never here.
func (c *Client) dispatch(frame controlMessage) {
	switch frame.Type {
	case msgTypePairCode:
		c.handlePairCode(frame)
	case msgTypePairComplete:
		c.handlePairComplete(frame)
	case msgTypePeerStatus:
		c.handlePeerStatus(frame)
	case msgTypeError:
		c.handleError(frame)
	case msgTypePong:
		// keepalive, nothing to do
	default:
		// Unknown type: ignored per §5's forward-compatibility rule.
	}
}

func (c *Client) handlePairCode(frame controlMessage) {
	waiter, ok := c.takePairWaiter("__request__")
	if ok {
		waiter.ch <- PairResult{Code: frame.Code, ExpiresIn: frame.ExpiresIn}
	}
}

// handlePairComplete implements §5.3: link the peer, and if we're the accepting
// device, start the Noise handshake as initiator immediately.
func (c *Client) handlePairComplete(frame controlMessage) {
	waiter, ok := c.takePairWaiter(frame.Code)
	if !ok {
		return
	}

	peer := newPeerConn(frame.PeerID, frame.PeerStaticKey)
	peer.setFirstPairWaiter(waiter.ch)
	c.setPeerConn(frame.PeerID, peer)

	if !waiter.weAreAcceptor {
		return // we requested; wait for the accepting device's incoming msg1
	}

	msg1, err := peer.startAsInitiator(c.opts.PrivateKey)
	if err != nil {
		if w := peer.takeFirstPairWaiter(); w != nil {
			w <- PairResult{Error: fmt.Errorf("relayly: starting handshake: %w", err)}
		}
		return
	}
	c.enqueueBinary(envelopeHandshake, msg1)
}

// handlePeerStatus implements §6's reconnect rule: whichever side's device_id is
// lexicographically smaller re-initiates a fresh handshake whenever the peer comes
// online (including this device's own reconnect, since welcome reports the peer's
// state to us too).
func (c *Client) handlePeerStatus(frame controlMessage) {
	online := frame.Online != nil && *frame.Online
	if c.opts.OnPeerStatus != nil {
		c.opts.OnPeerStatus(frame.PeerID, online)
	}
	if !online {
		return
	}

	peer := c.getPeerConn(frame.PeerID)
	if peer == nil {
		return
	}
	if c.opts.DeviceID >= frame.PeerID {
		return // larger ID: wait for the incoming msg1
	}

	msg1, err := peer.startRekeyAsInitiator(c.opts.PrivateKey)
	if err != nil {
		return // best-effort; a future peer_status or AEAD failure can retry
	}
	c.enqueueBinary(envelopeHandshake, msg1)
}

func (c *Client) handleError(frame controlMessage) {
	err := errForCode(frame.Code, frame.Message)
	c.pairWaitersMu.Lock()
	for code, waiter := range c.pairWaiters {
		waiter.ch <- PairResult{Error: err}
		delete(c.pairWaiters, code)
	}
	c.pairWaitersMu.Unlock()
}

// handleHandshakeEnvelope feeds one received Noise handshake message to the sole
// linked peer's connection and resolves it once (and if) it completes.
func (c *Client) handleHandshakeEnvelope(payload []byte) {
	peer := c.getSolePeer()
	if peer == nil {
		return
	}

	reply, completed, wasPending, err := peer.handleHandshakeEnvelope(c.opts.PrivateKey, payload)
	if reply != nil {
		c.enqueueBinary(envelopeHandshake, reply)
	}
	if completed == nil {
		return
	}
	c.resolveHandshake(peer, completed, wasPending, err)
}

// resolveHandshake runs once, exactly when a Noise handshake attempt finishes
// (successfully or not): it applies §7's client-side pin (the real boundary) and
// server-announced-key cross-check, promotes or abandons a rekey attempt per §6's
// make-before-break rule, notifies a first-pairing waiter if one is registered, and
// fires OnReady.
func (c *Client) resolveHandshake(peer *peerConn, session *noiseSession, wasPending bool, hsErr error) {
	var result PairResult
	result.PeerID = peer.id

	switch {
	case hsErr != nil:
		result.Error = fmt.Errorf("relayly: handshake failed: %w", hsErr)
	default:
		pub, verr := c.verifyAndPin(peer, session)
		if verr != nil {
			session.mu.Lock()
			session.status = statusFailed
			session.err = verr
			session.mu.Unlock()
			result.Error = verr
		} else {
			result.PeerPublicKey = pub
		}
	}

	if wasPending {
		if result.Error != nil {
			peer.abandon(session) // existing active session, if any, is untouched
			return
		}
		peer.promote(session)
	}

	if waiter := peer.takeFirstPairWaiter(); waiter != nil {
		waiter <- result
	}
	if result.Error == nil && c.opts.OnReady != nil {
		c.opts.OnReady(peer.id)
	}
}

// verifyAndPin implements §7 in full: the client-side pin (§7.1, the real security
// boundary) and the server-announced-key cross-check (§7.2, defense in depth).
func (c *Client) verifyAndPin(peer *peerConn, session *noiseSession) (PublicKey, error) {
	session.mu.Lock()
	authenticated := session.peerStatic
	session.mu.Unlock()
	authenticatedB64 := encodeBase64(authenticated)

	if err := c.peerStore.PinOrVerify(peer.id, authenticatedB64); err != nil {
		return PublicKey{}, fmt.Errorf("%w (peer %s)", err, peer.id)
	}
	if peer.announcedStaticKey != "" && peer.announcedStaticKey != authenticatedB64 {
		return PublicKey{}, fmt.Errorf("%w: server-announced key for peer %s does not match the authenticated handshake",
			ErrKeyMismatch, peer.id)
	}

	pub, err := publicKeyFromBase64(authenticatedB64)
	if err != nil {
		return PublicKey{}, fmt.Errorf("relayly: invalid authenticated peer key: %w", err)
	}
	return pub, nil
}

func (c *Client) handleTransportEnvelope(ciphertext []byte) {
	peer := c.getSolePeer()
	if peer == nil {
		return
	}

	plaintext, err := peer.recv(ciphertext)
	if err != nil {
		return // not ready, or decryption failed — drop silently
	}

	msg := Message{From: peer.id, Payload: plaintext, Timestamp: time.Now()}
	select {
	case c.messages <- msg:
	default:
		// channel full, drop (caller should read promptly)
	}
}

func (c *Client) enqueueBinary(kind byte, payload []byte) {
	select {
	case c.sends <- outFrame{kind: websocket.BinaryMessage, data: encodeEnvelope(kind, payload)}:
	case <-c.done:
	}
}

func (c *Client) writeJSON(conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	conn.SetWriteDeadline(time.Now().Add(DefaultWriteTimeout))
	return conn.WriteMessage(websocket.TextMessage, data)
}

func (c *Client) writeFrame(conn *websocket.Conn, frame outFrame) error {
	conn.SetWriteDeadline(time.Now().Add(DefaultWriteTimeout))
	return conn.WriteMessage(frame.kind, frame.data)
}

func (c *Client) takePairWaiter(key string) (pairWaiter, bool) {
	c.pairWaitersMu.Lock()
	defer c.pairWaitersMu.Unlock()
	w, ok := c.pairWaiters[key]
	if ok {
		delete(c.pairWaiters, key)
	}
	return w, ok
}

func (c *Client) removePairWaiter(key string) {
	c.pairWaitersMu.Lock()
	delete(c.pairWaiters, key)
	c.pairWaitersMu.Unlock()
}
