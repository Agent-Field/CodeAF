package workgraph

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// A CURSOR IS A POSITION, NOT AN OFFSET, AND IT IS SIGNED. It names the creation
// key of the last task a page ended on (or the last cluster), together with the
// scope, the view and the filter it was issued for, so tasks appended between
// two pages cannot shift what the next page holds. It is signed with a key made
// when the process started: a cursor that was edited, or issued by another
// process, or issued for another request, is refused rather than followed to a
// position nobody chose.
var cursorKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("workgraph: no randomness for the cursor key")
	}
	return key
}()

// cursorBody is what a cursor says.
type cursorBody struct {
	Scope  string `json:"s"`
	View   string `json:"v"`
	Filter string `json:"f,omitempty"`
	At     int64  `json:"a,omitempty"`
	Owner  string `json:"o,omitempty"`
	Seq    uint64 `json:"q,omitempty"`
	Task   string `json:"t,omitempty"`
	Key    string `json:"k,omitempty"`
}

func mac(payload []byte) []byte {
	h := hmac.New(sha256.New, cursorKey)
	h.Write(payload)
	return h.Sum(nil)[:16]
}

func encodeCursor(b cursorBody) string {
	payload, _ := json.Marshal(b)
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(payload))
}

// decodeCursor is the position a cursor names, or [ErrBadRequest] when it was
// not issued by this process for this very request.
func decodeCursor(raw string, want cursorBody) (cursorBody, error) {
	enc := base64.RawURLEncoding
	body, sig, ok := strings.Cut(raw, ".")
	if !ok || len(raw) > 1024 {
		return cursorBody{}, badf("the cursor is not one this engine issued")
	}
	payload, err1 := enc.DecodeString(body)
	given, err2 := enc.DecodeString(sig)
	if err1 != nil || err2 != nil || !hmac.Equal(mac(payload), given) {
		return cursorBody{}, badf("the cursor is not one this engine issued")
	}
	var got cursorBody
	if err := json.Unmarshal(payload, &got); err != nil {
		return cursorBody{}, badf("the cursor is not one this engine issued")
	}
	if got.Scope != want.Scope || got.View != want.View || got.Filter != want.Filter {
		return cursorBody{}, badf("the cursor was issued for a different request")
	}
	return got, nil
}
