package msgmodel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// Go-only tests. There is no `message-v2.test.ts` in swe-pro @3b25a1a (the only
// session-level suite that touches this surface is retry.test.ts, which belongs
// to engine/retry), so nothing here is a verbatim translation — these pin the
// seams and the fidelity notes the fixture corpus cannot express.

func ptrBool(b bool) *bool         { return &b }
func ptrU64(v uint64) *uint64      { return &v }
func ptrFloat(f float64) *float64  { return &f }
func ptrString(s string) *string   { return &s }
func raw(s string) json.RawMessage { return json.RawMessage(s) }
func rawObj(s string) RawObject    { return RawObject(s) }
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := jscompat.Stringify(v)
	if err != nil {
		t.Fatalf("stringify: %v", err)
	}
	return string(b)
}

// ── §5.3 differentModel ───────────────────────────────────────────────────

func TestDifferentModel(t *testing.T) {
	cases := []struct {
		name       string
		model      Model
		assistant  Assistant
		wantDiffer bool
	}{
		{
			name:      "identical",
			model:     Model{ProviderID: "openrouter", ID: "deepseek/deepseek-v4-pro"},
			assistant: Assistant{ProviderID: "openrouter", ModelID: "deepseek/deepseek-v4-pro"},
		},
		{
			name:       "different model id",
			model:      Model{ProviderID: "openrouter", ID: "qwen/qwen3-max"},
			assistant:  Assistant{ProviderID: "openrouter", ModelID: "deepseek/deepseek-v4-pro"},
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

// ── truncateToolOutput (message-v2.ts:326-330) ────────────────────────────

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
	// `length` is UTF-16 code units: an astral char counts as 2, so a 3-unit
	// cut keeps one BMP char plus a HALF surrogate pair. JS keeps the lone
	// surrogate; Go cannot hold one, so utf16.Decode yields U+FFFD. Bounded,
	// documented divergence.
	got := TruncateToolOutput("a\U0001F600b", ptrFloat(2))
	if !strings.HasPrefix(got, "a�") {
		t.Fatalf("surrogate split: %q", got)
	}
	if !strings.HasSuffix(got, "omitted 2 chars]") {
		t.Fatalf("omitted count should be UTF-16 based: %q", got)
	}
}

// ── §5.2 opaque JSON ──────────────────────────────────────────────────────

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

// ── providerMeta (message-v2.ts:723-727) ──────────────────────────────────

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

// ── §5.5 doom-loop key ────────────────────────────────────────────────────

func TestSameInputIsStringifyEqualityNotDeepEquality(t *testing.T) {
	if !SameInput(rawObj(`{"a":1,"b":2}`), rawObj(`{"a":1, "b":2}`)) {
		t.Fatal("insignificant whitespace must not matter")
	}
	if SameInput(rawObj(`{"a":1,"b":2}`), rawObj(`{"b":2,"a":1}`)) {
		t.Fatal("key ORDER is load-bearing: JSON.stringify differs, so the guard must not fire")
	}
	if !SameInput(nil, rawObj(`{}`)) {
		t.Fatal("absent input reads as {}")
	}
}

// ── §2.4 tool-part settlement ─────────────────────────────────────────────

func TestPendingToolState(t *testing.T) {
	got := mustJSON(t, PendingToolState())
	if got != `{"status":"pending","input":{},"raw":""}` {
		t.Fatalf("pending literal: %s", got)
	}
	if _, ok := PendingToolState().StartTime(); ok {
		t.Fatal("pending has no time at all (message-v2.ts:287-294)")
	}
}

func TestAbortedToolStateFromPendingUsesNow(t *testing.T) {
	got := AbortedToolState(PendingToolState(), 4242)
	if got.Time.Start != 4242 || got.Time.End != 4242 {
		t.Fatalf("time = %+v, want start and end both now", got.Time)
	}
	if got.Error != ToolAbortedError {
		t.Fatalf("error = %q", got.Error)
	}
	if string(got.Metadata) != `{"interrupted":true}` {
		t.Fatalf("metadata = %s", got.Metadata)
	}
}

func TestAbortedToolStateFromRunningKeepsStartAndMergesMetadata(t *testing.T) {
	prev := ToolStateRunning{
		Input:    rawObj(`{"cmd":"ls"}`),
		Title:    ptrString("bash"),
		Metadata: rawObj(`{"pid":7,"interrupted":false}`),
		Time:     ToolTimeStart{Start: 100},
	}
	got := AbortedToolState(prev, 900)
	if got.Time.Start != 100 || got.Time.End != 900 {
		t.Fatalf("time = %+v", got.Time)
	}
	// The pre-existing key keeps its position and flips to true.
	if string(got.Metadata) != `{"pid":7,"interrupted":true}` {
		t.Fatalf("metadata = %s", got.Metadata)
	}
	if string(got.Input) != `{"cmd":"ls"}` {
		t.Fatalf("input = %s", got.Input)
	}
}

func TestSpreadAbortedToolStateLeaksNonSchemaKeys(t *testing.T) {
	// processor.ts:646 spreads the previous state, so `raw` survives into an
	// object the ToolStateError schema never declares. Kept bug-for-bug.
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
		t.Fatalf("running title should leak through the spread: %s", got)
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

// ── getToolName (ai/dist/index.mjs:5257-5262) ─────────────────────────────

func TestStaticToolNamePreservesInternalDashes(t *testing.T) {
	cases := map[string]string{
		"tool-bash":               "bash",
		"tool-plandb-task-insert": "plandb-task-insert",
		"tool-":                   "",
		"nodash":                  "",
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

// ── §2.5 site 7: the synthetic attachment message ─────────────────────────

func TestSyntheticAttachmentMessageIsUnreachableForOpenRouter(t *testing.T) {
	// supportsMediaInToolResult has no @openrouter arm, so every media
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
	// The gemini arm lowercases first and requires gemini-3 AND not gemini-2.
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

func TestAPIErrorRecordOrderSurvivesRetryPartRoundTrip(t *testing.T) {
	const input = `{"id":"p","sessionID":"s","messageID":"m","type":"retry","attempt":2,"error":{"name":"APIError","data":{"message":"boom","isRetryable":true,"responseHeaders":{"z-last":"z","a-first":"a"},"metadata":{"zeta":"z","alpha":"a"}}},"time":{"created":42}}`
	part, err := UnmarshalPart([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, part); got != input {
		t.Fatalf("retry APIError record order changed:\n got %s\nwant %s", got, input)
	}
}

func TestSummaryAndBoolPointerHelpers(t *testing.T) {
	if boolValue(nil) || !boolValue(ptrBool(true)) || boolValue(ptrBool(false)) {
		t.Fatal("boolValue")
	}
}
