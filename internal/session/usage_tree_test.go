package session

// WHAT A CONVERSATION AND THE WORK IT STARTED HAVE SPENT, WHILE THE WORK IS
// STILL RUNNING.
//
// A node's money reaches the conversation's own books only when the node closes
// ([Agent.foldTaskUsage]), so a surface reading those books alone says the
// smaller number for as long as the work lasts — two hours and fifty dollars, on
// the run that produced issue #145. The ledger has had every one of those calls
// on it all along; what it did not have was the conversation they belong to.
// These tests pin the row that carries it and the rollup that reads it.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// treeNear is money compared the way money made of two floats has to be.
func treeNear(got, want float64) bool { return got-want < 0.000001 && want-got < 0.000001 }

// THE ACCEPTANCE ITSELF: a task that has spent something is on the
// conversation's total BEFORE the task closes, and the conversation's own books
// are still behind — which is what makes it a test of the gap rather than of
// the fold.
func TestATasksSpendIsOnTheConversationsTotalBeforeTheTaskCloses(t *testing.T) {
	// A home of this test's own, so that nothing here can reach the machine's
	// real ledger even if an agent were built without one.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AFORGE_HOME", t.TempDir())

	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	journal, _ := journalIn(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return pricedResponse("the work is done", 51.05), nil
		},
	}}, func(config *Config) {
		config.SessionFile = journal
		config.usageLedger = ledger
	})

	// The conversation's own bill: one sealed turn, which is the door every
	// other test in this package journals a cost through.
	agent.sealTurn(Usage{Input: 100, Output: 20, CostUSD: 2.53, Calls: 1}, time.Now(), "test/model")

	graph := agent.graph()
	graph.mu.Lock()
	// Nothing runs the node: this test drives its worker itself, so the node is
	// still open when the assertions are made.
	graph.run = func(*TaskNode) {}
	graph.mu.Unlock()
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "Fix the crash", brief: "fix it", acceptance: "it is fixed",
		model: "vendor/worker"})
	node := graph.node(id)

	worker, err := agent.newTaskAgent(context.Background(), t.TempDir(), node, "")
	if err != nil {
		t.Fatalf("spawn the worker: %v", err)
	}
	defer worker.Close()
	collect(t, mustSubmit(t, worker, "do the work"))
	FlushUsage()

	conversation := agent.journalID()
	if conversation == "" {
		t.Fatal("the conversation has no journal id to be the root of anything")
	}
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}

	// THE WORKER WROTE INTO THE CONVERSATION'S LEDGER and named the conversation
	// as its root. Either half missing is the rollup finding nothing.
	var found bool
	for _, line := range lines {
		if line.Task == "1" && line.Root == conversation {
			found = true
		}
	}
	if !found {
		t.Fatalf("no line names task 1 rooted in %s: %+v", conversation, lines)
	}

	tree := UsageTree(lines, conversation)
	if !treeNear(tree.TasksUSD, 51.05) {
		t.Fatalf("the work's spend reads %v, want 51.05: %+v", tree.TasksUSD, lines)
	}
	if !treeNear(tree.ConversationUSD, 2.53) {
		t.Fatalf("the conversation's own spend reads %v, want 2.53", tree.ConversationUSD)
	}
	if !treeNear(tree.TotalUSD(), 53.58) {
		t.Fatalf("the tree totals %v, want 53.58", tree.TotalUSD())
	}

	// AND THE NODE HAS NOT CLOSED, so its money is nowhere in the conversation's
	// own books. That is exactly the state the ambient figure used to be stuck
	// in for the whole of a run, and it is why the reading above has to come off
	// the ledger rather than off [Agent.Usage].
	if used := agent.Usage(); used.CostUSD >= 51.05 {
		t.Fatalf("the conversation's books already hold %v — the fold has happened, "+
			"so this test is no longer about the gap", used.CostUSD)
	}
}

// THE ROLLUP COUNTS EACH CALL ONCE AND KNOWS WHOSE IT IS, at any depth, and it
// leaves everybody else's money alone.
func TestTheTreeRollupNamesBothHalvesAndCountsEachCallOnce(t *testing.T) {
	const mine, other = "1111111111111111", "2222222222222222"
	now := time.Now()
	lines := []UsageLine{
		// This conversation's own turns.
		{At: now, Session: mine, USD: 2.00, Calls: 1},
		{At: now, Session: mine, Role: "title", USD: 0.53, Calls: 1},
		// A node of this conversation, and a node of THAT node: the root travels
		// all the way down, so a family of any depth is one sum.
		{At: now, Session: "aaaa", Task: "1", Root: mine, USD: 40.00, Calls: 3},
		{At: now, Session: "bbbb", Task: "2", Root: mine, USD: 10.00, Calls: 2},
		// The check that read what a node left carries no task id — it is an
		// agent of its own — and it is still this conversation's money.
		{At: now, Session: "cccc", Root: mine, USD: 1.05, Calls: 1},
		// Another conversation, and a node of it.
		{At: now, Session: other, USD: 99.00, Calls: 1},
		{At: now, Session: "dddd", Task: "1", Root: other, USD: 99.00, Calls: 1},
		// A standing firing nobody's conversation asked for.
		{At: now, Session: "eeee", Task: "1", Standing: "item-6am", USD: 5.00, Calls: 1},
	}

	tree := UsageTree(lines, mine)
	if !treeNear(tree.ConversationUSD, 2.53) {
		t.Fatalf("the conversation's own half reads %v, want 2.53", tree.ConversationUSD)
	}
	if !treeNear(tree.TasksUSD, 51.05) {
		t.Fatalf("the work's half reads %v, want 51.05", tree.TasksUSD)
	}
	if !treeNear(tree.TotalUSD(), 53.58) {
		t.Fatalf("the tree totals %v, want 53.58", tree.TotalUSD())
	}
	if tree.Calls != 8 {
		t.Fatalf("the tree counted %d calls, want 8", tree.Calls)
	}

	// A SURFACE THAT DOES NOT KNOW WHICH CONVERSATION IT IS IN GETS NOTHING, not
	// everything: an empty id matching every row would put the whole machine's
	// spending on one status line.
	if empty := UsageTree(lines, ""); empty.TotalUSD() != 0 {
		t.Fatalf("an unnamed conversation was given %v", empty.TotalUSD())
	}
}

// A CONVERSATION'S OWN LINE NAMES NO ROOT. Session is already that answer, and a
// second copy of it on the same row is the one-source-of-truth law broken where
// it is cheapest to break.
func TestAConversationsOwnLedgerLineNamesNoRoot(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})
	agent.sealTurn(Usage{Input: 100, Output: 20, CostUSD: 0.01, Calls: 1}, time.Now(), "test/model")
	FlushUsage()

	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil || len(lines) != 1 {
		t.Fatalf("read %d lines, %v", len(lines), err)
	}
	if lines[0].Root != "" {
		t.Fatalf("a conversation's line claims the root %q", lines[0].Root)
	}
	raw, err := json.Marshal(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"root"`) {
		t.Fatalf("an absent root reached the wire: %s", raw)
	}
}
