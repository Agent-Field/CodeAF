package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// AND THE TESTS DO NOT WRITE INTO THE DEVELOPER'S CHECKOUT EITHER.
//
// hermetic_test.go moves HOME because a real agent journals what it did. This
// is the other half, and it cost more: a real agent also COMMITS what it did.
// `git` runs with a working directory handed to it, and exec.Cmd reads an empty
// one as "wherever this process happens to be" — which, in a test binary, is
// the package's own directory inside somebody's checkout. One
// `go test ./internal/session/` run left three commits on the branch of the
// worktree it was launched from — "task: Rewrite", "task: Measure",
// "task: Paint", with a note.md and a marketing/sheet-*.png beside them — over
// another session's work, and they reached the remote before a rebase surfaced
// them.
//
// The seam is fixed where the directory is read (task_run.go's [gitWith] now
// refuses a command with nowhere to run). This is the witness for it: the run
// records the checkout's head and its uncommitted state before the first test
// and reads them again after the last, so the next path that reaches a
// repository the suite does not own is caught by the run that wrote it rather
// than by a rebase a day later.
//
// IT WATCHES THE CHECKOUT IT IS RUNNING IN and no other, because that is the
// one this suite can reach without being told where anything is. A test that
// makes a repository of its own in a t.TempDir() is invisible to it, which is
// right: that repository is the test's, and the suite may do as it likes there.
//
// AND IT WATCHES FOUR FACTS, BECAUSE TWO WERE NOT ENOUGH. The head and the
// porcelain caught the first incident and were silent through the second
// (#578): a t.TempDir() that lands INSIDE a checkout — which is where Go 1.26
// puts one when GOTMPDIR or TMPDIR names a directory in somebody's tree — is a
// directory whose repository, to `git rev-parse --show-toplevel`, is the
// checkout above it. A task grounded there cuts `task/do-the-thing-<hex>` in
// the person's own repository and registers a worktree at
// `<checkout>/.codeaf/tasks/s1/1`, and neither shows here: the branch never
// moves HEAD, and everything under `.codeaf/` is in .gitignore, so
// `git status --porcelain` never names it. So the branches and the registered
// worktrees are read too, and they are read the same symmetric way.
//
// THE GUARD REPORTS WHAT THIS RUN COULD HAVE WRITTEN, NOT EVERYTHING THAT
// MOVED. Branches and worktrees are not facts about this working copy: they
// belong to the whole shared repository, and this one shares its refs with
// ~/src/codeaf and with every other lane's worktree. This repo builds a
// feature wave in a worktree as a matter of routine, several sessions at once,
// and one `go test ./internal/session/` is nearly two minutes long — so a
// neighbour's `git worktree add` inside that window would appear here as `now:`
// and their `git branch -d` as `gone:`, and the run would fail for something no
// test did. A guard that fails on other people's ordinary work is a guard
// people learn to ignore, and a test that goes red only when other work runs
// beside it is a bug in this repository, not a shape to write down. So each of
// the two new readings is filtered to the harness's OWN shape first: a `task/`
// branch, which is [prepareTaskTree]'s spelling and nothing a person types by
// hand, and a worktree the harness would have registered.
//
// The trade is stated rather than hidden: a runaway that cut a branch under
// some other name slips past this reading. The head and the porcelain beside it
// do not care what anything is called and still catch the commit and the files,
// which is the shape that actually cost somebody an afternoon.
//
// AND A LINKED WORKTREE OF A SHARED CLONE READS LESS STILL. Branches belong to
// the clone's common directory, not to the working copy they were read from, so
// from a linked worktree every sibling's task/* churn — a branch cut for their
// own run, a branch deleted when their run landed — would look exactly like
// this run's damage, and the branch reading is skipped outright there. The
// worktree reading narrows instead of skipping: registrations under this root
// can still be this suite's, and a sibling's registration elsewhere in the
// clone cannot.
//
// It cannot see a content change to a file that was already dirty before the
// run — hashing the whole checkout each run costs more than it is worth — and
// the incident's shape, new commits, new files, new branches and new worktrees,
// is what it catches.

// checkout is what the tree looked like at a moment: the commit it stood on,
// the paths git considered dirty or unknown, the branches it had, and the
// worktrees registered against it. When the tree is a linked worktree of a
// shared clone, branches is nil — the reading is skipped, not empty — and the
// worktrees are only the ones registered under the tree itself.
type checkout struct {
	root      string
	linked    bool
	head      string
	dirty     map[string]bool
	branches  map[string]bool
	worktrees map[string]bool
}

