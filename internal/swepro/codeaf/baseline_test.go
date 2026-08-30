package codeaf

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/auditorgate"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// redSuiteWorkspace stages a Go repository that is ALREADY failing when the
// harness arrives — the shape of spf13/cobra at the benchmark pin, where
// TestFailGenFishCompletionFile asserts a permission error and cannot fail as
// root. The pre-existing red is committed; nothing the run does can repair it,
// and nothing it does should be blamed for it.
func redSuiteWorkspace(t *testing.T) (workspace, base string) {
	t.Helper()
	workspace, _ = guardWorkspace(t)
	write := func(name, body string) {
		t.Helper()
		if err := writeFile(filepath.Join(workspace, name), body); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/delta\n\ngo 1.23\n")
	write("delta.go", "package delta\n\nfunc Answer() int { return 41 }\n")
	write("delta_test.go", `package delta

import "testing"

func TestEnvironmentBound(t *testing.T) {
	t.Fatalf("this assertion cannot hold in this environment")
}
`)
	if err := gitRun(workspace, "add", "go.mod", "delta.go", "delta_test.go"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "a repository that arrives red"); err != nil {
		t.Fatal(err)
	}
	return workspace, strings.TrimSpace(gitOutput(context.Background(), workspace, "rev-parse", "HEAD"))
}

func deltaPipeline(t *testing.T, workspace string) *pipeline {
	t.Helper()
	t.Setenv("CODEAF_ADAPTIVE_CUTS", "0")
	t.Setenv("CODEAF_AUTORESUME", "0")
	t.Setenv("CODEAF_FRONTIER", "0")
	t.Setenv("CODEAF_VALIDITY", "0")
	t.Setenv("CODEAF_TAMPER", "0")
	t.Setenv("CODEAF_SPEC_IDS", "0")
	t.Setenv("CODEAF_AUDITOR", "0")
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Backend: backendFunc(func(context.Context, turn) (turnResult, error) {
			return turnResult{Text: "no model should be needed here"}, nil
		}),
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	t.Cleanup(runner.runtime.Close)
	return runner
}

func TestPreExistingRedDoesNotFailACorrectPatch(t *testing.T) {
	// audit-notes §14.4.1, the cobra#2257 shape: `go test ./...` exits 1 both
	// before and after, for the same test, and the correct patch beside it must
	// still settle. The old gate read the exit code and threw the work away.
	workspace, base := redSuiteWorkspace(t)
	runner := deltaPipeline(t, workspace)

	runner.captureBaseline(context.Background())
	if runner.baseline == nil || runner.baseline.redCount() == 0 {
		t.Fatalf("baseline = %#v, want a red photograph of the pristine tree", runner.baseline)
	}

	// The work: a correct change, plus the regression test that proves it.
	if err := writeFile(filepath.Join(workspace, "delta.go"),
		"package delta\n\nfunc Answer() int { return 42 }\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "fix_test.go"), `package delta

import "testing"

func TestZZBAnswerIsFortyTwo(t *testing.T) {
	if Answer() != 42 {
		t.Fatalf("Answer() = %d", Answer())
	}
}
`); err != nil {
		t.Fatal(err)
	}

	verification := runner.runProjectVerification(context.Background())
	if verification.Failed != nil {
		t.Fatalf("verification failed on a pre-existing red: %s", verification.Failure)
	}
	if len(verification.PreExisting) == 0 {
		t.Fatal("verification passed but said nothing about the pre-existing failure")
	}
	if !strings.Contains(verification.PreExisting[0], "TestEnvironmentBound") ||
		!strings.Contains(verification.PreExisting[0], "ALREADY failing") {
		t.Fatalf("pre-existing note = %q", verification.PreExisting[0])
	}
	if !strings.Contains(verification.Prompt, "PRE-EXISTING") {
		t.Fatalf("auditor prompt does not name the pre-existing failure:\n%s", verification.Prompt)
	}
	notes := projectVerificationPassVerdict(verification).Step2Signal.Notes
	if notes == nil || !strings.Contains(*notes, "ALREADY present before this run") {
		t.Fatalf("pass verdict notes = %v", notes)
	}

	audit, _, err := runner.auditFixLoop(context.Background(),
		"Make Answer return 42.", base, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusPass {
		t.Fatalf("audit = %#v, want pass over a pre-existing red", audit)
	}
}

func TestNewlyRedTestStillFails(t *testing.T) {
	// The half that must not bend. Same repository, same pre-existing red, but
	// the change breaks something that was passing — the delta is non-empty and
	// the run fails exactly as it did before any of this existed.
	workspace, base := redSuiteWorkspace(t)
	if err := writeFile(filepath.Join(workspace, "green_test.go"), `package delta

import "testing"

func TestAlreadyGreen(t *testing.T) {
	if Answer() != 41 {
		t.Fatalf("Answer() = %d", Answer())
	}
}
`); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "add", "green_test.go"); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(workspace, "commit", "-m", "a passing test beside the red one"); err != nil {
		t.Fatal(err)
	}
	base = strings.TrimSpace(gitOutput(context.Background(), workspace, "rev-parse", "HEAD"))

	runner := deltaPipeline(t, workspace)
	runner.captureBaseline(context.Background())

	// The work: a change that breaks the passing test.
	if err := writeFile(filepath.Join(workspace, "delta.go"),
		"package delta\n\nfunc Answer() int { return 7 }\n"); err != nil {
		t.Fatal(err)
	}

	verification := runner.runProjectVerification(context.Background())
	if verification.Failed == nil {
		t.Fatal("a newly red test was acquitted as pre-existing")
	}
	if !strings.Contains(verification.Failure, "TestAlreadyGreen") ||
		!strings.Contains(verification.Failure, "turned them red") {
		t.Fatalf("failure = %q, want it to name the test this change broke", verification.Failure)
	}
	if len(verification.PreExisting) != 0 {
		t.Fatalf("pre-existing = %#v, want none on a regression", verification.PreExisting)
	}

	audit, _, err := runner.auditFixLoop(context.Background(),
		"Make Answer return 7.", base, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != auditorgate.StatusFail {
		t.Fatalf("audit = %#v, want fail on a regression", audit)
	}
}

func TestUnnamedFailureIsNeverAcquitted(t *testing.T) {
	// The acquittal is only ever granted for a failure that can be NAMED on
	// both sides. A red entrypoint whose output no runner vocabulary reads —
	// a compiler error, a shell one-liner, an opaque suite — is judged exactly
	// as it was before any of this existed. The alternative is the broadest
	// possible acquittal ("the suite was red before, so its being red now
	// proves nothing"), which is wrong on the one run whose whole job was to
	// turn that suite green.
	entrypoint := verify.Entrypoint{Kind: verify.KindTest, Command: "make check"}
	record := &baselineRecord{Entries: map[string]baselineEntry{
		verificationMemoKey(entrypoint): {Command: "make check", Exit: 2},
	}}
	same := record.judge(entrypoint, 2, "make: *** [check] Error 2\n")
	if same.PreExisting {
		t.Fatalf("an unnamed failure was acquitted: %#v", same)
	}
	build := verify.Entrypoint{Kind: verify.KindBuild, Command: "make all"}
	record.Entries[verificationMemoKey(build)] = baselineEntry{
		Command: "make all", Exit: 2, Failing: []string{"TestFailGenFishCompletionFile"},
	}
	// A named test failing inside a BUILD-kind entrypoint still acquits: the
	// suite ran and said which test it was. This is the cobra#2257 shape.
	named := record.judge(build, 2, "--- FAIL: TestFailGenFishCompletionFile (0.00s)\nFAIL\n")
	if !named.PreExisting {
		t.Fatalf("the cobra shape was not acquitted: %#v", named)
	}
}
