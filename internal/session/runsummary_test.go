package session

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

func runSummaryFixture(t *testing.T, completer *scriptedCompleter, more ...func(*Config)) (*Agent, *plandb.Store, time.Time) {
	t.Helper()
	t.Setenv("CODEAF_TASK_BELT", "bash")
	dir := t.TempDir()
	store, err := plandb.Open(filepath.Join(dir, planStoreFilename), "run", planRootID, "The run", strings.Repeat("person ask ", 300))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "leaf", Title: "Write handler", Description: "work", ParentID: planRootID}}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 20, 0, 0, 0, time.UTC)
	agent, _ := newTestAgent(t, completer, func(c *Config) {
		c.Place = Place{Dir: dir}
		c.clock = func() time.Time { return now }
		for _, apply := range more {
			apply(c)
		}
	})
	return agent, store, now
}

func TestRunSummaryRefreshStoresFourLinesAndShapeStamp(t *testing.T) {
	client := &scriptedCompleter{steps: []step{finalText("what: make token refresh reliable\nsince: the handler now retries once\nnow: the focused test is passing\nnext: review the change. Nothing needs you.")}}
	agent, store, now := runSummaryFixture(t, client)
	got, ok := agent.RefreshRunSummary(context.Background(), planRootID, now.Add(-2*time.Hour))
	if !ok || got.What != "make token refresh reliable" || got.Since != "the handler now retries once" || got.Now != "the focused test is passing" || got.Next == "" || !got.WrittenAt.Equal(now) {
		t.Fatalf("stored summary = %#v, %v", got, ok)
	}
	if client.requests() != 1 {
		t.Fatalf("requests = %d, want one", client.requests())
	}
	request := messageText(client.request(0)[len(client.request(0))-1])
	if len(request) > 9000 {
		t.Fatalf("bounded request grew to %d bytes", len(request))
	}
	if strings.Count(request, "person ask ") > 140 {
		t.Fatalf("ask was not cut at 1500 characters")
	}
	if entries := store.Contexts(planRootID, runSummaryContextKind, 10); len(entries) != 1 || !strings.Contains(entries[0].Content, `"stamp"`) {
		t.Fatalf("summary context = %#v", entries)
	}
	read, stale := agent.PlanRunSummary(planRootID)
	if stale || read != got {
		t.Fatalf("unchanged read = %#v stale=%v, want %#v false", read, stale, got)
	}
}

func TestRunSummaryRefreshOnlyWhenShapeMovesAndPreservesWhat(t *testing.T) {
	client := &scriptedCompleter{steps: []step{
		finalText("what: keep the original goal\nsince: nothing\nnow: waiting on review\nnext: Nothing needs you."),
		finalText("what: keep the original goal\nsince: review landed\nnow: the run is done\nnext: Nothing needs you."),
	}}
	agent, store, now := runSummaryFixture(t, client)
	first, ok := agent.RefreshRunSummary(context.Background(), planRootID, time.Time{})
	if !ok || first.Since != "" {
		t.Fatalf("first = %#v, %v", first, ok)
	}
	again, ok := agent.RefreshRunSummary(context.Background(), planRootID, now)
	if !ok || again != first || client.requests() != 1 {
		t.Fatalf("unchanged refresh called model: %#v requests=%d", again, client.requests())
	}
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "review", Title: "Review", Description: "check", ParentID: planRootID}}); err != nil {
		t.Fatal(err)
	}
	if _, stale := agent.PlanRunSummary(planRootID); !stale {
		t.Fatal("new task did not stale the summary")
	}
	moved, ok := agent.RefreshRunSummary(context.Background(), planRootID, now.Add(-time.Hour))
	if !ok || moved.What != first.What || moved.Since != "review landed" || client.requests() != 2 {
		t.Fatalf("moved = %#v, %v requests=%d", moved, ok, client.requests())
	}
}

