package session

// ONE LEDGER, ONE NUMBER (issue #269).
//
// Money used to be BANKED PER CALL and JOURNALED PER TURN: [Agent.addUsage]
// moved the meter as each answer was decoded, and [Agent.sealTurn] wrote the
// machine's ledger once at the end. Every turn that failed to seal —
// interrupted, stopped, crashed, or simply still running while somebody looked —
// was money the meter had and the ledger never got. On the measured chat that
// was $0.087, every call of a final interrupted turn, and four surfaces quoting
// four numbers for one instant.
//
// These tests are the acceptance the issue asks for: the equality that was red,
// the property that keeps it true call by call, the fold rule that must survive
// the move, and the counter that makes a lost row visible instead of flattering.

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// priced puts a provider's own cost on an answer, which is the only place a
// price comes from (internal/provider's billing.go — there is no local table).
func priced(response *ai.Response, usd float64) *ai.Response {
	response.Usage.Cost = &usd
	return response
}

// foldInto brings a child's whole tally home the way a closing node does: into
// the parent's books through the fold door, which writes no ledger line because
// the child already wrote one per call as it spent (usage_ledger.go's second
// rule; [Agent.foldTaskUsage] and [Agent.foldHandUsage] are the two callers).
func foldInto(parent, child *Agent) {
	used := child.Usage()
	cost := used.CostUSD
	parent.addFoldedUsage(&ai.Response{Usage: &ai.Usage{
		PromptTokens:             used.Input,
		CompletionTokens:         used.Output,
		CacheReadInputTokens:     used.CacheRead,
		CacheCreationInputTokens: used.CacheWrite,
		Cost:                     &cost,
	}}, child.Model(), used.Calls)
}

// ledgerUSD is what a ledger adds up to, and how many rows it took.
func ledgerUSD(t *testing.T, path string) (float64, int) {
	t.Helper()
	FlushUsage()
	lines, err := ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	var total float64
	for _, line := range lines {
		total += line.USD
	}
	return total, len(lines)
}

// ── 1 ───────────────────────────────────────────────────────────────────────

// THE ACCEPTANCE, AND IT WAS RED: a turn cut off after two billed calls has both
// calls on the machine's ledger, and the conversation's meter says exactly the
// same money — WHILE THE TURN IS STILL RUNNING as well as after it ends.
//
// The reading in flight is the half that was red, and it is the shape a person
// actually meets: they glance at the status line, open /spend, and the two
// disagree about the turn they are watching. Under the old shape the ledger was
// written once at the seal, so everything the turn had spent so far was money
// the meter had and the file had never heard of — $0.087 on the measured chat.
// The reading after the interrupt is red too, differently: the seal wrote ONE
// row carrying a whole turn's tally, so `calls` on a row was a lie and a crash
// before the seal wrote nothing at all.
func TestAnInterruptedTurnsMoneyIsOnTheLedgerAndTheMeterAlike(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	billed := make(chan struct{}, 2)
	held := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			billed <- struct{}{}
			return priced(toolResponse("c1", "ls", `{"path":"."}`), 0.05), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			billed <- struct{}{}
			return priced(toolResponse("c2", "ls", `{"path":"."}`), 0.037), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			// The third call is the one in flight when the person presses
			// escape: it never answers and it never bills.
			close(held)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})

	events := mustSubmit(t, agent, "look around")
	for i := 0; i < 2; i++ {
		select {
		case <-billed:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of the two calls were ever billed", i)
		}
	}
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never reached the call it would be interrupted in")
	}

	// THE READING IN FLIGHT, taken with the third call still out.
	flying, rows := ledgerUSD(t, ledger)
	if rows != 2 {
		t.Fatalf("a running turn has put %d rows on the ledger, want one per billed call", rows)
	}
	if got := agent.Usage().CostUSD; got != flying {
		t.Fatalf("mid-turn the meter says %v and the ledger says %v — the two sources "+
			"disagree about the turn a person is watching, which is issue #269", got, flying)
	}
	if flying < 0.0869 || flying > 0.0871 {
		t.Fatalf("the ledger sums to %v mid-turn, want the two billed calls (0.087)", flying)
	}

	agent.Interrupt()
	collect(t, events)

	// AND THE INTERRUPT ADDS NOTHING AND LOSES NOTHING. The call that never
	// answered never billed, and the seal writes no ledger row at all.
	total, after := ledgerUSD(t, ledger)
	if after != 2 {
		t.Fatalf("the interrupted turn ended holding %d ledger rows, want the two billed calls", after)
	}
	if got := agent.Usage().CostUSD; got != total {
		t.Fatalf("after the interrupt the meter says %v and the ledger says %v", got, total)
	}
}

