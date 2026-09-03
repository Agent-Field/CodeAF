package session

// CARRY OR REFUSE, PROVED (issue #272).
//
// The counterfactual every test here starts from is the measured one: a person's
// live checkout holds uncommitted work, a task is carved from it — so the seal
// puts the same hunks on the task's branch — and then the task lands. Before
// this file the landing failed with one sentence that named no files at all and
// ended in a bare colon, and the node settled needing a look over a conflict
// nobody could see.

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// THE ISSUE'S FIRST ACCEPTANCE CLAUSE, and it was red on the code this replaces:
// the ground holds an uncommitted `shared.txt`, the node changes the same file,
// and the landing either goes in or NAMES the file and says the ground was
// dirty. What it may never be is a sentence trailing off after a colon.
func TestALandingIntoADirtyGroundNamesTheFilesOrGoesIn(t *testing.T) {
	repo := dirtyRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "abcd1234abcd1234", 11, "touch the shared file")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "shared.txt"),
		"the original line\nand the parent's own\nand what the node wrote\n")

	merge, detail, _ := tree.comeHome("touch the shared file", []string{"shared.txt"})
	if strings.HasSuffix(strings.TrimSpace(detail), ":") {
		t.Fatalf("the report ends in a bare colon and names nothing:\n%s", detail)
	}
	if merge == mergeMerged {
		return
	}
	if !strings.Contains(detail, "shared.txt") {
		t.Fatalf("the landing was refused and did not name the file it was refused over:\n%s", detail)
	}
	if !strings.Contains(detail, "your own uncommitted work") {
		t.Fatalf("the report does not say the ground was dirty:\n%s", detail)
	}
}

// THE CARRY HALF OF THE LAW. Their work is in the way, the branch merges anyway,
// and their work is put back on top of it — with the sentence saying so, nothing
// left in a stash, and not one conflict marker on their branch.
func TestALandingSetsTheirOwnWorkAsideAndPutsItBack(t *testing.T) {
	repo := newTestRepo(t)
	// A file with room in it: the person works at the top and the node works at
	// the bottom, which is the ordinary shape of two people in one repository and
	// the shape a three-way restore can put back.
	writeFile(t, filepath.Join(repo, "long.txt"), "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "the long file")

	place := Place{Dir: t.TempDir(), Workspace: repo}
	tree, err := prepareTaskTree(place, repo, "bcde2345bcde2345", 12, "work at the bottom")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	// The node changes the last line, in a world that is HEAD because the person
	// had not started yet.
	writeFile(t, filepath.Join(tree.dir, "long.txt"), "one\ntwo\nthree\nfour\nfive\nsix\nseven\nthe node's line\n")
	// AND THEN THE PERSON STARTS WORKING, on the first line, uncommitted. This is
	// the state git refuses the merge in.
	writeFile(t, filepath.Join(repo, "long.txt"), "the person's line\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n")

	merge, detail, _ := tree.comeHome("work at the bottom", []string{"long.txt"})
	if merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want the landing to carry their work and go in", merge, detail)
	}
	if !strings.Contains(detail, "set aside") || !strings.Contains(detail, "long.txt") {
		t.Fatalf("the landing moved their work and did not say so:\n%s", detail)
	}
	// BOTH HALVES ARE THERE: the node's work is committed, theirs is still
	// uncommitted and still theirs.
	landed := readFile(t, filepath.Join(repo, "long.txt"))
	if !strings.Contains(landed, "the node's line") {
		t.Fatalf("the node's work did not land:\n%s", landed)
	}
	if !strings.Contains(landed, "the person's line") {
		t.Fatalf("the person's own uncommitted work was not put back:\n%s", landed)
	}
	if strings.Contains(landed, "<<<<<<<") {
		t.Fatalf("a landing wrote conflict markers into the person's file:\n%s", landed)
	}
	if status := gitOut(t, repo, "status", "--porcelain"); !strings.Contains(status, "long.txt") {
		t.Fatalf("their work was committed out from under them:\n%s", status)
	}
	// AND NOTHING IS LEFT LYING ABOUT. A stash entry nobody can account for is
	// the most alarming thing a landing can leave in somebody's repository.
	if list := gitOut(t, repo, "stash", "list"); strings.TrimSpace(list) != "" {
		t.Fatalf("the landing left work in a stash:\n%s", list)
	}
}

