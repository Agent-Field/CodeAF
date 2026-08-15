package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestScopeAliasEventAndRebuildRoundTrip(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	fact, err := graph.RecordFact("", "domain:podcasts", FactLesson, "normalize loudness before publishing")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.AliasScope("domain:podcasts", "domain:podcast"); err != nil {
		t.Fatal(err)
	}

	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	aliasEvents := 0
	for _, event := range events {
		if event.Kind != EventScopeAliased {
			continue
		}
		aliasEvents++
		var payload scopeAliasedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.From != "domain:podcasts" || payload.To != "domain:podcast" {
			t.Fatalf("alias payload = %+v", payload)
		}
	}
	if aliasEvents != 1 {
		t.Fatalf("scope alias events = %d, want 1", aliasEvents)
	}
	assertScopeAliasState(t, graph, fact.Seq)

	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assertScopeAliasState(t, graph, fact.Seq)
}

func TestScopeAliasResolutionIsTransitiveAndRejectsCycles(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.AliasScope("tool:builders", "tool:builder"); err != nil {
		t.Fatal(err)
	}
	if err := graph.AliasScope("tool:builder", "tool:build-kit"); err != nil {
		t.Fatal(err)
	}
	resolved, err := graph.ResolveScope("tool:builders")
	if err != nil || resolved != "tool:build-kit" {
		t.Fatalf("transitive resolution = %q err=%v", resolved, err)
	}
	if err := graph.AliasScope("tool:build-kit", "tool:builders"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cycle alias error = %v, want ErrInvalid", err)
	}
	resolved, err = graph.ResolveScope("tool:builders")
	if err != nil || resolved != "tool:build-kit" {
		t.Fatalf("resolution after rejected cycle = %q err=%v", resolved, err)
	}
	if _, err := graph.db.Exec(`UPDATE scope_aliases SET to_scope = ? WHERE from_scope = ?`,
		"tool:builders", "tool:builder"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.ResolveScope("tool:builders"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("corrupt materialized cycle error = %v, want ErrInvalid", err)
	}
}

func TestScopeAliasReshelvesActiveFactsOnly(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	superseded, err := graph.RecordFact("", "repo:parsers", FactPlain, "the parser uses generated fixtures")
	if err != nil {
		t.Fatal(err)
	}
	active, err := graph.ReplaceFact(superseded.Seq, "", "repo:parsers", FactPlain, "the parser regenerates fixtures with make check")
	if err != nil {
		t.Fatal(err)
	}
	quarantined, err := graph.RecordFact("", "repo:parsers", FactLesson, "skip the generated fixture check")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.QuarantineFact(quarantined.Seq, 0, FactOriginUser); err != nil {
		t.Fatal(err)
	}
	if err := graph.AliasScope("repo:parsers", "repo:parser"); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		seq       int64
		wantScope string
		status    string
	}{
		{superseded.Seq, "repo:parsers", FactSuperseded},
		{quarantined.Seq, "repo:parsers", FactQuarantined},
		{active.Seq, "repo:parser", FactActive},
	} {
		fact, found, err := graph.FactBySeq(test.seq)
		if err != nil || !found || fact.Scope != test.wantScope || fact.Status != test.status {
			t.Fatalf("fact #%d = %+v found=%t err=%v, want scope %q status %q",
				test.seq, fact, found, err, test.wantScope, test.status)
		}
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		seq       int64
		wantScope string
	}{
		{superseded.Seq, "repo:parsers"},
		{quarantined.Seq, "repo:parsers"},
		{active.Seq, "repo:parser"},
	} {
		fact, found, err := graph.FactBySeq(test.seq)
		if err != nil || !found || fact.Scope != test.wantScope {
			t.Fatalf("rebuilt fact #%d = %+v found=%t err=%v, want scope %q",
				test.seq, fact, found, err, test.wantScope)
		}
	}
}

func TestFactRetrievalResolvesScopeAlias(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	fact, err := graph.RecordFact("", "domain:podcast-production", FactLesson, "measure room tone before recording")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.AliasScope("domain:podcast-production", "domain:podcast"); err != nil {
		t.Fatal(err)
	}
	found, err := graph.SearchFacts(FactQuery{Cues: []string{"domain:podcast-production"}, Limit: 5})
	if err != nil || len(found) != 1 || found[0].Seq != fact.Seq || found[0].Scope != "domain:podcast" {
		t.Fatalf("alias retrieval = %+v err=%v", found, err)
	}
}

func assertScopeAliasState(t *testing.T, graph *Store, factSeq int64) {
	t.Helper()
	aliases, err := graph.ScopeAliases()
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 1 || aliases[0].From != "domain:podcasts" || aliases[0].To != "domain:podcast" {
		t.Fatalf("scope aliases = %+v", aliases)
	}
	fact, found, err := graph.FactBySeq(factSeq)
	if err != nil || !found || fact.Scope != "domain:podcast" {
		t.Fatalf("aliased fact = %+v found=%t err=%v", fact, found, err)
	}
}