// ── 2 ───────────────────────────────────────────────────────────────────────

// THE PROPERTY, checked after every one of N calls and never at a turn boundary:
// the ledger and the meter are the same number.
//
// It is a property and not an example because the defect was structural — two
// accumulators moved by two different events — and an example can only ever
// catch the boundary somebody thought of. [Agent.bank] is the one door both go
// through, so this holds after each call rather than after each turn.
func TestTheLedgerAndTheMeterAgreeAfterEveryCall(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	agent, _ := ledgerAgent(t, ledger)

	// Prices that are awkward on purpose: repeating binary fractions, a sliver
	// under a cent, and one call the provider priced at nothing but still
	// counted tokens for.
	prices := []float64{0.05, 0.037, 0.1, 0.0003, 0.2, 0, 0.019, 0.44, 0.0001, 1.25}
	var turn Usage
	for at, price := range prices {
		bankCall(agent, &turn, "test/model", 100, 20, price, laneFacts{})
		total, rows := ledgerUSD(t, ledger)
		if got := agent.Usage().CostUSD; got != total {
			t.Fatalf("after call %d the meter says %v and the ledger says %v", at+1, got, total)
		}
		if rows != at+1 {
			t.Fatalf("after call %d the ledger holds %d rows, want one per call", at+1, rows)
		}
	}
}

// ── 3 ───────────────────────────────────────────────────────────────────────

// THE FOLD RULE SURVIVES THE MOVE, at two levels of nesting.
//
// A child agent journals its own calls into this file where it made them, and
// its whole tally is then folded into its parent's BOOKS — which is right for a
// conversation's own accounting and would be the same money twice in a
// machine-wide file. Moving the write from the seal to the call is exactly the
// change that could have broken it, so the nest is walked here: a conversation,
// a node of it, and a node of that node, each banking calls of its own and each
// folding home.
func TestANestedFamilyIsOnTheLedgerExactlyOnce(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), UsageLedgerName)
	journal, _ := journalIn(t)
	conversation, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = journal
	})
	root := conversation.journalID()
	if root == "" {
		t.Fatal("the conversation has no journal id for its family to be rooted in")
	}
	node, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "node.jsonl")
		config.taskID = 1
		config.rootSession = root
	})
	grandchild, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.usageLedger = ledger
		config.SessionFile = filepath.Join(t.TempDir(), "grandchild.jsonl")
		config.taskID = 2
		config.rootSession = root
	})

	var own, nodeTurn, deepTurn Usage
	bankCall(conversation, &own, "test/model", 100, 20, 2.53, laneFacts{})
	bankCall(node, &nodeTurn, "test/worker", 100, 20, 40.00, laneFacts{})
	bankCall(grandchild, &deepTurn, "test/worker", 100, 20, 11.05, laneFacts{})

	// AND EACH TALLY COMES HOME, deepest first, through the fold door itself
	// ([Agent.addFoldedUsage]) — exactly as a family closes.
	foldInto(node, grandchild)
	foldInto(conversation, node)

	total, rows := ledgerUSD(t, ledger)
	if rows != 3 {
		t.Fatalf("three calls and two folds wrote %d ledger rows, want one per CALL", rows)
	}
	if total < 53.5799 || total > 53.5801 {
		t.Fatalf("the machine's ledger sums to %v, want each call once (53.58)", total)
	}

	lines, err := ReadUsage(ledger, time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// THE ROWS STILL CARRY WHOSE THE MONEY WAS, which is what keeps one sum
	// possible over rows written by three different agents.
	receipt := UsageTree(lines, root)
	if !treeNear(receipt.Direct, 2.53) {
		t.Fatalf("the conversation's own half reads %v, want 2.53", receipt.Direct)
	}
	if !treeNear(receipt.Children, 51.05) {
		t.Fatalf("the family's half reads %v, want 51.05", receipt.Children)
	}
	if !treeNear(receipt.Folded(), 53.58) {
		t.Fatalf("the receipt folds to %v, want 53.58", receipt.Folded())
	}
	// And the node's own books DID take its child's fold, which is the half of
	// the rule the ledger deliberately does not copy.
	if got := node.Usage().CostUSD; got < 51.0499 || got > 51.0501 {
		t.Fatalf("the node's books hold %v, want its own call and its child's fold", got)
	}
}

