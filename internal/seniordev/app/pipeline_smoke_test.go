//go:build !windows

// This file exercises the solo run end to end against a scripted backend.
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

func eventStages(t *testing.T, raw []byte) []string {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	out := []string{}
	for _, line := range lines {
		var value event
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatalf("invalid NDJSON event %q: %v", line, err)
		}
		if value.Stage != "" {
			out = append(out, value.Stage)
		}
	}
	return out
}

func assertOrderedStages(t *testing.T, got, want []string) {
	t.Helper()
	at := 0
	for _, stage := range got {
		if at < len(want) && stage == want[at] {
			at++
		}
	}
	if at != len(want) {
		t.Fatalf("stage order = %v, missing ordered suffix %v", got, want[at:])
	}
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func gitRun(directory string, args ...string) error {
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=senior-dev-smoke",
		"GIT_AUTHOR_EMAIL=senior-dev@example.test",
		"GIT_COMMITTER_NAME=senior-dev-smoke",
		"GIT_COMMITTER_EMAIL=senior-dev@example.test",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return nil
}

// soloScriptedBackend drives one full solo run offline: the coder explores,
// writes a file, pins a command, and submits.
type soloScriptedBackend struct {
	calls int
	// onTurn runs before the scripted tool calls, so a test can make the model
	// misbehave -- stop without submitting, submit twice, edit after freezing.
	onTurn func(call int, request turn) (turnResult, bool, error)
}

func (backend *soloScriptedBackend) Run(
	ctx context.Context, request turn,
) (turnResult, error) {
	backend.calls++
	if backend.onTurn != nil {
		if result, handled, err := backend.onTurn(backend.calls, request); handled {
			return result, err
		}
	}
	if request.Execute == nil {
		return turnResult{Text: "no tools available"}, nil
	}
	call := func(name, input string) (steploop.ToolResult, error) {
		return request.Execute(ctx, steploop.ToolCall{
			ID: fmt.Sprintf("call_%d", backend.calls), Name: name,
			Input: json.RawMessage(input), SessionID: request.SessionID, Agent: request.Agent,
		})
	}
	if _, err := call("write", `{"filePath":"feature.txt","content":"implemented\n"}`); err != nil {
		return turnResult{}, err
	}
	// The real protocol writes a checklist in stage 0 and submit refuses without
	// one, so a backend that models the run has to write one too.
	if _, err := call("write", `{"filePath":".senior-dev/checklist.md","content":"- [x] feature implemented\n"}`); err != nil {
		return turnResult{}, err
	}
	if _, err := call("write", `{"filePath":".senior-dev/pinned.txt","content":"make test\n"}`); err != nil {
		return turnResult{}, err
	}
	result, err := call("submit", `{"reason":"feature implemented",`+
		`"evidence":"make test exit 0","checklist_satisfied":true}`)
	if err != nil {
		return turnResult{}, err
	}
	return turnResult{Text: "done: " + result.Output}, nil
}

func TestSoloRunGoesIntakeToFrozenShipInOneContext(t *testing.T) {
	// The end-to-end shape, offline. What it proves is the sequence and the
	// session count: one coding context, one submission, one terminal.
	//
	// The Makefile is part of the COMMITTED base: it is the project's existing
	// build system, not something this run produced. Leaving it uncommitted
	// would make it part of the candidate and the file count would not measure
	// what the run actually contributed.
	workspace := gitWorkspace(t, map[string]string{
		"README.md": "base\n",
		"Makefile":  "build:\n\t@true\n\ntest:\n\t@true\n",
	})
	base := strings.TrimSpace(gitOutput(context.Background(), workspace, "rev-parse", "HEAD"))
	var events bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Backend: &soloScriptedBackend{}, Events: newEventWriter(&events), Notes: io.Discard,
	})
	defer runner.runtime.Close()

	outcome, err := runner.runSolo(context.Background(), "Add the feature.", base)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "pass" {
		t.Fatalf("status = %q (%#v)", outcome.Status, outcome)
	}
	if outcome.Nudges != 0 {
		t.Fatalf("a run that submitted on its first turn was nudged %d time(s)", outcome.Nudges)
	}
	if outcome.Frozen == nil || outcome.Frozen.Reason != "feature implemented" {
		t.Fatalf("frozen candidate = %#v", outcome.Frozen)
	}

	// No "terminal" stage: the terminal is a type=="terminal" event emitted by
	// the CLI layer, which runSolo is below. What runSolo must produce is the
	// payload for it.
	assertOrderedStages(t, eventStages(t, events.Bytes()),
		[]string{"intake", "implement", "submit", "implement", "verification", "ship"})
	if outcome.TerminalData["submitted"] != true {
		t.Fatalf("terminal payload = %#v", outcome.TerminalData)
	}

	// The submitted file is what is on disk, and senior-dev's own bookkeeping did
	// not become the deliverable.
	content, err := os.ReadFile(filepath.Join(workspace, "feature.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "implemented\n" {
		t.Fatalf("shipped file = %q", content)
	}
	if outcome.Frozen.PatchFiles != 1 {
		t.Fatalf("PatchFiles = %d, want 1", outcome.Frozen.PatchFiles)
	}
}

