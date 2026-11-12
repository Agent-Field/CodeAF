package run

import (
	"database/sql"
	"math"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

type spendRow struct {
	model    string
	answered bool
}

// spendRows is the store's spend ledger for one task: dollars by model and
// whether the row says that model answered.
func spendRows(t *testing.T, path, task string) map[spendRow]float64 {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT model, answered, usd FROM spend WHERE task_id = ?`, task)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[spendRow]float64{}
	for rows.Next() {
		var row spendRow
		var usd float64
		if err := rows.Scan(&row.model, &row.answered, &usd); err != nil {
			t.Fatal(err)
		}
		out[row] += usd
	}
	return out
}

func spendStore(t *testing.T) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "p", "root", "Fix", "Fix it")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// A SEAT WHOSE MODEL FAILED OVER IS WRITTEN DOWN AS THE MODEL THAT ANSWERED,
// marked answered, and the seat's built-on model gets no answered row.
func TestAFallbackIsWrittenUnderTheModelThatAnswered(t *testing.T) {
	store := spendStore(t)
	worker := &BashWorker{store: store, model: "m/built"}
	worker.recordSpend("root", session.Usage{Input: 20, Output: 7, CostUSD: 0.03},
		map[string]session.Usage{"m/fallback": {Input: 20, Output: 7, CostUSD: 0.03}}, map[string]int{"m/fallback": 1})
	rows := spendRows(t, store.Path(), "root")
	if len(rows) != 1 || rows[spendRow{"m/fallback", true}] != 0.03 {
		t.Fatalf("rows %+v", rows)
	}
}

// THE CONFIGURED MODEL IS NEVER AN ANSWERED ONE: with no breakdown the row
// names the built-on model for billing and is not marked; money the breakdown
// does not cover, and money whose answer named no model, are unmarked too —
// and every row still sums to what the seat spent.
func TestMoneyNoAnswerCoversIsBilledButNeverMarkedAnswered(t *testing.T) {
	store := spendStore(t)
	(&BashWorker{store: store, model: "m/built"}).recordSpend("root", session.Usage{Input: 30, Output: 12, CostUSD: 0.04}, nil, nil)
	if rows := spendRows(t, store.Path(), "root"); len(rows) != 1 || rows[spendRow{"m/built", false}] != 0.04 {
		t.Fatalf("no breakdown wrote %+v", rows)
	}

	partial := spendStore(t)
	(&BashWorker{store: partial, model: "m/built"}).recordSpend("root", session.Usage{Input: 40, Output: 15, CostUSD: 0.05},
		map[string]session.Usage{
			"m/fallback": {Input: 20, Output: 7, CostUSD: 0.03},
			"":           {Input: 5, Output: 3, CostUSD: 0.005},
		}, map[string]int{"m/fallback": 1, "": 1})
	rows := spendRows(t, partial.Path(), "root")
	total := 0.0
	for _, usd := range rows {
		total += usd
	}
	if len(rows) != 3 || rows[spendRow{"m/fallback", true}] != 0.03 || rows[spendRow{"", false}] != 0.005 ||
		rows[spendRow{"m/built", false}] < 0.0149 || math.Abs(total-0.05) > 1e-9 {
		t.Fatalf("partial breakdown wrote %+v", rows)
	}
}

// AN ANSWER IS A FACT ABOUT WHO WORKED WHETHER OR NOT IT COST ANYTHING. A free
// model's answer, and an answer whose response carried no usage at all, is an
// answered row of no money; a model that banked money here but gave no answer
// (a late receipt) is billing only.
func TestAnAnswerIsWrittenEvenWhenItCostNothing(t *testing.T) {
	free := spendStore(t)
	(&BashWorker{store: free, model: "m/built"}).recordSpend("root", session.Usage{Calls: 2},
		map[string]session.Usage{"m/free": {Calls: 2}}, map[string]int{"m/free": 2})
	if rows := spendRows(t, free.Path(), "root"); len(rows) != 1 || rows[spendRow{"m/free", true}] != 0 {
		t.Fatalf("a free answer wrote %+v", rows)
	}

	bare := spendStore(t)
	(&BashWorker{store: bare, model: "m/built"}).recordSpend("root", session.Usage{}, nil, map[string]int{"m/quiet": 1})
	if rows := spendRows(t, bare.Path(), "root"); len(rows) != 1 || rows[spendRow{"m/quiet", true}] != 0 {
		t.Fatalf("an answer with no usage wrote %+v", rows)
	}

	paid := spendStore(t)
	(&BashWorker{store: paid, model: "m/built"}).recordSpend("root", session.Usage{Input: 9, Calls: 2, CostUSD: 0.02},
		map[string]session.Usage{"m/a": {Input: 9, Calls: 1, CostUSD: 0.02}, "m/b": {Calls: 1}},
		map[string]int{"m/a": 1, "m/b": 1})
	if rows := spendRows(t, paid.Path(), "root"); len(rows) != 2 || rows[spendRow{"m/a", true}] != 0.02 {
		t.Fatalf("a paid and a free answer wrote %+v", rows)
	} else if _, ok := rows[spendRow{"m/b", true}]; !ok {
		t.Fatalf("the free answer was dropped: %+v", rows)
	}

	// B's response carried no usage: no share, but still B's answer.
	quiet := spendStore(t)
	(&BashWorker{store: quiet, model: "m/built"}).recordSpend("root", session.Usage{Input: 9, Calls: 1, CostUSD: 0.02},
		map[string]session.Usage{"m/a": {Input: 9, Calls: 1, CostUSD: 0.02}}, map[string]int{"m/a": 1, "m/b": 1})
	if rows := spendRows(t, quiet.Path(), "root"); len(rows) != 2 {
		t.Fatalf("a paid answer and one with no usage wrote %+v", rows)
	} else if _, ok := rows[spendRow{"m/b", true}]; !ok {
		t.Fatalf("the answer with no usage was dropped: %+v", rows)
	}

	unnamed := spendStore(t)
	(&BashWorker{store: unnamed, model: "m/built"}).recordSpend("root", session.Usage{}, nil, map[string]int{"": 1})
	if rows := spendRows(t, unnamed.Path(), "root"); len(rows) != 1 {
		t.Fatalf("an unnamed answer wrote %+v", rows)
	} else if _, ok := rows[spendRow{"", false}]; !ok {
		t.Fatalf("an unnamed answer was marked or dropped: %+v", rows)
	}

	late := spendStore(t)
	(&BashWorker{store: late, model: "m/built"}).recordSpend("root", session.Usage{Input: 4, Calls: 1, CostUSD: 0.01},
		map[string]session.Usage{"m/late": {Input: 4, Calls: 1, CostUSD: 0.01}}, nil)
	if rows := spendRows(t, late.Path(), "root"); len(rows) != 1 || rows[spendRow{"m/late", false}] != 0.01 {
		t.Fatalf("a receipt with no answer wrote %+v", rows)
	}
}

// COVERAGE IS COUNTED IN CALLS AND TOKENS, EXACTLY. Calls the shares do not
// account for are written unmarked on the built-on model even when they cost
// nothing; shares that account for every call and token write no residual,
// however the dollars rounded.
func TestUncoveredCallsAreBilledUnmarkedAndCoveredOnesLeaveNoResidual(t *testing.T) {
	short := spendStore(t)
	(&BashWorker{store: short, model: "m/built"}).recordSpend("root", session.Usage{Calls: 3},
		map[string]session.Usage{"m/a": {Calls: 2}}, map[string]int{"m/a": 2})
	if rows := spendRows(t, short.Path(), "root"); len(rows) != 2 {
		t.Fatalf("an uncovered free call wrote %+v", rows)
	} else if _, ok := rows[spendRow{"m/built", false}]; !ok {
		t.Fatalf("the uncovered call is not billed unmarked: %+v", rows)
	}

	whole := spendStore(t)
	(&BashWorker{store: whole, model: "m/built"}).recordSpend("root", session.Usage{Input: 3, Calls: 3, CostUSD: 0.1 + 0.2},
		map[string]session.Usage{"m/a": {Input: 1, Calls: 1, CostUSD: 0.2}, "m/b": {Input: 2, Calls: 2, CostUSD: 0.1}},
		map[string]int{"m/a": 1, "m/b": 2})
	if rows := spendRows(t, whole.Path(), "root"); len(rows) != 2 {
		t.Fatalf("covered shares wrote a residual: %+v", rows)
	}
}

// Dollar-only receipts must not disappear merely because all token and call
// counters were already covered, even below an arbitrary currency threshold.
func TestUncoveredDollarOnlySpendIsRetained(t *testing.T) {
	store := spendStore(t)
	used := session.Usage{Input: 2, Calls: 1, CostUSD: 0.01 + 1e-12}
	(&BashWorker{store: store, model: "m/built"}).recordSpend("root", used,
		map[string]session.Usage{"m/served": {Input: 2, Calls: 1, CostUSD: 0.01}}, map[string]int{"m/served": 1})
	rows := spendRows(t, store.Path(), "root")
	if len(rows) != 2 || rows[spendRow{"m/built", false}] <= 0 ||
		math.Abs(rows[spendRow{"m/served", true}]+rows[spendRow{"m/built", false}]-used.CostUSD) > math.SmallestNonzeroFloat64 {
		t.Fatalf("a real dollar-only residual was discarded: %+v", rows)
	}
}