// watchTheCheckout reads the repository this test binary is running inside. A
// tree it cannot read — a tarball, a machine with no git, a sandbox — leaves an
// empty watch, which reports nothing rather than failing a run for a fact it
// never had.
func watchTheCheckout() checkout {
	dir, err := os.Getwd()
	if err != nil {
		return checkout{}
	}
	root, err := gitLine(dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return checkout{}
	}
	watched := checkout{root: root}
	if watched.head, err = gitLine(root, "rev-parse", "HEAD"); err != nil {
		return checkout{}
	}
	watched.linked = isLinkedWorktree(root)
	if !watched.linked {
		watched.branches = branchNames(root)
	}
	watched.dirty = porcelain(root)
	watched.worktrees = worktreePaths(root, watched.linked)
	return watched
}

// isLinkedWorktree says whether the working copy at root is one worktree among
// several of one shared clone, which `rev-parse` answers by naming two
// different directories: the repository this copy carries its state in, and
// the common one every sibling of the same clone points at. A plain checkout
// answers the same directory twice.
func isLinkedWorktree(root string) bool {
	gitDir, err := gitLine(root, "rev-parse", "--git-dir")
	if err != nil || gitDir == "" {
		return false
	}
	commonDir, err := gitLine(root, "rev-parse", "--git-common-dir")
	if err != nil || commonDir == "" {
		return false
	}
	if !filepath.IsAbs(gitDir) { // a plain checkout answers relative to root
		gitDir = filepath.Join(root, gitDir)
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(root, commonDir)
	}
	return filepath.Clean(gitDir) != filepath.Clean(commonDir)
}

// moved says how the checkout differs from what it was, and "" when it does
// not. A watch that was never taken never reports.
func (c checkout) moved() string {
	if c.root == "" {
		return ""
	}
	head, err := gitLine(c.root, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Sprintf("the checkout at %s can no longer be read: %v", c.root, err)
	}
	var said []string
	if head != c.head {
		said = append(said, fmt.Sprintf("it stood on %s and now stands on %s; what was committed on top of it:\n%s",
			short(c.head), short(head), commitsBetween(c.root, c.head, head)))
	}
	// THE COMPARISON IS SYMMETRIC. A line that appeared is a file the suite
	// wrote; a line that went is a file it deleted, or reverted, or committed
	// out from under the person — `?? note.md` becoming nothing at all is
	// exactly what an `add` plus a `commit` looks like from here.
	if moved := differing(c.dirty, porcelain(c.root)); len(moved) > 0 {
		said = append(said, "these paths are not what they were before the run:\n\t"+strings.Join(moved, "\n\t"))
	}
	// BRANCHES AND WORKTREES BELONG TO THE WHOLE SHARED CLONE the checkout was
	// read from, and a LINKED worktree reads even less than the ambient case:
	// every sibling of the same clone churns the same task/* refs this suite
	// could not have written, so from a linked worktree the branch reading is
	// skipped outright and the worktree reading keeps only the registrations
	// under the checkout itself. The head and the porcelain above still watch
	// everything, whatever the checkout is.
	if c.linked {
		if moved := differing(c.worktrees, worktreePaths(c.root, true)); len(moved) > 0 {
			said = append(said, "these worktrees are not what they were before the run:\n\t"+strings.Join(moved, "\n\t"))
		}
	} else {
		// AND A LIVE codeaf CREATES THE SAME BRANCHES. When another codeaf is using
		// this checkout and TMPDIR is outside it, branch and worktree churn is
		// ambient the way journal growth is (hermetic_test.go) — report the head
		// and the porcelain, which a neighbour's ordinary work does not move, and
		// leave the shared refs alone. The #578 TempDir-inside-checkout case still
		// reports everything, because that is when the suite itself can mint them.
		ignoreAmbientRefs := liveCodeafUsingCheckout(c.root) && !tempDirInsideCheckout(c.root)
		if !ignoreAmbientRefs {
			if moved := differing(c.branches, branchNames(c.root)); len(moved) > 0 {
				said = append(said, "these branches are not what they were before the run:\n\t"+strings.Join(moved, "\n\t"))
			}
			// AND A WORKTREE IS A WRITE GIT IS TOLD TO IGNORE. The ground ladder puts a
			// task's tree under `.codeaf/`, which .gitignore covers, so the porcelain
			// above stays silent about a whole second checkout sitting in the person's
			// tree. The registration is not ignorable: git keeps it, so it is asked for.
			if moved := differing(c.worktrees, worktreePaths(c.root, false)); len(moved) > 0 {
				said = append(said, "these worktrees are not what they were before the run:\n\t"+strings.Join(moved, "\n\t"))
			}
		} else if moved := differing(c.branches, branchNames(c.root)); len(moved) > 0 ||
			len(differing(c.worktrees, worktreePaths(c.root, false))) > 0 {
			fmt.Fprintf(os.Stderr, "checkout guard: another codeaf is using %s; ignoring task branch/worktree churn (head and porcelain still watched)\n", c.root)
		}
	}
	if len(said) == 0 {
		return ""
	}
	return "session tests changed the checkout they were running in (" + c.root + "):\n" +
		strings.Join(said, "\n") + "\n" +
		"a git command ran against a repository the suite does not own, by one of two roads. " +
		"Either it was given no working directory at all, which exec reads as this process's own " +
		"(task_run.go's gitWith, #402); or it was given a directory it does own whose ground " +
		"resolved to the repository ABOVE it — a t.TempDir() inside this checkout, which " +
		"`rev-parse --show-toplevel` answers with this checkout (task_run.go's repositoryRoot, " +
		"#578). Which one it was, this guard cannot tell you; if the run had GOTMPDIR or TMPDIR " +
		"pointing inside a checkout, suspect the second. Undo the above before pushing.\n" +
		"(a second checkout running this same suite beside you cannot cause this; a test that names no directory can, and so can a temporary directory that is not outside your tree. A live codeaf using this checkout is ignored for branch/worktree churn when TMPDIR is outside the tree. When this checkout is a linked worktree of a shared clone, branch churn is not reported at all and the worktree reading covers only registrations under the checkout, because both belong to the clone the siblings share — a leak that only cut a task/ branch from inside a linked worktree slips past here, and the head and the porcelain above are what catch it.)"
}

