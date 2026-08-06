package resident

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestExtractCuesOrdersAndDeduplicatesScopes(t *testing.T) {
	got := ExtractCues("Use git on internal/resident/notebook.go, then internal/resident/notebook.go and internal/store/facts.go with curl.")
	want := []string{
		"file:internal/resident/notebook.go",
		"file:internal/store/facts.go",
		"repo:internal/resident",
		"repo:internal",
		"repo:internal/store",
		"tool:git",
		"tool:curl",
		"user",
		"env",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractCues() = %#v, want %#v", got, want)
	}
}

func TestFailureDistillationRecordsScopedQuirkOnce(t *testing.T) {
	graph := openStore(t)
	err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "goal", Brief: "Run the suite", Stage: 2},
		{ID: "flaky", Parent: "goal", Brief: "Run pytest", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "session-failure", Intent: "run the tests"})
	if err != nil {
		t.Fatalf("splice fixture: %v", err)
	}

	calls := 0
	reconciler := New(graph, nil, nil).WithDistiller(
		func(_ context.Context, goal, outcome string, failed bool) ([]Learned, error) {
			calls++
			if goal != "run the tests" || outcome != "pytest loses its cache" || !failed {
				t.Fatalf("distill input = goal %q outcome %q failed %v", goal, outcome, failed)
			}
			return []Learned{{
				Scope: "tool:pytest",
				Kind:  store.FactQuirk,
				Body:  "pytest can lose its cache mid-run",
			}}, nil
		})
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("initialize watcher: %v", err)
	}

	claim, won, err := graph.Claim("flaky", "worker")
	if err != nil || !won {
		t.Fatalf("claim failed node: won=%v err=%v", won, err)
	}
	if err := graph.Fail(claim, "pytest loses its cache"); err != nil {
		t.Fatalf("fail node: %v", err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("distill failure: %v", err)
	}
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatalf("repeat tick: %v", err)
	}
	if calls != 1 {
		t.Fatalf("failure distilled %d times, want once", calls)
	}

	facts, err := graph.ActiveFacts("tool:pytest", 10)
	if err != nil {
		t.Fatalf("read distilled facts: %v", err)
	}
	if len(facts) != 1 || facts[0].NodeID != "flaky" || facts[0].Kind != store.FactQuirk ||
		facts[0].Body != "pytest can lose its cache mid-run" {
		t.Fatalf("distilled facts = %+v", facts)
	}
}

func TestNotebookDigestRetrievesPathScopeAndEmptyNotebook(t *testing.T) {
	graph := openStore(t)
	if got := NotebookDigest(graph, "inspect internal/resident/notebook.go", "fix cue lookup", 5); got != "" {
		t.Fatalf("empty notebook digest = %q", got)
	}
	if _, err := graph.RecordFact("", "file:internal/resident/notebook.go", store.FactPlain,
		"notebook.go keeps cues in priority order"); err != nil {
		t.Fatalf("record fact: %v", err)
	}

	got := NotebookDigest(graph, "inspect internal/resident/notebook.go", "fix cue lookup", 5)
	if !strings.HasPrefix(got, "notebook") || !strings.Contains(got, "- notebook.go keeps cues in priority order") {
		t.Fatalf("NotebookDigest() = %q, want header plus the recorded fact", got)
	}
}

func TestConsolidationFiresOnlyAboveThresholdAndReplacesScope(t *testing.T) {
	t.Run("at threshold", func(t *testing.T) {
		graph := openStore(t)
		recordScopeFacts(t, graph, "repo:threshold", consolidationThreshold)
		calls := 0
		reconciler := New(graph, nil, nil).WithConsolidator(
			func(context.Context, string, []store.Fact) ([]Learned, error) {
				calls++
				return nil, nil
			})
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		if calls != 0 {
			t.Fatalf("consolidator called at threshold")
		}
	})

	t.Run("above threshold", func(t *testing.T) {
		graph := openStore(t)
		recordScopeFacts(t, graph, "repo:overgrown", consolidationThreshold+1)
		calls := 0
		reconciler := New(graph, nil, nil).WithConsolidator(
			func(_ context.Context, scope string, facts []store.Fact) ([]Learned, error) {
				calls++
				if scope != "repo:overgrown" || len(facts) != consolidationThreshold+1 {
					t.Fatalf("consolidation input = scope %q facts %d", scope, len(facts))
				}
				return []Learned{
					{Scope: scope, Kind: store.FactLesson, Body: "overgrown uses bounded retries"},
					{Scope: scope, Kind: store.FactPlain, Body: "overgrown keeps a journal"},
				}, nil
			})
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		if err := reconciler.Tick(context.Background()); err != nil {
			t.Fatalf("repeat tick: %v", err)
		}
		if calls != 1 {
			t.Fatalf("consolidator calls = %d, want one", calls)
		}

		facts, err := graph.ActiveFacts("repo:overgrown", 100)
		if err != nil {
			t.Fatalf("read consolidated facts: %v", err)
		}
		if len(facts) != 2 {
			t.Fatalf("active facts after consolidation = %+v", facts)
		}
		bodies := facts[0].Body + "\n" + facts[1].Body
		for _, want := range []string{"overgrown uses bounded retries", "overgrown keeps a journal"} {
			if !strings.Contains(bodies, want) {
				t.Errorf("rewritten facts %q omit %q", bodies, want)
			}
		}

		events, err := graph.Events(0, 0)
		if err != nil {
			t.Fatalf("read events: %v", err)
		}
		superseded := 0
		for _, event := range events {
			if event.Kind == store.EventFactSuperseded {
				superseded++
			}
		}
		if superseded != consolidationThreshold+1 {
			t.Fatalf("supersession events = %d, want %d", superseded, consolidationThreshold+1)
		}
	})
}

func TestRenderCompileContextRetrievesNotebookByCue(t *testing.T) {
	graph := openStore(t)
	if _, err := graph.RecordFact("", "file:internal/resident/notebook.go", store.FactQuirk,
		"cue extraction strips line-number suffixes"); err != nil {
		t.Fatalf("record relevant fact: %v", err)
	}
	if _, err := graph.RecordFact("", "tool:curl", store.FactQuirk,
		"curl retries uploads twice"); err != nil {
		t.Fatalf("record irrelevant fact: %v", err)
	}
	snapshot, err := graph.ActiveSnapshot()
	if err != nil {
		t.Fatalf("active snapshot: %v", err)
	}

	got := New(graph, nil, nil).renderCompileContext(snapshot,
		"change internal/resident/notebook.go:42")
	if !strings.Contains(got, "cue extraction strips line-number suffixes") {
		t.Fatalf("compile context omitted cue-scoped fact: %q", got)
	}
	if strings.Contains(got, "curl retries uploads twice") {
		t.Fatalf("compile context included unrelated recent fact: %q", got)
	}
}

func recordScopeFacts(t *testing.T, graph *store.Store, scope string, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		if _, err := graph.RecordFact("", scope, store.FactPlain,
			fmt.Sprintf("original fact %02d", index)); err != nil {
			t.Fatalf("record fact %d: %v", index, err)
		}
	}
}
