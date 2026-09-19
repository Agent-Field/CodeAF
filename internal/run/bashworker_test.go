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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// finishCommand is the bash call a worker ends on: `plandb done` on its own
// task, spelled the way the plan line teaches it. Paired with realPlandbDoor
// the real CLI writes the store, and the worker detects the completion after
// the command runs (storeEnding) and ends the loop on it.
func finishCommand(id, result string) string {
	command := fmt.Sprintf("plandb done %s --agent %s --result %q", id, id, result)
	args, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{command})
	return string(args)
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

// THE LIVE STEP IS TRUE ONLY WHILE ITS COMMAND RUNS. The belt's begin event is
// what the run publishes as the task's live step — the number the step will be
// recorded under, the command, and the moment — and the step's own end line is
// what clears it. The run is read from BESIDE itself here: a goroutine hosts it,
// the test polls the store while the command is in flight, and a file handshake
// lets the command finish only once that reading has been seen.
func TestBashWorkerPublishesTheLiveStepWhileItsCommandRuns(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	workspace := t.TempDir()
	release := filepath.Join(workspace, "release")
	command := "while [ ! -f " + release + " ]; do sleep 0.02; done; echo done"
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":` + jsonString(command) + `}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("root", "the wait is over")), nil
		},
	}}
	worker := run.NewBashWorker(store, workspace, "test/model", seat)
	ctx := run.WithStepsPerTask(runContext(t), 9)

	done := make(chan error, 1)
	go func() {
		_, err := worker.Run(ctx, *store.Task(store.RootID()))
		done <- err
	}()

	live := waitForLiveStep(t, store, store.RootID())
	if live.Step != 1 {
		t.Fatalf("the live step number = %d, want the first step", live.Step)
	}
	if live.Command != command {
		t.Fatalf("the live command = %q, want the command the belt began", live.Command)
	}
	if live.Since.IsZero() {
		t.Fatal("the live step's moment is the zero time")
	}

	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatalf("release the command: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}
	// The finish cleared it: a task the store says is done is not running a
	// command, and the loop ended on that store ending, not on a reply.
	if after := store.Live(store.RootID()); !after.Empty() {
		t.Fatalf("the live step outlived its command: %#v", after)
	}
}

// Every ending of the loop clears the live step, so a stopped task never
// claims a present it is not in. The step cap is one such ending: the worker
// runs to its bound and comes home with the cap's own error, and the live row
// it published is gone.
func TestBashWorkerClearsTheLiveStepWhenTheCapStopsIt(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	// A WORKER THAT NEVER ENDS ITS TURN, so the only thing between it and
	// forever is the cap on its context.
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolReply(`{"command":"true"}`), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 3), *store.Task(store.RootID())); err == nil {
		t.Fatal("a worker that spent its step cap came home clean")
	}
	if after := store.Live(store.RootID()); !after.Empty() {
		t.Fatalf("a capped worker still claims a present: %#v", after)
	}
}

// And the wall is another: a command cut off by the run's own cancellation
// leaves no live step behind.
func TestBashWorkerClearsTheLiveStepWhenTheWallStopsIt(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	workspace := t.TempDir()
	release := filepath.Join(workspace, "release")
	command := "while [ ! -f " + release + " ]; do sleep 0.02; done"
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":` + jsonString(command) + `}`), nil
		},
	}}
	worker := run.NewBashWorker(store, workspace, "test/model", seat)

	ctx, cancel := context.WithCancel(runContext(t))
	done := make(chan error, 1)
	go func() {
		_, err := worker.Run(run.WithStepsPerTask(ctx, 9), *store.Task(store.RootID()))
		done <- err
	}()

	waitForLiveStep(t, store, store.RootID())
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a worker stopped by the wall came home clean")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the wall did not stop the worker")
	}
	if after := store.Live(store.RootID()); !after.Empty() {
		t.Fatalf("a worker the wall stopped still claims a present: %#v", after)
	}
}

