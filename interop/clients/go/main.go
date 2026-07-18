// Command shim is a thin CLI wrapper around sdk/go's public API, driven by
// newline-delimited JSON over stdin/stdout. It exists only for the interop harness
// (interop/harness/) to drive real SDK instances as subprocesses — it uses no
// internal/test-only hooks, proving the public API alone is enough for interop
// testing.
//
// Commands (stdin, one JSON object per line):
//
//	{"cmd":"request_pair_code"}
//	{"cmd":"accept_pair","code":"123456"}
//	{"cmd":"send","peer_id":"...","payload_b64":"..."}
//	{"cmd":"close"}
//
// Events (stdout, one JSON object per line):
//
//	{"event":"ready"}
//	{"event":"connect_error","message":"..."}
//	{"event":"pair_code","code":"...","expires_in":300}
//	{"event":"paired","peer_id":"...","peer_public_key_b64":"..."}
//	{"event":"pair_error","message":"..."}
//	{"event":"sent"}
//	{"event":"send_error","message":"..."}
//	{"event":"message","from":"...","payload_b64":"..."}
//	{"event":"peer_status","peer_id":"...","online":true}
//	{"event":"ready_signal","peer_id":"..."}
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"

	relayly "github.com/NIKX-Tech/relayly/sdk/go"
)

var (
	stdout   = bufio.NewWriter(os.Stdout)
	emitLock sync.Mutex
)

func emit(v map[string]any) {
	emitLock.Lock()
	defer emitLock.Unlock()
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	stdout.Write(data)
	stdout.WriteByte('\n')
	stdout.Flush()
}

func main() {
	server := flag.String("server", "", "relay server WebSocket URL")
	deviceID := flag.String("device-id", "", "device ID")
	deviceToken := flag.String("device-token", "", "device token")
	peerStorePath := flag.String("peer-store-path", "", "peer store path (optional)")
	flag.Parse()

	key, err := relayly.GenerateKey()
	if err != nil {
		emit(map[string]any{"event": "connect_error", "message": err.Error()})
		os.Exit(1)
	}

	opts := relayly.Options{
		DeviceID:    *deviceID,
		DeviceToken: *deviceToken,
		PrivateKey:  key,
		OnReady: func(peerID string) {
			emit(map[string]any{"event": "ready_signal", "peer_id": peerID})
		},
		OnPeerStatus: func(peerID string, online bool) {
			emit(map[string]any{"event": "peer_status", "peer_id": peerID, "online": online})
		},
	}
	if *peerStorePath != "" {
		opts.PeerStorePath = *peerStorePath
	}

	ctx := context.Background()
	client, err := relayly.Connect(ctx, *server, opts)
	if err != nil {
		emit(map[string]any{"event": "connect_error", "message": err.Error()})
		os.Exit(1)
	}
	defer client.Close()

	go func() {
		for msg := range client.Messages() {
			emit(map[string]any{
				"event":       "message",
				"from":        msg.From,
				"payload_b64": base64.StdEncoding.EncodeToString(msg.Payload),
			})
		}
	}()

	emit(map[string]any{"event": "ready"})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var cmd map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &cmd); err != nil {
			continue
		}
		handleCommand(ctx, client, cmd)
		if cmd["cmd"] == "close" {
			return
		}
	}
}

func handleCommand(ctx context.Context, client *relayly.Client, cmd map[string]any) {
	switch cmd["cmd"] {
	case "request_pair_code":
		go func() {
			code, err := client.RequestPairCode(ctx)
			if err != nil {
				emit(map[string]any{"event": "pair_error", "message": err.Error()})
				return
			}
			emit(map[string]any{"event": "pair_code", "code": code.Short, "expires_in": code.ExpiresIn})
			peer, err := code.Wait(ctx)
			if err != nil {
				emit(map[string]any{"event": "pair_error", "message": err.Error()})
				return
			}
			emit(map[string]any{
				"event":               "paired",
				"peer_id":             peer.ID,
				"peer_public_key_b64": peer.PublicKey.Base64(),
			})
		}()

	case "accept_pair":
		code, _ := cmd["code"].(string)
		go func() {
			peer, err := client.AcceptPair(ctx, code)
			if err != nil {
				emit(map[string]any{"event": "pair_error", "message": err.Error()})
				return
			}
			emit(map[string]any{
				"event":               "paired",
				"peer_id":             peer.ID,
				"peer_public_key_b64": peer.PublicKey.Base64(),
			})
		}()

	case "send":
		peerID, _ := cmd["peer_id"].(string)
		payloadB64, _ := cmd["payload_b64"].(string)
		go func() {
			payload, err := base64.StdEncoding.DecodeString(payloadB64)
			if err != nil {
				emit(map[string]any{"event": "send_error", "message": err.Error()})
				return
			}
			if err := client.Send(ctx, peerID, payload); err != nil {
				emit(map[string]any{"event": "send_error", "message": err.Error()})
				return
			}
			emit(map[string]any{"event": "sent"})
		}()

	case "close":
		client.Close()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %v\n", cmd["cmd"])
	}
}
