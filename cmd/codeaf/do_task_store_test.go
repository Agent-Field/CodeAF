package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

type taskRunJournal struct {
	spend []taskdoor.Spend
	steps []taskdoor.Step
	end   taskdoor.Finished
}

type taskBillingProvider struct {
	session.Completer
	callsFile string
}

func (p taskBillingProvider) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	file, err := os.OpenFile(p.callsFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.WriteString("call\n")
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return nil, err
	}
	return p.Completer.CompleteWithMessages(ctx, messages, options...)
}

func (j *taskRunJournal) Append(event any) error {
	switch event := event.(type) {
	case taskdoor.Spend:
		j.spend = append(j.spend, event)
	case taskdoor.Step:
		j.steps = append(j.steps, event)
	case taskdoor.Finished:
		j.end = event
	}
	return nil
}

// The subprocess exits immediately after the real do road returns, so the
// parent cannot accidentally flush its queued usage rows on the child's behalf.
func TestTaskDoBillHelper(t *testing.T) {
	if os.Getenv("CODEAF_TASK_DO_BILL_HELPER") != "1" {
		return
	}
	// This package's TestMain deliberately clears credentials/profile and
	// selects the older belt. Reapply only this fixture's disposable inputs.
	t.Setenv("HOME", os.Getenv("CODEAF_TEST_TASK_RUN_HOME"))
	t.Setenv("CODEAF_HOME", os.Getenv("CODEAF_TEST_TASK_RUN_HOME"))
	t.Setenv("CODEAF_PROFILE_DIR", os.Getenv("CODEAF_TEST_TASK_RUN_PROFILE"))
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	value := func(flag string) string {
		for n, arg := range os.Args {
			if arg == flag && n+1 < len(os.Args) {
				return os.Args[n+1]
			}
		}
		return ""
	}
	err := doErrand(doRequest{task: "write out.txt", workspace: value("--dir"), runStore: value("--task-store"), asJSON: true, timeout: 45 * time.Second,
		stdout: os.Stdout, stderr: os.Stderr, newBeltCompleter: func(string) session.Completer {
			return taskBillingProvider{finishingSeat(0.01), filepath.Join(filepath.Dir(value("--task-store")), "provider-calls")}
		}})
	os.Exit(exitCodeOf(err))
}

