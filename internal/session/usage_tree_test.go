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
	t.Setenv("CODEAF_HOME", t.TempDir())

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

	// The conversation's own bill: one call, which is the door every ledger row
	// in this package comes through since issue #269.
	var own Usage
	bankCall(agent, &own, "test/model", 100, 20, 2.53, laneFacts{})

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
	if !treeNear(tree.Children, 51.05) {
		t.Fatalf("the work's spend reads %v, want 51.05: %+v", tree.Children, lines)
	}
	if !treeNear(tree.Direct, 2.53) {
		t.Fatalf("the conversation's own spend reads %v, want 2.53", tree.Direct)
	}
	if !treeNear(tree.Folded(), 53.58) {
		t.Fatalf("the tree totals %v, want 53.58", tree.Folded())
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
	if !treeNear(tree.Direct, 2.53) {
		t.Fatalf("the conversation's own half reads %v, want 2.53", tree.Direct)
	}
	if !treeNear(tree.Children, 51.05) {
		t.Fatalf("the work's half reads %v, want 51.05", tree.Children)
	}
	if !treeNear(tree.Folded(), 53.58) {
		t.Fatalf("the tree totals %v, want 53.58", tree.Folded())
	}
	if tree.Calls != 8 {
		t.Fatalf("the tree counted %d calls, want 8", tree.Calls)
	}

	// A SURFACE THAT DOES NOT KNOW WHICH CONVERSATION IT IS IN GETS NOTHING, not
	// everything: an empty id matching every row would put the whole machine's
	// spending on one status line.
	if empty := UsageTree(lines, ""); empty.Folded() != 0 {
		t.Fatalf("an unnamed conversation was given %v", empty.Folded())
	}
}

// A CONVERSATION'S OWN LINE NAMES NO ROOT. Session is already that answer, and a
// second copy of it on the same row is the one-source-of-truth law broken where
// it is cheapest to break.
func TestAConversationsOwnLedgerLineNamesNoRoot(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)
	var own Usage
	bankCall(agent, &own, "test/model", 100, 20, 0.01, laneFacts{})
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

// pricedReplier is [replier] with a bill on the answers it chooses to price, so
// a test can say which agent in a run spent the money.
type pricedReplier func(messages []ai.Message) (string, float64)

func (r pricedReplier) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	text, cost := r(messages)
	if cost == 0 {
		return textResponse(text), nil
	}
	return pricedResponse(text, cost), nil
}

// AN ADAPTIVE RUN'S NODE IS UNDER THE SAME RULE, and it was the second fold that
// broke it. A run's worker keeps a journal of its own and writes its own ledger
// lines exactly as a task node does, so the tally folded home at the end of the
// node is a settlement and not a call.
func TestAnAdaptiveRunsNodeSpendRaisesTheDayTotalOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())

	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	journal, _ := journalIn(t)

	// Only the node's own answer costs anything: the planner, the namer and the
	// write-up are free, so every dollar below is the worker's.
	completer := pricedReplier(func(messages []ai.Message) (string, float64) {
		asked := lastUserText(messages)
		switch {
		case isPlannerCall(messages):
			if strings.Contains(asked, "nothing yet") {
				return `{"add":[{"id":"n1","goal":"read the release notes"}],"note":"one node to start"}`, 0
			}
			if !strings.Contains(asked, "n1: n1 read the notes") {
				return `{}`, 0
			}
			return `{"done":{"brief":"say what the notes said"}}`, 0
		case isNameCall(messages):
			return "reading the notes", 0
		case strings.HasPrefix(asked, "First message:"):
			return `{"work":false,"why":"the node was already work"}`, 0
		case strings.Contains(asked, "Ground every claim"):
			return "the write-up, grounded in (n1)", 0
		}
		return "n1 read the notes", 1.00
	})

	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = journal
		config.usageLedger = ledger
		config.AskConsent = true
	})

	id, err := agent.RunOrchestrate(context.Background(), "summarise the release notes", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, agent, id)
	FlushUsage()

	now := time.Now()
	lines, err := ReadUsage(ledger, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()))
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	day := UsageTotals(lines).USD
	if !treeNear(day, 1.00) {
		t.Fatalf("the day totals %v for one node that spent a dollar, want 1.00: %+v", day, lines)
	}

	// AND THE NODE'S OWN LINE NAMES THE CONVERSATION THE RUN BELONGS TO, so the
	// tree rollup reads a run while it is still running rather than when it
	// folds.
	conversation := agent.journalID()
	if conversation == "" {
		t.Fatal("the conversation has no journal id to be the root of anything")
	}
	tree := UsageTree(lines, conversation)
	if !treeNear(tree.Children, 1.00) {
		t.Fatalf("the run's spend reads %v on the tree, want 1.00: %+v", tree.Children, lines)
	}
}

// A HAND'S MONEY IS ON THE CONVERSATION'S OWN ROW OF THE SPEND PLACE, not on a
// row headed by an id nothing can name.
//
// A hand keeps no journal, so its ledger lines say `unfiled` where a
// conversation says its own id. Grouped on that, a fork drew a row nothing could
// put a title on — and the conversation it belonged to was short by exactly that
// money once its fold stopped writing a line of its own.
func TestWorkWithNoIdOfItsOwnIsOnItsConversationsSpendRow(t *testing.T) {
	const mine = "1111111111111111"
	now := time.Now()
	rows := UsageBySubject([]UsageLine{
		// The conversation's own turn.
		{At: now, Session: mine, USD: 2.00, Calls: 1},
		// Two hands of one of its replies, and the check that read what a node
		// left: three agents, no id of their own, all this conversation's work.
		{At: now, Session: "unfiled", Root: mine, USD: 1.00, Calls: 1},
		{At: now, Session: "unfiled", Root: mine, USD: 1.00, Calls: 1},
		{At: now, Session: "cccc", Root: mine, USD: 0.50, Calls: 1},
		// And a node of the same conversation, which HAS an id and keeps its own
		// row: this is not a change to how work with a name is grouped.
		{At: now, Session: "aaaa", Task: "1", Root: mine, USD: 10.00, Calls: 1},
	})
	if len(rows) != 2 {
		t.Fatalf("grouped into %d rows, want the node and the conversation: %+v", len(rows), rows)
	}
	if rows[0].Kind != SubjectTask || rows[0].ID != "1" || !treeNear(rows[0].USD, 10.00) {
		t.Fatalf("the node's row is %+v", rows[0])
	}
	if rows[1].Kind != SubjectConversation || rows[1].ID != mine || !treeNear(rows[1].USD, 4.50) {
		t.Fatalf("the conversation's row is %+v, want its own turn and the three agents under it", rows[1])
	}
	if rows[1].Session != rows[1].ID {
		t.Fatalf("a conversation row names the session %q against the id %q", rows[1].Session, rows[1].ID)
	}
}
