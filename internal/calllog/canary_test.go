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
	cheapestPackageThatReachesAModel,
	"internal/session",
	"internal/exec",
	"internal/lane",
	"internal/head",
}

// cheapestPackageThatReachesAModel is the one the canary runs a second time to
// prove there is traffic to refuse at all: it streams scripted answers through
// a client and finishes in under a second.
const cheapestPackageThatReachesAModel = "internal/subharness"

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

	root := moduleRoot(t)
	// TWO canaries, because a person has two roots and either can be theirs:
	// the state root, and the profile root AFORGE_PROFILE_DIR moves out from
	// under it. A run that watched only the first would pass while writing into
	// the second.
	homeRoot, profileRoot := t.TempDir(), t.TempDir()
	began := time.Now()
	output, runErr := runUnderTheCanaries(t, goTool, root, homeRoot, profileRoot, packagesThatReachAModel)
	t.Logf("the canary ran %d packages in %s", len(packagesThatReachAModel), time.Since(began).Round(time.Second))

	// The ledgers first, because that is the law. A red package underneath is a
	// separate report and its own suite will say so; a package that never built
	// is not, because a canary that passed on nothing proves nothing.
	assertTheLedgerIsUntouched(t, filepath.Join(homeRoot, DirName, FileName))
	assertTheLedgerIsUntouched(t, filepath.Join(profileRoot, DirName, FileName))
	assertEveryPackageActuallyRan(t, output)
	if runErr != nil {
		t.Logf("the packages under the canary were not all green (%v); the ledgers above are what this test is about", runErr)
	}

	// And the silence has to MEAN something. A package that stopped making
	// model calls would leave both ledgers empty for the wrong reason, so one
	// of them is run again with the log pinned somewhere the gate does not
	// refuse: the rows that appear there are the traffic that was not written
	// into anybody's ledger a moment ago.
	assertTheTrafficIsRealAndAPinStillTakesIt(t, goTool, root)
}

// runUnderTheCanaries runs the packages in a child process that believes both
// of a person's roots are the canaries, with the log pin taken away so it
// resolves its own path exactly as a person's own run would.
func runUnderTheCanaries(t *testing.T, goTool, root, homeRoot, profileRoot string, packages []string) (string, error) {
	t.Helper()
	run := exec.Command(goTool, append([]string{"test", "-count=1", "-timeout", "10m"}, relativeTo(packages)...)...)
	run.Dir = root
	run.Env = append(withoutTheLogPin(os.Environ()),
		home.EnvVar+"="+homeRoot,
		profileDirEnv+"="+profileRoot,
		theCanaryIsRunning+"=1",
	)
	output, err := run.CombinedOutput()
	return string(output), err
}

// assertTheTrafficIsRealAndAPinStillTakesIt is the other half of the evidence,
// and it is cheap: internal/subharness is the fastest of the packages above and
// on its own it writes about forty rows. If they arrive at a pinned path, the
// scripted traffic still exists and a test that WANTS a log still gets one; if
// they do not, the clean ledgers above proved nothing and this says so.
func assertTheTrafficIsRealAndAPinStillTakesIt(t *testing.T, goTool, root string) {
	t.Helper()
	pinned := filepath.Join(t.TempDir(), FileName)
	run := exec.Command(goTool, "test", "-count=1", "./"+cheapestPackageThatReachesAModel+"/")
	run.Dir = root
	run.Env = append(withoutTheLogPin(os.Environ()),
		home.EnvVar+"="+t.TempDir(),
		EnvVar+"="+pinned,
		theCanaryIsRunning+"=1",
	)
	// A red package still makes model calls, and its own suite is where its
	// redness is somebody's problem. Only the rows are read here.
	output, _ := run.CombinedOutput()
	raw, err := os.ReadFile(pinned)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		t.Fatalf("%s wrote no rows even at a pinned path (%v), so the empty ledgers above say nothing about the gate:\n%s",
			cheapestPackageThatReachesAModel, err, output)
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
