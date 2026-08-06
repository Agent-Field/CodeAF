package exec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
