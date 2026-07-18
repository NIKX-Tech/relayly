package relay

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/pairing"
	"go.uber.org/zap"
)

func newTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustCreateDevice(t *testing.T, db *database.DB, id, name string) {
	t.Helper()
	d, err := pairing.NewDevice(name)
	if err != nil {
		t.Fatalf("NewDevice: %v", err)
	}
	d.ID = id
	if err := db.CreateDevice(d); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
}

// newTestClient builds a Client with no real *websocket.Conn. This is safe for
// control.go's handlers, they only ever touch c.db, c.hub, c.log, and c.send; none of
// them read c.conn (that's readPump/writePump/close's job, not exercised here).
func newTestClient(deviceID string, db *database.DB, hub *Hub) *Client {
	return &Client{
		DeviceID:        deviceID,
		hub:             hub,
		db:              db,
		send:            make(chan wsFrame, 16),
		log:             zap.NewNop(),
		maxMessageBytes: 65536,
		pingInterval:    time.Minute,
		deadline:        time.Minute,
	}
}

func newTestHub() *Hub {
	return &Hub{
		clients:   make(map[string]*Client),
		pairCodes: newPairCodeRegistry(),
		log:       zap.NewNop(),
	}
}

// recvControl drains one control frame from c.send and decodes it. Fails the test if
// nothing arrives within the timeout.
func recvControl(t *testing.T, c *Client) (controlMessage, wsFrame) {
	t.Helper()
	select {
	case frame := <-c.send:
		var msg controlMessage
		if err := json.Unmarshal(frame.data, &msg); err != nil {
			t.Fatalf("decode control frame %q: %v", frame.data, err)
		}
		return msg, frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a control frame")
		return controlMessage{}, wsFrame{}
	}
}

// assertNoControl fails the test if a frame was actually enqueued.
func assertNoControl(t *testing.T, c *Client) {
	t.Helper()
	select {
	case frame := <-c.send:
		t.Fatalf("expected no control frame, got %q", frame.data)
	default:
	}
}

// ── ping / unknown / malformed ────────────────────────────────────────────────

func TestHandleControl_Ping(t *testing.T) {
	c := newTestClient("dev-a", newTestDB(t), newTestHub())
	c.handleControl([]byte(`{"type":"ping"}`))
	msg, _ := recvControl(t, c)
	if msg.Type != "pong" {
		t.Errorf("want pong, got %q", msg.Type)
	}
}

func TestHandleControl_UnknownTypeIgnored(t *testing.T) {
	c := newTestClient("dev-a", newTestDB(t), newTestHub())
	c.handleControl([]byte(`{"type":"something_from_the_future","field":"value"}`))
	assertNoControl(t, c)
}

func TestHandleControl_MalformedJSON(t *testing.T) {
	c := newTestClient("dev-a", newTestDB(t), newTestHub())
	c.handleControl([]byte(`not json at all`))
	msg, _ := recvControl(t, c)
	if msg.Type != "error" || msg.Code != "malformed" {
		t.Errorf("want error/malformed, got %+v", msg)
	}
}

// ── announce_key (§7.2 key locking) ───────────────────────────────────────────

func TestHandleAnnounceKey_FirstAnnouncementPersists(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-a", "A")
	c := newTestClient("dev-a", db, newTestHub())

	c.handleControl([]byte(`{"type":"announce_key","static_key":"key-a"}`))
	assertNoControl(t, c) // no ack on success

	got, err := db.GetDeviceByID("dev-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.StaticKey != "key-a" {
		t.Errorf("StaticKey: want key-a, got %q", got.StaticKey)
	}
}

func TestHandleAnnounceKey_MismatchClosesWithError(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-a", "A")
	c := newTestClient("dev-a", db, newTestHub())

	c.handleControl([]byte(`{"type":"announce_key","static_key":"key-a"}`))
	assertNoControl(t, c)

	c.handleControl([]byte(`{"type":"announce_key","static_key":"key-b"}`))
	msg, frame := recvControl(t, c)
	if msg.Type != "error" || msg.Code != "key_mismatch" {
		t.Errorf("want error/key_mismatch, got %+v", msg)
	}
	if !frame.closeAfter {
		t.Error("key_mismatch must close the connection (§7.2)")
	}
}

func TestHandleAnnounceKey_MissingStaticKey(t *testing.T) {
	c := newTestClient("dev-a", newTestDB(t), newTestHub())
	c.handleControl([]byte(`{"type":"announce_key"}`))
	msg, frame := recvControl(t, c)
	if msg.Type != "error" || msg.Code != "malformed" {
		t.Errorf("want error/malformed, got %+v", msg)
	}
	if frame.closeAfter {
		t.Error("malformed announce_key should not close the connection")
	}
}

