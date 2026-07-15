package relayly

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// runHandshake drives a full 3-message Noise XX handshake between an initiator and a
// responder noiseSession directly (no network), returning both once complete.
func runHandshake(t *testing.T, initKey, respKey PrivateKey) (init, resp *noiseSession) {
	t.Helper()

	initSession, msg1, err := newInitiatorSession(initKey)
	if err != nil {
		t.Fatalf("newInitiatorSession: %v", err)
	}
	respSession, err := newResponderSession(respKey)
	if err != nil {
		t.Fatalf("newResponderSession: %v", err)
	}

	msg2, done, err := respSession.handleHandshakeMessage(msg1)
	if err != nil || done {
		t.Fatalf("responder processing msg1: done=%v err=%v", done, err)
	}
	msg3, done, err := initSession.handleHandshakeMessage(msg2)
	if err != nil || !done {
		t.Fatalf("initiator processing msg2: done=%v err=%v", done, err)
	}
	_, done, err = respSession.handleHandshakeMessage(msg3)
	if err != nil || !done {
		t.Fatalf("responder processing msg3: done=%v err=%v", done, err)
	}

	if !initSession.ready() || !respSession.ready() {
		t.Fatal("both sessions should be ready after a full handshake")
	}
	return initSession, respSession
}

func TestNoiseHandshake_TransportRoundTrip(t *testing.T) {
	initKey, _ := GenerateKey()
	respKey, _ := GenerateKey()
	init, resp := runHandshake(t, initKey, respKey)

	// Initiator -> responder.
	ct, err := init.encrypt([]byte("hello from initiator"))
	if err != nil {
		t.Fatalf("initiator encrypt: %v", err)
	}
	pt, err := resp.decrypt(ct)
	if err != nil {
		t.Fatalf("responder decrypt: %v", err)
	}
	if string(pt) != "hello from initiator" {
		t.Errorf("got %q", pt)
	}

	// Responder -> initiator.
	ct, err = resp.encrypt([]byte("hello from responder"))
	if err != nil {
		t.Fatalf("responder encrypt: %v", err)
	}
	pt, err = init.decrypt(ct)
	if err != nil {
		t.Fatalf("initiator decrypt: %v", err)
	}
	if string(pt) != "hello from responder" {
		t.Errorf("got %q", pt)
	}
}

func TestNoiseHandshake_PeerStaticAuthenticated(t *testing.T) {
	initKey, _ := GenerateKey()
	respKey, _ := GenerateKey()
	initPub, _ := initKey.PublicKey()
	respPub, _ := respKey.PublicKey()

	init, resp := runHandshake(t, initKey, respKey)

	if !bytes.Equal(init.peerStatic, respPub.raw[:]) {
		t.Error("initiator did not authenticate the responder's real static key")
	}
	if !bytes.Equal(resp.peerStatic, initPub.raw[:]) {
		t.Error("responder did not authenticate the initiator's real static key")
	}
}

func TestNoiseSession_EncryptBeforeReady(t *testing.T) {
	key, _ := GenerateKey()
	session, _, err := newInitiatorSession(key)
	if err != nil {
		t.Fatalf("newInitiatorSession: %v", err)
	}
	if _, err := session.encrypt([]byte("too soon")); err != ErrNotReady {
		t.Errorf("want ErrNotReady, got %v", err)
	}
}

