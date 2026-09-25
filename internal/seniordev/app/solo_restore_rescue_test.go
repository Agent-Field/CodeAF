//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Edits and new files saved after a checkpoint cannot disappear when the
// submitted candidate or an earlier coherent checkpoint is restored. The
// rescue lives in codeaf's state root and its path reaches the ending.
func TestRestoreSetsAsideLaterEditsAndNamesTheirLocation(t *testing.T) {
	for _, plain := range []bool{false, true} {
		name := "git"
		if plain {
			name = "plain"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("CODEAF_HOME", root)
			var workspace string
			if plain {
				workspace = t.TempDir()
				if err := writeFile(filepath.Join(workspace, "README.md"), "base\n"); err != nil {
					t.Fatal(err)
				}
			} else {
				workspace, _ = guardWorkspace(t)
			}
			args := cliArgs{InPlace: plain}
			if err := writeFile(filepath.Join(workspace, ".gitignore"), ".env\n"); err != nil {
				t.Fatal(err)
			}
			if !plain {
				if err := gitRun(workspace, "add", ".gitignore"); err != nil {
					t.Fatal(err)
				}
				if err := gitRun(workspace, "commit", "-m", "ignore secret"); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeFile(filepath.Join(workspace, ".env"), "ignored secret\n"); err != nil {
				t.Fatal(err)
			}
			runner := newPipeline(args, workspace, pipelineDeps{Events: newEventWriter(discardWriter{}), Notes: discardWriter{}})
			t.Cleanup(runner.runtime.Close)
			wanted, err := runner.currentTreeSHA()
			if err != nil {
				t.Fatal(err)
			}
			checkpoint, err := runner.soloRecordTree(wanted, "candidate")
			if err != nil {
				t.Fatal(err)
			}
			if err := writeFile(filepath.Join(workspace, "README.md"), "person's edit\n"); err != nil {
				t.Fatal(err)
			}
			if err := writeFile(filepath.Join(workspace, "notes.txt"), "person's note\n"); err != nil {
				t.Fatal(err)
			}
			if err := writeFile(filepath.Join(workspace, " leading.txt"), "spaced name\n"); err != nil {
				t.Fatal(err)
			}
			if err := runner.soloRestoreTree(checkpoint, wanted); err != nil {
				t.Fatal(err)
			}
			rescueRoot := filepath.Join(root, "v3", "carried", "senior-dev", "rescued")
			rescues, err := os.ReadDir(rescueRoot)
			if err != nil || len(rescues) != 1 {
				t.Fatalf("rescue folders = %v, %v", rescues, err)
			}
			rescue := filepath.Join(rescueRoot, rescues[0].Name())
			for file, want := range map[string]string{"README.md": "person's edit\n", "notes.txt": "person's note\n", " leading.txt": "spaced name\n"} {
				body, err := os.ReadFile(filepath.Join(rescue, file))
				if err != nil || string(body) != want {
					t.Fatalf("rescued %s = %q, %v", file, body, err)
				}
			}
			if body, err := os.ReadFile(filepath.Join(workspace, "README.md")); err != nil || string(body) != "base\n" {
				t.Fatalf("candidate was not restored: %q, %v", body, err)
			}
			if _, err := os.Stat(filepath.Join(workspace, "notes.txt")); !os.IsNotExist(err) {
				t.Fatalf("later file remains in candidate: %v", err)
			}
			if body, err := os.ReadFile(filepath.Join(workspace, ".env")); err != nil || string(body) != "ignored secret\n" {
				t.Fatalf("ignored file was touched: %q, %v", body, err)
			}
			outcome := soloOutcome{Status: "pass"}
			runner.soloTerminal(&outcome, "submitted")
			ending := endingOf(pipelineResult{Status: "pass", Terminal: outcome.TerminalData})
			if !strings.Contains(ending.Message, "Files that changed in the folder before senior-dev restored its checkpoint were set aside in "+rescue) {
				t.Fatalf("ending did not name rescue: %q", ending.Message)
			}
		})
	}
}

// The frozen candidate cannot acquire a secret that was ignored when the
// run began merely because the run rewrote .gitignore before submitting.
func TestGitCandidateKeepsStartTimeIgnoredFilesOutOfItsTree(t *testing.T) {
	workspace, _ := guardWorkspace(t)
	if err := writeFile(filepath.Join(workspace, ".gitignore"), ".env\n"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", ".gitignore"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "ignore secret"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, ".env"), "PERSON_SECRET=private\n"); err != nil {
		t.Fatal(err)
	}
	ignored := filepath.Join(t.TempDir(), "ignored-at-start")
	if err := os.WriteFile(ignored, []byte(".env\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SENIOR_DEV_IGNORED_AT_START", ignored)
	if err := writeFile(filepath.Join(workspace, ".gitignore"), "# changed\n"); err != nil {
		t.Fatal(err)
	}
	recorder := newGitRecorder(workspace, func(string) {})
	tree, err := recorder.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.git("cat-file", "-e", tree+":.env"); err == nil {
		t.Fatal("the candidate tree includes a secret ignored at the start")
	}
	if _, err := os.Stat(filepath.Join(workspace, ".env")); err != nil {
		t.Fatalf("the secret was removed: %v", err)
	}
	handle, err := recorder.Record(tree, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "README.md"), "later edit\n"); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Restore(handle, tree); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(workspace, ".env")); err != nil || string(body) != "PERSON_SECRET=private\n" {
		t.Fatalf("restore touched an initially ignored secret: %q, %v", body, err)
	}
}
