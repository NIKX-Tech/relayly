// Package relay — JSON control channel (docs/PROTOCOL.md §5), carried on WebSocket
// text frames. Binary frames (the E2E envelope, §6) never reach this file, they are
// routed verbatim by readPump/Hub.Route without being parsed here.
package relay

import (
	"encoding/json"
	"errors"

	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/pairing"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// ProtocolVersion is the wire protocol version this server implements (docs/PROTOCOL.md).
const ProtocolVersion = 1

// controlMessage is the JSON frame exchanged on the control channel. Fields are
// selectively populated depending on Type; unknown fields on the way in are ignored
// by json.Unmarshal, and zero-value fields are omitted on the way out via omitempty.
type controlMessage struct {
	Type string `json:"type"`

	// welcome
	ProtocolVersion int        `json:"protocol_version,omitempty"`
	DeviceID        string     `json:"device_id,omitempty"`
	Peers           []peerInfo `json:"peers,omitempty"`

	// announce_key
	StaticKey string `json:"static_key,omitempty"`

	// pair_code / pair_accept / pair_complete (Code doubles as the error machine code
	// on error messages, matching docs/PROTOCOL.md's wire shape)
	Code      string `json:"code,omitempty"`
	ExpiresIn int    `json:"expires_in,omitempty"`

	// pair_complete (PeerID is reused by peer_status)
	PeerID        string `json:"peer_id,omitempty"`
	PeerStaticKey string `json:"peer_static_key,omitempty"`

	// peer_status — a pointer so an explicit "online":false is still sent
	Online *bool `json:"online,omitempty"`

	// error
	Message string `json:"message,omitempty"`
}

// peerInfo describes a linked peer inside a welcome message.
type peerInfo struct {
	ID        string `json:"id"`
	StaticKey string `json:"static_key"`
}

func boolPtr(b bool) *bool { return &b }

// sendJSON marshals v and enqueues it as a text control frame. It never blocks: if the
// send buffer is full (or the client has since closed) the frame is dropped, matching
// Route's existing "evict on backpressure" behavior for binary frames.
func (c *Client) sendJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		c.log.Error("control: marshal failed", zap.Error(err))
		return
	}
	if !c.enqueue(wsFrame{kind: websocket.TextMessage, data: data}) {
		c.log.Warn("send buffer full or client closed, dropping control frame")
	}
}

// sendError sends a recoverable error; the connection stays open.
func (c *Client) sendError(code, message string) {
	c.sendJSON(controlMessage{Type: "error", Code: code, Message: message})
}

// sendErrorAndClose sends an error and then closes the connection, once the error
// frame has actually been written (docs/PROTOCOL.md §7.2's "error + close"). Writes
// stay funneled through writePump, the connection's only writer goroutine, rather than
// writing directly here, so this never races with an in-flight ping or relayed frame.
func (c *Client) sendErrorAndClose(code, message string) {
	data, err := json.Marshal(controlMessage{Type: "error", Code: code, Message: message})
	if err != nil {
		c.log.Error("control: marshal failed", zap.Error(err))
		c.close()
		return
	}
	if !c.enqueue(wsFrame{kind: websocket.TextMessage, data: data, closeAfter: true}) {
		c.log.Warn("send buffer full or client closed while closing with error, closing without it")
		c.close()
	}
}

// sendWelcome sends the welcome message (§5.1) right after this client registers with
// the Hub, followed by an initial peer_status if it is already linked to a peer.
func (c *Client) sendWelcome(device *database.Device) {
	peers := []peerInfo{}
	if device.PairedWith != nil {
		staticKey := ""
		if peer, err := c.db.GetDeviceByID(*device.PairedWith); err == nil {
			staticKey = peer.StaticKey
		}
		peers = append(peers, peerInfo{ID: *device.PairedWith, StaticKey: staticKey})
	}

	c.sendJSON(controlMessage{
		Type:            "welcome",
		ProtocolVersion: ProtocolVersion,
		DeviceID:        c.DeviceID,
		Peers:           peers,
	})

	if device.PairedWith != nil {
		_, online := c.hub.GetClient(*device.PairedWith)
		c.sendJSON(controlMessage{Type: "peer_status", PeerID: *device.PairedWith, Online: boolPtr(online)})
	}
}

