package clientproto

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// The grant is what the approving computer hands a phone when the owner says yes
// (design 5.1 step 4.3): the phone's own certificate, the identity's PUBLIC key, the
// relay the computers use, and the card of the approving computer. It is sealed
// under a key derived from the link key, the secret only the phone and the
// approving computer hold (design 3.8a, added 2026-11-27 after the lead's review).
//
// WHY NOT SEAL IT TO THE PHONE'S BOX KEY. An anonymous seal authenticates nobody:
// the relay holds the phone's box public key (it is in the request) and could seal
// its own grant to it, naming its own identity key and its own computers, and the
// phone would pair with the relay's choice of owner. A secretbox under a key the
// relay never sees can be made only by someone who holds the link key, so the
// phone's trust in the grant rests on the same secret as the key binding of the
// request (bind.go), and the relay can neither read nor forge nor swap it.

// GrantVersion is the version of the grant plaintext.
const GrantVersion = 2

// MaxGrantExecutors is how many cards a grant may carry. The approving computer
// knows its own card and no other, so v1 sends one; the phone accepts a few more
// so a later approver that has seen its neighbours can say so.
const MaxGrantExecutors = 4

// MaxGrantBytes is the largest sealed grant. It equals the relay's join-grant
// bound (clusterstate.MaxJoinGrant), which a test holds to this value.
const MaxGrantBytes = 4096

// grantLabel is the first line of the text the grant key is derived from.
const grantLabel = "codeaf/phone-grant/v1\n"

// ErrGrant says a grant did not open, or opened to something that is not a grant for this request.
var ErrGrant = errors.New("clientproto: grant does not open")

// ClientGrant is the plaintext of a grant. It carries no seed, no dedup secret and
// no cell key, and its keys are the ones the phone may see: the identity PUBLIC key
// (to verify cards and enrollment-signed things) and the phone's own certificate.
// Device and Code name the pairing request it answers, so a grant made for another
// request or another phone does not pass even if it opened.
type ClientGrant struct {
	V           int           `json:"v"`
	Device      string        `json:"device"`
	Code        string        `json:"code"`
	Cert        identity.Cert `json:"cert"`
	IdentityPub []byte        `json:"identity_pub"`
	Relay       string        `json:"relay"`
	Executors   []Card        `json:"executors"`
}

// GrantKey widens the link key into the secretbox key, in the shape of BindKey with its own label.
func GrantKey(link []byte) [32]byte {
	return sha256.Sum256(append([]byte(grantLabel), link...))
}

// SealGrant answers nonce || secretbox.Seal(plaintext) under GrantKey(link). The
// caller draws a fresh random nonce (NewNonce) for every grant; one link key
// seals one grant, so a repeat would need a second approval of the same request.
func SealGrant(g ClientGrant, nonce *[NonceSize]byte, link []byte) ([]byte, error) {
	if len(link) != ClientLinkKeyBytes {
		return nil, ErrGrant
	}
	raw, err := jsonMarshal(g)
	if err != nil {
		return nil, err
	}
	key := GrantKey(link)
	sealed := secretbox.Seal(append([]byte(nil), nonce[:]...), raw, nonce, &key)
	if len(sealed) > MaxGrantBytes {
		return nil, ErrGrant
	}
	return sealed, nil
}

// OpenGrant opens a sealed grant with the link key and decodes it strictly: a
// short grant, a wrong key, any changed byte, an unknown key in the plaintext or
// trailing data is ErrGrant. It does not check what the grant says; call Check.
func OpenGrant(sealed, link []byte) (ClientGrant, error) {
	if len(link) != ClientLinkKeyBytes || len(sealed) < NonceSize+secretbox.Overhead || len(sealed) > MaxGrantBytes {
		return ClientGrant{}, ErrGrant
	}
	var nonce [NonceSize]byte
	copy(nonce[:], sealed)
	key := GrantKey(link)
	raw, ok := secretbox.Open(nil, sealed[NonceSize:], &nonce, &key)
	if !ok {
		return ClientGrant{}, ErrGrant
	}
	var g ClientGrant
	if jsonUnmarshalStrict(raw, &g) != nil || g.V != GrantVersion {
		return ClientGrant{}, ErrGrant
	}
	return g, nil
}

// Check is the phone's rule for an opened grant: it answers the phone's own
// request (device and code), its certificate is the phone's own and verifies under
// the identity key the grant carries, the relay is named, and at least one card
// verifies under that identity (so there is a computer to talk to). A card that
// does not verify is not an error here, because the phone drops it: the grant is
// refused only when none is left. It answers ErrGrant for anything else.
func (g ClientGrant) Check(device, code string, phoneSign ed25519.PublicKey) error {
	pub, err := hex.DecodeString(g.Cert.Device)
	if g.V != GrantVersion || g.Device != device || g.Code != code || len(g.IdentityPub) != ed25519.PublicKeySize || err != nil ||
		len(pub) != ed25519.PublicKeySize || !bytes.Equal(pub, phoneSign) || g.Cert.DeviceID() != device || g.Cert.Verify(ed25519.PublicKey(g.IdentityPub)) != nil ||
		g.Relay == "" || len(g.Executors) == 0 || len(g.Executors) > MaxGrantExecutors || len(g.VerifiedExecutors()) == 0 {
		return ErrGrant
	}
	return nil
}

// VerifiedExecutors is the cards of the grant that verify under its identity key.
func (g ClientGrant) VerifiedExecutors() []Card {
	var out []Card
	for _, c := range g.Executors {
		if c.Verify(ed25519.PublicKey(g.IdentityPub)) == nil {
			out = append(out, c)
		}
	}
	return out
}

// GrantNonce draws the nonce for SealGrant from r (crypto/rand.Reader outside tests).
func GrantNonce(r io.Reader) (*[NonceSize]byte, error) { return nonceFrom(r) }
