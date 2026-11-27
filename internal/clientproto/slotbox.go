package clientproto

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"

	"golang.org/x/crypto/nacl/box"
)

// A slot the relay holds for a phone is an authenticated NaCl box between the
// computer's executor X25519 key and the phone's box key (design 3.4, revised
// 2026-11-27 after the Hermes gate): only those two can compute the shared key,
// so the relay can neither read a slot nor forge or alter one. There is no
// certificate and no signature in a slot. What a signature used to cover is now
// inside the box, and the phone checks it against where it fetched the slot.

// SlotVersion is the version of the box plaintext.
const SlotVersion = 1

// NonceSize is the random nonce that leads every sealed slot.
const NonceSize = 24

// ErrSlot says a slot did not open, or opened to something that is not what was asked for.
var ErrSlot = errors.New("clientproto: slot does not open")

// SlotPlain is the plaintext of a slot. Payload is the existing JSON array of
// entries (or the outcome list), as bytes; Ver is the enrollment version the
// computer served the phone under (informational in v1: the phone records it
// and does not refuse on it).
type SlotPlain struct {
	V        int    `json:"v"`
	Slot     string `json:"slot"`
	Client   string `json:"client"`
	Executor string `json:"executor"`
	At       int64  `json:"at"`
	Ver      uint64 `json:"ver"`
	Payload  []byte `json:"payload"`
}

// SealSlot answers nonce || box.Seal(plaintext, nonce, phoneBoxPub, executorBoxPriv).
// The caller draws a fresh random nonce for every slot (NewNonce); a nonce must
// never repeat under one key pair.
func SealSlot(p SlotPlain, nonce *[NonceSize]byte, phoneBoxPub, executorBoxPriv *[32]byte) ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return box.Seal(append([]byte(nil), nonce[:]...), raw, nonce, phoneBoxPub, executorBoxPriv), nil
}

// NewNonce draws a fresh random nonce from crypto/rand.
func NewNonce() (*[NonceSize]byte, error) { return nonceFrom(rand.Reader) }

func nonceFrom(r io.Reader) (*[NonceSize]byte, error) {
	var n [NonceSize]byte
	_, err := io.ReadFull(r, n[:])
	return &n, err
}

// OpenSlot opens a sealed slot with the executor's public key (from its card) and
// the phone's private key. It answers ErrSlot for a short slot, a wrong key or
// any changed byte, and for plaintext that is not exactly a version-1 SlotPlain.
func OpenSlot(sealed []byte, executorBoxPub, phoneBoxPriv *[32]byte) (SlotPlain, error) {
	if len(sealed) < NonceSize+box.Overhead {
		return SlotPlain{}, ErrSlot
	}
	var nonce [NonceSize]byte
	copy(nonce[:], sealed)
	raw, ok := box.Open(nil, sealed[NonceSize:], &nonce, executorBoxPub, phoneBoxPriv)
	if !ok {
		return SlotPlain{}, ErrSlot
	}
	var p SlotPlain
	if jsonUnmarshalStrict(raw, &p) != nil || p.V != SlotVersion {
		return SlotPlain{}, ErrSlot
	}
	return p, nil
}

// Check is the phone's rule for an opened slot: it must name the slot it was fetched
// as, the phone it was fetched for and the computer that wrote it, and its `at` must
// be above the last one accepted for that slot (zero when none). It is the replay and
// rollback guard: a relay that serves an older slot, or one slot under another name,
// is refused here, not by the box.
func (p SlotPlain) Check(fetchedAs, client, writer string, lastAt int64) error {
	if p.Slot != fetchedAs || p.Client != client || p.Executor != writer || p.At <= lastAt {
		return ErrSlot
	}
	return nil
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// jsonUnmarshalStrict refuses unknown keys and trailing data.
func jsonUnmarshalStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return ErrSlot
	}
	return nil
}
