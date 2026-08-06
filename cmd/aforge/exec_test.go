package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

func TestExecExitCode(t *testing.T) {
	tests := []struct {
		name string
		stop exec.StopReason
		text string
		want int
	}{
		{name: "done", stop: exec.StopDone, text: "answer", want: 0},
		{name: "budget", stop: exec.StopBudget, text: "partial", want: 2},
		{name: "turn cap", stop: exec.StopTurnCap, text: "partial", want: 3},
		{name: "deadline", stop: exec.StopDeadline, text: "partial", want: 4},
		{name: "error", stop: exec.StopError, text: "partial", want: 5},
		{name: "done empty", stop: exec.StopDone, text: "", want: 6},
		{name: "done whitespace", stop: exec.StopDone, text: " \n\t", want: 6},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := execExitCode(test.stop, test.text); got != test.want {
				t.Fatalf("execExitCode(%q, %q) = %d, want %d", test.stop, test.text, got, test.want)
			}
		})
	}
}

func TestBuildExecEnvelopeJSONShape(t *testing.T) {
	outcome := &exec.Outcome{
		Text: "answer",
		Usage: exec.Usage{
			Calls:            2,
			PromptTokens:     30,
			CompletionTokens: 12,
			CachedTokens:     4,
			Cost:             0.125,
		},
		Stop:    exec.StopDone,
		Turns:   7,
		Elapsed: 1234 * time.Millisecond,
	}

	encoded, err := json.Marshal(buildExecEnvelope(outcome))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text":"answer","stop":"done","usage":{"calls":2,"prompt_tokens":30,"completion_tokens":12,"cached_tokens":4,"cost":0.125},"artifacts":[],"turns":7,"elapsed_ms":1234}`
	if string(encoded) != want {
		t.Fatalf("envelope JSON = %s, want %s", encoded, want)
	}
}

// A nil outcome is what a provider failure leaves behind, and the envelope is
// still the only thing a calling harness gets to read. It has to describe the
// failure rather than crash the process that was trying to report it.
func TestBuildExecEnvelopeToleratesNilOutcome(t *testing.T) {
	encoded, err := json.Marshal(buildExecEnvelope(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text":"","stop":"error","usage":{"calls":0,"prompt_tokens":0,"completion_tokens":0,"cached_tokens":0,"cost":0},"artifacts":[],"turns":0,"elapsed_ms":0}`
	if string(encoded) != want {
		t.Fatalf("envelope JSON = %s, want %s", encoded, want)
	}
	if code := execExitCode(buildExecEnvelope(nil).Stop, ""); code != 5 {
		t.Fatalf("exit code for a nil outcome = %d, want 5", code)
	}
}

func TestExecTask(t *testing.T) {
	task := execTask("  First line  \nSecond line", "be exact", "/tmp/workspace")

	if task.NodeID != 1 {
		t.Fatalf("NodeID = %d, want 1", task.NodeID)
	}
	if task.Title != "First line" {
		t.Fatalf("Title = %q, want first trimmed line", task.Title)
	}
	if task.Brief != "  First line  \nSecond line\n\nWorkspace root (your working directory): /tmp/workspace" {
		t.Fatalf("Brief = %q", task.Brief)
	}
	if !strings.Contains(task.Brief, "/tmp/workspace") {
		t.Fatal("Brief does not contain the workspace root")
	}
	if task.Contract != "be exact" {
		t.Fatalf("Contract = %q, want system value", task.Contract)
	}
	if task.OutputHint != "" {
		t.Fatalf("OutputHint = %q, want empty", task.OutputHint)
	}
	if task.Goal != "" {
		t.Fatalf("Goal = %q, want empty", task.Goal)
	}
	if len(task.Inputs) != 0 {
		t.Fatalf("Inputs has %d entries, want none", len(task.Inputs))
	}
}
