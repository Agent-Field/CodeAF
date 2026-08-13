package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// The engine is a process, so every test of the executor has to be one too.
//
// The stub is this test binary, re-exec'd with a sentinel — the same trick the
// real engine arrives by (cmd/aforge/swepro.go), which is why it is the right
// one here: nothing in the executor knows it is talking to a fake, because the
// only thing it ever knew was "an executable that writes NDJSON". TestMain
// answers the sentinel before the test framework has looked at a single flag,
// which is what keeps codeaf's argv from being parsed as go test's.
const fakeEngineEnv = "AFORGE_FAKE_SWE"

func TestMain(m *testing.M) {
	if scenario := os.Getenv(fakeEngineEnv); scenario != "" {
		os.Exit(fakeEngine(scenario, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeEngine speaks the engine's half of the contract and nothing else: the
// argv and environment it was handed, written where the test can read them, and
// then the NDJSON the scenario asks for.
func fakeEngine(scenario string, argv []string) int {
	directory := ""
	for index, arg := range argv {
		if arg == "--dir" && index+1 < len(argv) {
			directory = argv[index+1]
		}
	}
	// "bell-untouched" is the one scenario about a workspace nothing wrote to,
	// so it does not get to write the harness's own record into it either.
	if directory != "" && scenario != "bell-untouched" {
		environment := map[string]string{}
		for _, name := range []string{
			"AFORGE_SWEPRO", "CODEAF_CP_URL", "OPENROUTER_API_KEY",
			"OPENROUTER_BASE_URL", "PLANDB_DB", "HOME",
		} {
			environment[name] = os.Getenv(name)
		}
		record, _ := json.Marshal(map[string]any{"argv": argv, "env": environment})
		// A run that works in its own view leaves nothing behind for the test to
		// read: the view is removed the moment its work is home. A record file
		// named from outside is where such a run says what it was handed.
		where := filepath.Join(directory, engineRecordFile)
		if named := strings.TrimSpace(os.Getenv(fakeEngineRecordEnv)); named != "" {
			where = named
		}
		_ = os.WriteFile(where, record, 0o644)
	}
	if scenario == "isolation" {
		return fakeIsolationEngine(directory)
	}
	out := json.NewEncoder(os.Stdout)
	stage := func(name, status string, data map[string]any) {
		_ = out.Encode(map[string]any{"type": "stage", "stage": name, "status": status, "data": data})
	}

	switch scenario {
	case "hang", "hang-with-child":
		// A real hang, not `select {}`: an all-asleep program is a deadlock the
		// Go runtime kills on its own, and a stub that dies by itself would let
		// the cancellation test pass without a signal ever being sent.
		if scenario == "hang-with-child" && directory != "" {
			// The engine's own auto-resume supervisor re-execs a whole second
			// process. This stands in for it: a grandchild that only a signal
			// to the process group can reach.
			child := exec.Command("sleep", "600")
			// Deliberately not inheriting stdout: a grandchild holding the
			// pipe open is a different bug from a grandchild surviving, and a
			// test should only be able to fail for the reason it names.
			if err := child.Start(); err == nil {
				_ = os.WriteFile(filepath.Join(directory, ".grandchild"),
					[]byte(fmt.Sprint(child.Process.Pid)), 0o644)
			}
		}
		// A hang that has already spent money. The engine publishes each
		// assistant message's own running cost, and a run killed before it can
		// write a terminal line has no other accounting at all.
		//
		// It goes out BEFORE the stage line, and the stage line is what the
		// cancelling test waits for. The other order made the spend assertion a
		// race the machine won under load: the stub is a re-exec of this binary,
		// so a loaded box can leave it descheduled between two writes for longer
		// than the leaf's poll interval, and the kill landed with the money
		// still unsaid. Nothing asserts a bootstrap row on this scenario, so
		// ordering the two by which one a test has to observe costs nothing.
		_ = out.Encode(map[string]any{
			"id": "evt_1", "type": "message.updated",
			"properties": map[string]any{"info": map[string]any{
				"id": "msg_1", "role": "assistant", "cost": 0.0731,
				"tokens": map[string]any{"input": 90000, "output": 4000,
					"cache": map[string]any{"read": 0}},
			}},
		})
		stage("bootstrap", "ready", nil)
		time.Sleep(10 * time.Minute)
		return 0
	case "bell", "bell-unaudited", "bell-untouched":
		// The cli#2217 shape: the work is written, committed, verified and
		// audited, and then the wall clock arrives during the wrap-up. The
		// engine never writes a terminal line, exactly as it never did.
		stage("bootstrap", "ready", map[string]any{"workspace": directory})
		stage("verification", "pass", map[string]any{"commands": "go test ./..."})
		if scenario == "bell-unaudited" {
			stage("audit", "fail", map[string]any{"cycle": 2})
		} else {
			stage("audit", "pass", map[string]any{"cycle": 2})
		}
		if directory != "" && scenario != "bell-untouched" {
			_ = os.WriteFile(filepath.Join(directory, "fixed.txt"), []byte("the parser is fixed\n"), 0o644)
			fakeCommit(directory)
		}
		time.Sleep(10 * time.Minute)
		return 0
	case "silent":
		fmt.Fprintln(os.Stderr, "[codeaf] the catalog would not load")
		fmt.Fprintln(os.Stderr, "[codeaf] giving up")
		return 1
	}

	stage("bootstrap", "ready", map[string]any{"workspace": directory})
	stage("classifier", "focused", map[string]any{"reason": "one coding issue"})
	if scenario == "trivial" {
		// The engine deciding the whole goal is one leaf, in its own two
		// events: the root cut it made and the band it made it from.
		stage("root-cut", "selected", map[string]any{"band": "xs"})
		stage("classifier", "trivial", map[string]any{"reason": "one obvious edit"})
	}
	stage("plan-apply", "completed", map[string]any{"tasks": 3, "edges": 2})
	stage("scheduler", "cycle", map[string]any{"cycle": 1, "dispatched": 2})
	_ = out.Encode(map[string]any{
		"id": "evt_1", "type": "message.part.delta",
		"properties": map[string]any{"delta": "thinking out loud"},
	})
	_ = out.Encode(map[string]any{
		"id": "evt_2", "type": "message.updated",
		"properties": map[string]any{"info": map[string]any{
			"id": "msg_1", "role": "assistant", "cost": 0.0031,
			"tokens": map[string]any{"input": 1200, "output": 340,
				"cache": map[string]any{"read": 800}},
		}},
	})
	// Republished as it streams: the same message, the same tokens, twice.
	_ = out.Encode(map[string]any{
		"id": "evt_3", "type": "message.updated",
		"properties": map[string]any{"info": map[string]any{
			"id": "msg_1", "role": "assistant", "cost": 0.0031,
			"tokens": map[string]any{"input": 1200, "output": 340,
				"cache": map[string]any{"read": 800}},
		}},
	})
	verification := map[string]any{"commands": "go test ./..."}
	if scenario == "baseline" {
		// The engine's baseline delta, in the shape the contract carries it:
		// a check that came back red and was already red before the run.
		verification["pre_existing"] = []string{
			"`make all` exited 2, and every failing test it reports " +
				"(TestFailGenFishCompletionFile) was ALREADY failing at this commit " +
				"before the run touched the workspace.",
		}
	}
	stage("verification", "pass", verification)
	if scenario == "baseline" {
		// Verification runs once per audit cycle and repeats itself.
		stage("verification", "pass", verification)
	}
	if scenario == "strained" {
		stage("audit", "pass", map[string]any{"cycle": 5, "max_cycles": 5})
	} else {
		stage("audit", "pass", map[string]any{"cycle": 2})
	}

	if directory != "" && scenario == "pass" {
		_ = os.WriteFile(filepath.Join(directory, "fixed.txt"), []byte("the parser is fixed\n"), 0o644)
		fakeCommit(directory)
	}

	switch scenario {
	case "budget":
		_ = out.Encode(map[string]any{
			"type": "terminal", "status": "budget-exhausted",
			"message": "the cost ceiling was reached inside the audit loop",
			"data":    map[string]any{"cycle": 4, "cost_usd": 9.9812},
		})
	case "refused":
		_ = out.Encode(map[string]any{
			"type": "terminal", "status": "refused",
			"message": "this is not a change to a repository",
			"data":    map[string]any{"cycle": 0, "cost_usd": 0.0021},
		})
	case "fail":
		_ = out.Encode(map[string]any{
			"type": "terminal", "status": "fail",
			"message": "the audit gate never cleared",
			"data":    map[string]any{"cycle": 3, "cost_usd": 2.5},
		})
	case "trivial":
		_ = out.Encode(map[string]any{
			"type": "terminal", "status": "pass",
			"message": "The flag name was corrected.",
			"data":    map[string]any{"cycle": 1, "cost_usd": 0.0104},
		})
	case "strained":
		// Most of the ceiling, so the cheap-run note stays silent and only the
		// audit's own exhaustion speaks.
		_ = out.Encode(map[string]any{
			"type": "terminal", "status": "pass",
			"message": "It took five audit cycles, but the suite is green.",
			"data":    map[string]any{"cycle": 5, "cost_usd": 8.0},
		})
	default:
		_ = out.Encode(map[string]any{
			"type": "terminal", "status": "pass",
			"message": "The parser now accepts trailing commas, and the new table test covers them.",
			"data": map[string]any{"cycle": 2, "cost_usd": 0.4212,
				"project_id": "p1", "root_task_id": "t1"},
		})
	}
	return 0
}

// engineRecordFile is where the stub leaves its argv and environment. It is a
// dotfile so the artifact assertions are about the work, not the harness.
const engineRecordFile = ".engine-call.json"

func fakeCommit(directory string) {
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.name=fake", "-c", "user.email=fake@example.com",
			"-c", "commit.gpgsign=false", "commit", "--no-verify", "-m", "engine: the change"},
	} {
		command := exec.Command("git", args...)
		command.Dir = directory
		_ = command.Run()
	}
}

// ── the harness the tests share ──────────────────────────────────────────────

type sweProbe struct {
	worker    *SWE
	workspace *Workspace
	directory string

	mutex     sync.Mutex
	phases    []string
	milestone []string
}

func newSWEProbe(t *testing.T, scenario string) *sweProbe {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("the swe worker needs git")
	}
	// aforge's own state root, disposable. A leaf keeps things outside the
	// workspace now — the view root, and the plan database beside it — and a
	// test that let those land in the developer's real ~/.aforge would both
	// litter it and read whatever an earlier run left there.
	t.Setenv(home.EnvVar, t.TempDir())
	directory := t.TempDir()
	workspace, err := NewWorkspace(directory)
	if err != nil {
		t.Fatal(err)
	}
	worker := NewSWE(workspace, "vendor/model", "sk-test", "https://gateway.example/api/v1", time.Minute)
	worker.binary = os.Args[0]
	worker.extraEnv = []string{fakeEngineEnv + "=" + scenario}
	worker.pollEvery = 10 * time.Millisecond
	return &sweProbe{worker: worker, workspace: workspace, directory: workspace.Root()}
}

func (p *sweProbe) task() Task {
	return Task{
		NodeID: 7, Title: "fix the parser",
		Goal:  "make the importer work",
		Brief: "Fix the parser so trailing commas parse, with a test.",
		Progress: func(phase string, _, _ int, latest string) {
			p.mutex.Lock()
			defer p.mutex.Unlock()
			p.phases = append(p.phases, phase+"|"+latest)
		},
		Share: func(line string) error {
			p.mutex.Lock()
			defer p.mutex.Unlock()
			p.milestone = append(p.milestone, line)
			return nil
		},
	}
}

// controlWhenItSpeaks arms a control action that holds until the run has read
// something off the engine's stream, and reports through the task's own
// progress channel that it has.
//
// A test that cancels unconditionally is racing the child's own start. The stub
// is a re-exec of this binary; on a loaded machine the process can take longer
// to reach its first write than the leaf's poll interval, and the leaf then
// kills something that has done nothing — which passes a cancellation test for
// the wrong reason and fails every test that asserts what the run had already
// managed to say. Waiting for the first stage row is the one signal a test has
// that the stream is really flowing.
func controlWhenItSpeaks(task *Task, action ControlAction) {
	spoken := make(chan struct{})
	var once sync.Once
	reported := task.Progress
	task.Progress = func(phase string, done, total int, latest string) {
		if reported != nil {
			reported(phase, done, total, latest)
		}
		once.Do(func() { close(spoken) })
	}
	task.Control = func() ControlAction {
		select {
		case <-spoken:
			return action
		default:
			return ControlNone
		}
	}
}

func (p *sweProbe) said() ([]string, []string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return append([]string(nil), p.phases...), append([]string(nil), p.milestone...)
}

func (p *sweProbe) engineCall(t *testing.T) (argv []string, environment map[string]string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(p.directory, engineRecordFile))
	if err != nil {
		t.Fatalf("the engine was never called: %v", err)
	}
	var record struct {
		Argv []string          `json:"argv"`
		Env  map[string]string `json:"env"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return record.Argv, record.Env
}

func (p *sweProbe) trace(t *testing.T) string {
	t.Helper()
	full, _, err := p.workspace.ScratchPath(traceName("7"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("no trace was written: %v", err)
	}
	return string(raw)
}

// stream is the raw machine feed's sidecar — everything the engine said, in
// order, in the file no surface renders.
func (p *sweProbe) stream(t *testing.T) string {
	t.Helper()
	full, _, err := p.workspace.ScratchPath(streamName("7"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("no stream sidecar was written: %v", err)
	}
	return string(raw)
}

func contains(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// ── the tests ────────────────────────────────────────────────────────────────

// The whole contract in one run: the argv and environment the engine is handed,
// the verdict only a worker with a verifier may set, the cost, the files, the
// progress, the milestones, and the trace.
func TestSWERunPassesAndLandsAnOrdinaryOutcome(t *testing.T) {
	probe := newSWEProbe(t, "pass")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("a passing run returned an error: %v", err)
	}
	if outcome.Stop != StopDone {
		t.Fatalf("stop = %q, want done", outcome.Stop)
	}
	if outcome.Verdict != provider.VerdictVerifiedSuccess {
		t.Fatalf("verdict = %q — the swe worker owns a verifier and must say so", outcome.Verdict)
	}
	if outcome.Usage.Cost != 0.4212 {
		t.Fatalf("cost = %v, want the terminal event's figure", outcome.Usage.Cost)
	}
	// One message republished twice is one message's tokens.
	if outcome.Usage.PromptTokens != 1200 || outcome.Usage.CompletionTokens != 340 ||
		outcome.Usage.CachedTokens != 800 {
		t.Fatalf("tokens double-counted or lost: %+v", outcome.Usage)
	}
	if !strings.Contains(outcome.Text, "trailing commas") {
		t.Fatalf("the engine's own summary is not the deliverable: %q", outcome.Text)
	}
	if !strings.Contains(outcome.Text, "swe: pass after 2 cycles, $0.4212") {
		t.Fatalf("the stat line is missing: %q", outcome.Text)
	}
	if !contains(outcome.Artifacts, "fixed.txt") {
		t.Fatalf("a committed change was not reported as an artifact: %v", outcome.Artifacts)
	}
	for _, sidecar := range []string{".codeaf", ".plandb.db", ".obs"} {
		if contains(outcome.Artifacts, sidecar) {
			t.Fatalf("the engine's own bookkeeping was named as a deliverable: %v", outcome.Artifacts)
		}
	}
	if !contains(outcome.Ran, "audit pass") {
		t.Fatalf("the run record does not name what the engine did: %v", outcome.Ran)
	}

	phases, milestones := probe.said()
	for _, want := range []string{"planning the change|3 tasks, 2 edges", "auditing the result|pass"} {
		if !contains(phases, want) {
			t.Fatalf("progress never said %q: %v", want, phases)
		}
	}
	if contains(phases, "message.part.delta") {
		t.Fatalf("a streaming delta reached the progress channel: %v", phases)
	}
	for _, want := range []string{
		"coding plan ready — 3 tasks, 2 edges",
		"the repository's own checks passed",
		"audit verdict pass at cycle 2",
	} {
		if !contains(milestones, want) {
			t.Fatalf("milestone %q never reached the job: %v", want, milestones)
		}
	}
	if len(milestones) > 6 {
		t.Fatalf("the milestone channel is being used for chatter: %v", milestones)
	}

	// THE RECORDER IS PROSE AND THE SIDECAR IS THE STREAM. This assertion used
	// to read the other way round — "the trace is not the full stream" — and
	// that is the defect: the recorder is what the task room renders, so a raw
	// NDJSON line in it is a screen of raw NDJSON on somebody's display. Both
	// halves are still kept; they are kept in two files.
	trace := probe.trace(t)
	for _, want := range []string{"workspace:", streamNote, "stage: plan-apply completed"} {
		if !strings.Contains(trace, want) {
			t.Fatalf("the recorder lost a sentence a person reads — missing %q", want)
		}
	}
	for _, never := range []string{`{"id":"evt_`, "message.part.delta", `"properties"`} {
		if strings.Contains(trace, never) {
			t.Fatalf("a raw machine line is in the recorder as prose: %q", never)
		}
	}
	stream := probe.stream(t)
	for _, want := range []string{`"type":"terminal"`, "message.part.delta"} {
		if !strings.Contains(stream, want) {
			t.Fatalf("the sidecar is not the full stream — missing %q", want)
		}
	}
}

// The exact command line and environment. These are the seam: everything the
// engine does is decided here, and a silent change to any of it is a run
// against the wrong models, the wrong key, or a control plane that is not
// there.
func TestSWEHandsTheEngineItsArgvAndEnvironment(t *testing.T) {
	probe := newSWEProbe(t, "pass")
	if _, err := probe.worker.Run(context.Background(), probe.task()); err != nil {
		t.Fatal(err)
	}
	argv, environment := probe.engineCall(t)
	if argv[0] != "run" {
		t.Fatalf("argv[0] = %q, want run", argv[0])
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"--dir " + probe.directory,
		"--format json",
		"--high openrouter/vendor/model",
		"--low openrouter/vendor/model",
		"--max-cost 10",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv is missing %q: %v", want, argv)
		}
	}
	if argv[len(argv)-2] != "--" {
		t.Fatalf("the goal is not behind a -- separator: %v", argv)
	}
	if goal := argv[len(argv)-1]; !strings.Contains(goal, "trailing commas") ||
		!strings.Contains(goal, "make the importer work") {
		t.Fatalf("the goal did not arrive whole: %q", goal)
	}
	want := map[string]string{
		"AFORGE_SWEPRO":       "1",
		"CODEAF_CP_URL":       "off",
		"OPENROUTER_API_KEY":  "sk-test",
		"OPENROUTER_BASE_URL": "https://gateway.example/api/v1",
	}
	// THE PLAN DATABASE IS NOT IN THE DELIVERABLE. Left to itself the engine
	// puts it at <run dir>/.plandb.db and holds it out of the change with an
	// exclude file — advisory, and dead the moment anything commits. This is the
	// one piece of the engine's state a variable can move, so it is moved: a
	// file outside the tree cannot be staged, squashed, or landed in somebody's
	// history by any mistake made anywhere else.
	plandb := environment["PLANDB_DB"]
	if plandb != filepath.Join(sweStateDir(probe.directory, probe.task().leafKey()), "plandb.db") {
		t.Fatalf("PLANDB_DB = %q, want the leaf's own state directory beside its view", plandb)
	}
	if inside, err := filepath.Rel(probe.directory, plandb); err == nil && !strings.HasPrefix(inside, "..") {
		t.Fatalf("the plan database is inside the workspace the leaf delivers: %q", plandb)
	}
	for name, value := range want {
		if environment[name] != value {
			t.Fatalf("env %s = %q, want %q", name, environment[name], value)
		}
	}
	if environment["HOME"] != os.Getenv("HOME") {
		t.Fatalf("HOME was not preserved: %q", environment["HOME"])
	}
}

