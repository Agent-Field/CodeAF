package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// factory.spend_by_model is the same dollars as factory.spend_by_repo, split
// by the model a call was recorded on. Two jobs on two models, plus a factory
// AI call booked on the first job, come back dearest first. A job recorded
// before any model line keeps its whole spend as unknown_usd. The model lines
// and that unknown add up to spend_by_repo's total for the same days.
func TestAppFactorySpendByModelSplitsTheFactorysJobSpendByTheRecordedModel(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	if !strings.Contains(alice.w.lines[0], appwire.FeatureFactorySpendByModel) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactorySpendByModel, alice.w.lines[0])
	}
	var caps appwire.FactoryCapabilities
	alice.w.must("factory.capabilities", map[string]any{}, &caps)
	if !caps.SpendByModel {
		t.Fatalf("capabilities = %+v, want spend_by_model", caps)
	}
	october := func(day int) time.Time { return time.Date(2026, time.October, day, 12, 0, 0, 0, time.UTC) }
	f.create(t, factory.Item{Repo: "acme/api", State: factory.StateShipped, Stream: &factory.Stream{
		Started: october(2), Spent: 1.5,
		Models: []factory.ModelUse{
			{Model: "vendor/writer", USD: 1.25, Calls: 3, Input: 100, Output: 40},
			{Model: "vendor/notes", USD: 0.25, Calls: 1, Input: 20, Output: 8},
		},
	}})
	f.create(t, factory.Item{Repo: "acme/api", State: factory.StateShipped, Stream: &factory.Stream{
		Started: october(9), Spent: 0.5,
		Models: []factory.ModelUse{{Model: "vendor/reviewer", USD: 0.5, Calls: 2, Input: 50, Output: 10}},
	}})
	// A job from before model lines: its spend is unknown, not guessed.
	f.create(t, factory.Item{Repo: "acme/api", State: factory.StateShipped, Stream: &factory.Stream{Started: october(4), Spent: 0.25}})
	f.create(t, factory.Item{Repo: "acme/api", State: factory.StateShipped, Stream: &factory.Stream{Started: october(1).AddDate(0, -1, 0), Spent: 4}})

	params := map[string]any{"factory": alice.conn.ID, "from": "2026-10-01", "to": "2026-10-31"}
	var got appwire.FactorySpendByModel
	alice.w.must("factory.spend_by_model", params, &got)
	want := appwire.FactorySpendByModel{Factory: alice.conn.ID, SpendByModel: api.SpendByModel{
		From: "2026-10-01", To: "2026-10-31", TimeZone: "UTC", UnknownUSD: 0.25,
		Models: []api.ModelSpend{
			{Model: "vendor/writer", CostUSD: 1.25, Calls: 3, InputTokens: 100, OutputTokens: 40, Jobs: 1},
			{Model: "vendor/reviewer", CostUSD: 0.5, Calls: 2, InputTokens: 50, OutputTokens: 10, Jobs: 1},
			{Model: "vendor/notes", CostUSD: 0.25, Calls: 1, InputTokens: 20, OutputTokens: 8, Jobs: 1},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spend_by_model = %+v, want %+v", got, want)
	}
	var byRepo appwire.FactorySpendByRepo
	alice.w.must("factory.spend_by_repo", params, &byRepo)
	var sum float64
	for _, row := range got.Models {
		sum += row.CostUSD
	}
	sum += got.UnknownUSD
	if byRepo.TotalUSD != 2.25 || sum != 2.25 || sum != byRepo.TotalUSD {
		t.Fatalf("model spend %v and spend_by_repo %v, want both 2.25", sum, byRepo.TotalUSD)
	}

	sent := f.postCount()
	for _, bad := range []map[string]any{
		{"factory": alice.conn.ID, "from": "2026-10-01"},
		{"factory": alice.conn.ID, "to": "2026-10-31"},
		{"factory": alice.conn.ID, "from": "2026-10-31", "to": "2026-10-01"},
		{"factory": alice.conn.ID, "from": "2025-10-01", "to": "2026-10-31"},
		{"factory": alice.conn.ID, "from": "1 Oct", "to": "2026-10-31"},
		{"from": "2026-10-01", "to": "2026-10-31"},
		{"factory": alice.conn.ID, "from": "2026-10-01", "to": "2026-10-31", "repo": "acme/api"},
	} {
		alice.refused(t, "factory.spend_by_model", bad, "bad_params")
	}
	if f.postCount() != sent {
		t.Fatal("a refused read wrote to the factory")
	}
}
