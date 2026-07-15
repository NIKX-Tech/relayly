package relayly

import "time"

// Message is an incoming decrypted message from a paired peer.
type Message struct {
	// From is the device ID of the sender.
	From string

	// Payload is the decrypted plaintext message.
	Payload []byte

	// Timestamp is when this client received and decrypted the message. The E2E
	// transport envelope (docs/PROTOCOL.md §6) carries no timestamp of its own, so
	// unlike the pre-Protocol-v1 SDK this is a local receipt time, not a
	// server-assigned one.
	Timestamp time.Time
}