// handleControl parses and dispatches one text control frame.
func (c *Client) handleControl(raw []byte) {
	var msg controlMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		c.sendError("malformed", "invalid JSON control frame")
		return
	}

	switch msg.Type {
	case "announce_key":
		c.handleAnnounceKey(msg)
	case "pair_request":
		c.handlePairRequest()
	case "pair_accept":
		c.handlePairAccept(msg)
	case "ping":
		c.sendJSON(controlMessage{Type: "pong"})
	default:
		// Unknown type: ignored per §5's forward-compatibility rule.
	}
}

// handleAnnounceKey implements §7.2's client-key locking: the first announcement for a
// device is persisted, a later announcement with a different key closes the connection.
func (c *Client) handleAnnounceKey(msg controlMessage) {
	if msg.StaticKey == "" {
		c.sendError("malformed", "announce_key requires static_key")
		return
	}

	if err := c.db.SetStaticKeyIfUnset(c.DeviceID, msg.StaticKey); err != nil {
		if errors.Is(err, database.ErrStaticKeyMismatch) {
			c.sendErrorAndClose("key_mismatch", "announced static key does not match the key locked on first connection")
			return
		}
		c.log.Error("announce_key: db error", zap.Error(err))
		c.sendError("internal", "internal error")
	}
}

// handlePairRequest implements the requesting half of §5.3: generate a fresh code,
// register it, and hand it back to the caller.
func (c *Client) handlePairRequest() {
	code, err := pairing.GeneratePairingCode()
	if err != nil {
		c.log.Error("pair_request: generating code", zap.Error(err))
		c.sendError("internal", "internal error")
		return
	}

	c.hub.pairCodes.Put(code, c.DeviceID, pairing.PairingCodeTTL)
	c.sendJSON(controlMessage{
		Type:      "pair_code",
		Code:      code,
		ExpiresIn: int(pairing.PairingCodeTTL.Seconds()),
	})
}

// handlePairAccept implements the accepting half of §5.3: redeem the code, link both
// devices (reusing the existing db.PairDevices used by the CLI/admin manual-link path),
// update both currently-connected clients' in-memory routing, and notify both sides.
func (c *Client) handlePairAccept(msg controlMessage) {
	if msg.Code == "" {
		c.sendError("malformed", "pair_accept requires code")
		return
	}

	requesterID, expired, ok := c.hub.pairCodes.Take(msg.Code)
	if !ok {
		if expired {
			c.sendError("code_expired", "pairing code has expired")
		} else {
			c.sendError("invalid_code", "unknown pairing code")
		}
		return
	}
	if requesterID == c.DeviceID {
		c.sendError("invalid_code", "cannot pair with yourself")
		return
	}

	if err := c.db.PairDevices(requesterID, c.DeviceID); err != nil {
		switch {
		case errors.Is(err, database.ErrAlreadyPaired):
			c.sendError("already_paired", "one of the devices is already paired")
		case errors.Is(err, database.ErrNotFound):
			c.sendError("invalid_code", "requesting device no longer exists")
		default:
			c.log.Error("pair_accept: db error", zap.Error(err))
			c.sendError("internal", "internal error")
		}
		return
	}

	myStaticKey := ""
	if me, err := c.db.GetDeviceByID(c.DeviceID); err == nil {
		myStaticKey = me.StaticKey
	}
	requesterStaticKey := ""
	if requester, err := c.db.GetDeviceByID(requesterID); err == nil {
		requesterStaticKey = requester.StaticKey
	}

	c.setPeer(requesterID)
	c.sendJSON(controlMessage{
		Type:          "pair_complete",
		Code:          msg.Code,
		PeerID:        requesterID,
		PeerStaticKey: requesterStaticKey,
	})

	if requester, connected := c.hub.GetClient(requesterID); connected {
		requester.setPeer(c.DeviceID)
		requester.sendJSON(controlMessage{
			Type:          "pair_complete",
			Code:          msg.Code,
			PeerID:        c.DeviceID,
			PeerStaticKey: myStaticKey,
		})
	}
}
