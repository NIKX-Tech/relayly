package relayly

// Wire control-message type constants (docs/PROTOCOL.md §5). The old "auth"/"send"
// JSON types and their payload/nonce fields are gone: auth is now HTTP-layer query
// params, and encrypted data travels as a binary E2E envelope, never JSON.
const (
	msgTypeWelcome      = "welcome"
	msgTypeAnnounceKey  = "announce_key"
	msgTypePairRequest  = "pair_request"
	msgTypePairCode     = "pair_code"
	msgTypePairAccept   = "pair_accept"
	msgTypePairComplete = "pair_complete"
	msgTypePeerStatus   = "peer_status"
	msgTypePing         = "ping"
	msgTypePong         = "pong"
	msgTypeError        = "error"
)

// protocolVersion is the wire protocol version this SDK implements (docs/PROTOCOL.md).
const protocolVersion = 1

// wirePeer is one entry of welcome's peers array.
type wirePeer struct {
	ID        string `json:"id"`
	StaticKey string `json:"static_key"`
}

// controlMessage is the JSON frame exchanged on the control channel (text frames
// only). Fields are selectively populated depending on Type; unknown fields on the
// way in are ignored by json.Unmarshal, and zero-value fields are omitted going out.
type controlMessage struct {
	Type string `json:"type"`

	// welcome
	ProtocolVersion int        `json:"protocol_version,omitempty"`
	DeviceID        string     `json:"device_id,omitempty"`
	Peers           []wirePeer `json:"peers,omitempty"`

	// announce_key
	StaticKey string `json:"static_key,omitempty"`

	// pair_code / pair_accept / pair_complete (Code doubles as the error machine
	// code on error messages, matching the server's wire shape)
	Code      string `json:"code,omitempty"`
	ExpiresIn int    `json:"expires_in,omitempty"`

	// pair_complete (PeerID is reused by peer_status)
	PeerID        string `json:"peer_id,omitempty"`
	PeerStaticKey string `json:"peer_static_key,omitempty"`

	// peer_status — a pointer so an explicit "online":false is distinguishable from
	// "field absent" while decoding.
	Online *bool `json:"online,omitempty"`

	// error
	Message string `json:"message,omitempty"`
}
