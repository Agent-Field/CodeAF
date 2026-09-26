//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file hidden by an ignore rule the person added after submission belongs
// to their later work, so the git restore must copy it before resetting rules.
func TestNewlyIgnoredPersonalFileIsRescuedBeforeGitRestore(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	workspace, _ := guardWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("# baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", ".gitignore"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "baseline ignore"); err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("personal.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "personal.txt"), []byte("person's data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "personal.txt")); !os.IsNotExist(err) {
		t.Fatalf("later file remained in restored candidate: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(runner.rescuePath, "personal.txt"))
	if err != nil || string(body) != "person's data\n" {
		t.Fatalf("newly ignored file was not rescued: %q, %v", body, err)
	}
}

// A removed candidate file is a person's later deletion. The candidate may
// restore the file only after a plain manifest records that deletion outside it.
func TestPersonalDeletionIsListedBeforeGitRestore(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
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
	if err := os.Remove(filepath.Join(workspace, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "README.md")); err != nil {
		t.Fatalf("candidate file was not restored: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(runner.rescuePath, "deleted-files.txt"))
	if err != nil || string(body) != "README.md\n" {
		t.Fatalf("later deletion manifest = %q, %v", body, err)
	}
	outcome := soloOutcome{Status: "pass"}
	runner.soloTerminal(&outcome, "submitted")
	ending := endingOf(pipelineResult{Status: "pass", Terminal: outcome.TerminalData})
	if !strings.Contains(ending.Message, "deleted-files.txt") {
		t.Fatalf("ending did not name the deletion manifest: %s", ending.Message)
	}
}

// A restore with no intervening edit leaves no rescue folder or rescue claim.
func TestUnchangedGitRestoreCreatesNoRescue(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
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
	if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
		t.Fatal(err)
	}
	if runner.rescuePath != "" {
		entries, _ := os.ReadDir(runner.rescuePath)
		t.Fatalf("unchanged tree named a rescue: %s, entries %v", runner.rescuePath, entries)
	}
	if _, err := os.Stat(filepath.Join(state, "v3", "carried", "senior-dev", "rescued")); !os.IsNotExist(err) {
		t.Fatalf("unchanged tree created a rescue root: %v", err)
	}
	outcome := soloOutcome{Status: "pass"}
	runner.soloTerminal(&outcome, "submitted")
	if ending := endingOf(pipelineResult{Status: "pass", Terminal: outcome.TerminalData}); strings.Contains(ending.Message, "set aside") {
		t.Fatalf("unchanged tree named a rescue in its ending: %s", ending.Message)
	}
}

// The plain-folder recorder compares both sides of the saved manifest, so a
// file the person deleted after submission is recorded before restoration.
func TestPersonalDeletionIsListedBeforePlainFolderRestore(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{InPlace: true}, workspace, pipelineDeps{Events: newEventWriter(discardWriter{}), Notes: discardWriter{}})
	t.Cleanup(runner.runtime.Close)
	wanted, err := runner.currentTreeSHA()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := runner.soloRecordTree(wanted, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(runner.rescuePath, "deleted-files.txt")); err != nil || string(body) != "README.md\n" {
		t.Fatalf("plain-folder deletion manifest = %q, %v", body, err)
	}
}

// A plain folder can also hide a new personal file by changing .gitignore;
// the snapshot restore must rescue it before restoring the candidate rules.
func TestNewlyIgnoredPersonalFileIsRescuedBeforePlainFolderRestore(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("# baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{InPlace: true}, workspace, pipelineDeps{Events: newEventWriter(discardWriter{}), Notes: discardWriter{}})
	t.Cleanup(runner.runtime.Close)
	wanted, err := runner.currentTreeSHA()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := runner.soloRecordTree(wanted, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("personal.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "personal.txt"), []byte("person's data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "personal.txt")); !os.IsNotExist(err) {
		t.Fatalf("later file remained in restored candidate: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(runner.rescuePath, "personal.txt"))
	if err != nil || string(body) != "person's data\n" {
		t.Fatalf("newly ignored plain-folder file was not rescued: %q, %v", body, err)
	}
}

// The snapshot road also leaves no rescue when nothing changed after the
// candidate was recorded.
func TestUnchangedPlainFolderRestoreCreatesNoRescue(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{InPlace: true}, workspace, pipelineDeps{Events: newEventWriter(discardWriter{}), Notes: discardWriter{}})
	t.Cleanup(runner.runtime.Close)
	wanted, err := runner.currentTreeSHA()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := runner.soloRecordTree(wanted, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
		t.Fatal(err)
	}
	if runner.rescuePath != "" {
		t.Fatalf("unchanged plain folder named a rescue: %s", runner.rescuePath)
	}
	if _, err := os.Stat(filepath.Join(state, "v3", "carried", "senior-dev", "rescued")); !os.IsNotExist(err) {
		t.Fatalf("unchanged plain folder created a rescue root: %v", err)
	}
}

// A person's file may have the manifest's usual name. Both that file and a
// deletion remain readable in the rescue rather than overwriting each other.
func TestDeletionManifestDoesNotOverwriteARescuedFile(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	workspace := t.TempDir()
	for name, body := range map[string]string{"README.md": "base\n", "deleted-files.txt": "candidate\n"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runner := newPipeline(cliArgs{InPlace: true}, workspace, pipelineDeps{Events: newEventWriter(discardWriter{}), Notes: discardWriter{}})
	t.Cleanup(runner.runtime.Close)
	wanted, err := runner.currentTreeSHA()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := runner.soloRecordTree(wanted, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "deleted-files.txt"), []byte("person's file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"deleted-files.txt": "person's file\n", "deleted-files-2.txt": "README.md\n"} {
		body, err := os.ReadFile(filepath.Join(runner.rescuePath, name))
		if err != nil || string(body) != want {
			t.Fatalf("rescued %s = %q, %v", name, body, err)
		}
	}
	outcome := soloOutcome{Status: "pass"}
	runner.soloTerminal(&outcome, "submitted")
	ending := endingOf(pipelineResult{Status: "pass", Terminal: outcome.TerminalData})
	if !strings.Contains(ending.Message, "deleted-files-2.txt") {
		t.Fatalf("ending did not name the chosen manifest: %s", ending.Message)
	}
}

// Git keeps tracking a file after an ignore rule starts matching its name.
// Its candidate edit and a later personal edit both remain accounted for.
func TestTrackedFileMatchingIgnoreRuleKeepsCandidateAndRescue(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	workspace, _ := guardWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("README.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", ".gitignore"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "ignore tracked readme"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{Events: newEventWriter(discardWriter{}), Notes: discardWriter{}})
	t.Cleanup(runner.runtime.Close)
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wanted, err := runner.currentTreeSHA()
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := runner.soloRecordTree(wanted, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("person's edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(workspace, "README.md")); err != nil || string(body) != "candidate\n" {
		t.Fatalf("tracked candidate edit = %q, %v", body, err)
	}
	if body, err := os.ReadFile(filepath.Join(runner.rescuePath, "README.md")); err != nil || string(body) != "person's edit\n" {
		t.Fatalf("tracked later edit rescue = %q, %v", body, err)
	}
}
