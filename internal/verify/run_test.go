package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stageProject writes a Makefile whose `test` target is the given recipe body,
// which is what Discover reads as this project's own test entrypoint.
func stageProject(t *testing.T, recipe string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Makefile"),
		[]byte("test:\n\t"+recipe+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// A project that does not say how it is checked is not a project this can
// check, and the caller is told so rather than handed an empty reading it would
// have to tell apart from a green suite.
func TestAProjectThatSaysNothingAboutHowItIsCheckedIsNotChecked(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := Discover(root)
	if _, ok := RunTests(context.Background(), root, plan, time.Minute); ok {
		t.Errorf("a directory of loose files was reported as checked; plan = %#v", plan.Entrypoints)
	}
}

func TestTheProjectsOwnTestCommandIsRunAndTheNamesItPrintedAreRead(t *testing.T) {
	root := stageProject(t, "@echo 'FAILED tests/test_api.py::test_headers - AssertionError'; exit 1")

	result, ok := RunTests(context.Background(), root, Discover(root), time.Minute)

	if !ok {
		t.Fatalf("a Makefile with a test target was not recognised as a checkable project")
	}
	if result.Entrypoint.Command != "make test" {
		t.Errorf("entrypoint = %q, want the project's own command", result.Entrypoint.Command)
	}
	// make wraps its recipe's status in its own (2 for a failed recipe), which
	// is the project's own answer and the one worth carrying: what matters here
	// is that a red suite is not read as green.
	if result.Exit == 0 || result.TimedOut {
		t.Errorf("exit = %d, timedOut = %v; want the suite's own failing status",
			result.Exit, result.TimedOut)
	}
	if len(result.Failing) != 1 || result.Failing[0] != "tests/test_api.py::test_headers" {
		t.Errorf("failing = %#v, want the one test the runner named", result.Failing)
	}
}

// A hung suite is an INCOMPLETE observation, not a red one: it names nothing,
// so nothing can be subtracted from it and nothing attributed to it.
func TestASuiteKilledAtTheCeilingIsAnIncompleteObservationAndNotARedOne(t *testing.T) {
	root := stageProject(t, "@sleep 30")

	start := time.Now()
	result, ok := RunTests(context.Background(), root, Discover(root), 300*time.Millisecond)

	if !ok {
		t.Fatalf("the entrypoint was not discovered")
	}
	if !result.TimedOut {
		t.Errorf("timedOut = false for a suite killed at the ceiling: %#v", result)
	}
	if result.Exit != -1 {
		t.Errorf("exit = %d for a command that never exited; want -1", result.Exit)
	}
	if len(result.Failing) != 0 {
		t.Errorf("a hung suite named failures: %#v", result.Failing)
	}
	// The ceiling is honoured rather than waited out: WaitDelay bounds the wait
	// on the pipes after the process group is killed.
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("the reading took %v to give up on a 300ms ceiling", elapsed)
	}
}

// The preamble is what stops a suite going red behind a pipe and being read as
// green, so it has to actually be in force in the shell that runs the command.
func TestThePipelineFailureThePreambleGuardsAgainstIsNotReadAsGreen(t *testing.T) {
	// The plan is written by hand rather than discovered, because the pipeline
	// under test has to be THIS package's own command line: a `make` recipe
	// runs in make's shell, not in the one the preamble was set in.
	root := t.TempDir()
	plan := Plan{Entrypoints: []Entrypoint{{Kind: KindTest, Command: "false | cat"}}}

	result, ok := RunTests(context.Background(), root, plan, time.Minute)

	if !ok {
		t.Fatalf("the entrypoint was not discovered")
	}
	if result.Exit == 0 {
		t.Errorf("a pipeline whose first stage failed was read as green: %#v", result)
	}
}

// A suite that prints more than the buffer holds still gets its failure summary
// read, because the runners all print that summary last.
func TestASuiteThatPrintsMoreThanTheBufferHoldsStillHasItsSummaryRead(t *testing.T) {
	captured := &tailBuffer{limit: 64}
	captured.Write([]byte(strings.Repeat("noise\n", 100)))
	captured.Write([]byte("FAILED tests/test_api.py::test_headers\n"))

	if got := len(captured.String()); got > 64 {
		t.Errorf("the buffer kept %d bytes, want at most 64", got)
	}
	if names := FailingTests(captured.String()); len(names) != 1 ||
		names[0] != "tests/test_api.py::test_headers" {
		t.Errorf("failing = %#v, want the name from the tail", names)
	}
}
