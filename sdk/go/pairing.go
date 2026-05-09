package relayly

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// PairCode is returned by RequestPairCode and contains the code to share with the other device.
type PairCode struct {
	// Short is the 6-digit code to display or share out-of-band.
	Short string

	// ExpiresIn is the number of seconds until this code expires.
	ExpiresIn int

	client   *Client
	resultCh chan PairResult
}

// QRCodeURL returns a URL that encodes both the server address and pairing code,
// suitable for generating a QR code image.
//
// The URL format is: <serverURL>/pair?code=<short>
//
//	img := qrcode.New(code.QRCodeURL("wss://relay.example.com"), qrcode.Medium)
func (p *PairCode) QRCodeURL(serverURL string) string {
	u, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Sprintf("%s/pair?code=%s", serverURL, p.Short)
	}
	u.Path = "/pair"
	q := u.Query()
	q.Set("code", p.Short)
	u.RawQuery = q.Encode()
	return u.String()
}

// Wait blocks until the other device accepts the pairing or the context is cancelled.
// On success it returns the newly paired Peer.
//
//	peer, err := code.Wait(ctx)
//	fmt.Println("Paired with", peer.ID)
func (p *PairCode) Wait(ctx context.Context) (*Peer, error) {
	select {
	case result := <-p.resultCh:
		if result.Error != nil {
			return nil, result.Error
		}
		return &Peer{ID: result.PeerID, PublicKey: result.PeerPublicKey}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.client.done:
		return nil, fmt.Errorf("relayly: client closed before pairing completed")
	}
}

// Peer represents a paired remote device.
type Peer struct {
	// ID is the remote device's identifier, as registered with the server.
	ID string

	// PublicKey is the remote device's X25519 public key used for encryption.
	PublicKey PublicKey
}

// PairResult is used internally to pass pairing outcomes across goroutines.
type PairResult struct {
	// For pair_code responses
	Code      string
	ExpiresIn int

	// For pair_complete responses
	PeerID        string
	PeerPublicKey PublicKey

	// Non-nil if an error occurred
	Error error
}

// Message is an incoming decrypted message from a paired peer.
type Message struct {
	// From is the device ID of the sender.
	From string

	// Payload is the decrypted plaintext message.
	Payload []byte

	// Timestamp is the server-assigned receive timestamp.
	Timestamp time.Time
}
