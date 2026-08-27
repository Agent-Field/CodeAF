package session

// WHERE THE WORK HAPPENS, PROVED (docs/CHAT-V3.md, Decision 26): a session with
// a folder of its own keeps its worktrees, its lock and its node journals
// inside it — or under the state root — and NOTHING of ours is left in the
// person's repository. The zero Place is the legacy layout, unchanged, so the
// move can land without a flag day.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// A node's worktree lands in the session's own trees/, and git knows about it
// there — which is the whole claim the move rests on.
func TestAWorktreeLandsInsideTheSessionFolder(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}

	tree, err := prepareTaskTree(place, repo, "aaaa1111aaaa1111", 7, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if want := filepath.Join(place.Trees(), "7"); tree.dir != want {
		t.Fatalf("the worktree is at %q, want %q", tree.dir, want)
	}
	if info, err := os.Stat(tree.dir); err != nil || !info.IsDir() {
		t.Fatalf("nothing is at %s: %v", tree.dir, err)
	}
	if list := gitOut(t, repo, "worktree", "list"); !strings.Contains(list, tree.dir) {
		t.Fatalf("git does not know the worktree:\n%s", list)
	}
	// NOTHING OF OURS IN THEIR REPOSITORY. The old directory is not created, not
	// even empty.
	if _, err := os.Stat(filepath.Join(repo, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the repository was littered anyway (%v)", err)
	}
	// And the branch law is untouched: a branch off HEAD that merges home.
	if !strings.HasPrefix(tree.branch, "task/") {
		t.Fatalf("branch = %q, want a task branch", tree.branch)
	}
	writeFile(t, filepath.Join(tree.dir, "done.txt"), "all of it\n")
	if merge, detail := tree.comeHome("do the thing", []string{"done.txt"}); merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want it to come home", merge, detail)
	}
	if _, err := os.Stat(filepath.Join(repo, "done.txt")); err != nil {
		t.Fatalf("the work did not land in the person's tree: %v", err)
	}
}

// An owned work/ repository is bookkeeping, never the project a task branches
// from. With no anchor the task asks for one; non-code work may explicitly stay
// in place, and neither road registers an empty worktree.
func TestAnOwnedScratchWorkspaceIsNeverATaskBranchSource(t *testing.T) {
	work := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: work, Owned: true}

	if _, err := prepareTaskTreeAt(place, work, "owned", 1, "change the code", ""); err == nil ||
		!strings.Contains(err.Error(), "/workspace <path>") {
		t.Fatalf("default owned task error = %v, want the one-line anchor request", err)
	}
	if list := gitOut(t, work, "worktree", "list"); strings.Contains(list, place.Trees()) {
		t.Fatalf("the scratch repository gained a task worktree:\n%s", list)
	}
	tree, err := prepareTaskTreeAt(place, work, "owned", 2, "write notes", "in place")
	if err != nil {
		t.Fatal(err)
	}
	if tree.dir != work || tree.merge != mergeInPlace {
		t.Fatalf("in-place tree = %+v, want the owned workspace itself", tree)
	}
}

func TestAnExplicitTaskPlaceIsTheWorkersExactDirectory(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	named := t.TempDir()
	tree, err := prepareTaskTreeAt(place, repo, "named", 3, "write there", named)
	if err != nil {
		t.Fatal(err)
	}
	if tree.dir != named || tree.merge != mergeInPlace || tree.branch != "" {
		t.Fatalf("explicit tree = %+v, want exact in-place directory %s", tree, named)
	}
	if _, err := os.Stat(filepath.Join(place.Trees(), "3")); !os.IsNotExist(err) {
		t.Fatalf("an explicit place also created a task-folder worktree (%v)", err)
	}
}

