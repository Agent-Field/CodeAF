//go:build !windows

package msgmodel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// Tests for the seams and byte-level rules of the message model.

func ptrBool(b bool) *bool         { return &b }
func ptrU64(v uint64) *uint64      { return &v }
func ptrFloat(f float64) *float64  { return &f }
func ptrString(s string) *string   { return &s }
func raw(s string) json.RawMessage { return json.RawMessage(s) }
func rawObj(s string) RawObject    { return RawObject(s) }
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := jsonutil.Marshal(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
}

// ── DifferentModel ───────────────────────────────────────────────────────

func TestDifferentModel(t *testing.T) {
	cases := []struct {
		name       string
		model      Model
		assistant  Assistant
		wantDiffer bool
	}{
		{
			name:      "identical",
			model:     Model{ProviderID: "openrouter", ID: "acme/model-pro"},
			assistant: Assistant{ProviderID: "openrouter", ModelID: "acme/model-pro"},
		},
		{
			name:       "different model id",
			model:      Model{ProviderID: "openrouter", ID: "acme/model-max"},
			assistant:  Assistant{ProviderID: "openrouter", ModelID: "acme/model-pro"},
			wantDiffer: true,
		},
		{
			name:       "different provider id",
			model:      Model{ProviderID: "anthropic", ID: "m"},
			assistant:  Assistant{ProviderID: "openrouter", ModelID: "m"},
			wantDiffer: true,
		},
		{
			// The check is `${a}/${b}` string concatenation, so a slash inside
			// either half can make two distinct pairs compare EQUAL.
			name:      "slash split ambiguity compares equal",
			model:     Model{ProviderID: "a", ID: "b/c"},
			assistant: Assistant{ProviderID: "a/b", ModelID: "c"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DifferentModel(tc.model, tc.assistant); got != tc.wantDiffer {
				t.Fatalf("DifferentModel = %v, want %v", got, tc.wantDiffer)
			}
		})
	}
}

// ── TruncateToolOutput ───────────────────────────────────────────────────

func TestTruncateToolOutput(t *testing.T) {
	if got := TruncateToolOutput("abcdef", nil); got != "abcdef" {
		t.Fatalf("nil maxChars: %q", got)
	}
	if got := TruncateToolOutput("abcdef", ptrFloat(0)); got != "abcdef" {
		t.Fatalf("falsy 0 maxChars: %q", got)
	}
	if got := TruncateToolOutput("abcde", ptrFloat(5)); got != "abcde" {
		t.Fatalf("exact length: %q", got)
	}
	want := "abc\n[Tool output truncated for compaction: omitted 3 chars]"
	if got := TruncateToolOutput("abcdef", ptrFloat(3)); got != want {
		t.Fatalf("truncated:\n got %q\nwant %q", got, want)
	}
	// The limit counts characters, never bytes, so a multi-byte character is
	// kept whole.
	got := TruncateToolOutput("a\U0001F600b", ptrFloat(2))
	if !strings.HasPrefix(got, "a\U0001F600\n") {
		t.Fatalf("character cut: %q", got)
	}
	if !strings.HasSuffix(got, "omitted 1 chars]") {
		t.Fatalf("omitted count should count characters: %q", got)
	}
}

// ── opaque JSON ──────────────────────────────────────────────────────────

