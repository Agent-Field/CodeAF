//go:build !windows

package compaction

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

func toolPartCompleted(messageID, id, tool, input, output string) msgmodel.ToolPart {
	return msgmodel.ToolPart{
		PartBase: msgmodel.PartBase{ID: id, SessionID: "ses_1", MessageID: messageID},
		CallID:   id,
		Tool:     tool,
		State: msgmodel.ToolStateCompleted{
			Input:  msgmodel.RawObject(input),
			Output: output,
		},
	}
}

func TestSerializeTranscriptFlattensToOneBlockWithoutMessageArray(t *testing.T) {
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "Fix src/a.ts")),
		testAssistant("a0", "u0",
			textPart("a0", "Reading the file first."),
			toolPartCompleted("a0", "c1", "read", `{"file":"src/a.ts"}`, "line one\nline two"),
		),
	}
	out := SerializeTranscript(messages, 2000)

	for _, want := range []string{
		"[User]: Fix src/a.ts",
		"[Assistant]: Reading the file first.",
		"[Assistant tool calls]: read(",
		"[Tool result]: line one",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("serialized transcript missing %q:\n%s", want, out)
		}
	}
	// The whole point: a flattened string, not a replayable conversation.
	if strings.Contains(out, "\"role\"") {
		t.Fatalf("transcript leaked a message-array shape:\n%s", out)
	}
}

func TestSerializeTranscriptCapsToolOutput(t *testing.T) {
	big := strings.Repeat("x", 10_000)
	messages := []msgmodel.WithParts{
		testAssistant("a0", "u0", toolPartCompleted("a0", "c1", "bash", `{"cmd":"cat big"}`, big)),
	}
	out := SerializeTranscript(messages, 2000)
	if len(out) > 4000 {
		t.Fatalf("tool output not capped: serialized length %d", len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncation marker in:\n%s", out[:200])
	}
}

func TestSerializeTranscriptRendersToolErrors(t *testing.T) {
	messages := []msgmodel.WithParts{
		testAssistant("a0", "u0", msgmodel.ToolPart{
			PartBase: msgmodel.PartBase{ID: "c1", SessionID: "ses_1", MessageID: "a0"},
			CallID:   "c1", Tool: "bash",
			State: msgmodel.ToolStateError{
				Input: msgmodel.RawObject(`{"cmd":"false"}`),
				Error: "exit status 1",
			},
		}),
	}
	out := SerializeTranscript(messages, 2000)
	if !strings.Contains(out, "[Tool error]: exit status 1") {
		t.Fatalf("tool error not rendered:\n%s", out)
	}
}

func TestClassifySummaryFailure(t *testing.T) {
	cases := []struct {
		name string
		msg  msgmodel.WithParts
		want string
	}{
		{
			name: "dsml markup as text",
			msg:  testAssistant("a", "u", textPart("a", "<｜DSML｜invoke name=\"bash\">")),
			want: SummaryClassDSMLText,
		},
		{
			name: "xml tool call as text",
			msg:  testAssistant("a", "u", textPart("a", "<tool_call>read</tool_call>")),
			want: SummaryClassDSMLText,
		},
		{
			name: "empty",
			msg:  testAssistant("a", "u", textPart("a", "   ")),
			want: SummaryClassEmpty,
		},
		{
			name: "off format prose",
			msg:  testAssistant("a", "u", textPart("a", "Let me continue reading the code.")),
			want: SummaryClassFormat,
		},
		{
			name: "structural tool part",
			msg: testAssistant("a", "u",
				toolPartCompleted("a", "c1", "read", "{}", "ok")),
			want: SummaryClassToolCall,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifySummaryFailure(tc.msg); got != tc.want {
				t.Fatalf("class = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCapTranscriptCutsTheMiddleAndReportsIt(t *testing.T) {
	transcript := strings.Repeat("a", 1000) + strings.Repeat("z", 1000)
	capped, omitted := CapTranscript(transcript, 400)
	if omitted != 1600 {
		t.Fatalf("omitted = %v, want 1600", omitted)
	}
	if !strings.HasPrefix(capped, strings.Repeat("a", 100)) ||
		!strings.HasSuffix(capped, strings.Repeat("z", 300)) ||
		!strings.Contains(capped, "transcript cut here: 1600 chars") {
		t.Fatalf("capped transcript = %q", capped)
	}
	same, omitted := CapTranscript("short", 400)
	if same != "short" || omitted != 0 {
		t.Fatalf("short transcript was altered: %q %v", same, omitted)
	}
}
