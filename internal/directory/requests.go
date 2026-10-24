package directory

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// RequestState is where a pending request stands. It moves once, from
// RequestPending, and never back.
type RequestState string

// The three states of a request.
const (
	RequestPending  RequestState = "pending"
	RequestApproved RequestState = "approved"
	RequestDenied   RequestState = "denied"
)

// The numbers every implementation shares (contract 1, 3).
const (
	RequestTTL  = 10 * time.Minute // a request lives this long, counted by the relay's clock
	DecidedKeep = 2 * time.Minute  // a decided request stays readable this long
	MaxWait     = 25 * time.Second // the longest GetRequest wait
	MaxGrant    = 4096             // bytes of grant the relay stores
	MaxNewBody  = 1024             // bytes of a create body
	maxName     = 96               // bytes of name before sealing
	sealedExtra = 24 + 16          // secretbox nonce and tag
	keySize     = 32
)

// Request is one device asking to join, as the relay keeps it.
type Request struct {
	Code        string       `json:"code"`
	Device      string       `json:"device"`
	Pubkey      string       `json:"pubkey"`
	X25519      string       `json:"x25519"`
	NameSealed  string       `json:"name_sealed"`
	Platform    string       `json:"platform"`
	Check       string       `json:"check"`
	RequestedAt int64        `json:"requested_at"`
	ExpiresAt   int64        `json:"expires_at"`
	State       RequestState `json:"state"`
	Grant       *string      `json:"grant"` // nil until approved
}

// NewRequest is the body of a create. Every key is b64u of 32 bytes; NameSealed
// is the device name sealed under the link key, which the relay never sees.
type NewRequest struct {
	Pubkey     string `json:"pubkey"`
	X25519     string `json:"x25519"`
	NameSealed string `json:"name_sealed"`
	Platform   string `json:"platform"`
}

// Opened answers a create. The caller builds the link from Code and its own key.
type Opened struct {
	Code        string `json:"code"`
	Device      string `json:"device"`
	Check       string `json:"check"`
	RequestedAt int64  `json:"requested_at"`
	ExpiresAt   int64  `json:"expires_at"`
}

// Approval is the body of an approve: the Device record to write for the
// request's device, its certificate from the identity, and the grant sealed to
// the new device, which the relay keeps as opaque bytes.
type Approval struct {
	Device Device `json:"device"`
	Cert   string `json:"cert"`  // b64u of the identity.Cert JSON
	Grant  string `json:"grant"` // b64u of the sealed box
}

// Requests is the open side of link pairing: what the new device calls, with
// no identity. Links.From serves it in process and LinkHTTP over the wire.
type Requests interface {
	// CreateRequest asks to join. ErrBadRequest, ErrTooBig, ErrRateLimited, ErrFull.
	CreateRequest(ctx context.Context, in NewRequest) (Opened, error)
	// GetRequest reads a request. With wait > 0 it holds until the request is
	// decided and answers ErrStillPending if the wait (at most MaxWait) ends
	// first. ErrRequestGone, ErrRateLimited.
	GetRequest(ctx context.Context, code string, wait time.Duration) (Request, error)
}

// ErrStillPending ends a GetRequest wait in which nobody decided.
var ErrStillPending = errors.New("directory: request still pending")

// The errors link pairing adds (contract 7).
var (
	ErrBadRequest     = errors.New("directory: bad request")
	ErrRequestGone    = errors.New("directory: no such request") // unknown, expired and deleted are one answer
	ErrAlreadyDecided = errors.New("directory: request already decided")
	ErrTooBig         = errors.New("directory: body too big")
	ErrFull           = errors.New("directory: too many live requests")
	ErrRateLimited    = wireauth.ErrRateLimited // the wire's sentinel, so callers need not import wireauth
)

const codeAlphabet = "0123456789abcdefghjkmnpqrstvwxyz" // Crockford base32: no i, l, o, u

var b64u = base64.RawURLEncoding

// NormalizeCode is the one spelling a code is looked up by.
func NormalizeCode(code string) string { return strings.ToLower(strings.TrimSpace(code)) }

