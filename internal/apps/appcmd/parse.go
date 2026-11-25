// Package appcmd parses app commands without interpreting shell syntax.
package appcmd

import (
	"encoding/json"
	"errors"
	"strings"
)

const Prefix = "codeaf app "

// Parse accepts one verb and an optional JSON object. Scope is supplied by
// the caller's session, so arguments never select a shell or a working directory.
func Parse(command string) (string, json.RawMessage, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(command), Prefix)
	if !ok {
		return "", nil, errors.New("use codeaf app followed by a verb and optional JSON")
	}
	verb, raw, _ := strings.Cut(strings.TrimSpace(rest), " ")
	if verb == "" || strings.Trim(verb, "abcdefghijklmnopqrstuvwxyz-_") != "" {
		return "", nil, errors.New("invalid app command verb")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}
	if !strings.HasPrefix(raw, "{") || !json.Valid([]byte(raw)) {
		return "", nil, errors.New("app command arguments must be one JSON object")
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", nil, err
	}
	for _, key := range []string{"draft", "app", "scope", "home", "actor", "job"} {
		if args[key] != nil {
			return "", nil, errors.New("app command scope comes from this session")
		}
	}
	return strings.ReplaceAll(verb, "-", "_"), json.RawMessage(raw), nil
}