func TestRawObjectPreservesKeyOrderAndEmptyObject(t *testing.T) {
	part := ToolPart{
		PartBase: PartBase{ID: "p", SessionID: "s", MessageID: "m"},
		CallID:   "c",
		Tool:     "bash",
		State: ToolStateCompleted{
			Input:    rawObj(`{"zulu":1,"alpha":2,"0":3}`),
			Output:   "o",
			Title:    "t",
			Metadata: nil, // required field: must serialise as {}
			Time:     ToolTimeCompleted{Start: 1, End: 2},
		},
	}
	got := mustJSON(t, part)
	want := `{"id":"p","sessionID":"s","messageID":"m","type":"tool","callID":"c","tool":"bash",` +
		`"state":{"status":"completed","input":{"zulu":1,"alpha":2,"0":3},"output":"o","title":"t","metadata":{},"time":{"start":1,"end":2}}}`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestOptionalRawObjectIsOmittedWhenAbsentAndKeptWhenEmpty(t *testing.T) {
	absent := TextPart{PartBase: PartBase{ID: "p", SessionID: "s", MessageID: "m"}, Text: "x"}
	if got := mustJSON(t, absent); strings.Contains(got, "metadata") {
		t.Fatalf("absent metadata should be omitted: %s", got)
	}
	empty := absent
	empty.Metadata = rawObj("{}")
	if got := mustJSON(t, empty); !strings.Contains(got, `"metadata":{}`) {
		t.Fatalf("explicit {} metadata should survive: %s", got)
	}
}

func TestStringifyDoesNotEscapeHTMLInsideParts(t *testing.T) {
	part := TextPart{PartBase: PartBase{ID: "p", SessionID: "s", MessageID: "m"}, Text: "<b>&</b>"}
	if got := mustJSON(t, part); !strings.Contains(got, `"<b>&</b>"`) {
		t.Fatalf("HTML should not be escaped: %s", got)
	}
}

func TestMarshalForcesTheDiscriminant(t *testing.T) {
	// A hand-built value with no Type set must still carry its tag.
	if got := mustJSON(t, StepStartPart{}); !strings.Contains(got, `"type":"step-start"`) {
		t.Fatalf("step-start tag missing: %s", got)
	}
	if got := mustJSON(t, ToolStateError{}); !strings.Contains(got, `"status":"error"`) {
		t.Fatalf("error status missing: %s", got)
	}
	if got := mustJSON(t, Assistant{}); !strings.Contains(got, `"role":"assistant"`) {
		t.Fatalf("assistant role missing: %s", got)
	}
}

// ── providerMeta ─────────────────────────────────────────────────────────

func TestProviderMeta(t *testing.T) {
	cases := []struct {
		name string
		in   RawObject
		want string
	}{
		{"absent", nil, ""},
		{"empty object", rawObj(`{}`), ""},
		{"only providerExecuted", rawObj(`{"providerExecuted":true}`), ""},
		{"strips and preserves order", rawObj(`{"zeta":1,"providerExecuted":true,"alpha":2}`), `{"zeta":1,"alpha":2}`},
		{"nothing to strip", rawObj(`{"a":{"b":[1,2]}}`), `{"a":{"b":[1,2]}}`},
		{"non-object", rawObj(`"str"`), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := providerMeta(tc.in)
			if string(got) != tc.want {
				t.Fatalf("providerMeta = %q, want %q", got, tc.want)
			}
		})
	}
}

// ── doom-loop key ────────────────────────────────────────────────────────

func TestSameInputIsStringifyEqualityNotDeepEquality(t *testing.T) {
	if !SameInput(rawObj(`{"a":1,"b":2}`), rawObj(`{"a":1, "b":2}`)) {
		t.Fatal("insignificant whitespace must not matter")
	}
	if SameInput(rawObj(`{"a":1,"b":2}`), rawObj(`{"b":2,"a":1}`)) {
		t.Fatal("key ORDER is load-bearing: the stored bytes differ, so the guard must not fire")
	}
	if !SameInput(nil, rawObj(`{}`)) {
		t.Fatal("absent input reads as {}")
	}
}

// ── tool-part settlement ─────────────────────────────────────────────────

func TestPendingToolState(t *testing.T) {
	got := mustJSON(t, PendingToolState())
	if got != `{"status":"pending","input":{},"raw":""}` {
		t.Fatalf("pending literal: %s", got)
	}
	if _, ok := PendingToolState().StartTime(); ok {
		t.Fatal("pending has no time at all")
	}
}

func TestSpreadAbortedToolStateCarriesPreviousFields(t *testing.T) {
	// The spread carries the previous state's fields, so `raw` survives into
	// an object ToolStateError does not declare.
	got, err := SpreadAbortedToolState(PendingToolState(), 5)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"status":"error","input":{},"raw":"","error":"Tool execution aborted","metadata":{"interrupted":true},"time":{"start":5,"end":5}}`
	if string(got) != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}

	running := ToolStateRunning{Input: rawObj(`{}`), Title: ptrString("bash"), Time: ToolTimeStart{Start: 3}}
	got, err = SpreadAbortedToolState(running, 9)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"title":"bash"`) {
		t.Fatalf("running title should carry through the spread: %s", got)
	}
	if !strings.Contains(string(got), `"time":{"start":3,"end":9}`) {
		t.Fatalf("time should be overwritten in place: %s", got)
	}
}

