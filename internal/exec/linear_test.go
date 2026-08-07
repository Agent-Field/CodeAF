package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// scriptedCompleter plays back a fixed sequence of model turns and records
// every message list it was shown, so a test can assert what the model saw.
type scriptedCompleter struct {
	turns  [][]ai.ToolCall
	errors []error
	delays []time.Duration
	seen   [][]ai.Message
}

func (s *scriptedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	copied := make([]ai.Message, len(messages))
	copy(copied, messages)
	s.seen = append(s.seen, copied)

	index := len(s.seen) - 1
	if index < len(s.delays) && s.delays[index] > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.delays[index]):
		}
	}
	if index < len(s.errors) && s.errors[index] != nil {
		return nil, s.errors[index]
	}
	message := ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "working"}}}
	if index < len(s.turns) {
		message.ToolCalls = s.turns[index]
	} else {
		message.Content = []ai.ContentPart{{Type: "text", Text: "done"}}
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: message, FinishReason: "stop"}},
		Usage:   &ai.Usage{PromptTokens: 10, CompletionTokens: 5},
	}, nil
}

func call(id, name, arguments string) ai.ToolCall {
	return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{Name: name, Arguments: arguments}}
}

func TestTaskImageInputAndViewImageReachTheNextModelTurn(t *testing.T) {
	space := workspace(t)
	path := filepath.Join(space.Root(), "input.png")
	if err := os.WriteFile(path, []byte("pixels"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &scriptedCompleter{turns: [][]ai.ToolCall{{
		call("view", "view_image", `{"path":"input.png"}`),
	}}}
	media := &MediaTools{
		Provider: &fakeMediaProvider{}, Catalog: fakeModalities{"vision/model:input:image": true},
		WorkingModel: "vision/model",
	}
	linear := NewLinear(client, space, nil, 10, 1_000_000, time.Minute).WithMedia(media)
	if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "inspect", ImagePaths: []string{path}}); err != nil {
		t.Fatal(err)
	}
	if len(client.seen) < 2 {
		t.Fatalf("model calls = %d", len(client.seen))
	}
	countImages := func(messages []ai.Message) int {
		count := 0
		for _, message := range messages {
			for _, part := range message.Content {
				if part.Type == "image_url" && part.ImageURL != nil {
					count++
				}
			}
		}
		return count
	}
	if countImages(client.seen[0]) != 1 {
		t.Fatalf("initial task turn images = %d", countImages(client.seen[0]))
	}
	if countImages(client.seen[1]) != 2 {
		t.Fatalf("next turn images = %d, want initial + view_image follow-up", countImages(client.seen[1]))
	}
}

func TestLinearObservesCooperativeCancelAtTurnBoundary(t *testing.T) {
	client := &scriptedCompleter{}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, Brief: "work", Control: func() ControlAction { return ControlCancel },
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopCancelled || len(client.seen) != 0 {
		t.Fatalf("outcome=%+v model calls=%d", outcome, len(client.seen))
	}
}

func TestCallFailureRetriesAndCompletes(t *testing.T) {
	client := &scriptedCompleter{errors: []error{
		fmt.Errorf("first timeout"),
		fmt.Errorf("second timeout"),
	}}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want done", outcome.Stop)
	}
	if outcome.Text != "done" {
		t.Fatalf("text = %q, want done", outcome.Text)
	}
	if len(client.seen) != 3 {
		t.Fatalf("calls = %d, want 3", len(client.seen))
	}
}

func TestDeadlineExhaustionLandsWithTranscriptOutcome(t *testing.T) {
	space := workspace(t)
	client := &scriptedCompleter{
		turns:  [][]ai.ToolCall{{call("c1", "write", `{"path":"result.txt","text":"partial"}`)}},
		delays: []time.Duration{925 * time.Millisecond, time.Second},
	}
	linear := NewLinear(client, space, nil, 10, 1_000_000, time.Second)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 2, Brief: "work"})
	if err == nil {
		t.Fatal("deadline exhaustion returned no error")
	}
	if outcome.Stop != StopDeadline {
		t.Fatalf("stop = %s, want deadline", outcome.Stop)
	}
	if outcome.Text == "" {
		t.Fatal("deadline outcome discarded the last assistant text")
	}
	if len(outcome.Artifacts) == 0 {
		t.Fatal("deadline outcome discarded the node artifacts")
	}

	var landed bool
	for _, message := range client.seen[len(client.seen)-1] {
		if message.Role == "user" && strings.Contains(message.Content[0].Text, "wall-clock deadline") {
			landed = true
		}
	}
	if !landed {
		t.Fatal("deadline landing instruction was not added to the transcript")
	}
}

