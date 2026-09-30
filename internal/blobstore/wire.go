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
	{"skew", http.StatusUnauthorized, wireauth.ErrSkew},
	{"revoked", http.StatusUnauthorized, wireauth.ErrRevoked},
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

type hasAnswer struct {
	Have []bool `json:"have"`
}

const (
	pathFrames  = "/v1/store/frames"
	pathObjects = "/v1/store/objects/"
	pathHas     = "/v1/store/has"
	pathStats   = "/v1/store/stats"

	// maxSmallBody bounds every request body that is not a frame: a Has
	// request of MaxHas ids is about 70 KB, so a megabyte is generous.
	maxSmallBody = 1 << 20

	// nowHeader carries the server's clock, in unix milliseconds, on every answer.
	nowHeader = "Codeaf-Now"
)

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // the wire types above cannot fail to marshal
	}
	return b
}
