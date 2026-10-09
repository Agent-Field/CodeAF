package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// startLoop opens an engine over a workspace and session folder that the caller
// owns, so a second Loopback over the same two directories is a "reload".
func startLoop(t *testing.T, workspace, folder string) *Loop {
	t.Helper()
	place := session.Place{Dir: folder, Workspace: workspace}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, Workspace: workspace, Place: place}, nil
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// THE DIFF IS MEASURED FROM WHERE THE CONVERSATION STARTED: a commit made after
// the start still shows in the diff, and the start survives a reload.
func TestDiffBaseIsTheStartCommit(t *testing.T) {
	loop, workspace := repoLoop(t)
	folder := filepath.Join(filepath.Dir(workspace), "session")
	first := gitOut(t, workspace, "rev-parse", "HEAD")

	// Before anything is recorded the base is HEAD.
	if c, err := loop.Client.DiffChanges(nil); err != nil || c.Base == nil || c.Base.Kind != "head" || c.Base.StartGone {
		t.Fatalf("no record: %+v %v", c, err)
	}
	st, err := loop.Client.DiffStart()
	if err != nil || !st.Git || st.Commit != first {
		t.Fatalf("record: %+v %v", st, err)
	}

	// Commit a change after the start, then add an uncommitted file.
	write(t, filepath.Join(workspace, "lexer.go"), "a\nb\nc\nX\ne\nf\ng\nh\n")
	gitIn(t, workspace, "commit", "-q", "-am", "later")
	write(t, filepath.Join(workspace, "new.txt"), "one\n")

	// A second Diff.Start never moves the start.
	if again, _ := loop.Client.DiffStart(); again.Commit != first {
		t.Errorf("start moved: %+v", again)
	}
	c, err := loop.Client.DiffChanges(nil)
	if err != nil || c.Base == nil || c.Base.Kind != "start" || !strings.HasPrefix(first, c.Base.Sha) {
		t.Fatalf("base: %+v %v", c, err)
	}
	if len(c.Files) != 2 || c.Files[0].Path != "lexer.go" || c.Files[0].Added != 1 || c.Files[0].Deleted != 1 || c.Files[1].Status != "untracked" {
		t.Errorf("committed change missing from the list: %+v", c.Files)
	}
	d, err := loop.Client.DiffFile("lexer.go")
	if err != nil || d.Base == nil || d.Base.Kind != "start" || d.Status != "modified" || d.Added != 1 || d.Deleted != 1 {
		t.Errorf("file diff: %+v %v", d, err)
	}

	// Reload: a fresh engine over the same folder reads the start back.
	reloaded := startLoop(t, workspace, folder)
	if c, err := reloaded.Client.DiffChanges(nil); err != nil || c.Base == nil || c.Base.Kind != "start" || len(c.Files) != 2 {
		t.Errorf("reload lost the start: %+v %v", c, err)
	}
}

// AN UNBORN BRANCH RECORDS NOTHING, and the base stays head until there is a commit.
func TestDiffStartUnborn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	loop, workspace, folder := browseLoop(t)
	gitIn(t, workspace, "init", "-q")
	write(t, filepath.Join(workspace, "a.txt"), "x\n")
	st, err := loop.Client.DiffStart()
	if err != nil || !st.Git || st.Commit != "" {
		t.Fatalf("unborn: %+v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(folder, "diffbase.json")); err == nil {
		t.Error("an unborn branch wrote a record")
	}
	if c, _ := loop.Client.DiffChanges(nil); c.Base == nil || c.Base.Kind != "head" || c.Base.StartGone || len(c.Files) != 1 {
		t.Errorf("unborn changes: %+v", c)
	}
}

// A START THAT HISTORY NO LONGER REACHES FALLS BACK TO HEAD and says so.
func TestDiffStartUnreachableFallsBack(t *testing.T) {
	loop, workspace := repoLoop(t)
	write(t, filepath.Join(workspace, "lexer.go"), "changed\n")
	gitIn(t, workspace, "commit", "-q", "-am", "second")
	if _, err := loop.Client.DiffStart(); err != nil {
		t.Fatal(err)
	}
	// Rewrite history so the recorded commit is no longer an ancestor.
	gitIn(t, workspace, "checkout", "-q", "--orphan", "fresh")
	gitIn(t, workspace, "commit", "-q", "-m", "rewritten", "--allow-empty")
	write(t, filepath.Join(workspace, "extra.txt"), "x\n")
	c, err := loop.Client.DiffChanges(nil)
	if err != nil || c.Base == nil || c.Base.Kind != "head" || c.Base.Sha == "" || !c.Base.StartGone {
		t.Fatalf("fallback: %+v %v", c, err)
	}
	d, err := loop.Client.DiffFile("extra.txt")
	if err != nil || d.Base == nil || d.Base.Kind != "head" || d.Status != "untracked" || !d.Base.StartGone {
		t.Errorf("file fallback: %+v %v", d, err)
	}
}

func TestDiffStartOutsideGit(t *testing.T) {
	loop, _, _ := browseLoop(t)
	if st, err := loop.Client.DiffStart(); err != nil || st.Git || st.Commit != "" {
		t.Errorf("non-git: %+v %v", st, err)
	}
}
