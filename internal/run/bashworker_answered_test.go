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
		map[string]session.Usage{"m/fallback": {Input: 20, Output: 7, CostUSD: 0.03}})
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
	(&BashWorker{store: store, model: "m/built"}).recordSpend("root", session.Usage{Input: 30, Output: 12, CostUSD: 0.04}, nil)
	if rows := spendRows(t, store.Path(), "root"); len(rows) != 1 || rows[spendRow{"m/built", false}] != 0.04 {
		t.Fatalf("no breakdown wrote %+v", rows)
	}

	partial := spendStore(t)
	(&BashWorker{store: partial, model: "m/built"}).recordSpend("root", session.Usage{Input: 40, Output: 15, CostUSD: 0.05},
		map[string]session.Usage{
			"m/fallback": {Input: 20, Output: 7, CostUSD: 0.03},
			"":           {Input: 5, Output: 3, CostUSD: 0.005},
		})
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
