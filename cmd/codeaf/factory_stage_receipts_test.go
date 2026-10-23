package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

func TestStageCommandReceiptsPreserveCwdAndExcludeSummaryPASS(t *testing.T) {
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	journal := `{"role":"assistant","toolCalls":[{"id":"t1","function":{"name":"bash","arguments":"{\"command\":\"cd nested && GOPROXY=direct go test -race -v ./internal/retrieval\"}"}}]}
{"role":"tool","toolCallId":"t1","content":"--- PASS: TestA (0.00s)\n    --- PASS: TestA/sub (0.00s)\n--- PASS: TestB (0.00s)\nPASS\nok  example 1s\n"}
`
	if err := os.WriteFile(file, []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	c := stageConversation{file: file, dir: "/checkout"}
	notes, counts := c.ExecutionNotes()
	if len(notes) != 1 || !strings.Contains(notes[0], "GOPROXY=direct") || !strings.Contains(notes[0], `"cwd":"/checkout"`) || counts == nil || counts.Passed != 3 {
		t.Fatalf("receipt: %v %+v", notes, counts)
	}
}

func TestStageReceiptsKeepLargestCommandAndResolveRerunFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	journal := `{"role":"assistant","toolCalls":[{"id":"a","function":{"name":"bash","arguments":"{\"command\":\"go test -v ./...\"}"}}]}
{"role":"tool","toolCallId":"a","content":"--- PASS: TestA (0s)\n--- PASS: TestB (0s)\n--- PASS: TestC (0s)\nPASS\nok  example 1s\n"}
{"role":"assistant","toolCalls":[{"id":"b","function":{"name":"bash","arguments":"{\"command\":\"go test -v -run TestA ./...\"}"}}]}
{"role":"tool","toolCallId":"b","content":"--- FAIL: TestA (0s)\nFAIL\nCommand exited with code 1"}
{"role":"assistant","toolCalls":[{"id":"c","function":{"name":"bash","arguments":"{\"command\":\"go test -v -run TestA ./...\"}"}}]}
{"role":"tool","toolCallId":"c","content":"--- PASS: TestA (0s)\nPASS\nok  example 1s\n"}
`
	if err := os.WriteFile(file, []byte(journal), 0600); err != nil {
		t.Fatal(err)
	}
	c := stageConversation{file: file, dir: "/checkout"}
	notes, counts := c.ExecutionNotes()
	if counts == nil || counts.Passed != 3 || counts.Command != "go test -v ./..." {
		t.Fatalf("a later subset replaced full command: %+v", counts)
	}
	for _, n := range notes {
		if strings.HasPrefix(n, "Unresolved verification") {
			t.Fatal("successful rerun did not resolve failure", notes)
		}
	}
}

