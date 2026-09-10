package home

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A TEST BINARY NEVER RESOLVES THE STATE ROOT OF WHOEVER RAN IT.
//
// [Dir] answers ~/.aforge when AFORGE_HOME says nothing, which is exactly right
// for the product and exactly wrong under `go test`: a test binary inherits the
// HOME and the AFORGE_HOME of the person at the keyboard, so any package that
// reaches this seam without pinning a root of its own writes into that person's
// live state — their journals, their model caches, their router ledger, their
// profile.
//
// It has happened repeatedly, and each time it was fixed one package at a time.
// `internal/session` moves HOME for its whole run and still ends by counting
// the files under the real journal trees, because one run of it had left 183 of
// them there (hermetic_test.go). `internal/calllog` refuses an inherited path
// because a suite appended 356 rows of invented traffic to a real ledger
// (#286). `internal/lane` refuses one because a chooser reached through to a
// person's cached sheets and turned a test red for good (#475). Three packages
// remembered; every package written since started unprotected again, which is
// the defect this file closes (#402).
//
// SO THE GATE IS AT THE ONE SEAM EVERY ONE OF THOSE PATHS PASSES THROUGH. There
// is exactly one place the state root is resolved, and a gate on it holds for
// tests nobody has written yet — the same argument #352 and #475 made about the
// call log and the lane store, applied to the root they both resolve from.
//
// WHAT IS REFUSED IS AN INHERITED ROOT AND NEVER A ROOT A TEST CHOSE. A test
// that says where its state goes — `t.Setenv(home.EnvVar, t.TempDir())`, or
// moving HOME the way `internal/session` does — still gets exactly the root it
// asked for. Only the root nobody named is redirected, and it is redirected to
// [quarantine] rather than refused outright, because [Dir] has to answer with a
// directory a caller can write to: every caller here creates files, and "" or a
// panic would turn a silent corruption into a wide breakage across suites that
// are doing nothing wrong.
var underTest = testing.Testing()

// inherited is the state root this process was STARTED with, read once at
// package initialisation — before any test has run, because `t.Setenv` can only
// happen inside one. It is the root that belongs to the person rather than to
// the test, and it is the only thing the gate refuses.
var inherited = resolve()

// quarantine is where a test binary's unpinned state goes instead: a throwaway
// directory named for this process, so two packages running side by side never
// share one root the way `internal/session`'s node journals once shared a file.
//
// It is NAMED and not created. Callers under the state root make their own
// directories, and a root that is only named costs nothing on the runs that
// never touch it — which is most of them.
var quarantine = filepath.Join(os.TempDir(), "aforge-test-home-"+strconv.Itoa(os.Getpid()))

// InheritedDir is the state root this process was started with, ungated.
//
// It exists for the two callers that genuinely want the person's own root and
// are themselves tests: `internal/session`'s guard, which counts the files
// under the real journal trees to catch a path resolved from somewhere clever,
// and the e2e harness, which copies the person's provider credentials out of
// their real config.json before putting a throwaway home in front of the
// binary. Anything that is not a guard or a credential read wants [Dir].
func InheritedDir() string { return inherited }
