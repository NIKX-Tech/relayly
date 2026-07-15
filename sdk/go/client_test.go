package relayly_test

// Self-pair integration test: builds and runs the real cmd/relayly server binary
// (not an in-process test double) and drives two sdk/go Clients through it end to
// end — register, connect, pair, a real Noise XX handshake, and bidirectional
// encrypted delivery. This is the "each SDK against itself" leg of the interop matrix
// (docs/tasks/02-sdks-and-interop.md), landed early here since it directly de-risks
// this PR: PR 2's own migration bug was only ever caught by running the real binary,
// never by unit tests against a fake.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	relayly "github.com/NIKX-Tech/relayly/sdk/go"
)

func buildRelayServer(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "cmd", "relayly")); err != nil {
		t.Fatalf("cmd/relayly not found at %s (expected sdk/go to be two levels under the repo root): %v", repoRoot, err)
	}

	binPath := filepath.Join(t.TempDir(), "relayly-server")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/relayly")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building relay server: %v\n%s", err, out)
	}
	return binPath
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startRelayServer(t *testing.T, binPath string) string {
	t.Helper()

	port := freePort(t)
	dbPath := filepath.Join(t.TempDir(), "relayly.db")

	cmd := exec.Command(binPath, "start",
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
		"--db.path", dbPath,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting relay server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return baseURL
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("relay server did not become healthy in time")
	return ""
}

type createDeviceResponse struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

func registerDevice(t *testing.T, baseURL, name string) createDeviceResponse {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(`{"name":%q}`, name))
	resp, err := http.Post(baseURL+"/api/v1/devices", "application/json", body)
	if err != nil {
		t.Fatalf("registering device %s: %v", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registering device %s: status %d", name, resp.StatusCode)
	}
	var out createDeviceResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding device response: %v", err)
	}
	return out
}

func TestSelfPair_PairAndExchange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping self-pair integration test in -short mode")
	}

	binPath := buildRelayServer(t)
	baseURL := startRelayServer(t, binPath)
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) + "/ws"

	devA := registerDevice(t, baseURL, "device-a")
	devB := registerDevice(t, baseURL, "device-b")

	keyA, err := relayly.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey A: %v", err)
	}
	keyB, err := relayly.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey B: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	clientA, err := relayly.Connect(ctx, wsURL, relayly.Options{
		DeviceID:      devA.DeviceID,
		DeviceToken:   devA.DeviceToken,
		PrivateKey:    keyA,
		PeerStorePath: filepath.Join(t.TempDir(), "a-peers.json"),
	})
	if err != nil {
		t.Fatalf("connect A: %v", err)
	}
	defer clientA.Close()

	clientB, err := relayly.Connect(ctx, wsURL, relayly.Options{
		DeviceID:      devB.DeviceID,
		DeviceToken:   devB.DeviceToken,
		PrivateKey:    keyB,
		PeerStorePath: filepath.Join(t.TempDir(), "b-peers.json"),
	})
	if err != nil {
		t.Fatalf("connect B: %v", err)
	}
	defer clientB.Close()

	code, err := clientA.RequestPairCode(ctx)
	if err != nil {
		t.Fatalf("RequestPairCode: %v", err)
	}

	var peerA *relayly.Peer
	var waitErr error
	pairDone := make(chan struct{})
	go func() {
		defer close(pairDone)
		peerA, waitErr = code.Wait(ctx)
	}()

	// AcceptPair blocks until the Noise handshake (and §7 pin/cross-check) actually
	// completes, so peerB is immediately usable with Send.
	peerB, err := clientB.AcceptPair(ctx, code.Short)
	if err != nil {
		t.Fatalf("AcceptPair: %v", err)
	}
	<-pairDone
	if waitErr != nil {
		t.Fatalf("A's code.Wait: %v", waitErr)
	}
	if peerA == nil || peerA.ID != devB.DeviceID {
		t.Fatalf("unexpected peerA: %+v", peerA)
	}
	if peerB.ID != devA.DeviceID {
		t.Fatalf("unexpected peerB: %+v", peerB)
	}

	if err := clientA.Send(ctx, peerA.ID, []byte("hello from A")); err != nil {
		t.Fatalf("A send: %v", err)
	}
	select {
	case msg := <-clientB.Messages():
		if string(msg.Payload) != "hello from A" {
			t.Errorf("B got %q", msg.Payload)
		}
		if msg.From != devA.DeviceID {
			t.Errorf("B's message.From: want %s, got %s", devA.DeviceID, msg.From)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("B did not receive A's message")
	}

	if err := clientB.Send(ctx, peerB.ID, []byte("hello from B")); err != nil {
		t.Fatalf("B send: %v", err)
	}
	select {
	case msg := <-clientA.Messages():
		if string(msg.Payload) != "hello from B" {
			t.Errorf("A got %q", msg.Payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("A did not receive B's message")
	}
}
