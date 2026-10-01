// Package wireauth is the seam between the relay's HTTP wires and whatever
// proves who sent a request. The store and directory wires take these two
// function types and never learn how a request is signed, so they build and
// test without the signing package, and the relay binds the real one in one
// place.
package wireauth

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sign stamps an outgoing request with proof of its sender. body is the exact
// request body, because the proof covers it.
type Sign func(r *http.Request, body []byte)

// Authenticate answers who sent an incoming request: the identity is the
// namespace every read and write is confined to, and the device is the one
// machine of that identity that signed it. Any error refuses the request.
type Authenticate func(r *http.Request, body []byte) (identity, device string, err error)

// ErrSkew is a refusal because the sender's clock is too far from the
// receiver's. It is kept apart from every other refusal because the person can
// fix it and must be told once, never retried silently.
var ErrSkew = errors.New("wireauth: clock skew")

// ErrUnauthorized is every other refusal.
var ErrUnauthorized = errors.New("wireauth: unauthorized")

// ErrRevoked is a refusal of a device whose identity has stopped it. It is kept
// apart from every other refusal because retrying can never help: only a fresh
// pairing, which makes a new device key, brings the machine back.
var ErrRevoked = errors.New("wireauth: device revoked")

// ErrGone is a refusal because the identity was replaced and the relay has
// deleted it. Nothing is left to retry against, so it is kept apart from every
// other refusal: only pairing with the new identity brings a machine back.
var ErrGone = errors.New("wireauth: this identity was replaced and deleted")

// Narrow reduces any authentication failure to the four the wires name: skew,
// revoked and gone, which the person can act on, and the rest. Both wires call it,
// so they cannot disagree about what a refusal is.
func Narrow(err error) error {
	for _, named := range []error{ErrSkew, ErrRevoked, ErrGone} {
		if errors.Is(err, named) {
			return named
		}
	}
	return ErrUnauthorized
}

// ErrRotated is a refusal to write to an identity that has been replaced by a
// rotation: what it holds is read-only until the relay deletes it. It is not an
// authentication failure (the device is who it says), so it is kept out of
// Narrow; the two wires map it to the same answer and a person is told to pair
// again.
var ErrRotated = errors.New("wireauth: this identity was replaced by a rotation")

// ErrRateLimited is a refusal because the identity or one of its computers asked
// too often. Time alone cures it, so the wire carries how long the relay says
// to wait (see Wait).
var ErrRateLimited = errors.New("wireauth: rate limited")

// ErrTooManyIdentities is a refusal because one network has brought in more new
// identities today than the relay allows; like ErrRateLimited it is cured by
// time, but by hours and not seconds.
var ErrTooManyIdentities = errors.New("wireauth: too many new identities")

// waiting is a refusal together with how long the relay said to wait.
type waiting struct {
	err   error
	after time.Duration
}

func (w waiting) Error() string { return w.err.Error() }
func (w waiting) Unwrap() error { return w.err }

// Wait attaches the Retry-After of an answer to the refusal it came with, so
// every wire keeps the relay's word in one shape. An answer without a usable
// Retry-After leaves the refusal as it was.
func Wait(err error, h http.Header) error {
	secs, perr := strconv.Atoi(h.Get("Retry-After"))
	if perr != nil || secs <= 0 {
		return err
	}
	return waiting{err: err, after: time.Duration(secs) * time.Second}
}

// After is how long the relay said to wait before asking again, or zero when
// err carries no such word.
func After(err error) time.Duration {
	var w waiting
	if errors.As(err, &w) {
		return w.after
	}
	return 0
}

// Endpoint is the address of a route on the relay at base. The base may carry a
// path of its own, as the hosted relay does (https://host/fabric), and may or
// may not end in a slash; the route always starts with one. Every client builds
// its addresses here, so a base path works for the store, the directory, the
// watch, pairing and rotation alike. The route is joined as text, not re-parsed,
// because it is already escaped and may carry a query, and the request signature
// covers exactly the bytes sent.
func Endpoint(base, route string) string {
	return strings.TrimRight(base, "/") + route
}
