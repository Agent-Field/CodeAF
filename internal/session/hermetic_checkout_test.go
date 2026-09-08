package session

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
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
// `<checkout>/.aforge-v3/tasks/s1/1`, and neither shows here: the branch never
// moves HEAD, and everything under `.aforge-v3/` is in .gitignore, so
// `git status --porcelain` never names it. So the branches and the registered
// worktrees are read too, and they are read the same symmetric way.
//
// THE GUARD REPORTS WHAT THIS RUN COULD HAVE WRITTEN, NOT EVERYTHING THAT
// MOVED. Branches and worktrees are not facts about this working copy: they
// belong to the whole shared repository, and this one shares its refs with
// ~/src/aforge-v2 and with every other lane's worktree. This repo builds a
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
// It cannot see a content change to a file that was already dirty before the
// run — hashing the whole checkout each run costs more than it is worth — and
// the incident's shape, new commits, new files, new branches and new worktrees,
// is what it catches.

// checkout is what the tree looked like at a moment: the commit it stood on,
// the paths git considered dirty or unknown, the branches it had, and the
// worktrees registered against it.
type checkout struct {
	root      string
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
	watched.dirty = porcelain(root)
	watched.branches = branchNames(root)
	watched.worktrees = worktreePaths(root)
	return watched
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
	// A BRANCH IS A WRITE THAT MOVES NOTHING. `task/do-the-thing-<hex>` cut in
	// the person's repository leaves HEAD exactly where it was and leaves the
	// porcelain empty, and it is still the suite committing into a tree it does
	// not own — the merge that would have moved HEAD is the only part that did
	// not happen yet.
	if moved := differing(c.branches, branchNames(c.root)); len(moved) > 0 {
		said = append(said, "these branches are not what they were before the run:\n\t"+strings.Join(moved, "\n\t"))
	}
	// AND A WORKTREE IS A WRITE GIT IS TOLD TO IGNORE. The ground ladder puts a
	// task's tree under `.aforge-v3/`, which .gitignore covers, so the porcelain
	// above stays silent about a whole second checkout sitting in the person's
	// tree. The registration is not ignorable: git keeps it, so it is asked for.
	if moved := differing(c.worktrees, worktreePaths(c.root)); len(moved) > 0 {
		said = append(said, "these worktrees are not what they were before the run:\n\t"+strings.Join(moved, "\n\t"))
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
		"(a second checkout running this same suite beside you cannot cause this; a test that names no directory can, and so can a temporary directory that is not outside your tree.)"
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

// worktreePaths is the set of worktree directories registered against root that
// THIS SUITE could have registered — including one it hid under an ignored
// folder, which is the whole reason this reading exists.
//
// ONLY THE PATHS ARE KEPT. The plain listing carries each worktree's head
// beside its path, so keeping the whole line would report a head move a second
// time, in different words, from a set that is meant to answer a different
// question. The porcelain form is asked for so the path and the branch can be
// read separately, and an unreadable tree is an empty set as everywhere else
// here.
func worktreePaths(root string) map[string]bool {
	out, err := gitLines(root, "worktree", "list", "--porcelain")
	if err != nil {
		return map[string]bool{}
	}
	return harnessWorktrees(root, out)
}

// harnessWorktrees reads `git worktree list --porcelain` — one record per
// worktree, `worktree <path>` first and `branch <ref>` among the lines under it
// unless the head is detached — and keeps the paths of the records the harness
// could have made.
//
// THERE ARE TWO ROADS AND EACH NEEDS ITS OWN QUESTION. A task grounded in a
// repository puts its tree INSIDE that repository, under `.aforge-v3/tasks/…`,
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
// a working copy inside the watched checkout, or one standing on a task branch
// wherever it happens to live. The checkout itself is neither — it was there
// before the run and no test registered it.
func harnessWorktree(root, path, branch string) bool {
	if path == root {
		return false
	}
	if strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/") {
		return true
	}
	return harnessBranch(strings.TrimPrefix(branch, "refs/heads/"))
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
		{"aforge/leaf/task-2", false}, // a task in the NAME is not the prefix
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
		"worktree " + root + "/.aforge-v3/tasks/s1/1",
		"HEAD 2222222222222222222222222222222222222222",
		"branch refs/heads/task/do-the-thing-0f1430",
		"",
		"worktree /home/somebody/.aforge-v3/sessions/s1/trees/4",
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
		root + "/.aforge-v3/tasks/s1/1":                 true, // inside the checkout: ours whatever it stands on
		"/home/somebody/.aforge-v3/sessions/s1/trees/4": true, // outside it, but standing on a task branch
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