// ── pair_request / pair_code ───────────────────────────────────────────────────

func TestHandlePairRequest_GeneratesCode(t *testing.T) {
	hub := newTestHub()
	c := newTestClient("dev-a", newTestDB(t), hub)

	c.handlePairRequest()
	msg, _ := recvControl(t, c)
	if msg.Type != "pair_code" {
		t.Fatalf("want pair_code, got %q", msg.Type)
	}
	if len(msg.Code) != 6 {
		t.Errorf("want a 6-digit code, got %q", msg.Code)
	}
	if msg.ExpiresIn != int(pairing.PairingCodeTTL.Seconds()) {
		t.Errorf("want expires_in %d, got %d", int(pairing.PairingCodeTTL.Seconds()), msg.ExpiresIn)
	}

	requesterID, expired, ok := hub.pairCodes.Take(msg.Code)
	if !ok || expired || requesterID != "dev-a" {
		t.Errorf("code not registered correctly: requester=%q expired=%v ok=%v", requesterID, expired, ok)
	}
}

// ── pair_accept / pair_complete ─────────────────────────────────────────────────

func TestHandlePairAccept_Success(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-a", "A")
	mustCreateDevice(t, db, "dev-b", "B")
	if err := db.SetStaticKeyIfUnset("dev-a", "key-a"); err != nil {
		t.Fatalf("announce a: %v", err)
	}
	if err := db.SetStaticKeyIfUnset("dev-b", "key-b"); err != nil {
		t.Fatalf("announce b: %v", err)
	}

	hub := newTestHub()
	clientA := newTestClient("dev-a", db, hub)
	clientB := newTestClient("dev-b", db, hub)
	hub.clients["dev-a"] = clientA
	hub.clients["dev-b"] = clientB

	hub.pairCodes.Put("111222", "dev-a", pairing.PairingCodeTTL)

	clientB.handleControl([]byte(`{"type":"pair_accept","code":"111222"}`))

	// B gets its own pair_complete synchronously.
	msgB, _ := recvControl(t, clientB)
	if msgB.Type != "pair_complete" || msgB.PeerID != "dev-a" || msgB.PeerStaticKey != "key-a" {
		t.Errorf("B's pair_complete: %+v", msgB)
	}
	// A, reached via the Hub, gets the mirror.
	msgA, _ := recvControl(t, clientA)
	if msgA.Type != "pair_complete" || msgA.PeerID != "dev-b" || msgA.PeerStaticKey != "key-b" {
		t.Errorf("A's pair_complete: %+v", msgA)
	}

	if peer, ok := clientA.Peer(); !ok || peer != "dev-b" {
		t.Errorf("A.Peer(): want dev-b, got %q (ok=%v)", peer, ok)
	}
	if peer, ok := clientB.Peer(); !ok || peer != "dev-a" {
		t.Errorf("B.Peer(): want dev-a, got %q (ok=%v)", peer, ok)
	}

	a, _ := db.GetDeviceByID("dev-a")
	b, _ := db.GetDeviceByID("dev-b")
	if a.PairedWith == nil || *a.PairedWith != "dev-b" {
		t.Errorf("db: A.PairedWith want dev-b, got %v", a.PairedWith)
	}
	if b.PairedWith == nil || *b.PairedWith != "dev-a" {
		t.Errorf("db: B.PairedWith want dev-a, got %v", b.PairedWith)
	}
}

func TestHandlePairAccept_InvalidCode(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-b", "B")
	c := newTestClient("dev-b", db, newTestHub())

	c.handleControl([]byte(`{"type":"pair_accept","code":"000000"}`))
	msg, _ := recvControl(t, c)
	if msg.Type != "error" || msg.Code != "invalid_code" {
		t.Errorf("want error/invalid_code, got %+v", msg)
	}
}

func TestHandlePairAccept_CodeExpired(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-a", "A")
	mustCreateDevice(t, db, "dev-b", "B")

	hub := newTestHub()
	hub.pairCodes.Put("333444", "dev-a", -time.Second) // already expired
	c := newTestClient("dev-b", db, hub)

	c.handleControl([]byte(`{"type":"pair_accept","code":"333444"}`))
	msg, _ := recvControl(t, c)
	if msg.Type != "error" || msg.Code != "code_expired" {
		t.Errorf("want error/code_expired, got %+v", msg)
	}
}