func TestNoiseSession_WaitReady(t *testing.T) {
	initKey, _ := GenerateKey()
	respKey, _ := GenerateKey()

	initSession, msg1, err := newInitiatorSession(initKey)
	if err != nil {
		t.Fatalf("newInitiatorSession: %v", err)
	}
	respSession, err := newResponderSession(respKey)
	if err != nil {
		t.Fatalf("newResponderSession: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		msg2, _, err := respSession.handleHandshakeMessage(msg1)
		if err != nil {
			t.Errorf("responder msg1: %v", err)
			return
		}
		msg3, _, err := initSession.handleHandshakeMessage(msg2)
		if err != nil {
			t.Errorf("initiator msg2: %v", err)
			return
		}
		if _, _, err := respSession.handleHandshakeMessage(msg3); err != nil {
			t.Errorf("responder msg3: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := initSession.waitReady(ctx); err != nil {
		t.Fatalf("waitReady: %v", err)
	}
	<-done
}

// TestPeerConn_MakeBeforeBreak simulates §6: an in-flight rekey attempt must not
// disturb the existing active session until it actually completes, and a message on
// the old session must keep round-tripping throughout.
func TestPeerConn_MakeBeforeBreak(t *testing.T) {
	aKey, _ := GenerateKey()
	bKey, _ := GenerateKey()

	// First-ever handshake: A initiates, B responds (mirrors §5.3's shape, role
	// choice here is arbitrary since this test only exercises the rekey mechanics).
	aSession, msg1, err := newInitiatorSession(aKey)
	if err != nil {
		t.Fatalf("newInitiatorSession: %v", err)
	}
	bPeer := &peerConn{id: "device-a"}
	// Drive B's side through the peerConn wrapper, exactly as client.go would for an
	// incoming handshake envelope.
	reply, completed, _, err := bPeer.handleHandshakeEnvelope(bKey, msg1)
	if err != nil || completed != nil {
		t.Fatalf("B processing msg1: completed=%v err=%v", completed, err)
	}
	msg3, done, err := aSession.handleHandshakeMessage(reply)
	if err != nil || !done {
		t.Fatalf("A processing msg2: done=%v err=%v", done, err)
	}
	_, completed, _, err = bPeer.handleHandshakeEnvelope(bKey, msg3)
	if err != nil || completed == nil {
		t.Fatalf("B processing msg3: completed=%v err=%v", completed, err)
	}
	bPeer.promote(completed) // first-ever handshake: promote is what client.go calls

	// The original session (A's side) must still be usable.
	ct, err := aSession.encrypt([]byte("still using the old session"))
	if err != nil {
		t.Fatalf("A encrypt on original session: %v", err)
	}
	pt, err := bPeer.recv(ct)
	if err != nil {
		t.Fatalf("B decrypt on original session: %v", err)
	}
	if string(pt) != "still using the old session" {
		t.Errorf("got %q", pt)
	}

	// Now inject an unsolicited rekey attempt (B is the responder again) that never
	// completes (we stop after msg2) — the original session must keep working.
	aRekeySession, rekeyMsg1, err := newInitiatorSession(aKey)
	if err != nil {
		t.Fatalf("newInitiatorSession (rekey): %v", err)
	}
	_, completed, wasPending, err := bPeer.handleHandshakeEnvelope(bKey, rekeyMsg1)
	if err != nil || completed != nil || !wasPending {
		t.Fatalf("B processing rekey msg1: completed=%v wasPending=%v err=%v", completed, wasPending, err)
	}
	_ = aRekeySession // deliberately never finished

	ct, err = aSession.encrypt([]byte("original session still alive mid-rekey"))
	if err != nil {
		t.Fatalf("A encrypt during in-flight rekey: %v", err)
	}
	pt, err = bPeer.recv(ct)
	if err != nil {
		t.Fatalf("B decrypt during in-flight rekey: %v", err)
	}
	if string(pt) != "original session still alive mid-rekey" {
		t.Errorf("got %q", pt)
	}
}

// TestPeerConn_UnsolicitedMsg1RateLimited covers the realistic case the rate limit
// guards: repeated unsolicited handshake attempts in quick succession once no attempt
// is currently in flight (e.g. a prior one already failed/was abandoned) — not two
// concurrently in-flight attempts, which flynn/noise itself already rejects as
// malformed continuation data (a fresh msg1 is a different shape than the msg3 an
// in-flight responder session is expecting next).
func TestPeerConn_UnsolicitedMsg1RateLimited(t *testing.T) {
	aKey, _ := GenerateKey()
	bKey, _ := GenerateKey()

	aSession, msg1, err := newInitiatorSession(aKey)
	if err != nil {
		t.Fatalf("newInitiatorSession: %v", err)
	}
	bPeer := &peerConn{id: "device-a"}
	reply, _, _, err := bPeer.handleHandshakeEnvelope(bKey, msg1)
	if err != nil {
		t.Fatalf("B processing msg1: %v", err)
	}
	msg3, done, err := aSession.handleHandshakeMessage(reply)
	if err != nil || !done {
		t.Fatalf("A processing msg2: %v", err)
	}
	_, completed, _, err := bPeer.handleHandshakeEnvelope(bKey, msg3)
	if err != nil || completed == nil {
		t.Fatalf("B processing msg3: %v", err)
	}
	bPeer.promote(completed)

	// First unsolicited attempt: accepted, then abandoned (as client.go would after
	// it times out or fails) so `pending` is nil again for the next one.
	_, rekeyMsg1a, _ := newInitiatorSession(aKey)
	_, _, wasPending, err := bPeer.handleHandshakeEnvelope(bKey, rekeyMsg1a)
	if err != nil || !wasPending {
		t.Fatalf("first unsolicited msg1 should be accepted: wasPending=%v err=%v", wasPending, err)
	}
	bPeer.mu.Lock()
	firstPending := bPeer.pending
	bPeer.mu.Unlock()
	bPeer.abandon(firstPending)

	// A second unsolicited attempt arriving immediately after must be dropped.
	_, rekeyMsg1b, _ := newInitiatorSession(aKey)
	reply2, completed2, wasPending2, err := bPeer.handleHandshakeEnvelope(bKey, rekeyMsg1b)
	if err != nil {
		t.Fatalf("second unsolicited msg1: %v", err)
	}
	if reply2 != nil || completed2 != nil || wasPending2 {
		t.Error("second unsolicited msg1 within the rate-limit window should be dropped silently")
	}

	// The original session must still be unaffected throughout.
	ct, err := aSession.encrypt([]byte("still alive"))
	if err != nil {
		t.Fatalf("A encrypt: %v", err)
	}
	if pt, err := bPeer.recv(ct); err != nil || string(pt) != "still alive" {
		t.Errorf("original session broken: pt=%q err=%v", pt, err)
	}
}