// differing names every line that is in one listing and not the other, marked
// with the direction it moved, sorted so the failure reads the same way twice.
// It reads a set of paths, a set of branches and a set of worktrees alike,
// because the question asked of all three is the same one.
//
// A file that was ALREADY dirty and was then written again is invisible here,
// and deliberately: its content is the person's own work in progress, hashing
// a whole checkout on every run of this package would cost more than the guard
// is worth, and the shape this exists to catch — a task landing its ledger in
// the wrong repository — always writes files or commits that were not there.
func differing(before, after map[string]bool) []string {
	var moved []string
	for line := range after {
		if !before[line] {
			moved = append(moved, "now:  "+line)
		}
	}
	for line := range before {
		if !after[line] {
			moved = append(moved, "gone: "+line)
		}
	}
	sort.Strings(moved)
	return moved
}

// porcelain is the set of paths git calls dirty or unknown in root. An
// unreadable tree is an empty set: the caller compares two of these, and two
// empty sets say nothing changed, which is the quiet answer a guard owes a
// machine it cannot see.
func porcelain(root string) map[string]bool {
	found := map[string]bool{}
	out, err := gitLines(root, "status", "--porcelain")
	if err != nil {
		return found
	}
	for _, line := range out {
		if len(line) > 3 {
			found[line] = true
		}
	}
	return found
}

// harnessBranchPrefix is how a task's branch is spelled, and the whole of what
// makes a branch this suite's to report: `"task/" + slugify(title) + "-" +
// shortID()` in prepareTaskTree (task_run.go). Nothing a person cuts by hand in
// this repository is named that way — lanes are `fix/…`, `feat/…`, `bench/…` —
// so the prefix separates the harness's own work from the neighbour's without
// having to know who the neighbours are.
const harnessBranchPrefix = "task/"

// branchNames is the set of branches in root that THIS SUITE could have cut. It
// is asked with an explicit format rather than read off plain
// `git branch --list`, whose leading `* ` on the current branch would turn a
// head move into a second, invented line here.
//
// An unreadable tree is an empty set, for the same reason [porcelain] gives:
// two empty sets say nothing changed, which is what a guard owes a machine it
// cannot see.
func branchNames(root string) map[string]bool {
	found := map[string]bool{}
	out, err := gitLines(root, "branch", "--list", "--format=%(refname:short)")
	if err != nil {
		return found
	}
	for _, line := range out {
		if name := strings.TrimSpace(line); harnessBranch(name) {
			found[name] = true
		}
	}
	return found
}

