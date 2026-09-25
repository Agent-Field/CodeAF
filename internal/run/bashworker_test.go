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
	"sync/atomic"
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

func TestBashWorkerContinuesTaskNumbersButCapsAndReportsThisRun(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	id := store.RootID()
	dir := plandb.TaskDir(storeDir, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var record []byte
	for n := 1; n <= 3; n++ {
		line, err := json.Marshal(run.Step{Kind: "step", Step: n, Command: fmt.Sprintf("echo old-%d", n)})
		if err != nil {
			t.Fatal(err)
		}
		record = append(record, append(line, '\n')...)
	}
	if err := os.WriteFile(filepath.Join(dir, "trajectory.jsonl"), record, 0o644); err != nil {
		t.Fatal(err)
	}
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolReply(`{"command":"echo new"}`), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 2), *store.Task(id))
	if err == nil || !strings.Contains(err.Error(), "stopped at its step cap after 2 steps") {
		t.Fatalf("run error = %v, want this run stopped at its two-step cap", err)
	}
	if report.Steps != 2 {
		t.Fatalf("report steps = %d, want only this run's two steps", report.Steps)
	}
	steps, err := run.Trajectory(storeDir, id)
	if err != nil {
		t.Fatal(err)
	}
	var numbers []int
	for _, step := range steps {
		numbers = append(numbers, step.Step)
	}
	if got := fmt.Sprint(numbers); got != "[1 2 3 4 5]" {
		t.Fatalf("recorded step numbers = %s, want [1 2 3 4 5]", got)
	}
	end := endLine(t, rawTrajectory(t, storeDir, id))
	if end.Steps != 2 {
		t.Fatalf("ending steps = %d, want only this run's two steps", end.Steps)
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
	// The bound is on work, not just on what the recorder admits afterwards.
	// A fourth request has already spent past the cap even if its end event
	// is discarded, so count the provider calls as well as the written steps.
	seat.mu.Lock()
	calls := seat.seen
	seat.mu.Unlock()
	if calls != 3 {
		t.Fatalf("the provider received %d calls, want exactly the three allowed steps", calls)
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

// THE WORKER SAYS WHAT IT HAS SPENT WHILE IT IS STILL WORKING, not only when it
// comes home. A run's dollar limit is read from these figures, so the first
// paid call must be told to the run before the worker's turn is over, every
// figure must be the whole of what the worker has spent so far, and the last
// one must be the figure the report carries: two accounts of one worker's money
// would drift.
//
// THE SEAT HOLDS THE SECOND CALL UNTIL THE FIRST IS REPORTED, which is what makes
// "while it is still working" a fact and not a race: the figures are readings of
// a running total and a fast worker may fold several calls into one reading, so
// their count is not the property. A worker that reports only at its turn's
// end never frees the seat, and the first figure it then gives is not the first
// call's.
func TestBashWorkerReportsItsSpendWhileItIsStillWorking(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	firstReported := make(chan struct{})
	var calls atomic.Int32
	seat := &seat{ever: func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		if calls.Add(1) > 1 {
			select {
			case <-firstReported:
			case <-ctx.Done():
			case <-time.After(10 * time.Second):
			}
		}
		reply := toolReply(`{"command":"true"}`)
		cost := 0.25
		reply.Usage.Cost = &cost
		return reply, nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)
	var mu sync.Mutex
	var figures []float64
	ctx := run.WithSpendBank(run.WithStepsPerTask(runContext(t), 3), func(usd float64) {
		mu.Lock()
		figures = append(figures, usd)
		first := len(figures) == 1
		mu.Unlock()
		if first {
			close(firstReported)
		}
	})

	report, _ := worker.Run(ctx, *store.Task(store.RootID()))

	mu.Lock()
	defer mu.Unlock()
	if len(figures) == 0 || figures[0] != 0.25 {
		t.Fatalf("spend figures = %v, want the first paid call's 0.25 told before the second call", figures)
	}
	for i := 1; i < len(figures); i++ {
		if figures[i] <= figures[i-1] {
			t.Fatalf("spend figures are not a rising whole: %v", figures)
		}
	}
	if last := figures[len(figures)-1]; last != report.USD {
		t.Fatalf("the last figure reported = %v and the report carries %v, want one account", last, report.USD)
	}
}

// THE SAME-ACTION LAW, FIRST HALF: A WORKER THAT HAS STOPPED MAKING PROGRESS
// ENDS. The alternation one action + one text reply is the shape the
// no-action ending cannot see — every action resets that run to one — so a
// worker that repeats one identical failing command forever was bounded only
// by the step cap. THE LAW READS THE RECORD, NOT THE WORDS: the same command,
// the same answer, several finished steps running, nothing the store records
// moved between them — whatever the tool and whatever the cause, that is a
// worker whose work is not moving. Here the command fails the same way every
// time (the shape the real $4.30 run is thought to have taken), and the loop
// ends in a bounded number of rounds far below the cap, the task failed with
// a reason a person can read.
func TestBashWorkerEndsAWorkerThatRepeatsTheSameFailingCommand(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	// ONE IDENTICAL FAILING ACTION, ONE SHORT REPLY, over and over: the exact
	// alternation, with nothing about the answer ever changing.
	fail := `{"command":"cat missing.txt"}`
	calls := 0
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		calls++
		if calls%2 == 1 {
			return toolReply(fail), nil
		}
		return textReply("still working on it"), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 60), *store.Task(store.RootID()))

	if err == nil {
		t.Fatal("a worker repeating one failing command forever came home clean")
	}
	if report.Steps >= 60 {
		t.Fatalf("the worker spent %d steps, want the law's bound far below the cap", report.Steps)
	}
	// THE BOUND IS THE LAW'S OWN: the third identical step brings the
	// note, and the sixth is the last, three identical looks after the belt
	// said what it observed.
	if report.Steps != 6 {
		t.Fatalf("report steps = %d, want the law's bound of six identical steps", report.Steps)
	}
	if !strings.Contains(err.Error(), "the same command") || !strings.Contains(err.Error(), "the same answer") {
		t.Fatalf("the ending = %q, want a plain reason about the same command and the same answer", err.Error())
	}
	// THE TRAJECTORY'S ENDING LINE CARRIES THE REASON, in the same plain words
	// a person reads on the task's page — no machinery, no counts of things
	// they have no name for.
	lines := rawTrajectory(t, storeDir, store.RootID())
	end := endLine(t, lines)
	if end.Steps != 6 || !strings.Contains(end.Reason, "the same command") {
		t.Fatalf("the ending reads %d steps with %q, want the bound and the plain reason", end.Steps, end.Reason)
	}
	for i, want := range []string{"same command", "same answer"} {
		if !strings.Contains(end.Reason, want) {
			t.Fatalf("the ending reason %q does not name the %s", end.Reason, want)
		}
		_ = i
	}
	// AND THE ANSWER THE REPEATED COMMAND GOT IS IN THE RECORD, so a person
	// opening the page sees what came back, not just the count of it.
	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	if len(steps) != 6 || steps[0].Command != "cat missing.txt" {
		t.Fatalf("the trajectory reads %d steps, want the six identical commands", len(steps))
	}
	if !strings.Contains(steps[0].Observation, "No such file") {
		t.Fatalf("the recorded observation = %q, want the failure the command got", steps[0].Observation)
	}
}

