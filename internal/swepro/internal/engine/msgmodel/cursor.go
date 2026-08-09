// Cursor encoding ports src/session/message-v2.ts:660-676. JSON key order is
// id then time, and Node's "base64url" encoding is unpadded RFC 4648 URL base64.
package msgmodel

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type Cursor struct {
	ID   string            `json:"id"`
	Time jscompat.JSNumber `json:"time"`
}

func EncodeCursor(input Cursor) (string, error) {
	raw, err := jscompat.Stringify(input)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCursor(input string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(input)
	if err != nil {
		return Cursor{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Cursor{}, err
	}
	idRaw, idOK := fields["id"]
	timeRaw, timeOK := fields["time"]
	if !idOK || !timeOK || bytes.Equal(bytes.TrimSpace(idRaw), []byte("null")) ||
		bytes.Equal(bytes.TrimSpace(timeRaw), []byte("null")) {
		return Cursor{}, errors.New("msgmodel: cursor requires id and time")
	}
	var cursor Cursor
	if err := json.Unmarshal(idRaw, &cursor.ID); err != nil {
		return Cursor{}, err
	}
	if err := json.Unmarshal(timeRaw, &cursor.Time); err != nil {
		return Cursor{}, err
	}
	if err := validateCursor(cursor); err != nil {
		return Cursor{}, err
	}
	return cursor, nil
}

func validateCursor(cursor Cursor) error {
	n := float64(cursor.Time)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return errors.New("msgmodel: cursor time must be finite and non-negative")
	}
	return nil
}
