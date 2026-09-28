package session

// A RUN'S WORKING COPY IS ADOPTED, NEVER MADE AGAIN.
//
// The first test here is the one that matters, and it is a test about
// DESTRUCTION rather than about a field. The road that makes a run's copy
// carves the directory it is handed and mints a fresh branch with a random
// suffix; asking it for the same run's copy a second time — which is what a
// naive "carry on where it left off" would do — deletes every step of the work
// it was meant to resume and hands back a branch that looks perfectly healthy.
//
// The rest pin the record that makes adoption possible, and the refusal for a
// run that has no record, which is every run written before this existed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE DESTRUCTION, SHOWN. A run's copy is made, work is done in it, and then
// the copy is asked for a second time exactly as a resume would ask for it. The
// work is gone. This is not a bug being introduced; it is the behaviour of the
// road a resume would otherwise reuse, written down so that nothing reaches for
// it again.
func TestAskingForARunsCopyASecondTimeDestroysTheWorkInIt(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir()}

	first, err := prepareTaskTree(place, repo, "9999aaaa9999aaaa", 7, "port the parser")
	if err != nil {
		t.Fatalf("cutting the run's copy: %v", err)
	}
	work := filepath.Join(first.dir, "the-work.txt")
	if err := os.WriteFile(work, []byte("every step of it\n"), 0o644); err != nil {
		t.Fatalf("writing the run's work: %v", err)
	}

	second, err := prepareTaskTree(place, repo, "9999aaaa9999aaaa", 7, "port the parser")
	if err != nil {
		t.Fatalf("asking for the same run's copy again: %v", err)
	}

	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Skip("this road no longer clears the directory it is handed; the reason for the record has changed and this test should be rewritten rather than deleted")
	}
	// AND THE BRANCH IS A DIFFERENT ONE, which is the half that cannot be
	// recovered. The directory is derived from the run's number, so a second
	// call finds the same path; the branch carries a random suffix, so the name
	// the work is actually on is lost the moment nothing has written it down.
	if first.branch == "" || second.branch == "" {
		t.Fatalf("a copy was cut with no branch at all: %q then %q", first.branch, second.branch)
	}
	if first.branch == second.branch {
		t.Fatalf("both copies named the branch %q, so the branch is derived after all and this whole record is unnecessary", first.branch)
	}
	if first.dir != second.dir {
		t.Fatalf("the two copies stood in %q and %q; the directory is supposed to be derived from the run's own number", first.dir, second.dir)
	}
}

// SO THE COPY IS WRITTEN DOWN, and what is written down is what the run is
// actually working in rather than anything worked out a second time.
func TestARunsCopyIsWrittenDownFromTheTreeItIsWorkingIn(t *testing.T) {
	tree := taskTree{
		dir: "/w/trees/7", root: "/w/repo", branch: "task/port-the-parser-9c1a2f",
		home: "work", homeSha: "abc123", checkBase: "base123", ground: "/w/repo", mode: TaskModeWorktree,
	}
	record := runCopyOf(tree)
	if record == nil {
		t.Fatal("a run working in a real copy wrote nothing down")
	}
	if record.Dir != tree.dir || record.Branch != tree.branch || record.Root != tree.root {
		t.Fatalf("the record does not name the copy: %+v", record)
	}
	if record.Ground != tree.ground || record.Mode != tree.mode {
		t.Fatalf("the record does not say what the copy stands on: %+v", record)
	}
	if record.Home != tree.home || record.HomeSha != tree.homeSha {
		t.Fatalf("the record forgets the checkout the copy was cut from: %+v", record)
	}
	if record.CheckBase != tree.checkBase {
		t.Fatalf("the record forgets the run's commit base: %+v", record)
	}
}

// AND IT SURVIVES THE ROUND TRIP THROUGH THE STORE, which is the only trip that
// matters: the whole point is a process that was not there when the run started.
func TestTheCopySurvivesBeingWrittenDownAndReadBack(t *testing.T) {
	notice := TaskNotice{
		ID: 7, Title: "port the parser", State: TaskRunning,
		Copy: &TaskCopyRecord{Dir: "/w/trees/7", Branch: "task/port-the-parser-9c1a2f", Root: "/w/repo", CheckBase: "base123"},
	}
	restored := runRowNotice(runRowRecord(notice))
	if restored.Copy == nil {
		t.Fatal("the copy did not survive the record, so nothing could be carried on")
	}
	if restored.Copy.Branch != "task/port-the-parser-9c1a2f" {
		t.Fatalf("the branch came back as %q", restored.Copy.Branch)
	}
	if restored.Copy.Dir != "/w/trees/7" {
		t.Fatalf("the directory came back as %q", restored.Copy.Dir)
	}
	if restored.Copy.CheckBase != "base123" {
		t.Fatalf("the commit base came back as %q", restored.Copy.CheckBase)
	}
	// AND THE ROW IS STILL READ AS INTERRUPTED, which is the state this record
	// exists to make actionable rather than merely honest.
	if restored.State != TaskInterrupted {
		t.Fatalf("the restored row reads %s", restored.State)
	}
}

