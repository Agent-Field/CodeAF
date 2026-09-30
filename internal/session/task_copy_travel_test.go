package session

// A TASK'S COPY TRAVELS WITH THE CHAT, WHICHEVER WAY /task MADE IT.
//
// The first tests of internal/taskcopy cut their copies by hand with
// `git worktree add`, which is why nobody saw that a started task is normally a
// fork with a repository of its own: the seal carried worktrees only, and a
// running task's edits never reached the second machine. Nothing here makes a
// copy by hand. Each test asks [prepareTaskTree], the road /task takes, and each
// runs on both rungs that road can land on.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/taskcopy"
)

// copyRoads is the two ways /task makes a copy of a repository: a fork with a
// repository of its own where furrow can fork the ground, and a linked worktree
// where it cannot.
var copyRoads = []struct {
	name string
	rung GroundRung
	prep func(*testing.T)
}{
	{"fork", GroundRungUniverse, installFakeFurrow},
	{"worktree", GroundRungSnapshot, func(*testing.T) {}},
}

// takenTask is what a task left on machine A and what machine B holds after the
// chat took over there.
type takenTask struct {
	a       taskTree
	bDir    string
	headOnA string
}

// takeATask starts a task on a project holding uncommitted work, has the task
// edit, delete and commit, seals the chat on A and takes it over on B, which
// shares nothing with A but what the seal carried: the project (with its .git) and
// the carried copies.
func takeATask(t *testing.T) takenTask {
	t.Helper()
	repo := dirtyRepo(t)
	placeA := Place{Dir: t.TempDir(), Workspace: repo}
	tree, err := prepareTaskTree(placeA, repo, "aaaa9999aaaa9999", 1, "carry the copy")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "committed.txt"), "the task committed this\n")
	mustGit(t, tree.dir, "add", "committed.txt")
	mustGit(t, tree.dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "task work")
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "edited by the task, not committed\n")
	writeFile(t, filepath.Join(tree.dir, "note.txt"), "a file the task made\n")
	if err := os.Remove(filepath.Join(tree.dir, "invented.txt")); err != nil {
		t.Fatal(err)
	}
	if err := (taskcopy.Carry{}).Compose(cell.Cell{ID: "c", Root: placeA.Dir}); err != nil {
		t.Fatalf("compose: %v", err)
	}

	headOnA := strings.TrimSpace(gitOut(t, tree.dir, "rev-parse", "HEAD"))
	// B shares nothing with A, so A's copy is gone from the disk B sees. Leaving
	// it would keep its registration live and hide the very case a takeover is.
	if err := os.RemoveAll(tree.dir); err != nil {
		t.Fatal(err)
	}

	projectB, placeB := t.TempDir(), Place{Dir: t.TempDir()}
	copyFolder(t, repo, projectB)
	copyFolder(t, filepath.Join(placeA.Dir, cell.StateDir), filepath.Join(placeB.Dir, cell.StateDir))
	restored, err := taskcopy.Carry{Cutter: TaskCopyCutter{}}.Restore(cell.Cell{ID: "c", Root: placeB.Dir}, projectB)
	if err != nil || len(restored) != 1 {
		t.Fatalf("restore = %v, %v; want the one copy back", restored, err)
	}
	return takenTask{a: tree, bDir: filepath.Join(placeB.Trees(), "1"), headOnA: headOnA}
}

// copyFolder is a folder arriving on another machine, hidden files included.
func copyFolder(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-a", from+"/.", to+"/").CombinedOutput(); err != nil {
		t.Fatalf("cp %s: %v\n%s", from, err, out)
	}
}

func TestATaskCopyMadeByTheRealRoadArrivesWithItsEdits(t *testing.T) {
	for _, road := range copyRoads {
		t.Run(road.name, func(t *testing.T) {
			road.prep(t)
			taken := takeATask(t)
			if taken.a.rung != road.rung {
				t.Fatalf("/task made this copy on the %q rung, want %q", taken.a.rung, road.rung)
			}
			for path, want := range map[string]string{
				"shared.txt":    "edited by the task, not committed\n",
				"note.txt":      "a file the task made\n",
				"committed.txt": "the task committed this\n",
			} {
				if got := readFile(t, filepath.Join(taken.bDir, path)); got != want {
					t.Errorf("B's %s = %q, want %q", path, got, want)
				}
			}
			if _, err := os.Stat(filepath.Join(taken.bDir, "invented.txt")); err == nil {
				t.Error("the file the task deleted is back on B")
			}
		})
	}
}

func TestATaskCopyMadeByTheRealRoadArrivesOnItsBranchAtItsCommit(t *testing.T) {
	for _, road := range copyRoads {
		t.Run(road.name, func(t *testing.T) {
			road.prep(t)
			taken := takeATask(t)
			if got := strings.TrimSpace(gitOut(t, taken.bDir, "rev-parse", "--abbrev-ref", "HEAD")); got != taken.a.branch {
				t.Errorf("B's copy is on %q, want %q", got, taken.a.branch)
			}
			if got := strings.TrimSpace(gitOut(t, taken.bDir, "rev-parse", "HEAD")); got != taken.headOnA {
				t.Errorf("B's copy is at %s, want the commit A's copy was at, %s", got, taken.headOnA)
			}
		})
	}
}
