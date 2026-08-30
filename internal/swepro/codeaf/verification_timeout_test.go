package codeaf

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/verify"
)

// Validation contract for a verification entrypoint that hangs, derived from
// werkzeug-3146, which spent 1800s of a 3600s budget running `python3 -m
// pytest` three times, each killed at the 600s ceiling with byte-identical
// output, and was then told to "rerun the exact command until it exits 0":
//
//   - a hung entrypoint must be reported as HUNG, not as "exited -1", so the
//     model can tell an unfinished suite from a red one;
//   - the same command must not be re-executed against an unchanged tree — the
//     answer cannot differ and each attempt costs the full ceiling;
//   - a changed tree DOES earn a fresh attempt, because the agent may have
//     fixed the hang;
//   - a hang must never let the run go green.
func newTimeoutWorkspace(t *testing.T) (workspace, marker string) {
	t.Helper()
	workspace = t.TempDir()
	marker = filepath.Join(t.TempDir(), "executions")
	if err := writeFile(filepath.Join(workspace, "go.mod"),
		"module example.test/hang\n\ngo 1.23\n"); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`package hang

import (
  "os"
  "testing"
)

func TestHang(t *testing.T) {
  file, err := os.OpenFile(%q, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
  if err != nil { t.Fatal(err) }
  defer file.Close()
  if _, err := file.WriteString("x"); err != nil { t.Fatal(err) }
}
`, marker)
	if err := writeFile(filepath.Join(workspace, "hang_test.go"), source); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(workspace, "AGENTS.md"),
		"Run `go build ./...` and `go test -count=1 ./...`.\n"); err != nil {
		t.Fatal(err)
	}
	// worktreeFingerprint shells out to `git ls-files`, so the memo only has a
	// tree identity to compare against inside a repository.
	if out, err := exec.Command("git", "-C", workspace, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v: %s", err, out)
	}
	return workspace, marker
}

func executionCount(t *testing.T, marker string) int {
	t.Helper()
	body, err := os.ReadFile(marker)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(body)
}

func TestVerificationTimeoutIsNotRerunAgainstAnUnchangedTree(t *testing.T) {
	workspace, marker := newTimeoutWorkspace(t)
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()

	plan := verify.Discover(workspace)
	var testEntry *verify.Entrypoint
	for _, entrypoint := range plan.Entrypoints {
		if entrypoint.Kind == verify.KindTest {
			found := entrypoint
			testEntry = &found
			break
		}
	}
	if testEntry == nil {
		t.Fatalf("no test entrypoint discovered from %#v", plan.Entrypoints)
	}
	fingerprint, ok := runner.worktreeFingerprint(context.Background())
	if !ok {
		t.Skip("worktree fingerprint unavailable in this environment")
	}

	// Stand in for a prior cycle that hung: the entrypoint is on record as
	// having been killed at the ceiling against exactly this tree.
	runner.verificationTimeouts = map[string]timedOutEntrypoint{
		verificationMemoKey(*testEntry): {
			Tail:        "tests/test_serving.py .\ncommand timed out after 600000ms",
			Fingerprint: fingerprint,
			HaveFinger:  true,
		},
	}

	result := runner.runProjectVerification(context.Background())

	if got := executionCount(t, marker); got != 0 {
		t.Errorf("test entrypoint executed %d time(s); a recorded hang against an "+
			"unchanged tree must be replayed, not re-run at the full ceiling", got)
	}
	if !result.TimedOut {
		t.Errorf("result.TimedOut = false; the replayed observation must stay a timeout")
	}
	if result.Failed == nil {
		t.Fatalf("result.Failed = nil; a hung suite must not let the run go green")
	}
	if strings.Contains(result.Failure, "exited -1") {
		t.Errorf("failure text reports an exit code for a command that never exited: %q", result.Failure)
	}
	if !strings.Contains(result.Failure, "hung") {
		t.Errorf("failure text does not say the suite hung: %q", result.Failure)
	}
	gate := projectVerificationFailure(result)
	if len(gate.Verdict.RepairHints) == 0 ||
		!strings.Contains(gate.Verdict.RepairHints[0], "Do NOT rerun it verbatim") {
		t.Errorf("repair hint still advises rerunning a hung command: %#v", gate.Verdict.RepairHints)
	}
	// The evidence row must carry the distinction the prompt text now makes.
	row, isMap := result.Commands[len(result.Commands)-1].(map[string]any)
	if !isMap || row["timedOut"] != true {
		t.Errorf("evidence row missing timedOut marker: %#v", result.Commands)
	}
}

func TestVerificationTimeoutIsRetriedOnceTheTreeChanges(t *testing.T) {
	workspace, marker := newTimeoutWorkspace(t)
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{
		Events: newEventWriter(io.Discard), Notes: io.Discard,
	})
	defer runner.runtime.Close()

	plan := verify.Discover(workspace)
	for _, entrypoint := range plan.Entrypoints {
		if entrypoint.Kind != verify.KindTest {
			continue
		}
		// A stale fingerprint: the agent has edited the tree since the hang, so
		// the command deserves a fresh attempt.
		runner.verificationTimeouts = map[string]timedOutEntrypoint{
			verificationMemoKey(entrypoint): {
				Tail:        "command timed out after 600000ms",
				Fingerprint: "stale-fingerprint-from-before-the-fix", HaveFinger: true,
			},
		}
	}

	result := runner.runProjectVerification(context.Background())

	if got := executionCount(t, marker); got == 0 {
		t.Errorf("test entrypoint never ran; a changed tree must earn a fresh attempt")
	}
	if result.TimedOut {
		t.Errorf("result.TimedOut = true for a suite that completed")
	}
	if result.Failed != nil {
		t.Errorf("green verification reported a failure: %#v", result.Failed)
	}
}
