//go:build !windows

package compaction

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// charEstimate sizes messages by their text and tool output characters so the
// tests can reason in exact numbers.
func charEstimate(messages []msgmodel.WithParts, _ Model) (float64, error) {
	total := 0
	for _, message := range messages {
		for _, raw := range message.Parts {
			switch part := raw.(type) {
			case msgmodel.TextPart:
				total += len(part.Text)
			case msgmodel.ToolPart:
				if completed, ok := part.State.(msgmodel.ToolStateCompleted); ok {
					total += len(completed.Output)
				}
			}
		}
	}
	return float64(total), nil
}

func TestSelectTailKeepsNewestVerbatimEvenOverBudget(t *testing.T) {
	huge := strings.Repeat("x", 50_000)
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "goal")),
		testAssistant("a0", "u0", textPart("a0", "older reply")),
		testAssistant("a1", "u0", toolPartCompleted("a1", "c1", "bash", `{"cmd":"cat"}`, huge)),
	}
	// A budget too small for even "older reply": only the newest message is
	// kept, and it is kept whole despite being 500x the budget.
	selected, err := selectTail(messages, 5, Model{}, charEstimate, 4_000)
	if err != nil {
		t.Fatal(err)
	}
	if selected.StartID == nil || *selected.StartID != "a1" || len(selected.Head) != 2 {
		t.Fatalf("selection = %#v", selected)
	}
	if selected.Messages != 1 || selected.Tokens != 50_000 || len(selected.Truncated) != 0 {
		t.Fatalf("the newest message must be kept whole and uncut: %#v", selected)
	}
}

func TestSelectTailTruncatesOlderToolOutputsAndMeasuresAfterwards(t *testing.T) {
	big := strings.Repeat("y", 10_000)
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "goal")),
		testAssistant("a0", "u0", textPart("a0", strings.Repeat("f", 2_000))),
		testAssistant("a1", "u0", toolPartCompleted("a1", "c1", "bash", `{}`, big)),
		testAssistant("a2", "u0", toolPartCompleted("a2", "c2", "bash", `{}`, big)),
		testAssistant("a3", "u0", textPart("a3", "newest")),
	}
	// Each truncated output costs 4,000 chars plus a marker; a budget of
	// 9,000 fits both older tool messages after truncation but neither
	// before it, and stops short of the 2,000-char reply before them.
	selected, err := selectTail(messages, 9_000, Model{}, charEstimate, 4_000)
	if err != nil {
		t.Fatal(err)
	}
	if selected.StartID == nil || *selected.StartID != "a1" || len(selected.Head) != 2 {
		t.Fatalf("selection = %#v", selected)
	}
	if selected.Messages != 3 || len(selected.Truncated) != 2 || selected.TruncatedOutputs != 2 {
		t.Fatalf("tail accounting = %#v", selected)
	}
	if selected.Tokens >= 20_000 || selected.Tokens < 8_000 {
		t.Fatalf("tail measured before truncation: %v", selected.Tokens)
	}
	for _, raw := range selected.Truncated {
		part := raw.(msgmodel.ToolPart)
		output := part.State.(msgmodel.ToolStateCompleted).Output
		if !strings.Contains(output, "truncated at a context compaction") ||
			!strings.HasPrefix(output, strings.Repeat("y", 1_000)) ||
			!strings.HasSuffix(output, strings.Repeat("y", 3_000)) {
			t.Fatalf("truncated output = %q", output[:80])
		}
	}
	// The caller's messages are untouched: truncation is reported, not
	// applied in place.
	original := messages[2].Parts[0].(msgmodel.ToolPart).State.(msgmodel.ToolStateCompleted).Output
	if original != big {
		t.Fatal("selectTail mutated the input messages")
	}
}

