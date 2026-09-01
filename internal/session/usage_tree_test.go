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
	"fmt"
	"os"
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

// ── a fork's hands, which spend the same money in a different shape ──────────

// THE ACCEPTANCE OF ISSUE #168: a forked hand that spends a dollar raises the
// machine's day total by a dollar.
//
// A hand journals its own calls into the ledger as it makes them, and its whole
// tally is then folded into the turn that opened it. While that fold went
// through the auxiliary door it wrote a SECOND line for money already on the
// file, so the figure the per-day rail reads — every line of today, added up —
// was double on every fork anybody ran.
func TestAForkedHandsSpendRaisesTheDayTotalOnce(t *testing.T) {
	// A home of this test's own, so nothing here can reach the machine's real
	// ledger even if an agent were built without one.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AFORGE_HOME", t.TempDir())

	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	journal, _ := journalIn(t)

	completer := newForkCompleter(2)
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "fork", forkCall(
				forkPartJSON("the adapters", "adapters"),
				forkPartJSON("the docs", "docs"),
			)), nil
		},
	}
	// The caller's own rounds are priced at nothing, so every dollar in the
	// ledger below is a hand's and the arithmetic is about the thing under test.
	completer.callerTail = keepWorking(completer, "stitched")
	completer.hand = func(index, _ int, _ []ai.Message) (*ai.Response, error) {
		return pricedResponse(fmt.Sprintf("hand %d is done", index), 1.00), nil
	}

	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = journal
		config.usageLedger = ledger
	})
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split this up"))
	handsAreHome(t, agent)
	FlushUsage()

	// THE DAY TOTAL, READ THE WAY THE RAIL READS IT: every line of today, added
	// up, with nothing in the sum that knows what a hand is.
	now := time.Now()
	lines, err := ReadUsage(ledger, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()))
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	day := 0.0
	for _, line := range lines {
		day += line.USD
	}
	if !treeNear(day, 2.00) {
		t.Fatalf("the day totals %v for two hands that spent a dollar each, want 2.00: %+v", day, lines)
	}

	// AND THE FOLD STILL HAPPENED, into the books it always went into: this is a
	// change to the ledger's arithmetic and not to the conversation's, and a
	// hand's money is the caller's money as soon as the hand is home.
	if used := agent.Usage(); !treeNear(used.CostUSD, 2.00) {
		t.Fatalf("the conversation's own books hold %v after both hands came home, want 2.00", used.CostUSD)
	}

	// AND THE WORD `hand` SURVIVED THE MOVE. It is what makes a turn's journal
	// readable afterwards — which of these tokens the hands spent and which the
	// caller did — and it lives on the journal line now rather than on a ledger
	// row.
	transcript, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	if !strings.Contains(string(transcript), `"role":"`+auxRoleHand+`"`) {
		t.Fatalf("no folded line in the journal names the hands:\n%s", transcript)
	}
}

// A HAND'S SPEND IS ON THE CONVERSATION'S TREE WHILE THE HAND IS STILL OUT, for
// the reason a task's is ([TestATasksSpendIsOnTheConversationsTotalBeforeTheTaskCloses]):
// the fold is a settlement at the end and a person glances in the middle.
//
// A hand keeps no journal of its own, so before this its ledger lines named a
// session id nobody outside the fork had ever heard of and belonged, as far as
// any reader could tell, to nothing at all.
func TestALiveHandsSpendIsOnTheConversationsTreeBeforeItComesHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AFORGE_HOME", t.TempDir())

	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	journal, _ := journalIn(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return pricedResponse("my part is done", 51.05), nil
		},
	}}, func(config *Config) {
		config.SessionFile = journal
		config.usageLedger = ledger
	})

	// The conversation's own bill, through the door every other test in this
	// package journals a cost through.
	agent.sealTurn(Usage{Input: 100, Output: 20, CostUSD: 2.53, Calls: 1}, time.Now(), "test/model")

	// The hand is minted and driven here rather than through the verb, because
	// what is being pinned is the state DURING a fork: nothing folds this hand
	// in, exactly as nothing has folded a hand that is still working.
	seed, system := agent.forkSeed()
	hand, err := agent.newHandAgent(forkPart{Role: "the adapters", Scope: []string{"adapters"}},
		seed, system, &handLeash{limit: forkRounds})
	if err != nil {
		t.Fatalf("mint a hand: %v", err)
	}
	defer hand.Close()
	collect(t, mustSubmit(t, hand, "do your part"))
	FlushUsage()

	conversation := agent.journalID()
	if conversation == "" {
		t.Fatal("the conversation has no journal id to be the root of anything")
	}
	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}

	// THE HAND WROTE INTO THE CONVERSATION'S OWN LEDGER — the family spends into
	// one file — AND NAMED THE CONVERSATION AS ITS ROOT. Either half missing is
	// the rollup finding nothing.
	var found bool
	for _, line := range lines {
		if !treeNear(line.USD, 51.05) {
			continue
		}
		found = true
		if line.Root != conversation {
			t.Fatalf("the hand's line is rooted in %q, want the conversation %q", line.Root, conversation)
		}
		if line.Session == conversation {
			t.Fatalf("the hand's line claims the conversation's own journal, so the rollup would count it in both halves")
		}
	}
	if !found {
		t.Fatalf("the hand's money is not on the conversation's ledger at all: %+v", lines)
	}

	tree := UsageTree(lines, conversation)
	if !treeNear(tree.TasksUSD, 51.05) {
		t.Fatalf("the hand's spend reads %v on the tree, want 51.05: %+v", tree.TasksUSD, lines)
	}
	if !treeNear(tree.ConversationUSD, 2.53) {
		t.Fatalf("the conversation's own spend reads %v, want 2.53", tree.ConversationUSD)
	}
	if !treeNear(tree.TotalUSD(), 53.58) {
		t.Fatalf("the tree totals %v, want 53.58", tree.TotalUSD())
	}

	// AND THE HAND HAS NOT COME HOME, so its money is nowhere in the
	// conversation's own books yet. That is the whole state this test is about.
	if used := agent.Usage(); used.CostUSD >= 51.05 {
		t.Fatalf("the conversation's books already hold %v — the fold has happened, "+
			"so this test is no longer about the gap", used.CostUSD)
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
	t.Setenv("AFORGE_HOME", t.TempDir())

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
	day := 0.0
	for _, line := range lines {
		day += line.USD
	}
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
	if !treeNear(tree.TasksUSD, 1.00) {
		t.Fatalf("the run's spend reads %v on the tree, want 1.00: %+v", tree.TasksUSD, lines)
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
