// Package passkeytest is a software WebAuthn authenticator for tests. It
// builds the exact bytes a browser hands a relying party — clientDataJSON, an
// attestation object of fmt "none", and an ES256 assertion signed over
// authenticatorData || SHA-256(clientDataJSON) — so a server's real
// go-webauthn verification runs on them. Its flags are the test's to choose,
// which is how a missing user verification is proven to be refused.
//
// IT IS FOR TESTS ONLY. Nothing outside a _test.go file imports it.
package passkeytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// Authenticator data flags.
const (
	FlagUP = 0x01
	FlagUV = 0x04
	FlagBE = 0x08
	FlagBS = 0x10
	FlagAT = 0x40
)

// Key is one ES256 passkey.
type Key struct {
	Priv       *ecdsa.PrivateKey
	ID         []byte
	UserHandle []byte
	Count      uint32
	Flags      byte
	Transports []string
}

// B64 is base64url without padding, as WebAuthn spells bytes.
func B64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// New is a synced, user-verifying passkey with a fresh 32-byte id.
func New() (*Key, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return &Key{Priv: priv, ID: id, Flags: FlagUP | FlagUV | FlagBE | FlagBS, Transports: []string{"internal", "hybrid"}}, nil
}

func (k *Key) cose() ([]byte, error) {
	x, y := make([]byte, 32), make([]byte, 32)
	k.Priv.PublicKey.X.FillBytes(x)
	k.Priv.PublicKey.Y.FillBytes(y)
	return cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
}

func clientData(kind, challenge, origin string) []byte {
	out, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return out
}

func authData(rpID string, flags byte, count uint32) []byte {
	hash := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, hash[:]...)
	out = append(out, flags)
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], count)
	return append(out, c[:]...)
}

// Create answers navigator.credentials.create() for options (the server's
// {"publicKey": …} JSON) as a browser at origin would.
func (k *Key) Create(options []byte, origin string) (json.RawMessage, error) {
	var opt struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opt); err != nil {
		return nil, fmt.Errorf("creation options: %w", err)
	}
	handle, err := base64.RawURLEncoding.DecodeString(opt.PublicKey.User.ID)
	if err != nil {
		return nil, fmt.Errorf("user id: %w", err)
	}
	k.UserHandle = handle
	ad := authData(opt.PublicKey.RP.ID, k.Flags|FlagAT, k.Count)
	ad = append(ad, make([]byte, 16)...) // AAGUID: none
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(k.ID)))
	ad = append(ad, l[:]...)
	ad = append(ad, k.ID...)
	key, err := k.cose()
	if err != nil {
		return nil, err
	}
	ad = append(ad, key...)
	att, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": ad})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": B64(k.ID), "rawId": B64(k.ID), "type": "public-key", "authenticatorAttachment": "platform",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    B64(clientData("webauthn.create", opt.PublicKey.Challenge, origin)),
			"attestationObject": B64(att), "transports": k.Transports,
		},
	})
}

// Get answers navigator.credentials.get() for options at origin.
func (k *Key) Get(options []byte, origin string) (json.RawMessage, error) {
	var opt struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opt); err != nil {
		return nil, fmt.Errorf("request options: %w", err)
	}
	k.Count++
	ad := authData(opt.PublicKey.RPID, k.Flags, k.Count)
	cd := clientData("webauthn.get", opt.PublicKey.Challenge, origin)
	sum := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.Priv, digest[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id": B64(k.ID), "rawId": B64(k.ID), "type": "public-key", "clientExtensionResults": map[string]any{},
		"response": map[string]any{"clientDataJSON": B64(cd), "authenticatorData": B64(ad), "signature": B64(sig), "userHandle": B64(k.UserHandle)},
	})
}
