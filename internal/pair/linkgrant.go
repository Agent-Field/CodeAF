package pair

// What an approving device sends the one it lets in: the identity, the relay
// the identity syncs through, and the certificate that makes the new device one
// of this identity's devices. It is sealed to the key the new device showed, so
// the relay carries bytes it cannot read.

import (
	"bytes"
	"crypto/rand"
	"encoding/json"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// LinkGrant is a Grant and the certificate for the device receiving it.
type LinkGrant struct {
	Grant
	Cert identity.Cert
}

type linkDocument struct {
	grantDocument
	Cert identity.Cert `json:"cert"`
}

// seal writes the grant and seals it to the new device's box key.
func (g LinkGrant) seal(to *[32]byte) (string, error) {
	raw, err := g.Identity.Marshal()
	if err != nil {
		return "", err
	}
	doc, err := json.Marshal(linkDocument{
		grantDocument{V: grantVersion, Identity: raw, SyncURL: g.SyncURL, Replaces: g.Replaces}, g.Cert})
	if err != nil {
		return "", err
	}
	sealed, err := box.SealAnonymous(nil, doc, to, rand.Reader)
	return b64u.EncodeToString(sealed), err
}

// openLinkGrant opens a sealed grant with the box keys the new device made.
func openLinkGrant(sealed string, pub, priv *[32]byte) (LinkGrant, error) {
	raw, err := b64u.DecodeString(sealed)
	if err != nil {
		return LinkGrant{}, ErrBadGrant
	}
	plain, ok := box.OpenAnonymous(nil, raw, pub, priv)
	if !ok {
		return LinkGrant{}, ErrBadGrant
	}
	var doc linkDocument
	dec := json.NewDecoder(bytes.NewReader(plain))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil || doc.V != grantVersion || !ended(dec) {
		return LinkGrant{}, ErrBadGrant
	}
	return linkGrantOf(doc)
}

func linkGrantOf(doc linkDocument) (LinkGrant, error) {
	id, err := identity.Unmarshal(doc.Identity)
	if err != nil || checkSyncURL(doc.SyncURL) != nil || !wholeLineage(doc.Replaces) || doc.Cert.Verify(id.PublicKey()) != nil {
		return LinkGrant{}, ErrBadGrant
	}
	return LinkGrant{Grant{Identity: id, SyncURL: doc.SyncURL, Replaces: doc.Replaces}, doc.Cert}, nil
}
