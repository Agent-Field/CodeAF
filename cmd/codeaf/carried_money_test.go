//go:build !windows

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// carriedOwingFunnel answers at once with no usage block and owes the call's
// receipt, which lands after a delay — the provider's own order: owed before
// the fetch, answered after the sink has the money.
type carriedOwingFunnel struct{ late time.Duration }

func (f carriedOwingFunnel) completerFor(string) modelapi.Completer { return f }

func (f carriedOwingFunnel) CompleteWithMessages(ctx context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	done := provider.ReceiptPendingFrom(ctx)()
	sink := provider.ReconcileSinkFrom(ctx)
	go func() {
		defer done()
		time.Sleep(f.late)
		sink(provider.Reconciled{Billed: provider.Billed{Model: request.Model, PromptTokens: 52139, CompletionTokens: 4895, Cost: 0.058188488}, Found: true})
	}()
	return &ai.Response{Model: request.Model, Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "cut short"}}},
		FinishReason: "stop",
	}}}, nil
}

// A SHELL RUN WAITS FOR THE PRICE OF ITS LAST CALL, SAYS SO, AND FILES IT. The
// call's receipt lands after the program has exited; the run waits for it
// before its last lines and before this process's ledger closes, tells the
// person why on stderr, files the row under the run, and keeps the program's
// own clock in its record folder.
func TestAShellRunWaitsForItsLastCallsPriceAndKeepsItsClock(t *testing.T) {
	_, printed := hostWithRealChild(t, 0)
	carriedModels = func() (carriedRoad, error) {
		return carriedRoad{completerFor: carriedOwingFunnel{late: 300 * time.Millisecond}.completerFor, seat: "seat/model"}, nil
	}
	told := &lockedBuffer{}
	previous := carriedStderr
	carriedStderr = told
	t.Cleanup(func() { carriedStderr = previous })
	workspace := t.TempDir()
	before := time.Now()
	err := runCarried(fakeCarriedProgram(), []string{"--calls", "1", "--dir", workspace, "fix it"})
	if code := exitCodeOf(err); code != 0 {
		t.Fatalf("left with %d (%v):\n%s", code, err, printed)
	}
	if !strings.Contains(told.String(), "waiting up to 1m 10s for the price of 1 call that was cut short") {
		t.Fatalf("stderr = %q, want the wait said", told.String())
	}
	if !strings.Contains(printed.String(), "  1 model call · $0.06") {
		t.Fatalf("the last line does not hold the late call:\n%s", printed)
	}
	record := newestRecord(t)
	rows := ledgerRowsFor(t, workspace)
	if len(rows) != 1 || !rows[0].Reconciled || rows[0].USD != 0.058188488 || rows[0].Task != filepath.Base(record) {
		t.Fatalf("ledger rows = %+v, want the late receipt filed under the run %q", rows, filepath.Base(record))
	}
	program, ok := delegate.ReadProgram(record)
	if !ok || program.Name != fakeCarried || program.StartedAt.Before(before) || !program.EndedAt.After(program.StartedAt) ||
		len(program.Stages) == 0 {
		t.Fatalf("program record = %+v (%v), want its name, stages and the program's own start and end", program, ok)
	}
}

// THE LAST LINE SAYS HOW LONG THE PROGRAM RAN, the way a person says it, and
// leaves off a figure nobody measured rather than writing a zero. IT IS THE LAST
// LINE EVEN THOUGH THE RUN KEPT A RECORD: every real run has a record folder by
// its end, and the line naming it used to follow the summary, so the manual's
// "last line" was the folder's path.
func TestAShellRunsLastLineSaysHowLongItRan(t *testing.T) {
	for _, row := range []struct {
		calls int
		spent float64
		took  time.Duration
		want  string
	}{
		{calls: 277, spent: 2.295385, took: 22*time.Minute + 51*time.Second, want: "  277 model calls · $2.30 · 22m 51s\n"},
		{took: 2*time.Hour + 5*time.Minute, want: "  2h 5m\n"},
		{calls: 1, took: 400 * time.Millisecond, want: "  1 model call\n"},
	} {
		printed := &lockedBuffer{}
		inv := &delegate.Invocation{Program: fakeCarriedProgram(), Workspace: t.TempDir()}
		record := t.TempDir()
		view := newCarriedView(printed, inv, record)
		view.calls = row.calls
		view.Terminal(delegate.Terminal{Status: delegate.StatusPass, Message: "done"})
		_ = view.end(delegate.Result{}, nil, false, row.spent, row.took)
		if !strings.HasSuffix(printed.String(), row.want) {
			t.Fatalf("printed %q, want it to end %q", printed.String(), row.want)
		}
		if !strings.Contains(printed.String(), "  the run's record is in "+record+"\n") {
			t.Fatalf("printed %q, want the record folder named before the last line", printed.String())
		}
	}
}

// A SHELL RUN'S TIME IS THE PROGRAM'S, NOT THE DRAIN'S. The launch returns only
// once the program's stdout is drained, and a helper the program left holding
// stdout keeps that open for up to the grace after the program itself exited.
// The shell took its end after the launch returned, so the same program read up
// to fifteen seconds longer from a shell than from a conversation, whose worker
// already ends the clock at the process's own exit.
func TestAShellRunsTimeEndsWhenTheProgramExitedAndNotWhenItsOutputDrained(t *testing.T) {
	_, printed := hostWithRealChild(t, 0.01)
	const linger = 2 * time.Second
	before := time.Now()
	err := runCarried(fakeCarriedProgram(), []string{"--calls", "1", "--linger", linger.String(), "--dir", t.TempDir(), "fix it"})
	if code := exitCodeOf(err); code != 0 {
		t.Fatalf("left with %d (%v):\n%s", code, err, printed)
	}
	if waited := time.Since(before); waited < linger {
		t.Fatalf("the run returned after %v, before the helper let go of stdout at %v", waited, linger)
	}
	program, ok := delegate.ReadProgram(newestRecord(t))
	if !ok || program.EndedAt.Before(program.StartedAt) {
		t.Fatalf("program record = %+v (%v), want the program's own start and end", program, ok)
	}
	if ran := program.EndedAt.Sub(program.StartedAt); ran >= linger {
		t.Fatalf("the record says the program ran %v, which is the drain's %v and not the process's", ran, linger)
	}
}