func TestSoloRunNudgesThenGivesUpHonestly(t *testing.T) {
	// A model that never submits must not produce a run that reports an
	// attempt. It gets soloMaxNudges chances carrying the facts senior-dev checked,
	// and then the terminal says plainly that nothing was submitted.
	workspace, base := guardWorkspace(t)
	backend := &soloScriptedBackend{
		onTurn: func(int, turn) (turnResult, bool, error) {
			return turnResult{Text: "I believe this is complete."}, true, nil
		},
	}
	var events bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(&events), Notes: io.Discard,
	})
	defer runner.runtime.Close()

	outcome, err := runner.runSolo(context.Background(), "Add the feature.", base)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "unsubmitted" {
		t.Fatalf("status = %q, want unsubmitted", outcome.Status)
	}
	if backend.calls != soloMaxNudges+1 {
		t.Fatalf("model turns = %d, want %d (one attempt plus %d nudges)",
			backend.calls, soloMaxNudges+1, soloMaxNudges)
	}
	if status, reason := soloResultStatus(outcome); status != "fail" ||
		!strings.Contains(reason, "without submitting") {
		t.Fatalf("result status = %q / %q", status, reason)
	}
}

func TestSoloRunCorrectsPlainTextDSMLWithoutSpendingANudge(t *testing.T) {
	workspace := gitWorkspace(t, map[string]string{
		"README.md": "base\n",
		"Makefile":  "build:\n\t@true\n\ntest:\n\t@true\n",
	})
	base := strings.TrimSpace(gitOutput(context.Background(), workspace, "rev-parse", "HEAD"))
	backend := &soloScriptedBackend{
		onTurn: func(call int, _ turn) (turnResult, bool, error) {
			if call == 1 {
				return turnResult{Text: `<｜DSML｜bash>{"cmd":"make test"}`}, true, nil
			}
			return turnResult{}, false, nil
		},
	}
	var events bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(&events), Notes: io.Discard,
	})
	defer runner.runtime.Close()

	outcome, err := runner.runSolo(context.Background(), "Add the feature.", base)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "pass" || outcome.Nudges != 0 || backend.calls != 2 {
		t.Fatalf("outcome=%#v calls=%d", outcome, backend.calls)
	}
	leaks := soloStageEvents(t, &events, "implement")
	found := false
	for _, event := range leaks {
		found = found || event["status"] == "tool-call-leak"
	}
	if !found {
		t.Fatal("plain-text tool call was not recorded")
	}
}

func TestToolLeakDetectorDoesNotOverrideAnExecutedToolCall(t *testing.T) {
	result := turnResult{
		Text:  "DSML bash markup appeared in an explanation",
		Parts: []turnPart{{Type: "tool", Tool: "bash", Status: "completed"}},
	}
	if leakedToolCall(result) {
		t.Fatal("an executed tool call was misclassified as leaked markup")
	}
}

// TestBudgetExhaustedRunStillShipsAndReportsWhy pins ship running on every
// ending. If soloConverse returning an error sent runSolo home before
// soloShip, the common ending of a full-budget run would produce neither a
// restore nor any statement of whether the run had submitted: both halves of
// stage 4 skipped on the ending that happens most.
func TestBudgetExhaustedRunStillShipsAndReportsWhy(t *testing.T) {
	workspace, base := guardWorkspace(t)
	backend := &soloScriptedBackend{
		onTurn: func(int, turn) (turnResult, bool, error) {
			return turnResult{}, true, context.DeadlineExceeded
		},
	}
	var events bytes.Buffer
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Backend: backend, Events: newEventWriter(&events), Notes: io.Discard,
	})
	defer runner.runtime.Close()

	outcome, err := runner.runSolo(context.Background(), "Add the feature.", base)
	if err == nil {
		t.Fatal("a turn that died on its deadline returned no error")
	}
	// The error propagates -- the run did die -- but the account survives it.
	if outcome.Status != "unsubmitted" {
		t.Fatalf("status = %q, want unsubmitted", outcome.Status)
	}
	if outcome.TerminalData == nil {
		t.Fatal("a run killed mid-turn produced no terminal payload")
	}
	if outcome.TerminalData["submitted"] != false {
		t.Fatalf("terminal payload = %#v", outcome.TerminalData)
	}
	reason, _ := outcome.TerminalData["reason"].(string)
	if !strings.Contains(reason, "without calling submit") {
		t.Fatalf("terminal reason = %q, want it to name the missing submission", reason)
	}
	// The cause is carried too, so a reader can tell "never tried" from "ran
	// out of time trying".
	if !strings.Contains(reason, context.DeadlineExceeded.Error()) {
		t.Fatalf("terminal reason = %q, want it to carry the underlying cause", reason)
	}
}

// TestTerminalEventCarriesTheRunsAccount pins the contract at the boundary an
// external reader sees: the type=="terminal" event -- not a stage named
// "terminal" -- has to answer whether the run submitted. A test that asserts
// on the stage event alone passes while the terminal carries only a cost.
func TestTerminalEventCarriesTheRunsAccount(t *testing.T) {
	result := pipelineResult{
		Status: "fail", Reason: "the run ended without submitting",
		CostUSD:  0.25,
		Terminal: map[string]any{"submitted": false, "nudges": 2, "reason": "no submission"},
	}
	var out bytes.Buffer
	invocation := &cliInvocation{
		events: newEventWriter(&out),
		runner: newPipeline(cliArgs{}, t.TempDir(), pipelineDeps{
			Events: newEventWriter(io.Discard), Notes: io.Discard,
		}),
	}
	defer invocation.runner.runtime.Close()
	invocation.persistTerminalResult(result)

	var terminals []map[string]any
	for _, line := range bytes.Split(out.Bytes(), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal(line, &value); err != nil {
			continue
		}
		if value["type"] == "terminal" {
			terminals = append(terminals, value)
		}
	}
	if len(terminals) != 1 {
		t.Fatalf("type==terminal events = %d, want exactly 1", len(terminals))
	}
	data, _ := terminals[0]["data"].(map[string]any)
	for _, key := range []string{"submitted", "nudges", "reason", "cost_usd"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("counted terminal event is missing %q: %#v", key, data)
		}
	}
}
