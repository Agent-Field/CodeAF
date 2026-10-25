package appwire

import (
	"bytes"
	"encoding/json"
)

// Call runs one in-process request through the desktop's method table. It does
// not own the engine's lifetime; shutdown belongs to the connection loop alone.
func Call(engine Engine, name string, params json.RawMessage) (any, error) {
	m, ok := methods[name]
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "there is no method named %q", name)
	}
	params = bytes.TrimSpace(params)
	if len(params) == 0 || bytes.Equal(params, []byte("null")) {
		params = json.RawMessage(`{}`)
	}
	if params[0] != '{' || !json.Valid(params) {
		return nil, Errorf(CodeBadParams, "params must be an object")
	}
	return m.call(engine, params)
}
