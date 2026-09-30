package pair

// What the device that showed the code hands over, and how the device that typed
// it decides whether to take it.
//
// THE GRANT IS THE WHOLE IDENTITY, and the words say so. A device that is
// paired holds the same root as the one that paired it: same chats, same vault,
// same relay account. That is the owner's shape for "your chats on every machine
// you own", and it is why revoking a device cuts its relay access and cannot
// take back what it already holds.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// grantVersion is the only shape of grant this build reads.
const grantVersion = 1

// maxGrant bounds what is read as a grant. The real one is a few hundred bytes;
// anything near this is not a grant, and is refused before it is parsed.
const maxGrant = 2048

// Grant is what one device gives another on a successful pairing.
type Grant struct {
	// Identity is the root the other device adopts.
	Identity identity.Identity
	// SyncURL is the relay to sync through, or empty for the default relay.
	SyncURL string
}

// grantDocument is the grant as it travels.
type grantDocument struct {
	V        int             `json:"V"`
	Identity json.RawMessage `json:"identity"`
	SyncURL  string          `json:"sync_url,omitempty"`
}

// verdictOf is the reply that carries a grant: a welcome, and the grant after it.
func (g Grant) verdictOf() ([]byte, error) {
	raw, err := g.Identity.Marshal()
	if err != nil {
		return nil, err
	}
	doc, err := json.Marshal(grantDocument{V: grantVersion, Identity: raw, SyncURL: g.SyncURL})
	if err != nil {
		return nil, err
	}
	return append(sayVerdict(""), doc...), nil
}

// hearGrant reads the reply of the device that showed the code: a grant, or a
// refusal. Nothing it returns has touched the disk.
func hearGrant(said []byte) (Grant, error) {
	if len(said) == 0 {
		return Grant{}, ErrBadGrant
	}
	if said[0] == verdictNo {
		return Grant{}, ErrRefused
	}
	if said[0] != verdictWelcome || len(said) > maxGrant {
		return Grant{}, ErrBadGrant
	}
	return readGrant(said[1:])
}

// readGrant parses a grant strictly: a known version, a whole identity, and a
// relay address that is a web address or nothing.
func readGrant(raw []byte) (Grant, error) {
	var doc grantDocument
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil || doc.V != grantVersion || !ended(dec) {
		return Grant{}, ErrBadGrant
	}
	id, err := identity.Unmarshal(doc.Identity)
	if err != nil {
		return Grant{}, ErrBadGrant
	}
	if err := checkSyncURL(doc.SyncURL); err != nil {
		return Grant{}, err
	}
	return Grant{Identity: id, SyncURL: doc.SyncURL}, nil
}

// ended is whether nothing but space follows the document, so that a grant is
// the whole of what arrived and not the first thing in it.
func ended(dec *json.Decoder) bool {
	_, err := dec.Token()
	return err == io.EOF
}

// checkSyncURL accepts no address, or an http or https address with a host.
func checkSyncURL(text string) error {
	if text == "" {
		return nil
	}
	u, err := url.Parse(text)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrBadGrant
	}
	return nil
}
