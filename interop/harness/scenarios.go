package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const eventTimeout = 15 * time.Second

// maxMessageBytes mirrors config/relayly.yaml's websocket.max_message_bytes default.
// If that default ever changes, this constant needs updating too — it exists to
// exercise the exact wire-size boundary (§4), not an arbitrary large payload.
const maxMessageBytes = 65536

// noiseEnvelopeOverhead is the 1-byte envelope type prefix + 16-byte ChaChaPoly AEAD
// tag added to every transport ciphertext (docs/PROTOCOL.md §4, §6).
const noiseEnvelopeOverhead = 1 + 16

func fillBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func decodeB64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

// runPositiveFlow implements steps 1-5 of the task doc's scenario list for one SDK
// pair: register, pair, verify pinning, bidirectional roundtrip at three sizes,
// sever+reconnect+rekey, one more roundtrip. This is the actual interop assurance —
// it runs against all 10 pairs.
func runPositiveFlow(server *RunningServer, proxy *Proxy, aDef, bDef SDKDef, workDir string) error {
	devA, err := registerDevice(server.BaseURL, "a-"+aDef.Name+"-"+bDef.Name)
	if err != nil {
		return fmt.Errorf("register A: %w", err)
	}
	devB, err := registerDevice(server.BaseURL, "b-"+aDef.Name+"-"+bDef.Name)
	if err != nil {
		return fmt.Errorf("register B: %w", err)
	}

	aStore := filepath.Join(workDir, "a-peers.json")
	bStore := filepath.Join(workDir, "b-peers.json")

	aInst, err := StartInstance(aDef, proxy.URL(), devA.DeviceID, devA.DeviceToken, aStore)
	if err != nil {
		return fmt.Errorf("start A (%s): %w", aDef.Name, err)
	}
	defer aInst.Close()

	bInst, err := StartInstance(bDef, proxy.URL(), devB.DeviceID, devB.DeviceToken, bStore)
	if err != nil {
		return fmt.Errorf("start B (%s): %w", bDef.Name, err)
	}
	defer bInst.Close()

	// Step 2-3: pair, and confirm both sides settle on matching peer IDs.
	if err := aInst.Send(map[string]any{"cmd": "request_pair_code"}); err != nil {
		return fmt.Errorf("A request_pair_code: %w", err)
	}
	codeEv, err := aInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "pair_code" })
	if err != nil {
		return fmt.Errorf("A pair_code: %w", err)
	}
	code := codeEv.Str("code")
	if len(code) != 6 {
		return fmt.Errorf("A pair_code: expected 6-digit code, got %q", code)
	}

	if err := bInst.Send(map[string]any{"cmd": "accept_pair", "code": code}); err != nil {
		return fmt.Errorf("B accept_pair: %w", err)
	}
	bPaired, err := bInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "paired" })
	if err != nil {
		return fmt.Errorf("B paired: %w", err)
	}
	aPaired, err := aInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "paired" })
	if err != nil {
		return fmt.Errorf("A paired: %w", err)
	}
	if aPaired.Str("peer_id") != devB.DeviceID {
		return fmt.Errorf("A paired with %q, want %q", aPaired.Str("peer_id"), devB.DeviceID)
	}
	if bPaired.Str("peer_id") != devA.DeviceID {
		return fmt.Errorf("B paired with %q, want %q", bPaired.Str("peer_id"), devA.DeviceID)
	}

	// Pinned keys must match what pair_complete announced (both sides pin the
	// AUTHENTICATED handshake key, docs/PROTOCOL.md §7.1).
	if err := assertPinMatches(aStore, devB.DeviceID, aPaired.Str("peer_public_key_b64")); err != nil {
		return fmt.Errorf("A pin check: %w", err)
	}
	if err := assertPinMatches(bStore, devA.DeviceID, bPaired.Str("peer_public_key_b64")); err != nil {
		return fmt.Errorf("B pin check: %w", err)
	}

	// Consume the initial pairing's ready_signal on both sides now, not later: it
	// fires alongside "paired" and would otherwise sit in each Instance's event
	// backlog and get falsely matched by step 5's post-reconnect ready_signal wait
	// (WaitFor checks the backlog before reading new events) — a real race that let
	// step 5 proceed before the actual rekey had finished, found by running this
	// harness against itself.
	if _, err := aInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "ready_signal" && e.Str("peer_id") == devB.DeviceID
	}); err != nil {
		return fmt.Errorf("A did not see the initial ready_signal: %w", err)
	}
	if _, err := bInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "ready_signal" && e.Str("peer_id") == devA.DeviceID
	}); err != nil {
		return fmt.Errorf("B did not see the initial ready_signal: %w", err)
	}

	// Step 4: bidirectional roundtrip at three sizes.
	sizes := []int{1, 1024, maxMessageBytes - noiseEnvelopeOverhead}
	for _, n := range sizes {
		if err := roundtrip(aInst, bInst, devA.DeviceID, devB.DeviceID, n); err != nil {
			return fmt.Errorf("A->B roundtrip (%d bytes): %w", n, err)
		}
		if err := roundtrip(bInst, aInst, devB.DeviceID, devA.DeviceID, n); err != nil {
			return fmt.Errorf("B->A roundtrip (%d bytes): %w", n, err)
		}
	}

	// Step 5: sever B, expect A to see it go offline then online, then rekey, then
	// one more message each way.
	proxy.SeverConnection(devB.DeviceID)
	if _, err := aInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "peer_status" && e.Str("peer_id") == devB.DeviceID && !e.Bool("online")
	}); err != nil {
		return fmt.Errorf("A did not see B go offline: %w", err)
	}
	if _, err := aInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "peer_status" && e.Str("peer_id") == devB.DeviceID && e.Bool("online")
	}); err != nil {
		return fmt.Errorf("A did not see B come back online: %w", err)
	}
	// Whichever side has the lexicographically smaller device_id re-initiates
	// (docs/PROTOCOL.md §6); wait for a fresh ready_signal on both sides to know the
	// rekey completed before sending again.
	if _, err := aInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "ready_signal" && e.Str("peer_id") == devB.DeviceID
	}); err != nil {
		return fmt.Errorf("A did not see a post-reconnect ready_signal: %w", err)
	}
	if _, err := bInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "ready_signal" && e.Str("peer_id") == devA.DeviceID
	}); err != nil {
		return fmt.Errorf("B did not see a post-reconnect ready_signal: %w", err)
	}
	if err := roundtrip(aInst, bInst, devA.DeviceID, devB.DeviceID, 64); err != nil {
		return fmt.Errorf("post-reconnect A->B roundtrip: %w", err)
	}
	if err := roundtrip(bInst, aInst, devB.DeviceID, devA.DeviceID, 64); err != nil {
		return fmt.Errorf("post-reconnect B->A roundtrip: %w", err)
	}

	return nil
}

