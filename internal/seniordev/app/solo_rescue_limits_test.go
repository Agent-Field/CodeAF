//go:build !windows

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// The manual's limits must move in the same change as the copying bounds.
func TestRescueManualQuotesTheBoundedCopyLimits(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "manual", "chat", "senior-dev.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int64{rescueFileLimitBytes, rescueTotalLimitBytes} {
		figure := fmt.Sprintf("%d MiB", limit>>20)
		if !strings.Contains(string(page), figure) {
			t.Fatalf("rescue manual omits %s", figure)
		}
	}
}

// The total bound is checked before the first file is copied, even when every
// individual file fits under the per-file bound.
func TestRescueTotalLimitRefusesBeforeCopying(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("CODEAF_HOME", stateRoot)
	workspace := t.TempDir()
	runner := newPipeline(cliArgs{InPlace: true}, workspace, pipelineDeps{Events: newEventWriter(discardWriter{}), Notes: discardWriter{}})
	t.Cleanup(runner.runtime.Close)
	var paths []string
	for index := 0; index < int(rescueTotalLimitBytes/rescueFileLimitBytes)+1; index++ {
		name := fmt.Sprintf("later-%02d.bin", index)
		file := filepath.Join(workspace, name)
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(file, rescueFileLimitBytes); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, name)
	}
	if err := runner.rescueBeforeRestore(paths); err == nil || !strings.Contains(err.Error(), "250 MiB") {
		t.Fatalf("over-total rescue = %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "v3", "carried", "senior-dev", "rescued")); !os.IsNotExist(err) {
		t.Fatalf("over-total rescue wrote a folder: %v", err)
	}
}

// An oversized later file refuses the entire restore before any project path
// is changed, and the ending describes the checkout as unchecked.
func TestOversizedRescueLeavesTheFolderUntouchedAndUnchecked(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("CODEAF_HOME", stateRoot)
	runner, state, _, _ := soloPipeline(t)
	feature := filepath.Join(runner.workspace, "feature.txt")
	if err := writeFile(feature, "candidate\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("done")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(feature, "later edit\n"); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(runner.workspace, "large.bin")
	if err := os.WriteFile(large, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(large, rescueFileLimitBytes+1); err != nil {
		t.Fatal(err)
	}
	outcome := &soloOutcome{Status: "pass", Frozen: state.candidate()}
	runner.soloRestoreIfDiverged(state, outcome)
	runner.soloTerminal(outcome, "submitted, and its build and tests passed")
	ending := endingOf(pipelineResult{Status: delegate.StatusPass, Terminal: outcome.TerminalData})
	if outcome.Status != "pass-unverified" || !strings.Contains(outcome.RestoreFailed, "larger than") {
		t.Fatalf("oversized rescue ended as %q: %q", outcome.Status, outcome.RestoreFailed)
	}
	if strings.Contains(outcome.TerminalReason, "build and tests passed") || !strings.Contains(ending.Message, "could not be put back") {
		t.Fatalf("unchecked ending = %q, terminal = %q", ending.Message, outcome.TerminalReason)
	}
	if data, err := os.ReadFile(feature); err != nil || string(data) != "later edit\n" {
		t.Fatalf("later edit changed: %q, %v", data, err)
	}
	if info, err := os.Stat(large); err != nil || info.Size() != rescueFileLimitBytes+1 {
		t.Fatalf("large file changed: %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, "v3", "carried", "senior-dev", "rescued")); !os.IsNotExist(err) {
		t.Fatalf("oversized rescue made a folder before refusal: %v", err)
	}
}

// A successful rescue stays private even when the source files are readable
// by everyone in the project.
func TestRescueFolderAndFilesArePrivate(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("CODEAF_HOME", stateRoot)
	workspace, _ := guardWorkspace(t)
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{Events: newEventWriter(discardWriter{}), Notes: discardWriter{}})
	t.Cleanup(runner.runtime.Close)
	wanted, err := runner.currentTreeSHA()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := runner.soloRecordTree(wanted, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{runner.rescuePath: 0o700, filepath.Join(runner.rescuePath, "notes.txt"): 0o600, filepath.Join(runner.rescuePath, "deleted-files.txt"): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("rescue mode %s = %v, %v; want %v", path, info, err, want)
		}
	}
}