// The law's other mercy: A WORKER BLOCKED ON ANOTHER TASK IS SENT TO THE
// PARKING THE BELT ALREADY HAS, not ended. The store itself says what the
// worker is blocked on — an open child here — so the loop parks the task the
// way `plandb wait` would and ends waiting, to be woken when the wait is
// over, rather than failing work that was merely standing still behind
// somebody else's.
func TestBashWorkerParksAStalledWorkerThatIsBlockedOnAnotherTask(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "leaf", Title: "the blocking leaf", ParentID: "root"}}); err != nil {
		t.Fatalf("add the open child: %v", err)
	}
	fail := `{"command":"cat missing.txt"}`
	calls := 0
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		calls++
		if calls%2 == 1 {
			return toolReply(fail), nil
		}
		return textReply("the leaf is not done yet"), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 60), *store.Task(store.RootID()))

	if err != nil {
		t.Fatalf("a worker blocked on an open child was failed: %v", err)
	}
	if !report.Waiting {
		t.Fatal("the blocked worker was not parked; want it waiting on its child")
	}
	if report.Steps != 6 {
		t.Fatalf("report steps = %d, want the law's bound of six identical steps", report.Steps)
	}
	after := store.Task(store.RootID())
	if after == nil || !after.Waiting {
		t.Fatalf("the store holds no parked root after the law fired: %#v", after)
	}
	lines := rawTrajectory(t, storeDir, store.RootID())
	end := endLine(t, lines)
	if !strings.Contains(end.Reason, "waiting") || !strings.Contains(end.Reason, "the same command") {
		t.Fatalf("the ending reason = %q, want the wait named beside the stall", end.Reason)
	}
}

