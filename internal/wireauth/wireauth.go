// Package wireauth is the seam between the relay's HTTP wires and whatever
// proves who sent a request. The store and directory wires take these two
// function types and never learn how a request is signed, so they build and
// test without the signing package, and the relay binds the real one in one
// place.
package wireauth

import (
	"errors"
	"net/http"
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