// A budget stop is not a failure — it is a leaf that was still working when the
// money ran out, and Exhausted is the field the continuation replan reads.
func TestSWEBudgetExhaustedFeedsTheContinuationReplan(t *testing.T) {
	probe := newSWEProbe(t, "budget")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err != nil {
		t.Fatalf("a budget stop was reported as an error: %v", err)
	}
	if outcome.Stop != StopBudget || outcome.Exhausted != StopBudget {
		t.Fatalf("stop=%q exhausted=%q, want budget on both", outcome.Stop, outcome.Exhausted)
	}
	if !outcome.Overran() {
		t.Fatal("the outcome does not read as an overrun")
	}
	if outcome.Verdict != provider.VerdictBudgetStop {
		t.Fatalf("verdict = %q, want the shared law's budget stop", outcome.Verdict)
	}
	if outcome.Usage.Cost != 9.9812 {
		t.Fatalf("cost = %v", outcome.Usage.Cost)
	}
}

// A refusal is a fact about the routing, not about the model: it must read as a
// refusal, must not grade, and must not buy an escalation. A crash-shaped
// failure must do the opposite.
func TestSWERefusalAndFailureCarryTheirOwnMeanings(t *testing.T) {
	refused := newSWEProbe(t, "refused")
	outcome, err := refused.worker.Run(context.Background(), refused.task())
	if err == nil {
		t.Fatal("a refusal did not surface as a failed leaf")
	}
	if !strings.Contains(err.Error(), "declined this goal") {
		t.Fatalf("the refusal reads as a crash: %v", err)
	}
	if outcome.Verdict.Escalates() {
		t.Fatalf("a refusal bought an escalation: %q", outcome.Verdict)
	}

	failed := newSWEProbe(t, "fail")
	outcome, err = failed.worker.Run(context.Background(), failed.task())
	if err == nil {
		t.Fatal("a failing run did not surface as a failed leaf")
	}
	if !strings.Contains(err.Error(), "the audit gate never cleared") {
		t.Fatalf("the engine's reason was lost: %v", err)
	}
	if !outcome.Verdict.Escalates() {
		t.Fatalf("a failure did not reach the escalation machinery: %q", outcome.Verdict)
	}
}

