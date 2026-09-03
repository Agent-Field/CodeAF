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

	// The question and the word that answers it are the aside; only the
	// result line is the answer a script captures (streams.go).
	commentary := captureAside(t)
	var declined strings.Builder
	if err := runRebuildWith([]string{"--db", path}, strings.NewReader("n\n"), &declined); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(commentary.String(), "cancelled") {
		t.Fatalf("a bare newline rebuilt the store: %q", commentary.String())
	}
	if declined.Len() != 0 {
		t.Fatalf("a rebuild nobody agreed to wrote to the answer stream: %q", declined.String())
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

// TestTheRebuildQuestionIsAnAsideAndNotInThePipe is row 28.
//
// `aforge rebuild` asked `[y/N] ` on the command's `output` — os.Stdout in the
// shipped binary — so `aforge rebuild | tee log` handed the person a blank
// terminal waiting for a word they could not see, and put the question into the
// data file. It is the exact defect `cache clean` was fixed for.
//
// And the question was in the storage engine's own words. `materialized view`
// is not something a person has to know to decide whether they want this; what
// is thrown away is everything aforge worked out from the journal.
func TestTheRebuildQuestionIsAnAsideAndNotInThePipe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	graph, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	commentary := captureAside(t)
	var answer strings.Builder
	if err := runRebuildWith([]string{"--db", path}, strings.NewReader("y\n"), &answer); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(answer.String(), "[y/N]") {
		t.Errorf("the question is in the stream a script captures — a `| tee` of this waits on a word "+
			"nobody can see, and writes the question into the data file:\n%s", answer.String())
	}
	if !strings.Contains(commentary.String(), "[y/N]") {
		t.Errorf("the question was not written to the aside either, so nobody is asked at all:\n%s",
			commentary.String())
	}
	if !strings.Contains(answer.String(), "rebuilt") {
		t.Errorf("the answer line left with the question; stdout should still carry what was done:\n%s",
			answer.String())
	}
	for _, machinery := range []string{"materialized view", "derived table"} {
		if strings.Contains(commentary.String()+usageText, machinery) {
			t.Errorf("%q is the storage engine's word for itself and reaches a person here", machinery)
		}
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