// THE SAME-ACTION LAW, SECOND HALF: A WORKER WHOSE LOOK ANSWERS DIFFERENTLY
// EVERY TIME IS NOT ENDED BY IT. The same command, run sixteen times — four
// times the bound — with an answer that grows by a line each run: the world
// the worker is looking at is changing under the identical question, which is
// the shape of a worker legitimately waiting on something that changes. The
// law never fires; only the step cap ends the turn, and every step is on the
// record.
func TestBashWorkerKeepsAWorkerWhoseEveryLookAnswersDifferently(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	// THE COMMAND IS IDENTICAL EVERY TIME and the answer is not: each run
	// appends a line before reading the file back.
	command := "echo tick >> count.txt && cat count.txt"
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolReply(`{"command":` + jsonString(command) + `}`), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	const cap = 16
	report, err := worker.Run(run.WithStepsPerTask(runContext(t), cap), *store.Task(store.RootID()))

	if err == nil || !strings.Contains(err.Error(), "step cap") {
		t.Fatalf("the worker ended with %v, want the step cap after sixteen changing looks", err)
	}
	if report.Steps != cap {
		t.Fatalf("report steps = %d, want the whole cap of changing looks", report.Steps)
	}
	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	if len(steps) != cap {
		t.Fatalf("the trajectory reads %d steps, want the cap's sixteen", len(steps))
	}
	for i, step := range steps {
		if step.Command != command {
			t.Fatalf("step %d command = %q, want the identical look", i+1, step.Command)
		}
		if i > 0 && step.Observation == steps[i-1].Observation {
			t.Fatalf("steps %d and %d read the same answer %q, want a changing one", i, i+1, step.Observation)
		}
	}
}

// And the store's own half of that: A WORKER WHOSE STORE MOVES BETWEEN LOOKS IS
// NOT ENDED BY IT, even when the look itself comes back byte for byte the
// same. The command here is identical every time and so is its answer — the
// note it files lands in the store each run — so only the store's own
// movement can save it, and it does: the store is the one thing a waiting or
// coordinating worker legitimately watches, and a store that moved is the
// world having changed.
// A BROKEN WORKER IN A BUSY RUN IS ENDED WHILE THE OTHERS WORK. The identical
// failing look, and between every two looks the STORE MOVES, but on a sibling's
// task: that is the rest of the run getting on with its own work, and read over
// the whole store it would reset this worker's count for ever. Read over the
// worker's own task and the rows under it, the law fires at its bound.
func TestBashWorkerEndsABrokenWorkerWhileItsSiblingsMoveTheStore(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "mine", Title: "the broken worker's task", ParentID: "root"},
		{ID: "theirs", Title: "a sibling that keeps working", ParentID: "root"},
	}); err != nil {
		t.Fatalf("add the two tasks: %v", err)
	}
	if _, err := store.Claim("mine", "mine"); err != nil {
		t.Fatalf("claim the worker's task: %v", err)
	}
	command := "plandb task note theirs tick >/dev/null 2>&1; cat missing.txt"
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolReply(`{"command":` + jsonString(command) + `}`), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 60), *store.Task("mine"))

	if err == nil || !strings.Contains(err.Error(), "the same command came back with the same answer") {
		t.Fatalf("the worker ended with %v, want the same-action ending: a sibling's moves are not its progress", err)
	}
	if report.Steps != 6 {
		var record []string
		steps, _ := run.Trajectory(filepath.Dir(store.Path()), "mine")
		for _, step := range steps {
			record = append(record, fmt.Sprintf("%d %q -> %q", step.Step, step.Command, step.Observation))
		}
		t.Fatalf("report steps = %d, want the law's bound of six; the record:\n%s", report.Steps, strings.Join(record, "\n"))
	}
	if notes := store.Notes("theirs", 0); len(notes) < 4 {
		t.Fatalf("the sibling's task holds %d notes, want one per look: the store did not move and the test proves nothing", len(notes))
	}
}

