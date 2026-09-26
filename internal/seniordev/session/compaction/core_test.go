//go:build !windows

package compaction

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
)

func testUser(id string, parts ...msgmodel.Part) msgmodel.WithParts {
	return msgmodel.WithParts{
		Info: msgmodel.User{
			MessageBase: msgmodel.MessageBase{ID: id, SessionID: "ses_1"},
			Time:        msgmodel.TimeCreated{Created: 1},
			Agent:       "build",
			Model:       msgmodel.UserModel{ProviderID: "openrouter", ModelID: "m"},
		},
		Parts: parts,
	}
}

func testAssistant(id, parent string, parts ...msgmodel.Part) msgmodel.WithParts {
	return msgmodel.WithParts{
		Info: msgmodel.Assistant{
			MessageBase: msgmodel.MessageBase{ID: id, SessionID: "ses_1"},
			Time:        msgmodel.AssistantTime{Created: 2},
			ParentID:    parent, ModelID: "m", ProviderID: "openrouter",
			Mode: "build", Agent: "build",
			Path:   msgmodel.AssistantPath{Cwd: "/work", Root: "/work"},
			Tokens: msgmodel.Tokens{Cache: msgmodel.TokenCache{}},
		},
		Parts: parts,
	}
}

func textPart(messageID, text string) msgmodel.TextPart {
	return msgmodel.TextPart{
		PartBase: msgmodel.PartBase{
			ID: "p_" + messageID, SessionID: "ses_1", MessageID: messageID,
		},
		Text: text,
	}
}

func testValidSummary(goal string) string {
	return strings.Join([]string{
		"## Working State",
		"### Completed", "- " + goal,
		"### Current", "- inspect the implementation",
		"### Verification", "- (none)",
		"### Next", "- continue",
		"### Files", "- (none)",
	}, "\n")
}

func testOverflowModel(context, output float64) overflow.Model {
	return overflow.Model{
		Limit: calc.ModelLimit{Context: context, Output: output},
	}
}

func TestTailBudgetDefaultsAndExplicitValueWins(t *testing.T) {
	marks := overflow.CompactionWatermarks{Capacity: 500_000, High: 300_000, Low: 200_000}
	if got := tailBudget(overflow.Config{}, marks); got != 60_000 {
		t.Fatalf("default budget = %v, want 60000 (0.2 of high)", got)
	}
	explicit := float64(0)
	got := tailBudget(overflow.Config{Compaction: &overflow.CompactionConfig{
		PreserveRecentTokens: &explicit,
	}}, marks)
	if got != 0 {
		t.Fatalf("explicit zero budget = %v", got)
	}
	_ = testOverflowModel
}

func TestTailBudgetIsAFractionOfHigh(t *testing.T) {
	marks := overflow.CompactionWatermarks{Capacity: 500_000, High: 300_000, Low: 200_000}
	window := func(mutate func(*overflow.CompactionConfig)) overflow.Config {
		c := &overflow.CompactionConfig{Policy: overflow.PolicyWindow}
		if mutate != nil {
			mutate(c)
		}
		return overflow.Config{Compaction: c}
	}
	if got := tailBudget(window(nil), marks); got != 60_000 {
		t.Fatalf("default fraction budget = %v, want 60000 (0.2 of high)", got)
	}
	tenth := 0.1
	if got := tailBudget(window(func(c *overflow.CompactionConfig) { c.PreserveRecentFraction = &tenth }), marks); got != 30_000 {
		t.Fatalf("explicit fraction budget = %v, want 30000", got)
	}
	// Out-of-range fractions fall back to the default rather than producing
	// an empty or whole-context tail.
	for _, bad := range []float64{0, 1, 1.5, -0.2} {
		f := bad
		if got := tailBudget(window(func(c *overflow.CompactionConfig) { c.PreserveRecentFraction = &f }), marks); got != 60_000 {
			t.Fatalf("fraction %v budget = %v, want the 60000 default", bad, got)
		}
	}
	// An explicit token budget still wins over the fraction.
	tokens := 45_000.0
	if got := tailBudget(window(func(c *overflow.CompactionConfig) {
		c.PreserveRecentTokens = &tokens
		c.PreserveRecentFraction = &tenth
	}), marks); got != 45_000 {
		t.Fatalf("tokens should win over fraction: %v", got)
	}
}