// These go through a real shell and the persisted journal/exit receipts rather
// than giving the collector invented PASS strings or a model's claimed result.
func TestFactoryVerificationReceiptsDoNotConfuseChecksAndReporting(t *testing.T) {
	cases := []struct {
		name     string
		commands []string
		blocked  bool
	}{
		{"zero failures is success", []string{"go test -v ./... | grep -c -e '--- FAIL:'"}, false},
		{"live chained pass fail skip counts", []string{`cd nested && export PATH="$PATH" && go test ./... -count=1 -v 2>&1 | grep -cE '^\s*--- PASS:' && echo "---" && go test ./... -count=1 -v 2>&1 | grep -cE '^\s*--- FAIL:' && echo "---" && go test ./... -count=1 -v 2>&1 | grep -cE '^\s*--- SKIP:'`}, false},
		{"zero failures must not hide skipped second target", []string{`go test ./... | grep -c -e '--- FAIL:' && CHECK_EXIT=7 go vet ./...`}, true},
		{"real failure in chained counts", []string{`CHECK_EXIT=7 go test ./... | grep -c -e '--- FAIL:' && echo "---" && CHECK_EXIT=7 go test ./... | grep -c -e '--- SKIP:'`}, true},
		{"corrected output verbosity clears same check", []string{"CHECK_EXIT=7 go test -v ./...", "go test ./..."}, false},
		{"headers and changed setup do not retain stale failure", []string{`CHECK_EXIT=7 go vet ./... && echo VET_CLEAN`, `export PATH="$PATH" && go vet ./... && echo clean`}, false},
		{"saved log and exit do not retain stale failure", []string{`CHECK_EXIT=7 go test ./... > check.log 2>&1; ec=$?; echo "EXIT=$ec"; grep -E 'test result|FAIL' check.log | head -8`, `export PATH="$PATH" && go test ./...`}, false},
		{"saved log real failure survives successful reporter", []string{`CHECK_EXIT=7 go test ./... > check.log 2>&1; ec=$?; echo "EXIT=$ec"; cat check.log`}, true},
		{"saved log subset cannot clear failure", []string{`CHECK_EXIT=7 go test ./... > check.log 2>&1; ec=$?; echo "EXIT=$ec"; cat check.log`, `go test -run TestA ./...`}, true},
		{"saved log different target cannot clear failure", []string{`GOOS=linux CHECK_EXIT=7 go test ./... > check.log 2>&1; ec=$?; echo "EXIT=$ec"; cat check.log`, `GOOS=darwin go test ./...`}, true},
		{"saved exit cannot hide failed suffix", []string{`go test ./... > check.log 2>&1; ec=$?; echo "EXIT=$ec"; false`, `go test ./...`}, true},
		{"actual failure survives tail", []string{"CHECK_EXIT=7 go test -v ./... | tail -2"}, true},
		{"corrected filter clears failed check", []string{"CHECK_EXIT=7 go test -v ./... | tail -2", "go test -v ./... | grep -c -e '--- FAIL:'"}, false},
		{"unrelated subset cannot clear", []string{"CHECK_EXIT=7 go test -v ./...", "go test -v -run TestA ./..."}, true},
		{"other directory cannot clear", []string{"CHECK_EXIT=7 go test -v ./...", "cd nested && go test -v ./..."}, true},
		{"other environment cannot clear", []string{"CHECK_EXIT=7 go test -v ./...", "GOOS=linux go test -v ./..."}, true},
		{"compound build still blocks", []string{"go test -v ./... && false"}, true},
		{"plain rerun cannot erase failed compound suffix", []string{"go test -v ./... | cat; false", "go test -v ./..."}, true},
		{"plain rerun cannot erase failed build after pipe", []string{"go test -v ./... | tail -2 && false", "go test -v ./..."}, true},
		{"compound second check still blocks", []string{"go test -v ./... && CHECK_EXIT=7 go vet ./..."}, true},
		{"same compound corrected", []string{"go test -v ./... && test -f fixed", "touch fixed", "go test -v ./... && test -f fixed"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "transcript.jsonl")
			bin := filepath.Join(root, "bin")
			for _, dir := range []string{bin, filepath.Join(root, "nested")} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nprintf '%s\\n' '--- PASS: TestA (0s)' PASS\nexit \"${CHECK_EXIT:-0}\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			journal, err := os.Create(file)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			enc := json.NewEncoder(journal)
			for i, command := range tc.commands {
				command = "export PATH=" + strconv.Quote(bin) + ":\"$PATH\"; " + command
				wrapped, err := verification.Prepare(file, command)
				if err != nil {
					t.Fatal(err)
				}
				run := exec.Command("bash", "-o", "pipefail", "-c", wrapped)
				run.Dir = root
				raw, runErr := run.CombinedOutput()
				output := string(raw)
				if runErr != nil {
					if exit, ok := runErr.(*exec.ExitError); ok {
						output += fmt.Sprintf("\nCommand exited with code %d", exit.ExitCode())
					} else {
						t.Fatal(runErr)
					}
				}
				id := strconv.Itoa(i)
				args, _ := json.Marshal(map[string]string{"command": command})
				if err := enc.Encode(map[string]any{"role": "assistant", "toolCalls": []any{map[string]any{"id": id, "function": map[string]string{"name": "bash", "arguments": string(args)}}}}); err != nil {
					t.Fatal(err)
				}
				if err := enc.Encode(map[string]string{"role": "tool", "toolCallId": id, "content": output}); err != nil {
					t.Fatal(err)
				}
			}
			notes, _ := (&stageConversation{file: file, dir: root}).ExecutionNotes()
			blocked := false
			for _, note := range notes {
				blocked = blocked || strings.HasPrefix(note, "Unresolved verification command failed:")
			}
			if blocked != tc.blocked {
				t.Fatalf("blocked=%v want %v; notes %v", blocked, tc.blocked, notes)
			}
		})
	}
}