func roundtrip(sender, receiver *Instance, senderID, receiverID string, n int) error {
	payload := fillBytes(n)
	if err := sender.Send(map[string]any{"cmd": "send", "peer_id": receiverID, "payload_b64": b64(payload)}); err != nil {
		return err
	}
	ev, err := receiver.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "message" && e.Str("from") == senderID
	})
	if err != nil {
		return err
	}
	got, err := decodeB64(ev.Str("payload_b64"))
	if err != nil {
		return fmt.Errorf("decoding received payload: %w", err)
	}
	if len(got) != len(payload) {
		return fmt.Errorf("payload length mismatch: sent %d, got %d", len(payload), len(got))
	}
	for i := range payload {
		if got[i] != payload[i] {
			return fmt.Errorf("payload mismatch at byte %d", i)
		}
	}
	return nil
}

type pinFileEntry struct {
	StaticKey string `json:"static_key"`
	PinnedAt  string `json:"pinned_at"`
}

func readPinFile(path string) (map[string]pinFileEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]pinFileEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func assertPinMatches(storePath, peerID, wantB64 string) error {
	m, err := readPinFile(storePath)
	if err != nil {
		return fmt.Errorf("reading peer store %s: %w", storePath, err)
	}
	entry, ok := m[peerID]
	if !ok {
		return fmt.Errorf("no pin recorded for peer %s", peerID)
	}
	if entry.StaticKey != wantB64 {
		return fmt.Errorf("pinned key %q does not match announced key %q", entry.StaticKey, wantB64)
	}
	return nil
}