// platforms are the values a platform may take; anything else is "other".
var platforms = map[string]bool{"darwin": true, "linux": true, "windows": true, "ios": true, "android": true, "other": true}

// platformOf keeps a known platform, maps an unknown one to "other" and leaves
// an unset one unset.
func platformOf(p string) string {
	switch {
	case p == "" || platforms[p]:
		return p
	}
	return "other"
}

// CheckOf is the four digits both ends show so a substituted key is visible.
func CheckOf(pubkey []byte) string {
	sum := sha256.Sum256(pubkey)
	return fmt.Sprintf("%04d", (int(sum[0])<<8|int(sum[1]))%10000)
}

// deviceOf is the device id of a public key, the same id identity.Cert gives.
func deviceOf(pubkey []byte) string {
	sum := sha256.Sum256(pubkey)
	return "dev_" + hex.EncodeToString(sum[:16])
}

// opened is the answer and the record for n made at now, or ErrBadRequest when n is not a
// request: wrong key lengths, a name that is too long or not sealed.
func (n NewRequest) opened(code string, now int64) (Opened, Request, error) {
	pub, ok1 := decodeKey(n.Pubkey)
	_, ok2 := decodeKey(n.X25519)
	if !ok1 || !ok2 || !sealedName(n.NameSealed) {
		return Opened{}, Request{}, ErrBadRequest
	}
	o := Opened{Code: code, Device: deviceOf(pub), Check: CheckOf(pub), RequestedAt: now, ExpiresAt: now + RequestTTL.Milliseconds()}
	r := Request{
		Code: code, Device: o.Device, Pubkey: n.Pubkey, X25519: n.X25519, NameSealed: n.NameSealed,
		Platform: platformOf(n.Platform), Check: o.Check, RequestedAt: now, ExpiresAt: o.ExpiresAt, State: RequestPending,
	}
	if r.Platform == "" {
		r.Platform = "other"
	}
	return o, r, nil
}

func decodeKey(s string) ([]byte, bool) {
	raw, err := b64u.DecodeString(s)
	return raw, err == nil && len(raw) == keySize
}

// sealedName says s is b64u of a sealed name: nonce, tag and at most maxName bytes.
func sealedName(s string) bool {
	raw, err := b64u.DecodeString(s)
	return err == nil && len(raw) >= sealedExtra && len(raw) <= sealedExtra+maxName
}

// Decision is one move of a request out of pending.
type Decision struct {
	State  RequestState
	Device string // the device the approval's certificate names; empty for a denial
	Grant  string
}

// errRepeat marks a decision that was already made exactly so.
var errRepeat = errors.New("directory: repeated decision")

// Settle says whether d may move r: nil when r is pending, errRepeat when r was
// already decided the same way, ErrAlreadyDecided otherwise. First decision wins.
func Settle(r Request, d Decision) error {
	switch {
	case r.State == RequestPending:
		return nil
	case r.State == d.State && d.sameBody(r):
		return errRepeat
	}
	return ErrAlreadyDecided
}

func (d Decision) sameBody(r Request) bool {
	return d.State == RequestDenied || (d.Device == r.Device && r.Grant != nil && d.Grant == *r.Grant)
}

// Decided is r after d.
func Decided(r Request, d Decision) Request {
	r.State = d.State
	if d.State == RequestApproved {
		r.Grant = &d.Grant
	}
	return r
}

// decisionOf checks an approval on its own and names what it decides: the
// grant fits, and the certificate decodes to a device.
func decisionOf(a Approval) (Decision, error) {
	if b64u.DecodedLen(len(a.Grant)) > MaxGrant {
		return Decision{}, ErrTooBig
	}
	raw, err := b64u.DecodeString(a.Cert)
	var cert identity.Cert
	if err != nil || json.Unmarshal(raw, &cert) != nil || a.Grant == "" {
		return Decision{}, ErrBadRequest
	}
	return Decision{State: RequestApproved, Device: cert.DeviceID(), Grant: a.Grant}, nil
}
