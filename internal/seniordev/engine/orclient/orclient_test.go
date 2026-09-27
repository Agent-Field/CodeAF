//go:build !windows

package orclient

// Unit tests for the SSE frame decoder, the tool-call accumulator's index
// model, the ordered-JSON spread semantics, and the surrogate handling.

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"unicode/utf16"
)

// ── SSE decoder ───────────────────────────────────────────────────────────

func decodeAll(t *testing.T, raw string) []SSEEvent {
	t.Helper()
	dec := NewSSEDecoder(strings.NewReader(raw))
	var out []SSEEvent
	for {
		ev, err := dec.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		out = append(out, ev)
	}
}

func TestSSEDecoder(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"lf", "data: a\n\ndata: b\n\n", []string{"a", "b"}},
		{"crlf", "data: a\r\n\r\ndata: b\r\n\r\n", []string{"a", "b"}},
		{"bare cr", "data: a\r\rdata: b\r\r", []string{"a", "b"}},
		{"no space after colon", "data:a\n\n", []string{"a"}},
		{"exactly one leading space stripped", "data:  a\n\n", []string{" a"}},
		{"comment lines ignored", ": OPENROUTER PROCESSING\n\ndata: a\n\n", []string{"a"}},
		{"utf8 bom is stripped", "\uFEFFdata: a\n\n", []string{"a"}},
		{"comment inside a frame", "data: a\n: note\ndata: b\n\n", []string{"a\nb"}},
		{"multiple data lines join with newline", "data: a\ndata: b\n\n", []string{"a\nb"}},
		{"empty data field", "data:\n\n", []string{""}},
		{"blank line with no data dispatches nothing", "\n\n\n", nil},
		{"trailing frame without blank line", "data: a\n\ndata: b", []string{"a", "b"}},
		{"trailing frame without newline at all", "data: a", []string{"a"}},
		{"event and id fields do not dispatch", "event: x\nid: 7\ndata: a\n\n", []string{"a"}},
		{"unknown field ignored", "banana: x\ndata: a\n\n", []string{"a"}},
		{"retry field ignored", "retry: 500\ndata: a\n\n", []string{"a"}},
		{"DONE is just data", "data: [DONE]\n\n", []string{"[DONE]"}},
		{"empty input", "", nil},
		{"only comments", ": a\n\n: b\n\n", nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := decodeAll(t, tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("event count: want %d %v, got %d %v", len(tc.want), tc.want, len(got), got)
			}
			for i := range got {
				if got[i].Data != tc.want[i] {
					t.Errorf("event %d: want %q, got %q", i, tc.want[i], got[i].Data)
				}
			}
		})
	}
}

func TestSSEDecoderKeepsEventAndIDFields(t *testing.T) {
	got := decodeAll(t, "event: message\nid: 42\ndata: a\n\ndata: b\n\n")
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %d", len(got))
	}
	if got[0].Event != "message" || got[0].ID != "42" {
		t.Errorf("first event: %+v", got[0])
	}
	// `lastEventId` persists across events; `event` resets.
	if got[1].Event != "" || got[1].ID != "42" {
		t.Errorf("second event: %+v", got[1])
	}
}

// ── the tool-call accumulator's index model ──────────────────────────────

// Only a non-negative integer index extends length; any other key is stored
// without being iterated.
func TestToolCallArrayIndexSemantics(t *testing.T) {
	a := newToolCallArray()
	if a.length != 0 {
		t.Fatalf("fresh array length: %d", a.length)
	}

	a.setKey("-1", &toolCallSlot{id: "ghost"})
	if a.length != 0 {
		t.Errorf("a negative index must NOT extend length, got %d", a.length)
	}
	if a.get(-1) == nil {
		t.Error("the slot must still be reachable at -1")
	}

	a.setKey("2", &toolCallSlot{id: "third"})
	if a.length != 3 {
		t.Errorf("index 2 must set length to 3, got %d", a.length)
	}

	var visited []string
	a.iterate(func(s *toolCallSlot) {
		if s == nil {
			visited = append(visited, "<hole>")
			return
		}
		visited = append(visited, s.id)
	})
	want := []string{"<hole>", "<hole>", "third"}
	if strings.Join(visited, ",") != strings.Join(want, ",") {
		t.Errorf("iteration must walk 0..length-1 including holes: want %v, got %v", want, visited)
	}

	a.setKey("1", &toolCallSlot{id: "second"})
	if a.length != 3 {
		t.Errorf("filling a hole must not shrink or grow length, got %d", a.length)
	}

	a.setKey("1.5", &toolCallSlot{id: "fractional"})
	a.setKey("01", &toolCallSlot{id: "leading-zero"})
	a.setKey("4294967295", &toolCallSlot{id: "uint32-max"})
	if a.length != 3 {
		t.Errorf("non-canonical array properties must not extend length, got %d", a.length)
	}
	for _, key := range []string{"1.5", "01", "4294967295"} {
		if a.getKey(key) == nil {
			t.Errorf("plain array property %q must remain addressable", key)
		}
	}
}

