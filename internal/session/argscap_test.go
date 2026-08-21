package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE ARGS A SURFACE READS ARE JSON AT EVERY SIZE.
//
// This is the pin on the defect the display cap used to have: a write whose
// content ran past [argsLimit] was cut at a byte offset, which lands in the
// middle of a string literal, and internal/tui3's argsOf then answered nil for a
// call the model had spelled perfectly. The expansion read an empty content
// field and drew the dim em dash it draws for a call with nothing in it — so the
// biggest writes, the ones actually worth opening, were the ones that showed a
// dash.
func TestArgsStayParseableAtEverySize(t *testing.T) {
	// The overhead of the smallest write payload around a content field, so the
	// exactly-at-the-cap case below can be built to the byte.
	const shell = `{"content":"","path":"f.go"}`

	for _, size := range []int{
		0, 1, 64, 4000,
		argsLimit - len(shell) - 1,
		argsLimit - len(shell), // exactly at the cap
		argsLimit - len(shell) + 1,
		argsLimit,
		10 * argsLimit,
	} {
		content := strings.Repeat("a", size)
		wire, err := json.Marshal(map[string]any{"path": "f.go", "content": content})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got := argsText(ai.ToolCall{
			ID: "c1", Type: "function",
			Function: ai.ToolCallFunction{Name: "write", Arguments: string(wire)},
		})
		if len(got) > argsLimit {
			t.Fatalf("content of %d bytes produced %d bytes of Args, past the %d cap",
				size, len(got), argsLimit)
		}
		var fields struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(got), &fields); err != nil {
			t.Fatalf("content of %d bytes produced unparseable Args: %v", size, err)
		}
		if fields.Path != "f.go" {
			t.Fatalf("content of %d bytes lost the path: %q", size, fields.Path)
		}
		switch {
		case len(wire) <= argsLimit:
			if fields.Content != content {
				t.Fatalf("content of %d bytes fits under the cap and was shortened anyway", size)
			}
		default:
			if !strings.HasSuffix(fields.Content, "more bytes)") {
				t.Fatalf("content of %d bytes was shortened without saying so: ends %q",
					size, lastRunes(fields.Content, 24))
			}
			if !strings.HasPrefix(fields.Content, "aaaa") {
				t.Fatalf("content of %d bytes kept nothing of the file", size)
			}
		}
	}
}

// A call carrying SEVERAL long strings shares the budget between them, rather
// than spending the whole of it on whichever the model happened to send first.
// An edit sending four replacement blocks is the case: a diff computed from one
// whole block and three empty ones is a diff about a change that did not happen.
func TestArgsShareTheCapBetweenSeveralLongFields(t *testing.T) {
	const blocks = 4
	edits := make([]map[string]string, 0, blocks)
	for i := 0; i < blocks; i++ {
		edits = append(edits, map[string]string{
			"oldText": strings.Repeat("old line\n", 2000),
			"newText": strings.Repeat("new line\n", 2000),
		})
	}
	wire, err := json.Marshal(map[string]any{"path": "f.go", "edits": edits})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := argsText(ai.ToolCall{
		ID: "c1", Type: "function",
		Function: ai.ToolCallFunction{Name: "edit", Arguments: string(wire)},
	})
	if len(got) > argsLimit {
		t.Fatalf("Args is %d bytes, past the %d cap", len(got), argsLimit)
	}
	var fields struct {
		Path  string `json:"path"`
		Edits []struct {
			OldText string `json:"oldText"`
			NewText string `json:"newText"`
		} `json:"edits"`
	}
	if err := json.Unmarshal([]byte(got), &fields); err != nil {
		t.Fatalf("a shortened edit payload no longer parses: %v", err)
	}
	if fields.Path != "f.go" {
		t.Fatalf("the path was shortened: %q", fields.Path)
	}
	if len(fields.Edits) != blocks {
		t.Fatalf("kept %d replacement blocks, want %d", len(fields.Edits), blocks)
	}
	// Every block keeps a real window onto its own text, and every window is the
	// same size — that is what "one threshold for the whole payload" means.
	want := len(fields.Edits[0].OldText)
	if want < 500 {
		t.Fatalf("each block kept only %d bytes, which is not a diff", want)
	}
	for i, edit := range fields.Edits {
		for _, side := range []struct {
			name, text, opens string
		}{
			{"oldText", edit.OldText, "old line"},
			{"newText", edit.NewText, "new line"},
		} {
			if len(side.text) != want {
				t.Fatalf("block %d's %s kept %d bytes, block 0's oldText kept %d",
					i, side.name, len(side.text), want)
			}
			if !strings.HasPrefix(side.text, side.opens) {
				t.Fatalf("block %d's %s lost its own text: %q", i, side.name, firstRunes(side.text, 16))
			}
			if !strings.HasSuffix(side.text, "more bytes)") {
				t.Fatalf("block %d's %s was shortened without saying so", i, side.name)
			}
		}
	}
}