func TestRunSummaryAbsentNeverBroken(t *testing.T) {
	t.Run("no plan", func(t *testing.T) {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
		if got, stale := agent.PlanRunSummary(planRootID); got != (RunPlanSummary{}) || stale {
			t.Fatalf("got %#v stale=%v", got, stale)
		}
		if got, ok := agent.RefreshRunSummary(context.Background(), planRootID, time.Time{}); got != (RunPlanSummary{}) || ok {
			t.Fatalf("got %#v ok=%v", got, ok)
		}
	})
	t.Run("refusal keeps stored", func(t *testing.T) {
		client := &scriptedCompleter{steps: []step{finalText("what: original\nsince: first\nnow: running\nnext: Nothing needs you."), func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("refused") }}}
		agent, store, _ := runSummaryFixture(t, client)
		before, _ := agent.RefreshRunSummary(context.Background(), planRootID, time.Time{})
		if _, err := store.AddMany([]plandb.TaskSpec{{ID: "later", Title: "Later", Description: "x", ParentID: planRootID}}); err != nil {
			t.Fatal(err)
		}
		after, ok := agent.RefreshRunSummary(context.Background(), planRootID, time.Time{})
		if !ok || after != before {
			t.Fatalf("refusal replaced summary: before=%#v after=%#v ok=%v", before, after, ok)
		}
	})
	t.Run("unlabelled stores nothing", func(t *testing.T) {
		agent, store, _ := runSummaryFixture(t, &scriptedCompleter{steps: []step{finalText("all good")}})
		if got, ok := agent.RefreshRunSummary(context.Background(), planRootID, time.Time{}); got != (RunPlanSummary{}) || ok {
			t.Fatalf("got %#v ok=%v", got, ok)
		}
		if entries := store.Contexts(planRootID, runSummaryContextKind, 10); len(entries) != 0 {
			t.Fatalf("stored malformed answer: %#v", entries)
		}
	})
}

// A summary is about ONE run: the task it is asked for and everything under
// it. A sibling's rows are another piece of work, and a sentence about them
// under this run's name would be a sentence about the wrong work.
func TestRunSummaryReadsOnlyTheFamilyUnderItsRoot(t *testing.T) {
	_, store, _ := runSummaryFixture(t, &scriptedCompleter{})
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "other", Title: "Rotate the key", Description: "work", ParentID: planRootID},
		{ID: "under", Title: "Test the handler", Description: "work", ParentID: "leaf"},
	}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, task := range runSummaryFamily(store, "leaf") {
		ids = append(ids, task.ID)
	}
	if got := strings.Join(ids, ","); got != "leaf,under" {
		t.Fatalf("family of leaf = %q, want leaf,under", got)
	}
	if whole := runSummaryFamily(store, planRootID); len(whole) != 4 {
		t.Fatalf("family of the root holds %d tasks, want all 4", len(whole))
	}
	if runSummaryFamily(store, "nobody") != nil {
		t.Fatal("a root the store does not hold has a family")
	}
}

// THE QUESTIONS A RUN HOLDS ARE THE SESSION'S OWN OPEN QUESTIONS, handed in by
// their heads: a question raised or answered makes the lines stale, and the
// model is shown the question in its own words so `next` can lead with it.
func TestRunSummaryStampAndInputCarryTheHeldQuestions(t *testing.T) {
	_, store, now := runSummaryFixture(t, &scriptedCompleter{})
	family := runSummaryFamily(store, planRootID)
	held := []string{"keep the old table for a week?"}
	if runSummaryStamp(family, nil) == runSummaryStamp(family, held) {
		t.Fatal("a held question did not move the stamp")
	}
	input := runSummaryInput(family, held, planRootID, time.Time{}, now, RunPlanSummary{})
	if !strings.Contains(input, "OPEN QUESTIONS\nkeep the old table for a week?\n") {
		t.Fatalf("the held question is missing from the input:\n%s", input)
	}
	if !strings.Contains(input, "LAST LOOK\nnever\n") {
		t.Fatalf("a run never looked at must say so:\n%s", input)
	}
}

