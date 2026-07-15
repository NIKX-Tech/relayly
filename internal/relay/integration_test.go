package relay_test

// Integration tests against a real HTTP test server running relay.Handler, driven by
// hand-rolled raw *websocket.Conn test clients (NOT sdk/go, which is still on the old
// protocol until docs/tasks/02-sdks-and-interop.md lands). These prove the server's
// side of docs/PROTOCOL.md end to end: registration, connect, the control channel,
// in-band pairing, a real Noise XX handshake run device-to-device through the relay,
// verbatim binary relay, and reconnect/re-handshake behavior.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	flnoise "github.com/flynn/noise"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/NIKX-Tech/relayly/internal/config"
	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/pairing"
	"github.com/NIKX-Tech/relayly/internal/relay"
)

// noiseCipherSuite matches docs/PROTOCOL.md §6: Noise_XX_25519_ChaChaPoly_BLAKE2s.
var noiseCipherSuite = flnoise.NewCipherSuite(flnoise.DH25519, flnoise.CipherChaChaPoly, flnoise.HashBLAKE2s)

// wireMsg mirrors the control-channel JSON shape from the test's (external) point of
// view; internal/relay's own controlMessage type is unexported.
type wireMsg struct {
	Type            string `json:"type"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
	DeviceID        string `json:"device_id,omitempty"`
	Peers           []struct {
		ID        string `json:"id"`
		StaticKey string `json:"static_key"`
	} `json:"peers,omitempty"`
	StaticKey     string `json:"static_key,omitempty"`
	Code          string `json:"code,omitempty"`
	ExpiresIn     int    `json:"expires_in,omitempty"`
	PeerID        string `json:"peer_id,omitempty"`
	PeerStaticKey string `json:"peer_static_key,omitempty"`
	Online        *bool  `json:"online,omitempty"`
	Message       string `json:"message,omitempty"`
}

type testServer struct {
	*httptest.Server
	db *database.DB
}

func newTestServer(t *testing.T, maxMessageBytes int64) *testServer {
	t.Helper()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hub := relay.NewHub(zap.NewNop())
	go hub.Run()

	cfg := &config.Config{
		WebSocket: config.WSCfg{
			MaxMessageBytes: maxMessageBytes,
			PingInterval:    time.Minute,
			Deadline:        time.Minute,
		},
	}

	mux := http.NewServeMux()
	mux.Handle("/ws", relay.Handler(hub, db, cfg, zap.NewNop()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &testServer{Server: srv, db: db}
}

func registerDevice(t *testing.T, ts *testServer, id, name string) *database.Device {
	t.Helper()
	d, err := pairing.NewDevice(name)
	if err != nil {
		t.Fatalf("NewDevice: %v", err)
	}
	d.ID = id
	if err := ts.db.CreateDevice(d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	return d
}

func dial(t *testing.T, ts *testServer, d *database.Device) *websocket.Conn {
	t.Helper()
	url := strings.Replace(ts.URL, "http://", "ws://", 1) + "/ws?device_id=" + d.ID + "&token=" + d.DeviceToken
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readControl(t *testing.T, conn *websocket.Conn) wireMsg {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	typ, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read control: %v", err)
	}
	if typ != websocket.TextMessage {
		t.Fatalf("expected text control frame, got message type %d: %q", typ, data)
	}
	var m wireMsg
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode control frame %q: %v", data, err)
	}
	return m
}

func sendControl(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal control frame: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write control frame: %v", err)
	}
}

func sendEnvelope(t *testing.T, conn *websocket.Conn, envType byte, payload []byte) {
	t.Helper()
	frame := append([]byte{envType}, payload...)
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
}

func readEnvelope(t *testing.T, conn *websocket.Conn) (byte, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	typ, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	if typ != websocket.BinaryMessage {
		t.Fatalf("expected binary envelope, got message type %d", typ)
	}
	if len(data) < 1 {
		t.Fatalf("empty envelope")
	}
	return data[0], data[1:]
}

func genStaticKeypair(t *testing.T) flnoise.DHKey {
	t.Helper()
	kp, err := noiseCipherSuite.GenerateKeypair(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return kp
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// ── The main flow: pair, real Noise XX handshake, transport, reconnect, re-handshake ──

func TestIntegration_PairHandshakeExchangeReconnect(t *testing.T) {
	ts := newTestServer(t, 65536)

	// "device-a" < "device-b" lexicographically: A requests pairing, so B is the
	// Noise initiator on first pairing (§5.3); A is the smaller ID, so A must
	// re-initiate after any reconnect (§6) — the two phases exercise both roles.
	devA := registerDevice(t, ts, "device-a", "A")
	devB := registerDevice(t, ts, "device-b", "B")
	keyA := genStaticKeypair(t)
	keyB := genStaticKeypair(t)

	connA := dial(t, ts, devA)
	welcomeA := readControl(t, connA)
	if welcomeA.Type != "welcome" || welcomeA.DeviceID != "device-a" || len(welcomeA.Peers) != 0 {
		t.Fatalf("unexpected welcome for A: %+v", welcomeA)
	}

	connB := dial(t, ts, devB)
	welcomeB := readControl(t, connB)
	if welcomeB.Type != "welcome" || welcomeB.DeviceID != "device-b" {
		t.Fatalf("unexpected welcome for B: %+v", welcomeB)
	}

	sendControl(t, connA, wireMsg{Type: "announce_key", StaticKey: b64(keyA.Public)})
	sendControl(t, connB, wireMsg{Type: "announce_key", StaticKey: b64(keyB.Public)})

	// Pairing: A requests a code, the harness plays courier (a human reading a
	// 6-digit code off one screen and typing it into the other), B accepts it.
	sendControl(t, connA, wireMsg{Type: "pair_request"})
	pairCode := readControl(t, connA)
	if pairCode.Type != "pair_code" || len(pairCode.Code) != 6 {
		t.Fatalf("unexpected pair_code: %+v", pairCode)
	}

	sendControl(t, connB, wireMsg{Type: "pair_accept", Code: pairCode.Code})

	completeB := readControl(t, connB)
	if completeB.Type != "pair_complete" || completeB.PeerID != "device-a" || completeB.PeerStaticKey != b64(keyA.Public) {
		t.Fatalf("B's pair_complete: %+v", completeB)
	}
	completeA := readControl(t, connA)
	if completeA.Type != "pair_complete" || completeA.PeerID != "device-b" || completeA.PeerStaticKey != b64(keyB.Public) {
		t.Fatalf("A's pair_complete: %+v", completeA)
	}

	// First Noise XX handshake: B is the initiator (§5.3).
	csA1, csA2, csB1, csB2 := runNoiseHandshake(t, connA, connB, keyA, keyB, false /* aIsInitiator */)

	// Transport both ways: cs1 = initiator(B)->responder(A), cs2 = responder(A)->initiator(B).
	exchange(t, connB, connA, csB1, csA1, "hello from B, first session")
	exchange(t, connA, connB, csA2, csB2, "hello from A, first session")

	// Reconnect A: close and redial with the same identity.
	_ = connA.Close()
	statusOfflineB := readControl(t, connB)
	if statusOfflineB.Type != "peer_status" || statusOfflineB.PeerID != "device-a" || statusOfflineB.Online == nil || *statusOfflineB.Online {
		t.Fatalf("expected peer_status offline for device-a, got %+v", statusOfflineB)
	}

	devAFresh, err := ts.db.GetDeviceByID("device-a")
	if err != nil {
		t.Fatalf("get device-a: %v", err)
	}
	connA2 := dial(t, ts, devAFresh)
	welcomeA2 := readControl(t, connA2)
	if welcomeA2.Type != "welcome" || len(welcomeA2.Peers) != 1 || welcomeA2.Peers[0].ID != "device-b" {
		t.Fatalf("unexpected welcome on reconnect: %+v", welcomeA2)
	}
	peerStatusA2 := readControl(t, connA2)
	if peerStatusA2.Type != "peer_status" || peerStatusA2.PeerID != "device-b" || peerStatusA2.Online == nil || !*peerStatusA2.Online {
		t.Fatalf("expected peer_status online for device-b on reconnect welcome, got %+v", peerStatusA2)
	}

	statusOnlineB := readControl(t, connB)
	if statusOnlineB.Type != "peer_status" || statusOnlineB.PeerID != "device-a" || statusOnlineB.Online == nil || !*statusOnlineB.Online {
		t.Fatalf("expected peer_status online for device-a, got %+v", statusOnlineB)
	}

	// Re-handshake: "device-a" is lexicographically smaller, so it must be the
	// initiator this time (§6) — the opposite role from the first handshake.
	csA1b, csA2b, csB1b, csB2b := runNoiseHandshake(t, connA2, connB, keyA, keyB, true /* aIsInitiator */)

	exchange(t, connA2, connB, csA1b, csB1b, "hello from A, second session")
	exchange(t, connB, connA2, csB2b, csA2b, "hello from B, second session")
}

// runNoiseHandshake drives a full 3-message Noise XX handshake between connA and connB
// through the relay (binary envelope, type 0x01), with the two long-term static
// keypairs supplied by the caller. It returns both sides' cipher states in a fixed
// convention regardless of who initiated: (aDecrypt, aEncrypt, bDecrypt, bEncrypt) —
// i.e. csA1 decrypts what A receives, csA2 encrypts what A sends, and symmetrically
// for B, matching flynn/noise's cs1 = initiator->responder / cs2 = responder->initiator.
func runNoiseHandshake(
	t *testing.T,
	connA, connB *websocket.Conn,
	keyA, keyB flnoise.DHKey,
	aIsInitiator bool,
) (aDecrypt, aEncrypt, bDecrypt, bEncrypt *flnoise.CipherState) {
	t.Helper()

	initConn, initKey, respConn, respKey := connA, keyA, connB, keyB
	if !aIsInitiator {
		initConn, initKey, respConn, respKey = connB, keyB, connA, keyA
	}

	hsInit, err := flnoise.NewHandshakeState(flnoise.Config{
		CipherSuite: noiseCipherSuite, Random: rand.Reader,
		Pattern: flnoise.HandshakeXX, Initiator: true, StaticKeypair: initKey,
	})
	if err != nil {
		t.Fatalf("new initiator handshake state: %v", err)
	}
	hsResp, err := flnoise.NewHandshakeState(flnoise.Config{
		CipherSuite: noiseCipherSuite, Random: rand.Reader,
		Pattern: flnoise.HandshakeXX, Initiator: false, StaticKeypair: respKey,
	})
	if err != nil {
		t.Fatalf("new responder handshake state: %v", err)
	}

	msg1, _, _, err := hsInit.WriteMessage(nil, nil)
	if err != nil {
		t.Fatalf("write msg1: %v", err)
	}
	sendEnvelope(t, initConn, 0x01, msg1)
	envType, data := readEnvelope(t, respConn)
	if envType != 0x01 {
		t.Fatalf("expected handshake envelope 0x01, got %#x", envType)
	}
	if _, _, _, err := hsResp.ReadMessage(nil, data); err != nil {
		t.Fatalf("read msg1: %v", err)
	}

	msg2, _, _, err := hsResp.WriteMessage(nil, nil)
	if err != nil {
		t.Fatalf("write msg2: %v", err)
	}
	sendEnvelope(t, respConn, 0x01, msg2)
	envType, data = readEnvelope(t, initConn)
	if envType != 0x01 {
		t.Fatalf("expected handshake envelope 0x01, got %#x", envType)
	}
	if _, _, _, err := hsInit.ReadMessage(nil, data); err != nil {
		t.Fatalf("read msg2: %v", err)
	}

	msg3, initCS1, initCS2, err := hsInit.WriteMessage(nil, nil)
	if err != nil {
		t.Fatalf("write msg3: %v", err)
	}
	sendEnvelope(t, initConn, 0x01, msg3)
	envType, data = readEnvelope(t, respConn)
	if envType != 0x01 {
		t.Fatalf("expected handshake envelope 0x01, got %#x", envType)
	}
	_, respCS1, respCS2, err := hsResp.ReadMessage(nil, data)
	if err != nil {
		t.Fatalf("read msg3: %v", err)
	}
	if initCS1 == nil || respCS1 == nil {
		t.Fatalf("handshake did not complete: initiator or responder cipher states are nil")
	}

	// cs1 = initiator->responder, cs2 = responder->initiator (flynn/noise convention).
	if aIsInitiator {
		return respCS1, initCS2, initCS1, respCS2
	}
	return initCS1, respCS2, respCS1, initCS2
}

// exchange encrypts plaintext with sendCS, relays it as a transport envelope (0x02)
// from senderConn to receiverConn, decrypts with recvCS, and asserts round-trip.
func exchange(t *testing.T, senderConn, receiverConn *websocket.Conn, sendCS, recvCS *flnoise.CipherState, plaintext string) {
	t.Helper()
	ct, err := sendCS.Encrypt(nil, nil, []byte(plaintext))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	sendEnvelope(t, senderConn, 0x02, ct)

	envType, data := readEnvelope(t, receiverConn)
	if envType != 0x02 {
		t.Fatalf("expected transport envelope 0x02, got %#x", envType)
	}
	got, err := recvCS.Decrypt(nil, nil, data)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != plaintext {
		t.Errorf("round-trip mismatch: want %q, got %q", plaintext, got)
	}
}

// ── Negative cases ─────────────────────────────────────────────────────────────

func TestIntegration_BinaryFrameDroppedBeforePairing(t *testing.T) {
	ts := newTestServer(t, 65536)
	dev := registerDevice(t, ts, "solo", "Solo")
	conn := dial(t, ts, dev)
	readControl(t, conn) // welcome

	// Not paired with anyone: this must be silently discarded (§8), not routed
	// anywhere and not crash the connection.
	sendEnvelope(t, conn, 0x02, []byte("nobody should receive this"))

	// Prove the connection is still alive and the server is still reading frames:
	// pings still get answered.
	sendControl(t, conn, wireMsg{Type: "ping"})
	pong := readControl(t, conn)
	if pong.Type != "pong" {
		t.Fatalf("expected pong after dropped binary frame, got %+v", pong)
	}
}

func TestIntegration_OversizedFrameClosesConnection(t *testing.T) {
	ts := newTestServer(t, 128)
	dev := registerDevice(t, ts, "big-sender", "Big")
	conn := dial(t, ts, dev)
	readControl(t, conn) // welcome

	oversized := make([]byte, 1024)
	if err := conn.WriteMessage(websocket.BinaryMessage, oversized); err != nil {
		t.Fatalf("write oversized frame: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected the connection to be closed after an oversized frame")
	}
}

func TestIntegration_VerbatimRelay_RandomPayloads(t *testing.T) {
	ts := newTestServer(t, 65536)
	devA := registerDevice(t, ts, "va", "VA")
	devB := registerDevice(t, ts, "vb", "VB")

	connA := dial(t, ts, devA)
	readControl(t, connA) // welcome
	connB := dial(t, ts, devB)
	readControl(t, connB) // welcome

	// Pair in-band so both already-connected clients pick up Peer() immediately,
	// no reconnect needed (real static keys aren't relevant to this test).
	sendControl(t, connA, wireMsg{Type: "pair_request"})
	pairCode := readControl(t, connA)
	sendControl(t, connB, wireMsg{Type: "pair_accept", Code: pairCode.Code})
	readControl(t, connB) // B's pair_complete
	readControl(t, connA) // A's mirrored pair_complete

	for _, size := range []int{1, 1024, 65536 - 1} {
		payload := make([]byte, size)
		if _, err := rand.Read(payload); err != nil {
			t.Fatalf("rand: %v", err)
		}
		sendEnvelope(t, connA, 0x02, payload)
		envType, got := readEnvelope(t, connB)
		if envType != 0x02 {
			t.Fatalf("expected transport envelope 0x02, got %#x", envType)
		}
		if len(got) != len(payload) {
			t.Fatalf("size %d: length mismatch, want %d got %d", size, len(payload), len(got))
		}
		for i := range payload {
			if got[i] != payload[i] {
				t.Fatalf("size %d: byte %d mismatch: want %#x got %#x", size, i, payload[i], got[i])
			}
		}
	}
}
