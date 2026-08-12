package main

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The receipt has to agree with the ledger it is a receipt for.
//
// The defect this pins (audit §11 D4): a run reported `spend: 0.5255` while its
// own usage table summed to $0.8221 — a 36 % under-report, and the missing
// third was one swe leaf that journaled its row on the way down from the wall,
// after the figure had been read. The figure was also a subtraction of today's
// spend across the whole store, so it was wrong about the day boundary and
// about a store somebody else was also billing.
//
// So the test is deliberately about the shapes of row that used to be lost:
// leaf rows, the per-node structuring row beside each one, the planning row that
// bills the root and belongs to no node, and a row written late. All four are in
// this journal, and the answer must be the sum of the table.
func TestErrandSpendSumsEveryKindOfUsageRow(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	session := "headless-receipt"
	openedAt, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "build the ledger", Stage: 0},
		{ID: "task-1-n1", Parent: "task-1", Brief: "write the parser", Stage: 0},
		{ID: "task-1-n2", Parent: "task-1", Brief: "write the balance command", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: "build the ledger"}); err != nil {
		t.Fatal(err)
	}

	// The mixture, in the proportions the audited run had: a structuring row
	// beside every node, a leaf row under it, and the plan's own passes billed
	// to the root.
	rows := []store.NodeUsage{
		{NodeID: "task-1", PromptTokens: 3_000, CompletionTokens: 120, Cost: 0.0042},
		{NodeID: "task-1-n1", PromptTokens: 2_900, CompletionTokens: 90, Cost: 0.0031},
		{NodeID: "task-1-n1", PromptTokens: 880_000, CompletionTokens: 22_000, Cost: 0.0744},
		{NodeID: "task-1-n2", PromptTokens: 2_950, CompletionTokens: 95, Cost: 0.0033},
		{NodeID: store.RootID, PromptTokens: 41_000, CompletionTokens: 1_800, Cost: 0.0126},
	}
	for _, row := range rows {
		if err := graph.RecordUsage(row); err != nil {
			t.Fatalf("record usage for %s: %v", row.NodeID, err)
		}
	}

	var outcome headlessOutcome
	priceErrand(graph, session, openedAt, &outcome)
	assertSpendIsTheTable(t, graph, outcome)

	// The late row. This is the swe leaf that landed at the wall: it is written
	// after a receipt would once have been printed, and the only way the figure
	// can carry it is by being a query rather than a counter read at settle
	// time. A second pricing pass over the same store must move.
	before := outcome.Spend
	if err := graph.RecordUsage(store.NodeUsage{
		NodeID: "task-1-n2", PromptTokens: 1_877_154, CompletionTokens: 41_000, Cost: 0.2967,
	}); err != nil {
		t.Fatal(err)
	}
	outcome = headlessOutcome{}
	priceErrand(graph, session, openedAt, &outcome)
	assertSpendIsTheTable(t, graph, outcome)
	if outcome.Spend <= before {
		t.Fatalf("the late leaf never reached the receipt: %.4f then %.4f", before, outcome.Spend)
	}

	// The two named halves are the whole bill and nothing else. Work is what
	// this errand's nodes cost; overhead is what deciding on them cost.
	if !closeEnough(outcome.SpendWork+outcome.SpendOverhead, outcome.Spend) {
		t.Fatalf("the halves do not add up: work %.4f + overhead %.4f != spend %.4f",
			outcome.SpendWork, outcome.SpendOverhead, outcome.Spend)
	}
	if !closeEnough(outcome.SpendOverhead, 0.0126) {
		t.Fatalf("the root-billed planning row is not the overhead half: %.4f", outcome.SpendOverhead)
	}
	if outcome.SpendWork <= outcome.SpendOverhead {
		t.Fatalf("leaf work priced below planning overhead: %.4f vs %.4f",
			outcome.SpendWork, outcome.SpendOverhead)
	}
}

// The other half of the contract: on a durable store the errand is one of
// several tenants, and its receipt may only carry its own money. Summing the
// whole table there would be the same defect pointing the other way.
func TestErrandSpendLeavesAnotherSessionsMoneyAlone(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	// Somebody else's job, already billed, before this errand opens.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-0", Brief: "somebody else's job", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "other-room", Intent: "somebody else's job"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "task-0", PromptTokens: 500_000, Cost: 0.5000}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, PromptTokens: 9_000, Cost: 0.0300}); err != nil {
		t.Fatal(err)
	}

	session := "headless-tenant"
	openedAt, err := graph.LatestEventSeq()
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "my job", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: "my job"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "task-1", PromptTokens: 120_000, Cost: 0.0800}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(store.NodeUsage{NodeID: store.RootID, PromptTokens: 7_000, Cost: 0.0100}); err != nil {
		t.Fatal(err)
	}
	// The other room keeps spending while this errand runs; none of it is this
	// errand's bill.
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "task-0", PromptTokens: 400_000, Cost: 0.4000}); err != nil {
		t.Fatal(err)
	}

	var outcome headlessOutcome
	priceErrand(graph, session, openedAt, &outcome)
	if !closeEnough(outcome.SpendWork, 0.0800) {
		t.Fatalf("work spend picked up another room: %.4f, want 0.0800", outcome.SpendWork)
	}
	if !closeEnough(outcome.SpendOverhead, 0.0100) {
		t.Fatalf("overhead reached back before the errand opened: %.4f, want 0.0100", outcome.SpendOverhead)
	}
	if !closeEnough(outcome.Spend, 0.0900) {
		t.Fatalf("errand spend is %.4f, want 0.0900", outcome.Spend)
	}
}

// assertSpendIsTheTable is the pin itself: on a store this errand is the only
// tenant of, the reported figure is SUM(cost) over usage, exactly.
func assertSpendIsTheTable(t *testing.T, graph *store.Store, outcome headlessOutcome) {
	t.Helper()
	total, err := graph.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if !closeEnough(outcome.Spend, total.Cost) {
		t.Fatalf("the receipt says $%.4f and the usage table sums to $%.4f (%.1f%% off)",
			outcome.Spend, total.Cost, 100*(total.Cost-outcome.Spend)/total.Cost)
	}
}

func closeEnough(got, want float64) bool { return math.Abs(got-want) < 1e-9 }