// ── 4 ───────────────────────────────────────────────────────────────────────

// A ROW THAT NEVER REACHED A FILE IS COUNTED, because a ledger that quietly
// loses rows reads as a machine that spent less — the flattering direction, and
// the one direction a bill must never be wrong in. The drop itself stays right:
// a spending record is worth less than the turn that earned it.
func TestALedgerWriteThatFailsIsCountedRatherThanSwallowed(t *testing.T) {
	// A path that cannot be a ledger: its parent is a FILE, so the directory the
	// writer would make cannot be made and the row has nowhere to go.
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := UsageDrops()

	RecordUsage(filepath.Join(blocker, UsageLedgerName), UsageLine{
		At: time.Now(), Model: "test/model", Calls: 1, Input: 100, Output: 20, USD: 0.31,
	})
	FlushUsage()

	if got := UsageDrops() - before; got != 1 {
		t.Fatalf("a row that could not be written moved the counter by %d, want 1", got)
	}
	// And an ordinary row moves it by nothing, so a nonzero count is news.
	good := filepath.Join(t.TempDir(), UsageLedgerName)
	steady := UsageDrops()
	RecordUsage(good, UsageLine{At: time.Now(), Model: "test/model", Calls: 1, Input: 1, USD: 0.01})
	FlushUsage()
	if got := UsageDrops() - steady; got != 0 {
		t.Fatalf("a row that was written moved the drop counter by %d, want 0", got)
	}
}

// ── 5 ───────────────────────────────────────────────────────────────────────

// ONE DOOR, ENFORCED IN THE SOURCE. The defect was two accumulators; the fix is
// that a caller cannot move the session's money without offering the machine's
// ledger a row, because both happen inside [Agent.bank]. That is a structural
// claim and a second `a.usage.CostUSD +=` somewhere else would quietly undo it
// while every behavioural test above stayed green.
func TestMoneyEntersTheSessionsBooksInExactlyOnePlace(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	var doors []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok || assign.Tok != token.ADD_ASSIGN {
				return true
			}
			for _, target := range assign.Lhs {
				if selectorPath(target) == "a.usage.CostUSD" {
					doors = append(doors, name+":"+itoa(fset.Position(assign.Pos()).Line))
				}
			}
			return true
		})
	}
	if len(doors) != 1 {
		t.Fatalf("the session's cost is added to in %d places (%s), want the one door "+
			"[Agent.bank] — a second one is a second set of books, which is issue #269",
			len(doors), strings.Join(doors, ", "))
	}
}

// selectorPath spells a dotted expression back out — `a.usage.CostUSD` — and
// answers nothing for anything that is not one.
func selectorPath(node ast.Expr) string {
	switch value := node.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		if inner := selectorPath(value.X); inner != "" {
			return inner + "." + value.Sel.Name
		}
	}
	return ""
}
