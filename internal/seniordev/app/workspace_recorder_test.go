//go:build !windows

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
)

func snapshotWorkspace(t *testing.T, files map[string]string) (string, *snapshotRecorder) {
	t.Helper()
	workspace := t.TempDir()
	for name, content := range files {
		if err := writeFile(filepath.Join(workspace, name), content); err != nil {
			t.Fatal(err)
		}
	}
	recorder := newSnapshotRecorder(workspace, func(string) {})
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	return workspace, recorder
}

// The identifier is a content address: the same bytes give the same id from a
// different directory, and any change gives a different one. Everything the
// run does with these — the submit gate, the restore proof — rests on it.
func TestSnapshotIdentifiesTreesByContent(t *testing.T) {
	first, recorderA := snapshotWorkspace(t, map[string]string{
		"main.go": "package main\n", "docs/readme.md": "hello\n",
	})
	_, recorderB := snapshotWorkspace(t, map[string]string{
		"main.go": "package main\n", "docs/readme.md": "hello\n",
	})
	idA, err := recorderA.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	idB, err := recorderB.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if idA != idB {
		t.Fatalf("identical trees gave different ids: %s and %s", shortSHA(idA), shortSHA(idB))
	}

	if err := writeFile(filepath.Join(first, "main.go"), "package main // edited\n"); err != nil {
		t.Fatal(err)
	}
	changed, err := recorderA.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if changed == idA {
		t.Fatal("editing a file did not change the tree id")
	}
}

// A file's mode is part of the tree: chmod +x with no content change is a real
// change, and a restore that dropped it would ship a broken script.
func TestSnapshotIdentityIncludesFileMode(t *testing.T) {
	workspace, recorder := snapshotWorkspace(t, map[string]string{"run.sh": "#!/bin/sh\n"})
	before, err := recorder.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(workspace, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	after, err := recorder.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("chmod +x did not change the tree id")
	}
}