func TestSpreadObjectKeyPositions(t *testing.T) {
	got := SpreadObject(rawObj(`{"a":1,"b":2}`),
		RawField{Key: "b", Value: raw("9")},
		RawField{Key: "c", Value: raw("3")},
	)
	if string(got) != `{"a":1,"b":9,"c":3}` {
		t.Fatalf("spread = %s", got)
	}
	if string(SpreadObject(nil)) != "{}" {
		t.Fatal("empty spread should be {}")
	}
}

func TestToolPartProviderExecutedIsTruthyNotStrict(t *testing.T) {
	cases := map[string]bool{
		`{"providerExecuted":true}`:  true,
		`{"providerExecuted":"yes"}`: true,
		`{"providerExecuted":1}`:     true,
		`{"providerExecuted":false}`: false,
		`{"providerExecuted":0}`:     false,
		`{"providerExecuted":""}`:    false,
		`{"providerExecuted":null}`:  false,
		`{}`:                         false,
	}
	for meta, want := range cases {
		part := ToolPart{Metadata: rawObj(meta)}
		if got := part.ProviderExecuted(); got != want {
			t.Fatalf("%s → %v, want %v", meta, got, want)
		}
	}
}

// ── staticToolName ───────────────────────────────────────────────────────

func TestStaticToolNamePreservesInternalDashes(t *testing.T) {
	cases := map[string]string{
		"tool-bash":            "bash",
		"tool-multi-word-name": "multi-word-name",
		"tool-":                "",
		"nodash":               "",
	}
	for typ, want := range cases {
		if got := staticToolName(typ); got != want {
			t.Fatalf("%s → %q, want %q", typ, got, want)
		}
	}
}

// ── the synthetic-message seam ────────────────────────────────────────────

func TestSetMessageIDFactoryForTesting(t *testing.T) {
	restore := SetMessageIDFactoryForTesting(func() string { return "msg_pinned" })
	if messageIDAscending() != "msg_pinned" {
		t.Fatal("factory not installed")
	}
	restore()
	if messageIDAscending() == "msg_pinned" {
		t.Fatal("restore did not undo the swap")
	}
}

// ── the synthetic attachment message ─────────────────────────────────────

func TestSupportsMediaInToolResultByProvider(t *testing.T) {
	// supportsMediaInToolResult has no @openrouter case, so every media
	// attachment on the OpenRouter path is extracted into the synthetic user
	// message rather than staying in the tool result.
	if supportsMediaInToolResult(Model{API: ModelAPI{Npm: "@openrouter/ai-sdk-provider"}}, "image/png") {
		t.Fatal("openrouter must not support media in tool results")
	}
	if !supportsMediaInToolResult(Model{API: ModelAPI{Npm: "@ai-sdk/amazon-bedrock"}}, "image/png") {
		t.Fatal("bedrock supports images")
	}
	if supportsMediaInToolResult(Model{API: ModelAPI{Npm: "@ai-sdk/amazon-bedrock"}}, "application/pdf") {
		t.Fatal("bedrock does not support pdfs")
	}
	// The gemini case lowercases first and requires gemini-3 AND not gemini-2.
	if !supportsMediaInToolResult(Model{API: ModelAPI{Npm: "@ai-sdk/google", ID: "GEMINI-3-PRO"}}, "image/png") {
		t.Fatal("gemini-3 is case-insensitive")
	}
	if supportsMediaInToolResult(Model{API: ModelAPI{Npm: "@ai-sdk/google", ID: "gemini-3-and-gemini-2"}}, "image/png") {
		t.Fatal("a gemini-2 substring disqualifies")
	}
}

// ── FilterCompacted returns values the caller may mutate ──────────────────

