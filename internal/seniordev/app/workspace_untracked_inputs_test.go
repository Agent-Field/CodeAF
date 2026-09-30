//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/gitidentity"
)

// COPIED INPUTS ARE NEITHER A CANDIDATE NOR A REASON TO COMMIT. The same
// persisted list guards freezing, clean-tree checks and checkpoint restore.
func TestGitRecorderKeepsCopiedInputsOutOfObjectsAndCandidates(t *testing.T) {
	workspace, _ := guardWorkspace(t)
	inputs := []string{"credentials.json", "local/odd\n[1].txt"}
	for _, path := range inputs {
		if err := writeFile(filepath.Join(workspace, path), "local-only content: "+path); err != nil {
			t.Fatal(err)
		}
	}
	list := filepath.Join(t.TempDir(), "inputs")
	if err := os.WriteFile(list, []byte(strings.Join(inputs, "\x00")+"\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SENIOR_DEV_IGNORED_AT_START", list)
	recorder := newGitRecorder(workspace, func(string) {})
	if findings := recorder.statusFindings(); len(findings) != 0 {
		t.Fatalf("copied inputs prompt a commit: %q", findings)
	}
	if err := writeFile(filepath.Join(workspace, "made.txt"), "new work\n"); err != nil {
		t.Fatal(err)
	}
	if findings := recorder.statusFindings(); len(findings) == 0 {
		t.Fatal("the program's own new file was mistaken for a copied input")
	}
	tree, err := recorder.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range inputs {
		blob, err := recorder.git("hash-object", "--", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := recorder.git("cat-file", "-e", blob); err == nil {
			t.Fatalf("copied input %q was written as a git blob", path)
		}
		if _, err := recorder.git("cat-file", "-e", tree+":"+path); err == nil {
			t.Fatalf("copied input %q entered the submitted tree", path)
		}
	}
	if _, err := recorder.git("cat-file", "-e", tree+":made.txt"); err != nil {
		t.Fatalf("the submitted tree lost the program's new work: %v", err)
	}
	handle, err := recorder.Record(tree, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Restore(handle, tree); err != nil {
		t.Fatal(err)
	}
	for _, path := range inputs {
		if body, err := os.ReadFile(filepath.Join(workspace, path)); err != nil || string(body) != "local-only content: "+path {
			t.Fatalf("restore lost copied input %q: %q, %v", path, body, err)
		}
	}
}

// AN INPUT THE RUN CHANGED IS ITS WORK. codeaf hands the run its copied
// untracked files with their fingerprints (gitidentity.InputsEnv): the one it
// finished enters the candidate and counts against a clean tree, and the one
// it left alone does neither and never becomes a blob.
func TestGitRecorderTakesTheInputsTheRunChanged(t *testing.T) {
	workspace, _ := guardWorkspace(t)
	for path, body := range map[string]string{"parser.py": "half\n", "credentials.json": "secret\n"} {
		if err := writeFile(filepath.Join(workspace, path), body); err != nil {
			t.Fatal(err)
		}
	}
	inputs := gitidentity.Inputs{}
	for _, path := range []string{"parser.py", "credentials.json"} {
		inputs[path], _ = gitidentity.Fingerprint(filepath.Join(workspace, path))
	}
	list := filepath.Join(t.TempDir(), "inputs")
	if err := gitidentity.WriteInputs(list, inputs); err != nil {
		t.Fatal(err)
	}
	t.Setenv(gitidentity.InputsEnv, list)
	recorder := newGitRecorder(workspace, func(string) {})
	if findings := recorder.statusFindings(); len(findings) != 0 {
		t.Fatalf("inputs as they were copied prompt a commit: %q", findings)
	}
	if err := writeFile(filepath.Join(workspace, "parser.py"), "finished\n"); err != nil {
		t.Fatal(err)
	}
	if findings := recorder.statusFindings(); len(findings) == 0 {
		t.Fatal("the input the run finished was not counted as its uncommitted work")
	}
	tree, err := recorder.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if body, err := recorder.git("cat-file", "-p", tree+":parser.py"); err != nil || body != "finished" {
		t.Fatalf("the candidate's parser.py = %q (%v), want the run's", body, err)
	}
	if _, err := recorder.git("cat-file", "-e", tree+":credentials.json"); err == nil {
		t.Fatal("the input the run left alone entered the candidate")
	}
	blob, err := recorder.git("hash-object", "--", "credentials.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.git("cat-file", "-e", blob); err == nil {
		t.Fatal("the input the run left alone was written as a git blob")
	}
}
