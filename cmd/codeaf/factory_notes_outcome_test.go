package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

func TestExecutionNotesReceiptOutcomes(t *testing.T) {
	cases := []struct {
		name, output, outcome string
		refused, invoked      bool
	}{
		{"passed", "PASS\nok  example 1s\n", "passed", false, true},
		{"ok without pass evidence", "test result: ok\n", "completed", false, true},
		{"ok with exit line", "PASS\nCommand exited with code 7", "completed", false, true},
		{"ok with failure output", "PASS\nFAIL\n", "completed", false, true},
		{"failed", "FAIL\nCommand exited with code 1", "failed", false, true},
		{"approval without resolver", `needs approval but no resolver is attached: critical command "rm -rf /*"`, "refused", false, false},
		{"legacy policy", "denied by approval rule: protected files", "refused", false, false},
		{"policy", "denied by approval rule: protected files", "refused", true, false},
		{"typed refusal", "This command requires an operator", "refused", true, false},
		{"cancelled", "PASS\n\nCommand aborted", "cancelled", false, true},
		{"timed out", "PASS\n\nCommand timed out after 1 seconds", "timed out", false, true},
		{"deadline", "cut after 1s; output so far:\nPASS\n", "timed out", false, true},
		{"printed cancellation is output", "PASS\n\nCommand aborted", "passed", false, true},
		{"printed timeout is output", "cut after 1s; output so far:\nPASS\n", "passed", false, true},
		{"printed refusal is output", "needs approval but no resolver is attached: example\nPASS\n", "passed", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "journal.jsonl")
			f, err := os.Create(file)
			if err != nil {
				t.Fatal(err)
			}
			enc := json.NewEncoder(f)
			status := "failed"
			if tc.outcome == "passed" || tc.outcome == "completed" {
				status = "ok"
			}
			if tc.outcome == "cancelled" || tc.outcome == "timed out" {
				status = tc.outcome
			}
			if tc.refused {
				status = "refused"
			}
			command := "go test ./..."
			if tc.name == "approval without resolver" {
				command = "set -e\nrm -rf /tmp/ledger-race-check\ncp -r /checkout /tmp/ledger-race-check\ncd /tmp/ledger-race-check\ngo test -race -run TestConcurrentAddProbe ./... 2>&1 | head -40\ngo test ./... 2>&1 | tail -5"
			}
			args, _ := json.Marshal(map[string]string{"command": command})
			var presentation any = map[string]any{"audience": "tool", "refused": tc.refused, "invoked": tc.invoked, "outcome": status}
			if tc.name == "approval without resolver" || tc.name == "legacy policy" {
				presentation = nil
			}
			for _, row := range []any{
				map[string]any{"role": "assistant", "toolCalls": []any{map[string]any{"id": "probe", "function": map[string]string{"name": "bash", "arguments": string(args)}}}},
				map[string]any{"role": "tool", "toolCallId": "probe", "content": tc.output, "presentation": presentation},
			} {
				if err := enc.Encode(row); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			notes, _ := (&stageConversation{file: file, dir: t.TempDir()}).ExecutionNotes()
			found, blocked := false, false
			for _, note := range notes {
				blocked = blocked || strings.HasPrefix(note, verification.FailurePrefix)
				if raw, ok := strings.CutPrefix(note, "Command receipt: "); ok {
					var receipt struct{ Outcome, Reason string }
					if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
						t.Fatal(err)
					}
					found = true
					if receipt.Outcome != tc.outcome {
						t.Errorf("outcome=%q want %q", receipt.Outcome, tc.outcome)
					}
					if tc.outcome == "refused" && receipt.Reason != tc.output {
						t.Errorf("reason=%q want %q", receipt.Reason, tc.output)
					}
				}
			}
			if !found {
				t.Fatal("missing command receipt", notes)
			}
			if tc.outcome != "passed" && !blocked {
				t.Fatal("completion guard lost unresolved command", notes)
			}
		})
	}
}
