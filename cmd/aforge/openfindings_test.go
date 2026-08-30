package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE TEXTUAL RUN OF 2026-08-29 (s9), AT THE MOMENT IT STOPPED TELLING ANYBODY.
//
// Its coverage gate measured the same two unexercised behaviours on all three of
// its rounds — 2, then 2, then 2 — and those three rounds briefed fourteen
// nodes. THIRTEEN OF THE FOURTEEN NAME NEITHER BEHAVIOUR; one names one, by luck
// of the planner's wording. The job measured its shortfall three times, spent
// fourteen leaves on it, and never once told a worker what it was.
//
// The findings were structured when they were measured and were flattened into
// prose on the only path that mattered: written into a goal, handed to a
// planner, and restated in whatever words that planner chose. A model
// summarising a page of instructions drops a two-item list most times it is
// asked.
func TestEveryRepairBriefNamesEveryOpenFinding(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: "RichLog still snaps back to the newest entry", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "richlog follow state"}); err != nil {
		t.Fatalf("splice: %v", err)
	}

	const (
		first  = "RichLog exposes is_following_end"
		second = "RichLog.write(expand=True) preserves justified rendering"
		gap    = "the deliverable describes the work rather than carrying it"
	)
	if err := graph.RecordDeliveryGate("task-2", store.DeliveryGate{
		Pass: false, Gap: gap, Unexercised: []string{first, second}, Unclosed: true,
	}); err != nil {
		t.Fatalf("record the gate: %v", err)
	}

	// The repair round's own brief, as the planner wrote it: the planner's words
	// name neither behaviour, which is the fourteen-brief measurement above.
	planned := "You are writing tests for RichLog snap-back and expand behaviors."
	child := store.Node{ID: "task-2-x1-n2", Parent: "task-2-x1", Brief: planned}

	brief := withOpenFindings(graph, child, planned)
	for _, want := range []string{first, second, gap} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the repair brief does not name %q:\n%s", want, brief)
		}
	}
	// It goes FIRST, because a worker that reads one section reads the first,
	// and what a round exists to close belongs above the assignment that has
	// already been attempted once.
	if !strings.HasPrefix(brief, "WHAT THIS JOB IS STILL SHORT OF") {
		t.Fatalf("the findings are not the first thing the worker reads:\n%s", firstLine(brief))
	}
	// And the assignment survives underneath it.
	if !strings.Contains(brief, planned) {
		t.Fatal("the findings section replaced the assignment rather than preceding it")
	}

	// A FIRST ATTEMPT IS TOLD NOTHING, because there is nothing measured yet: a
	// heading announcing no findings teaches the model the heading means
	// nothing. `task-2` is its own lineage and its gate has not run when its
	// first worker reads its brief.
	fresh, freshErr := store.Open(filepath.Join(t.TempDir(), "fresh.db"))
	if freshErr != nil {
		t.Fatalf("open store: %v", freshErr)
	}
	defer fresh.Close()
	if err := fresh.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: "do the thing", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "do the thing"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	opening := store.Node{ID: "task-2", Parent: store.RootID, Brief: "do the thing"}
	if got := withOpenFindings(fresh, opening, "do the thing"); got != "do the thing" {
		t.Fatalf("a first attempt was handed a findings section:\n%s", got)
	}
}
