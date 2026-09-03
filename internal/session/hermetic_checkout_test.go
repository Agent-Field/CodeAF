package session

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
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

// branchNames is the set of branch names in root. It is asked with an explicit
// format rather than read off plain `git branch --list`, whose leading `* ` on
// the current branch would turn a head move into a second, invented line here.
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
		if line = strings.TrimSpace(line); line != "" {
			found[line] = true
		}
	}
	return found
}

// worktreePaths is the set of worktree directories registered against root — the
// main one and every linked one, including a linked one the suite hid under an
// ignored folder.
//
// ONLY THE PATHS ARE KEPT. The plain listing carries each worktree's head
// beside its path, so keeping the whole line would report a head move a second
// time, in different words, from a set that is meant to answer a different
// question. The porcelain form is asked for so the path can be taken on its
// own, and an unreadable tree is an empty set as everywhere else here.
func worktreePaths(root string) map[string]bool {
	found := map[string]bool{}
	out, err := gitLines(root, "worktree", "list", "--porcelain")
	if err != nil {
		return found
	}
	for _, line := range out {
		if path := strings.TrimPrefix(line, "worktree "); path != line && strings.TrimSpace(path) != "" {
			found[strings.TrimSpace(path)] = true
		}
	}
	return found
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
