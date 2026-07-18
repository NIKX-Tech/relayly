package relayly

import (
	"fmt"
	"time"
)

// Options configures a Relayly client connection.
// Options is passed by value to Connect() and must not contain mutexes.
type Options struct {
	// DeviceID is a unique identifier for this device. It is registered with the
	// server and used to address messages. Required.
	DeviceID string

	// DeviceToken authenticates this device to the relay (docs/PROTOCOL.md §2, §3).
	// Obtain one from POST /api/v1/devices (or the relayly CLI's `pair` command).
	// Required.
	DeviceToken string

	// PrivateKey is the device's X25519 static identity key, used as the Noise XX
	// static keypair for device-to-device end-to-end encryption. Generate one with
	// GenerateKey() or LoadOrGenerateKey(). Required.
	PrivateKey PrivateKey

	// PeerStorePath is where pinned peer static keys (docs/PROTOCOL.md §7.1) are
	// persisted. Defaults to DefaultPeerStorePath ("~/.relayly/peers.json") if empty.
	PeerStorePath string

	// PingInterval is how often the client sends keepalive pings.
	// Defaults to DefaultPingInterval (30 seconds).
	PingInterval time.Duration

	// ReconnectDelay is the initial delay before reconnect attempts.
	// Set to -1 to disable automatic reconnection.
	// Defaults to DefaultReconnectDelay (1 second).
	ReconnectDelay time.Duration

	// MaxReconnectDelay caps the exponential backoff.
	// Defaults to DefaultMaxReconnectDelay (60 seconds).
	MaxReconnectDelay time.Duration

	// OnReconnect is called each time the client successfully reconnects.
	// Optional.
	OnReconnect func()

	// OnDisconnect is called when the connection is lost (before reconnect attempts).
	// Optional.
	OnDisconnect func(err error)

	// OnReady is called whenever a peer's Noise session becomes usable for Send —
	// both after the very first pairing and after any later re-handshake following a
	// reconnect (docs/PROTOCOL.md §6). Optional; RequestPairCode/AcceptPair/
	// PairCode.Wait already block until the first handshake completes, so this is
	// mainly useful for noticing when a peer recovers after a reconnect.
	OnReady func(peerID string)

	// OnPeerStatus is called whenever the server reports the paired peer's
	// online/offline transition (docs/PROTOCOL.md §5.1's peer_status). Optional.
	OnPeerStatus func(peerID string, online bool)
}

func (o Options) validate() error {
	if o.DeviceID == "" {
		return fmt.Errorf("DeviceID is required")
	}
	if o.DeviceToken == "" {
		return fmt.Errorf("DeviceToken is required")
	}
	return nil
}

func (o Options) peerStorePath() string {
	if o.PeerStorePath != "" {
		return o.PeerStorePath
	}
	return DefaultPeerStorePath
}
