package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// WITHDRAWN IS NOT LOST.
//
// The job's own second thought drops a step when a landed result shows that
// what the step was for is already covered, and the store records that as a
// cancellation like every other. The delivery composition, told nothing else,
// counted the withdrawal as a casualty and closed a job that had landed whole
// with "Not all of this landed… much of it is missing" — a false statement
// about the thing the person was reading, made by the system that had just
// made the decision it was describing.
func TestAStepDroppedAsRedundantIsNotReportedAsMissing(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "withdrawn.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "port the parser", Stage: 2},
		{ID: "part-a", Parent: "job", Title: "write the parser", Brief: "write it", Stage: 1},
		{ID: "part-b", Parent: "job", Title: "port the lexer", Brief: "port it", Stage: 1},
		{ID: "part-c", Parent: "job", Title: "re-check the parser", Brief: "check it", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "port the parser"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"part-a", "part-b"} {
		claim, ok, err := graph.Claim(id, "worker")
		if err != nil || !ok {
			t.Fatal(err)
		}
		if err := graph.Start(claim); err != nil {
			t.Fatal(err)
		}
		if err := graph.Complete(claim, "done, and its own checks passed"); err != nil {
			t.Fatal(err)
		}
	}
	// The reviser's own words, through the one path that writes them.
	if err := graph.CancelPending("part-c",
		resident.WithdrawnByPlan+`"write the parser" already ran the suite and it was green`); err != nil {
		t.Fatal(err)
	}
	root, _, err := graph.Node("job")
	if err != nil {
		t.Fatal(err)
	}

	note := failedPartsNote(graph, root)
	if strings.Contains(note, "Not all of this landed") || strings.Contains(note, "much of it is missing") {
		t.Fatalf("a withdrawn step was reported as a hole in the delivery:\n%s", note)
	}
	for _, want := range []string{
		"became unnecessary",
		"re-check the parser",
		"already ran the suite and it was green",
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("the note does not say what actually happened (%q):\n%s", want, note)
		}
	}
	// The reviser's own prefix is machinery and belongs in the record, not in a
	// sentence somebody reads.
	if strings.Contains(note, resident.WithdrawnByPlan) {
		t.Fatalf("the note leaked the revision prefix:\n%s", note)
	}
}

// The other half of the same split, so neither can quietly absorb the other: a
// step that really did fail is still a hole, it is still counted, and the count
// is against what the job was still asking for.
func TestAGenuineFailureIsStillReportedAsMissing(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "dnf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "port the parser", Stage: 2},
		{ID: "part-a", Parent: "job", Title: "write the parser", Brief: "write it", Stage: 1},
		{ID: "part-b", Parent: "job", Title: "port the lexer", Brief: "port it", Stage: 1},
		{ID: "part-c", Parent: "job", Title: "re-check the parser", Brief: "check it", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "port the parser"}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim("part-a", "worker")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "done"); err != nil {
		t.Fatal(err)
	}
	claim, ok, err = graph.Claim("part-b", "worker")
	if err != nil || !ok {
		t.Fatal(err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fail(claim, "the lexer port did not finish — openrouter: 500 upstream is unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := graph.CancelPending("part-c", resident.WithdrawnByPlan+"already covered by the parser work"); err != nil {
		t.Fatal(err)
	}
	root, _, err := graph.Node("job")
	if err != nil {
		t.Fatal(err)
	}

	note := failedPartsNote(graph, root)
	// Two of the three parts were still being asked for, and one of them died.
	if !strings.Contains(note, "1 of 2 parts finished") {
		t.Fatalf("the count is wrong — a withdrawn step is not one of the parts that did not finish:\n%s", note)
	}
	if !strings.Contains(note, "upstream is unavailable") {
		t.Fatalf("the real failure is unnamed:\n%s", note)
	}
	if !strings.Contains(note, "much of it is missing") {
		t.Fatalf("a real hole stopped being announced:\n%s", note)
	}
	if !strings.Contains(note, "became unnecessary") {
		t.Fatalf("the withdrawal was folded back into the failures:\n%s", note)
	}
}
