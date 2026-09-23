//go:build !windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
	"github.com/Agent-Field/codeaf/internal/session"
)

// carriedFunnel is the person's model road as a test writes it: every call
// answered with words and billed at cost, the way the provider's decode bills
// one.
type carriedFunnel struct {
	mu     sync.Mutex
	cost   float64
	models []string
}

func (f *carriedFunnel) completerFor(string) modelapi.Completer { return f }

func (f *carriedFunnel) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	f.mu.Lock()
	f.models = append(f.models, request.Model)
	f.mu.Unlock()
	if sink := provider.BillingSinkFrom(ctx); sink != nil {
		sink(provider.Billed{Model: request.Model, PromptTokens: 100, CompletionTokens: 10, Cost: f.cost})
	}
	return &ai.Response{Model: request.Model, Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "answered " + messages[len(messages)-1].Content[0].Text}}},
		FinishReason: "stop",
	}}}, nil
}

func (f *carriedFunnel) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.models...)
}

// hostWithRealChild carries the fake program, marks the environment so the
// child this test binary starts runs it, and replaces the person's profile
// with a funnel costing cost a call. It answers the funnel and what the shell
// run prints, in a buffer that can be read while the run is still writing it
// (chatv3_host_duty_test.go's lockedBuffer).
func hostWithRealChild(t *testing.T, cost float64) (*carriedFunnel, *lockedBuffer) {
	t.Helper()
	restore := builtin.Override([]delegate.Delegate{fakeCarriedProgram()})
	t.Cleanup(restore)
	t.Setenv(carriedChildEnv, "1")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
	calling := &carriedFunnel{cost: cost}
	previousRoad, previousOut, previousGrace := carriedModels, carriedStdout, carriedGrace
	carriedModels = func() (carriedRoad, error) {
		return carriedRoad{completerFor: calling.completerFor, seat: "seat/model"}, nil
	}
	printed := &lockedBuffer{}
	carriedStdout = printed
	carriedGrace = 5 * time.Second
	t.Cleanup(func() { carriedModels, carriedStdout, carriedGrace = previousRoad, previousOut, previousGrace })
	return calling, printed
}

// ledgerRowsFor is this machine's spending ledger's rows for one workspace,
// once the writers have drained. Other tests in this binary share the ledger,
// and a workspace of this test's own is what tells its rows apart.
func ledgerRowsFor(t *testing.T, workspace string) []session.UsageLine {
	t.Helper()
	session.FlushUsage()
	file, err := os.Open(session.UsageLedgerPath())
	if err != nil {
		t.Fatalf("the spending ledger was never written: %v", err)
	}
	defer file.Close()
	var rows []session.UsageLine
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var row session.UsageLine
		if json.Unmarshal(scanner.Bytes(), &row) == nil && row.Workspace == workspace {
			rows = append(rows, row)
		}
	}
	return rows
}

// newestRecord is the most recent shell run's record folder for the fake.
func newestRecord(t *testing.T) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(home.Join("v3", "carried", fakeCarried), "*"))
	if len(matches) == 0 {
		t.Fatal("the shell run kept no record folder")
	}
	newest := matches[0]
	for _, match := range matches[1:] {
		if match > newest {
			newest = match
		}
	}
	return newest
}

// A PERSON'S SHELL RUN IS THE SAME TWO PROCESSES A CHAT'S RUN IS: this process
// serves the model API, the program runs as a real child of this executable
// with its own command's flag carried through, every call it makes is
// metered onto this machine's spending ledger once, the conversation is kept
// in the run's record folder, and a person reads the stage, each step, each
// call and the ending as lines.
func TestAShellRunHostsTheModelAPIForARealChildAndPrintsItsWork(t *testing.T) {
	calling, printed := hostWithRealChild(t, 0.004)
	workspace := t.TempDir()
	err := runCarried(fakeCarriedProgram(), []string{"--calls", "2", "--dir", workspace, "fix", "the", "flaky", "test"})
	if code := exitCodeOf(err); code != 0 {
		t.Fatalf("the shell run left with %d (%v):\n%s", code, err, printed)
	}
	out := printed.String()
	for _, line := range []string{
		fakeCarried + " · working in " + workspace,
		"implement · running",
		"  model: ask · answered question 1: fix the flaky test",
		"  openrouter/deepseek/deepseek-v4-flash-0731 · 100 in · 10 out · $0.0040",
		"verify · pass",
		fakeCarried + " finished: submitted and verified",
		"  " + fakeCarried + "'s model said: the test is fixed",
		"  2 model calls · $0.0080",
		"  the run's record is in ",
	} {
		if !strings.Contains(out, line) {
			t.Fatalf("the shell run never printed %q:\n%s", line, out)
		}
	}
	// THE PERSON'S OWN FLAG REACHED THE PROGRAM: two questions, not one, and
	// the ask's `openrouter/` spelling reached the funnel as the ask.
	if models := calling.seen(); len(models) != 2 || models[0] != "openrouter/deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("the funnel was asked for %q", models)
	}
	rows := ledgerRowsFor(t, workspace)
	if len(rows) != 2 || rows[0].USD != 0.004 || rows[0].Model != "openrouter/deepseek/deepseek-v4-flash-0731" || rows[0].Seat != session.SeatWorker {
		t.Fatalf("ledger rows = %+v, want one per call", rows)
	}
	record := newestRecord(t)
	turns, err := delegate.ReadTurns(record, 0)
	if err != nil || len(turns) != 2 || turns[1].Reply != "answered question 2: fix the flaky test" || turns[1].CostUSD != 0.004 {
		t.Fatalf("the kept conversation = %+v (%v)", turns, err)
	}
}