// TestRepeatedReadAfterEditSeesTheNewContent guards the cache invalidation
// rule. The dedup cache once memoised results forever, so a read repeated
// after an edit served the pre-edit file presented as current — an answer that
// is confidently wrong, in the one workflow (modify, then re-check) where the
// model most needs the truth.
func TestRepeatedReadAfterEditSeesTheNewContent(t *testing.T) {
	space := workspace(t)
	if err := os.WriteFile(filepath.Join(space.Root(), "f.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}

	read := `{"cmd":"cat f.txt"}`
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "sh", read)},
		{call("c2", "edit", `{"path":"f.txt","old":"alpha","new":"beta"}`)},
		{call("c3", "sh", read)},
	}}
	linear := NewLinear(client, space, nil, 10, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "edit the file"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want done", outcome.Stop)
	}

	// The final model call saw the whole transcript; the last tool message in
	// it is the repeated read's result.
	final := client.seen[len(client.seen)-1]
	var lastTool string
	for _, message := range final {
		if message.Role == "tool" {
			lastTool = message.Content[0].Text
		}
	}
	if !strings.Contains(lastTool, "beta") {
		t.Fatalf("repeated read returned %q, want the post-edit content", lastTool)
	}
	if strings.Contains(lastTool, "identical call already made") {
		t.Fatalf("repeated read was served from the cache after a mutation: %q", lastTool)
	}
}

// TestResolveAcceptsAbsolutePathsInsideTheWorkspace guards the rule that cost
// a run its deliverable: an agent that just ran pwd writes absolute paths in
// good faith, and on macOS the same workspace has two spellings because /tmp
// is a symlink to /private/tmp.
func TestResolveAcceptsAbsolutePathsInsideTheWorkspace(t *testing.T) {
	space := workspace(t)
	for _, root := range []string{space.Root(), space.real} {
		inside := filepath.Join(root, "sub", "file.txt")
		resolved, err := space.Resolve(inside)
		if err != nil {
			t.Fatalf("Resolve(%q) = %v, want accepted", inside, err)
		}
		if want := filepath.Join(space.Root(), "sub", "file.txt"); resolved != want {
			t.Fatalf("Resolve(%q) = %q, want %q", inside, resolved, want)
		}
	}
	if _, err := space.Resolve("/somewhere/else/entirely"); err == nil {
		t.Fatal("Resolve accepted a path outside the workspace")
	}
	if _, err := space.Resolve("../sibling"); err == nil {
		t.Fatal("Resolve accepted a relative escape")
	}
}

