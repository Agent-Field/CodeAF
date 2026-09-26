//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A STOP DURING THE FINAL CHECK IS NOT A FAILED CHECK. codeaf stops a run
// with SIGTERM, which ends its context; if that lands while the project's own
// tests are running, the test command is killed half way and leaves no exit
// status. The run must not read that as the candidate failing: it ships the
// frozen candidate and says nothing finished checking it, which is what is
// true.
func TestAStopDuringTheCheckShipsTheCandidateUnchecked(t *testing.T) {
	runner, state, _, _ := soloPipeline(t)
	started := filepath.Join(t.TempDir(), "tests-started")
	makefile := "build:\n\t@true\n\ntest:\n\t@touch " + started + " && sleep 30\n"
	if err := writeFile(filepath.Join(runner.workspace, "Makefile"), makefile); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(runner.workspace, "feature.txt"), "implemented\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.soloFreezeWithContext(context.Background(), state, soloSubmission("implemented")); err != nil {
		t.Fatal(err)
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		// The stop lands once the test command is running, not before the
		// check could start, which is the case this test is about.
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(started); err == nil {
				stop()
				return
			}
		}
	}()
	outcome := &soloOutcome{Status: "fail"}
	began := time.Now()
	runner.soloShip(ctx, state, outcome, nil)
	if took := time.Since(began); took > 20*time.Second {
		t.Fatalf("ship took %s after the stop; the test command was not cut", took)
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatalf("the test command never started, so nothing was stopped mid-check: %v", err)
	}
	if outcome.Status != "pass-unverified" {
		t.Fatalf("status = %q, want the candidate shipped unchecked (%#v)", outcome.Status, outcome.TerminalData)
	}
	if status, _ := soloResultStatus(*outcome); status != "pass" {
		t.Fatalf("result status = %q, want pass: the frozen candidate stands", status)
	}
	if reason, _ := outcome.TerminalData["reason"].(string); !strings.Contains(reason, "stopped while") {
		t.Fatalf("reason = %q, want it to say the check was stopped", reason)
	}
}