// waitForLiveStep polls the store from beside the run until the task publishes
// a live step, and fails the test if it never does — the command is the file
// handshake, so the poll is bounded by the harness rather than by luck.
func waitForLiveStep(t *testing.T, store *plandb.Store, id string) plandb.LiveStep {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if live := store.Live(id); !live.Empty() {
			return live
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the live step was never published while the command ran")
	return plandb.LiveStep{}
}

// jsonString is one string as the JSON the belt's arguments carry, so a
// scripted command with quotes or brackets in it is spelled the way a model's
// own argument object would be.
func jsonString(s string) string {
	body, _ := json.Marshal(s)
	return string(body)
}

func TestBashWorkerRecordsItsStepsAndReportsThem(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	// THE LOOP ENDS IN THE STORE NOW. The first reply acts, the second finishes
	// with `plandb done` — the only clean ending a task has — and the worker
	// detects the completion after that command runs.
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"echo hi"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("root", "the greeting is in place")), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID()))

	if err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}
	if report.Steps != 2 {
		t.Fatalf("report steps = %d, want the command and the finish the script ran", report.Steps)
	}
	if report.Result != "the greeting is in place" {
		t.Fatalf("report result = %q, want the result the finish command carried", report.Result)
	}
	// THE TRAJECTORY IS THE RECORD. Two step lines — the command as the model
	// spelled it, and the finish — and the ending line under them, which names
	// the store's own completion.
	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("the trajectory reads %d steps, want the two the worker took", len(steps))
	}
	if steps[0].Command != "echo hi" {
		t.Fatalf("the recorded command = %q, want what the model asked for", steps[0].Command)
	}
	if !strings.Contains(steps[0].Observation, "hi") {
		t.Fatalf("the recorded observation = %q, want the command's output in it", steps[0].Observation)
	}
	lines := rawTrajectory(t, storeDir, store.RootID())
	if len(lines) != 4 {
		t.Fatalf("the trajectory holds %d lines, want the opening line, the two steps and the ending", len(lines))
	}
	end := endLine(t, lines)
	if end.Steps != 2 || end.Reason != "finished in the store" {
		t.Fatalf("the ending reads %d steps with reason %q, want the store's completion", end.Steps, end.Reason)
	}
}

func TestBashWorkerOpensOnARecordedPredecessorWithTheResumeSentence(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	// THE PREDECESSOR'S ONE STEP, already in the file: whatever it did is in
	// the tree now, and the fresh worker opens on that fact.
	writeStep(t, storeDir, store.RootID(), run.Step{Kind: "step", Step: 1, Command: "mkdir -p out", Observation: ""})
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("root", "resumed, inspected, and satisfied")), nil
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

// A RUN WORKER WHOSE WORKSPACE IS NOT A REPOSITORY RUNS `git clone`.
//
// The run's `-w` folder can be an empty directory — a person pointing a run at
// a fresh place — and then there is no copy of the person's work for the git
// guard to protect. An objective whose first step is `git clone` is the work,
// not a reach for somebody else's commits, so the guard stands down and the
// clone reaches bash. (The command's own failure is beside the point —
// /does/not/exist is not a repository — and what is asserted is that the
// REFUSAL never came back.)
func TestBashWorkerInANonRepositoryWorkspaceRunsGitClone(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	workspace := t.TempDir()
	// THE PREMISE IS THE TEST: a t.TempDir() sits inside a checkout whenever
	// GOTMPDIR or TMPDIR names one, and then this workspace IS in a repository.
	if workspaceInsideGitWorkTree(workspace) {
		t.Skipf("the temp workspace %s sits inside a git work tree, so it is not the empty folder this test is about", workspace)
	}
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(`{"command":"git clone /does/not/exist vendored"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolReply(finishCommand("root", "the clone was the first step")), nil
		},
	}}
	worker := run.NewBashWorker(store, workspace, "test/model", seat)

	if _, err := worker.Run(run.WithStepsPerTask(runContext(t), 9), *store.Task(store.RootID())); err != nil {
		t.Fatalf("the worker's run failed: %v", err)
	}

	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	if len(steps) < 1 || steps[0].Command != "git clone /does/not/exist vendored" {
		t.Fatalf("the trajectory reads %d steps, want the clone as the first", len(steps))
	}
	if strings.Contains(steps[0].Observation, "not yours to run") {
		t.Fatalf("git clone was refused in a workspace that is not a repository: %q", steps[0].Observation)
	}
	// AND IT REACHED GIT, whose own complaint about a source that is not there
	// is the proof the command ran rather than being answered by the guard.
	if !strings.Contains(steps[0].Observation, "does not exist") {
		t.Fatalf("the observation = %q, want git's own complaint about a missing source", steps[0].Observation)
	}
}

// workspaceInsideGitWorkTree is a local reading of the question the session's
// guard asks, so this run test can prove its own premise and skip rather than
// assert wrongly wherever a developer's GOTMPDIR or TMPDIR sits inside a
// checkout.
func workspaceInsideGitWorkTree(root string) bool {
	dir := filepath.Clean(root)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
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
	if len(lines) != 5 {
		t.Fatalf("the trajectory holds %d lines, want the opening line, three steps and the ending", len(lines))
	}
	for i, line := range lines[1:4] {
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
