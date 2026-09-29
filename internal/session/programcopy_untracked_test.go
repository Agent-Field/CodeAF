package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A LOCAL INPUT IS AVAILABLE WITHOUT BECOMING HISTORY. Both an ordinary
// ending and recovery from a closed process use the persisted protection
// list, while staged additions and the program's new files still ship.
func TestProgramUntrackedInputsStayOutOfSnapshotsAndEndings(t *testing.T) {
	for _, crashed := range []bool{false, true} {
		name := "finished"
		if crashed {
			name = "recovered"
		}
		t.Run(name, func(t *testing.T) {
			repo := newTestRepo(t)
			writeFile(t, filepath.Join(repo, "shared.txt"), "tracked edit\n")
			writeFile(t, filepath.Join(repo, "staged.go"), "package staged\n")
			mustGit(t, repo, "add", "staged.go")
			inputs := []string{"credentials.json", "local/large.bin", "local/odd\n[1].txt"}
			for _, path := range inputs {
				writeFile(t, filepath.Join(repo, path), "local input: "+path+strings.Repeat("x", 65536))
			}
			status := gitOut(t, repo, "status", "--porcelain", "-z")
			folder := prepareIn(t, testPrograms("fake")[0], repo, "Use local inputs")
			for _, path := range inputs {
				if got := readFile(t, filepath.Join(folder.Dir, path)); got != readFile(t, filepath.Join(repo, path)) {
					t.Fatalf("%q was not copied intact", path)
				}
				blob := strings.TrimSpace(gitOut(t, repo, "hash-object", "--", path))
				if _, err := git(repo, "cat-file", "-e", blob); err == nil {
					t.Fatalf("local input %q was written into git's object store", path)
				}
				if !folder.excludedFromCommit(path) {
					t.Fatalf("local input %q is absent from the finishing protection list", path)
				}
			}
			if !strings.Contains(folder.LeftBehindWords(), "copied as local inputs, not committed") {
				t.Fatalf("receipt misstates where the inputs are: %s", folder.LeftBehindWords())
			}
			if tree := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); tree != "shared.txt\nstaged.go\n" {
				t.Fatalf("initial branch contains %q", tree)
			}
			writeFile(t, filepath.Join(folder.Dir, "credentials.json"), "changed local input\n")
			writeFile(t, filepath.Join(folder.Dir, "made.go"), "package made\n")
			if crashed {
				folder.release()
				sweepProgramCopies(repo)
			} else {
				folder.Finish("done")
			}
			if tree := gitOut(t, repo, "ls-tree", "-r", "--name-only", folder.Branch); tree != "made.go\nshared.txt\nstaged.go\n" {
				t.Fatalf("finished branch contains %q", tree)
			}
			if got := gitOut(t, repo, "status", "--porcelain", "-z"); got != status {
				t.Fatalf("the person's index or files changed: %q, want %q", got, status)
			}
			if _, err := os.Stat(folder.Dir); !os.IsNotExist(err) {
				t.Fatalf("the finished copy remains: %v", err)
			}
		})
	}
}

func TestOnlyUntrackedInputsMakeNoSnapshotCommit(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "credentials.json"), "local input\n")
	folder := prepareIn(t, testPrograms("fake")[0], repo, "Read the input")
	if folder.Snapshot != "" || branchCommit(repo, folder.Branch) != folder.Start {
		t.Fatal("untracked inputs alone made a snapshot commit")
	}
	if got := readFile(t, filepath.Join(folder.Dir, "credentials.json")); got != "local input\n" {
		t.Fatalf("local input = %q", got)
	}
	if end := folder.Finish("done"); end.Kept || branchCommit(repo, folder.Branch) != folder.Start {
		t.Fatalf("an unchanged input became a finishing commit: %+v", end)
	}
}
