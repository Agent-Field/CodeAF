package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// spendingRead is factory.spending's answer as a client reads it: a figure
// that is not set is null, and a model that is not set is "".
type spendingRead struct {
	Factory          string   `json:"factory"`
	DefaultBudgetUSD *float64 `json:"default_budget_usd"`
	AutoStartMaxUSD  *float64 `json:"auto_start_max_usd"`
	DefaultModel     string   `json:"default_model"`
}

// spendingWire is the app over a real solo factory handler and its store.
func spendingWire(t *testing.T) (*factoryWire, string, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.Seam{GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "alice"}, nil }}
	h := serve.New(seam, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "alice", Store: st})
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); h.Close() })
	w := newFactoryWire(t, testFactoryHub(t))
	w.backend.listModels = func() ([]appwire.Model, error) {
		return []appwire.Model{{ID: "deepseek/deepseek-v4.1-flash"}, {ID: "vendor/other"}}, nil
	}
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	return w, conn.ID, st
}

// THE THREE FACTORY-WIDE SETTINGS ARE READ AND WRITTEN THROUGH THE APP, END TO
// END against the real factory handler: nothing is set at first (nulls and ""),
// a write changes only the fields it names and answers what the factory kept,
// null clears a figure, and a stale expect writes nothing.
func TestFactorySpendingIsReadAndWrittenThroughTheApp(t *testing.T) {
	w, id, _ := spendingWire(t)
	for _, word := range []string{"factory.default_budget", "factory.auto_start_cap", "factory.default_model"} {
		if !strings.Contains(w.hello, `"`+word+`"`) {
			t.Fatalf("hello does not announce %s: %s", word, w.hello)
		}
	}
	var got spendingRead
	w.must("factory.spending", map[string]any{"factory": id}, &got)
	if got.Factory != id || got.DefaultBudgetUSD != nil || got.AutoStartMaxUSD != nil || got.DefaultModel != "" {
		t.Fatalf("a fresh factory reads %+v", got)
	}

	var set spendingRead
	w.must("factory.spending_set", map[string]any{"factory": id, "default_budget_usd": 12.5, "auto_start_max_usd": 0, "default_model": "deepseek/deepseek-v4.1-flash"}, &set)
	if set.DefaultBudgetUSD == nil || *set.DefaultBudgetUSD != 12.5 || set.AutoStartMaxUSD == nil || *set.AutoStartMaxUSD != 0 || set.DefaultModel != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("the write answered %+v", set)
	}
	got = spendingRead{}
	w.must("factory.spending", map[string]any{"factory": id}, &got)
	if got.DefaultBudgetUSD == nil || *got.DefaultBudgetUSD != 12.5 || got.AutoStartMaxUSD == nil || *got.AutoStartMaxUSD != 0 || got.DefaultModel != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("the read after the write is %+v", got)
	}

	// Only the named field changes; null clears a figure and "" the model.
	w.must("factory.spending_set", map[string]any{"factory": id, "auto_start_max_usd": nil}, &set)
	if set.AutoStartMaxUSD != nil || set.DefaultBudgetUSD == nil || *set.DefaultBudgetUSD != 12.5 || set.DefaultModel == "" {
		t.Fatalf("clearing one figure answered %+v", set)
	}
	w.must("factory.spending_set", map[string]any{"factory": id, "default_model": ""}, &set)
	if set.DefaultModel != "" || set.DefaultBudgetUSD == nil {
		t.Fatalf("clearing the model answered %+v", set)
	}

	// A stale read writes nothing.
	stale := map[string]any{"default_budget_usd": 3}
	w.mustFail("factory.spending_set", map[string]any{"factory": id, "default_budget_usd": 4, "expect": stale}, appwire.CodeStale)
	current := map[string]any{"default_budget_usd": 12.5}
	w.must("factory.spending_set", map[string]any{"factory": id, "default_budget_usd": 4, "expect": current}, &set)
	if set.DefaultBudgetUSD == nil || *set.DefaultBudgetUSD != 4 {
		t.Fatalf("a current expect answered %+v", set)
	}
}

// A WRITE THE FACTORY WOULD NOT KEEP IS REFUSED BEFORE ANYTHING IS SENT: a job
// always has a budget, a cap is never below nothing, a model is one the app
// offers, and a field it does not know is never believed honoured.
func TestFactorySpendingRefusesWhatCannotBeKept(t *testing.T) {
	w, id, _ := spendingWire(t)
	for name, params := range map[string]map[string]any{
		"zero budget":    {"factory": id, "default_budget_usd": 0},
		"negative cap":   {"factory": id, "auto_start_max_usd": -1},
		"huge budget":    {"factory": id, "default_budget_usd": 1e9},
		"unknown model":  {"factory": id, "default_model": "vendor/nobody-offers-this"},
		"model spaces":   {"factory": id, "default_model": "two words"},
		"unknown field":  {"factory": id, "rail_usd": 3},
		"no factory":     {"default_budget_usd": 3},
		"nothing to set": {"factory": id},
	} {
		if _, code, _ := w.call("factory.spending_set", params); code != appwire.CodeBadParams {
			t.Errorf("%s: code %q, want %q", name, code, appwire.CodeBadParams)
		}
	}
	var got spendingRead
	w.must("factory.spending", map[string]any{"factory": id}, &got)
	if got.DefaultBudgetUSD != nil || got.AutoStartMaxUSD != nil || got.DefaultModel != "" {
		t.Fatalf("a refused write changed %+v", got)
	}
}

// WHAT IS SET IS WHAT A JOB RUNS UNDER. A repository that says nothing of its
// own takes the factory's default budget and its cap for starting on its own;
// a repository that sets a cap keeps it unless the factory's is stricter.
func TestFactorySpendingIsWhatAJobRunsUnder(t *testing.T) {
	w, id, st := spendingWire(t)
	var set spendingRead
	w.must("factory.spending_set", map[string]any{"factory": id, "default_budget_usd": 9, "auto_start_max_usd": 2}, &set)
	f, err := withFactoryHouseRules(st, pipeline.Defaults(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.DefaultBudget != 9 || f.Start.MaxCost != 2 {
		t.Fatalf("a silent repository reads budget %v, cap %v", f.DefaultBudget, f.Start.MaxCost)
	}
	own, err := pipeline.Parse("default_budget: 3\nstart:\n  max_cost: 1\n")
	if err != nil {
		t.Fatal(err)
	}
	f, err = withFactoryHouseRules(st, own, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.DefaultBudget != 3 || f.Start.MaxCost != 1 {
		t.Fatalf("a repository's own figures read budget %v, cap %v", f.DefaultBudget, f.Start.MaxCost)
	}
}
