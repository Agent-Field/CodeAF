// Package taskdoor carries task attempts between devices of one identity.
// The relay only routes encrypted streams; device certificates decide who may
// submit work, independently of the machine-access pairing book.
package taskdoor

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relay"
)

const doorKeySalt = "codeaf task door v1"

// DoorKey derives a device's task key without publishing another directory
// entry. A cell-key rotation therefore also changes every task door.
func DoorKey(id identity.Identity, deviceID string) (*ecdh.PrivateKey, error) {
	raw, err := hkdf.Key(sha256.New, id.CellKey(), []byte(doorKeySalt), deviceID, 32)
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPrivateKey(raw)
}

// DoorName is the relay rendezvous name of the device's derived public key.
func DoorName(id identity.Identity, deviceID string) (string, error) {
	key, err := DoorKey(id, deviceID)
	if err != nil {
		return "", err
	}
	return relay.NameFor(key.PublicKey().Bytes()), nil
}
