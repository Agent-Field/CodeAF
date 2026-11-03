package main

import (
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

// TestFactoryMeasuredOnlyPublication tests the journal-to-publication boundary.
// Shell commands run against a local stub, never Go or a model provider.
func TestFactoryMeasuredOnlyPublication(t *testing.T) {
	type call struct {
		command, output, status string
		observe                 bool
		exits                   []int
		want                    bool
		dropExit                bool
	}
	type row struct {
		name  string
		calls []call
	}
	pass := "PASS\n"
	rows := []row{}
	for _, status := range []string{"", "ok", "failed", "cancelled", "timed out", "refused"} {
		for _, measurement := range []int{-1, 0, 7} {
			command := "go test ./..."
			if measurement == 7 {
				command = "CHECK_EXIT=7 " + command
			}
			var exits []int
			if measurement >= 0 {
				exits = []int{measurement}
			}
			label := status
			if label == "" {
				label = "none"
			}
			rows = append(rows, row{fmt.Sprintf("status=%s_exit=%d", label, measurement), []call{{command, pass, status, measurement >= 0, exits, measurement == 0 && (status == "" || status == "ok"), false}}})
		}
	}
	for _, tc := range []struct {
		name, command, output string
		observe               bool
		exits                 []int
		want                  bool
	}{
		{"semicolon_mask", "CHECK_EXIT=7 go test ./... ; true", pass, true, []int{7}, false},
		{"or_mask", "CHECK_EXIT=7 go test ./... || true", pass, true, []int{7}, false},
		{"tee_mask", "CHECK_EXIT=7 go test ./... | tee log", pass, true, []int{7}, false},
		{"semicolon_zero", "go test ./... ; true", pass, true, []int{0}, true},
		{"or_zero", "go test ./... || true", pass, true, []int{0}, true},
		{"tee_zero", "go test ./... | tee log", pass, true, []int{0}, true},
		{"echo_PASS", "echo PASS", pass, false, nil, false},
		{"echo_ok", "echo 'ok  fake'", "ok  fake\n", false, nil, false},
		{"printf_PASS", "printf 'PASS\\n'; exit 0", pass, false, nil, false},
		{"background", "go test ./... &", pass, true, []int{0}, true},
		{"true_and", "true && go test ./...", pass, true, []int{0}, true},
		{"bash_c", "bash -c 'go test ./...'", pass, true, []int{0}, false},
		{"sh_script", "sh script.sh", pass, true, nil, false},
		{"make_test", "make test", pass, true, nil, false},
		{"mixed_exits", "go test ./first; CHECK_EXIT=7 go test ./second", pass, true, []int{0, 7}, false},
		{"all_zero", "go test ./first; go test ./second", pass, true, []int{0, 0}, true},
		{"old_PASS", "go test ./...", pass, false, nil, false},
		{"old_ok", "go test ./...", "ok  fake\n", false, nil, false},
		{"ok_unmeasured_blank", "go test ./...", "", false, nil, false},
		{"ok_unmeasured_FAIL", "go test ./...", "FAIL\nPASS\n", false, nil, false},
		{"ok_unmeasured_exit_text", "go test ./...", "PASS\nCommand exited with code 7", false, nil, false},
		{"absolute_uninstrumented", "ABS_GO test ./...", pass, true, nil, false},
	} {
		status := "ok"
		if strings.HasPrefix(tc.name, "old_") {
			status = ""
		}
		rows = append(rows, row{tc.name, []call{{tc.command, tc.output, status, tc.observe, tc.exits, tc.want, false}}})
	}
	rows = append(rows,
		row{"failed_different_rerun", []call{{"CHECK_EXIT=7 go test ./first", pass, "ok", true, []int{7}, false, false}, {"go test ./second", pass, "ok", true, []int{0}, true, false}}},
		row{"failed_unmeasured_rerun", []call{{"go test ./...", pass, "failed", true, []int{7}, false, false}, {"go test ./...", pass, "ok", false, nil, false, false}}},
		row{"zero_unknown_compound", []call{{"go test ./first; go test ./second", pass, "ok", true, []int{0, 0}, false, true}}},
		row{"passed_failed_rerun", []call{{"go test ./...", pass, "ok", true, []int{0}, false, false}, {"go test ./...", pass, "failed", true, []int{7}, false, false}}},
	)
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "journal.jsonl")
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			stub := "#!/bin/sh\nprintf 'PASS\\n'\nexit \"${CHECK_EXIT:-0}\"\n"
			if err := os.WriteFile(filepath.Join(bin, "go"), []byte(stub), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "script.sh"), []byte("go test ./...\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n\tgo test ./...\n"), 0600); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(file)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			enc := json.NewEncoder(f)
			var commands []string
			for i, c := range tc.calls {
				cmd := strings.ReplaceAll(c.command, "ABS_GO", filepath.Join(bin, "go"))
				commands = append(commands, cmd)
				id := fmt.Sprint(i)
				wrapped := cmd
				if c.observe {
					wrapped, err = verification.PrepareCallIn(file, "", cmd, id)
					if err != nil {
						t.Fatal(err)
					}
				}
				run := exec.Command("bash", "-c", wrapped)
				run.Dir = root
				exitEnv := "0"
				if len(c.exits) == 1 {
					exitEnv = fmt.Sprint(c.exits[0])
				}
				run.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "CHECK_EXIT="+exitEnv)
				if _, err := run.CombinedOutput(); err != nil {
					if _, ok := err.(*exec.ExitError); !ok {
						t.Fatal(err)
					}
				}
				checks, err := verification.Read(file)
				if err != nil {
					t.Fatal(err)
				}
				var actual []int
				for _, check := range checks {
					if check.CallID == id {
						if check.Exit == nil {
							actual = append(actual, -1)
						} else {
							actual = append(actual, *check.Exit)
						}
					}
				}
				if fmt.Sprint(actual) != fmt.Sprint(c.exits) {
					t.Fatalf("fixture measured %v want %v", actual, c.exits)
				}
				if c.dropExit {
					paths, _ := filepath.Glob(file + ".checks/call-*/check-*.exit")
					if len(paths) == 0 {
						t.Fatal("no exit to remove")
					}
					if err := os.Remove(paths[len(paths)-1]); err != nil {
						t.Fatal(err)
					}
				}
				args, _ := json.Marshal(map[string]string{"command": cmd})
				if err := enc.Encode(map[string]any{"role": "assistant", "toolCalls": []any{map[string]any{"id": id, "function": map[string]string{"name": "bash", "arguments": string(args)}}}}); err != nil {
					t.Fatal(err)
				}
				tool := map[string]any{"role": "tool", "toolCallId": id, "content": c.output}
				if c.status != "" {
					tool["presentation"] = map[string]any{"invoked": c.status != "refused", "refused": c.status == "refused", "outcome": c.status}
				}
				if err := enc.Encode(tool); err != nil {
					t.Fatal(err)
				}
			}
			notes, _ := (&stageConversation{file: file, dir: root}).ExecutionNotes()
			body := factory.PublishBody(factory.Item{Title: "matrix", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Result: &factory.StageResult{Notes: notes}}}}}, "")
			wants := map[string]bool{}
			for i, c := range tc.calls {
				wants[commands[i]] = wants[commands[i]] || c.want
			}
			for cmd, want := range wants {
				got := strings.Contains(body, "Passed locally: `"+cmd+"`.")
				if got != want {
					t.Errorf("command=%q publishes=%v want=%v", cmd, got, want)
				}
			}
		})
	}
}