func TestAFailedTaskKeepsItsFolderWithoutAWorktreeRegistration(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	tree, err := prepareTaskTree(place, repo, "failed", 4, "unfinished change")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, "notes.txt"), "unfinished\n")
	if merge, _ := keptWork(tree, "unfinished change", []string{"notes.txt"}); merge != mergeAborted {
		t.Fatalf("merge = %q", merge)
	}
	if list := gitOut(t, repo, "worktree", "list"); strings.Contains(list, tree.dir) {
		t.Fatalf("failed task remained registered:\n%s", list)
	}
	if _, err := os.Stat(filepath.Join(tree.dir, "notes.txt")); err != nil {
		t.Fatalf("the kept task folder lost its leavings: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree.dir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("the kept folder still claims to be a worktree (%v)", err)
	}
	if left := leftBehind(tree.dir); len(left) != 0 {
		t.Fatalf("the empty leavings snapshot became %v after unregistering", left)
	}
}

// The zero Place is the legacy layout and it has not moved: a caller that has
// not adopted the folder gets exactly what it got yesterday.
func TestTheLegacyWorktreeStaysUnderTheRepository(t *testing.T) {
	// The repository root is an identity, so the containment law compares the
	// two canonical paths production records rather than two aliases for it.
	repo := canonicalPath(newTestRepo(t))
	tree, err := prepareTaskTree(Place{}, repo, "bbbb2222bbbb2222", 3, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	want := filepath.Join(repo, filepath.FromSlash(tasksDirName), "bbbb2222bbbb2222", "3")
	if tree.dir != want {
		t.Fatalf("the worktree is at %q, want %q", tree.dir, want)
	}
}

// The canonical spelling is chosen before the task directory exists. This is
// the cross-platform form of macOS's /var → /private/var path: the explicit
// symlink makes every builder prove the same law.
func TestWorktreePathsResolveSymlinkedParentsBeforeRecording(t *testing.T) {
	repo := newTestRepo(t)
	alias := filepath.Join(t.TempDir(), "repository")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}

	tree, err := prepareTaskTree(Place{}, alias, "cccc3333cccc3333", 4, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	root := canonicalPath(repo)
	want := filepath.Join(root, filepath.FromSlash(tasksDirName), "cccc3333cccc3333", "4")
	if tree.root != root || tree.dir != want {
		t.Fatalf("the canonical tree is root %q at %q, want root %q at %q", tree.root, tree.dir, root, want)
	}
	if !treeCovers(root, tree.dir) {
		t.Fatalf("the legacy worktree escaped its repository: root %q, tree %q", root, tree.dir)
	}

	realPlace := t.TempDir()
	placeAlias := filepath.Join(t.TempDir(), "conversation")
	if err := os.Symlink(realPlace, placeAlias); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: filepath.Join(placeAlias, "not-made-yet")}
	if got, want := place.Trees(), filepath.Join(canonicalPath(realPlace), "not-made-yet", placeTrees); got != want {
		t.Fatalf("Trees() = %q, want canonical missing path %q", got, want)
	}
}

// The cross-process lock moves out of the person's repository with the
// worktrees, and it is keyed on the REPOSITORY: two sessions working over one
// checkout must meet on one file or the lock serializes nothing.
func TestTheGitRootLockLivesUnderTheStateRoot(t *testing.T) {
	writtenRoot := t.TempDir()
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)

	first := Place{Dir: filepath.Join(state, "v3", "projects", "-repo", "aaaa")}
	second := Place{Dir: filepath.Join(state, "v3", "projects", "-repo", "bbbb")}

	root := canonicalPath(writtenRoot)
	digest := sha256.Sum256([]byte(filepath.Clean(root)))
	want := filepath.Join(state, "v3", "locks", hex.EncodeToString(digest[:])[:gitRootLockStem]+".lock")

	release := lockGitRoot(first, writtenRoot)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("no lock at %s: %v", want, err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(tasksDirName), gitRootLockName)); !os.IsNotExist(err) {
		t.Fatalf("the repository still holds a lock file (%v)", err)
	}
	// The other window derives the same file from the same repository, which is
	// the only reason the flock under it means anything.
	other := otherWindowsLock(t, second, root)
	defer other.Close()
	if other.Name() != want {
		t.Fatalf("the second session locks %q, want %q", other.Name(), want)
	}
	release()

	// The locks directory is ours and nobody else's business.
	if info, err := os.Stat(filepath.Dir(want)); err != nil {
		t.Fatalf("stat the locks directory: %v", err)
	} else if mode := info.Mode().Perm(); mode != 0o700 {
		t.Fatalf("the locks directory is %o, want 700", mode)
	}
}