// A process that dies with nothing to say still owes an account of itself, and
// the only account there is is whatever it complained about on the way out.
func TestSWEDeathWithoutATerminalEventReportsWhatItSaid(t *testing.T) {
	probe := newSWEProbe(t, "silent")
	outcome, err := probe.worker.Run(context.Background(), probe.task())
	if err == nil {
		t.Fatal("a silent death was reported as a success")
	}
	if !strings.Contains(err.Error(), "the catalog would not load") &&
		!strings.Contains(err.Error(), "giving up") {
		t.Fatalf("the stderr tail was not carried: %v", err)
	}
	if outcome.Stop != StopError {
		t.Fatalf("stop = %q", outcome.Stop)
	}
}

// Cancel and pause take the whole tree down the same way and differ only in
// what the store is told — which is the difference between "this is over" and
// "come back to this".
func TestSWEControlStopsTheEngine(t *testing.T) {
	for _, testCase := range []struct {
		action ControlAction
		want   StopReason
		says   string
	}{
		{ControlCancel, StopCancelled, "cancelled"},
		{ControlPause, StopPaused, "paused"},
	} {
		probe := newSWEProbe(t, "hang")
		task := probe.task()
		task.Control = func() ControlAction { return testCase.action }
		started := time.Now()
		outcome, err := probe.worker.Run(context.Background(), task)
		if err != nil {
			t.Fatalf("%s returned an error: %v", testCase.action, err)
		}
		if outcome.Stop != testCase.want {
			t.Fatalf("%s: stop = %q, want %q", testCase.action, outcome.Stop, testCase.want)
		}
		if elapsed := time.Since(started); elapsed > 20*time.Second {
			t.Fatalf("%s took %s — the process group was not signalled", testCase.action, elapsed)
		}
		if !strings.Contains(strings.ToLower(outcome.Text), testCase.says) {
			t.Fatalf("%s: the text does not say what happened: %q", testCase.action, outcome.Text)
		}
		if outcome.Verdict.Escalates() {
			t.Fatalf("%s graded the model: %q", testCase.action, outcome.Verdict)
		}
	}
}

