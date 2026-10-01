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
	{wireauth.ErrRateLimited, "rate_limited", http.StatusTooManyRequests},
	{wireauth.ErrTooManyIdentities, "too_many_identities", http.StatusTooManyRequests},
	{wireauth.ErrSkew, "skew", http.StatusUnauthorized},
	{ErrTooManyWatchers, "too_many_watchers", http.StatusTooManyRequests},
	{errUpgradeRequired, "upgrade_required", http.StatusUpgradeRequired},
	{errBadRequest, "bad_request", http.StatusBadRequest},
	{errTooLarge, "too_large", http.StatusRequestEntityTooLarge},
	// The link-pairing rows (docs/ux-pairing-contract.md). "gone" is 404 here
	// and 410 for a deleted identity, so a row is matched by code and status.
	{ErrRequestGone, "gone", http.StatusNotFound},
	{ErrAlreadyDecided, "already_decided", http.StatusConflict},
	{ErrTooBig, "too_big", http.StatusRequestEntityTooLarge},
	{ErrFull, "full", http.StatusServiceUnavailable},
}

// Codes that have no sentinel a Client returns: the wire's own refusals.
var (
	errBadRequest = ErrBadRequest
	errTooLarge   = errors.New("directory: request too large")
	// errUpgradeRequired answers a watch request that is not a WebSocket upgrade.
	errUpgradeRequired = errors.New("directory: upgrade required")
	errInternal        = errors.New("directory: internal error")
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

// errorOf turns a code and the status that carried it back into its sentinel,
// or nil when the pair is unknown.
func errorOf(code string, status int) error {
	for _, w := range wireErrors {
		if w.code == code && w.status == status {
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

// WatchPath is the signed WebSocket route that says the directory changed.
const WatchPath = dirBase + "/watch"

// rotationPath is the one signed verb for replacing an identity. It is the
// identity's, not the directory's, so it sits beside the directory's base.
const rotationPath = "/v1/identity/rotation"
