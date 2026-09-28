//go:build !windows

// Package jsonutil wraps encoding/json for the two encodings senior-dev uses
// everywhere: compact and two-space indented, both without HTML escaping so
// that `<`, `>` and `&` inside model-visible text stay readable.
package jsonutil

import (
	"bytes"
	"encoding/json"
)

// Marshal encodes v as compact JSON without escaping HTML characters.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// MarshalIndent encodes v as two-space indented JSON without escaping HTML
// characters.
func MarshalIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
