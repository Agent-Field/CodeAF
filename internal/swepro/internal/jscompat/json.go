package jscompat

import (
	"bytes"
	"encoding/json"
)

// Stringify mirrors JSON.stringify(v): compact output, no HTML escaping of
// < > &, no trailing newline.
//
// Known residual divergences from V8 (accepted, revisit if a differential run
// hits them): Go escapes U+2028/U+2029 inside strings where JS emits them
// literally, and Go marshals map[string]any keys in sorted order where JS
// preserves insertion order (typed structs are unaffected — fields marshal in
// declaration order, which the port keeps aligned with the TS object
// literals).
func Stringify(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// StringifyIndent mirrors JSON.stringify(v, null, 2).
func StringifyIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