// WITH --json THE RECORDS PASS THROUGH AS RECORDS, and nothing a person reads
// is mixed into them: one reader of the protocol reads the host's stdout the
// way it reads a program's.
func TestAShellRunWithJSONPassesTheRecordsThrough(t *testing.T) {
	_, printed := hostWithRealChild(t, 0.001)
	err := runCarried(fakeCarriedProgram(), []string{"--json", "--dir", t.TempDir(), "fix it"})
	if code := exitCodeOf(err); code != 0 {
		t.Fatalf("left with %d:\n%s", code, printed)
	}
	reading, readErr := delegate.Read(strings.NewReader(printed.String()), nil)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if reading.Ignored != 0 || reading.Hello == nil || reading.Steps != 1 || reading.Terminal == nil || reading.Terminal.Status != delegate.StatusPass {
		t.Fatalf("reading = %+v, want the program's records and nothing else:\n%s", reading, printed)
	}
	if reading.Terminal.Claim() != "the test is fixed" {
		t.Fatalf("the terminal's data did not pass through: %+v", reading.Terminal)
	}
}

// A LIMIT THE PERSON SET STOPS THE PROGRAM FROM OUTSIDE: the dollar ceiling is
// reached by the second metered call, the program is stopped, a third call is
// refused before it is made, and the run leaves on the limit rung.
func TestAShellRunStopsItsProgramAtTheDollarCeiling(t *testing.T) {
	calling, printed := hostWithRealChild(t, 0.004)
	workspace := t.TempDir()
	err := runCarried(fakeCarriedProgram(), []string{"--max-cost", "0.005", "--calls", "4", "--wait", "--dir", workspace, "fix it"})
	if code := exitCodeOf(err); code != int(exitLimit) {
		t.Fatalf("left with %d, want the limit rung:\n%s", code, printed)
	}
	if !strings.Contains(printed.String(), fakeCarried+" was stopped at a limit you set") {
		t.Fatalf("the ending does not name the limit:\n%s", printed)
	}
	if models := calling.seen(); len(models) != 2 {
		t.Fatalf("the funnel was asked %d times, want the two calls that reached the ceiling", len(models))
	}
	if rows := ledgerRowsFor(t, workspace); len(rows) != 2 {
		t.Fatalf("ledger rows = %+v", rows)
	}
}

// A RUN WITH NO BRIEF IS NOT STARTED: nothing is spent, no child is started,
// and the run leaves on the first rung.
func TestAShellRunWithNoBriefStartsNothing(t *testing.T) {
	calling, printed := hostWithRealChild(t, 0.001)
	err := runCarried(fakeCarriedProgram(), []string{"--dir", t.TempDir()})
	if code := exitCodeOf(err); code != int(exitCannotRun) {
		t.Fatalf("left with %d, want the rung for a run that could not start", code)
	}
	if len(calling.seen()) != 0 || printed.String() != "" {
		t.Fatalf("a run with no brief did something: %d calls, printed %q", len(calling.seen()), printed.String())
	}
}

// CTRL-C IS A STOP: the program is sent SIGTERM, writes how it ended inside
// its grace, and the run leaves as work that did not finish.
func TestAShellRunIsStoppedCleanlyWhenItsContextEnds(t *testing.T) {
	_, printed := hostWithRealChild(t, 0.001)
	inv, err := delegate.Parse(fakeCarriedProgram(), []string{"--calls", "1", "--wait", "--dir", t.TempDir(), "fix it"}, printed)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Once the program has asked its one question it is waiting to be
		// stopped; that is when a person reaches for ctrl-c.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && !strings.Contains(printed.String(), "model: ask") {
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	started := time.Now()
	err = runCarriedHost(ctx, inv)
	if code := exitCodeOf(err); code != int(exitIncomplete) {
		t.Fatalf("left with %d, want the rung for work that did not finish:\n%s", code, printed)
	}
	if !strings.Contains(printed.String(), fakeCarried+" did not finish: stopped before it finished") {
		t.Fatalf("the program's own ending did not arrive inside its grace:\n%s", printed)
	}
	if time.Since(started) > 8*time.Second {
		t.Fatalf("the stop took %s; the program was not stopped by SIGTERM", time.Since(started))
	}
}
