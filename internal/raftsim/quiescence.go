package raftsim

import (
	"crypto/ed25519"
	"crypto/sha256"
)

// This public deterministic fixture key belongs only to the simulator's narrow
// anchor authority. It never authenticates a real identity or peer.
func quiescenceKey() ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("raftsim quiescence anchor fixture"))
	return ed25519.NewKeyFromSeed(seed[:])
}
