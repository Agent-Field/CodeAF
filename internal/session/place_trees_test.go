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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
	if merge, detail, _, _ := tree.comeHome("do the thing", []string{"done.txt"}); merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want it to come home", merge, detail)
	}
	if _, err := os.Stat(filepath.Join(repo, "done.txt")); err != nil {
		t.Fatalf("the work did not land in the person's tree: %v", err)
	}
}

// A TASK IN A CONVERSATION WITH NO PROJECT JUST WORKS. The conversation's own
// work/ IS a repository — the door makes one with a first commit for exactly
// this reason — so a task with nothing named takes the ordinary road: a worktree
// in the session's trees/, a branch off work/'s HEAD, and a merge home. It once
// refused every such task with "this task needs a project", which turned people
// away from work that needed no repository at all.
func TestATaskInAConversationWithNoProjectBranchesFromItsOwnWorkspace(t *testing.T) {
	place, work := newOwnedPlace(t)

	tree, err := prepareTaskTreeAt(context.Background(), place, work, "owned", 1, "file the issue", "", "")
	if err != nil {
		t.Fatalf("prepareTaskTreeAt: %v", err)
	}
	if want := filepath.Join(place.Trees(), "1"); tree.dir != want {
		t.Fatalf("the worktree is at %q, want %q", tree.dir, want)
	}
	if tree.root != canonicalPath(work) || !strings.HasPrefix(tree.branch, "task/") {
		t.Fatalf("owned tree = %+v, want a task branch off the conversation's own repository", tree)
	}
	if list := gitOut(t, work, "worktree", "list"); !strings.Contains(list, tree.dir) {
		t.Fatalf("git does not know the worktree:\n%s", list)
	}
	// And it comes home, which is the half that makes the branch worth cutting.
	writeFile(t, filepath.Join(tree.dir, "issue.md"), "filed\n")
	if merge, detail, _, _ := tree.comeHome("file the issue", []string{"issue.md"}); merge != mergeMerged {
		t.Fatalf("merge = %q (%s), want it to come home", merge, detail)
	}
	if _, err := os.Stat(filepath.Join(work, "issue.md")); err != nil {
		t.Fatalf("the work did not land in the conversation's own workspace: %v", err)
	}

	// C1: A model-authored in-place request inside that repository takes the same
	// branch road. Owned changes still merge below because this is aforge's own
	// working repository rather than the person's checkout.
	inPlace, err := prepareTaskTreeAt(context.Background(), place, work, "owned", 2, "write notes", "in place", "")
	if err != nil {
		t.Fatal(err)
	}
	if inPlace.dir == work || inPlace.root != canonicalPath(work) || !strings.HasPrefix(inPlace.branch, "task/") {
		t.Fatalf("in-place tree = %+v, want a branch of the owned workspace", inPlace)
	}
	inPlace.releaseKept()
}

// AN OWNED WORKSPACE THAT IS NOT A REPOSITORY STILL HAS SOMEWHERE TO STAND. It
// is what an older build left behind, and what a machine with no git leaves
// today, because the door treats a failed init as a loss of undo history rather
// than a refusal to open. The honest answer is the one every non-repository
// gets: run in place and say so — never a sentence about needing a project.
func TestATaskInAnUninitialisedOwnedWorkspaceRunsInPlace(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: dir, Workspace: work, Owned: true}

	tree, err := prepareTaskTreeAt(context.Background(), place, work, "owned", 1, "file the issue", "", "")
	if err != nil {
		t.Fatalf("prepareTaskTreeAt: %v", err)
	}
	if tree.dir != work || tree.merge != mergeInPlace || tree.branch != "" {
		t.Fatalf("tree = %+v, want the workspace itself with nothing to merge", tree)
	}
}

