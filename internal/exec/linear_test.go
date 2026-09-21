package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plan"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/store"
)

// scriptedCompleter plays back a fixed sequence of model turns and records
// every message list it was shown, so a test can assert what the model saw.
type scriptedCompleter struct {
	turns    [][]ai.ToolCall
	finishes []string
	errors   []error
	delays   []time.Duration
	seen     [][]ai.Message
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
	finish := "stop"
	if index < len(s.finishes) && strings.TrimSpace(s.finishes[index]) != "" {
		finish = s.finishes[index]
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: message, FinishReason: finish}},
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

// The same material, fetched twice, is carried once.
//
// Measured over 332 leaf turns, 12.5% of every observation byte the loop paid
// for was material it had already been shown — 27 duplicate fetches, none of
// which the old call-keyed memo caught, because any successful sh emptied it
// and sh is in 87% of turns. Keying on the bytes catches all of them: the call
// still runs, and only the second copy of its answer is replaced by a line
// saying where the first one is.
func TestTheSameOutputFetchedTwiceIsCarriedOnce(t *testing.T) {
	space := workspace(t)
	body := strings.Repeat("the quick brown fox\n", 200)
	if err := os.WriteFile(filepath.Join(space.Root(), "notes.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	read := `{"cmd":"cat notes.txt"}`
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "sh", read)},
		{call("c2", "sh", `{"cmd":"echo thinking"}`)},
		{call("c3", "sh", read)},
	}}
	linear := NewLinear(client, space, nil, 10, 1_000_000, time.Minute)
	if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "read the notes"}); err != nil {
		t.Fatal(err)
	}

	final := client.seen[len(client.seen)-1]
	var results []string
	for _, message := range final {
		if message.Role == "tool" {
			results = append(results, message.Content[0].Text)
		}
	}
	if len(results) != 3 {
		t.Fatalf("the transcript carries %d tool results, want three", len(results))
	}
	if !strings.Contains(results[0], "quick brown fox") {
		t.Fatalf("the first read was not carried in full: %q", results[0])
	}
	if strings.Contains(results[2], "quick brown fox") {
		t.Fatalf("the repeated read was carried a second time: %d bytes", len(results[2]))
	}
	if !strings.Contains(results[2], "turn 1") {
		t.Fatalf("the repeated read does not point at the first copy: %q", results[2])
	}
	if len(results[2]) >= len(results[0])/8 {
		t.Fatalf("the pointer is %d bytes against a %d-byte first copy", len(results[2]), len(results[0]))
	}
}

