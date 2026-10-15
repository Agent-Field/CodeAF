package run

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// spendRows is the store's spend ledger for one task, model to dollars.
func spendRows(t *testing.T, path, task string) map[string]float64 {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT model, usd FROM spend WHERE task_id = ?`, task)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var model string
		var usd float64
		if err := rows.Scan(&model, &usd); err != nil {
			t.Fatal(err)
		}
		out[model] += usd
	}
	return out
}

// A SEAT WHOSE MODEL FAILED OVER IS WRITTEN DOWN AS THE MODELS THAT ANSWERED,
// and the rows still sum to what it spent; with no breakdown the one row names
// the seat's model, as it always did.
func TestSpendIsWrittenUnderTheModelsThatAnswered(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "p", "root", "Fix", "Fix it")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	worker := &BashWorker{store: store, model: "m/built"}
	used := session.Usage{Input: 30, Output: 12, CostUSD: 0.04}
	worker.recordSpend("root", used, map[string]session.Usage{
		"m/fallback": {Input: 20, Output: 7, CostUSD: 0.03},
		"m/built":    {Input: 10, Output: 5, CostUSD: 0.01},
	})
	rows := spendRows(t, store.Path(), "root")
	if len(rows) != 2 || rows["m/fallback"] != 0.03 || rows["m/built"] != 0.01 {
		t.Fatalf("rows %+v", rows)
	}

	other, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "p", "root", "Fix", "Fix it")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	(&BashWorker{store: other, model: "m/built"}).recordSpend("root", used, nil)
	if rows := spendRows(t, other.Path(), "root"); len(rows) != 1 || rows["m/built"] != 0.04 {
		t.Fatalf("no breakdown wrote %+v", rows)
	}
}