func TestFilterCompactedDoesNotAliasTheInputSlice(t *testing.T) {
	in := []WithParts{
		{Info: User{MessageBase: MessageBase{ID: "u2"}}, Parts: Parts{}},
		{Info: User{MessageBase: MessageBase{ID: "u1"}}, Parts: Parts{}},
	}
	out := FilterCompacted(in)
	if len(out) != 2 || out[0].Info.MessageID() != "u1" || out[1].Info.MessageID() != "u2" {
		t.Fatalf("expected chronological order, got %v", []string{out[0].Info.MessageID(), out[1].Info.MessageID()})
	}
	if in[0].Info.MessageID() != "u2" {
		t.Fatal("FilterCompacted must not reverse the caller's slice in place")
	}
}

// ── ToModelMessages seam smoke test ───────────────────────────────────────

func TestToModelMessagesIsMediaClassification(t *testing.T) {
	for mime, want := range map[string]bool{
		"image/png":               true,
		"image/svg+xml":           true,
		"application/pdf":         true,
		"text/plain":              false,
		"application/x-directory": false,
	} {
		if got := IsMedia(mime); got != want {
			t.Fatalf("IsMedia(%q) = %v", mime, got)
		}
	}
}

func TestUnknownUnionTagsAreErrors(t *testing.T) {
	if _, err := UnmarshalPart([]byte(`{"type":"nope"}`)); err == nil {
		t.Fatal("expected an error for an unknown part type")
	}
	if _, err := UnmarshalToolState([]byte(`{"status":"nope"}`)); err == nil {
		t.Fatal("expected an error for an unknown tool status")
	}
	if _, err := UnmarshalInfo([]byte(`{"role":"tool"}`)); err == nil {
		t.Fatal("expected an error for an unknown message role")
	}
}

func TestAssistantErrorConstructors(t *testing.T) {
	if got := mustJSON(t, NewMessageAbortedError("stopped")); got != `{"name":"MessageAbortedError","data":{"message":"stopped"}}` {
		t.Fatalf("aborted: %s", got)
	}
	if got := mustJSON(t, NewMessageOutputLengthError()); got != `{"name":"MessageOutputLengthError","data":{}}` {
		t.Fatalf("output length: %s", got)
	}
	api := NewAPIError(APIError{Message: "boom", StatusCode: ptrU64(429), IsRetryable: true, ResponseBody: ptrString(`{"e":1}`)})
	want := `{"name":"APIError","data":{"message":"boom","statusCode":429,"isRetryable":true,"responseBody":"{\"e\":1}"}}`
	if got := mustJSON(t, api); got != want {
		t.Fatalf("api:\n got %s\nwant %s", got, want)
	}
	if api.IsAborted() {
		t.Fatal("APIError must not report as an abort")
	}
	aborted := NewMessageAbortedError("x")
	if !aborted.IsAborted() {
		t.Fatal("MessageAbortedError must report as an abort")
	}
	var nilErr *AssistantError
	if nilErr.IsAborted() {
		t.Fatal("nil error is not an abort")
	}
}

func TestSummaryAndBoolPointerHelpers(t *testing.T) {
	if boolValue(nil) || !boolValue(ptrBool(true)) || boolValue(ptrBool(false)) {
		t.Fatal("boolValue")
	}
}

// `upstream` on a step-finish part is present only when the provider reported
// an endpoint; a record without one marshals without the key.
func TestStepFinishUpstreamIsOptionalAndRoundTrips(t *testing.T) {
	const withUpstream = `{"id":"p","sessionID":"s","messageID":"m","type":"step-finish","reason":"stop","cost":0,"tokens":{"input":10,"output":1,"reasoning":0,"cache":{"read":0,"write":0}},"upstream":"provider-b"}`
	part, err := UnmarshalPart([]byte(withUpstream))
	if err != nil {
		t.Fatal(err)
	}
	finish, ok := part.(StepFinishPart)
	if !ok || finish.Upstream != "provider-b" {
		t.Fatalf("decoded part = %#v", part)
	}
	if got := mustJSON(t, part); got != withUpstream {
		t.Fatalf("step-finish with upstream changed shape:\n got %s\nwant %s", got, withUpstream)
	}
	finish.Upstream = ""
	if got := mustJSON(t, finish); strings.Contains(got, "upstream") {
		t.Fatalf("an unreported upstream must not be serialized: %s", got)
	}
}