// THE REFUSE HALF. When their work cannot go back over the merge, the tree goes
// back to the commit it stood at with their work on top — to the byte — and the
// branch is kept for them to look at.
func TestARefusedLandingLeavesTheGroundExactlyAsItWas(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "cdef3456cdef3456", 13, "rewrite the shared line")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "the node's whole rewrite\n")
	// The person rewrites the same one-line file, uncommitted: there is no way to
	// have both, which is exactly the case this refuses.
	const theirs = "the person's whole rewrite\n"
	writeFile(t, filepath.Join(repo, "shared.txt"), theirs)
	stood := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD"))

	merge, detail, _ := tree.comeHome("rewrite the shared line", []string{"shared.txt"})
	if merge != mergeConflicted {
		t.Fatalf("merge = %q (%s), want the branch kept", merge, detail)
	}
	if !strings.Contains(detail, "shared.txt") || !strings.Contains(detail, "your own uncommitted work") {
		t.Fatalf("the refusal does not name the file or say whose work was in the way:\n%s", detail)
	}
	// EXACTLY AS IT WAS.
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != theirs {
		t.Fatalf("their file reads %q, want their own work untouched", got)
	}
	if now := strings.TrimSpace(gitOut(t, repo, "rev-parse", "HEAD")); now != stood {
		t.Fatalf("the ground stands at %s, want the commit it started at %s", now, stood)
	}
	if list := gitOut(t, repo, "stash", "list"); strings.TrimSpace(list) != "" {
		t.Fatalf("the refusal left their work in a stash:\n%s", list)
	}
	if _, err := git(repo, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err == nil {
		t.Fatal("their checkout was left mid-merge")
	}
	// And the work is still where the report says it is.
	if listed := gitOut(t, repo, "ls-tree", "-r", "--name-only", tree.branch); !strings.Contains(listed, "shared.txt") {
		t.Fatalf("branch %s does not hold the node's work:\n%s", tree.branch, listed)
	}
}

// THE ISSUE'S SECOND ACCEPTANCE CLAUSE: the seal-strip replay will not go, and
// that is JOURNALED and on the node's report rather than swallowed by a silent
// `rebase --abort`. The branch carrying the person's own uncommitted work is a
// fact about their repository, and the report is the only place they can learn
// it.
func TestAReplayThatWouldNotGoIsSaidRatherThanSwallowed(t *testing.T) {
	repo := dirtyRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "def45678def45678", 14, "rewrite the shared file")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.base == "" {
		t.Fatal("the dirty ground was not sealed, so there is no replay to fail")
	}
	// The node rewrites the same lines the parent's uncommitted work changed, so
	// replaying its commit off the seal and onto the ground's HEAD cannot go.
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "a line nobody else has\n")
	if tree.replayOwnWork() != true {
		t.Fatal("the replay went through; this test has nothing to prove")
	}

	agent := &Agent{}
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "rewrite the shared file"}, state: TaskRunning}
	graph.mu.Lock()
	graph.nodes[node.id] = node
	graph.order = append(graph.order, node.id)
	graph.mu.Unlock()

	var log bytes.Buffer
	state := agent.landFinished(node, tree, []string{"shared.txt"},
		"the shared file now reads the one way", "", "", &log)

	if journaled := log.String(); !strings.Contains(journaled, "still carries your own uncommitted work") {
		t.Fatalf("the replay failure was not journaled:\n%s", journaled)
	}
	report, _, _, _ := node.leavings()
	if !strings.Contains(report, "still carries your own uncommitted work") {
		t.Fatalf("the node's report does not say the branch carries their work:\n%s", report)
	}
	if state != TaskUnverified {
		t.Fatalf("state = %q, want a landing that needs a look", state)
	}
}

