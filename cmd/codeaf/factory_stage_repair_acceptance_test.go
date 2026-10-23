package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

type repairReportDoor struct {
	reports int
	edits   int
	last    factory.StageResult
}

func (d *repairReportDoor) Report(_ context.Context, r factory.StageResult) error {
	d.reports++
	d.last = r
	return nil
}

// Real persisted check exits and the command journal must agree after a red
// regression demonstration, a checkout repair and a differently presented rerun.
func TestRepairSavedExitFailureIsSupersededOnTheRepairedCheckout(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	parent := &repairReportDoor{}
	door := &receiptStageDoor{StageDoor: parent, file: file, dir: root, repair: true}
	t.Cleanup(door.closeVerification)
	fixed := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "regression repaired"}}
	run(`CHECK_EXIT=127 cargo test --test cli regression > check.log 2>&1; ec=$?; echo "EXIT=$ec"; grep -E 'test result|FAIL' check.log | head -8`)
	if err := door.Report(t.Context(), fixed); err == nil {
		t.Fatal("failed check accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "code.txt"), []byte("repaired"), 0600); err != nil {
		t.Fatal(err)
	}
	run(`cargo test --test cli regression 2>&1 | tail -2`)
	if err := door.Report(t.Context(), fixed); err != nil {
		t.Fatalf("passing equivalent check retained historical shell failure: %v", err)
	}
	if parent.reports != 1 || !parent.last.Done {
		t.Fatal("corrected repair did not reach runner")
	}
}

func TestRepairRepeatedUnchangedReportsStopWithoutAnotherPaidRound(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	parent := &repairReportDoor{}
	door := &receiptStageDoor{StageDoor: parent, file: file, dir: root, repair: true}
	t.Cleanup(door.closeVerification)
	stopped := door.stoppedVerification()
	fixed := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "claimed fixed"}}
	for i := range stageVerificationRejectLimit {
		// New receipt files and changed output formatting are not progress.
		run(fmt.Sprintf("CHECK_EXIT=7 cargo test --test cli regression | tail -%d", i+1))
		if err := door.Report(t.Context(), fixed); err == nil {
			t.Fatal("failed repair accepted")
		}
	}
	select {
	case reason := <-stopped:
		if !strings.Contains(reason, "same unresolved evidence") {
			t.Fatal(reason)
		}
	default:
		t.Fatal("repeated rejection left the agent running")
	}
	if parent.reports != 1 || parent.last.Done || parent.last.Repair.Decision != "needs_you" {
		t.Fatal("stop did not retain an incomplete result")
	}
	if err := door.Report(t.Context(), fixed); err == nil || parent.reports != 1 {
		t.Fatal("late completion overwrote the stop")
	}
}

func TestRepairCorrectionCancelsTheStallStop(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	parent := &repairReportDoor{}
	door := &receiptStageDoor{StageDoor: parent, file: file, dir: root, repair: true}
	t.Cleanup(door.closeVerification)
	fixed := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "checked repair"}}
	run("CHECK_EXIT=7 cargo test --test cli regression")
	for range stageVerificationRejectLimit - 1 {
		_ = door.Report(t.Context(), fixed)
	}
	run("cargo test --test cli regression")
	if err := door.Report(t.Context(), fixed); err != nil {
		t.Fatal(err)
	}
	door.stopVerification("the completion recovery time limit was reached")
	select {
	case <-door.stoppedVerification():
		t.Fatal("a corrected repair was stopped by its old deadline")
	default:
	}
	if parent.reports != 1 || !parent.last.Done {
		t.Fatal("successful report was replaced")
	}
}

