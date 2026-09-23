//go:build !windows

package app

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// These tests describe what senior-dev's own project verification guarantees.
//
// The family they belong to is "you cannot manufacture a green verification":
// a run whose evidence array carries exit=1 commands must not report a pass,
// and a project whose suite was never discovered is not a project that passed.

func writePassingPythonUnitTest(t *testing.T, workspace string) {
	t.Helper()
	const source = `import unittest

class GreenTest(unittest.TestCase):
    def test_green(self):
        self.assertEqual(2 + 2, 4)
`
	if err := writeFile(filepath.Join(workspace, "tests", "test_green.py"), source); err != nil {
		t.Fatal(err)
	}
}

func TestRedEntrypointAlwaysFailsVerification(t *testing.T) {
	// A command that exits non-zero fails verification. Unconditionally.
	//
	// An escape hatch that excused a command because a pre-edit baseline probe
	// had also seen it red would log the command as evidence but never count
	// it, so NewFailures would stay 0 and the run would report a green
	// verification while its own evidence array carried exit=1 commands.
	//
	// This test is the floor: a broken build is a failed verification no matter
	// what the tree looked like before the first edit.
	workspace := t.TempDir()
	writePassingPythonUnitTest(t, workspace)
	if err := writeFile(filepath.Join(workspace, "Makefile"),
		"build:\n\texit 1\ntest:\n\ttrue\n"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()

	verification := runner.runProjectVerification(context.Background())
	if verification.Failed == nil {
		t.Fatal("a build exiting 1 must fail verification")
	}
	if verification.NewFailures == 0 {
		t.Fatalf("NewFailures = 0 with a red build; the excused path is back")
	}
}

func TestEveryRedVerificationCommandIsCounted(t *testing.T) {
	// Every command recorded with a non-zero exit is counted as a new
	// failure. A recorded red command that does not reach NewFailures means
	// some caller has introduced a way to excuse a failure again.
	workspace := t.TempDir()
	writePassingPythonUnitTest(t, workspace)
	if err := writeFile(filepath.Join(workspace, "Makefile"),
		"build:\n\texit 1\ntest:\n\texit 1\n"); err != nil {
		t.Fatal(err)
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()

	verification := runner.runProjectVerification(context.Background())
	red := 0
	for _, command := range verification.Commands {
		evidence, ok := command.(map[string]any)
		if !ok {
			continue
		}
		if exit, ok := evidence["exit"].(float64); ok && exit != 0 {
			red++
		}
	}
	if red == 0 {
		t.Fatal("fixture produced no red command; the test proves nothing")
	}
	if verification.NewFailures < red {
		t.Fatalf("NewFailures = %d but %d commands exited non-zero: "+
			"some red command was recorded as evidence without being counted",
			verification.NewFailures, red)
	}
}

func verificationWorkspace(t *testing.T, files map[string]string) *pipeline {
	t.Helper()
	workspace := t.TempDir()
	for name, content := range files {
		if err := writeFile(filepath.Join(workspace, name), content); err != nil {
			t.Fatal(err)
		}
	}
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)
	return runner
}

func TestShellControlFlowCannotManufactureGreenVerification(t *testing.T) {
	// Six ways to make a red suite exit 0. Each of them turns "the tests pass"
	// into "the shell returned zero", and a run that ships on that evidence has
	// verified nothing. senior-dev executes discovered entrypoints under pipefail
	// for exactly this reason.
	for _, command := range []string{
		"go test ./... || true",
		"true || go test ./...",
		"exit 0; go test ./...",
		"true; go test ./...",
		"go test ./... | cat",
		"go test ./... 2>&1 | tee test.log",
	} {
		t.Run(command, func(t *testing.T) {
			runner := verificationWorkspace(t, map[string]string{
				"go.mod": "module example.test/red\n\ngo 1.23\n",
				"red_test.go": "package red\n\nimport \"testing\"\n\n" +
					"func TestRed(t *testing.T) { t.Fatal(\"red\") }\n",
				"AGENTS.md": "Run `go build ./...` and `" + command + "`.\n",
			})
			if verification := runner.runProjectVerification(context.Background()); verification.Failed == nil {
				t.Fatalf("control-flow bypass produced a green verification: %#v", verification)
			}
		})
	}
}

func TestAProjectWhoseTestEntrypointWasNeverFoundDoesNotPass(t *testing.T) {
	// The vacuous-green shape. A Go module is expected to have a test
	// entrypoint; if discovery cannot find one, that is a failure with no
	// failing COMMAND behind it -- Failed is set and the command list is empty.
	//
	// This is why soloShip reads verification.Failed rather than counting
	// non-zero exits: counting commands would report a verified pass for a
	// project whose suite was never located.
	// A Makefile with a build target and nothing else: discovering any command
	// makes the workspace accountable, and accountability is what demands a
	// test entrypoint. A go.mod would defeat the fixture -- the Go ecosystem
	// defaults supply `go test ./...` unprompted, so nothing would be missing.
	runner := verificationWorkspace(t, map[string]string{
		"Makefile": "build:\n\t@true\n",
	})
	verification := runner.runProjectVerification(context.Background())
	if verification.Failed == nil {
		t.Fatal("a project with no discoverable test entrypoint reported a green verification")
	}
	if !missingEntrypointFailure(verification) {
		t.Fatalf("expected a discovery failure, got %#v", verification.Failed)
	}
	if countFailingEntrypoints(verification) != 0 {
		t.Fatal("fixture no longer isolates the missing-entrypoint case from failing commands")
	}
}

func TestABareWorkspaceVerifiesVacuouslyRatherThanFailing(t *testing.T) {
	// The complement, and the reason the check above is Failed rather than
	// "did we run anything". A directory with no manifest, build system or
	// suite has nothing to verify. Demanding entrypoints there would fail every
	// documentation-only task on principle.
	runner := verificationWorkspace(t, map[string]string{"NOTES.txt": "no build system here\n"})
	verification := runner.runProjectVerification(context.Background())
	if verification.Failed != nil {
		t.Fatalf("a bare workspace was failed for having nothing to run: %#v", verification.Failed)
	}
	if !strings.Contains(verification.Prompt, "VACUOUS") {
		t.Fatalf("a vacuous pass must say so in its evidence:\n%s", verification.Prompt)
	}
}
