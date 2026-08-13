package store

import (
	"math"
	"path/filepath"
	"testing"
)

func turnLedgerStore(t *testing.T) *Store {
	t.Helper()
	graph := openTestStore(t, filepath.Join(t.TempDir(), "turns.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "leaf", Brief: "run a long loop", Title: "Leaf"},
	}}, Provenance{Origin: OriginUser, Intent: "run a long loop"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

var ledgerRows = []TurnUsage{
	{Turn: 1, PromptTokens: 11_000, CompletionTokens: 900, CachedTokens: 9_000, SentTokens: 11_000, Cost: 0.02},
	{Turn: 2, PromptTokens: 19_000, CompletionTokens: 800, CachedTokens: 17_000, SentTokens: 19_000, Cost: 0.03},
	{Turn: 3, PromptTokens: 27_000, CompletionTokens: 700, CachedTokens: 25_000, SentTokens: 27_000, Cost: 0.04},
}

// The ledger's whole job is to sum to the row that was already being written.
// A finer accounting that does not reconcile with the coarse one is a second
// accounting, and nobody would know which to believe.
func TestTurnRowsSumToTheNodeRow(t *testing.T) {
	graph := turnLedgerStore(t)
	node := NodeUsage{NodeID: "leaf", PromptTokens: 57_000, CompletionTokens: 2_400,
		CachedTokens: 51_000, Cost: 0.09, Model: "worker/model"}
	if err := graph.RecordUsage(node); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordTurnUsage("leaf", node.Model, ledgerRows); err != nil {
		t.Fatal(err)
	}

	turns, err := graph.TurnUsageFor("leaf")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != len(ledgerRows) {
		t.Fatalf("the journal kept %d rows, want %d", len(turns), len(ledgerRows))
	}
	var prompt, completion, cached int
	var cost float64
	for index, turn := range turns {
		if turn != ledgerRows[index] {
			t.Fatalf("row %d came back as %+v, want %+v", index, turn, ledgerRows[index])
		}
		prompt += turn.PromptTokens
		completion += turn.CompletionTokens
		cached += turn.CachedTokens
		cost += turn.Cost
	}
	if prompt != node.PromptTokens || completion != node.CompletionTokens || cached != node.CachedTokens {
		t.Fatalf("rows sum to prompt=%d completion=%d cached=%d; the node row says %+v",
			prompt, completion, cached, node)
	}
	if math.Abs(cost-node.Cost) > 1e-9 {
		t.Fatalf("rows sum to $%.6f; the node row says $%.6f", cost, node.Cost)
	}
}

// The additive half. Half a dozen readers count usage rows and mean executions
// by it — a job's Runs, a receipt's calls, the day's rail. The shape lands in a
// table of its own precisely so none of those counts moves.
func TestTheTurnLedgerLeavesEveryExistingUsageReaderAlone(t *testing.T) {
	graph := turnLedgerStore(t)
	if err := graph.RecordUsage(NodeUsage{NodeID: "leaf", PromptTokens: 57_000,
		CompletionTokens: 2_400, Cost: 0.09}); err != nil {
		t.Fatal(err)
	}
	before, err := graph.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordTurnUsage("leaf", "worker/model", ledgerRows); err != nil {
		t.Fatal(err)
	}
	after, err := graph.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("the graph total moved from %+v to %+v when the shape was journaled", before, after)
	}
	spend, err := graph.SpendToday()
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(spend-0.09) > 1e-9 {
		t.Fatalf("today's spend reads $%.6f; the ledger was double-counted", spend)
	}
}

// The journal is the source of truth, so the shape has to survive being derived
// from it again — and a node that never metered turns records no shape rather
// than a row of zeroes.
func TestTurnRowsSurviveARebuildAndSilenceIsNotAZero(t *testing.T) {
	graph := turnLedgerStore(t)
	if err := graph.RecordUsage(NodeUsage{NodeID: "leaf", Cost: 0.09}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordTurnUsage("leaf", "worker/model", ledgerRows); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordTurnUsage("leaf", "worker/model", nil); err != nil {
		t.Fatalf("an executor that meters no turns must journal nothing, not fail: %v", err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	turns, err := graph.TurnUsageFor("leaf")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != len(ledgerRows) {
		t.Fatalf("a rebuild recovered %d rows, want %d", len(turns), len(ledgerRows))
	}
	for index, turn := range turns {
		if turn != ledgerRows[index] {
			t.Fatalf("row %d came back from the journal as %+v, want %+v", index, turn, ledgerRows[index])
		}
	}
	if silent, err := graph.TurnUsageFor(RootID); err != nil || len(silent) != 0 {
		t.Fatalf("a node nobody metered answers with %d rows, err=%v", len(silent), err)
	}
}

// A ledger has to be numbered from one, in order, or it is not a ledger.
func TestTurnRowsRefuseAnUnnumberedLedger(t *testing.T) {
	graph := turnLedgerStore(t)
	if err := graph.RecordTurnUsage("leaf", "", []TurnUsage{{Turn: 0, PromptTokens: 10}}); err == nil {
		t.Fatal("an unnumbered turn was accepted")
	}
	if err := graph.RecordTurnUsage("", "", ledgerRows); err == nil {
		t.Fatal("a ledger for no node was accepted")
	}
}
