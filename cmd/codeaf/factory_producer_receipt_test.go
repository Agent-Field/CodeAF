package main

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"os"
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
		if strings.HasPrefix(note, "Command receipt:") && !strings.Contains(note, `"outcome":"passed"`) {
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