func TestBashWorkerKeepsAWorkerWhoseStoreMovedBetweenLooks(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	// THE IDENTICAL LOOK: one note filed, the same one word read back, every
	// time — the note is a new row in the store on every run.
	command := "plandb task note root tick >/dev/null 2>&1; echo noted"
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolReply(`{"command":` + jsonString(command) + `}`), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	const cap = 16
	report, err := worker.Run(run.WithStepsPerTask(runContext(t), cap), *store.Task(store.RootID()))

	if err == nil || !strings.Contains(err.Error(), "step cap") {
		t.Fatalf("the worker ended with %v, want the step cap after sixteen store-moving looks", err)
	}
	if report.Steps != cap {
		t.Fatalf("report steps = %d, want the whole cap of store-moving looks", report.Steps)
	}
	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	if len(steps) != cap {
		t.Fatalf("the trajectory reads %d steps, want the cap's sixteen", len(steps))
	}
	for i, step := range steps {
		if step.Command != command {
			t.Fatalf("step %d command = %q, want the identical look", i+1, step.Command)
		}
		if !strings.Contains(step.Observation, "noted") {
			t.Fatalf("step %d observation = %q, want the same answer every time", i+1, step.Observation)
		}
	}
}

// THE BELT SPEAKS ONCE BEFORE IT ENDS, and the first thing that proves is the
// mercy: A WORKER THAT CHANGES ITS ACTION AFTER THE NOTE IS NOT ENDED. Three
// identical looks bring the harness's own sentence into the turn that is
// still running, the worker changes what it does, and the run carries on to
// its own finish. The note itself is the thing asserted: it must be in the
// messages the seat was sent, exactly once, and it must have arrived before
// the changed action, because the seat answers with the changed command only
// to a request that carries it.
func TestBashWorkerSpeaksOnceAndKeepsAWorkerThatChangesItsAction(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	// THE IDENTICAL RUN: one missing file, one short reply, over and over,
	// until the note reaches the turn.
	fail := `{"command":"cat missing.txt"}`
	changed := `{"command":"cat other-place.txt"}`
	var told int
	var looks int
	seat := &seat{ever: func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if hasSpoken(messages) {
			// The note is in front of the worker: the first reply after it
			// changes the action, the second finishes the task.
			told++
			if told == 1 {
				return toolReply(changed), nil
			}
			return toolReply(finishCommand("root", "moved on after the note")), nil
		}
		looks++
		if looks%2 == 1 {
			return toolReply(fail), nil
		}
		return textReply("looking again"), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 24), *store.Task(store.RootID()))

	if err != nil {
		t.Fatalf("a worker that changed its action after the note was ended: %v", err)
	}
	if report.Waiting {
		t.Fatal("the worker that changed its action was parked, want its own finish")
	}
	if report.Result != "moved on after the note" {
		t.Fatalf("report result = %q, want the worker's own finish after the change", report.Result)
	}
	if report.Steps >= 24 {
		t.Fatalf("the worker spent %d steps, want its own finish far below the cap", report.Steps)
	}
	if !hasSpoken(seat.last(t)) {
		t.Fatal("the note never reached the worker: the seat's last request does not carry it")
	}
	if spoken := countSpoken(seat.last(t)); spoken != 1 {
		t.Fatalf("the note was sent %d times, want exactly once in the messages the seat was sent", spoken)
	}
	if told < 2 {
		t.Fatalf("the changed action and the finish were never asked for, told = %d", told)
	}
}