// harnessBranch says whether a branch name is one a task run made.
func harnessBranch(name string) bool {
	return strings.HasPrefix(name, harnessBranchPrefix)
}

// worktreePaths is the set of worktree directories registered against root
// that THIS SUITE could have registered — including one it hid under an ignored
// folder, which is the whole reason this reading exists. For a linked worktree
// it is asked with underRoot, because the listing answers for the whole shared
// clone and only the registrations under the checkout itself can be the
// suite's.
//
// ONLY THE PATHS ARE KEPT. The plain listing carries each worktree's head
// beside its path, so keeping the whole line would report a head move a second
// time, in different words, from a set that is meant to answer a different
// question. The porcelain form is asked for so the path and the branch can be
// read separately, and an unreadable tree is an empty set as everywhere else
// here.
func worktreePaths(root string, underRoot bool) map[string]bool {
	out, err := gitLines(root, "worktree", "list", "--porcelain")
	if err != nil {
		return map[string]bool{}
	}
	found := harnessWorktrees(root, out)
	if !underRoot {
		return found
	}
	under := strings.TrimSuffix(root, "/") + "/"
	for path := range found {
		if !strings.HasPrefix(path, under) {
			delete(found, path)
		}
	}
	return found
}

// harnessWorktrees reads `git worktree list --porcelain` — one record per
// worktree, `worktree <path>` first and `branch <ref>` among the lines under it
// unless the head is detached — and keeps the paths of the records the harness
// could have made.
//
// THERE ARE TWO ROADS AND EACH NEEDS ITS OWN QUESTION. A task grounded in a
// repository puts its tree INSIDE that repository, under `.codeaf/tasks/…`,
// so a path below the watched root is the harness's whatever it is checked out
// on. A task grounded in a session folder puts its tree at
// `<session folder>/trees/<id>`, which is nowhere near the checkout and is
// still registered against it — that one is recognised by its branch instead.
func harnessWorktrees(root string, lines []string) map[string]bool {
	found := map[string]bool{}
	path, branch := "", ""
	keep := func() {
		if path != "" && harnessWorktree(root, path, branch) {
			found[path] = true
		}
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "worktree "):
			keep()
			path, branch = strings.TrimSpace(strings.TrimPrefix(line, "worktree ")), ""
		case strings.HasPrefix(line, "branch "):
			branch = strings.TrimSpace(strings.TrimPrefix(line, "branch "))
		}
	}
	keep()
	return found
}

// harnessWorktree says whether one registered worktree is one a task run made:
// a working copy inside the watched checkout (except the ambient `.codeaf/`
// tree a live codeaf also uses — see below), or one standing on a task branch
// wherever it happens to live. The checkout itself is neither — it was there
// before the run and no test registered it.
//
// `.codeaf/` UNDER THE CHECKOUT IS AMBIENT when TMPDIR/GOTMPDIR do not point
// inside the tree. A developer running codeaf in another window registers
// exactly those paths (`<checkout>/.codeaf/tasks/…`), and a guard that named
// them would fail every shared-box run for something no test did — which is the
// same law hermetic_test.go already keeps for journal trees. The #578 leak
// (t.TempDir() inside the checkout) still reaches this reading: when the
// temporary directory itself is under the watched root, in-tree `.codeaf/`
// worktrees ARE reported, because that is the only road the suite has to put
// them there without also moving HEAD.
func harnessWorktree(root, path, branch string) bool {
	if path == root {
		return false
	}
	under := strings.TrimSuffix(root, "/") + "/"
	if strings.HasPrefix(path, under+".codeaf/") && !tempDirInsideCheckout(root) {
		return false
	}
	if strings.HasPrefix(path, under) {
		return true
	}
	return harnessBranch(strings.TrimPrefix(branch, "refs/heads/"))
}

// tempDirInsideCheckout reports whether GOTMPDIR or TMPDIR names a directory
// under the watched checkout — the #578 shape. An unset or empty value is
// outside: Go then picks the system temp, which is not this tree.
func tempDirInsideCheckout(root string) bool {
	under := strings.TrimSuffix(root, "/") + "/"
	for _, name := range []string{"GOTMPDIR", "TMPDIR"} {
		dir := strings.TrimSpace(os.Getenv(name))
		if dir == "" {
			continue
		}
		if dir == root || strings.HasPrefix(dir, under) {
			return true
		}
	}
	return false
}

