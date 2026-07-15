package relayly

import (
	"sync"
	"time"
)

// unsolicitedMsg1MinInterval throttles how often this client starts a brand new
// responder session for an unsolicited msg1 arriving on an already-healthy peer
// connection. The relay is untrusted (docs/PROTOCOL.md §6, §7), so without this a
// malicious or compromised relay could otherwise force perpetual handshake churn.
const unsolicitedMsg1MinInterval = 2 * time.Second

// peerConn tracks everything this client knows about one paired device: its
// authenticated static key once known, the currently active Noise session, and (while
// a rekey is in flight) a pending replacement session that must not replace active
// until it actually completes — docs/PROTOCOL.md §6's make-before-break rule.
type peerConn struct {
	mu sync.Mutex

	id                 string
	announcedStaticKey string // from pair_complete, for the §7.2 cross-check

	active  *noiseSession // nil until the first handshake for this peer has started
	pending *noiseSession // non-nil only while a rekey replacement is in flight

	// firstPairWaiter, if set, is notified exactly once — when this peer's very
	// first handshake resolves (successfully or not). Cleared on first use.
	firstPairWaiter chan PairResult

	lastUnsolicitedMsg1 time.Time
}

// setFirstPairWaiter registers the channel to notify when this peer's first-ever
// handshake resolves (docs/PROTOCOL.md §5.3's pairing flow).
func (p *peerConn) setFirstPairWaiter(ch chan PairResult) {
	p.mu.Lock()
	p.firstPairWaiter = ch
	p.mu.Unlock()
}

// takeFirstPairWaiter returns and clears the registered waiter, if any, so it is only
// ever notified once.
func (p *peerConn) takeFirstPairWaiter() chan PairResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := p.firstPairWaiter
	p.firstPairWaiter = nil
	return ch
}

func newPeerConn(id, announcedStaticKey string) *peerConn {
	return &peerConn{id: id, announcedStaticKey: announcedStaticKey}
}

// startAsInitiator begins the very first handshake for this peer (§5.3: the accepting
// device initiates). Returns the msg1 payload to send.
func (p *peerConn) startAsInitiator(priv PrivateKey) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, msg1, err := newInitiatorSession(priv)
	if err != nil {
		return nil, err
	}
	p.active = session
	return msg1, nil
}

// startRekeyAsInitiator begins a replacement handshake (§6: triggered when this
// device's ID is lexicographically smaller than the peer's, on any reconnect). The
// existing active session, if any, keeps working until the replacement completes.
func (p *peerConn) startRekeyAsInitiator(priv PrivateKey) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	session, msg1, err := newInitiatorSession(priv)
	if err != nil {
		return nil, err
	}
	p.pending = session
	return msg1, nil
}

// handleHandshakeEnvelope feeds one received envelopeHandshake payload to the right
// session, starting a new responder session if needed and applying make-before-break
// plus unsolicited-msg1 rate limiting. reply is non-nil when a response must be sent.
// completed is set once the session it was driving finishes (successfully or not);
// wasPending tells the caller whether that was a rekey attempt (abandon on failure,
// promote on success) or the peer's first-ever handshake (nothing to fall back to).
func (p *peerConn) handleHandshakeEnvelope(priv PrivateKey, data []byte) (reply []byte, completed *noiseSession, wasPending bool, err error) {
	p.mu.Lock()

	var session *noiseSession
	switch {
	case p.pending != nil:
		session = p.pending
		wasPending = true
	case p.active != nil && !p.active.ready():
		// Continuing the (first-ever) in-progress handshake.
		session = p.active
	case p.active == nil:
		// No session at all yet: this incoming msg1 starts the very first
		// handshake for this peer, with us as responder.
		session, err = newResponderSession(priv)
		if err != nil {
			p.mu.Unlock()
			return nil, nil, false, err
		}
		p.active = session
	default:
		// active exists and is ready: an unsolicited msg1 on a healthy connection.
		if time.Since(p.lastUnsolicitedMsg1) < unsolicitedMsg1MinInterval {
			p.mu.Unlock()
			return nil, nil, false, nil // rate-limited, drop silently
		}
		p.lastUnsolicitedMsg1 = time.Now()
		session, err = newResponderSession(priv)
		if err != nil {
			p.mu.Unlock()
			return nil, nil, false, err
		}
		p.pending = session
		wasPending = true
	}
	p.mu.Unlock()

	var done bool
	reply, done, err = session.handleHandshakeMessage(data)
	if !done {
		return reply, nil, wasPending, err
	}
	return reply, session, wasPending, err
}

// promote swaps a just-completed pending session into active. No-op if session was
// already active (the peer's first-ever handshake, not a rekey).
func (p *peerConn) promote(session *noiseSession) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == session {
		p.active = session
		p.pending = nil
	}
}

// abandon drops a failed pending replacement, leaving the existing active session
// (still healthy) untouched. No-op if session was the peer's first-ever (active)
// session — a failed first handshake fails the peer entirely; the caller handles that
// case separately, there is nothing to fall back to.
func (p *peerConn) abandon(session *noiseSession) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == session {
		p.pending = nil
	}
}

// send encrypts payload for this peer using the active session, or ErrNotReady if
// there isn't a ready one yet.
func (p *peerConn) send(payload []byte) ([]byte, error) {
	p.mu.Lock()
	active := p.active
	p.mu.Unlock()
	if active == nil {
		return nil, ErrNotReady
	}
	return active.encrypt(payload)
}

// recv decrypts an incoming transport envelope using the active session.
func (p *peerConn) recv(ciphertext []byte) ([]byte, error) {
	p.mu.Lock()
	active := p.active
	p.mu.Unlock()
	if active == nil {
		return nil, ErrNotReady
	}
	return active.decrypt(ciphertext)
}
