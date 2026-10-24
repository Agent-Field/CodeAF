package pairbox

import (
	"errors"
	"net/http"
	"strconv"
	"time"
)

// The routes and header of the wire (contract 18.4). Both relays serve exactly
// these and neither adds a header, a field or a status.
const (
	// Path is the collection: POST creates a mailbox.
	Path = "/v1/pair"
	// KeyHeader carries the side key, base64url.
	KeyHeader = "Codeaf-Pair-Key"
	// GenHeader names one opening of a nameplate. A relay that fences says it in
	// every answer about a mailbox, a device sends back the one it was told, and
	// a mailbox that is a later opening of the same nameplate answers 404 gone.
	// It is optional both ways, so a relay or a device without it still works.
	GenHeader = "Codeaf-Pair-Gen"
	// PeerHeader is where a device may say which network it comes from; a relay
	// honours it only behind a proxy it was told to trust.
	PeerHeader = "X-Forwarded-For"
)

// The bodies of the wire.
type (
	createdBody struct {
		Nameplate string `json:"nameplate"`
		ExpiresMS int64  `json:"expires_in_ms"`
	}
	postedBody struct {
		N int `json:"n"`
	}
	batchBody struct {
		Msgs [][]byte `json:"msgs"`
		Next int      `json:"next"`
	}
	limitsBody struct {
		Limits
		TTLMS int64 `json:"ttl_ms"`
	}
	errorBody struct {
		Err string `json:"err"`
	}
)

// failure is one refusal, as a status and the word a body carries.
type failure struct {
	err    error
	status int
	word   string
}

// failures is the table both directions read: the handler to choose a status
// for an error, and the client to choose an error for a status. Order matters
// only for the client, where two rows never share a status but 409 and 503
// share the word "full" with distinct statuses.
var failures = []failure{
	{ErrForbidden, http.StatusForbidden, "forbidden"},
	{ErrGone, http.StatusNotFound, "gone"},
	{ErrSideFull, http.StatusConflict, "full"},
	{ErrTooBig, http.StatusRequestEntityTooLarge, "too big"},
	{ErrRelayFull, http.StatusServiceUnavailable, "full"},
	{ErrRateLimited, http.StatusTooManyRequests, "slow down"},
}

// statusOf is the row for an error, or a plain 500 for one no row names.
func statusOf(err error) failure {
	for _, f := range failures {
		if errors.Is(err, f.err) {
			return f
		}
	}
	return failure{err: err, status: http.StatusInternalServerError, word: "error"}
}

// errorFor is the error a status stands for.
func errorFor(status int) (error, bool) {
	for _, f := range failures {
		if f.status == status {
			return f.err, true
		}
	}
	return nil, false
}

// retryAfter reads Retry-After seconds, and is a second when it is absent.
func retryAfter(h http.Header) time.Duration {
	secs, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || secs < 1 {
		return time.Second
	}
	return time.Duration(secs) * time.Second
}