// The reason the child gets a process group of its own: the engine re-execs
// this binary again for its auto-resume supervisor, and a signal to the leader
// alone would leave that grandchild running against a leaf nobody is waiting
// for any more.
func TestSWECancelReachesTheWholeProcessTree(t *testing.T) {
	probe := newSWEProbe(t, "hang-with-child")
	task := probe.task()
	// The grandchild is started before the stub says anything, so waiting for
	// the first row is also waiting for the pid file to exist.
	controlWhenItSpeaks(&task, ControlCancel)
	if _, err := probe.worker.Run(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(probe.directory, ".grandchild"))
	if err != nil {
		t.Skipf("the stub could not start a grandchild: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		t.Fatalf("unreadable grandchild pid %q", raw)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("grandchild %d outlived the cancelled leaf", pid)
}

// The engine cannot take guidance mid-run yet. What it must never do is take
// the guidance out of the mailbox and drop it: a drained mailbox and a
// delivered one look identical from the store's side.
func TestSWECarriesUnhonouredSteeringIntoTheOutcome(t *testing.T) {
	probe := newSWEProbe(t, "pass")
	task := probe.task()
	sent := false
	task.Steer = func() []string {
		if sent {
			return nil
		}
		sent = true
		return []string{"use the existing lexer, do not write a second one"}
	}
	outcome, err := probe.worker.Run(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outcome.Text, "steering received late") ||
		!strings.Contains(outcome.Text, "do not write a second one") {
		t.Fatalf("the user's words were swallowed: %q", outcome.Text)
	}
	if outcome.Steered != 0 {
		t.Fatalf("Steered counts lines a run actually read, and this one read none: %d", outcome.Steered)
	}
	if !strings.Contains(probe.trace(t), "steered (received, not injectable mid-run)") {
		t.Fatal("the steering never reached the trace either")
	}
}

// The engine requires a committed repository. A workspace that is not one is
// made one, and the trace says which of the two happened, because a leaf that
// ran against a repository it created itself has no history to reason from.
func TestSWEInitialisesAWorkspaceThatIsNotARepository(t *testing.T) {
	probe := newSWEProbe(t, "budget")
	if err := os.WriteFile(filepath.Join(probe.directory, "notes.md"), []byte("prior work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := probe.worker.Run(context.Background(), probe.task()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(probe.directory, ".git")); err != nil {
		t.Fatalf("no repository was made: %v", err)
	}
	if !strings.Contains(probe.trace(t), "initialised one and committed a baseline") {
		t.Fatal("the trace does not record that the repository was ours")
	}
	// The baseline commit carries what was already there, so the run's own
	// changes are the only thing the diff can report.
	command := exec.Command("git", "log", "--format=%s")
	command.Dir = probe.directory
	out, err := command.Output()
	if err != nil || !strings.Contains(string(out), "aforge: baseline before the swe worker ran") {
		t.Fatalf("the baseline commit is missing: %q (%v)", out, err)
	}
}

// The other half of the same rule: a workspace that is already a repository is
// run in place, and no commit of ours appears in somebody's history.
func TestSWERunsInPlaceInAnExistingRepository(t *testing.T) {
	probe := newSWEProbe(t, "budget")
	if err := os.WriteFile(filepath.Join(probe.directory, "notes.md"), []byte("prior work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"add", "-A"},
		{"-c", "user.name=someone", "-c", "user.email=someone@example.com",
			"-c", "commit.gpgsign=false", "commit", "--no-verify", "-m", "theirs"}} {
		command := exec.Command("git", args...)
		command.Dir = probe.directory
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if _, err := probe.worker.Run(context.Background(), probe.task()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(probe.trace(t), "an existing git repository, run in place") {
		t.Fatal("the trace does not say the repository was theirs")
	}
	command := exec.Command("git", "log", "--format=%s")
	command.Dir = probe.directory
	out, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "aforge: baseline") {
		t.Fatalf("we committed into somebody else's history: %q", out)
	}
}

// A restarted leaf's earned advantage: the engine left a checkpoint, so the
// node resumes the run instead of starting it again. And the other half of the
// same rule — a checkpoint the engine would refuse to resume is not one worth
// asking it about, because asking costs a process that exits saying nothing.
func TestSWEResumesFromACheckpointAndOnlyAResumableOne(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		status  string
		goal    string
		command string
	}{
		{"a failed run is resumable", "fail", "fix the parser", "resume"},
		{"a budget stop is resumable", "budget-exhausted", "fix the parser", "resume"},
		{"a passed run is not", "pass", "fix the parser", "run"},
		{"a checkpoint with no goal is not", "fail", "", "run"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			probe := newSWEProbe(t, "budget")
			sidecar := filepath.Join(probe.directory, ".codeaf")
			if err := os.MkdirAll(sidecar, 0o755); err != nil {
				t.Fatal(err)
			}
			row, _ := json.Marshal(map[string]any{
				"goal": testCase.goal, "finalStatus": testCase.status, "cycle": 2,
			})
			if err := os.WriteFile(filepath.Join(sidecar, "resume-checkpoint.json"), row, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := probe.worker.Run(context.Background(), probe.task()); err != nil {
				t.Fatal(err)
			}
			argv, _ := probe.engineCall(t)
			if argv[0] != testCase.command {
				t.Fatalf("argv[0] = %q, want %q", argv[0], testCase.command)
			}
		})
	}
}

// A workspace with no checkpoint at all starts from the beginning.
func TestSWEWithoutACheckpointRunsRatherThanResumes(t *testing.T) {
	probe := newSWEProbe(t, "budget")
	if _, err := probe.worker.Run(context.Background(), probe.task()); err != nil {
		t.Fatal(err)
	}
	argv, _ := probe.engineCall(t)
	if argv[0] != "run" {
		t.Fatalf("argv[0] = %q, want run", argv[0])
	}
}

// aforge names a model one way and the engine another. This is the entire
// translation, so it is the entire thing that can be wrong about it.
func TestEnginePoolTranslatesTheModelName(t *testing.T) {
	for _, testCase := range []struct{ given, want string }{
		{"deepseek/deepseek-v4-pro", "openrouter/deepseek/deepseek-v4-pro"},
		{"~moonshotai/kimi-k2.6", "openrouter/moonshotai/kimi-k2.6"},
		{"openrouter/z-ai/glm-5.1", "openrouter/z-ai/glm-5.1"},
		{"  ", ""},
	} {
		if got := enginePool(testCase.given); got != testCase.want {
			t.Fatalf("enginePool(%q) = %q, want %q", testCase.given, got, testCase.want)
		}
	}
}

// The engine's ceiling has to bite before ours does, or a run is killed
// mid-merge instead of checkpointing itself.
func TestSWEKeepsALandingReserveUnderItsOwnDeadline(t *testing.T) {
	for _, deadline := range []time.Duration{30 * time.Minute, time.Hour, 3 * time.Hour} {
		worker := &SWE{deadline: deadline}
		granted := time.Duration(worker.maxHours() * float64(time.Hour))
		if granted >= deadline {
			t.Fatalf("deadline %s granted the engine %s — no reserve at all", deadline, granted)
		}
		if deadline-granted > 10*time.Minute {
			t.Fatalf("deadline %s kept back %s — the reserve is eating the work", deadline, deadline-granted)
		}
	}
}

// The sentinel name is the whole re-exec seam, and it is spelled in two
// packages that cannot import each other.
func TestSWESentinelNameMatchesTheBinarysHalf(t *testing.T) {
	if sweproSentinelEnv != "AFORGE_SWEPRO" {
		t.Fatalf("the sentinel was renamed to %q; cmd/aforge/swepro.go still says AFORGE_SWEPRO", sweproSentinelEnv)
	}
}
