package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Rebuild is the routine that makes good the claim the whole architecture
// rests on — every table is a projection of the journal — and it had no caller
// outside the test suite. This exercises the operator's path end to end,
// including the confirmation, because a command that discards every derived
// table must not be one keystroke away by accident.
func TestRebuildReplaysTheJournalBehindAConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "do the thing", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "do the thing"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	var declined strings.Builder
	if err := runRebuildWith([]string{"--db", path}, strings.NewReader("n\n"), &declined); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(declined.String(), "cancelled") {
		t.Fatalf("a bare newline rebuilt the store: %q", declined.String())
	}

	var done strings.Builder
	if err := runRebuildWith([]string{"--db", path, "--yes"}, strings.NewReader(""), &done); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if !strings.Contains(done.String(), "rebuilt") {
		t.Fatalf("rebuild said nothing: %q", done.String())
	}

	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	node, found, err := reopened.Node("job")
	if err != nil || !found || node.Brief != "do the thing" {
		t.Fatalf("replayed node = %+v found=%t err=%v", node, found, err)
	}
}

// A journaled plan has no materialized view, so replay must accept it as the
// deliberate boundary it is rather than choking on an unknown kind — and must
// still be able to hand the plan back afterwards, since the event itself
// remains the source of truth.
func TestAJournaledPlanSurvivesARebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-3", Brief: "do the thing", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "do the thing"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordPlanGraph("task-3", store.PlanGraph{
		Root: "task-3", Model: "work/model", Graph: []byte(`{"goal":"do the thing"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild refused a journaled plan: %v", err)
	}
	journaled, found, err := graph.PlanGraphFor("task-3")
	if err != nil || !found || journaled.Root != "task-3" || journaled.Model != "work/model" {
		t.Fatalf("plan after rebuild = %+v found=%t err=%v", journaled, found, err)
	}
}