// AND THE ONE SHAPE WHERE AN EMPTY INDEX IS THE TRUTH: the merge git refused
// before it started. The names are in the BODY of its message, and quoting the
// first line alone is what ended a person's whole report in a bare colon.
func TestTheSentenceNamesTheFilesGitRefusedTheMergeOver(t *testing.T) {
	const refusal = "error: Your local changes to the following files would be overwritten by merge:\n" +
		"\tinternal/session/task_run.go\n" +
		"\tinternal/session/loop.go\n" +
		"Please commit your changes or stash them before you merge.\n" +
		"Aborting\n"

	blocked, untracked := overwrittenPaths(refusal)
	if untracked {
		t.Error("a local-changes refusal was read as an untracked one")
	}
	want := []string{"internal/session/task_run.go", "internal/session/loop.go"}
	if len(blocked) != len(want) {
		t.Fatalf("read %v, want %v", blocked, want)
	}
	for index, path := range want {
		if blocked[index] != path {
			t.Fatalf("read %v, want %v", blocked, want)
		}
	}

	said := conflictSentence("task/land-it", nil, refusal)
	for _, name := range want {
		if !strings.Contains(said, name) {
			t.Fatalf("the sentence does not name %s:\n%s", name, said)
		}
	}
	if strings.HasSuffix(strings.TrimSpace(said), ":") {
		t.Fatalf("the sentence ends in a bare colon:\n%s", said)
	}

	// The untracked refusal is the other heading, and it is read too — what
	// differs is that the landing may not set those files aside.
	const untrackedRefusal = "error: The following untracked working tree files would be overwritten by merge:\n" +
		"\tnotes.md\n" +
		"Please move or remove them before you merge.\n"
	if paths, saidUntracked := overwrittenPaths(untrackedRefusal); !saidUntracked ||
		len(paths) != 1 || paths[0] != "notes.md" {
		t.Fatalf("read %v untracked=%v, want notes.md and the untracked shape", paths, saidUntracked)
	}
}

// AND A REFUSAL GIT SAID NOTHING USEFUL ABOUT IS STILL A WHOLE SENTENCE. There
// is no shape of failure that may leave a person reading a line that trails off.
func TestNoRefusalEverEndsInABareColon(t *testing.T) {
	for _, out := range []string{
		"",
		"error: Your local changes to the following files would be overwritten by merge:",
		"fatal: something git has never said before\n",
		"Auto-merging shared.txt\n",
	} {
		said := conflictSentence("task/land-it", nil, out)
		if strings.HasSuffix(strings.TrimSpace(said), ":") {
			t.Fatalf("git output %q produced a sentence that trails off:\n%s", out, said)
		}
	}
	// A real conflicted index still names its own files, unchanged.
	if said := conflictSentence("task/land-it", []string{"shared.txt"}, ""); !strings.Contains(said, "shared.txt") {
		t.Fatalf("a conflicted index stopped naming its files:\n%s", said)
	}
}

// AND THE CARRY NEVER POPS SOMEBODY ELSE'S STASH. `git stash push` with nothing
// to save prints "No local changes to save" and EXITS ZERO, so a road that
// trusted its exit code would go on to pop whatever entry the person happened to
// have from some other day. The ref before and after is the test that says
// whether anything was actually put away.
func TestTheCarryCanTellWhetherItActuallyStashedAnything(t *testing.T) {
	repo := newTestRepo(t)
	if top := stashTop(repo); top != "" {
		t.Fatalf("a fresh repository answered %q for its stash", top)
	}
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's own line\n")
	mustGit(t, repo, "stash", "push", "-m", "the person's own")
	first := stashTop(repo)
	if first == "" {
		t.Fatal("a repository holding a stash answered that it holds none")
	}
	// A push with nothing to save writes no entry AND DOES NOT FAIL, which is the
	// whole reason this reading exists.
	if _, err := git(repo, "stash", "push", "-m", groundStashMessage("task/nothing")); err != nil {
		t.Fatalf("an empty stash push failed, which is not what git does: %v", err)
	}
	if top := stashTop(repo); top != first {
		t.Fatalf("the top of the stash moved to %q on a push that saved nothing", top)
	}
}