// A node's transcript lives beside the conversation that commissioned it, and
// the parallel tree under the state root is the legacy answer — which now
// finally follows AFORGE_HOME, the direct os.UserHomeDir call being the reason
// it did not.
func TestANodeJournalFollowsItsSession(t *testing.T) {
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)
	place := Place{Dir: t.TempDir()}

	journal := taskJournalPath(place, "aaaa1111aaaa1111", 4, "")
	if got := filepath.Dir(journal); got != place.NodeJournals() {
		t.Fatalf("the node journal is in %q, want %q", got, place.NodeJournals())
	}
	if name := filepath.Base(journal); !strings.HasSuffix(name, "_4.jsonl") {
		t.Fatalf("the journal is named %q, want the stamped node name", name)
	}
	if audit := taskJournalPath(place, "aaaa1111aaaa1111", 4, "-audit-9c1a2f"); !strings.HasSuffix(audit, "_4-audit-9c1a2f.jsonl") {
		t.Fatalf("the audit journal is named %q", filepath.Base(audit))
	}

	legacy := taskJournalPath(Place{}, "aaaa1111aaaa1111", 4, "")
	if want := filepath.Join(state, "v3", "tasks", "aaaa1111aaaa1111"); filepath.Dir(legacy) != want {
		t.Fatalf("the legacy journal is in %q, want %q", filepath.Dir(legacy), want)
	}
}

// AND A PIECE OF THAT NODE'S WORK IS IN THE SAME FOLDER AS THE NODE.
//
// Every path a running node needs is arithmetic on one Place, and the Place used
// to be read off the agent that OWNED the node — which is the conversation for
// the work it proposed itself and the parent's WORKER for a part it handed
// further out. A worker carries no Place (it is not a session), so a part's
// worktree landed in the person's own repository under the legacy layout while
// its parent's sat inside the session folder, and one family took the git root's
// lock on two different files. [Agent.familyPlace] is the one answer both halves
// now ask.
func TestAPieceOfATasksWorkKeepsToTheSameSessionFolder(t *testing.T) {
	repo := newTestRepo(t)
	state := t.TempDir()
	t.Setenv(home.EnvVar, state)
	place := Place{Dir: filepath.Join(state, "v3", "projects", "-repo", "aaaa1111aaaa1111"), Workspace: repo}

	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = repo
		config.Place = place
	})
	// Built by the production constructor, in the repository the node would be
	// working in — everything below hangs off what THAT agent knows.
	worker, node := workerFor(t, session, taskSpec{
		title: "the whole job", brief: "b", acceptance: "a", depth: 1,
	})
	if worker.config.Place.Dir != "" {
		t.Fatal("a worker was made into a second session; the point of this test is that it is not one")
	}
	piece := pieceOf(t, session.graph(), node, worker, "one part of it")

	// The two lines [Agent.workTaskNode] runs for a node it is about to start.
	tree, err := prepareTaskTree(worker.familyPlace(piece), repo, worker.journalID(), piece.id, piece.title())
	if err != nil {
		t.Fatalf("prepareTaskTree for a piece: %v", err)
	}
	if want := filepath.Join(place.Trees(), strconv.FormatUint(piece.id, 10)); tree.dir != want {
		t.Fatalf("the piece works in %q, want %q — inside the session that commissioned the family", tree.dir, want)
	}
	if _, err := os.Stat(filepath.Join(repo, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("a piece littered the person's repository anyway (%v)", err)
	}
	// AND ITS TRANSCRIPT SITS BESIDE ITS PARENT'S, which is the same question
	// asked of the same Place through the real constructor.
	part, err := worker.newTaskAgent(context.Background(), tree.dir, piece, "")
	if err != nil {
		t.Fatalf("the production constructor refused to build a piece's worker: %v", err)
	}
	t.Cleanup(func() { _ = part.Close() })
	if got := filepath.Dir(part.config.SessionFile); got != place.NodeJournals() {
		t.Fatalf("the piece's transcript is in %q, want %q", got, place.NodeJournals())
	}
}
