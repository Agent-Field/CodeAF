package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// A RUN'S SPEND IS THE CONVERSATION'S, AND NOT THE RUNNING TURN'S. The run's
// dollars reach the conversation's books, but a run works beside the turns: a
// turn running (or abandoned) while the run spends is not the turn that spent
// it, so its share does not move. The run is handed the conversation's own id
// to file its ledger rows under.
func TestARunsSpendIsFoldedIntoTheConversationAndNotTheRunningTurn(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run ended")
	double.summary.USD = 0.42
	registerBeltRunEngine(t, double)

	dir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run ended"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.SessionFile = Place{Dir: dir}.Transcript()
		config.AskConsent = false
	})
	agent.mu.Lock()
	agent.turnSpend = Usage{Input: 5, Output: 2, Calls: 1, CostUSD: 0.11}
	agent.mu.Unlock()
	updates, stopUpdates := agent.WatchTaskUpdates()
	defer stopUpdates()

	if _, _, _, err := agent.StartTask(context.Background(), "account for this run", false); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	<-double.entered
	close(double.release)
	lastTaskUpdate(t, updates)

	if got := agent.Usage().CostUSD; got != 0.42 {
		t.Fatalf("conversation cost = %v, want the run's $0.42 once", got)
	}
	agent.mu.Lock()
	running := agent.turnSpend
	agent.mu.Unlock()
	if running.CostUSD != 0.11 || running.Input != 5 || running.Calls != 1 {
		t.Fatalf("the run's spend moved the running turn's share: %+v", running)
	}
	double.mu.Lock()
	conversation := double.spec.Conversation
	double.mu.Unlock()
	if conversation == "" || conversation != agent.journalID() {
		t.Fatalf("the run was handed conversation %q, want this conversation's own id %q", conversation, agent.journalID())
	}
}

// A PROGRAM'S CALL IS FOLDED WHOLE, AND EACH DOLLAR ONCE. A call metered by a
// program's model API reaches the books with its tokens, its cached share and
// one call, even from a service that reports no price; the run's running total
// adds only what the calls did not already carry — a bash worker's spend — and
// float dust between two sums of the same calls is not a line of its own. The
// fold writes nothing to the machine's ledger: the run's worker wrote it.
func TestARunsCallsAreFoldedWholeAndEachDollarOnce(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	fold := &beltFold{agent: agent}
	fold.charge(RunCharge{Model: "deepseek/deepseek-v4-pro", TokensIn: 100, TokensOut: 10, Cached: 60, USD: 0.05})
	fold.total(0.05 + 1e-17)
	// A service that reports no price: its tokens are still the conversation's.
	fold.charge(RunCharge{Model: "gpt-5.6-sol", TokensIn: 200, TokensOut: 20})
	fold.total(0.05)
	// A worker that reports only its total: the remainder, once.
	fold.total(0.08)
	fold.total(0.08)

	usage := agent.Usage()
	if usage.Input != 300 || usage.Output != 30 || usage.CacheRead != 60 || usage.Calls != 2 || usage.Turns != 0 {
		t.Fatalf("conversation usage = %+v, want both calls' tokens and two calls", usage)
	}
	if usage.CostUSD < 0.08-1e-12 || usage.CostUSD > 0.08+1e-12 {
		t.Fatalf("conversation cost = %v, want $0.08: each dollar once", usage.CostUSD)
	}
	FlushUsage()
	if lines, _ := ReadUsage(ledger, time.Time{}); len(lines) != 0 {
		t.Fatalf("the fold wrote %d ledger rows, want none", len(lines))
	}
}