// The second thing the law proves: A WORKER THAT IGNORES THE NOTE IS ENDED AT
// THE BOUND, three more identical steps after the sentence, with the plain
// reason on the ending line, and the note was sent exactly once. The shape is
// the one the law was written for, one failing command alternating with one
// line of text, which never stacks up the no-action ending at all.
func TestBashWorkerEndsAWorkerThatIgnoresTheNote(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	fail := `{"command":"cat missing.txt"}`
	calls := 0
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		calls++
		if calls%2 == 1 {
			return toolReply(fail), nil
		}
		return textReply("still working on it"), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	report, err := worker.Run(run.WithStepsPerTask(runContext(t), 60), *store.Task(store.RootID()))

	if err == nil {
		t.Fatal("a worker that ignored the note came home clean")
	}
	if !strings.Contains(err.Error(), "the same command") || !strings.Contains(err.Error(), "the same answer") {
		t.Fatalf("the ending = %q, want a plain reason about the same command and the same answer", err.Error())
	}
	// THE BOUND IS THREE IDENTICAL STEPS AFTER THE NOTE, which is three more on
	// top of the three that brought it: six finished steps in all.
	if report.Steps != 6 {
		t.Fatalf("report steps = %d, want the bound of six identical steps, three after the note", report.Steps)
	}
	if report.Steps >= 60 {
		t.Fatalf("the worker spent %d steps, want the law's bound far below the cap", report.Steps)
	}
	lines := rawTrajectory(t, storeDir, store.RootID())
	end := endLine(t, lines)
	if end.Steps != 6 || !strings.Contains(end.Reason, "6 times in a row") {
		t.Fatalf("the ending reads %d steps with %q, want the bound and its figure", end.Steps, end.Reason)
	}
	if spoken := countSpoken(seat.last(t)); spoken != 1 {
		t.Fatalf("the note was sent %d times, want exactly once in the messages the seat was sent", spoken)
	}
	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	if len(steps) != 6 {
		t.Fatalf("the trajectory reads %d steps, want the six identical commands", len(steps))
	}
}

// THE NOTE IS THE HARNESS'S OWN VOICE AND NOT A PERSON'S, and the record is
// where that shows: the sentence the belt speaks draws no step and no row of
// its own. The trajectory of the ignored-note run above holds only the
// worker's own commands; the note lives in the messages the model was sent
// and nowhere else. This test reads the same run from the record's side: the
// steps are the worker's alone, and no step's command is the note.
func TestBashWorkerNoteDrawsNoStepOfItsOwn(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	fail := `{"command":"cat missing.txt"}`
	calls := 0
	seat := &seat{ever: func(context.Context, []ai.Message) (*ai.Response, error) {
		calls++
		if calls%2 == 1 {
			return toolReply(fail), nil
		}
		return textReply("still working on it"), nil
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)

	_, err := worker.Run(run.WithStepsPerTask(runContext(t), 60), *store.Task(store.RootID()))
	if err == nil {
		t.Fatal("the run was expected to end on the law")
	}
	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatalf("read the trajectory: %v", err)
	}
	for i, step := range steps {
		if strings.Contains(step.Command, "has come back with the same answer") {
			t.Fatalf("step %d carries the note as a command: %q", i+1, step.Command)
		}
		if step.Command != "cat missing.txt" {
			t.Fatalf("step %d command = %q, want only the worker's own look", i+1, step.Command)
		}
	}
}

// hasSpoken answers whether the request carries the belt's own note, by its
// opening words: the lead phrase is the note's alone, and the ending reason
// that says the same thing in the past tense does not reach the messages.
func hasSpoken(messages []ai.Message) bool {
	for _, message := range messages {
		if message.Role == "user" && strings.Contains(messageContent(message), "has come back with the same answer") {
			return true
		}
	}
	return false
}

// countSpoken counts the note's occurrences in one request, which is the
// count of times it was ever said: once in the transcript, it rides every
// request after it, so the last request carries the whole run's delivery.
func countSpoken(messages []ai.Message) int {
	var count int
	for _, message := range messages {
		if message.Role == "user" && strings.Contains(messageContent(message), "has come back with the same answer") {
			count++
		}
	}
	return count
}

// last is the seat's most recent request, the one that carries the whole run.
func (s *seat) last(t *testing.T) []ai.Message {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		t.Fatal("the seat was never asked anything")
	}
	return s.requests[len(s.requests)-1]
}
