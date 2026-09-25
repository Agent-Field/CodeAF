//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// A RESTORE THAT COULD NOT RUN LEAVES AN ENDING THAT SAYS SO. The submitted
// candidate passed its build and tests, but the folder changed after that and
// the restore could not set the later bytes aside (here the rescue folder's
// place is taken by a file), so it refused to put the candidate back. Nothing is
// lost, and what the folder holds is no longer what was checked: the ending must
// not claim the build and tests passed on it, and must say why.
func TestAFailedRestoreEndsUncheckedAndSaysWhy(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("CODEAF_HOME", stateRoot)
	runner, state, _, _ := soloPipeline(t)
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "candidate\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "later untested bytes\n"); err != nil {
		t.Fatal(err)
	}
	rescueRoot := filepath.Join(stateRoot, "v3", "carried", "senior-dev", "rescued")
	if err := os.MkdirAll(filepath.Dir(rescueRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rescueRoot, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := &soloOutcome{Status: "pass", Frozen: state.candidate()}
	runner.soloRestoreIfDiverged(state, outcome)
	runner.soloTerminal(outcome, "submitted, and its build and tests passed")
	if outcome.Status != "pass-unverified" {
		t.Fatalf("a restore that failed left the outcome %q; the folder is not what was checked", outcome.Status)
	}
	ending := endingOf(pipelineResult{Status: delegate.StatusPass, Terminal: outcome.TerminalData})
	if strings.Contains(ending.Message, "build and tests passed") {
		t.Fatalf("the ending claims a pass for a folder nothing checked: %q", ending.Message)
	}
	if !strings.Contains(ending.Message, "could not be put back") {
		t.Fatalf("the ending does not say the folder could not be put back: %q", ending.Message)
	}
	content, err := os.ReadFile(filepath.Join(runner.workspace, "feature.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "later untested bytes\n" {
		t.Fatalf("a failed restore must leave the folder as it was, got %q", content)
	}
}