// ── isParsableJson ────────────────────────────────────────────────────────

func TestIsParsableJSON(t *testing.T) {
	cases := map[string]bool{
		``:                                 false,
		`{}`:                               true,
		`{"a":1}`:                          true,
		`{`:                                false,
		`{"a":`:                            false,
		`[]`:                               true,
		`null`:                             true,
		`"str"`:                            true,
		`42`:                               true,
		`{"a":1}{"b":2}`:                   false,
		`  {"a":1}  `:                      true,
		`{"__proto__":{}}`:                 false, // secureJsonParse rejects it
		`{"constructor":{"prototype":{}}}`: false,
		`{"constructor":{"x":1}}`:          true,
	}
	for input, want := range cases {
		if got := isParsableJSON(input); got != want {
			t.Errorf("isParsableJSON(%q) = %v, want %v", input, got, want)
		}
	}
}

// ── SanitizeSurrogates: WTF-8 input ──────────────────────────────────────

// A lone surrogate is not reachable through encoding/json (which maps
// `\uD800` to U+FFFD on decode), but it IS through the WTF-8 byte sequence,
// which a byte-level splice or a non-strict decoder can produce.
func TestSanitizeSurrogatesWTF8(t *testing.T) {
	// WTF-8 for U+D800 (a lone high surrogate).
	loneHigh := string([]byte{0xED, 0xA0, 0x80})
	// WTF-8 for U+DC00 (a lone low surrogate).
	loneLow := string([]byte{0xED, 0xB0, 0x80})

	if got := SanitizeSurrogates("a" + loneHigh + "b"); got != "a�b" {
		t.Errorf("lone high (WTF-8): got %q", got)
	}
	if got := SanitizeSurrogates("a" + loneLow + "b"); got != "a�b" {
		t.Errorf("lone low (WTF-8): got %q", got)
	}
	// CESU-8: a well-formed PAIR encoded as two 3-byte sequences is a valid
	// pair and must be returned unchanged.
	pair := loneHigh + string([]byte{0xED, 0xB0, 0x80})
	if got := SanitizeSurrogates(pair); got != pair {
		t.Errorf("a well-formed CESU-8 pair must be returned byte-identical: got %q", got)
	}
	// Two lone highs in a row: the first is unpaired, the second is too.
	both := loneHigh + loneHigh
	if got := SanitizeSurrogates(both); got != "��" {
		t.Errorf("two lone highs: got %q", got)
	}
	// A genuine astral character survives untouched and does not take the slow
	// path at all.
	if got := SanitizeSurrogates("a😀b"); got != "a😀b" {
		t.Errorf("astral char must be untouched: got %q", got)
	}
	// A string that cannot contain a surrogate is returned by identity.
	plain := "hello, 世界"
	if got := SanitizeSurrogates(plain); got != plain {
		t.Errorf("plain text must be untouched: got %q", got)
	}
}

func TestUTF16UnitsRoundTrip(t *testing.T) {
	for _, s := range []string{"", "abc", "😀", "a😀b", "日本語", "�"} {
		units := utf16Units(s)
		if got := string(utf16.Decode(units)); got != s {
			t.Errorf("round trip of %q gave %q", s, got)
		}
	}
}

// ── ordered JSON ──────────────────────────────────────────────────────────