// TestRepeatedReadAfterEditSeesTheNewContent guards the correctness half, and
// it is the reason the old memo had to go rather than be tuned. That memo
// answered a repeated call from the previous result without running it, so a
// read repeated after an edit served the pre-edit file presented as current —
// confidently wrong, in the one workflow (modify, then re-check) where the
// model most needs the truth. Content addressing cannot make that mistake: the
// call runs, and bytes that differ are simply different bytes.
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
	if strings.Contains(lastTool, "identical to the result of") {
		t.Fatalf("a read whose answer had changed was replaced by a pointer: %q", lastTool)
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
	if outcome.Turns != 1 || outcome.Verdict != provider.ReadingUnverifiedSuccess {
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

// A thing was built, every part of it was exercised in a harness, and the leaf
// reported that everything was verified — while the person who opened it could
// not do the one thing they had asked for. Three sentences stand between the
// harness and that report: the user's first use is settled before building, the
// check is the whole path rather than the parts, and "verified" is a word that
// costs an actual run. The fourth keeps the honest exit open, because a law
// with no honest exit is a law that teaches the lie.
func TestTheLeafMustEarnTheWordVerified(t *testing.T) {
	for name, required := range map[string]string{
		"the user's first use is settled before building": "settle before you build what their first\nreal use looks like",
		"proportional to the ask":                         "a one-shot\nartefact needs no ceremony beyond being right",
		"the check is the whole path":                     "exercising the whole of it the way its eventual user would reach it,\nnot part by part",
		"verified is protected":                           "Verified is a word you earn by running the finished thing the way it will be\nused",
		"no inference across the join":                    "never reason from working pieces\nto a working result",
		"the honest gap is the way out":                   "name the part that is unverified and\nhand over the one short check that settles it",
	} {
		if !strings.Contains(systemPrompt, required) {
			t.Errorf("the leaf contract no longer states %s: %q missing", name, required)
		}
	}
	// Generic by construction: the craft is about the shape of the claim, never
	// about a kind of thing being built.
	for _, forbidden := range []string{"browser", "GUI", "web app", "headless"} {
		if strings.Contains(strings.ToLower(systemPrompt), strings.ToLower(forbidden)) {
			t.Errorf("the leaf contract grew a domain specific: %q", forbidden)
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
	if outcome.Verdict != provider.ReadingBudgetStop {
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
	if outcome.Verdict != provider.ReadingUnverifiedSuccess {
		t.Fatalf("verdict = %s, want the unchanged unverified success", outcome.Verdict)
	}
}

// An escalation that repeats the task verbatim buys a stronger model and pays
// it to rediscover what the first attempt already found — including the files
// sitting in the workspace it is about to write again.
func TestAnEscalatedAttemptIsShownWhatTheFirstOneProduced(t *testing.T) {
	graph := &plan.Graph{Goal: "ship it", Nodes: []plan.Node{{
		ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Investigate", Brief: "look into it",
		State: plan.StatePending, Verdict: provider.ReadingBudgetStop,
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
	// The working method leads the brief, and is nowhere in the system message.
	//
	// It used to be appended to the system message, on the theory that it
	// belonged beside the harness's invariants in the frozen prefix. That got
	// the economics backwards: a contract written for THIS node made the system
	// message per-node, so four leaves of one job — launched at once, same
	// model, same invariants — agreed on nothing and each wrote the shared
	// prefix cold. The prefix is worth more than the placement, and the head of
	// the brief still reaches the model ahead of the assignment.
	if strings.Contains(system, "read every result in full before writing") {
		t.Fatalf("a per-node working method is back in the shared system message:\n%s", system)
	}
	if !strings.HasPrefix(user, "How this particular kind of job is done well:\nread every result in full before writing") {
		t.Fatalf("the brief does not lead with the working method:\n%s", user)
	}
}

// The gate above this package used to judge a final message with nothing to
// check it against. A count of tool calls settles nothing; what a reader of a
// finished job needs is whether the check the deliverable claims appears
// anywhere in the run. The record is the tail of what actually ran, it names a
// failed call as failed, and it is bounded — a leaf that ran for an hour must
// not hand its whole history to whoever asks.
func TestTheRunRecordsWhatItActuallyRan(t *testing.T) {
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "write", `{"path":"result.txt","text":"the answer"}`)},
		{call("c2", "sh", `{"command":"cat missing.txt"}`)},
	}}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Ran) != 2 {
		t.Fatalf("run record = %v, want both calls", outcome.Ran)
	}
	if !strings.HasPrefix(outcome.Ran[0], `write {"path":"result.txt"`) {
		t.Errorf("the write is not recorded as itself: %q", outcome.Ran[0])
	}
	if !strings.Contains(outcome.Ran[1], `cat missing.txt`) {
		t.Errorf("the command is not recorded: %q", outcome.Ran[1])
	}
	if !strings.HasSuffix(outcome.Ran[1], "→ error") {
		t.Errorf("a call that failed is recorded as though it worked: %q", outcome.Ran[1])
	}
}

// A LEAF'S DONE CARRIES THE COMMANDS IT ACTUALLY ISSUED. A write is not a
// command, and a batch refused at the output limit never executed, so neither
// is allowed to look like shell work the leaf performed.
func TestALandingCarriesTheCommandsTheLeafRan(t *testing.T) {
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "sh", `{"cmd":"go build ./..."}`)},
	}}
	outcome, err := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute).
		Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.CommandsRun != 1 || len(outcome.Commands) != 1 || outcome.Commands[0] != "go build ./..." {
		t.Fatalf("commands = %v of %d run, want the one command the leaf issued",
			outcome.Commands, outcome.CommandsRun)
	}

	writeOnly := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "write", `{"path":"result.txt","text":"done"}`)},
	}}
	written, err := NewLinear(writeOnly, workspace(t), nil, 10, 1_000_000, time.Minute).
		Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if written.CommandsRun != 0 || len(written.Commands) != 0 {
		t.Fatalf("a write was recorded as commands = %v of %d run", written.Commands, written.CommandsRun)
	}

	refusedSpace := workspace(t)
	refused := &scriptedCompleter{
		turns:    [][]ai.ToolCall{{call("c1", "sh", `{"cmd":"touch refused.txt"}`)}},
		finishes: []string{"length"},
	}
	notRun, err := NewLinear(refused, refusedSpace, nil, 10, 1_000_000, time.Minute).
		Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if notRun.CommandsRun != 0 || len(notRun.Commands) != 0 {
		t.Fatalf("an unexecuted batch was recorded as commands = %v of %d run",
			notRun.Commands, notRun.CommandsRun)
	}
	if _, err := os.Stat(filepath.Join(refusedSpace.Root(), "refused.txt")); !os.IsNotExist(err) {
		t.Fatalf("the refused shell batch ran anyway: %v", err)
	}
}

