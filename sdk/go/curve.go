package relayly

import "golang.org/x/crypto/curve25519"

// scalarBaseMult computes the Curve25519 scalar multiplication of the base point
// with the given scalar (private key), writing the resulting public key to dst.
func scalarBaseMult(dst, scalar *[32]byte) {
	curve25519.ScalarBaseMult(dst, scalar)
}
