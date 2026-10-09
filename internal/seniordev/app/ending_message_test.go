//go:build !windows

package app

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// A pass is said as passed only when the project's own build and tests ran.
// A real run on a folder holding only a README printed
//
//	senior-dev finished: submitted a change, and the project's own build and tests passed
//	  senior-dev observed: the project has no build or tests it could find to run
//
// two witnesses contradicting each other about one check, because the ending's
// sentence read the status and never the count of commands behind it.
func TestPassEndingSaysOnlyWhatWasChecked(t *testing.T) {
	const passed = "submitted a change, and the project's own build and tests passed"
	for _, test := range []struct {
		name     string
		data     map[string]any
		message  string
		observed string
	}{
		{
			name:     "commands ran",
			data:     map[string]any{"status": "pass", "verification_commands": 3, "verification_failing": 0},
			message:  passed,
			observed: "the project's 3 build and test commands all passed",
		},
		{
			// The same record after it has crossed JSON, which is how codeaf
			// holds it once the run has ended.
			name:     "commands ran, read back from JSON",
			data:     map[string]any{"status": "pass", "verification_commands": float64(2), "verification_failing": float64(0)},
			message:  passed,
			observed: "the project's 2 build and test commands all passed",
		},
		{
			name:     "no command ran",
			data:     map[string]any{"status": "pass", "verification_commands": 0, "verification_failing": 0},
			message:  "submitted a change; the project has no build or tests it could find to run",
			observed: "the project has no build or tests it could find to run",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ending := endingOf(pipelineResult{Status: delegate.StatusPass, Terminal: test.data})
			if ending.Message != test.message {
				t.Errorf("message = %q, want %q", ending.Message, test.message)
			}
			if ending.Observed != test.observed {
				t.Errorf("observed = %q, want %q", ending.Observed, test.observed)
			}
		})
	}
}

// The same ending from the real road: a submitted change in a folder with
// nothing discoverable to run, through the project's own check and the
// terminal it writes. Neither the sentence nor the longer reason beside it may
// say anything passed.
func TestFolderWithNothingToRunEndsWithoutSayingItPassed(t *testing.T) {
	workspace := gitWorkspace(t, map[string]string{"README.md": "Notes about this folder.\n"})
	base := strings.TrimSpace(gitOutput(context.Background(), workspace, "rev-parse", "HEAD"))
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Backend: &soloScriptedBackend{}, Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()

	outcome, err := runner.runSolo(context.Background(), "Add the feature.", base)
	if err != nil {
		t.Fatal(err)
	}
	status, reason := soloResultStatus(outcome)
	if status != delegate.StatusPass || outcome.Status != "pass" {
		t.Fatalf("status = %q (inner %q), want a pass: %#v", status, outcome.Status, outcome.TerminalData)
	}
	if commands, ok := outcome.TerminalData["verification_commands"]; !ok || wholeNumber(commands) != 0 {
		t.Fatalf("verification_commands = %#v, want 0 for a folder with nothing to run", commands)
	}
	ending := endingOf(pipelineResult{Status: status, Reason: reason, Terminal: outcome.TerminalData})
	if want := "submitted a change; the project has no build or tests it could find to run"; ending.Message != want {
		t.Errorf("message = %q, want %q", ending.Message, want)
	}
	if want := "the project has no build or tests it could find to run"; ending.Observed != want {
		t.Errorf("observed = %q, want %q", ending.Observed, want)
	}
	for name, said := range map[string]string{"message": ending.Message, "reason": ending.Reason} {
		if strings.Contains(said, "passed") {
			t.Errorf("%s = %q, which says something passed when no command ran", name, said)
		}
	}
	if !strings.Contains(ending.Reason, "found no build or tests to run") {
		t.Errorf("reason = %q, want it to say no build or tests were found to run", ending.Reason)
	}
}