func TestFactoryObserverSetupFailureRemainsBlockingWithoutReceiptFiles(t *testing.T) {
	for _, command := range []string{"go test ./...", "npm run test", "cat README.md"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "transcript.jsonl")
			// A real storage refusal occurs before the original command runs.
			// Removing its cause before collection ensures Read alone cannot
			// accidentally make this test pass by reporting a storage error.
			if err := os.WriteFile(file+".checks", []byte("not a directory"), 0600); err != nil {
				t.Fatal(err)
			}
			_, setupErr := verification.Prepare(file, command)
			if setupErr == nil {
				t.Fatal("invalid observer storage accepted")
			}
			if err := os.Remove(file + ".checks"); err != nil {
				t.Fatal(err)
			}
			journal, err := os.Create(file)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			enc := json.NewEncoder(journal)
			writeResult := func(id, output string) {
				t.Helper()
				args, _ := json.Marshal(map[string]string{"command": command})
				if err := enc.Encode(map[string]any{"role": "assistant", "toolCalls": []any{map[string]any{"id": id, "function": map[string]string{"name": "bash", "arguments": string(args)}}}}); err != nil {
					t.Fatal(err)
				}
				if err := enc.Encode(map[string]string{"role": "tool", "toolCallId": id, "content": output}); err != nil {
					t.Fatal(err)
				}
			}
			blocked := func() bool {
				t.Helper()
				notes, _ := (&stageConversation{file: file, dir: root}).ExecutionNotes()
				for _, note := range notes {
					if strings.HasPrefix(note, verification.FailurePrefix) {
						return true
					}
				}
				return false
			}
			writeResult("refused", verification.SetupFailurePrefix+" "+setupErr.Error())
			if !blocked() {
				t.Fatal("observer setup failure allowed unexecuted check to pass")
			}
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"go", "npm"} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nprintf 'PASS\\n'\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("ready"), 0600); err != nil {
				t.Fatal(err)
			}
			wrapped, err := verification.Prepare(file, command)
			if err != nil {
				t.Fatal(err)
			}
			retry := exec.Command("bash", "-o", "pipefail", "-c", wrapped)
			retry.Dir = root
			retry.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, err := retry.CombinedOutput()
			if err != nil {
				t.Fatalf("retry failed: %v: %s", err, output)
			}
			writeResult("retried", string(output))
			if blocked() {
				t.Fatal("successful exact retry did not resolve observer setup failure")
			}
		})
	}
}

func TestCompactedToolResultsContributeOneBlocker(t *testing.T) {
	file := filepath.Join(t.TempDir(), "session.jsonl")
	rows := `{"role":"assistant","toolCalls":[{"id":"once","function":{"name":"bash","arguments":"{\"command\":\"cd /missing && go test ./...\"}"}}]}` + "\n" + `{"role":"tool","toolCallId":"once","content":"cd failed"}` + "\n"
	if err := os.WriteFile(file, []byte(strings.Repeat(rows, 4)), 0600); err != nil {
		t.Fatal(err)
	}
	notes, _ := (&stageConversation{file: file, dir: t.TempDir()}).ExecutionNotes()
	count := 0
	for _, n := range notes {
		if strings.HasPrefix(n, verification.FailurePrefix) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("compaction multiplied blockers: %v", notes)
	}
}

func TestVerificationCheckpointBypassesTerminalRepairGuardAndRunningWorkDefersWatchdog(t *testing.T) {
	parent := &repairReportDoor{}
	running := true
	door := &receiptStageDoor{StageDoor: parent, repair: true, reason: "missing result", workRunning: func() bool { return running }}
	t.Cleanup(door.closeVerification)
	if err := door.Report(t.Context(), factory.StageResult{Done: true, Checkpoint: true}); err != nil {
		t.Fatal(err)
	}
	door.stopVerification("report timeout")
	if door.stopped || door.timer == nil || parent.reports != 1 {
		t.Fatal("running check was stopped by reporting watchdog")
	}
	running = false
	door.stopVerification("report timeout")
	if !door.stopped || parent.reports != 2 || parent.last.Repair.Decision != "needs_you" {
		t.Fatal("idle unresolved recovery was not bounded")
	}
}

