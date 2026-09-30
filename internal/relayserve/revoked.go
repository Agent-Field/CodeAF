package relayserve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// turningRevokedAway is the one place the signature check meets the directory.
// reqsign proves who sent a request and knows nothing of records; the
// directory knows which devices were stopped and knows nothing of signatures.
// The relay joins them here, so both wires refuse a revoked device the same
// way, with the same answer, and neither has to be taught about revocation.
//
// The check follows the signature on purpose: an unproven request must not
// learn whether a device id is revoked. A directory that cannot be opened
// refuses the request as unauthorized, never as allowed.
func turningRevokedAway(auth wireauth.Authenticate, revoked func(identity, device string) (bool, error)) wireauth.Authenticate {
	return func(r *http.Request, body []byte) (string, string, error) {
		id, dev, err := auth(r, body)
		if err != nil {
			return id, dev, err
		}
		stopped, err := revoked(id, dev)
		switch {
		case err != nil:
			return "", "", wireauth.ErrUnauthorized
		case stopped:
			return "", "", wireauth.ErrRevoked
		}
		return id, dev, nil
	}
}