// The short fields of an oversized payload survive whole. A `path` is what a
// person opens the file by, and a path cut to share a budget with a file body is
// a path that opens nothing.
func TestArgsLeaveShortFieldsAlone(t *testing.T) {
	wire, err := json.Marshal(map[string]any{
		"path":    "internal/session/a-fairly-long-and-specific-path.go",
		"content": strings.Repeat("x", 200000),
		"timeout": 120,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := argsText(ai.ToolCall{
		ID: "c1", Type: "function",
		Function: ai.ToolCallFunction{Name: "write", Arguments: string(wire)},
	})
	var fields struct {
		Path    string `json:"path"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal([]byte(got), &fields); err != nil {
		t.Fatalf("unparseable: %v", err)
	}
	if fields.Path != "internal/session/a-fairly-long-and-specific-path.go" {
		t.Fatalf("the path was shortened: %q", fields.Path)
	}
	if fields.Timeout != 120 {
		t.Fatalf("timeout came back as %d, want 120 — the round trip changed a number",
			fields.Timeout)
	}
}

// Arguments that are not JSON at all keep the byte cut, because there is nothing
// in them to cut inside of and a malformed payload was already malformed.
func TestArgsThatAreNotJSONKeepTheByteCut(t *testing.T) {
	junk := strings.Repeat("not json at all ", 4000)
	got := argsText(ai.ToolCall{
		ID: "c1", Type: "function",
		Function: ai.ToolCallFunction{Name: "write", Arguments: junk},
	})
	if len(got) > argsLimit {
		t.Fatalf("Args is %d bytes, past the %d cap", len(got), argsLimit)
	}
	if !strings.HasPrefix(got, "not json at all") {
		t.Fatalf("the raw text did not pass through: %q", firstRunes(got, 24))
	}
}

// ── the partial read ────────────────────────────────────────────────────────

// PartialString answers about a string the model has not finished sending, which
// is every state a forming call is ever seen in. The two hard prefixes are a
// string with no closing quote and a text that stops in the middle of an escape;
// both have to yield everything BEFORE the cut and nothing invented after it.
func TestPartialStringReadsAnUnfinishedValue(t *testing.T) {
	for _, tc := range []struct {
		name, args, want string
		found            bool
	}{
		{
			name:  "still open",
			args:  `{"path":"f.go","content":"package main\nfunc mai`,
			want:  "package main\nfunc mai",
			found: true,
		},
		{
			name:  "cut on a lone backslash",
			args:  `{"path":"f.go","content":"one line\`,
			want:  "one line",
			found: true,
		},
		{
			name:  "cut inside a unicode escape",
			args:  `{"path":"f.go","content":"caf\u00`,
			want:  "caf",
			found: true,
		},
		{
			name:  "a whole unicode escape decodes",
			args:  `{"path":"f.go","content":"café and`,
			want:  "café and",
			found: true,
		},
		{
			name:  "closed",
			args:  `{"path":"f.go","content":"done\n"}`,
			want:  "done\n",
			found: true,
		},
		{
			name:  "opened and empty",
			args:  `{"path":"f.go","content":"`,
			want:  "",
			found: true,
		},
		{
			name:  "not there yet",
			args:  `{"path":"f.go"`,
			want:  "",
			found: false,
		},
		{
			name:  "nothing at all",
			args:  "",
			want:  "",
			found: false,
		},
		{
			name:  "a nested field of the same name is not this field",
			args:  `{"edits":[{"content":"nested text"`,
			want:  "",
			found: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := PartialString(tc.args, "content")
			if ok != tc.found {
				t.Fatalf("found = %v, want %v", ok, tc.found)
			}
			if got != tc.want {
				t.Fatalf("PartialString = %q, want %q", got, tc.want)
			}
		})
	}
}

// The answer FOLLOWS growth: fed the same call one fragment longer each time —
// which is exactly how the wire sends it, cumulatively — the text only ever
// gains, and past [PartialStringLimit] it is the END that is kept, because the
// end is where the model is writing.
func TestPartialStringFollowsAGrowingValue(t *testing.T) {
	body := strings.Repeat("the quick brown fox\n", 2000)
	wire, err := json.Marshal(map[string]any{"path": "f.go", "content": body})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The closing `"}` is dropped so every prefix below is a value still arriving.
	stream := strings.TrimSuffix(string(wire), `"}`)

	previous := ""
	for at := 1; at <= len(stream); at += 97 {
		got, ok := PartialString(stream[:at], "content")
		if !ok {
			continue
		}
		if len(got) > PartialStringLimit {
			t.Fatalf("at %d bytes the answer is %d, past the %d window",
				at, len(got), PartialStringLimit)
		}
		if len(previous) < PartialStringLimit && !strings.HasPrefix(got, previous) {
			t.Fatalf("at %d bytes the answer un-said what it had already said", at)
		}
		previous = got
	}

	whole, ok := PartialString(stream, "content")
	if !ok {
		t.Fatal("the whole stream reports no content field")
	}
	if !strings.HasSuffix(body, whole) {
		t.Fatal("the window is not the tail of what arrived")
	}
	if len(whole) < PartialStringLimit-64 {
		t.Fatalf("the window kept %d bytes of a %d-byte body", len(whole), len(body))
	}
}

func firstRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