func TestObservedFreshnessDoesNotInventPolicyProofCommands(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("cargo test --test cli regression")
	conv := &stageConversation{file: file, dir: root, verification: &receiptStageDoor{requireFresh: true}}
	notes, _ := conv.ExecutionNotes()
	for _, n := range notes {
		if strings.HasPrefix(n, verification.FailurePrefix) {
			t.Fatal(n)
		}
	}
	os.WriteFile(filepath.Join(root, "code.txt"), []byte("changed after verification"), 0600)
	notes, _ = conv.ExecutionNotes()
	stale := false
	for _, n := range notes {
		if strings.HasPrefix(n, verification.FailurePrefix) && strings.Contains(n, "stale") {
			stale = true
		}
	}
	if !stale {
		t.Fatal("ordinary observed Test accepted stale verification", notes)
	}
	policy := filepath.Join(t.TempDir(), "policy.jsonl")
	os.WriteFile(policy, nil, 0600)
	notes, _ = (&stageConversation{file: policy, dir: t.TempDir(), verification: &receiptStageDoor{requireFresh: true}}).ExecutionNotes()
	for _, n := range notes {
		if strings.HasPrefix(n, verification.FailurePrefix) {
			t.Fatalf("policy-only proof invented a command requirement: %s", n)
		}
	}
}

type withdrawingReportDoor struct {
	repairReportDoor
	rejects    int
	failReport bool
}

func (d *withdrawingReportDoor) Report(ctx context.Context, r factory.StageResult) error {
	if d.failReport {
		return errors.New("new report rejected")
	}
	return d.repairReportDoor.Report(ctx, r)
}

func (d *withdrawingReportDoor) RejectReport() {
	d.rejects++
	d.last.Checkpoint = true
}

func TestReceiptReportRefusalsWithdrawEarlierTerminalCandidate(t *testing.T) {
	for _, kind := range []string{"stopped", "missing repair", "inconsistent fixed", "no_change with failure", "fixed with failure", "underlying refusal"} {
		t.Run(kind, func(t *testing.T) {
			root, file, run := repairCheckFixture(t)
			parent := &withdrawingReportDoor{repairReportDoor: repairReportDoor{last: factory.StageResult{Done: true}, reports: 1}}
			d := &receiptStageDoor{StageDoor: parent, file: file, dir: root, repair: true}
			t.Cleanup(d.closeVerification)
			r := factory.StageResult{Done: true}
			switch kind {
			case "stopped":
				d.stopped = true
			case "inconsistent fixed":
				r.Exit = 1
				r.Repair = &factory.CIRepairResult{Decision: "fixed"}
			case "no_change with failure", "fixed with failure":
				run("CHECK_EXIT=7 cargo test --test cli regression")
				r.Repair = &factory.CIRepairResult{Decision: "fixed"}
				if kind == "no_change with failure" {
					r.Repair.Decision = "no_change"
				}
			case "underlying refusal":
				d.repair = false
				parent.failReport = true
			}
			if err := d.Report(t.Context(), r); err == nil {
				t.Fatal("invalid newer report accepted")
			}
			if parent.rejects != 1 || !parent.last.Checkpoint || parent.reports != 1 {
				t.Fatalf("older terminal survived: %+v", parent)
			}
		})
	}
	parent := &withdrawingReportDoor{}
	d := &receiptStageDoor{StageDoor: parent, repair: true}
	if err := d.Report(t.Context(), factory.StageResult{Done: true, Checkpoint: true}); err != nil || parent.rejects != 0 {
		t.Fatalf("accepted progress report was invalidated: %v %+v", err, parent)
	}
	d.RejectReport()
	if parent.rejects != 1 {
		t.Fatal("parser rejection was not forwarded")
	}
}
