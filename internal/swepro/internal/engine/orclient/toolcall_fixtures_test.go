package orclient

// Replays testdata/toolcall-fixtures.json, produced by
// tools/fixtures/gen-ortoolcall.ts by driving `streamText` from ai@6.0.168 over
// a MockLanguageModelV3, with codeaf's REAL repair callback
// (src/session/llm.ts:427-447).
//
// ENGINE-DESIGN §3.3a's three stages are all pinned here: validation (including
// "an empty input string validates {}"), the mis-cased-name repair, the rewrite
// to the `invalid` tool, and — the one that is easiest to miss — the SAFETY NET
// where a repaired call fails a SECOND time and the result is a synthetic
// `invalid:true` call carrying the ORIGINAL tool name and the RAW input string.
//
// One normalisation, applied identically here and in the generator: the text of
// a JSON parse failure is engine-specific (JavaScriptCore vs encoding/json), so
// it collapses to `<JSON_PARSE_ERROR>`. Everything up to that point — including
// the whole `Model tried to call unavailable tool …` message and the whole
// `Type validation failed: Value: … Error message: …` message — is compared
// byte-for-byte.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type toolCallArgs struct {
	Tools []string `json:"tools"`
	Call  struct {
		ToolCallID string `json:"toolCallId"`
		ToolName   string `json:"toolName"`
		Input      string `json:"input"`
	} `json:"call"`
	Validate map[string]string `json:"validate"`
}

// requireCmd mirrors the generator's one JSON-transportable validator spec.
func requireCmd(input json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err == nil {
		if raw, ok := obj["cmd"]; ok {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				return nil
			}
		}
	}
	return NewTypeValidationError(input, errors.New("cmd must be a string"))
}

func normalizeJSONParseError(message string) string {
	at := strings.Index(message, "JSON parsing failed:")
	if at < 0 {
		return message
	}
	return message[:at] + "<JSON_PARSE_ERROR>"
}

func TestToolCallFixtures(t *testing.T) {
	cases := loadFixtures(t, "toolcall-fixtures.json")
	if len(cases) < 15 {
		t.Fatalf("toolcall fixture corpus looks truncated: %d cases", len(cases))
	}
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			if c.Fn != "parseToolCall" {
				t.Fatalf("unexpected fn %q", c.Fn)
			}
			var args toolCallArgs
			decodeArgs(t, c.ArgsJSON, &args)

			specs := make([]ToolSpec, 0, len(args.Tools))
			for _, name := range args.Tools {
				spec := ToolSpec{Name: name}
				if args.Validate[name] == "requireCmd" {
					spec.Validate = requireCmd
				}
				specs = append(specs, spec)
			}
			tools := NewToolMap(specs...)

			got := ParseToolCall(RawToolCall{
				ToolCallID: args.Call.ToolCallID,
				ToolName:   args.Call.ToolName,
				Input:      args.Call.Input,
			}, tools, CodeafRepairToolCall)

			// Project to the generator's recorded shape.
			out := NewObject()
			out.SetString("toolName", got.ToolName)
			input := got.Input
			if got.ToolName == InvalidToolName {
				input = normalizeInvalidInput(t, input)
			}
			if err := out.Set("input", input); err != nil {
				t.Fatalf("set input: %v", err)
			}
			if got.Invalid {
				out.SetBool("invalid", true)
				message := ""
				if got.Error != nil {
					message = got.Error.Error()
				}
				out.SetString("error", normalizeJSONParseError(message))
			}
			assertEqual(t, stringify(t, out), c.OutJSON)
		})
	}
}

// normalizeInvalidInput applies the JSON-parse-error normalisation INSIDE the
// repaired `invalid` tool's input, where the failure message is embedded.
func normalizeInvalidInput(t *testing.T, input json.RawMessage) json.RawMessage {
	t.Helper()
	obj, err := ParseObject(input)
	if err != nil {
		return input
	}
	raw, ok := obj.Get("error")
	if !ok {
		return input
	}
	var message string
	if err := json.Unmarshal(raw, &message); err != nil {
		return input
	}
	obj.SetString("error", normalizeJSONParseError(message))
	encoded, err := obj.MarshalJSON()
	if err != nil {
		t.Fatalf("re-encode invalid input: %v", err)
	}
	return encoded
}