// WHAT THE WORKER IS TOLD. A directory holding nothing because the conversation
// never had a project, and a checkout that failed, look identical from inside —
// so the prompt says which this is, and says it of the task folder as well as of
// the workspace itself. A place the person named is somewhere they chose and is
// never described this way.
func TestAWorkerInTheConversationsOwnSpaceIsToldThereIsNoProject(t *testing.T) {
	place, work := newOwnedPlace(t)
	tree, err := prepareTaskTreeAt(context.Background(), place, work, "owned", 1, "file the issue", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{work, tree.dir} {
		if !standingInOwnSpace(place, dir) {
			t.Fatalf("%s was not read as the conversation's own space", dir)
		}
	}
	named := t.TempDir()
	if standingInOwnSpace(place, named) {
		t.Fatalf("a place the person named (%s) was read as the conversation's own space", named)
	}
	if standingInOwnSpace(Place{Dir: place.Dir, Workspace: work}, work) {
		t.Fatal("a borrowed conversation was read as having no project")
	}

	prompt := renderSystemAt(Config{Workspace: tree.dir, ownSpace: true}, time.Now())
	const line = "- There is no project here: this is the conversation's own space, and it holds only what this conversation has put there."
	if !strings.Contains(prompt, line) {
		t.Fatalf("the worker's prompt does not say %q", line)
	}
	if strings.Contains(renderSystemAt(Config{Workspace: named}, time.Now()), line) {
		t.Fatal("a worker in a project was told there is no project here")
	}
}

// newOwnedPlace is the layout a conversation opened outside any project gets:
// a session folder whose work/ the door has made into a repository with a first
// commit (cmd/aforge's prepareOwnedWorkspace).
func newOwnedPlace(t *testing.T) (Place, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "init")
	mustGit(t, work, "-c", "user.name=aforge", "-c", "user.email=aforge@localhost",
		"commit", "--allow-empty", "-m", "session opened")
	return Place{Dir: dir, Workspace: work, Owned: true}, work
}

func TestAnExplicitTaskPlaceIsTheWorkersExactDirectory(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	named := t.TempDir()
	tree, err := prepareTaskTreeAt(context.Background(), place, repo, "named", 3, "write there", named, "")
	if err != nil {
		t.Fatal(err)
	}
	// THE EXACT DIRECTORY, IN THE ONE SPELLING. What this test is about is that
	// the worker stands in the folder somebody named rather than in a task
	// folder; the spelling of it is [canonicalPath]'s, because that is what every
	// other directory this engine compares it against is spelled as.
	if tree.dir != canonicalPath(named) || tree.merge != mergeInPlace || tree.branch != "" {
		t.Fatalf("explicit tree = %+v, want exact in-place directory %s", tree, canonicalPath(named))
	}
	if _, err := os.Stat(filepath.Join(place.Trees(), "3")); !os.IsNotExist(err) {
		t.Fatalf("an explicit place also created a task-folder worktree (%v)", err)
	}
}

// NAMING A PLACE THAT IS NOT THERE YET IS AN ERRAND, NOT A MISTAKE. `use
// ~/new-scratch` used to answer "no such file"; a person who names a fresh
// folder means "work there", so it is made.
func TestANamedPlaceThatIsNotThereYetIsCreated(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	fresh := filepath.Join(t.TempDir(), "new-scratch", "reports")

	tree, err := prepareTaskTreeAt(context.Background(), place, repo, "fresh", 5, "write the report", fresh, "")
	if err != nil {
		t.Fatalf("prepareTaskTreeAt: %v", err)
	}
	// The folder that was made, in the one spelling — see the test above.
	if tree.dir != canonicalPath(fresh) || tree.merge != mergeInPlace {
		t.Fatalf("fresh tree = %+v, want the named directory %s in place", tree, canonicalPath(fresh))
	}
	if info, err := os.Stat(fresh); err != nil || !info.IsDir() {
		t.Fatalf("the named place was not made: %v", err)
	}

	// C3: A relative name still hangs off the conversation's own workspace, but
	// because that path is inside a committed repository it gets a branch and the
	// real checkout is not made to hold the new folder.
	relative, err := prepareTaskTreeAt(context.Background(), place, repo, "fresh", 6, "write more", "notes/out", "")
	if err != nil {
		t.Fatalf("relative fresh place: %v", err)
	}
	if relative.root != canonicalPath(repo) || !strings.HasPrefix(relative.branch, "task/") {
		t.Fatalf("relative repository place made %+v", relative)
	}
	if _, err := os.Stat(filepath.Join(repo, "notes", "out")); !os.IsNotExist(err) {
		t.Fatalf("the relative place was made in the person's checkout: %v", err)
	}
	relative.releaseKept()
}

// The one refusal that survives: something IS there and it is not a directory.
// Creating beside it would be the surface guessing.
func TestANamedPlaceThatIsAFileIsStillRefused(t *testing.T) {
	repo := newTestRepo(t)
	place := Place{Dir: t.TempDir(), Workspace: repo}
	file := filepath.Join(t.TempDir(), "notes.md")
	writeFile(t, file, "not a folder\n")

	_, err := prepareTaskTreeAt(context.Background(), place, repo, "file", 7, "write there", file, "")
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("error = %v, want the not-a-directory refusal", err)
	}
	if info, statErr := os.Stat(file); statErr != nil || info.IsDir() {
		t.Fatalf("the named file was disturbed: %v", statErr)
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
