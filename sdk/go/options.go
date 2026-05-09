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

	// PrivateKey is the device's X25519 private key used for end-to-end encryption.
	// Generate one with GenerateKey() or LoadOrGenerateKey(). Required.
	PrivateKey PrivateKey

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
}

func (o Options) validate() error {
	if o.DeviceID == "" {
		return fmt.Errorf("DeviceID is required")
	}
	return nil
}
