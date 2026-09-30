package blobstore

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// The wire vocabulary is one table read by both ends, so a code the handler
// can send is a code the client can read: adding an error means adding a row,
// and the two sides cannot drift apart.
var wireErrors = []struct {
	code   string
	status int
	err    error
}{
	{"bad_frame", http.StatusBadRequest, ErrBadFrame},
	{"bad_rid", http.StatusBadRequest, ErrBadRID},
	{"too_many", http.StatusBadRequest, ErrTooMany},
	{"not_found", http.StatusNotFound, ErrNotFound},
	{"conflict", http.StatusConflict, ErrConflict},
	{"full", http.StatusInsufficientStorage, ErrFull},
	{"damaged", http.StatusInternalServerError, ErrDamaged},
	{"unreachable", http.StatusServiceUnavailable, ErrUnreachable},
	{"rate_limited", http.StatusTooManyRequests, wireauth.ErrRateLimited},
	{"too_many_identities", http.StatusTooManyRequests, wireauth.ErrTooManyIdentities},
	{"skew", http.StatusUnauthorized, wireauth.ErrSkew},
	{"revoked", http.StatusUnauthorized, wireauth.ErrRevoked},
	{"rotated", http.StatusGone, wireauth.ErrRotated},
	{"gone", http.StatusGone, wireauth.ErrGone},
	{"unauthorized", http.StatusUnauthorized, wireauth.ErrUnauthorized},
}

// codeOf finds the row for err; ok is false for an error the wire has no name for.
func codeOf(err error) (code string, status int, ok bool) {
	for _, row := range wireErrors {
		if errors.Is(err, row.err) {
			return row.code, row.status, true
		}
	}
	return "", 0, false
}

// errOf is the inverse of codeOf: the sentinel a code names, or nil.
func errOf(code string) error {
	for _, row := range wireErrors {
		if row.code == code {
			return row.err
		}
	}
	return nil
}

// errBody is the JSON every failure carries.
type errBody struct {
	Err string `json:"err"`
	// LimitBytes is the byte ceiling a full relay names; absent when it has none.
	LimitBytes int64 `json:"limit_bytes,omitempty"`
}

// ceiling is a refusal together with the byte ceiling the relay named.
type ceiling struct {
	err   error
	bytes int64
}

func (c ceiling) Error() string { return c.err.Error() }
func (c ceiling) Unwrap() error { return c.err }

// Capped attaches a byte ceiling to err. A store that has a cap returns its ErrFull this way and the
// handler tells the client; the client attaches the ceiling it read. A ceiling of zero means the relay
// named none, and err is left alone.
func Capped(err error, bytes int64) error {
	if bytes <= 0 {
		return err
	}
	return ceiling{err: err, bytes: bytes}
}

// LimitOf is the byte ceiling the relay named with err, or zero when it named none.
func LimitOf(err error) int64 {
	var c ceiling
	if errors.As(err, &c) {
		return c.bytes
	}
	return 0
}

// statsBody is the answer to GET /v1/store/stats.
type statsBody struct {
	Puts     int64 `json:"puts"`
	Gets     int64 `json:"gets"`
	Has      int64 `json:"has"`
	BytesIn  int64 `json:"bytes_in"`
	BytesOut int64 `json:"bytes_out"`
}

type putAnswer struct {
	Frame   string `json:"frame"`
	Objects int    `json:"objects"`
}

type hasRequest struct {
	Rids []string `json:"rids"`
}

// getManyRequest names the objects a GetMany asks for; the answer is a frame.
type getManyRequest = hasRequest

type hasAnswer struct {
	Have []bool `json:"have"`
}

// locateRequest names the rids a Locate asks about; locateAnswer answers where
// each held one lies, keyed by rid.
type locateRequest = hasRequest

type locateAnswer struct {
	At map[string]Location `json:"at"`
}

const (
	pathFrames  = "/v1/store/frames"
	pathObjects = "/v1/store/objects/"
	pathMany    = "/v1/store/objects"
	pathHas     = "/v1/store/has"
	pathLocate  = "/v1/store/locate"
	pathStats   = "/v1/store/stats"

	// maxSmallBody bounds every request body that is not a frame: a Has
	// request of MaxHas ids is about 70 KB, so a megabyte is generous.
	maxSmallBody = 1 << 20

	// nowHeader carries the server's clock, in unix milliseconds, on every answer.
	nowHeader = "Codeaf-Now"
)

// manyKeyID is the cell key id of the frame that answers a GetMany. A frame
// needs one, and this answer belongs to no cell, so it is all zeros.
const manyKeyID = "00000000000000000000000000000000"

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // the wire types above cannot fail to marshal
	}
	return b
}
