//go:build !windows

package app

import (
	"context"
	"os"
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

	t.Setenv("SENIOR_DEV_CP_URL", deadControlPlaneURL(t))
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	args := []string{
		"run", "--in-place", "--dir", workspace, "--high", "provider/high",
		"Implement the thing.",
	}
	backend := &coderOnlyBackend{}
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), args, backend, &stdout, &stderr); err != nil {
		t.Fatalf("in-place run failed: %v\n%s", err, stderr.String())
	}

	after := gitOutput(context.Background(), workspace, "rev-parse", "HEAD")
	if after != before {
		t.Fatalf("HEAD moved: %s -> %s", shortSHA(before), shortSHA(after))
	}
	if now := gitOutput(context.Background(), workspace, "log", "--oneline"); now != beforeLog {
		t.Fatalf("the run wrote history:\nbefore:\n%s\nafter:\n%s", beforeLog, now)
	}
	if !strings.Contains(stdout.String(), `"workspace_recorder":"snapshot"`) {
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

	t.Setenv("SENIOR_DEV_CP_URL", deadControlPlaneURL(t))
	t.Setenv("SENIOR_DEV_SCRATCH_ROOT", t.TempDir())
	args := []string{
		"run", "--in-place", "--dir", workspace, "--high", "provider/high",
		"Implement the thing.",
	}
	backend := &coderOnlyBackend{}
	var stdout, stderr strings.Builder
	if err := runCLI(context.Background(), args, backend, &stdout, &stderr); err != nil {
		t.Fatalf("run without a repository failed: %v\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(workspace, ".git")); !os.IsNotExist(err) {
		t.Fatal("the run created a repository in a workspace that had none")
	}
}

// Without --in-place the workspace must still be a repository. The default
// path is unchanged, and this is what says so.
func TestDefaultRunStillRequiresARepository(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("SENIOR_DEV_CP_URL", deadControlPlaneURL(t))
	args := []string{
		"run", "--dir", workspace, "--high", "provider/high", "Implement the thing.",
	}
	err := runCLI(context.Background(), args, &coderOnlyBackend{}, &strings.Builder{}, &strings.Builder{})
	if err == nil {
		t.Fatal("a non-repository workspace was accepted without --in-place")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error does not name the cause: %v", err)
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