func TestSelectTailWithEverythingFittingHasNoHead(t *testing.T) {
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "goal")),
		testAssistant("a0", "u0", textPart("a0", "reply")),
	}
	selected, err := selectTail(messages, 1_000, Model{}, charEstimate, 4_000)
	if err != nil {
		t.Fatal(err)
	}
	// No head, but the tail is still NAMED from the first message: a boundary
	// whose compaction part carries no tail_start_id keeps nothing before it.
	if selected.StartID == nil || *selected.StartID != "u0" ||
		len(selected.Head) != 0 || selected.Messages != 2 {
		t.Fatalf("selection = %#v", selected)
	}
	empty, err := selectTail(nil, 1_000, Model{}, charEstimate, 4_000)
	if err != nil || empty.StartID != nil || empty.Messages != 0 {
		t.Fatalf("empty selection = %#v %v", empty, err)
	}
}

func reasoningPart(messageID, id, text string, metadata string) msgmodel.ReasoningPart {
	part := msgmodel.ReasoningPart{
		PartBase: msgmodel.PartBase{ID: id, SessionID: "ses_1", MessageID: messageID},
		Text:     text,
	}
	if metadata != "" {
		part.Metadata = msgmodel.RawObject(metadata)
	}
	return part
}

// Reasoning is cut in every kept message, the newest included: a newest
// message dominated by reasoning would otherwise cost the whole tail. Signed
// reasoning is left alone.
func TestSelectTailTruncatesUnsignedReasoningEverywhereIncludingNewest(t *testing.T) {
	long := strings.Repeat("thinking ", 2_000) // 18,000 chars
	messages := []msgmodel.WithParts{
		// A 5,000-char request that cannot fit the 3,000 budget, so it forms
		// the head and a0 is the first kept message.
		testUser("u0", textPart("u0", strings.Repeat("g", 5_000))),
		testAssistant("a0", "u0", reasoningPart("a0", "r0", long, ""), textPart("a0", "older")),
		testAssistant("a1", "u0",
			reasoningPart("a1", "r1", long, `{"anthropic":{"signature":"sig"}}`),
			reasoningPart("a1", "r2", long, ""),
			toolPartCompleted("a1", "c1", "bash", `{}`, strings.Repeat("o", 9_000)),
		),
	}
	selected, err := selectTail(messages, 3_000, Model{}, charEstimate, 4_000)
	if err != nil {
		t.Fatal(err)
	}
	if selected.StartID == nil || *selected.StartID != "a0" || selected.TruncatedReasoning != 2 ||
		selected.TruncatedOutputs != 0 {
		t.Fatalf("selection = %#v", selected)
	}
	// newest: unsigned r2 cut, signed r1 untouched, tool output whole.
	// older: r0 cut; its size after the cut fits the 3,000 budget.
	ids := map[string]bool{}
	for _, raw := range selected.Truncated {
		part := raw.(msgmodel.ReasoningPart)
		ids[part.ID] = true
		if !strings.HasPrefix(part.Text, "thinking ") || !strings.Contains(part.Text, "Reasoning truncated") ||
			len(part.Text) > 1_200 {
			t.Fatalf("reasoning cut = %q", part.Text[:80])
		}
	}
	if !ids["r0"] || !ids["r2"] || ids["r1"] {
		t.Fatalf("truncated reasoning ids = %v", ids)
	}
	if selected.Tokens > 1_000+18_000+9_000+200 || selected.Tokens < 9_000 {
		t.Fatalf("tail tokens = %v", selected.Tokens)
	}
}

func TestTruncateTailOutputKeepsHeadAndMostlyTail(t *testing.T) {
	text := strings.Repeat("h", 500) + strings.Repeat("t", 500)
	out := truncateTailOutput(text, 200)
	if !strings.HasPrefix(out, strings.Repeat("h", 50)+"\n") ||
		!strings.HasSuffix(out, strings.Repeat("t", 150)) ||
		!strings.Contains(out, "omitted 800 chars") {
		t.Fatalf("truncated = %q", out)
	}
	if got := truncateTailOutput("short", 200); got != "short" {
		t.Fatalf("short output altered: %q", got)
	}
}