// TestTheCheckoutGuardReportsOnlyWhatThisRunCouldHaveWritten holds the filter
// still. Everything else in this file only ever runs from TestMain, where its
// answer is a failure message on somebody's terminal and never an assertion —
// so the one judgement it makes about OTHER PEOPLE'S work is the one part that
// has to be checked out loud.
//
// The rows on the false side are the reason the filter exists: `fix/578-…` and
// a wave's worktree at ~/af-579 are a neighbour's ordinary afternoon in a
// repository whose refs are shared, and a guard that named them would be
// switched off within a week.
func TestTheCheckoutGuardReportsOnlyWhatThisRunCouldHaveWritten(t *testing.T) {
	const root = "/home/somebody/af-578"

	for _, branch := range []struct {
		name string
		ours bool
	}{
		{"task/do-the-thing-0f1430", true},
		{"task/paint", true},
		{"dev", false},
		{"fix/578-task-ground", false},
		{"bench/canary", false},
		{"codeaf/leaf/task-2", false}, // a task in the NAME is not the prefix
		{"", false},
	} {
		if got := harnessBranch(branch.name); got != branch.ours {
			t.Errorf("harnessBranch(%q) = %v, want %v", branch.name, got, branch.ours)
		}
	}

	// One listing, read whole, because the parsing and the filter are one
	// answer: a record whose `branch` line was missed reads as detached, and a
	// detached record outside the root is one this suite is told to ignore.
	listing := []string{
		"worktree " + root,
		"HEAD 1111111111111111111111111111111111111111",
		"branch refs/heads/fix/578-task-ground",
		"",
		"worktree " + root + "/.codeaf/tasks/s1/1",
		"HEAD 2222222222222222222222222222222222222222",
		"branch refs/heads/task/do-the-thing-0f1430",
		"",
		"worktree /home/somebody/.codeaf/sessions/s1/trees/4",
		"HEAD 3333333333333333333333333333333333333333",
		"branch refs/heads/task/measure-9ab120",
		"",
		"worktree /home/somebody/af-579",
		"HEAD 4444444444444444444444444444444444444444",
		"branch refs/heads/feat/579-something-else",
		"",
		"worktree /home/somebody/af-canary-build-713945e3",
		"HEAD 5555555555555555555555555555555555555555",
		"detached",
	}
	want := map[string]bool{
		// `.codeaf/` under the checkout is ambient when TMPDIR is outside —
		// a live codeaf writes the same path. The session-folder worktree on a
		// task branch is still ours: that shape is nowhere near a person's
		// ordinary lane worktree.
		"/home/somebody/.codeaf/sessions/s1/trees/4": true,
	}
	got := harnessWorktrees(root, listing)
	for path := range want {
		if !got[path] {
			t.Errorf("harnessWorktrees did not report %q, which a task run makes", path)
		}
	}
	for path := range got {
		if !want[path] {
			t.Errorf("harnessWorktrees reported %q, which is somebody else's ordinary work", path)
		}
	}

	// AND WHEN TMPDIR IS INSIDE THE CHECKOUT, the in-tree `.codeaf/` path is
	// the #578 leak and MUST be reported — that is the whole reason the worktree
	// reading exists.
	t.Setenv("TMPDIR", root+"/tmp")
	t.Setenv("GOTMPDIR", "")
	wantInside := map[string]bool{
		root + "/.codeaf/tasks/s1/1":                 true,
		"/home/somebody/.codeaf/sessions/s1/trees/4": true,
	}
	gotInside := harnessWorktrees(root, listing)
	for path := range wantInside {
		if !gotInside[path] {
			t.Errorf("with TMPDIR inside the checkout, harnessWorktrees did not report %q", path)
		}
	}
	for path := range gotInside {
		if !wantInside[path] {
			t.Errorf("with TMPDIR inside the checkout, harnessWorktrees reported %q", path)
		}
	}
}

