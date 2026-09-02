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

// checkout is what the tree looked like at a moment: the commit it stood on,
// and the paths git considered dirty or unknown.
type checkout struct {
	root  string
	head  string
	dirty map[string]bool
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
	if len(said) == 0 {
		return ""
	}
	return "session tests changed the checkout they were running in (" + c.root + "):\n" +
		strings.Join(said, "\n") + "\n" +
		"a git command was given a working directory the suite does not own — most likely none at all, " +
		"which exec reads as this process's own (task_run.go's gitWith). Undo the above before pushing.\n" +
		"(a second checkout running this same suite beside you cannot cause this; a test that names no directory can.)"
}

// differing names every porcelain line that is in one listing and not the
// other, marked with the direction it moved, sorted so the failure reads the
// same way twice.
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