// A RUN'S READING IS PAID FOR, SO IT IS IN THE BOOKS. The card's lines are a
// worker-tier call, and until this test they reached the conversation's journal
// and nowhere else: the status line, `/cost` and the machine's spending ledger
// all left them out, which a stub service that billed every call caught at
// three unbanked calls on every senior-dev run.
func TestARunSummaryIsInTheConversationsBooksAndTheLedger(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	client := &scriptedCompleter{steps: []step{pricedText("what: w\nsince: s\nnow: n\nnext: Nothing needs you.", 0.0123)}}
	agent, _, _ := runSummaryFixture(t, client, func(c *Config) { c.usageLedger = ledger })
	if _, ok := agent.RefreshRunSummary(context.Background(), planRootID, time.Time{}); !ok {
		t.Fatal("the refresh stored no summary")
	}
	if got := agent.Usage().CostUSD; math.Abs(got-0.0123) > 1e-9 {
		t.Fatalf("the conversation's books hold $%.4f, want the reading's $0.0123", got)
	}
	FlushUsage()
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	if len(lines) != 1 || math.Abs(lines[0].USD-0.0123) > 1e-9 {
		t.Fatalf("ledger = %+v, want one row of $0.0123", lines)
	}
}

// AN ANSWER THAT CANNOT BE READ WAS STILL PAID FOR. The last good reading
// stands on the card, and the money for the one that could not be used is in
// the books all the same.
func TestARunSummaryAnswerThatCannotBeReadIsStillInTheBooks(t *testing.T) {
	client := &scriptedCompleter{steps: []step{pricedText("not the four lines", 0.004)}}
	agent, _, _ := runSummaryFixture(t, client)
	if _, ok := agent.RefreshRunSummary(context.Background(), planRootID, time.Time{}); ok {
		t.Fatal("an unreadable answer was stored as a summary")
	}
	if got := agent.Usage().CostUSD; math.Abs(got-0.004) > 1e-9 {
		t.Fatalf("the conversation's books hold $%.4f, want the unreadable answer's $0.004", got)
	}
}

// A PROGRAM'S RUN BUYS NO READING. Its store holds one task and a live stage,
// its page is its conversation with codeaf, and four model-written lines about
// one row would say again, for money, what the row already says.
func TestAProgramsRunBuysNoRunSummary(t *testing.T) {
	client := &scriptedCompleter{steps: []step{pricedText("what: w\nsince: s\nnow: n\nnext: n", 0.0123)}}
	agent, store, _ := runSummaryFixture(t, client)
	if err := delegate.WriteProgram(plandb.TaskDir(filepath.Dir(store.Path()), planRootID), delegate.ProgramRecord{Name: "senior-dev"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := agent.RefreshRunSummary(context.Background(), planRootID, time.Time{}); ok {
		t.Fatal("a program's run was given a summary")
	}
	if client.requests() != 0 {
		t.Fatalf("a program's run asked a model %d times for a summary, want none", client.requests())
	}
}

// A RUN WITH NOTHING IN IT BUYS NO READING. The first refresh of a run used to
// be sent before its store held the task, with an empty ask and no rows, and a
// model was paid to summarise nothing.
func TestARunSummaryOfNoRowsMakesNoCall(t *testing.T) {
	client := &scriptedCompleter{steps: []step{pricedText("what: w\nsince: s\nnow: n\nnext: n", 0.0123)}}
	agent, _, _ := runSummaryFixture(t, client)
	if _, ok := agent.RefreshRunSummary(context.Background(), "no-such-root", time.Time{}); ok {
		t.Fatal("a run with no rows was given a summary")
	}
	if client.requests() != 0 {
		t.Fatalf("a run with no rows asked a model %d times, want none", client.requests())
	}
}

// ONE READING AT A TIME FOR ONE RUN. Two surfaces asking in the same moment
// (a window's own refresh and the page it just opened) both found the reading
// stale and both paid for one; the second now keeps the last reading.
func TestTwoRefreshesOfOneRunAtOnceBuyOneReading(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		entered <- struct{}{}
		<-release
		cost := 0.0123
		response := textResponse("what: w\nsince: s\nnow: n\nnext: n")
		response.Usage.Cost = &cost
		return response, nil
	}}}
	agent, _, _ := runSummaryFixture(t, client)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		agent.RefreshRunSummary(context.Background(), planRootID, time.Time{})
	}()
	<-entered
	agent.RefreshRunSummary(context.Background(), planRootID, time.Time{})
	close(release)
	wg.Wait()
	if client.requests() != 1 {
		t.Fatalf("two refreshes at once asked a model %d times, want one", client.requests())
	}
}