// runWrongPinTest implements scenario 6: pre-seed the responder's peer store with a
// wrong static key for the peer ID before it connects; the handshake must hard-fail
// and the original (wrong) pin must remain untouched.
func runWrongPinTest(server *RunningServer, proxy *Proxy, victimDef, partnerDef SDKDef, workDir string) error {
	devA, err := registerDevice(server.BaseURL, "wrongpin-a-"+partnerDef.Name)
	if err != nil {
		return err
	}
	devB, err := registerDevice(server.BaseURL, "wrongpin-b-"+victimDef.Name)
	if err != nil {
		return err
	}

	bStore := filepath.Join(workDir, "b-peers.json")
	wrongKey := b64(fillBytes(32))
	seed := map[string]pinFileEntry{devA.DeviceID: {StaticKey: wrongKey, PinnedAt: "2020-01-01T00:00:00Z"}}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(bStore, data, 0o600); err != nil {
		return err
	}

	aStore := filepath.Join(workDir, "a-peers.json")
	aInst, err := StartInstance(partnerDef, proxy.URL(), devA.DeviceID, devA.DeviceToken, aStore)
	if err != nil {
		return err
	}
	defer aInst.Close()
	bInst, err := StartInstance(victimDef, proxy.URL(), devB.DeviceID, devB.DeviceToken, bStore)
	if err != nil {
		return err
	}
	defer bInst.Close()

	if err := aInst.Send(map[string]any{"cmd": "request_pair_code"}); err != nil {
		return err
	}
	codeEv, err := aInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "pair_code" })
	if err != nil {
		return err
	}
	if err := bInst.Send(map[string]any{"cmd": "accept_pair", "code": codeEv.Str("code")}); err != nil {
		return err
	}

	if _, err := bInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "pair_error" }); err != nil {
		return fmt.Errorf("expected B (%s) to reject the mismatched pin, but it didn't: %w", victimDef.Name, err)
	}

	m, err := readPinFile(bStore)
	if err != nil {
		return fmt.Errorf("reading B's peer store after failed handshake: %w", err)
	}
	if m[devA.DeviceID].StaticKey != wrongKey {
		return fmt.Errorf("B's original (wrong) pin was overwritten — a mismatch must never auto-repin")
	}
	return nil
}

// runKeyRewriteTest implements scenario 7: rewrite the peer_static_key the server
// delivers to the victim in pair_complete, so it no longer matches what the
// handshake actually authenticates. The §7.2 cross-check must hard-fail — a
// different code path from scenario 6's client-side pin check.
func runKeyRewriteTest(server *RunningServer, proxy *Proxy, victimDef, partnerDef SDKDef, workDir string) error {
	devA, err := registerDevice(server.BaseURL, "rewrite-a-"+partnerDef.Name)
	if err != nil {
		return err
	}
	devB, err := registerDevice(server.BaseURL, "rewrite-b-"+victimDef.Name)
	if err != nil {
		return err
	}

	aStore := filepath.Join(workDir, "a-peers.json")
	bStore := filepath.Join(workDir, "b-peers.json")

	aInst, err := StartInstance(partnerDef, proxy.URL(), devA.DeviceID, devA.DeviceToken, aStore)
	if err != nil {
		return err
	}
	defer aInst.Close()
	bInst, err := StartInstance(victimDef, proxy.URL(), devB.DeviceID, devB.DeviceToken, bStore)
	if err != nil {
		return err
	}
	defer bInst.Close()

	proxy.RewriteNextPairComplete(devB.DeviceID, b64(fillBytes(32)))

	if err := aInst.Send(map[string]any{"cmd": "request_pair_code"}); err != nil {
		return err
	}
	codeEv, err := aInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "pair_code" })
	if err != nil {
		return err
	}
	if err := bInst.Send(map[string]any{"cmd": "accept_pair", "code": codeEv.Str("code")}); err != nil {
		return err
	}

	if _, err := bInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "pair_error" }); err != nil {
		return fmt.Errorf("expected B (%s) to reject the rewritten pair_complete key, but it didn't: %w", victimDef.Name, err)
	}
	return nil
}

