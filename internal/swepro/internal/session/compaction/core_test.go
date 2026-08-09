package compaction

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/calc"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/overflow"
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

func testOverflowModel(context, output float64) overflow.Model {
	return overflow.Model{
		Limit: calc.ModelLimit{Context: context, Output: output},
	}
}

func TestPreserveRecentBudgetClampsAndExplicitValueWins(t *testing.T) {
	restore := overflow.SetEnvForTesting(map[string]string{})
	defer restore()
	cases := []struct {
		name    string
		context float64
		want    float64
	}{
		{name: "minimum", context: 10_000, want: 2_000},
		{name: "middle", context: 25_000, want: 2_521},
		{name: "maximum", context: 100_000, want: 8_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := preserveRecentBudget(
				overflow.Config{Compaction: &overflow.CompactionConfig{}},
				testOverflowModel(tc.context, 8_192),
			)
			if got != tc.want {
				t.Fatalf("budget = %v, want %v", got, tc.want)
			}
		})
	}
	explicit := float64(0)
	got := preserveRecentBudget(
		overflow.Config{Compaction: &overflow.CompactionConfig{
			PreserveRecentTokens: &explicit,
		}},
		testOverflowModel(100_000, 8_192),
	)
	if got != 0 {
		t.Fatalf("explicit zero budget = %v", got)
	}
}

func TestSelectKeepsWholeTurnsThenSplitsAnOversizedTurn(t *testing.T) {
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "first")),
		testAssistant("a0", "u0", textPart("a0", "reply")),
		testUser("u1", textPart("u1", "second")),
		testAssistant("a1", "u1", textPart("a1", "reply")),
	}
	preserve := float64(10)
	tailTurns := float64(2)
	cfg := overflow.Config{Compaction: &overflow.CompactionConfig{
		PreserveRecentTokens: &preserve, TailTurns: &tailTurns,
	}}
	model := Model{}
	estimate := func(messages []msgmodel.WithParts, _ Model) (float64, error) {
		return float64(len(messages) * 4), nil
	}
	selected, err := selectMessages(messages, cfg, model, estimate)
	if err != nil {
		t.Fatal(err)
	}
	if selected.TailStartID == nil || *selected.TailStartID != "u1" ||
		len(selected.Head) != 2 {
		t.Fatalf("selection = %#v", selected)
	}

	oneTurn := float64(1)
	cfg.Compaction.TailTurns = &oneTurn
	estimate = func(messages []msgmodel.WithParts, _ Model) (float64, error) {
		return float64(len(messages) * 6), nil
	}
	selected, err = selectMessages(messages, cfg, model, estimate)
	if err != nil {
		t.Fatal(err)
	}
	if selected.TailStartID == nil || *selected.TailStartID != "a1" ||
		len(selected.Head) != 3 {
		t.Fatalf("split selection = %#v", selected)
	}

	zero := float64(0)
	cfg.Compaction.TailTurns = &zero
	selected, err = selectMessages(messages, cfg, model, estimate)
	if err != nil || selected.TailStartID != nil || len(selected.Head) != len(messages) {
		t.Fatalf("zero tail selection = %#v, %v", selected, err)
	}
}

func TestEstimateTokensUsesUTF16AndJSMathRound(t *testing.T) {
	cases := map[string]float64{
		"": 0, "a": 0, "ab": 1, "abcde": 1,
		"abcdef": 2, "😀": 1, "😀a": 1,
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
	ok := testAssistant("ac", "uc", textPart("ac", " one "), textPart("ac2", "two"))
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
	if len(got) != 1 || got[0].Summary == nil || *got[0].Summary != "one\n\ntwo" {
		t.Fatalf("completed compactions = %#v", got)
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

func TestPinnedPromptAndAutoContinueStrings(t *testing.T) {
	base := "BASE"
	auditor := buildAuditorPin("auditor-light", "/repo")
	if !stringsContainsAll(auditor,
		"Rewrite /repo/.codeaf/auditor-verdict.json",
		"## Next Steps",
	) {
		t.Fatalf("auditor pin = %q", auditor)
	}
	if got := assemblePinnedPrompt(base, "", auditor); got != base+"\n\n"+auditor {
		t.Fatalf("pinned prompt = %q", got)
	}
	if got := buildAuditorPin("coder", "/repo"); got != "" {
		t.Fatalf("non-auditor pin = %q", got)
	}
	if got := autoContinueText(false); got !=
		"Continue if you have next steps, or stop and ask for clarification if you are unsure how to proceed." {
		t.Fatalf("normal continue = %q", got)
	}
	if got := autoContinueText(true); !stringsContainsAll(
		got, "exceeded the provider's size limit", "\n\nContinue if",
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

func TestWorkingSetDriftZeroWindowKeepsSourcePanic(t *testing.T) {
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "one")),
		testUser("u1", textPart("u1", "two")),
	}
	defer func() {
		if recover() == nil {
			t.Fatal("zero window should index beyond the real-turn array")
		}
	}()
	_ = WorkingSetDrift(messages, 0)
}
