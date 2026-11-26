// Package appcmd parses app commands without interpreting shell syntax.
package appcmd

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

const Prefix = "codeaf app "

// Parse accepts one verb and an optional JSON object or a parts intent.
// The caller's session provides scope, so arguments never select a shell
// or a working directory.
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
	if verb == "parts" && strings.HasPrefix(raw, "--for ") {
		args, err := PartsArgs(strings.TrimSpace(strings.TrimPrefix(raw, "--for ")))
		return verb, args, err
	}
	if raw == "" {
		raw = "{}"
	}
	// A model often quotes JSON as it would on a command line. Unwrap a
	// single literal without expanding variables or evaluating shell syntax.
	if len(raw) > 1 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		raw = raw[1 : len(raw)-1]
	} else if strings.HasPrefix(raw, "\"") {
		literal, err := strconv.Unquote(raw)
		if err != nil {
			return "", nil, errors.New("app command arguments must be one JSON object")
		}
		raw = literal
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

// PartsArgs translates the literal flag shared by the CLI and scoped command.
func PartsArgs(intent string) (json.RawMessage, error) {
	if !ValidIntent(intent) || intent == "" {
		return nil, errors.New("use codeaf app parts --for followed by an intent")
	}
	return json.Marshal(map[string]string{"for": intent})
}

// ValidIntent reports whether an intent is a literal catalog selector.
func ValidIntent(intent string) bool {
	return len(intent) <= 96 && strings.Trim(intent, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_/") == ""
}
