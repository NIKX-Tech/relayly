package relayly

import "time"

// Wire message type constants — match the Relayly server protocol.
const (
	msgTypeAuth         = "auth"
	msgTypeAuthOK       = "auth_ok"
	msgTypePairRequest  = "pair_request"
	msgTypePairCode     = "pair_code"
	msgTypePairAccept   = "pair_accept"
	msgTypePairComplete = "pair_complete"
	msgTypeSend         = "send"
	msgTypeMessage      = "message"
	msgTypePing         = "ping"
	msgTypePong         = "pong"
	msgTypeError        = "error"
)

// wireMessage is the JSON-encoded frame exchanged with the Relayly server.
// Fields are selectively populated depending on the message type.
type wireMessage struct {
	// Common
	Type string `json:"type"`

	// auth
	DeviceID  string `json:"device_id,omitempty"`
	PublicKey string `json:"public_key,omitempty"`

	// auth_ok
	SessionID string `json:"session_id,omitempty"`

	// pair_code, pair_accept, pair_complete — the 6-digit code
	Code      string `json:"code,omitempty"`
	ExpiresIn int    `json:"expires_in,omitempty"`

	// pair_complete
	PeerID        string `json:"peer_id,omitempty"`
	PeerPublicKey string `json:"peer_public_key,omitempty"`

	// send / message
	To        string    `json:"to,omitempty"`
	From      string    `json:"from,omitempty"`
	Payload   string    `json:"payload,omitempty"`  // base64-encoded ciphertext
	Nonce     string    `json:"nonce,omitempty"`    // base64-encoded 24-byte nonce
	Timestamp time.Time `json:"timestamp,omitempty"`

	// error — Code holds error_code when type == "error"
	Message string `json:"message,omitempty"`
}

