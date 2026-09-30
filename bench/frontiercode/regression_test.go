// Package frontiercode makes the bench's grading regression suite visible to
// the repository's own test entrypoints. The rig's graders are Python and
// shell, which `go test ./...` cannot discover on its own; this test runs them
// so a change to grade/rubric.py or fetch-results.sh is exercised by
// `make test-touched` and `make test` beside every other package.
package frontiercode

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func rigDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the rig directory")
	}
	return filepath.Dir(file)
}

func runScript(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v failed: %v\n%s", args, err, out)
	}
}

func TestGraderRegressionSuite(t *testing.T) {
	dir := rigDir(t)
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required to run the rig's graders")
	}
	runScript(t, dir, python, "grade/test_rubric.py")
}

func TestFetchResultsCompleteness(t *testing.T) {
	dir := rigDir(t)
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required to run the fetch-results regression")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to run fetch-results.sh")
	}
	if _, err := os.Stat(filepath.Join(dir, "tests", "fetch-results-check.sh")); err != nil {
		t.Fatalf("the fetch-results regression is missing: %v", err)
	}
	runScript(t, dir, "bash", "tests/fetch-results-check.sh")
}