// Contracts 5 and 12: the actual do run keeps the requested private record,
// and the executor's per-call ledger source sums to that run's own receipt.
func TestTheTaskDoorKeepsTheRealDoRecordAndItsWholeBill(t *testing.T) {
	beltRunEnv(t)
	home, err := os.MkdirTemp("", "rn-l1-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", home)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	attempt := filepath.Join(home, "att_0000000000000081")
	record := filepath.Join(attempt, "run")
	work := filepath.Join(attempt, "work")
	if err := os.MkdirAll(attempt, 0700); err != nil {
		t.Fatal(err)
	}
	// A received task has its own Git history. The do receipt reads changed
	// files from that history, so a plain directory would not exercise it.
	if err := os.Rename(beltRepoWorkspace(t), work); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	jsonFile, err := os.Create(filepath.Join(attempt, "do.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer jsonFile.Close()
	logFile, err := os.Create(filepath.Join(attempt, "do.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestTaskDoBillHelper$", "--", "--dir", work, "--task-store", record)
	cmd.Env = append(os.Environ(), "CODEAF_TASK_DO_BILL_HELPER=1", "CODEAF_HOME="+filepath.Join(home, "bootstrap"), "CODEAF_TEST_TASK_RUN_HOME="+home, "CODEAF_TEST_TASK_RUN_PROFILE="+os.Getenv("CODEAF_PROFILE_DIR"))
	cmd.Stdout, cmd.Stderr = jsonFile, logFile
	err = cmd.Run()
	jsonRaw, _ := os.ReadFile(jsonFile.Name())
	logRaw, _ := os.ReadFile(logFile.Name())
	stdout.Write(jsonRaw)
	stderr.Write(logRaw)
	if err != nil {
		t.Fatalf("run: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(record, "plandb.db")); err != nil {
		t.Fatal("private run was not kept", err)
	}
	if stores, _ := filepath.Glob(filepath.Join(home, "runs", "codeaf-do-*")); len(stores) != 0 {
		t.Fatalf("record escaped attempt: %v", stores)
	}
	if err := os.WriteFile(filepath.Join(attempt, "events.jsonl"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	journal := &taskRunJournal{}
	a := &taskdoor.Attempt{ID: filepath.Base(attempt), Dir: attempt}
	if err := taskdoor.NewExecutor(taskdoor.ExecutorConfig{Ledger: session.UsageLedgerPath()}).Adopt(t.Context(), a, journal); err != nil {
		t.Fatal(err)
	}
	var bill struct {
		Spend float64 `json:"spend_usd"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &bill); err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, row := range journal.spend {
		sum += row.Line.USD
	}
	providerCalls, err := os.ReadFile(filepath.Join(attempt, "provider-calls"))
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.spend) != len(strings.Fields(string(providerCalls))) {
		t.Fatalf("journal charges=%d provider calls=%q", len(journal.spend), providerCalls)
	}
	if journal.end.Outcome != "done" {
		t.Fatalf("completed child did not finish normally: %+v", journal.end)
	}
	if len(journal.end.Changed) != 1 || journal.end.Changed[0] != "out.txt" {
		t.Fatalf("changed files are not portable: %v", journal.end.Changed)
	}
	var order uint64
	for _, line := range journal.steps {
		if line.Step.RecordedOrder <= order {
			t.Fatalf("steps lost write order: %+v", journal.steps)
		}
		order = line.Step.RecordedOrder
	}
	if len(journal.spend) < 3 || bill.Spend <= 0 || math.Abs(sum-bill.Spend) > 1e-9 {
		t.Fatalf("journal=%+v sum=%g actual bill=%g", journal.spend, sum, bill.Spend)
	}
}

// The task-store flag is internal serve plumbing, so the ordinary do help
// must not promise a user-facing option for it.
func TestTheTaskStoreFlagIsAbsentFromDoHelp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	out, _ := captureUsage(t)
	if code := exitCodeOf(runDo([]string{"--help"})); code != 0 {
		t.Fatalf("help exit=%d", code)
	}
	text := out.String()
	if strings.Contains(text, "--"+taskdoor.TaskStoreFlag) {
		t.Fatalf("private flag appeared in help: %s", text)
	}
}

// The task's settled limits and effort travel to the real run, while this
// computer's own spending bound continues to constrain it.
func TestTheTaskDoorHonorsTheSettledTaskControls(t *testing.T) {
	for _, kind := range []string{"steps", "spend", "no progress"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			beltRunEnv(t)
			t.Setenv("CODEAF_TELEMETRY", "off")
			t.Setenv("DO_NOT_TRACK", "1")
			attempt := t.TempDir()
			record := filepath.Join(attempt, "run")
			work := beltRepoWorkspace(t)
			settled := taskdoor.Start{Effort: "high"}
			switch kind {
			case "steps":
				settled.MaxSteps = 2
			case "spend":
				settled.Limits.USD = 0.02
			case "no progress":
				settled.NoProgress = 2
			}
			raw, err := json.Marshal(settled)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(attempt, "request.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			seat := &beltSeat{ever: func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				calls++
				if rung := provider.ReasoningEffortFrom(ctx); rung != provider.EffortHigh {
					t.Errorf("effort=%s", rung)
				}
				reply := beltToolReply("printf 'same answer'")
				cost := 0.01
				reply.Usage.Cost = &cost
				return reply, nil
			}}
			out, err := runErrand(doRequest{task: "work", workspace: work, runStore: record, timeout: 30 * time.Second, stderr: &strings.Builder{}, newBeltCompleter: func(string) session.Completer { return seat }}, stubErrandSeats())
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("settled %s allowed %d provider calls, outcome=%+v", kind, calls, out)
			}
			if kind == "spend" && out.status() != exitLimit {
				t.Fatalf("spending bound did not stop run: %+v", out)
			}
		})
	}
}
