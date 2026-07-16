package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Proxy is a transparent WebSocket man-in-the-middle sitting between shims and the
// real relay server: every frame is forwarded verbatim in both directions unless a
// harness-registered rule matches, letting scenarios 7 and 8 manufacture wire-level
// conditions (a corrupted pair_complete frame, a severed connection) that no SDK's
// public API can produce on its own — exactly the point of testing them.
type Proxy struct {
	upstream string // e.g. "ws://127.0.0.1:PORT/ws"
	listener net.Listener

	mu    sync.Mutex
	rules map[string]*rewriteRule // device_id -> single-shot pair_complete rewrite

	connsMu sync.Mutex
	conns   map[string]*proxyConn // device_id -> active connection
}

type rewriteRule struct {
	fakeStaticKeyB64 string
}

type proxyConn struct {
	client   *websocket.Conn
	upstream *websocket.Conn
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// NewProxy starts listening on an OS-assigned localhost port and returns once ready.
func NewProxy(upstream string) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{
		upstream: upstream,
		listener: ln,
		rules:    make(map[string]*rewriteRule),
		conns:    make(map[string]*proxyConn),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", p.handleWS)
	go func() {
		_ = http.Serve(ln, mux) //nolint:gosec // localhost-only test harness, not a real server
	}()
	return p, nil
}

// URL returns the ws://... base URL shims should connect to instead of the real server.
func (p *Proxy) URL() string {
	return "ws://" + p.listener.Addr().String() + "/ws"
}

func (p *Proxy) Close() {
	_ = p.listener.Close()
}

func (p *Proxy) handleWS(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")

	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	upstreamURL := p.upstream + "?" + r.URL.RawQuery
	upstreamConn, _, err := websocket.DefaultDialer.Dial(upstreamURL, nil)
	if err != nil {
		log.Printf("proxy: dialing upstream for %s: %v", deviceID, err)
		_ = clientConn.Close()
		return
	}

	pc := &proxyConn{client: clientConn, upstream: upstreamConn}
	p.connsMu.Lock()
	p.conns[deviceID] = pc
	p.connsMu.Unlock()
	defer func() {
		p.connsMu.Lock()
		if p.conns[deviceID] == pc {
			delete(p.conns, deviceID)
		}
		p.connsMu.Unlock()
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		p.pump(deviceID, upstreamConn, clientConn, true)
	}()
	go func() {
		defer wg.Done()
		p.pump(deviceID, clientConn, upstreamConn, false)
	}()
	wg.Wait()
}

// pump copies frames from src to dst verbatim, applying rewrite rules only on the
// upstream-to-client direction (fromUpstream) since that's the only direction
// scenario 7 needs (the server is what sends pair_complete).
func (p *Proxy) pump(deviceID string, src, dst *websocket.Conn, fromUpstream bool) {
	for {
		msgType, data, err := src.ReadMessage()
		if err != nil {
			_ = dst.Close()
			return
		}
		if fromUpstream {
			data = p.maybeRewrite(deviceID, msgType, data)
		}
		if err := dst.WriteMessage(msgType, data); err != nil {
			_ = src.Close()
			return
		}
	}
}

func (p *Proxy) maybeRewrite(deviceID string, msgType int, data []byte) []byte {
	if msgType != websocket.TextMessage {
		return data
	}

	p.mu.Lock()
	rule, ok := p.rules[deviceID]
	p.mu.Unlock()
	if !ok {
		return data
	}

	var frame map[string]any
	if err := json.Unmarshal(data, &frame); err != nil {
		return data
	}
	if frame["type"] != "pair_complete" {
		return data
	}

	frame["peer_static_key"] = rule.fakeStaticKeyB64
	out, err := json.Marshal(frame)
	if err != nil {
		return data
	}

	// Single-shot: this rule only ever corrupts the first matching frame.
	p.mu.Lock()
	delete(p.rules, deviceID)
	p.mu.Unlock()

	return out
}

// RewriteNextPairComplete arranges for the next pair_complete frame delivered to
// deviceID to have its peer_static_key replaced with fakeStaticKeyB64 (scenario 7:
// the §7.2 server-announced-key cross-check must hard-fail).
func (p *Proxy) RewriteNextPairComplete(deviceID, fakeStaticKeyB64 string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules[deviceID] = &rewriteRule{fakeStaticKeyB64: fakeStaticKeyB64}
}

// SeverConnection immediately closes deviceID's current connection (scenario 8's
// trigger — the victim shim's own reconnect-with-backoff does the rest, and whichever
// peer has the lexicographically smaller device_id re-initiates a rekey per §6).
func (p *Proxy) SeverConnection(deviceID string) {
	p.connsMu.Lock()
	pc, ok := p.conns[deviceID]
	p.connsMu.Unlock()
	if !ok {
		return
	}
	_ = pc.client.Close()
	_ = pc.upstream.Close()
}