// TestTheCheckoutGuardDoesNotReportASiblingWorktreesBranch is the shared-clone
// case, run for real: a linked worktree stands beside a sibling in one clone,
// the sibling cuts and takes away task/ branches and registers a worktree of
// its own while "the run" is watched, and none of it may read as this run's
// damage — branches belong to the clone's common directory, and the sibling's
// registrations stand outside the watched tree. The second half proves the
// churn is real: watched from the main checkout, where the refs are this
// checkout's own, the same branch deletion is reported.
func TestTheCheckoutGuardDoesNotReportASiblingWorktreesBranch(t *testing.T) {
	shared := newTestRepo(t)
	linked := filepath.Join(shared, "linked")
	sibling := filepath.Join(shared, "sibling")
	mustGit(t, shared, "worktree", "add", "-b", "task/this-run-0a1b2c", linked)
	mustGit(t, shared, "worktree", "add", "-b", "task/sibling-run-9f8e7d", sibling)
	// A branch the sibling will take away during the run, seen only through
	// the clone's shared refs.
	mustGit(t, shared, "branch", "task/sibling-gone-554433")

	t.Chdir(linked)
	watched := watchTheCheckout()
	if !watched.linked {
		t.Fatal("the linked worktree was not read as linked, so this test is not standing where it means to")
	}
	// THE SAME REFS READ FROM THE MAIN CHECKOUT, before the churn, are this
	// checkout's own — that watch is the contrast the narrowing has to hold
	// against.
	head, _ := gitLine(shared, "rev-parse", "HEAD")
	watchedMain := checkout{root: shared, head: head,
		dirty: porcelain(shared), branches: branchNames(shared), worktrees: worktreePaths(shared, false)}

	mustGit(t, sibling, "branch", "task/sibling-cuts-778899")
	mustGit(t, sibling, "branch", "-D", "task/sibling-gone-554433")
	mustGit(t, shared, "worktree", "add", "-b", "task/sibling-tree-112233", filepath.Join(shared, "sibling-tree"))

	if said := watched.moved(); said != "" {
		t.Fatalf("a sibling worktree's churn was reported as this run's damage:\n%s", said)
	}

	// AND THE SAME CHURN IS REPORTED FROM THE MAIN CHECKOUT — the narrowing
	// above is tied to the linked worktree, not to the branch having been
	// someone else's.
	if said := watchedMain.moved(); !strings.Contains(said, "gone: task/sibling-gone-554433") {
		t.Fatalf("from the main checkout the sibling's deleted branch was not reported:\n%s", said)
	}
}

// liveCodeafUsingCheckout reports whether another codeaf process looks like it
// is working in this repository right now — a chat, a tick, a hosted session —
// whose task/ branches and `.codeaf/` worktrees would otherwise look exactly
// like the suite's own leak.
//
// THE QUESTION IS ASKED OF THE PROCESS TABLE, which exists wherever this
// program does: any other process whose command line names a worktree of the
// repository root belongs to is taken for a live codeaf. The worktrees come
// from `git worktree list --porcelain` — the checkout itself, and every tree
// registered against the same clone. A /proc walk was tried first and
// abandoned: it exists on Linux only, so on darwin the answer was always
// "no", and the ambient reading never engaged on the machine this suite is
// written on.
//
// IT IS BEST-EFFORT AND FAILS OPEN TO "NO". A machine without ps, a binary
// renamed something else, or a sandbox that hides other processes simply keeps
// the stricter reading; a false negative is a red that a person can re-run with
// codeaf closed, and a false positive would hide a real suite write.
func liveCodeafUsingCheckout(root string) bool {
	lines, err := gitLines(root, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	places := map[string]bool{root: true}
	for _, line := range lines {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			places[strings.TrimSpace(rest)] = true
		}
	}
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return false
	}
	self := os.Getpid()
	for _, line := range strings.Split(string(out), "\n") {
		pid, command, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found {
			continue
		}
		// The suite's own test binary stands IN a worktree but does not name one
		// on its command line, so it never reads as a neighbour here.
		own, err := strconv.Atoi(strings.TrimSpace(pid))
		if err != nil || own <= 0 || own == self {
			continue
		}
		if !strings.Contains(command, "codeaf") {
			continue
		}
		for place := range places {
			if strings.Contains(command, place) {
				return true
			}
		}
	}
	return false
}

// commitsBetween names what was written on top of the head the run started on,
// one line each, indented for the failure that prints it.
func commitsBetween(root, from, to string) string {
	out, err := gitLines(root, "log", "--oneline", "--no-decorate", from+".."+to)
	if err != nil || len(out) == 0 {
		return "\t(the log between them could not be read)"
	}
	return "\t" + strings.Join(out, "\n\t")
}

func gitLine(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func gitLines(dir string, args ...string) ([]string, error) {
	out, err := gitLine(dir, args...)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
