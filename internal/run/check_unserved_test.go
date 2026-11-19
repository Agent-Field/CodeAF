package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// run21CheckerError is the error run21's rota checker came home with, built
// the way the session's turn loop builds it: the spent endpoint ladder, wrapped
// in the turn's retry count.
func run21CheckerError() error {
	return fmt.Errorf("after 0 retries: %w", &provider.RefusalError{
		Model:    "google/gemini-4-carbon",
		Params:   []string{"tools", "tool_choice", "reasoning", "provider.ignore"},
		Stripped: []string{"provider.require_parameters", "reasoning", "tools"},
		Attempts: 4,
		Refusal: &provider.APIError{Status: 404, Message: "Your request contains encrypted reasoning or compaction content that can only be processed " +
			"by the provider that created it (google-ai-studio), but your request's provider.ignore preference excludes that provider."},
	})
}

// run21Factory is the rota run: the worker builds the rota and reports it, and
// the review round's check comes home with checkErr instead of an answer.
func run21Factory(checkErr error) WorkerFactory {
	return func(plandb.Task) Worker {
		return workerFunc(func(_ context.Context, task plandb.Task) (Report, error) {
			if task.Role == plandb.RoleCheck {
				return Report{Steps: 5}, checkErr
			}
			return Report{Result: "Rota sheet tabs Rota Coverage Hours built", Steps: 34}, nil
		})
	}
}

// THE RUN21 SEQUENCE: the worker finished ("✓ t-1 done", "finished in the
// store"), then its checker's model could not be served — the router refused
// every shape of the request — and the task settled failed, drawn in red over a
// finished rota. A check whose model could not be served is a fact about the
// checker, exactly as a check out of budget is (#2841): the run answers done,
// carries the real reason typed, and the person is asked.
func TestRun21AFinishedWorksCheckWhoseModelCouldNotBeServedIsNotAFailure(t *testing.T) {
	store := boundedRunStore(t, "Build next week's shift rota")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: run21Factory(run21CheckerError()), Limits: Limits{ReviewRound: true}})

	if outcome != OutcomeDone {
		t.Fatalf("outcome = %q, want %q: a finished rota whose check could not be served is not a failed run (summary %+v)", outcome, OutcomeDone, summary)
	}
	want := `the check could not finish: the provider would not serve its model google/gemini-4-carbon and said "Your request contains encrypted reasoning or compaction content that can only be processed by the provider that created it (google-ai-studio), but your request's provider.ignore preference excludes that provider."`
	if summary.Unchecked != want {
		t.Fatalf("summary.Unchecked = %q,\nwant %q", summary.Unchecked, want)
	}
	if summary.Result != "Rota sheet tabs Rota Coverage Hours built" {
		t.Fatalf("summary.Result = %q, want the worker's own report", summary.Result)
	}
	if root := store.Task(store.RootID()); root.Status != plandb.StatusDone {
		t.Fatalf("root = %s (%q), want done", root.Status, root.Error)
	}
	checks := checkTasks(store)
	if len(checks) != 1 || checks[0].Status != plandb.StatusDone || checks[0].Result != want {
		t.Fatalf("check = %+v, want it ended on the real stop as its plain account", checks)
	}
}

// Every infrastructure ending is the same fact, whatever shape it arrived in:
// a provider's refusal, a stream the guard cut, a connection that stayed down.
func TestEveryWayACheckersModelCanFailToAnswerLeavesTheWorkStanding(t *testing.T) {
	for name, checkErr := range map[string]error{
		"refused":    fmt.Errorf("after 3 retries: %w", &provider.APIError{Status: 502, Message: "upstream error"}),
		"stalled":    fmt.Errorf("turn: %w", &provider.StreamCut{Reason: provider.CutSilent}),
		"connection": fmt.Errorf("turn: %w", &provider.ConnectionUnavailableError{}),
	} {
		t.Run(name, func(t *testing.T) {
			store := boundedRunStore(t, "Build next week's shift rota")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: run21Factory(checkErr), Limits: Limits{ReviewRound: true}})
			if outcome != OutcomeDone || !strings.HasPrefix(summary.Unchecked, checkStopLead) {
				t.Fatalf("outcome = %q, unchecked = %q; want done and a check that says why it could not finish", outcome, summary.Unchecked)
			}
		})
	}
}

// A CHECK THAT ENDED ON ITS OWN ACCOUNT STILL FAILS. Running into the step cap
// or the no-action bound is the checker's work going wrong, not its model being
// unreachable, and nothing here may soften it.
func TestACheckThatFailedOnItsOwnAccountStillFails(t *testing.T) {
	store := boundedRunStore(t, "Build next week's shift rota")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1,
		Factory: run21Factory(errors.New("4 replies in a row carried no action")), Limits: Limits{ReviewRound: true}})
	if outcome == OutcomeDone || summary.Unchecked != "" {
		t.Fatalf("outcome = %q, unchecked = %q; a check that failed on its own account is not an infrastructure stop", outcome, summary.Unchecked)
	}
}

// AND THE RUN'S OWN ENDING IS NEVER THE CHECKER'S. A provider timeout and the
// run's wall can wrap the same context error, so the question is asked of the
// worker's context, not of the words.
func TestTheRunsOwnEndingIsNotACheckThatCouldNotFinish(t *testing.T) {
	wall := fmt.Errorf("the run's wall stopped the worker: %w", context.DeadlineExceeded)
	if checkCouldNotFinish(workerReturn{err: wall, ended: true}) {
		t.Fatal("the run's wall read as a check that could not finish")
	}
	if checkCouldNotFinish(workerReturn{err: fmt.Errorf("stopped: %w", context.Canceled), ended: true}) {
		t.Fatal("a person's stop read as a check that could not finish")
	}
	if !checkCouldNotFinish(workerReturn{err: run21CheckerError()}) {
		t.Fatal("run21's refusal did not read as a check that could not finish")
	}
}
