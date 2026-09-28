package session

// THE TESTS DO NOT WRITE INTO THE DEVELOPER'S HOME.
//
// A test that runs a task runs a real agent, and a real agent journals what it
// did. That journal's directory resolves from the state root (internal/home),
// and the state root is the one thing a t.TempDir() cannot move: a test could
// hand the session a temporary workspace, a temporary Place and a temporary
// session file, and the node's transcript would still land in the person's own
// ~/.codeaf/v3/tasks. One run of this package left 183 files across 40 hash
// directories there, every one of them a session line pointing at a t.TempDir()
// that no longer existed.
//
// So the package moves HOME, and only HOME, for the whole run.
//
// IT MOVES HOME AND NOT CODEAF_HOME, which looks like the long way round and is
// not. CODEAF_HOME outranks HOME, so setting it here would have overruled every
// test that already does t.Setenv("HOME", t.TempDir()) for isolation of its own
// — the careful tests — and collapsed their separate state roots back into one
// shared directory. Node journals are named to the second, so a shared root is
// a shared file: the second agent to ask for tasks/unfiled/<stamp>_1.jsonl gets
// handed the first one's transcript, and resumes it. Moving HOME leaves each of
// those tests exactly the root it asked for, one level below the developer's.
//
// CODEAF_HOME is cleared for the same reason it is not set: a developer who
// exports it has pointed the state root at a real directory of their own, and
// it would win over everything here.
//
// Nothing about production changes. A real session still resolves ~/.codeaf,
// because nothing outside this file touches either variable.
//
// And then the guard, because moving HOME only covers what reads HOME. The run
// ends by looking at the real state root's journal trees and failing if they
// grew, which is how the next path resolved from somewhere clever gets caught
// on the day it is written instead of a hundred and eighty files later.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

func TestMain(m *testing.M) { os.Exit(runTests(m)) }

// runTests is TestMain's body as a function with a return value, so the
// temporary home is still removed on the way out — os.Exit runs no deferred
// call, and a package that leaks a directory per run to stop leaking files per
// run has not fixed anything.
func runTests(m *testing.M) int {
	// The guard watches wherever this machine actually keeps its state: the real
	// home, or the CODEAF_HOME a developer had already pointed somewhere else.
	// It asks [home.InheritedDir] rather than [home.Dir] because Dir hands a
	// test binary a throwaway root of its own (internal/home/undertest.go), and
	// a guard watching that would be watching the very directory it exists to
	// keep the files out of.
	real := home.InheritedDir()
	before := journalTrees(real)
	// AND THE CHECKOUT THIS BINARY IS RUNNING IN, for the same reason and at the
	// same moment: a node commits as well as journals, and a git command with no
	// directory of its own runs here. See hermetic_checkout_test.go.
	tree := watchTheCheckout()

	root, err := os.MkdirTemp("", "codeaf-session-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "session tests: no temporary home: %v\n", err)
		return 1
	}
	defer os.RemoveAll(root)
	if err := os.Setenv("HOME", root); err != nil {
		fmt.Fprintf(os.Stderr, "session tests: could not move HOME: %v\n", err)
		return 1
	}
	if err := os.Unsetenv(home.EnvVar); err != nil {
		fmt.Fprintf(os.Stderr, "session tests: could not clear %s: %v\n", home.EnvVar, err)
		return 1
	}
	// AND THE TESTS DO NOT REACH THE MACHINE'S OWN FURROW EITHER.
	//
	// The ground ladder's top rung attaches a folder to furrow and forks it
	// (groundladder.go), so on a developer's machine — where furrow is on PATH,
	// and on a built binary where it is carried inside codeaf — every test that
	// grounds a task in a plain folder would attach a t.TempDir() and seal a
	// snapshot of it. That is a suite whose answers depend on what the person
	// running it happens to have installed, which is the one thing a test may
	// never be. CODEAF_FURROW is the pin a person chose, and a pin naming
	// something that is not there is an error rather than a quiet fall back to
	// PATH — so this is the whole of "no furrow here".
	//
	// A test that wants the rung installs a furrow of its own over the top of
	// this with t.Setenv, which is restored for it on the way out.
	if err := os.Setenv("CODEAF_FURROW", filepath.Join(root, "no-furrow-in-the-tests")); err != nil {
		fmt.Fprintf(os.Stderr, "session tests: could not pin furrow away: %v\n", err)
		return 1
	}
	// AND THIS PACKAGE'S SUITE IS THE OLDER BELT'S SUITE, SAID ONCE HERE.
	//
	// The bash belt is the product's default. Most of the tests in this package
	// were written against the node belt and describe its behaviour — its
	// landings, its auditor, its own task tree — and they said so by saying
	// nothing, because the default used to agree with them. When the default
	// moved they did not become wrong, they became silent about which road they
	// meant, and a hundred and forty of them changed what they were testing
	// without one line of them changing.
	//
	// So the road they were written for is named here rather than a hundred and
	// forty times, and a test that means the harness says so with
	// t.Setenv(..., "bash"), which outranks this for that test and is restored
	// on the way out. Every harness test in this package already does.
	//
	// THIS IS NOT A PIN THAT MAKES THE DEFAULT UNTESTED. The harness road has
	// its own suites and they run on the real default: internal/run,
	// internal/plandb, and this package's own bashbelt, plandb and land_run
	// files. What is pinned here is the older engine's suite, which is the only
	// thing it ever tested.
	if err := os.Setenv("CODEAF_TASK_BELT", "node"); err != nil {
		fmt.Fprintf(os.Stderr, "session tests: could not pin the belt: %v\n", err)
		return 1
	}

	code := m.Run()

	if moved := tree.moved(); moved != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", moved)
		if code == 0 {
			code = 1
		}
	}
	if leaked := grew(before, journalTrees(real)); len(leaked) > 0 {
		fmt.Fprintf(os.Stderr, "\nsession tests wrote %d file(s) into the real state root at %s:\n", len(leaked), real)
		for _, path := range leaked {
			fmt.Fprintf(os.Stderr, "\t%s\n", path)
		}
		fmt.Fprintf(os.Stderr, "a journal path was resolved from something other than HOME; route it through internal/home and delete the files above.\n")
		fmt.Fprintf(os.Stderr, "(a second checkout running this same suite beside you, without this guard, writes there too.)\n")
		if code == 0 {
			code = 1
		}
	}
	return code
}

// journalTrees lists every file under the state root's two journal trees — the
// only places an agent's transcript can land. It is deliberately not the whole
// state root: a developer running codeaf in another window writes history lines
// and model caches there while the suite runs, and a guard that cries about
// those is a guard people learn to ignore.
func journalTrees(state string) map[string]bool {
	found := map[string]bool{}
	for _, tree := range []string{"tasks", "runs"} {
		root := filepath.Join(state, "v3", tree)
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			found[path] = true
			return nil
		})
	}
	return found
}

// grew names what the second listing holds and the first did not, sorted so the
// failure reads the same way twice.
func grew(before, after map[string]bool) []string {
	var added []string
	for path := range after {
		if !before[path] {
			added = append(added, path)
		}
	}
	sort.Strings(added)
	return added
}
