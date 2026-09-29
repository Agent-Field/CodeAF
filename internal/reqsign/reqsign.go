// Package reqsign proves who sent an HTTP request to the relay. A request is
// signed by the device key and carries the device's cert, which the identity
// signed, so the relay needs no account table: the namespace is the hash of the
// identity key and the acting device is the hash of the device key. A device
// therefore cannot act as another, and a lost one can later be dropped.
package reqsign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// Skew is how far a request's time may be from the receiver's clock.
const Skew = 5 * time.Minute

// The four headers of a signed request.
const (
	HeaderIdentity = "Codeaf-Identity"
	HeaderCert     = "Codeaf-Cert"
	HeaderTime     = "Codeaf-Time"
	HeaderSig      = "Codeaf-Sig"
)

const msgLabel = "codeaf-req-v1\n"

// Signer is the sending side: a device and the identity key that vouches for it.
type Signer interface {
	IdentityKey() ed25519.PublicKey // verifies the cert
	Cert() identity.Cert            // this device's cert, signed by the identity
	Sign(msg []byte) []byte         // signs with the device key
}

// refusal is an error that says what went wrong and is also its wireauth kind,
// so callers match either the specific error or the seam's one.
type refusal struct {
	text string
	kind error
}

func (e *refusal) Error() string { return "reqsign: " + e.text }
func (e *refusal) Unwrap() error { return e.kind }

// Every refusal but skew is unauthorized to the seam; skew is its own kind.
var (
	ErrUnsigned     error = &refusal{"request is not signed", wireauth.ErrUnauthorized}
	ErrBadCert      error = &refusal{"device certificate does not verify", wireauth.ErrUnauthorized}
	ErrBadSignature error = &refusal{"signature does not verify", wireauth.ErrUnauthorized}
	ErrSkew         error = &refusal{"clock is too far from the receiver's", wireauth.ErrSkew}
)

var b64 = base64.RawURLEncoding

// IDOf is the identity id for an identity public key.
func IDOf(identityKey ed25519.PublicKey) string {
	sum := sha256.Sum256(identityKey)
	return "id_" + hex.EncodeToString(sum[:16])
}

// message is what the device signs: the method, the path with its query, the
// time and the body's hash, so none of them can change in flight.
func message(r *http.Request, at string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(msgLabel + r.Method + "\n" + r.URL.RequestURI() + "\n" + at + "\n" + hex.EncodeToString(sum[:]))
}

// Sign stamps r with the four headers, timed at now.
func Sign(r *http.Request, body []byte, s Signer, now time.Time) {
	at := strconv.FormatInt(now.UnixMilli(), 10)
	cert, _ := json.Marshal(s.Cert())
	r.Header.Set(HeaderIdentity, b64.EncodeToString(s.IdentityKey()))
	r.Header.Set(HeaderCert, b64.EncodeToString(cert))
	r.Header.Set(HeaderTime, at)
	r.Header.Set(HeaderSig, b64.EncodeToString(s.Sign(message(r, at, body))))
}

// proof is the four headers, decoded.
type proof struct {
	identityKey ed25519.PublicKey
	cert        identity.Cert
	at          string
	sig         []byte
}

// readProof decodes the headers, or says the request is unsigned or its cert
// unreadable.
func readProof(r *http.Request) (proof, error) {
	var p proof
	h := r.Header
	key, e1 := b64.DecodeString(h.Get(HeaderIdentity))
	rawCert, e2 := b64.DecodeString(h.Get(HeaderCert))
	sig, e3 := b64.DecodeString(h.Get(HeaderSig))
	if e1 != nil || e2 != nil || e3 != nil || len(key) != ed25519.PublicKeySize || h.Get(HeaderTime) == "" {
		return p, ErrUnsigned
	}
	if json.Unmarshal(rawCert, &p.cert) != nil {
		return p, ErrBadCert
	}
	return proof{identityKey: key, cert: p.cert, at: h.Get(HeaderTime), sig: sig}, nil
}

// deviceKey is the cert's device public key, when it is well formed.
func deviceKey(c identity.Cert) (ed25519.PublicKey, bool) {
	pub, err := hex.DecodeString(c.Device)
	return pub, err == nil && len(pub) == ed25519.PublicKeySize
}

// withinSkew reports whether at (unix ms text) is near now.
func withinSkew(at string, now time.Time) bool {
	ms, err := strconv.ParseInt(at, 10, 64)
	if err != nil {
		return false
	}
	d := now.Sub(time.UnixMilli(ms))
	return d <= Skew && -d <= Skew
}

// Verify checks a signed request: the cert under the identity key, the
// signature under the cert's device key, then the time. It answers the
// identity id and the device id.
func Verify(r *http.Request, body []byte, now time.Time) (identityID, device string, err error) {
	p, err := readProof(r)
	if err != nil {
		return "", "", err
	}
	pub, ok := deviceKey(p.cert)
	if !ok || p.cert.Verify(p.identityKey) != nil {
		return "", "", ErrBadCert
	}
	if !ed25519.Verify(pub, message(r, p.at, body), p.sig) {
		return "", "", ErrBadSignature
	}
	if !withinSkew(p.at, now) {
		return "", "", ErrSkew
	}
	return IDOf(p.identityKey), p.cert.DeviceID(), nil
}

// SignFor is the wire seam's Sign, bound to a signer and a clock.
func SignFor(s Signer, now func() time.Time) wireauth.Sign {
	return func(r *http.Request, body []byte) { Sign(r, body, s, now()) }
}

// AuthenticateAt is the wire seam's Authenticate, bound to a clock.
func AuthenticateAt(now func() time.Time) wireauth.Authenticate {
	return func(r *http.Request, body []byte) (string, string, error) { return Verify(r, body, now()) }
}