// A RUN WITH NO COPY WRITTEN DOWN IS REFUSED, AND THAT IS THE ANSWER FOREVER.
// Every run row on disk today predates the record, so this is the first thing a
// person will meet. Nothing here may carve a directory, cut a branch or derive
// a path: that is the destructive road above, wearing a helpful face.
func TestARunWithNoCopyWrittenDownIsRefusedRatherThanRebuilt(t *testing.T) {
	for _, record := range []*TaskCopyRecord{nil, {}, {Branch: "task/only-a-branch"}} {
		tree, err := runCopyTree(record, Place{})
		if err == nil {
			t.Fatalf("a run with no copy recorded was handed the tree %+v", tree)
		}
		if tree.dir != "" {
			t.Fatalf("a refusal still named a directory: %q", tree.dir)
		}
		if !strings.Contains(err.Error(), "was not written down") {
			t.Fatalf("the refusal does not say what is missing: %v", err)
		}
	}
}

// AND A COPY THAT IS GONE FROM DISK IS REFUSED TOO, naming the branch, because
// the branch is the one thing left that a person can act on.
func TestACopyThatIsGoneIsRefusedAndNamesTheBranch(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here")
	_, err := runCopyTree(&TaskCopyRecord{Dir: missing, Branch: "task/port-the-parser-9c1a2f"}, Place{})
	if err == nil {
		t.Fatal("a copy that is not on disk was handed back as though it were")
	}
	if !strings.Contains(err.Error(), "task/port-the-parser-9c1a2f") {
		t.Fatalf("the refusal does not name the branch the work is on: %v", err)
	}
}

// AND A COPY THAT IS THERE IS ADOPTED WHOLE, with nothing invented and nothing
// cut.
func TestACopyThatIsThereIsAdoptedAndNothingIsCut(t *testing.T) {
	dir := t.TempDir()
	tree, err := runCopyTree(&TaskCopyRecord{
		Dir: dir, Branch: "task/port-the-parser-9c1a2f", Root: "/w/repo",
		Ground: "/w/repo", Mode: TaskModeWorktree, Home: "work", HomeSha: "abc123", CheckBase: "base123",
	}, Place{Dir: "/w/place"})
	if err != nil {
		t.Fatalf("adopting a copy that is there: %v", err)
	}
	if tree.dir != dir || tree.branch != "task/port-the-parser-9c1a2f" {
		t.Fatalf("the adopted tree is not the one recorded: %+v", tree)
	}
	if tree.ground != "/w/repo" || tree.mode != TaskModeWorktree {
		t.Fatalf("the adopted tree forgot what it stands on: %+v", tree)
	}
	if tree.checkBase != "base123" {
		t.Fatalf("the adopted tree forgot the run's commit base: %+v", tree)
	}
	if !tree.bashBelt {
		t.Fatal("the adopted tree is not a shell worker's, so its landing would expect a write ledger no run fills")
	}
	// AND THE MERGE IS STILL OPEN. A branch that is still out has no outcome
	// yet, and inventing one here would land the work without looking at it.
	if tree.merge != "" {
		t.Fatalf("the adopted tree already claims an outcome: %q", tree.merge)
	}
}

// AND A LATER PUBLISH DOES NOT DROP IT. Every publish after the first replaces
// the row, and only the first one knows where the work is.
func TestALaterPublishKeepsTheCopyTheFirstOneWroteDown(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	g := agent.graph()
	if g == nil {
		t.Fatal("the conversation has no graph to publish into")
	}
	agent.publishRunRow(g, TaskNotice{
		ID: 7, Title: "port the parser", State: TaskRunning,
		Copy: &TaskCopyRecord{Dir: "/w/trees/7", Branch: "task/port-the-parser-9c1a2f"},
	})
	// A second publish that has not thought about the copy at all.
	agent.publishRunRow(g, TaskNotice{ID: 7, Title: "port the parser", State: TaskRunning})

	rows := g.runRows(7)
	if len(rows) != 1 {
		t.Fatalf("%d rows kept for one run", len(rows))
	}
	if rows[0].Copy == nil {
		t.Fatal("the second publish dropped the copy, so the run became one nobody can carry on")
	}
	if rows[0].Copy.Branch != "task/port-the-parser-9c1a2f" {
		t.Fatalf("the carried copy names the branch %q", rows[0].Copy.Branch)
	}
}
