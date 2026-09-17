package run_test

// The BashWorker's tests are scripted, not live — the same shape
// internal/session's bash-belt tests take (bashbelt_plandb_test.go): a fake
// provider answers every call out of a script, there is no key anywhere, the
// plan store is real at a real path, and the seat is judged by what the
// trajectory file holds and what the Report carried. Every run is bounded by
// its context, so a loop that stops moving fails the test instead of hanging
// it.
//
// THE SCRIPT IS THE WHOLE PROVIDER. A run worker has no task to name and no
// room to narrate to, and the session's checkpoint meter stands down for an
// InTask agent — so every call the seat makes is a turn round, and the script
// answers them in order. The plandb shim arms against a stub through its own
// override, which is all the CLI the worker's arming needs here.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// seat is the fake provider: one answer per call, in the order they arrive,
// and every request recorded so the opening document can be read back. A
// script spent answers empty prose — the turn's own ending — and an `ever`
// step answers everything, which is how a worker that never ends its turn is
// scripted.
type seat struct {
	mu       sync.Mutex
	script   []step
	ever     step
	seen     int
	requests [][]ai.Message
}

type step func(context.Context, []ai.Message) (*ai.Response, error)

func (s *seat) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	snapshot := append([]ai.Message(nil), messages...)
	s.mu.Lock()
	s.requests = append(s.requests, snapshot)
	var next step
	if s.ever != nil {
		next = s.ever
	} else if s.seen < len(s.script) {
		next = s.script[s.seen]
	}
	s.seen++
	s.mu.Unlock()
	if next == nil {
		return textReply(""), nil
	}
	return next(ctx, snapshot)
}

// opening is the worker's opening document: the user message that carries the
// plan line, whole — the only place the composed brief can be read.
func (s *seat) opening(t *testing.T) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, messages := range s.requests {
		for _, message := range messages {
			if message.Role == "user" && strings.Contains(messageContent(message), "YOUR TASK IN THE PLAN IS t-") {
				return messageContent(message)
			}
		}
	}
	t.Fatal("the worker's opening never carried its plan line")
	return ""
}

// textReply is one plain assistant answer, the shape a turn ends on.
func textReply(text string) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role:    "assistant",
			Content: []ai.ContentPart{{Type: "text", Text: text}},
		}}},
		Usage: &ai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

// toolReply is one bash call, the shape the belt's envelope accepts: exactly
// one call, name bash, arguments one command.
func toolReply(arguments string) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{
			Role: "assistant",
			ToolCalls: []ai.ToolCall{{
				ID:       "call-1",
				Type:     "function",
				Function: ai.ToolCallFunction{Name: "bash", Arguments: arguments},
			}},
		}}},
		Usage: &ai.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
	}
}

func messageContent(message ai.Message) string {
	var parts []string
	for _, part := range message.Content {
		parts = append(parts, part.Text)
	}
	return strings.Join(parts, "")
}

// stubCLI is the plandb shim's override: a binary that answers nothing and
// exits, which is all the arming probes through its override road.
func stubCLI(t *testing.T) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "stub-codeaf")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

// recordStep writes one predecessor's step into the task's trajectory file —
// the record's own format, marshalled through the reader's own type, because
// a line a resume road reads is a line the reader parses.
func writeStep(t *testing.T, storeDir, id string, step run.Step) {
	t.Helper()
	line, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	dir := plandb.TaskDir(storeDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trajectory.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rawTrajectory reads the task's trajectory file whole, the way the two-line
// and four-line counts are read: the reader answers the steps, and the ending
// line is read beside them.
func rawTrajectory(t *testing.T, storeDir, id string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(plandb.TaskDir(storeDir, id), "trajectory.jsonl"))
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func endLine(t *testing.T, lines []string) run.Step {
	t.Helper()
	var end run.Step
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &end); err != nil || end.Kind != "end" {
		t.Fatalf("the trajectory's last line is no ending: %q", lines[len(lines)-1])
	}
	return end
}

func TestBashWorkerRecordsItsStepsAndReportsThem(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo hi"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textReply("the greeting is in place"), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID()))

	if err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}
	if report.Steps != 1 {
		t.Fatalf("report steps = %d, want the one command the script ran", report.Steps)
	}
	// THE TRAJECTORY IS THE RECORD. One step line — the command as the model
	// spelled it, the observation it brought back — and the ending line under
	// it, which carries the worker's own account of the turn.
	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("the trajectory reads %d steps, want the one the worker took", len(steps))
	}
	if steps[0].Command != "echo hi" {
		t.Fatalf("the recorded command = %q, want what the model asked for", steps[0].Command)
	}
	if !strings.Contains(steps[0].Observation, "hi") {
		t.Fatalf("the recorded observation = %q, want the command's output in it", steps[0].Observation)
	}
	lines := rawTrajectory(t, storeDir, store.RootID())
	if len(lines) != 2 {
		t.Fatalf("the trajectory holds %d lines, want the step and the ending", len(lines))
	}
	end := endLine(t, lines)
	if end.Steps != 1 || end.Result == "" {
		t.Fatalf("the ending reads %d steps with %q, want the turn's own count and account", end.Steps, end.Result)
	}
}

func TestBashWorkerOpensOnARecordedPredecessorWithTheResumeSentence(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	// THE PREDECESSOR'S ONE STEP, already in the file: whatever it did is in
	// the tree now, and the fresh worker opens on that fact.
	writeStep(t, storeDir, store.RootID(), run.Step{Kind: "step", Step: 1, Command: "mkdir -p out", Observation: ""})
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textReply("resumed, inspected, and satisfied"), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID())); err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}

	brief := seat.opening(t)
	// THE CLAUSE IS ADDED, NOT SUBSTITUTED: the plan line names the task and
	// the finish command first, and the sentence about the predecessor rides
	// the same document.
	for _, want := range []string{
		"YOUR TASK IN THE PLAN IS t-root, claimed by agent root.",
		"your predecessor was interrupted mid-work; effects may exist in the tree — inspect before repeating anything",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the resumed worker's opening is missing %q:\n%s", want, brief)
		}
	}
}

func TestBashWorkerEndsItsLoopAtTheStepCap(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	// A WORKER THAT NEVER ENDS ITS TURN: every round asks for one more
	// command, so the only thing between it and forever is the cap on its
	// context.
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolReply(`{"command":"true"}`), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 3), *store.Task(store.RootID()))

	if err == nil {
		t.Fatal("a worker that spent its step cap came home clean")
	}
	if report.Steps != 3 {
		t.Fatalf("report steps = %d, want the cap the loop stopped at", report.Steps)
	}
	lines := rawTrajectory(t, storeDir, store.RootID())
	if len(lines) != 4 {
		t.Fatalf("the trajectory holds %d lines, want three steps and the ending", len(lines))
	}
	for i, line := range lines[:3] {
		var step run.Step
		if json.Unmarshal([]byte(line), &step) != nil || step.Kind != "step" || step.Step != i+1 {
			t.Fatalf("trajectory line %d is not step %d: %q", i, i+1, line)
		}
	}
	end := endLine(t, lines)
	if end.Steps != 3 || !strings.Contains(end.Reason, "step cap") {
		t.Fatalf("the ending reads %d steps with %q, want the cap's count and reason", end.Steps, end.Reason)
	}
}
