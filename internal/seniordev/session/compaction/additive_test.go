//go:build !windows

package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
)

// scriptedProcessor drives the fake summary processor with one outcome.
type scriptedProcessor struct {
	text     string // written as the summary text when non-empty
	result   steploop.Result
	failWith string // sets an assistant error when non-empty
	calls    int
	requests []SummaryRequest
}

func (sc *scriptedProcessor) factory(store *memoryStore) ProcessorFactory {
	return ProcessorFactoryFunc(func(
		_ context.Context, assistant *msgmodel.Assistant, _ string, _ Model,
	) (SummaryProcessor, error) {
		return &fakeProcessor{
			message: assistant,
			process: func(ctx context.Context, request SummaryRequest) (steploop.Result, error) {
				sc.calls++
				sc.requests = append(sc.requests, request)
				if sc.failWith != "" {
					converted := msgmodel.NewUnknownError(sc.failWith)
					assistant.Error = &converted
					if err := store.UpdateMessage(ctx, *assistant); err != nil {
						return steploop.ResultStop, err
					}
					return steploop.ResultStop, nil
				}
				finish := "stop"
				assistant.Finish = &finish
				assistant.Tokens = msgmodel.Tokens{Input: 1234, Output: 56}
				if err := store.UpdateMessage(ctx, *assistant); err != nil {
					return steploop.ResultStop, err
				}
				if sc.text != "" {
					if err := store.UpdatePart(ctx, msgmodel.TextPart{
						PartBase: msgmodel.PartBase{
							ID: "sp_" + assistant.ID, SessionID: "ses_1", MessageID: assistant.ID,
						},
						Text: sc.text,
					}); err != nil {
						return steploop.ResultStop, err
					}
				}
				result := sc.result
				if result == "" {
					result = steploop.ResultContinue
				}
				return result, nil
			},
		}, nil
	})
}

func additiveDeps(store *memoryStore, script *scriptedProcessor) (Dependencies, *[]CompactionDecision) {
	deps := baseDeps(store)
	deps.Provider = &fakeProvider{model: serviceModel(), provider: ProviderInfo{Source: "env"}}
	deps.Instance = InstanceContext{Directory: "/repo", Worktree: "/repo"}
	decisions := &[]CompactionDecision{}
	deps.Decisions = DecisionSinkFunc(func(d CompactionDecision) {
		*decisions = append(*decisions, d)
	})
	deps.Processors = script.factory(store)
	deps.ChangedFiles = func(context.Context) []string {
		return []string{" src/a.ts | 4 ++--"}
	}
	return deps, decisions
}

// sessionWithPriorBoundary is a session that already compacted once: the
// goal, a completed boundary (uc0 + its valid summary), the tail of that
// boundary, more work, and the new compaction parent uc.
func sessionWithPriorBoundary() []msgmodel.WithParts {
	finish := "stop"
	flag := true
	previous := testAssistant("as0", "uc0", textPart("as0", testValidSummary("Implemented parse() in src/a.ts")))
	info := previous.Info.(msgmodel.Assistant)
	info.Summary = &flag
	info.Finish = &finish
	previous.Info = info
	big := strings.Repeat("test output line\n", 600)
	return []msgmodel.WithParts{
		testUser("u0", textPart("u0", "Fix src/a.ts")),
		testUser("uc0", msgmodel.CompactionPart{
			PartBase: msgmodel.PartBase{ID: "pc0", SessionID: "ses_1", MessageID: "uc0"},
			Auto:     true,
		}),
		previous,
		// Long enough (~1,700 tokens) that a 1,500-token tail budget cannot
		// hold it once the truncated a2 is in.
		testAssistant("a1", "u0", textPart("a1", strings.Repeat("Continuing with the tests. ", 250))),
		testAssistant("a2", "u0", toolPartCompleted("a2", "c2", "bash", `{"cmd":"npm test"}`, big)),
		testAssistant("a3", "u0", textPart("a3", "Two tests still fail.")),
		testUser("uc", msgmodel.CompactionPart{
			PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses_1", MessageID: "uc"},
			Auto:     true,
		}),
	}
}

func selectableBoundaries(t *testing.T, store *memoryStore, parent string) int {
	t.Helper()
	msgs, _ := store.Messages(context.Background(), "ses_1")
	count := 0
	for _, m := range msgs {
		a, ok := m.Info.(msgmodel.Assistant)
		if !ok || !boolPointer(a.Summary) || a.ParentID != parent {
			continue
		}
		if a.Finish != nil && *a.Finish != "" && a.Error == nil {
			count++
		}
	}
	return count
}