func TestHandlePairAccept_CodeIsSingleUse(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-a", "A")
	mustCreateDevice(t, db, "dev-b", "B")
	mustCreateDevice(t, db, "dev-c", "C")

	hub := newTestHub()
	hub.clients["dev-a"] = newTestClient("dev-a", db, hub)
	hub.pairCodes.Put("555666", "dev-a", pairing.PairingCodeTTL)

	cB := newTestClient("dev-b", db, hub)
	cB.handleControl([]byte(`{"type":"pair_accept","code":"555666"}`))
	recvControl(t, cB)                   // B's own pair_complete
	recvControl(t, hub.clients["dev-a"]) // A's mirrored pair_complete

	cC := newTestClient("dev-c", db, hub)
	cC.handleControl([]byte(`{"type":"pair_accept","code":"555666"}`))
	msg, _ := recvControl(t, cC)
	if msg.Type != "error" || msg.Code != "invalid_code" {
		t.Errorf("reusing a redeemed code should fail invalid_code, got %+v", msg)
	}
}

func TestHandlePairAccept_SelfPairRejected(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-a", "A")

	hub := newTestHub()
	hub.pairCodes.Put("777888", "dev-a", pairing.PairingCodeTTL)
	c := newTestClient("dev-a", db, hub)

	c.handleControl([]byte(`{"type":"pair_accept","code":"777888"}`))
	msg, _ := recvControl(t, c)
	if msg.Type != "error" || msg.Code != "invalid_code" {
		t.Errorf("want error/invalid_code, got %+v", msg)
	}
}

func TestHandlePairAccept_AlreadyPaired(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-a", "A")
	mustCreateDevice(t, db, "dev-b", "B")
	mustCreateDevice(t, db, "dev-c", "C")
	if err := db.PairDevices("dev-a", "dev-b"); err != nil {
		t.Fatalf("pre-pair a+b: %v", err)
	}

	hub := newTestHub()
	hub.pairCodes.Put("999000", "dev-a", pairing.PairingCodeTTL)
	c := newTestClient("dev-c", db, hub)

	c.handleControl([]byte(`{"type":"pair_accept","code":"999000"}`))
	msg, _ := recvControl(t, c)
	if msg.Type != "error" || msg.Code != "already_paired" {
		t.Errorf("want error/already_paired, got %+v", msg)
	}
}

func TestHandlePairAccept_MissingCode(t *testing.T) {
	c := newTestClient("dev-b", newTestDB(t), newTestHub())
	c.handleControl([]byte(`{"type":"pair_accept"}`))
	msg, _ := recvControl(t, c)
	if msg.Type != "error" || msg.Code != "malformed" {
		t.Errorf("want error/malformed, got %+v", msg)
	}
}

// ── welcome ────────────────────────────────────────────────────────────────────

func TestSendWelcome_Unpaired(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-a", "A")
	c := newTestClient("dev-a", db, newTestHub())

	device, err := db.GetDeviceByID("dev-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	c.sendWelcome(device)

	msg, _ := recvControl(t, c)
	if msg.Type != "welcome" || msg.ProtocolVersion != ProtocolVersion || msg.DeviceID != "dev-a" {
		t.Errorf("unexpected welcome: %+v", msg)
	}
	if len(msg.Peers) != 0 {
		t.Errorf("unpaired device should have no peers, got %+v", msg.Peers)
	}
	assertNoControl(t, c) // no peer_status when there's no peer yet
}

func TestSendWelcome_PairedAndPeerOnline(t *testing.T) {
	db := newTestDB(t)
	mustCreateDevice(t, db, "dev-a", "A")
	mustCreateDevice(t, db, "dev-b", "B")
	if err := db.SetStaticKeyIfUnset("dev-b", "key-b"); err != nil {
		t.Fatalf("announce b: %v", err)
	}
	if err := db.PairDevices("dev-a", "dev-b"); err != nil {
		t.Fatalf("pair: %v", err)
	}

	hub := newTestHub()
	hub.clients["dev-b"] = newTestClient("dev-b", db, hub) // B is "online"
	c := newTestClient("dev-a", db, hub)

	device, err := db.GetDeviceByID("dev-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	c.sendWelcome(device)

	welcome, _ := recvControl(t, c)
	if len(welcome.Peers) != 1 || welcome.Peers[0].ID != "dev-b" || welcome.Peers[0].StaticKey != "key-b" {
		t.Errorf("unexpected peers in welcome: %+v", welcome.Peers)
	}

	status, _ := recvControl(t, c)
	if status.Type != "peer_status" || status.PeerID != "dev-b" || status.Online == nil || !*status.Online {
		t.Errorf("unexpected peer_status: %+v", status)
	}
}