func TestFactorySetupRefusalWithdrawsPublication(t *testing.T) {
	root, file, run := staleGuardFixture(t)
	run("go test ./...")
	checks, err := verification.Read(file)
	if err != nil || len(checks) != 1 {
		t.Fatalf("fixture checks: %v: %v", checks, err)
	}
	guardUnrunJournal(t, file, checks[0].Command, verification.SetupFailurePrefix+" observer unavailable")
	notes, _ := (&stageConversation{file: file, dir: root}).ExecutionNotes()
	body := factory.PublishBody(factory.Item{Title: "setup refusal", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Result: &factory.StageResult{Notes: notes}}}}}, "")
	if strings.Contains(body, "Passed locally:") {
		t.Fatal("setup-refused rerun retained earlier publication", body)
	}
}

func TestStageCommandReceiptsExcludeUnmeasuredCounts(t *testing.T) {
	for _, status := range []string{"", "ok", "failed", "refused", "cancelled", "timed out"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "journal.jsonl")
			f, err := os.Create(file)
			if err != nil { t.Fatal(err) }
			enc := json.NewEncoder(f)
			args, _ := json.Marshal(map[string]string{"command": "go test ./..."})
			for _, row := range []any{
				map[string]any{"role": "assistant", "toolCalls": []any{map[string]any{"id": "a", "function": map[string]string{"name": "bash", "arguments": string(args)}}}},
				map[string]any{"role": "tool", "toolCallId": "a", "content": "--- PASS: TestPrinted (0s)\n--- FAIL: TestPrintedFailure (0s)\n--- SKIP: TestPrintedSkip (0s)\nPASS\n", "presentation": map[string]any{"outcome": status, "refused": status == "refused", "invoked": status != "refused"}},
			} {
				if err := enc.Encode(row); err != nil { t.Fatal(err) }
			}
			if err := f.Close(); err != nil { t.Fatal(err) }
			notes, counts := (&stageConversation{file: file, dir: root}).ExecutionNotes()
			if counts != nil || strings.Contains(strings.Join(notes, "\n"), `"tests":`) {
				t.Fatalf("unmeasured named output acquired counts: %v: %+v", notes, counts)
			}
		})
	}
}
