package session

import (
	"math"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// ── qloop run23, vector-db t1: the cheap job on the dear row ────────────────
//
// The replay. "Comparison charts" opened the run and its worker spent $0.13.
// "Deep research" joined the run as the root's child, split into five research
// leaves on the worker seat, and the review round put one careful-seat check on
// each leaf. The run's figure was $1.43, and before this change all of it went
// onto the charts' row while the research row read nothing. These figures are
// the run's own, rounded from its spend ledger.
func TestAJoinedHandoffCarriesItsOwnSpendAndTheRunsRowTheRest(t *testing.T) {
	agent, store, run, _ := landingSummaryFixture(t, &scriptedCompleter{})
	const charts, planner, leaf, check, runFigure = 0.13, 0.17, 0.02, 0.164, 1.43

	specs := []plandb.TaskSpec{{ID: "2", Title: "Deep research: vector DB facts and sources", Description: "research", ParentID: run.root}}
	for index := 0; index < 5; index++ {
		specs = append(specs, plandb.TaskSpec{ID: strconv.Itoa(3 + index), Title: "research a store", Description: "one store", ParentID: "2"})
	}
	if _, err := store.AddMany(specs); err != nil {
		t.Fatalf("add the joined research: %v", err)
	}
	if err := store.AddSpend(run.root, "deepseek/deepseek-v4.1-flash", plandb.RoleWork, charts, 1000, 100); err != nil {
		t.Fatalf("charge the charts: %v", err)
	}
	if err := store.AddSpend("2", "google/gemini-4-hydrogen", plandb.RolePlan, planner, 1000, 100); err != nil {
		t.Fatalf("charge the planner: %v", err)
	}
	for index := 0; index < 5; index++ {
		id := strconv.Itoa(3 + index)
		if err := store.AddSpend(id, "deepseek/deepseek-v4.1-flash", plandb.RoleWork, leaf, 1000, 100); err != nil {
			t.Fatalf("charge leaf %s: %v", id, err)
		}
		// A REVIEW CHECK'S CHARGE NAMES THE LEAF'S COORDINATOR'S SUBTREE: the
		// check is seated beside the leaf it reads ([run.Supervisor.addReviewCheck]).
		if err := store.AddSpend(id, "google/gemini-4-carbon", plandb.RoleCheck, check, 1000, 100); err != nil {
			t.Fatalf("charge the check of leaf %s: %v", id, err)
		}
	}
	run.joined = []uint64{2}
	agent.settleBeltRun(run, RunSummary{Outcome: beltRunOutcomeDone, Result: "charts made", USD: runFigure}, RunLanding{})

	research := planner + 5*(leaf+check)
	joined := agent.graph().runRows(2)
	if len(joined) != 1 {
		t.Fatalf("the joined row published %d times, want once", len(joined))
	}
	if got := joined[0].CostUSD; math.Abs(got-research) > 1e-9 {
		t.Fatalf("the research row reads $%.4f, want its own subtree's $%.4f", got, research)
	}
	var root *TaskNotice
	for _, row := range agent.graph().runRows(run.row) {
		if row.ID == run.row {
			root = &row
		}
	}
	if root == nil {
		t.Fatal("the run's own row was not published")
	}
	if got, want := root.CostUSD, runFigure-research; math.Abs(got-want) > 1e-9 {
		t.Fatalf("the charts row reads $%.4f, want the run's figure less the research: $%.4f", got, want)
	}
	if got := root.CostUSD + joined[0].CostUSD; math.Abs(got-runFigure) > 1e-9 {
		t.Fatalf("the two rows add to $%.4f, want the run's own $%.2f", got, runFigure)
	}
}

// A RUN NOTHING JOINED KEEPS ITS WHOLE FIGURE on its own row.
func TestARunWithNothingJoinedKeepsItsWholeFigure(t *testing.T) {
	_, store, _, _ := landingSummaryFixture(t, &scriptedCompleter{})
	if got := rootRowShare(store, 0.42, nil); got != 0.42 {
		t.Fatalf("a run with no joined work reads $%.2f, want its whole $0.42", got)
	}
}

// THE RUN'S ROW NEVER READS BELOW WHAT ITS OWN WORK CHARGED, even when the
// joined work's ledger outruns the run's figure.
func TestTheRunsRowNeverReadsBelowItsOwnCharges(t *testing.T) {
	_, store, run, _ := landingSummaryFixture(t, &scriptedCompleter{})
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "2", Title: "joined", Description: "d", ParentID: run.root}}); err != nil {
		t.Fatalf("add joined: %v", err)
	}
	_ = store.AddSpend(run.root, "m", plandb.RoleWork, 0.10, 1, 1)
	_ = store.AddSpend("2", "m", plandb.RoleWork, 0.50, 1, 1)
	if got := rootRowShare(store, 0.40, []uint64{2}); math.Abs(got-0.10) > 1e-9 {
		t.Fatalf("the run's row reads $%.2f, want at least its own $0.10", got)
	}
}