// The restore has to handle all three shapes of divergence at once: a file the
// model edited, one it created, and one it deleted.
func TestSnapshotRestoreReturnsTheExactTree(t *testing.T) {
	workspace, recorder := snapshotWorkspace(t, map[string]string{
		"keep.txt": "keep\n", "edit.txt": "before\n", "delete-me.txt": "doomed\n",
	})
	original, err := recorder.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Record(original, "starting tree"); err != nil {
		t.Fatal(err)
	}

	if err := writeFile(filepath.Join(workspace, "edit.txt"), "after\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "delete-me.txt")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "nested/new.txt"), "added\n"); err != nil {
		t.Fatal(err)
	}
	diverged, err := recorder.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if diverged == original {
		t.Fatal("the tree did not diverge")
	}

	if err := recorder.Restore(original, original); err != nil {
		t.Fatalf("restore: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "edit.txt"))
	if err != nil || string(content) != "before\n" {
		t.Fatalf("edit.txt = %q, %v; want the recorded content", content, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "delete-me.txt")); err != nil {
		t.Fatal("a deleted file was not brought back")
	}
	if _, err := os.Stat(filepath.Join(workspace, "nested/new.txt")); !os.IsNotExist(err) {
		t.Fatal("a file added after the checkpoint survived the restore")
	}
}

// Restore proves itself by re-identifying the result. A caller that asks for a
// tree it did not record must be told, not quietly given something else.
func TestSnapshotRestoreRefusesAMismatch(t *testing.T) {
	_, recorder := snapshotWorkspace(t, map[string]string{"a.txt": "one\n"})
	id, err := recorder.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Record(id, "start"); err != nil {
		t.Fatal(err)
	}
	err = recorder.Restore(id, strings.Repeat("0", 64))
	if err == nil {
		t.Fatal("restore accepted a tree that is not the one requested")
	}
	if !strings.Contains(err.Error(), "restored tree") {
		t.Fatalf("error does not name the mismatch: %v", err)
	}
}

// Ignored paths and senior-dev's own artifacts are not the answer, so they are
// not in the tree the run compares, freezes or restores.
func TestSnapshotHonoursIgnoresAndSkipsArtifacts(t *testing.T) {
	workspace, recorder := snapshotWorkspace(t, map[string]string{
		".gitignore":          "build/\n*.log\n!keep.log\n",
		"src/main.go":         "package main\n",
		"build/artifact.bin":  "binary\n",
		"debug.log":           "noise\n",
		"keep.log":            "wanted\n",
		".senior-dev/spec.md": "the request\n",
	})
	paths, _, overBudget, err := recorder.ListPaths(context.Background(), 1<<20)
	if err != nil || overBudget {
		t.Fatalf("ListPaths: err=%v overBudget=%v", err, overBudget)
	}
	listed := strings.Join(paths, " ")
	for _, want := range []string{".gitignore", "src/main.go", "keep.log"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("%s missing from the tree: %v", want, paths)
		}
	}
	for _, unwanted := range []string{"build/artifact.bin", "debug.log", ".senior-dev/spec.md"} {
		if strings.Contains(listed, unwanted) {
			t.Fatalf("%s should not be part of the answer: %v", unwanted, paths)
		}
	}
	// And the ignored files are still on disk: excluded from the answer is not
	// the same as deleted.
	if _, err := os.Stat(filepath.Join(workspace, "debug.log")); err != nil {
		t.Fatal("an ignored file was removed from the workspace")
	}
}

// The submit gate asks exactly one question: has anything changed since the
// start. It has to answer that without git.
func TestSnapshotChangeDrivesTheSubmitGate(t *testing.T) {
	workspace, recorder := snapshotWorkspace(t, map[string]string{"main.go": "package main\n"})
	base, err := recorder.Base(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Record(base, "start"); err != nil {
		t.Fatal(err)
	}

	change, err := recorder.Change(base)
	if err != nil {
		t.Fatal(err)
	}
	if change.changed {
		t.Fatal("an untouched tree reported a change")
	}

	if err := writeFile(filepath.Join(workspace, "feature.go"), "package main\n"); err != nil {
		t.Fatal(err)
	}
	change, err = recorder.Change(base)
	if err != nil {
		t.Fatal(err)
	}
	if !change.changed || change.files != 1 {
		t.Fatalf("change = %+v, want one changed file", change)
	}

	// senior-dev's own bookkeeping is not an implementation: a tree whose only
	// new content is .senior-dev/ must still read as unchanged, or every run
	// could submit having done nothing.
	if err := writeFile(filepath.Join(workspace, "feature.go"), "package main\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(workspace, "feature.go")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, ".senior-dev/checklist.md"), "[x] done\n"); err != nil {
		t.Fatal(err)
	}
	change, err = recorder.Change(base)
	if err != nil {
		t.Fatal(err)
	}
	if change.changed {
		t.Fatalf("senior-dev's own artifacts counted as an implementation: %+v", change)
	}
}

// The point of the mode: a repository the run has no business writing to must
// come out with its history untouched.
func TestInPlaceRunLeavesGitHistoryAlone(t *testing.T) {
	workspace := testRepoWithEntrypoints(t)
	before := gitOutput(context.Background(), workspace, "rev-parse", "HEAD")
	beforeLog := gitOutput(context.Background(), workspace, "log", "--oneline")

	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	host := &testHost{workspace: workspace}
	var notes strings.Builder
	ending := runWith(context.Background(), host, Options{
		Goal: "Implement the thing.", High: "provider/high", InPlace: true,
	}, &notes, &coderOnlyBackend{})
	if ending.Status == "crashed" {
		t.Fatalf("in-place run failed: %s\n%s", ending.Message, notes.String())
	}

	after := gitOutput(context.Background(), workspace, "rev-parse", "HEAD")
	if after != before {
		t.Fatalf("HEAD moved: %s -> %s", shortSHA(before), shortSHA(after))
	}
	if now := gitOutput(context.Background(), workspace, "log", "--oneline"); now != beforeLog {
		t.Fatalf("the run wrote history:\nbefore:\n%s\nafter:\n%s", beforeLog, now)
	}
	if !strings.Contains(notes.String(), `"workspace_recorder":"snapshot"`) {
		t.Fatal("the run contract does not record the snapshot recorder")
	}
}

// And the mode's other half: no repository at all.
func TestInPlaceRunNeedsNoRepository(t *testing.T) {
	workspace := t.TempDir()
	for name, content := range map[string]string{
		"README.md": "base\n",
		"Makefile":  "build:\n\t@true\n\ntest:\n\t@true\n",
	} {
		if err := writeFile(filepath.Join(workspace, name), content); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, ".git")); !os.IsNotExist(err) {
		t.Fatal("the fixture is a repository; this test needs one that is not")
	}

	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	var notes strings.Builder
	ending := runWith(context.Background(), &testHost{workspace: workspace}, Options{
		Goal: "Implement the thing.", High: "provider/high", InPlace: true,
	}, &notes, &coderOnlyBackend{})
	if ending.Status == "crashed" {
		t.Fatalf("run without a repository failed: %s\n%s", ending.Message, notes.String())
	}
	if _, err := os.Stat(filepath.Join(workspace, ".git")); !os.IsNotExist(err) {
		t.Fatal("the run created a repository in a workspace that had none")
	}
}

// Without --in-place, git is used only if it is there. A plain folder runs
// on the snapshot recorder and ends like any run, never with "not a git
// repository", and no repository is made for it.
func TestADefaultRunInAPlainFolderUsesTheSnapshotRecorder(t *testing.T) {
	workspace := t.TempDir()
	for name, content := range map[string]string{
		"README.md": "base\n",
		"Makefile":  "build:\n\t@true\n\ntest:\n\t@true\n",
	} {
		if err := writeFile(filepath.Join(workspace, name), content); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	var notes strings.Builder
	ending := runWith(context.Background(), &testHost{workspace: workspace}, Options{
		Goal: "Implement the thing.", High: "provider/high",
	}, &notes, &coderOnlyBackend{})
	if ending.Status == "crashed" {
		t.Fatalf("a plain folder crashed the default run: %s\n%s", ending.Message, notes.String())
	}
	if !strings.Contains(notes.String(), `"workspace_recorder":"snapshot"`) {
		t.Fatal("the run contract does not record the snapshot recorder")
	}
	if _, err := os.Stat(filepath.Join(workspace, ".git")); !os.IsNotExist(err) {
		t.Fatal("the run created a repository in a workspace that had none")
	}
}

// The recorder follows what git can actually give: a work tree with a commit
// is git's, and everything short of that — no repository, one with no commit
// yet, a .git folder with no HEAD — is the snapshot recorder's. --in-place
// still forces the snapshot recorder over a real repository.
func TestTheRecorderIsGitOnlyWhereThereIsGitHistory(t *testing.T) {
	ctx := context.Background()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{
			"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
		}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	plain := t.TempDir()
	empty := t.TempDir()
	git(empty, "init", "-q")
	broken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(broken, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	committed := t.TempDir()
	git(committed, "init", "-q")
	if err := writeFile(filepath.Join(committed, "a.txt"), "a\n"); err != nil {
		t.Fatal(err)
	}
	git(committed, "add", "a.txt")
	git(committed, "commit", "-q", "-m", "base")
	nested := filepath.Join(committed, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, dir string
		inPlace   bool
		want      string
	}{
		{"plain folder", plain, false, "snapshot"},
		{"repository with no commit", empty, false, "snapshot"},
		{"a .git folder with no HEAD", broken, false, "snapshot"},
		{"repository with a commit", committed, false, "git"},
		{"a folder inside one", nested, false, "git"},
		{"--in-place over a repository", committed, true, "snapshot"},
	} {
		got := newWorkspaceRecorder(cliArgs{InPlace: tc.inPlace}, tc.dir, func(string) {})
		if got.Kind() != tc.want {
			t.Errorf("%s: recorder %q, want %q", tc.name, got.Kind(), tc.want)
		}
		if err := got.Prepare(ctx); err != nil {
			t.Errorf("%s: %s recorder did not prepare: %v", tc.name, got.Kind(), err)
		}
	}
}

// Every in-place rewrite must fire against the real prompt text. A rewrite
// that silently matched nothing would leave the model with git-shaped
// instructions it cannot follow, which is invisible at runtime.
func TestInPlacePromptRewritesAllMatch(t *testing.T) {
	coder, ok := baked.GetBakedAgent("coder")
	if !ok {
		t.Fatal("the coder agent is not available")
	}
	solo := buildSoloPrompt("Do the thing.", "", ".senior-dev/checklist.md")
	recorder := newSnapshotRecorder(t.TempDir(), func(string) {})

	adaptedCoder, err := adaptCoderPrompt(recorder, coder)
	if err != nil {
		t.Fatalf("a coder rewrite no longer matches: %v", err)
	}
	adaptedSolo, err := adaptSoloPrompt(recorder, solo)
	if err != nil {
		t.Fatalf("a run-instruction rewrite no longer matches: %v", err)
	}
	for _, leftover := range []string{"starting commit", "git-ignored", "is a git repository"} {
		if strings.Contains(adaptedCoder, leftover) {
			t.Fatalf("coder prompt still says %q", leftover)
		}
		if strings.Contains(adaptedSolo, leftover) {
			t.Fatalf("solo prompt still says %q", leftover)
		}
	}
	if !strings.Contains(adaptedSolo, "does not use git") {
		t.Fatal("the solo prompt does not tell the model git is unavailable")
	}
}

// The git path's prompt bytes are the deliverable of choosing substitution
// over rewording: an unchanged prompt hash keeps earlier runs comparable.
func TestGitRecorderLeavesPromptsByteIdentical(t *testing.T) {
	coder, _ := baked.GetBakedAgent("coder")
	solo := buildSoloPrompt("Do the thing.", "", ".senior-dev/checklist.md")
	recorder := newGitRecorder(t.TempDir(), func(string) {})

	adaptedCoder, err := adaptCoderPrompt(recorder, coder)
	if err != nil {
		t.Fatal(err)
	}
	adaptedSolo, err := adaptSoloPrompt(recorder, solo)
	if err != nil {
		t.Fatal(err)
	}
	if adaptedCoder != coder {
		t.Fatal("the git path's system prompt changed")
	}
	if adaptedSolo != solo {
		t.Fatal("the git path's run instruction changed")
	}
}

// A folder or file the workspace will not let the run read is not part of the
// tree: the run is not ended by it, no snapshot holds it, and a restore neither
// removes nor writes it. macOS answers `operation not permitted` for some
// folders even to their owner, and one of them ended a run at its first step.
func TestSnapshotSkipsWhatItMayNotRead(t *testing.T) {
	workspace, recorder := snapshotWorkspace(t, map[string]string{
		"main.go": "package main\n", "locked/inside.txt": "private\n", "sealed.txt": "private\n",
	})
	locked, sealed := filepath.Join(workspace, "locked"), filepath.Join(workspace, "sealed.txt")
	for _, name := range []string{locked, sealed} {
		if err := os.Chmod(name, 0); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755); _ = os.Chmod(sealed, 0o644) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("this user reads a folder with no permissions (root?)")
	}
	original, err := recorder.Snapshot()
	if err != nil {
		t.Fatalf("snapshot of a workspace holding an unreadable folder: %v", err)
	}
	paths, _, _, err := recorder.ListPaths(context.Background(), 1<<20)
	if err != nil || strings.Join(paths, ",") != "main.go" {
		t.Fatalf("paths = %q, %v; want only the readable file", paths, err)
	}
	if _, err := recorder.Record(original, "start"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "main.go"), "package main // edited\n"); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Restore(original, original); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for _, name := range []string{locked, sealed} {
		if _, err := os.Lstat(name); err != nil {
			t.Fatalf("the restore touched %s: %v", name, err)
		}
	}
}
