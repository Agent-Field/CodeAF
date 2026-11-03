package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func staleGuardFixture(t *testing.T) (string, string, func(string)) {
	t.Helper()
	root, file, run := repairCheckFixture(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\necho PASS\nexit \"${CHECK_EXIT:-0}\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return root, file, func(command string) { run("export PATH=" + repairShellQuote(bin) + ":\"$PATH\"; " + command) }
}

func TestGuardStaleWiderPass(t *testing.T) {
	root, file, run := staleGuardFixture(t)
	run("CHECK_EXIT=7 go test -run TestA ./pkg")
	run("go test ./...")
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err != nil {
		t.Fatal(err)
	}
}

func TestGuardDeletedScratch(t *testing.T) {
	root, file, run := staleGuardFixture(t)
	scratch := t.TempDir()
	run("cp code.txt " + repairShellQuote(filepath.Join(scratch, "copy")) + "; cd " + repairShellQuote(scratch) + "; CHECK_EXIT=7 go test ./...")
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatal(err)
	}
	run("go test ./...")
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err != nil {
		t.Fatal(err)
	}
}

func guardUnrunJournal(t *testing.T, file, command, outcome string) {
	t.Helper()
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint("unrun-", stat.Size())
	enc := json.NewEncoder(f)
	args, _ := json.Marshal(map[string]string{"command": command})
	for _, row := range []any{
		map[string]any{"role": "assistant", "toolCalls": []any{map[string]any{"id": id, "function": map[string]string{"name": "bash", "arguments": string(args)}}}},
		map[string]any{"role": "tool", "toolCallId": id, "content": outcome, "presentation": map[string]any{"outcome": outcome, "refused": outcome == "refused"}},
	} {
		if err := enc.Encode(row); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuardUnrunProbe(t *testing.T) {
	for _, outcome := range []string{"refused", "cancelled", "timed out"} {
		t.Run(outcome, func(t *testing.T) {
			root, file, run := staleGuardFixture(t)
			guardUnrunJournal(t, file, "go test -run TestProbe ./pkg", outcome)
			run("go test ./...")
			door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
			t.Cleanup(door.closeVerification)
			if err := door.Report(t.Context(), factory.StageResult{Done: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGuardOwedAndBound(t *testing.T) {
	for _, kind := range []string{"real failure", "refused", "cancelled", "timed out", "no receipt"} {
		t.Run(kind, func(t *testing.T) {
			root, file, run := staleGuardFixture(t)
			if kind == "real failure" {
				run("CHECK_EXIT=7 go test ./...")
			} else if kind != "no receipt" {
				guardUnrunJournal(t, file, "go test ./...", kind)
			}
			parent := &repairReportDoor{}
			door := &receiptStageDoor{StageDoor: parent, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
			t.Cleanup(door.closeVerification)
			stopped := door.stoppedVerification()
			for range 3 {
				err := door.Report(t.Context(), factory.StageResult{Done: true, Output: "Tests pass"})
				if err == nil {
					t.Fatal("accepted missing or failed verification")
				}
				if strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "owed:") {
					t.Errorf("hold must name exactly what is owed on one line: %s", err)
				}
			}
			select {
			case <-stopped:
			default:
				t.Fatal("unchanged evidence exceeded three refusals")
			}
			if parent.last.Done {
				t.Fatal("bounded stop reported success")
			}
		})
	}
}

func TestGuardSameUnrunPass(t *testing.T) {
	root, file, run := staleGuardFixture(t)
	run("go test ./...")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Calls []struct {
			Function struct {
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"toolCalls"`
	}
	if err := json.Unmarshal([]byte(strings.Split(string(raw), "\n")[0]), &row); err != nil {
		t.Fatal(err)
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(row.Calls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	guardUnrunJournal(t, file, args.Command, "refused")
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err != nil {
		t.Fatal(err)
	}
}

func TestGuardOlderPassCannotCoverNewerRefusal(t *testing.T) {
	root, file, run := staleGuardFixture(t)
	run("go test ./...")
	guardUnrunJournal(t, file, "go test -run TestProbe ./pkg", "refused")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Calls []struct {
			Function struct {
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"toolCalls"`
	}
	if err := json.Unmarshal([]byte(strings.Split(string(raw), "\n")[0]), &row); err != nil {
		t.Fatal(err)
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(row.Calls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	guardUnrunJournal(t, file, args.Command, "completed")
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err == nil {
		t.Fatal("old pass erased a newer refused test without a later measured receipt")
	}
}
func TestGuardUnmeasuredSameCommandCannotClearRefusal(t *testing.T) {
	root, file, run := staleGuardFixture(t)
	run("go test ./...")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Calls []struct {
			Function struct {
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"toolCalls"`
	}
	if err := json.Unmarshal([]byte(strings.Split(string(raw), "\n")[0]), &row); err != nil {
		t.Fatal(err)
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(row.Calls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	guardUnrunJournal(t, file, args.Command, "refused")
	guardUnrunJournal(t, file, args.Command, "completed")
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err == nil {
		t.Fatal("old pass erased a newer refused test without a later measured receipt")
	}
}

func TestGuardCompoundRerunClearsMeasuredFailure(t *testing.T) {
	root, file, run := staleGuardFixture(t)
	t.Setenv("CHECK_EXIT", "7")
	run("go test ./a; go test ./b")
	door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err == nil {
		t.Fatal("measured failure did not hold completion")
	}
	t.Setenv("CHECK_EXIT", "0")
	run("go test ./a; go test ./b")
	if err := door.Report(t.Context(), factory.StageResult{Done: true}); err != nil {
		t.Fatal(err)
	}
}