func TestEstimateTokensCountsCharacters(t *testing.T) {
	cases := map[string]float64{
		"": 0, "a": 0, "ab": 1, "abcde": 1,
		"abcdef": 2, "😀": 0, "😀a": 1, "😀😀": 1,
	}
	for input, want := range cases {
		if got := estimateTokens(input); got != want {
			t.Errorf("estimateTokens(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestCompletedCompactionsRequireSuccessfulFinishedSummary(t *testing.T) {
	finish := "stop"
	summary := true
	compaction := msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses_1", MessageID: "uc"},
	}
	user := testUser("uc", compaction)
	valid := testValidSummary("Fix src/a.ts")
	ok := testAssistant("ac", "uc", textPart("ac", " "+valid+" "))
	assistant := ok.Info.(msgmodel.Assistant)
	assistant.Summary = &summary
	assistant.Finish = &finish
	ok.Info = assistant
	failed := ok
	failedAssistant := failed.Info.(msgmodel.Assistant)
	failedAssistant.ID = "af"
	converted := msgmodel.NewUnknownError("boom")
	failedAssistant.Error = &converted
	failed.Info = failedAssistant
	got := completedCompactions([]msgmodel.WithParts{user, ok, failed})
	if len(got) != 1 || got[0].Summary == nil || *got[0].Summary != valid {
		t.Fatalf("completed compactions = %#v", got)
	}
}

func TestValidateSummaryRejectsEmptyMalformedAndToolShapedOutput(t *testing.T) {
	validText := testValidSummary("Fix src/a.ts")
	valid := testAssistant("valid", "uc", textPart("valid", validText))
	if err := ValidateSummary(valid); err != nil {
		t.Fatalf("valid summary rejected: %v", err)
	}

	cases := map[string]msgmodel.WithParts{
		"empty":     testAssistant("empty", "uc"),
		"malformed": testAssistant("malformed", "uc", textPart("malformed", "I will inspect the code next.")),
		"missing section": testAssistant(
			"partial", "uc", textPart("partial", "## Goal\n- Fix src/a.ts"),
		),
		"tool shaped": testAssistant(
			"tool", "uc",
			textPart("tool", validText),
			msgmodel.ToolPart{
				PartBase: msgmodel.PartBase{
					ID: "p_tool", SessionID: "ses_1", MessageID: "tool",
				},
				CallID: "call_1", Tool: "bash", State: msgmodel.PendingToolState(),
			},
		),
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateSummary(message); err == nil {
				t.Fatal("invalid summary was accepted")
			}
		})
	}
}

func TestNormalizeOffFormatSummaryKeepsStateButRejectsContinuation(t *testing.T) {
	report := testAssistant("report", "uc", textPart(
		"report",
		"## Summary of Changes\n### Current\n- Added SortBy in src/cli.rs\n- cargo build: error: could not compile",
	))
	normalized, ok := NormalizeOffFormatSummary(report)
	if !ok || ValidateSummaryText(normalized) != nil {
		t.Fatalf("off-format state was not normalized:\n%s", normalized)
	}
	if !strings.Contains(normalized, "src/cli.rs") ||
		!strings.Contains(normalized, "error: could not compile") {
		t.Fatalf("normalization lost working state:\n%s", normalized)
	}
	if !strings.Contains(normalized, `> \### Current`) {
		t.Fatalf("recovered headings can collide with the summary envelope:\n%s", normalized)
	}

	continuation := testAssistant(
		"continue", "uc", textPart("continue", "Let me verify the edit by reading src/cli.rs."),
	)
	if _, ok := NormalizeOffFormatSummary(continuation); ok {
		t.Fatal("a coding continuation was mistaken for serialized state")
	}
}

func TestOverflowHistoryAndReplayPartSurgery(t *testing.T) {
	imageName := "photo.png"
	image := msgmodel.FilePart{
		PartBase: msgmodel.PartBase{ID: "pi", SessionID: "ses_1", MessageID: "u1"},
		Mime:     "image/png", Filename: &imageName, URL: "data:image/png;base64,AA",
	}
	plain := msgmodel.FilePart{
		PartBase: msgmodel.PartBase{ID: "pt", SessionID: "ses_1", MessageID: "u1"},
		Mime:     "text/plain", URL: "file:///a.txt",
	}
	compaction := msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses_1", MessageID: "uc"},
	}
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "old")),
		testAssistant("a0", "u0", textPart("a0", "reply")),
		testUser("u1", image, plain),
		testUser("uc", compaction),
	}
	selected := selectOverflowHistory(messages, "uc", true)
	if selected.Replay == nil || selected.Replay.Info.ID != "u1" ||
		len(selected.Messages) != 2 {
		t.Fatalf("overflow history = %#v", selected)
	}
	id := 0
	parts := buildReplayParts(*selected.Replay, "ses_new", "msg_new", func(prefix string) string {
		id++
		return fmt.Sprintf("%s_%d", prefix, id)
	})
	if len(parts) != 2 {
		t.Fatalf("replay parts = %#v", parts)
	}
	placeholder, ok := parts[0].(msgmodel.TextPart)
	if !ok || placeholder.Text != "[Attached image/png: photo.png]" ||
		placeholder.SessionID != "ses_new" || placeholder.MessageID != "msg_new" {
		t.Fatalf("placeholder = %#v", parts[0])
	}
	replayedFile, ok := parts[1].(msgmodel.FilePart)
	if !ok || replayedFile.ID != "part_2" || replayedFile.Mime != "text/plain" {
		t.Fatalf("plain replay = %#v", parts[1])
	}

	noHead := selectOverflowHistory(messages[2:], "uc", true)
	if noHead.Replay != nil || len(noHead.Messages) != 2 {
		t.Fatalf("fallback history = %#v", noHead)
	}
}

func TestAutoContinueStrings(t *testing.T) {
	if got := autoContinueText(false); got !=
		"The conversation was compacted: the state record above replaces the older transcript, and the most recent messages are retained verbatim. Continue from the current state." {
		t.Fatalf("normal continue = %q", got)
	}
	if got := autoContinueText(true); !stringsContainsAll(
		got, "exceeded the provider's size limit", "\n\nThe conversation was compacted",
	) {
		t.Fatalf("overflow continue = %q", got)
	}
}

func stringsContainsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