// A leaf recorded done after changing the tree, but its account named no check
// and no absence of one; naming only the build it ran made that unchecked
// landing read like checked work.
func TestALeafThatChangedFilesAndRanNoCheckSaysSoWhenItLands(t *testing.T) {
	writeOnly := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "write", `{"path":"result.txt","text":"done"}`)},
	}}
	outcome, err := NewLinear(writeOnly, workspace(t), nil, 10, 1_000_000, time.Minute).
		Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Account == nil {
		t.Fatal("a leaf that changed the tree landed with no account")
	}
	if report := outcome.Account.Report(); !strings.Contains(report, noCheckWords) {
		t.Fatalf("landing account did not say no check was run:\n%s", report)
	}
	if summary := outcome.Account.Summary(); !strings.Contains(summary, noCheckWords) {
		t.Fatalf("landing summary did not say no check was run: %q", summary)
	}
	if outcome.Account.CommandsRun != 0 || len(outcome.Account.Commands) != 0 {
		t.Fatalf("landing account claims commands = %v of %d run",
			outcome.Account.Commands, outcome.Account.CommandsRun)
	}

	withBuild := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("c1", "write", `{"path":"result.txt","text":"done"}`)},
		{call("c2", "sh", `{"cmd":"go build ./..."}`)},
	}}
	built, err := NewLinear(withBuild, workspace(t), nil, 10, 1_000_000, time.Minute).
		Run(context.Background(), Task{NodeID: 1, Brief: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if built.Account == nil {
		t.Fatal("a leaf that changed the tree and ran a build landed with no account")
	}
	report := built.Account.Report()
	if !strings.Contains(report, "What the work ran itself:\n  go build ./...") {
		t.Fatalf("landing account did not name the build the leaf ran:\n%s", report)
	}
	if !strings.Contains(report, noCheckWords) {
		t.Fatalf("landing account treated the leaf's build as a check:\n%s", report)
	}
}

// THE COMMAND RECORD IS A BOUNDED TAIL AND SAYS HOW MUCH PRECEDED IT. That
// keeps a long run from filling the next reader's context without presenting
// the newest commands as the whole history.
func TestTheCommandRecordIsBoundedAndSaysWhatItLeftOut(t *testing.T) {
	var outcome Outcome
	total := commandsKept + 7
	for index := 0; index < total; index++ {
		outcome.noteCommand(call("c", "sh", fmt.Sprintf(`{"cmd":"step %d"}`, index)))
	}
	if len(outcome.Commands) != commandsKept {
		t.Fatalf("command record kept %d commands, want the newest %d", len(outcome.Commands), commandsKept)
	}
	if outcome.CommandsRun != total {
		t.Fatalf("CommandsRun = %d, want all %d issued commands", outcome.CommandsRun, total)
	}
	if outcome.Commands[0] != fmt.Sprintf("step %d", total-commandsKept) {
		t.Errorf("oldest kept command = %q, want the first command after the cut", outcome.Commands[0])
	}
	if outcome.Commands[len(outcome.Commands)-1] != fmt.Sprintf("step %d", total-1) {
		t.Errorf("newest command was dropped: %q", outcome.Commands[len(outcome.Commands)-1])
	}

	var clipped Outcome
	clipped.noteCommand(call("c", "sh", `{"cmd":"`+strings.Repeat("x", ranArgumentBytes*3)+`"}`))
	if len(clipped.Commands) != 1 || len(clipped.Commands[0]) > ranArgumentBytes+len("…") {
		t.Fatalf("clipped command = %q (%d bytes), want at most %d bytes plus the cut mark",
			clipped.Commands[0], len(clipped.Commands[0]), ranArgumentBytes)
	}
}

func TestTheRunRecordIsATailAndNotATranscript(t *testing.T) {
	var outcome Outcome
	for index := 0; index < ranLimit*3; index++ {
		outcome.record(call("c", "sh", fmt.Sprintf(`{"command":"step %d"}`, index)), false)
	}
	if len(outcome.Ran) != ranLimit {
		t.Fatalf("run record kept %d calls, want the last %d", len(outcome.Ran), ranLimit)
	}
	if !strings.Contains(outcome.Ran[len(outcome.Ran)-1], fmt.Sprintf("step %d", ranLimit*3-1)) {
		t.Errorf("the record dropped the newest call: %q", outcome.Ran[len(outcome.Ran)-1])
	}
	// A pasted file in one argument must not carry the whole file into a
	// judge's context.
	outcome.record(call("c", "write", `{"path":"big.txt","text":"`+strings.Repeat("x", 4000)+`"}`), false)
	if got := len(outcome.Ran[len(outcome.Ran)-1]); got > ranArgumentBytes+64 {
		t.Errorf("one recorded call is %d bytes, want it clipped near %d", got, ranArgumentBytes)
	}
}

// A refusal of the request itself — the provider's own words, a 4xx the router
// issued on our account — is handed back at once. Sending the same body into
// the same wall three times is three bills for one answer that was already here.
func TestARefusalOfOurOwnRequestIsNotRetried(t *testing.T) {
	client := &scriptedCompleter{errors: []error{
		&provider.APIError{Status: 400, Message: "max_tokens is too large for this endpoint"},
	}}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute)
	if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "work"}); err == nil {
		t.Fatal("a refused request must surface, not be retried into success")
	}
	if len(client.seen) != 1 {
		t.Fatalf("calls = %d, want exactly one: the refusal is the answer", len(client.seen))
	}
}