func TestRejectedSummaryInstallsDeterministicRecordCarryingThePreviousSummary(t *testing.T) {
	messages := sessionWithPriorBoundary()
	store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
	script := &scriptedProcessor{text: "Let me keep going and read src/b.ts next."}
	deps, decisions := additiveDeps(store, script)

	result, err := NewService(deps).Process(context.Background(), ProcessInput{
		ParentID: "uc", Messages: messages, SessionID: "ses_1", Auto: true,
	})
	if err != nil || result != steploop.ResultContinue {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if script.calls != 1 {
		t.Fatalf("summary calls = %d, want exactly 1 (no retries)", script.calls)
	}
	d := (*decisions)[0]
	if d.SummaryStatus != "fallback" || d.SummaryClass != SummaryClassFormat ||
		!d.PreviousSummaryCarried || d.SummaryPromptTokens != 1234 || d.SummaryOutputTokens != 56 {
		t.Fatalf("decision = %#v", d)
	}
	if selectableBoundaries(t, store, "uc") != 1 {
		t.Fatal("the rejected attempt did not become the boundary")
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	prior := completedCompactions(fresh)
	record := *prior[len(prior)-1].Summary
	if !stringsContainsAll(record,
		"carried forward verbatim",
		"> - Implemented parse() in src/a.ts", // the previous summary, quoted as data
		"> \\### Completed",                   // its headings escaped
		"See the CHANGED FILES record",
	) {
		t.Fatalf("deterministic record:\n%s", record)
	}
	if strings.Contains(record, "read src/b.ts next") {
		t.Fatalf("the rejected continuation leaked into the record:\n%s", record)
	}
	// The prompt sent was the flattened head only: the newest message is the
	// verbatim tail and must not have been summarized.
	prompt := promptOf(t, script.requests[0])
	if !strings.Contains(prompt, "Continuing with the tests.") ||
		strings.Contains(prompt, "Two tests still fail.") {
		t.Fatalf("head/tail split is wrong in prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "<previous-summary>") {
		t.Fatalf("previous summary was not offered to the summarizer:\n%s", prompt)
	}
}

func TestTailIsKeptAndOlderToolOutputsAreTruncatedInTheStore(t *testing.T) {
	messages := sessionWithPriorBoundary()
	store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
	script := &scriptedProcessor{text: testValidSummary("tests running")}
	deps, decisions := additiveDeps(store, script)
	// Enough budget for the truncated a2 but not for an untruncated one:
	// a2's output is ~10,200 chars (~2,550 tokens); truncated it is ~4,100.
	budget := float64(1_500)
	deps.Config = ConfigProviderFunc(func(context.Context) (overflow.Config, error) {
		return overflow.Config{Compaction: &overflow.CompactionConfig{
			PreserveRecentTokens: &budget,
		}}, nil
	})

	if _, err := NewService(deps).Process(context.Background(), ProcessInput{
		ParentID: "uc", Messages: messages, SessionID: "ses_1", Auto: true,
	}); err != nil {
		t.Fatal(err)
	}
	d := (*decisions)[0]
	if d.SummaryStatus != "valid" || d.TailMessages != 2 || d.TailTruncatedOutputs != 1 {
		t.Fatalf("decision = %#v", d)
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	var tailStart *string
	var truncatedOutput string
	for _, m := range fresh {
		if m.Info.MessageID() == "uc" {
			tailStart = m.Parts[0].(msgmodel.CompactionPart).TailStartID
		}
		if m.Info.MessageID() == "a2" {
			truncatedOutput = m.Parts[0].(msgmodel.ToolPart).State.(msgmodel.ToolStateCompleted).Output
		}
	}
	if tailStart == nil || *tailStart != "a2" {
		t.Fatalf("tail start = %v, want a2", tailStart)
	}
	if !strings.Contains(truncatedOutput, "truncated at a context compaction") ||
		len(truncatedOutput) > 4_400 {
		t.Fatalf("old tool output was not truncated in the store: %d chars", len(truncatedOutput))
	}
	// And the projection places the tail after the summary: compaction user,
	// summary, then a2 and a3 verbatim, then the auto-continue user.
	projected := msgmodel.FilterCompacted(newestFirstMessages(fresh))
	order := []string{}
	for _, m := range projected {
		order = append(order, m.Info.MessageID())
	}
	summaryIndex := indexOfPrefix(order, "message_")
	if indexOf(order, "uc") != 0 || summaryIndex != 1 ||
		indexOf(order, "a2") != 2 || indexOf(order, "a3") != 3 || len(order) != 5 {
		t.Fatalf("projection order = %v", order)
	}
}

func TestSummaryCallErrorInstallsDeterministicRecordAndContinues(t *testing.T) {
	messages := compactionConversation("coder")
	store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
	script := &scriptedProcessor{failWith: "unexpected EOF"}
	deps, decisions := additiveDeps(store, script)

	result, err := NewService(deps).Process(context.Background(), ProcessInput{
		ParentID: "uc", Messages: messages, SessionID: "ses_1", Auto: true,
	})
	if err != nil || result != steploop.ResultContinue {
		t.Fatalf("result=%s err=%v", result, err)
	}
	d := (*decisions)[0]
	if d.SummaryStatus != "summary-error" || !strings.Contains(d.SummaryError, "unexpected EOF") {
		t.Fatalf("decision = %#v", d)
	}
	if selectableBoundaries(t, store, "uc") != 1 {
		t.Fatal("an errored summary call left no usable boundary")
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	if prior := completedCompactions(fresh); len(prior) != 1 ||
		!strings.Contains(*prior[0].Summary, "unexpected EOF") {
		t.Fatalf("record does not name the cause: %#v", prior)
	}
}

func TestNothingToSummarizeSkipsTheModelCall(t *testing.T) {
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "Fix src/a.ts")),
		testUser("uc", msgmodel.CompactionPart{
			PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses_1", MessageID: "uc"},
			Auto:     true,
		}),
	}
	store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
	script := &scriptedProcessor{text: testValidSummary("never used")}
	deps, decisions := additiveDeps(store, script)

	result, err := NewService(deps).Process(context.Background(), ProcessInput{
		ParentID: "uc", Messages: messages, SessionID: "ses_1", Auto: true,
	})
	if err != nil || result != steploop.ResultContinue {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if script.calls != 0 {
		t.Fatalf("summary calls = %d, want 0", script.calls)
	}
	if d := (*decisions)[0]; d.SummaryStatus != "no-head" || d.TranscriptMessages != 0 || d.TailMessages != 1 {
		t.Fatalf("decision = %#v", d)
	}
	if selectableBoundaries(t, store, "uc") != 1 {
		t.Fatal("no boundary was installed")
	}
	// The whole history is the tail, and the projection must still carry it:
	// compaction user, record, then u0 verbatim, then the auto-continue.
	fresh, _ := store.Messages(context.Background(), "ses_1")
	tailStart := fresh[1].Parts[0].(msgmodel.CompactionPart).TailStartID
	if tailStart == nil || *tailStart != "u0" {
		t.Fatalf("no-head boundary did not name the tail: %v", tailStart)
	}
	order := []string{}
	for _, m := range msgmodel.FilterCompacted(newestFirstMessages(fresh)) {
		order = append(order, m.Info.MessageID())
	}
	if len(order) != 4 || order[0] != "uc" || order[2] != "u0" {
		t.Fatalf("projection after a no-head boundary = %v", order)
	}
}

func TestChangedFilesPinIsInstalledBesideTheSummary(t *testing.T) {
	for name, script := range map[string]*scriptedProcessor{
		"valid":    {text: testValidSummary("x")},
		"rejected": {text: "not a record"},
	} {
		t.Run(name, func(t *testing.T) {
			messages := compactionConversation("coder")
			store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
			deps, _ := additiveDeps(store, script)
			if _, err := NewService(deps).Process(context.Background(), ProcessInput{
				ParentID: "uc", Messages: messages, SessionID: "ses_1", Auto: false,
			}); err != nil {
				t.Fatal(err)
			}
			fresh, _ := store.Messages(context.Background(), "ses_1")
			prior := completedCompactions(fresh)
			if len(prior) != 1 {
				t.Fatalf("completed compactions = %#v", prior)
			}
			pinned := false
			for _, raw := range fresh[prior[0].AssistantIndex].Parts {
				part, ok := raw.(msgmodel.TextPart)
				if ok && strings.Contains(string(part.Metadata), `"changed_files"`) &&
					strings.Contains(part.Text, "src/a.ts | 4 ++--") && boolPointer(part.Synthetic) {
					pinned = true
				}
			}
			if !pinned {
				t.Fatal("changed-files pin missing")
			}
		})
	}
}

func TestFallbackRecordAlwaysValidates(t *testing.T) {
	previous := "## Working State\n### Completed\n- did things\n### Current\n- x\n### Verification\n- y\n### Next\n- z\n### Files\n- f"
	for name, record := range map[string]string{
		"with previous": fallbackRecord(&previous, "task", errString("boom"), true),
		"bare":          fallbackRecord(nil, "", nil, false),
	} {
		if err := ValidateSummaryText(record); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, record)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func newestFirstMessages(messages []msgmodel.WithParts) []msgmodel.WithParts {
	out := make([]msgmodel.WithParts, len(messages))
	for i := range messages {
		out[len(messages)-1-i] = messages[i]
	}
	return out
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}

func indexOfPrefix(values []string, prefix string) int {
	for i, value := range values {
		if strings.HasPrefix(value, prefix) {
			return i
		}
	}
	return -1
}
