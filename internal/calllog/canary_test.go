package calllog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// modulePath is what `go test` prints beside a package it ran, and what this
// file has to recognise to know that a package was actually built rather than
// quietly skipped.
const modulePath = "github.com/Agent-Field/aforge-v2"

// theCanaryIsRunning stops the canary from starting a canary. Nothing in the
// list below is this package, but a list is a thing people add to, and a test
// that forks the suite it is part of is a bad afternoon.
const theCanaryIsRunning = "AFORGE_CALL_LOG_CANARY"

// packagesThatReachAModel is what the canary runs, and it is a WITNESS rather
// than the guarantee. The guarantee is one gate at the one place a path is
// resolved (undertest.go); these are the packages whose tests actually put
// invented traffic — `vendor/vision-model` at $4.25, `work/model`, `sim/model`,
// `test/model` — into a real person's ledger in #286, and running them proves
// the gate on the exact code that broke the law rather than on a fixture.
var packagesThatReachAModel = []string{
	"internal/subharness",
	"internal/session",
	"internal/exec",
	"internal/lane",
	"internal/head",
}

// TestNoTestInTheTreeWritesIntoTheLedgerOfWhoeverRanIt is the run-side law of
// #286: a person runs the suite — by hand, or through a leaf whose workspace is
// this repository — and their AFORGE_HOME/logs/calls.jsonl gains nothing.
//
// It runs the packages as a subprocess with a canary state root, which is the
// only shape that can prove it: the fiction is written by a test BINARY that
// inherited an environment, so nothing short of a second `go test` under a home
// nobody else can reach is evidence about the thing that went wrong.
func TestNoTestInTheTreeWritesIntoTheLedgerOfWhoeverRanIt(t *testing.T) {
	if os.Getenv(theCanaryIsRunning) != "" {
		t.Skip("this is the canary's own child run")
	}
	if testing.Short() {
		t.Skip("the canary builds and runs five packages' suites; about two minutes")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain here to run the packages with")
	}

	canary, root := t.TempDir(), moduleRoot(t)
	arguments := append([]string{"test", "-count=1", "-timeout", "10m"}, relativeTo(packagesThatReachAModel)...)
	run := exec.Command(goTool, arguments...)
	run.Dir = root
	run.Env = append(withoutTheLogPin(os.Environ()),
		home.EnvVar+"="+canary,
		theCanaryIsRunning+"=1",
	)
	began := time.Now()
	output, runErr := run.CombinedOutput()
	t.Logf("the canary ran %d packages in %s", len(packagesThatReachAModel), time.Since(began).Round(time.Second))

	// The ledger first, because that is the law. A red package underneath is a
	// separate report and its own suite will say so; a package that never built
	// is not, because a canary that passed on nothing proves nothing.
	assertTheLedgerIsUntouched(t, filepath.Join(canary, DirName, FileName))
	assertEveryPackageActuallyRan(t, string(output))
	if runErr != nil {
		t.Logf("the packages under the canary were not all green (%v); the ledger above is what this test is about", runErr)
	}
}

// assertTheLedgerIsUntouched is the whole assertion: not "few rows", not "no
// expensive rows" — the file a person reads is not there at all, because a test
// has no business creating it.
func assertTheLedgerIsUntouched(t *testing.T, ledger string) {
	t.Helper()
	raw, err := os.ReadFile(ledger)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read the canary ledger: %v", err)
	}
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	shown := rows
	if len(shown) > 3 {
		shown = shown[:3]
	}
	t.Fatalf("the suite wrote %d rows into the ledger of whoever ran it (%s); the first of them:\n%s",
		len(rows), ledger, strings.Join(shown, "\n"))
}

// assertEveryPackageActuallyRan reads the child's own report, so that a build
// that fell over cannot be read as a clean ledger.
func assertEveryPackageActuallyRan(t *testing.T, output string) {
	t.Helper()
	for _, pkg := range packagesThatReachAModel {
		if !ran(output, modulePath+"/"+pkg) {
			t.Fatalf("%s never ran under the canary, so its silence means nothing:\n%s", pkg, output)
		}
	}
}

// ran reports whether `go test` said what happened to one package. It reads the
// summary lines rather than the count of tests, because "ok" and "FAIL" are
// both answers and "[build failed]" is not one.
func ran(output, importPath string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != importPath {
			continue
		}
		if strings.Contains(line, "build failed") {
			return false
		}
		if fields[0] == "ok" || fields[0] == "FAIL" {
			return true
		}
	}
	return false
}

// relativeTo spells the packages the way the command line wants them.
func relativeTo(packages []string) []string {
	arguments := make([]string, 0, len(packages))
	for _, pkg := range packages {
		arguments = append(arguments, "./"+pkg+"/")
	}
	return arguments
}

// withoutTheLogPin drops AFORGE_CALL_LOG from an environment, so the child
// resolves its log exactly the way a person's own run would rather than
// inheriting a pin from whoever is running this suite.
func withoutTheLogPin(environment []string) []string {
	kept := environment[:0:0]
	for _, entry := range environment {
		if strings.HasPrefix(entry, EnvVar+"=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// moduleRoot is where the child has to run from, found by walking up from this
// package rather than asked of the toolchain: one stat is cheaper than a
// process, and a checkout that has no go.mod above it is a broken tree rather
// than a skip.
func moduleRoot(t *testing.T) string {
	t.Helper()
	here, err := os.Getwd()
	if err != nil {
		t.Fatalf("where am I: %v", err)
	}
	for directory := here; ; {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatalf("no go.mod above %s", here)
		}
		directory = parent
	}
}
