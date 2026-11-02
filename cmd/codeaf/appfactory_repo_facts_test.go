package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// factory.repo_facts is one repository's facts counted by the factory over
// every job it keeps: hello announces it, capabilities say so, bad params
// are refused before anything is sent, and the answer names the connection,
// the repository, the window and the factory's zone, with each fact's own
// phrase and an engine source.
func TestAppFactoryRepoFactsReadsOneRepositorysFactsFromTheFactory(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	if !strings.Contains(alice.w.lines[0], appwire.FeatureFactoryRepoFacts) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryRepoFacts, alice.w.lines[0])
	}
	var caps appwire.FactoryCapabilities
	alice.w.must("factory.capabilities", map[string]any{}, &caps)
	if !caps.RepoFacts {
		t.Fatalf("capabilities = %+v, want repo_facts", caps)
	}
	month := time.Now().UTC()
	f.create(t, factory.Item{Repo: "acme/api", State: factory.StateShipped, Change: &factory.Change{Files: []factory.ChangedFile{{Path: "cli/main.go"}}},
		Stream: &factory.Stream{Started: month.Add(-time.Minute), Spent: 1.5}})

	var got appwire.FactoryRepoFacts
	alice.w.must("factory.repo_facts", map[string]any{"factory": alice.conn.ID, "repo": " acme/api ", "days": 7, "months": 1}, &got)
	if got.Factory != alice.conn.ID || got.Repo != "acme/api" || got.Days != 7 || got.Months != 1 || got.AsOf.IsZero() {
		t.Fatalf("repo_facts = %+v", got)
	}
	want := map[string]string{
		"usd.month:" + month.Format("2006-01"): "$1.50 spent on 1 job in " + month.Format("January 2006"),
		"area:cli/":                            "cli/: 1 job in the last 7 days",
	}
	for _, fact := range got.Facts {
		if !strings.HasPrefix(fact.Source, "engine:") {
			t.Fatalf("fact %+v has no engine source", fact)
		}
		if phrase, ok := want[fact.ID]; ok && fact.Phrase == phrase {
			delete(want, fact.ID)
		}
	}
	if len(want) > 0 {
		t.Fatalf("repo_facts lacks %v in %+v", want, got.Facts)
	}

	sent := f.postCount()
	for _, bad := range []map[string]any{
		{"factory": alice.conn.ID},
		{"factory": alice.conn.ID, "repo": "acme"},
		{"factory": alice.conn.ID, "repo": "acme/api", "days": 367},
		{"factory": alice.conn.ID, "repo": "acme/api", "months": 13},
		{"repo": "acme/api"},
		{"factory": alice.conn.ID, "repo": "acme/api", "from": "2026-10-01"},
	} {
		alice.refused(t, "factory.repo_facts", bad, "bad_params")
	}
	if f.postCount() != sent {
		t.Fatal("a refused read wrote to the factory")
	}
}
