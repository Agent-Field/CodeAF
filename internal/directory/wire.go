package directory

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// wireError is one row of the wire's error table: the sentinel a Client
// returns, the code the body names it by, and the HTTP status that carries it.
type wireError struct {
	err    error
	code   string
	status int
}

// wireErrors is the single place a sentinel and its code meet, so the handler
// and the client cannot drift apart. Order matters only for errors.Is chains.
var wireErrors = []wireError{
	{ErrNotFound, "not_found", http.StatusNotFound},
	{ErrExists, "exists", http.StatusConflict},
	{ErrLeaseHeld, "lease_held", http.StatusConflict},
	{ErrFenceStale, "fence_stale", http.StatusConflict},
	{ErrHeadMoved, "head_moved", http.StatusConflict},
	{ErrCAS, "cas", http.StatusConflict},
	{ErrUnauthorized, "unauthorized", http.StatusUnauthorized},
	{ErrRevoked, "revoked", http.StatusUnauthorized},
	{ErrSelfRevoke, "self_revoke", http.StatusBadRequest},
	{ErrRotated, "rotated", http.StatusGone},
	{wireauth.ErrGone, "gone", http.StatusGone},
	{ErrRotationStep, "rotation_step", http.StatusConflict},
	{ErrBadGrace, "bad_grace", http.StatusBadRequest},
	{wireauth.ErrSkew, "skew", http.StatusUnauthorized},
	{errBadRequest, "bad_request", http.StatusBadRequest},
	{errTooLarge, "too_large", http.StatusRequestEntityTooLarge},
}

// Codes that have no sentinel a Client returns: the wire's own refusals.
var (
	errBadRequest = errors.New("directory: bad request")
	errTooLarge   = errors.New("directory: request too large")
	errInternal   = errors.New("directory: internal error")
)

// classify finds the table row for err; any error the table does not know is
// a server fault.
func classify(err error) wireError {
	for _, w := range wireErrors {
		if errors.Is(err, w.err) {
			return w
		}
	}
	return wireError{errInternal, "internal", http.StatusInternalServerError}
}

// errorOf turns a code back into its sentinel, or nil when the code is unknown.
func errorOf(code string) error {
	for _, w := range wireErrors {
		if w.code == code {
			return w.err
		}
	}
	return nil
}

// errBody is the JSON every refusal carries.
type errBody struct {
	Err string `json:"err"`
}

const dirBase = "/v1/dir"

// rotationPath is the one signed verb for replacing an identity. It is the
// identity's, not the directory's, so it sits beside the directory's base.
const rotationPath = "/v1/identity/rotation"
