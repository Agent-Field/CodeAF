package main

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func receiptProducerOK(t *testing.T, file string) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	enc := json.NewEncoder(&out)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row["role"] == "tool" {
			row["presentation"] = map[string]any{"invoked": true, "outcome": "ok"}
		}
		if err := enc.Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(file, []byte(out.String()), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestReceiptSuccessfulLaunchText(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run(`printf 'job 12 started; log at /tmp/log\n'; cargo test --test cli regression`)
	receiptProducerOK(t, file)
	notes, _ := (&stageConversation{file: file, dir: root}).ExecutionNotes()
	for _, note := range notes {
		if strings.HasPrefix(note, "Command receipt:") && !strings.Contains(note, `"outcome":"completed"`) {
			t.Errorf("foreground exit=0 producer=ok misread: %s", note)
		}
	}
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err != nil {
		t.Errorf("measured foreground pass held: %v", err)
	}
}
func TestReceiptQuotedRefusedProbe(t *testing.T) {
	root, file, run := staleGuardFixture(t)
	guardUnrunJournal(t, file, `go test -run '^TestProbe$' ./pkg`, "refused")
	run("go test ./...")
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err != nil {
		t.Errorf("wider measured pass retained quoted refused probe: %v", err)
	}
}

func TestReceiptFailureTimeoutText(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run(`printf 'cut after 1s; output so far:\n'; CHECK_EXIT=7 cargo test --test cli regression`)
	receiptProducerOK(t, file)
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), `"outcome":"ok"`, `"outcome":"failed"`))
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	notes, _ := (&stageConversation{file: file, dir: root}).ExecutionNotes()
	for _, note := range notes {
		if strings.HasPrefix(note, "Command receipt:") && !strings.Contains(note, `"outcome":"failed"`) {
			t.Errorf("real exit=7 producer=failed misread: %s", note)
		}
	}
}

func TestReceiptQuotedRefusedProbeWithoutPass(t *testing.T) {
	root, file, _ := staleGuardFixture(t)
	command := `go test -run '^TestProbe$' ./pkg`
	guardUnrunJournal(t, file, command, "refused")
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err == nil || !strings.Contains(err.Error(), command+" · refused") {
		t.Fatalf("refused required check must remain owed: %v", err)
	}
}

func TestReceiptMaskedFailurePublication(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run(`CHECK_EXIT=7 cargo test --test cli regression; true`)
	receiptProducerOK(t, file)
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err == nil {
		t.Fatal("masked failure did not hold completion")
	}
	run(`cargo test --test cli regression`)
	receiptProducerOK(t, file)
	notes, _ := (&stageConversation{file: file, dir: root}).ExecutionNotes()
	for _, note := range notes {
		if strings.HasPrefix(note, verification.FailurePrefix) {
			t.Fatal("passing rerun did not clear failure", note)
		}
	}
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err != nil {
		t.Fatalf("passing rerun held completion: %v", err)
	}
	body := factory.PublishBody(factory.Item{Title: "review", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Result: &factory.StageResult{Notes: notes}}}}}, "")
	if strings.Contains(body, "CHECK_EXIT=7 cargo test --test cli regression; true") {
		t.Errorf("exit=7 check published as pass: %s", strings.ReplaceAll(body, "\n", " "))
	}
}

func TestReceiptLeadingFAIL(t *testing.T) {
	for _, output := range []string{"FAIL\nPASS\n", "FAIL\nok  example/pkg\n"} {
		got, _ := commandReceiptOutcome(output, "ok", true, false, false, nil)
		if got != "completed" {
			t.Errorf("producer ok/no measured exit: leading FAIL returned %s; want completed", got)
		}
	}
}

func TestReceiptCompoundCheckExits(t *testing.T) {
	for _, tc := range []struct {
		name       string
		command    string
		unmeasured string
		want       string
	}{
		{"failed_then_passed", "CHECK_EXIT=7 cargo test --test cli first; cargo test --test cli second", "", "failed"},
		{"passed_then_failed", "cargo test --test cli first; CHECK_EXIT=7 cargo test --test cli second", "", "failed"},
		{"zero_then_nil", "cargo test --test cli first; cargo test --test cli second", "second", "completed"},
		{"nil_then_zero", "cargo test --test cli first; cargo test --test cli second", "first", "completed"},
		{"nil_then_failed", "cargo test --test cli first; CHECK_EXIT=7 cargo test --test cli second", "first", "failed"},
		{"failed_then_nil", "CHECK_EXIT=7 cargo test --test cli first; cargo test --test cli second", "second", "failed"},
		{"all_zero", "cargo test --test cli first; cargo test --test cli second", "", "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, file, run := repairCheckFixture(t)
			run("printf 'PASS\\n'; " + tc.command)
			if tc.unmeasured != "" {
				paths, err := filepath.Glob(file + ".checks/call-*/check-*")
				if err != nil {
					t.Fatal(err)
				}
				removed := false
				for _, path := range paths {
					body, err := os.ReadFile(path)
					if err == nil && strings.Contains(string(body), tc.unmeasured) {
						if err := os.Remove(path + ".exit"); err != nil {
							t.Fatal(err)
						}
						removed = true
					}
				}
				if !removed {
					t.Fatal("missing check fixture")
				}
			}
			receiptProducerOK(t, file)
			notes, _ := (&stageConversation{file: file, dir: root}).ExecutionNotes()
			found := false
			for _, note := range notes {
				if strings.HasPrefix(note, "Command receipt:") {
					found = true
					if !strings.Contains(note, `"outcome":"`+tc.want+`"`) {
						t.Errorf("compound receipt want %s: %s", tc.want, note)
					}
				}
			}
			if !found {
				t.Fatal("missing command receipt")
			}
			body := factory.PublishBody(factory.Item{Title: "review", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Result: &factory.StageResult{Notes: notes}}}}}, "")
			if tc.want != "passed" && strings.Contains(body, "Passed locally:") {
				t.Error("compound unresolved check published as passed")
			}
			door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
			t.Cleanup(door.closeVerification)
			if err := door.Report(t.Context(), factory.StageResult{Done: true}); (err == nil) != (tc.name == "all_zero") {
				t.Errorf("completion guard want %s: %v", tc.want, err)
			}
			if tc.name == "failed_then_passed" {
				run("cargo test --test cli first")
				receiptProducerOK(t, file)
				if err := door.Report(t.Context(), factory.StageResult{Done: true}); err != nil {
					t.Errorf("passing rerun remained held: %v", err)
				}
			}
		})
	}
}
