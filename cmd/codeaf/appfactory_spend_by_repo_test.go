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

// factory.spend_by_repo is the factory's own per-repository spend over its
// calendar days, read from the job documents it keeps: hello announces it,
// capabilities say so, a bad range is refused before anything is sent, and
// the answer names the connection and repeats the range asked.
func TestAppFactorySpendByRepoReadsTheFactorysJobSpendByRepository(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	if !strings.Contains(alice.w.lines[0], appwire.FeatureFactorySpendByRepo) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactorySpendByRepo, alice.w.lines[0])
	}
	var caps appwire.FactoryCapabilities
	alice.w.must("factory.capabilities", map[string]any{}, &caps)
	if !caps.SpendByRepo {
		t.Fatalf("capabilities = %+v, want spend_by_repo", caps)
	}
	october := func(day int) time.Time { return time.Date(2026, time.October, day, 12, 0, 0, 0, time.UTC) }
	f.create(t, factory.Item{Repo: "acme/api", State: factory.StateShipped, Stream: &factory.Stream{Started: october(2), Spent: 1.5}})
	f.create(t, factory.Item{Repo: "acme/api", State: factory.StateShipped, Stream: &factory.Stream{Started: october(9), Spent: 0.25}})
	f.create(t, factory.Item{Repo: "acme/api", State: factory.StateShipped, Stream: &factory.Stream{Started: october(1).AddDate(0, -1, 0), Spent: 4}})

	var got appwire.FactorySpendByRepo
	alice.w.must("factory.spend_by_repo", map[string]any{"factory": alice.conn.ID, "from": "2026-10-01", "to": "2026-10-31"}, &got)
	want := appwire.FactorySpendByRepo{Factory: alice.conn.ID, SpendByRepo: api.SpendByRepo{
		From: "2026-10-01", To: "2026-10-31", TimeZone: "UTC", TotalUSD: 1.75, Repos: []api.RepoSpend{{Repo: "acme/api", SpentUSD: 1.75, Jobs: 2}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spend_by_repo = %+v, want %+v", got, want)
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
		alice.refused(t, "factory.spend_by_repo", bad, "bad_params")
	}
	if f.postCount() != sent {
		t.Fatal("a refused read wrote to the factory")
	}
}
