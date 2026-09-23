//go:build !windows

package run_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// fakeDelegate writes a shell program that stands in for codeaf running a
// program — a stage, a spend, two steps, then body — and answers the program's
// definition and the setup that starts the script in codeaf's place.
func fakeDelegate(t *testing.T, body string) (delegate.Delegate, run.DelegateSetup) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake.sh")
	program := "#!/bin/sh\n" + strings.Join([]string{
		`if [ -n "$FAKE_ARGS" ]; then printf '%s\n' "$@" > "$FAKE_ARGS"; fi`,
		`echo '{"type":"stage","stage":"implement","status":"running"}'`,
		`echo '{"type":"spend","cost_usd":0.05}'`,
		`echo '{"type":"step","command":"bash: go test ./...","observation":"ok"}'`,
		`echo '{"type":"step","command":"edit: a.go"}'`,
		`echo '{"type":"spend","cost_usd":0.11}'`,
		body,
	}, "\n") + "\n"
	if err := os.WriteFile(script, []byte(program), 0o755); err != nil {
		t.Fatal(err)
	}
	return delegate.Delegate{Name: "fake", Summary: "a fake program", Default: "run"}, run.DelegateSetup{Exe: script}
}

func passLine(claim string) string {
	return `echo '{"type":"terminal","status":"pass","message":"submitted and verified","data":{"cost_usd":0.12,"submission_reason":"` + claim + `","status":"pass"}}'`
}

