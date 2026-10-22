package main

import (
	"context"
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
}

func (d *repairReportDoor) Report(context.Context, factory.StageResult) error {
	d.reports++
	return nil
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
