package identity

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"time"
)

// Seed is a device's secret key before any identity has certified it. A device
// that asks to join an identity makes a Seed, shows its public half, and becomes
// a Dev when an already-joined device sends back the certificate.
type Seed struct{ seed key }

// NewSeed makes a fresh device secret.
func NewSeed() (Seed, error) {
	k, err := randomKey()
	return Seed{seed: k}, err
}

// Public is the device's public key, the one a certificate names.
func (s Seed) Public() ed25519.PublicKey {
	return ed25519.NewKeyFromSeed(s.seed[:]).Public().(ed25519.PublicKey)
}

// Join turns the seed into a device under cert, which must name this seed's key.
func (s Seed) Join(cert Cert) (Dev, error) {
	if cert.Device != hex.EncodeToString(s.Public()) {
		return Dev{}, errors.New("identity: the certificate is for another device")
	}
	return Dev{Cert: cert, seed: s.seed}, nil
}

// IssueCert signs a certificate for the device with public key pub, made at now.
// It is how a device that is already joined lets another one in.
func IssueCert(id Identity, pub ed25519.PublicKey, now time.Time) Cert {
	cert := Cert{V: version, Device: hex.EncodeToString(pub), Made: now.UnixMilli()}
	cert.Sig = hex.EncodeToString(id.Sign(cert.body()))
	return cert
}