func TestDelegateWorkerRecordsStepsBanksSpendAndReportsTheEnding(t *testing.T) {
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKE_ARGS", args)
	workspace := t.TempDir()
	m, setup := fakeDelegate(t, passLine("tests are green"))
	worker := run.NewDelegateWorker(store, workspace, m, setup, 2.5, 0)

	var banked []float64
	ctx := run.WithSpendBank(runContext(t), func(usd float64) { banked = append(banked, usd) })
	report, err := worker.Run(ctx, *store.Task(store.RootID()))
	if err != nil {
		t.Fatalf("the delegate's run failed: %v", err)
	}
	if report.Steps != 2 {
		t.Fatalf("steps = %d, want the two step records the program sent", report.Steps)
	}
	// The report's dollars are the terminal's total, which is higher than the
	// last streamed figure; the bank saw the streamed figures as they rose.
	if report.USD != 0.12 {
		t.Fatalf("usd = %v, want the terminal's 0.12", report.USD)
	}
	if len(banked) != 2 || banked[0] != 0.05 || banked[1] != 0.11 {
		t.Fatalf("banked = %v, want the two rising spend records", banked)
	}
	if !strings.Contains(report.Result, "submitted and verified") || !strings.Contains(report.Result, "fake's model said: tests are green") || !strings.Contains(report.Result, "fake observed: pass") {
		t.Fatalf("result = %q, want the message, the claim and the observation as separate sentences", report.Result)
	}
	// The brief the program was handed is the task's description, and the
	// ceiling is the run's.
	got, _ := os.ReadFile(args)
	if want := "fake\nrun\n--json\n--dir\n" + workspace + "\n--max-cost\n2.5\n--\ndrive the plan to the ground\n"; string(got) != want {
		t.Fatalf("argv =\n%s\nwant\n%s", got, want)
	}
	// The trajectory: the opening line, two steps, the ending.
	steps, err := run.Trajectory(storeDir, store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Command != "bash: go test ./..." || steps[0].Observation != "ok" || steps[1].Step != 2 {
		t.Fatalf("trajectory steps = %+v", steps)
	}
	lines := rawTrajectory(t, storeDir, store.RootID())
	if len(lines) != 4 {
		t.Fatalf("the trajectory holds %d lines, want the opening, two steps and the ending", len(lines))
	}
	end := endLine(t, lines)
	if end.Steps != 2 || !strings.HasPrefix(end.Reason, "finished: ") {
		t.Fatalf("ending = %+v", end)
	}
	// The live step was cleared with the process, the spend row names the
	// delegate, and stderr went to the task's folder.
	if live := store.LiveSteps(); len(live) != 0 {
		t.Fatalf("live steps = %+v, want none after the program ended", live)
	}
	if _, err := os.Stat(filepath.Join(plandb.TaskDir(storeDir, store.RootID()), "delegate-stderr.log")); err != nil {
		t.Fatalf("no stderr file beside the trajectory: %v", err)
	}
	spend := store.SpendSummary()
	if got := spend.ByModel["delegate/fake"]; got.USD != 0.12 || got.Calls != 1 {
		t.Fatalf("spend by model = %+v, want one row of 0.12 under the delegate's name", spend.ByModel)
	}
}

func TestDelegateWorkerReportsAFailedEndingAsAnError(t *testing.T) {
	store := runOpenStore(t)
	m, setup := fakeDelegate(t, `echo '{"type":"terminal","status":"fail","message":"unsubmitted","data":{"cost_usd":0.2,"status":"unsubmitted"}}'`)
	worker := run.NewDelegateWorker(store, t.TempDir(), m, setup, 0, 0)
	report, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err == nil || !strings.Contains(err.Error(), "fake did not finish: unsubmitted") {
		t.Fatalf("err = %v", err)
	}
	if report.USD != 0.2 || report.Steps != 2 {
		t.Fatalf("report = %+v, want the money and the steps kept on a failed ending", report)
	}
}

func TestDelegateWorkerNamesAnExitWithoutATerminal(t *testing.T) {
	store := runOpenStore(t)
	m, setup := fakeDelegate(t, "exit 7")
	worker := run.NewDelegateWorker(store, t.TempDir(), m, setup, 0, 0)
	_, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err == nil || err.Error() != "fake exited 7 without a terminal record; its last stage was implement" {
		t.Fatalf("err = %v", err)
	}
}

func TestDelegateWorkerComesHomeWithTheContextsEndingWhenTheRunStopsIt(t *testing.T) {
	store := runOpenStore(t)
	m, setup := fakeDelegate(t, strings.Join([]string{
		`trap 'echo "{\"type\":\"terminal\",\"status\":\"budget-exhausted\",\"message\":\"told to stop\",\"data\":{\"cost_usd\":0.11}}"; exit 0' TERM`,
		`sleep 30 &`,
		`wait $!`,
	}, "\n"))
	worker := run.NewDelegateWorker(store, t.TempDir(), m, setup, 0, 0)
	ctx, cancel := context.WithCancel(runContext(t))
	go func() {
		// Once the store has the program's live step, the program is past its
		// trap line and the signal will be caught.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if live := store.LiveSteps(); len(live) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	report, err := worker.Run(ctx, *store.Task(store.RootID()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the context's own so the run records the cut", err)
	}
	if report.USD != 0.11 {
		t.Fatalf("usd = %v, want what was spent before the stop", report.USD)
	}
	lines := rawTrajectory(t, filepath.Dir(store.Path()), store.RootID())
	end := endLine(t, lines)
	if end.Reason != "stopped by the run: fake said told to stop" {
		t.Fatalf("ending reason = %q", end.Reason)
	}
}

// The whole road: a run of one task whose root is the delegate, driven by the
// supervisor to done, with the delegate's words as the run's result.
func TestARunSeatsTheDelegateOnItsRootAndEndsDone(t *testing.T) {
	store := runOpenStore(t)
	m, setup := fakeDelegate(t, passLine("all green"))
	factory := run.DelegateFactory(store, t.TempDir(), m, setup, run.Limits{CostUSD: 5}, nil)
	outcome, summary := run.Start(runContext(t), run.Spec{
		Store:     store,
		Workspace: t.TempDir(),
		Title:     "The run",
		Brief:     "drive the plan to the ground",
		Slots:     1,
		Limits:    run.Limits{CostUSD: 5},
		Factory:   factory,
	})
	if outcome != run.OutcomeDone {
		t.Fatalf("outcome = %q, want done", outcome)
	}
	if !strings.Contains(summary.Result, "fake's model said: all green") {
		t.Fatalf("result = %q", summary.Result)
	}
	if summary.USD != 0.12 || summary.Steps != 2 || summary.Nodes != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusDone {
		t.Fatalf("root status = %q", root.Status)
	}
}

// A run whose dollar ceiling the delegate's streamed spend crosses is ended by
// the run on the limit word, with the program terminated and its own terminal
// kept.
func TestARunEndsADelegateThatCrossesTheCostCeiling(t *testing.T) {
	store := runOpenStore(t)
	m, setup := fakeDelegate(t, strings.Join([]string{
		`trap 'echo "{\"type\":\"terminal\",\"status\":\"budget-exhausted\",\"message\":\"stopped\",\"data\":{\"cost_usd\":0.11}}"; exit 0' TERM`,
		`sleep 30 &`,
		`wait $!`,
	}, "\n"))
	factory := run.DelegateFactory(store, t.TempDir(), m, setup, run.Limits{CostUSD: 0.10}, nil)
	outcome, summary := run.Start(runContext(t), run.Spec{
		Store: store, Workspace: t.TempDir(), Slots: 1,
		Limits:  run.Limits{CostUSD: 0.10},
		Factory: factory,
	})
	if outcome != run.OutcomeLimit || summary.Limit != run.LimitCost {
		t.Fatalf("outcome = %q limit = %q, want the cost limit", outcome, summary.Limit)
	}
	if len(summary.Cut) != 1 {
		t.Fatalf("cut = %v, want the root cut by the run's own ending", summary.Cut)
	}
}

// TWO BUILDS, ONE RUN: a child that says another protocol version than this
// build reads is stopped before it spends, and the reason names the fix.
func TestDelegateWorkerStopsAChildOfAnotherBuild(t *testing.T) {
	store := runOpenStore(t)
	script := filepath.Join(t.TempDir(), "newer.sh")
	program := "#!/bin/sh\n" + strings.Join([]string{
		`echo '{"type":"hello","protocol":99,"delegate":"fake"}'`,
		`trap 'exit 0' TERM`,
		`sleep 30 &`,
		`wait $!`,
	}, "\n") + "\n"
	if err := os.WriteFile(script, []byte(program), 0o755); err != nil {
		t.Fatal(err)
	}
	worker := run.NewDelegateWorker(store, t.TempDir(), delegate.Delegate{Name: "fake", Default: "run"}, run.DelegateSetup{Exe: script, Grace: time.Second}, 0, 0)
	started := time.Now()
	_, err := worker.Run(runContext(t), *store.Task(store.RootID()))
	if err == nil || !strings.Contains(err.Error(), "restart codeaf to run fake") || errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the rebuild named and not the run's own ending", err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatal("the mismatched child was not stopped")
	}
}