// runRekeySafetyTest implements scenario 8: after a healthy pairing, sever the
// smaller-device-id side's connection so the OTHER side is the one that re-initiates
// a rekey toward the victim — an unsolicited msg1 arriving on an already-healthy
// session from the victim's point of view. The victim's existing session must keep
// working throughout, and a final roundtrip must succeed once the rekey completes.
func runRekeySafetyTest(server *RunningServer, proxy *Proxy, victimDef, partnerDef SDKDef, workDir string) error {
	// Per docs/PROTOCOL.md §6, whichever side's device_id is lexicographically
	// smaller re-initiates a rekey once it learns its peer is back online. To make
	// the victim the *receiver* of an unsolicited msg1 (the thing this scenario
	// actually tests) rather than the initiator, partner must end up with the
	// smaller ID. registerDevice IDs are server-assigned UUIDs, so retry
	// registration until the ordering comes out that way.
	var victim, partner deviceCreds
	const maxAttempts = 10
	for attempt := 0; ; attempt++ {
		v, err := registerDevice(server.BaseURL, fmt.Sprintf("rekey-victim-%s-%d", victimDef.Name, attempt))
		if err != nil {
			return err
		}
		p, err := registerDevice(server.BaseURL, fmt.Sprintf("rekey-partner-%s-%d", partnerDef.Name, attempt))
		if err != nil {
			return err
		}
		if p.DeviceID < v.DeviceID {
			victim, partner = v, p
			break
		}
		if attempt == maxAttempts-1 {
			return fmt.Errorf("could not obtain a partner device_id smaller than the victim's after %d attempts", maxAttempts)
		}
	}

	victimStore := filepath.Join(workDir, "victim-peers.json")
	partnerStore := filepath.Join(workDir, "partner-peers.json")

	victimInst, err := StartInstance(victimDef, proxy.URL(), victim.DeviceID, victim.DeviceToken, victimStore)
	if err != nil {
		return err
	}
	defer victimInst.Close()
	partnerInst, err := StartInstance(partnerDef, proxy.URL(), partner.DeviceID, partner.DeviceToken, partnerStore)
	if err != nil {
		return err
	}
	defer partnerInst.Close()

	if err := victimInst.Send(map[string]any{"cmd": "request_pair_code"}); err != nil {
		return err
	}
	codeEv, err := victimInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "pair_code" })
	if err != nil {
		return err
	}
	if err := partnerInst.Send(map[string]any{"cmd": "accept_pair", "code": codeEv.Str("code")}); err != nil {
		return err
	}
	if _, err := partnerInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "paired" }); err != nil {
		return err
	}
	if _, err := victimInst.WaitFor(eventTimeout, func(e Event) bool { return e.Type() == "paired" }); err != nil {
		return err
	}

	// Consume the initial pairing's ready_signal on both sides now — see the matching
	// comment in runPositiveFlow for why this must happen before the later
	// post-rekey ready_signal wait below, or that wait falsely matches the stale one.
	if _, err := victimInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "ready_signal" && e.Str("peer_id") == partner.DeviceID
	}); err != nil {
		return fmt.Errorf("victim did not see the initial ready_signal: %w", err)
	}
	if _, err := partnerInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "ready_signal" && e.Str("peer_id") == victim.DeviceID
	}); err != nil {
		return fmt.Errorf("partner did not see the initial ready_signal: %w", err)
	}

	// Sever the VICTIM's connection. Its own client reconnects automatically; the
	// server then notifies the (still-connected) partner that the victim is back
	// online, and since partner's device_id is smaller, partner proactively
	// re-initiates — an unsolicited msg1 from the victim's point of view, exercising
	// its make-before-break handling (docs/PROTOCOL.md §6) exactly as intended.
	proxy.SeverConnection(victim.DeviceID)

	if _, err := partnerInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "peer_status" && e.Str("peer_id") == victim.DeviceID && e.Bool("online")
	}); err != nil {
		return fmt.Errorf("partner never saw the victim come back online: %w", err)
	}
	if _, err := victimInst.WaitFor(eventTimeout, func(e Event) bool {
		return e.Type() == "ready_signal" && e.Str("peer_id") == partner.DeviceID
	}); err != nil {
		return fmt.Errorf("victim never saw a ready_signal for the completed rekey: %w", err)
	}

	if err := roundtrip(victimInst, partnerInst, victim.DeviceID, partner.DeviceID, 32); err != nil {
		return fmt.Errorf("post-rekey A->B roundtrip failed: %w", err)
	}
	if err := roundtrip(partnerInst, victimInst, partner.DeviceID, victim.DeviceID, 32); err != nil {
		return fmt.Errorf("post-rekey B->A roundtrip failed: %w", err)
	}
	return nil
}