// TestBudgetExhaustionLandsInsteadOfGuillotining guards the two-stage stop.
// A hard stop at the limit twice left files syntactically broken: the model's
// already-emitted repair calls were discarded unexecuted. Exhaustion must
// execute the paid-for calls and grant a bounded landing reserve, then stop.
func TestBudgetExhaustionLandsInsteadOfGuillotining(t *testing.T) {
	space := workspace(t)
	var turns [][]ai.ToolCall
	for index := 0; index < 10; index++ {
		turns = append(turns, []ai.ToolCall{call(
			fmt.Sprintf("c%d", index), "write",
			fmt.Sprintf(`{"path":"out-%d.txt","text":"x"}`, index))})
	}
	client := &scriptedCompleter{turns: turns}
	// A budget of 1 token is exhausted from the first turn, so the whole run
	// is landing: the first turn grants the reserve, the reserve counts down,
	// and the loop stops on its own well before the scripted turns run out.
	linear := NewLinear(client, space, nil, 50, 1, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 2, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopBudget {
		t.Fatalf("stop = %s, want budget", outcome.Stop)
	}
	if outcome.Turns >= 10 {
		t.Fatalf("ran %d turns, want the landing reserve to bound the run", outcome.Turns)
	}
	// Every turn that ran must have had its emitted call executed — that is
	// the difference between landing and guillotining.
	for index := 0; index < outcome.Turns; index++ {
		if _, err := os.Stat(filepath.Join(space.Root(), fmt.Sprintf("out-%d.txt", index))); err != nil {
			t.Fatalf("turn %d's emitted write was discarded: %v", index, err)
		}
	}
}

func TestReflexExecutorPromotesWithUsefulPartial(t *testing.T) {
	client := &scriptedCompleter{turns: [][]ai.ToolCall{{
		call("promote-1", "promote", `{"partial":"found two coupled migrations and preserved the schema notes"}`),
	}}}
	linear := NewLinear(client, workspace(t), nil, 4, 18_750, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 9, Brief: "Make the tiny schema change", Reflex: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Promote || outcome.Stop != StopPromote ||
		outcome.Text != "found two coupled migrations and preserved the schema notes" {
		t.Fatalf("promotion outcome = %+v", outcome)
	}
	if outcome.Turns != 1 || outcome.Verdict != provider.VerdictUnverifiedSuccess {
		t.Fatalf("promotion turns/verdict = %d/%s", outcome.Turns, outcome.Verdict)
	}
	if len(client.seen) != 1 || len(client.seen[0]) == 0 ||
		!strings.Contains(client.seen[0][0].Content[0].Text, "This assignment is a reflex") {
		t.Fatalf("reflex contract did not reach executor: %+v", client.seen)
	}
}

// The leaf's contract and the delivery gate must not pull in opposite
// directions. The worker is told to keep its final message short and put the
// long version in a file — which is precisely the pressure that produced a
// pointer where an answer belonged — so the paragraph has to name the split it
// means: the answer against its working, never the answer against a pointer to
// the answer.
func TestTheFinalMessageContractSplitsAnswerFromWorkingNotFromPointer(t *testing.T) {
	for _, required := range []string{
		"is the deliverable itself",
		"never end with a statement that the work is done",
		"The split is between the answer\nand its working, never between the answer and a pointer to the answer",
	} {
		if !strings.Contains(systemPrompt, required) {
			t.Fatalf("the leaf contract no longer resolves the pointer pressure: %q missing", required)
		}
	}
}

// The landing the budget orders is the case that matters, and it is the one the
// loop used to record as an ordinary finish.
//
// Exhaustion grants a reserve and tells the leaf to land; the leaf complies —
// that is what the instruction is for — and the next turn calls no tools, which
// is StopDone by every honest reading. Reading only Stop, the whole continuation
// subsystem was therefore dead on its designed path: a truncated partial posted
// as a finished deliverable, no re-decomposition ever ran, and the router's
// ledger recorded a success. Exhausted is what the two readings needed to be
// told apart.
func TestABudgetLandingThatCompliesStillReportsWhatRanOut(t *testing.T) {
	space := workspace(t)
	// One tool call, then nothing. Turn 0 spends the whole (one-token) budget
	// and is granted the reserve; turn 1 is the compliant final message.
	client := &scriptedCompleter{turns: [][]ai.ToolCall{{
		call("c0", "write", `{"path":"partial.md","text":"half of it"}`),
	}}}
	linear := NewLinear(client, space, nil, 50, 1, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 3, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %s, want done — the leaf did comply with the landing order", outcome.Stop)
	}
	if outcome.Exhausted != StopBudget {
		t.Fatalf("exhausted = %q, want budget — nothing else records that the leaf was still working", outcome.Exhausted)
	}
	if !outcome.Overran() {
		t.Fatal("Overran() is false, so re-decomposition never runs for a leaf that ran out of budget")
	}
	if outcome.Verdict != provider.VerdictBudgetStop {
		t.Fatalf("verdict = %s, want a budget stop so the leaf can escalate", outcome.Verdict)
	}
	if !outcome.Verdict.Escalates() {
		t.Fatal("a budget-blown leaf graded as a success; nothing will retry it on a stronger model")
	}
}

// A leaf that finishes inside its budget must be unchanged by all of the above:
// nothing ran out, so nothing is recorded, and the ending grades as it always
// did. This is the guard on the other side of the same fix — assigning the stop
// reason directly would have made every successful landing an escalating
// failure.
func TestAnOrdinaryFinishRecordsNothingExhausted(t *testing.T) {
	client := &scriptedCompleter{}
	linear := NewLinear(client, workspace(t), nil, 50, 150_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 4, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Stop != StopDone || outcome.Exhausted != "" || outcome.Overran() {
		t.Fatalf("outcome = stop %s, exhausted %q, overran %t", outcome.Stop, outcome.Exhausted, outcome.Overran())
	}
	if outcome.Verdict != provider.VerdictUnverifiedSuccess {
		t.Fatalf("verdict = %s, want the unchanged unverified success", outcome.Verdict)
	}
}

// An escalation that repeats the task verbatim buys a stronger model and pays
// it to rediscover what the first attempt already found — including the files
// sitting in the workspace it is about to write again.
func TestAnEscalatedAttemptIsShownWhatTheFirstOneProduced(t *testing.T) {
	graph := &plan.Graph{Goal: "ship it", Nodes: []plan.Node{{
		ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Investigate", Brief: "look into it",
		State: plan.StatePending, Verdict: provider.VerdictBudgetStop,
		Result: "the v2 endpoints are all 410 Gone", Artifacts: []string{"01-investigate.md"},
	}}}
	scheduler := &Scheduler{}
	task := scheduler.taskFor(graph, &graph.Nodes[0])
	if len(task.Inputs) != 1 {
		t.Fatalf("inputs = %+v, want the previous attempt carried into the retry", task.Inputs)
	}
	previous := task.Inputs[0]
	if !strings.Contains(previous.Result, "410 Gone") {
		t.Fatalf("the retry was not shown what the first attempt found: %q", previous.Result)
	}
	if len(previous.Artifacts) != 1 || previous.Artifacts[0] != "01-investigate.md" {
		t.Fatalf("the retry was not shown the file already written: %+v", previous.Artifacts)
	}
	if strings.TrimSpace(previous.Title) == "" {
		t.Fatal("the previous attempt arrived untitled, under a header saying it is work already done")
	}
}

// Inputs arrive under a header calling them work the leaf already has and must
// not gather again. Untitled they rendered as `=== from "" ===`, so a standing
// notebook lesson reading "check X before Y" arrived as an anonymous claim that
// X had been checked — and the artifact pointer, which is what makes the
// 300-word cap survivable, never fired at all on the resident path because the
// artifact list was never populated.
func TestTheBriefNamesEachInputAndRoutesToItsFiles(t *testing.T) {
	client := &scriptedCompleter{}
	linear := NewLinear(client, workspace(t), nil, 4, 150_000, time.Minute)
	if _, err := linear.Run(context.Background(), Task{
		NodeID: 5, Goal: "merge the findings", Brief: "merge them",
		Contract: "read every result in full before writing",
		Inputs: []Input{
			{Title: "your notebook", Result: "check the changelog before the source"},
			{Title: "task-1-n2", Result: "four defects, worst first",
				Artifacts: []string{"/workspace/job/findings.md"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if len(client.seen) == 0 {
		t.Fatal("no call was made")
	}
	first := client.seen[0]
	system, user := first[0].Content[0].Text, first[1].Content[0].Text
	if strings.Contains(user, `=== from "" ===`) {
		t.Fatalf("an input arrived unattributed:\n%s", user)
	}
	for _, want := range []string{`=== from "your notebook" ===`, `=== from "task-1-n2" ===`,
		"/workspace/job/findings.md"} {
		if !strings.Contains(user, want) {
			t.Fatalf("brief is missing %q:\n%s", want, user)
		}
	}
	// The working method belongs beside the harness's own invariants, in the
	// frozen prefix every turn is billed against — not in the user message
	// below everything that changes between leaves.
	if !strings.Contains(system, "read every result in full before writing") {
		t.Fatalf("the working method never reached the system message:\n%s", system)
	}
}
