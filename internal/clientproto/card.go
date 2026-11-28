package clientproto

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// cardLabel is the first line of the text a card is signed over.
const cardLabel = "codeaf-card-v1\n"

// MaxCardNameRunes is the longest display name a card carries.
const MaxCardNameRunes = 32

// ErrCard says a card is malformed or does not verify under the identity.
var ErrCard = errors.New("clientproto: card does not verify")

// Card is how a computer introduces itself to a phone: the executor key the
// phone seals its calls to, under the computer's own device certificate. It is
// the one signed thing the phone checks, and it is how the phone learns a
// computer's key at all: slots are authenticated by that key (slotbox.go), so
// a relay that cannot forge a card cannot forge a slot either.
type Card struct {
	V      int           `json:"v"`
	Writer string        `json:"writer"`
	Cert   identity.Cert `json:"cert"`
	BoxPub []byte        `json:"box_pub"`
	Name   string        `json:"name"`
	At     int64         `json:"at"`
	Sig    []byte        `json:"sig"`
}

// SignText is the exact text the device signature covers.
func (c Card) SignText() []byte {
	return []byte(cardLabel + c.Writer + "\n" + hex.EncodeToString(c.BoxPub) + "\n" + strconv.FormatInt(c.At, 10) + "\n" + c.Name)
}

// Sign signs c with the computer's device key and answers the signed copy.
func (c Card) Sign(device ed25519.PrivateKey) Card {
	c.Sig = ed25519.Sign(device, c.SignText())
	return c
}

// ValidCardName says a name is readable: not empty, at most 32 runes, no control characters.
func ValidCardName(name string) bool {
	n := utf8.RuneCountInString(name)
	if n == 0 || n > MaxCardNameRunes || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Verify checks the card under the identity key the phone received at pairing:
// the certificate verifies under that identity, names the writer, and the card
// is signed by that certificate's device key.
func (c Card) Verify(identityPub ed25519.PublicKey) error {
	pub, err := hex.DecodeString(c.Cert.Device)
	if c.V != 1 || c.Cert.V != 1 || len(identityPub) != ed25519.PublicKeySize || err != nil || len(pub) != ed25519.PublicKeySize ||
		len(c.BoxPub) != 32 || len(c.Sig) != ed25519.SignatureSize || !ValidCardName(c.Name) ||
		c.Cert.Verify(identityPub) != nil || c.Cert.DeviceID() != c.Writer || c.Writer != DeviceIDOf(pub) ||
		!ed25519.Verify(pub, c.SignText(), c.Sig) {
		return ErrCard
	}
	return nil
}

// SealCard seals a card to the phone's box key for its slot `card.<writer>`. It is
// anonymous on purpose: the phone has no key for this computer yet, and the card's
// own signature chain, not the box, is what authenticates it. rand is the
// ephemeral key source (crypto/rand.Reader outside tests).
func SealCard(c Card, phoneBoxPub *[32]byte, rand io.Reader) ([]byte, error) {
	raw, err := jsonMarshal(c)
	if err != nil {
		return nil, err
	}
	return box.SealAnonymous(nil, raw, phoneBoxPub, rand)
}

// OpenCard opens a sealed card with the phone's box key pair. It does not verify
// the chain: call Verify on the result.
func OpenCard(sealed []byte, phoneBoxPub, phoneBoxPriv *[32]byte) (Card, error) {
	raw, ok := box.OpenAnonymous(nil, sealed, phoneBoxPub, phoneBoxPriv)
	if !ok {
		return Card{}, ErrCard
	}
	var c Card
	if err := jsonUnmarshalStrict(raw, &c); err != nil {
		return Card{}, ErrCard
	}
	return c, nil
}
