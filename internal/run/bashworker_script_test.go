package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// scriptSeat answers every request with one script call that makes two bash
// calls of its own, the second exiting 4.
type scriptSeat struct{}

func (scriptSeat) CompleteWithMessages(_ context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	args := `{"source":"await tools.bash({command: \"echo one\"}); const r = await tools.bash({command: \"echo two; exit 4\"}); return r.exit_code"}`
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
		ID: "script-1", Type: "function", Function: ai.ToolCallFunction{Name: "script", Arguments: args},
	}}}}}}, nil
}

// A SCRIPT IS ONE STEP. Its nested calls are recorded on that step, never as
// steps of their own, so they spend nothing of the step cap and a page drawn
// from the record can hang them under the script that made them.
func TestBashWorkerRecordsAScriptAsOneStepWithItsCalls(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_TASK_SCRIPT", "on")
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	stub := filepath.Join(root, "stub-codeaf")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_PLANDB_BIN", stub)
	store, err := plandb.Open(filepath.Join(root, "plan.db"), "script", "root", "Script", "Run a script")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	worker := NewBashWorker(store, root, "test/model", scriptSeat{})
	ctx, cancel := context.WithTimeout(WithStepsPerTask(t.Context(), 1), 20*time.Second)
	defer cancel()
	report, runErr := worker.Run(ctx, *store.Task(store.RootID()))
	if runErr == nil || !strings.Contains(runErr.Error(), "after 1 step") || report.Steps != 1 {
		t.Fatalf("the script was not counted as one step: report=%+v err=%v", report, runErr)
	}
	steps, err := Trajectory(root, store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Tool != "script" {
		t.Fatalf("want one script step, got %+v", steps)
	}
	calls := steps[0].Calls
	if len(calls) != 2 {
		t.Fatalf("the script step carries %d calls, want 2: %+v", len(calls), calls)
	}
	if calls[0].Tool != "bash" || calls[0].Command != "echo one" || calls[0].ExitCode == nil || *calls[0].ExitCode != 0 {
		t.Errorf("first call recorded as %+v", calls[0])
	}
	if calls[1].Command != "echo two; exit 4" || calls[1].ExitCode == nil || *calls[1].ExitCode != 4 || !strings.Contains(calls[1].Observation, "two") {
		t.Errorf("second call recorded as %+v", calls[1])
	}
}

// bigScriptSeat makes one script call whose nested bash is cut at the
// script's width, then returns a short answer that is not cut.
type bigScriptSeat struct{}

func (bigScriptSeat) CompleteWithMessages(_ context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	args := `{"source":"const r = await tools.bash({command: \"head -c 1100000 /dev/zero | tr '\\\\0' x\"}); return r.truncated ? 'cut' : 'whole'"}`
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{{
		ID: "script-1", Type: "function", Function: ai.ToolCallFunction{Name: "script", Arguments: args},
	}}}}}}, nil
}

// A SCRIPT STEP'S WHOLE OUTPUT IS ITS OWN. A nested bash cut at the script's
// width files its output too, and the step must not take that file as the
// script's.
func TestBashWorkerScriptStepDoesNotTakeANestedCallsFile(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_TASK_SCRIPT", "on")
	root := t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	stub := filepath.Join(root, "stub-codeaf")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_PLANDB_BIN", stub)
	store, err := plandb.Open(filepath.Join(root, "plan.db"), "script", "root", "Script", "Run a script")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	worker := NewBashWorker(store, root, "test/model", bigScriptSeat{})
	ctx, cancel := context.WithTimeout(WithStepsPerTask(t.Context(), 1), 30*time.Second)
	defer cancel()
	_, _ = worker.Run(ctx, *store.Task(store.RootID()))
	steps, err := Trajectory(root, store.RootID())
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps = %+v, err = %v", steps, err)
	}
	if !strings.Contains(steps[0].Observation, "cut") {
		t.Fatalf("the nested bash was not cut, so this proves nothing: %q", steps[0].Observation)
	}
	if steps[0].FullOutput != "" {
		t.Errorf("the script step names %q as its whole output; that file is a nested call's", steps[0].FullOutput)
	}
}

// A STEP THAT IS NOT RECORDED LEAVES NO CALLS BEHIND.
func TestStepRecorderDropsTheCallsOfAStepItSkips(t *testing.T) {
	var rec stepRecorder
	rec.nest(session.Event{Kind: session.EventToolEnd, Tool: "bash", Parent: "p", Args: `{"command":"ls"}`})
	rec.drop("p")
	if len(rec.calls) != 0 {
		t.Errorf("a skipped step's calls are still held: %+v", rec.calls)
	}
}