// shelfOnDisk puts one active skill on a real store's shelf — the candidate
// plus the activation, the only transition [Store.SkillFacts] surfaces — and
// returns the store, as every other reader of the shelf opens it.
func shelfOnDisk(t *testing.T, name, body string) *store.Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	candidate, err := db.RecordSkillCandidate(store.RootID, "repo:/test", body, "/shelf/"+name)
	if err != nil {
		t.Fatalf("record skill candidate %q: %v", body, err)
	}
	if err := db.ActivateSkill(candidate.Seq, "/shelf/"+name, ""); err != nil {
		t.Fatalf("activate skill %q: %v", name, err)
	}
	return db
}

// TestABriefRendersAttachedSkillsInOrder is the render half of the
// attachment: a leaf whose plan attached skills reads them beside the working
// method, in the order the plan composed, each with its doc and shelf path —
// and a name the shelf does not hold renders as nothing rather than as an
// empty bullet.
func TestABriefRendersAttachedSkillsInOrder(t *testing.T) {
	db := shelfOnDisk(t, "imgshrink", "optimize images without losing quality")
	candidate, err := db.RecordSkillCandidate(store.RootID, "repo:/test", "gofmt vet and lint the tree", "/shelf/lint")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateSkill(candidate.Seq, "/shelf/lint", ""); err != nil {
		t.Fatal(err)
	}
	linear := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute).WithStore(db)
	got := linear.brief(Task{Brief: "Do the thing.", Skills: []string{"lint", "imgshrink", "nothere"}})

	if !strings.Contains(got, "Skills attached to this work:\n") {
		t.Fatalf("the brief never rendered the attached skills:\n%s", got)
	}
	if !strings.Contains(got, "- gofmt vet and lint the tree [/shelf/lint]\n") {
		t.Errorf("the lint skill's doc line is missing:\n%s", got)
	}
	if !strings.Contains(got, "- optimize images without losing quality [/shelf/imgshrink]\n") {
		t.Errorf("the imgshrink skill's doc line is missing:\n%s", got)
	}
	if !strings.Contains(got, "Earlier-listed skills win when two skills conflict.") {
		t.Errorf("the precedence line is missing:\n%s", got)
	}
	// Order is precedence: the plan pinned lint first, so its line leads.
	if lint, shrink := strings.Index(got, "- gofmt vet and lint"), strings.Index(got, "- optimize images"); lint < 0 || shrink < 0 || lint > shrink {
		t.Errorf("skill lines are not in the composed order (lint at %d, imgshrink at %d):\n%s", lint, shrink, got)
	}
	if strings.Contains(got, "nothere") {
		t.Errorf("a name the shelf does not hold rendered anyway:\n%s", got)
	}
}

// TestABriefWithoutSkillsRendersExactlyAsBefore is the zero render: a leaf
// with nothing attached — and a leaf whose names resolve to nothing, and a
// loop with no store at all — reads byte for byte what it read before
// attachment existed.
func TestABriefWithoutSkillsRendersExactlyAsBefore(t *testing.T) {
	db := shelfOnDisk(t, "imgshrink", "optimize images without losing quality")
	withShelf := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute).WithStore(db)
	withoutShelf := NewLinear(&scriptedCompleter{}, workspace(t), nil, 10, 1_000_000, time.Minute)

	base := withShelf.brief(Task{Brief: "Do the thing."})
	if got := withShelf.brief(Task{Brief: "Do the thing.", Skills: nil}); got != base {
		t.Fatalf("nil skills changed the brief:\ngot:  %q\nwant: %q", got, base)
	}
	if got := withShelf.brief(Task{Brief: "Do the thing.", Skills: []string{"nothere"}}); got != base {
		t.Fatalf("unresolvable names changed the brief:\ngot:  %q\nwant: %q", got, base)
	}
	if got := withoutShelf.brief(Task{Brief: "Do the thing.", Skills: []string{"imgshrink"}}); got != base {
		t.Fatalf("attached names with no shelf behind them changed the brief:\ngot:  %q\nwant: %q", got, base)
	}
}