func TestObjectSpreadKeepsPositionOnOverwrite(t *testing.T) {
	target := NewObject()
	target.SetNumber("a", 1)
	target.SetBool("usage", false)
	target.SetNumber("z", 2)

	source := NewObject()
	source.SetBool("usage", true)
	source.SetString("new", "x")

	out := target.Clone()
	for _, k := range source.Keys() {
		v, _ := source.Get(k)
		if err := out.Set(k, v); err != nil {
			t.Fatalf("set: %v", err)
		}
	}
	encoded, err := out.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"a":1,"usage":true,"z":2,"new":"x"}`
	if string(encoded) != want {
		t.Errorf("want %s, got %s", want, encoded)
	}
}

func TestInvalidToolChoiceErrorMatchesProvider(t *testing.T) {
	_, err := BuildRequestBody(RequestParams{
		ModelID:    "x/y",
		Tools:      []Tool{{Type: "function", Name: "bash"}},
		ToolChoice: &ToolChoice{Type: "future", ToolName: "bash"},
	})
	if err == nil {
		t.Fatal("expected an invalid tool choice to fail")
	}
	if got, want := err.Error(), `Invalid tool choice type: {"type":"future","toolName":"bash"}`; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestMergeOptionsDeepMergeSemantics(t *testing.T) {
	target, err := ParseObject([]byte(`{"a":{"x":1,"y":2},"b":1,"keep":true}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	source, err := ParseObject([]byte(`{"a":{"y":9,"z":3},"b":{"deep":1},"new":5}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := MergeOptions(target, source).MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// `a` recurses in place; `b` is replaced wholesale (target's value is not a
	// plain object); `new` is appended.
	want := `{"a":{"x":1,"y":9,"z":3},"b":{"deep":1},"keep":true,"new":5}`
	if string(got) != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

// ── DeterministicStringify edge cases ────────────────────────────────────

func TestDeterministicStringifyUndefined(t *testing.T) {
	// An absent input is reported to the caller, which drops the key.
	if _, err := DeterministicStringify(nil); err == nil {
		t.Error("an absent input must be reported, not guessed at")
	}
	if _, err := DeterministicStringify(json.RawMessage("   ")); err == nil {
		t.Error("a whitespace-only input must be reported")
	}
	if _, err := DeterministicStringify(json.RawMessage("{not json")); err == nil {
		t.Error("an unparsable input must be reported")
	}
}

func TestDeterministicStringifySortsKeysRecursively(t *testing.T) {
	got, err := DeterministicStringify(json.RawMessage(`{"zeta":1,"Alpha":{"y":[{"b":1,"a":2}],"x":0},"_x":3.50}`))
	if err != nil {
		t.Fatalf("DeterministicStringify: %v", err)
	}
	want := `{"Alpha":{"x":0,"y":[{"a":2,"b":1}]},"_x":3.50,"zeta":1}`
	if string(got) != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

// ── generateId ────────────────────────────────────────────────────────────

func TestGenerateIDDrawsOncePerCharacter(t *testing.T) {
	draws := 0
	restore := SetRandomForTesting(func() float64 {
		draws++
		return 0
	})
	defer restore()

	id := generateId()
	if draws != idSize {
		t.Errorf("generateId must draw exactly %d times, drew %d", idSize, draws)
	}
	if len(id) != idSize {
		t.Errorf("id length: want %d, got %d (%q)", idSize, len(id), id)
	}
	if id != strings.Repeat("0", idSize) {
		t.Errorf("random()==0 must select alphabet[0]: got %q", id)
	}
}

// ── finish-reason mapping ─────────────────────────────────────────────────

func TestMapToUnifiedCoversTheWholeDomain(t *testing.T) {
	cases := map[string]string{
		"stop":           FinishStop,
		"length":         FinishLength,
		"content_filter": FinishContentFilter,
		"function_call":  FinishToolCalls,
		"tool_calls":     FinishToolCalls,
		// `error` falls through to `other`, NOT to `error`. The only sources
		// of unified `error` are a parse failure, a top-level error payload,
		// and a reader error.
		"error":   FinishOther,
		"":        FinishOther,
		"banana":  FinishOther,
		"STOP":    FinishOther,
		"toolUse": FinishOther,
	}
	for raw, want := range cases {
		if got := MapToUnified(raw); got != want {
			t.Errorf("MapToUnified(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := MapOpenRouterFinishReason(nil); got.Unified != FinishOther || got.Raw != nil {
		t.Errorf("a nil finish_reason must give {other, absent}, got %+v", got)
	}
}

// ── tool map ordering ─────────────────────────────────────────────────────

func TestSortedToolMapSortsByName(t *testing.T) {
	m := SortedToolMap(
		ToolSpec{Name: "zebra"},
		ToolSpec{Name: "Apple"},
		ToolSpec{Name: "_hidden"},
		ToolSpec{Name: "invalid"},
		ToolSpec{Name: "apple"},
	)
	got := strings.Join(m.Names(), ",")
	want := "Apple,_hidden,apple,invalid,zebra"
	if got != want {
		t.Errorf("tool map order:\n want %s\n  got %s", want, got)
	}
	active := strings.Join(m.ActiveTools(), ",")
	if strings.Contains(active, "invalid") {
		t.Errorf("activeTools must exclude the invalid tool, got %s", active)
	}
}

// ── reasoning-details live reference ──────────────────────────────────────

func TestReasoningDetailsViewIsLive(t *testing.T) {
	acc := []ReasoningDetail{{Type: ReasoningDetailText, Text: rawStringLiteral("A")}}
	view := detailsRef(&acc)

	first, err := view.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(first) != `[{"type":"reasoning.text","text":"A"}]` {
		t.Fatalf("initial: %s", first)
	}

	acc = append(acc, ReasoningDetail{Type: ReasoningDetailSummary, Summary: "LATE"})
	second, err := view.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `[{"type":"reasoning.text","text":"A"},{"type":"reasoning.summary","summary":"LATE"}]`
	if string(second) != want {
		t.Errorf("a view handed out earlier must see LATER appends:\n want %s\n  got %s", want, second)
	}

	detached := DetailsValue(acc...)
	acc = append(acc, ReasoningDetail{Type: ReasoningDetailEncrypted, Data: "E"})
	third, err := detached.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(third) != want {
		t.Errorf("a DETACHED view must not see later appends: got %s", third)
	}
}

func TestEmptyReasoningDetailsMarshalAsAnArrayNotNull(t *testing.T) {
	got, err := DetailsValue().MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != "[]" {
		t.Errorf("the empty case is meaningful and must be `[]`, got %s", got)
	}
	var nilView ReasoningDetailsView
	got, err = nilView.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != "[]" {
		t.Errorf("a zero view must also be `[]`, got %s", got)
	}
}

// ── orTruthy over nullish JSON ───────────────────────────────────────────

func TestOrTruthyFalsiness(t *testing.T) {
	cases := []struct {
		a, b, want string
	}{
		{`"x"`, `"y"`, `"x"`},
		{`null`, `"y"`, `"y"`},
		{`""`, `"y"`, `"y"`},
		{``, `"y"`, `"y"`},
		{`null`, ``, ``},
		{`""`, `null`, `null`},
		{`false`, `"y"`, `"y"`},
		{`0`, `"y"`, `"y"`},
	}
	for _, tc := range cases {
		var a, b json.RawMessage
		if tc.a != "" {
			a = json.RawMessage(tc.a)
		}
		if tc.b != "" {
			b = json.RawMessage(tc.b)
		}
		got := orTruthy(a, b)
		gotStr := ""
		if got != nil {
			gotStr = string(got)
		}
		if gotStr != tc.want {
			t.Errorf("orTruthy(%q, %q) = %q, want %q", tc.a, tc.b, gotStr, tc.want)
		}
	}
}

// ── tool-call deltas without an index ─────────────────────────────────────

func TestToolCallDeltaWithoutIndex(t *testing.T) {
	// A first no-index delta with non-parsable args must land at slot 0 and be
	// flushed rather than dropped.
	t.Run("first no-index delta appends at slot 0 and flushes", func(t *testing.T) {
		sse := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_a\",\"type\":\"function\",\"function\":{\"name\":\"f\",\"arguments\":\"\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
		parts, thrown := runSSE(sse, 1234)
		if thrown != nil {
			t.Fatalf("unexpected error: %v", thrown)
		}
		// The tool call must be present: tool-input-start, tool-input-delta,
		// tool-input-end, tool-call, then finish.
		hasToolCall := false
		for _, p := range parts {
			if p.PartType() == PartTypeToolCall {
				hasToolCall = true
				break
			}
		}
		if !hasToolCall {
			t.Error("tool-call part missing — no-index delta was dropped instead of appended at slot 0")
		}
	})

	// A later no-index delta still targets the last slot (length-1), merging into it.
	t.Run("later no-index delta merges into last slot", func(t *testing.T) {
		sse := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\",\"type\":\"function\",\"function\":{\"name\":\"f\",\"arguments\":\"{\\\"x\\\":\"}}]}}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"function\":{\"arguments\":\"1}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
		parts, thrown := runSSE(sse, 1234)
		if thrown != nil {
			t.Fatalf("unexpected error: %v", thrown)
		}
		// The second chunk has no index; it must merge into index 0 (the last slot).
		var toolCall ToolCallPart
		found := false
		for _, p := range parts {
			if tc, ok := p.(ToolCallPart); ok {
				toolCall = tc
				found = true
				break
			}
		}
		if !found {
			t.Fatal("no tool-call part found")
		}
		if toolCall.Input != "{\"x\":1}" {
			t.Errorf("merged input = %q, want %q", toolCall.Input, "{\"x\":1}")
		}
	})
}