func TestRepairRecoveryDeadlineRetainsWorkAndStopsAnUnresponsiveTurn(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	parent := &repairReportDoor{}
	door := &receiptStageDoor{StageDoor: parent, file: file, dir: root, repair: true}
	t.Cleanup(door.closeVerification)
	run("CHECK_EXIT=7 cargo test --test cli regression")
	_ = door.Report(t.Context(), factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "claimed fixed"}})
	// Invoke the same callback as the timer, without waiting five minutes.
	door.stopVerification("the completion recovery time limit was reached")
	if reason := <-door.stoppedVerification(); !strings.Contains(reason, "time limit") {
		t.Fatal(reason)
	}
	if parent.last.Done || parent.last.Repair.Decision != "needs_you" {
		t.Fatal("deadline accepted unfinished work")
	}
	if raw, err := os.ReadFile(filepath.Join(root, "code.txt")); err != nil || string(raw) != "before" {
		t.Fatal("stop changed the checkout")
	}
}

func repairCheckFixture(t *testing.T) (string, string, func(string)) {
	t.Helper()
	root := resolvedTempDir(t)
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	gitT(t, root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, "code.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	gitT(t, root, "add", ".")
	gitT(t, root, "-c", "user.name=ZeroPoint95", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "cargo"), []byte("#!/bin/sh\nif [ \"${CHECK_EXIT:-0}\" = 127 ]; then exit 127; fi\necho 'test result: ok'\nexit \"${CHECK_EXIT:-0}\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	journal, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	enc, n := json.NewEncoder(journal), 0
	run := func(command string) {
		t.Helper()
		// Keep reporting logs outside the measured checkout.
		command = strings.ReplaceAll(command, "check.log", repairShellQuote(filepath.Join(bin, "check.log")))
		command = "export PATH=" + repairShellQuote(bin) + ":\"$PATH\"; " + command
		wrapped, err := verification.Prepare(file, command)
		if err != nil {
			t.Fatal(err)
		}
		c := exec.Command("bash", "-o", "pipefail", "-c", wrapped)
		c.Dir = root
		out, runErr := c.CombinedOutput()
		output := string(out)
		if runErr != nil {
			if exit, ok := runErr.(*exec.ExitError); ok {
				output += fmt.Sprintf("\nCommand exited with code %d", exit.ExitCode())
			} else {
				t.Fatal(runErr)
			}
		}
		n++
		id := fmt.Sprint(n)
		args, _ := json.Marshal(map[string]string{"command": command})
		if err := enc.Encode(map[string]any{"role": "assistant", "toolCalls": []any{map[string]any{"id": id, "function": map[string]string{"name": "bash", "arguments": string(args)}}}}); err != nil {
			t.Fatal(err)
		}
		if err := enc.Encode(map[string]string{"role": "tool", "toolCallId": id, "content": output}); err != nil {
			t.Fatal(err)
		}
	}
	return root, file, run
}
func (d *repairReportDoor) Edit(context.Context, factory.PlanEdit) error { d.edits++; return nil }
func (d *repairReportDoor) Ask(_ context.Context, q factory.Asked) (string, error) {
	return "manager answered", nil
}

func TestRepairFixedRequiresCurrentMeasuredAuditsAndKeepsPartialWorkIncomplete(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	gitT(t, root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, "code.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	gitT(t, root, "add", ".")
	gitT(t, root, "-c", "user.name=ZeroPoint95", "-c", "user.email=test@example.invalid", "commit", "-qm", "fixture")
	bin := t.TempDir()
	for _, name := range []string{"npm", "pip-audit"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf 'PASS\\n'\nexit \"${CHECK_EXIT:-0}\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(command string) {
		t.Helper()
		wrapped, err := verification.Prepare(file, "export PATH="+repairShellQuote(bin)+":\"$PATH\"; "+command)
		if err != nil {
			t.Fatal(err)
		}
		c := exec.Command("bash", "-o", "pipefail", "-c", wrapped)
		c.Dir = root
		_, _ = c.CombinedOutput()
	}
	parent := &repairReportDoor{}
	door := receiptStageDoor{StageDoor: parent, file: file, dir: root, repair: true}
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err == nil {
		t.Fatal("omitting repair status bypassed evidence guard")
	}
	fixed := factory.StageResult{Done: true, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "all checks fixed"}}
	if err := door.Report(t.Context(), fixed); err == nil {
		t.Fatal("no measured checks accepted fixed")
	}
	run("CHECK_EXIT=1 npm audit --omit=dev --audit-level=high || true")
	if err := door.Report(t.Context(), fixed); err == nil || !strings.Contains(err.Error(), "npm") {
		t.Fatalf("failing required audit accepted: %v", err)
	}
	partial := factory.StageResult{Done: false, Repair: &factory.CIRepairResult{Decision: "needs_repair", Reason: "useful partial fixes; high findings remain"}}
	if err := door.Report(t.Context(), partial); err != nil {
		t.Fatal(err)
	}
	if factory.Met(factory.Stage{Name: factory.FollowPRStep, Until: factory.UntilDone}, partial) {
		t.Fatal("partial remediation passed")
	}
	run("npm audit --omit=dev --audit-level=high")
	run("CHECK_EXIT=1 pip-audit || true")
	if err := door.Report(t.Context(), fixed); err == nil {
		t.Fatal("passing npm audit cleared unrelated failing pip audit")
	}
	run("pip-audit")
	if err := door.Report(t.Context(), fixed); err != nil {
		t.Fatalf("measured successful reruns rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "code.txt"), []byte("changed after testing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := door.Report(t.Context(), fixed); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("old checkout evidence accepted: %v", err)
	}
	if parent.reports != 2 {
		t.Fatalf("rejected reports reached parent: %d", parent.reports)
	}
	if answer, err := door.Ask(t.Context(), factory.Asked{}); err != nil || answer != "manager answered" {
		t.Fatal("receipt guard lost manager decisions")
	}
	if err := door.Edit(t.Context(), factory.PlanEdit{}); err != nil || parent.edits != 1 {
		t.Fatal("receipt guard lost plan edits")
	}
}
func repairShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func TestRepairInvalidCargoInvocationIsRecoverableAfterValidCoverage(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("CHECK_EXIT=1 cargo test --test cli regression_one regression_two")
	run("cargo test --test cli regression_one")
	notes, _ := (&stageConversation{file: file, dir: root}).RepairExecutionNotes()
	if !strings.Contains(strings.Join(notes, "\n"), verification.FailurePrefix) {
		t.Fatal("incomplete coverage cleared invalid invocation")
	}
	run("cargo test --test cli regression_two")
	notes, _ = (&stageConversation{file: file, dir: root}).RepairExecutionNotes()
	for _, n := range notes {
		if strings.HasPrefix(n, verification.FailurePrefix) {
			t.Fatalf("valid corrected coverage remained blocked: %s", n)
		}
	}
}

func TestRepairInvalidArgvCorrectionCannotEraseAnUnrelatedFailedShellSuffix(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("CHECK_EXIT=1 cargo test --test cli one two; false")
	run("cargo test --test cli")
	notes, _ := (&stageConversation{file: file, dir: root}).RepairExecutionNotes()
	if !strings.Contains(strings.Join(notes, "\n"), verification.FailurePrefix) {
		t.Fatal("corrected test erased an independent failed shell command")
	}
}
func TestRepairCorrectsInvalidCargoWithSavedExitAndReporting(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run(`CHECK_EXIT=1 cargo test --test cli one two > check.log 2>&1; ec=$?; echo "EXIT=$ec"; grep -E "test result" check.log; exit $ec`)
	run(`cargo test --test cli one`)
	run(`cargo test --test cli two`)
	notes, _ := (&stageConversation{file: file, dir: root}).RepairExecutionNotes()
	for _, n := range notes {
		if strings.HasPrefix(n, verification.FailurePrefix) {
			t.Fatalf("saved-exit correction remained blocked: %s", n)
		}
	}
}
