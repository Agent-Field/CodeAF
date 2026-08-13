package resident

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The sentinel's four edits, mirrored onto the store: add splices, remove
// cancels, rewire replaces edges, retitle amends — and each refuses anything
// that already started.
func TestApplyRevisionMirrorsSentinelEditsOntoTheStore(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job-n9", Brief: "synthesize"},
		{ID: "job-n1", Parent: "job-n9", Brief: "research approach A", Title: "Approach A"},
		{ID: "job-n2", Parent: "job-n9", Brief: "build on A", Title: "Build on A",
			Needs: []store.Need{{NodeID: "job-n1", Kind: store.FeedsInto}}},
		{ID: "job-n3", Parent: "job-n9", Brief: "summarize findings", Title: "Summarize",
			Needs: []store.Need{{NodeID: "job-n2", Kind: store.FeedsInto}}},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "test"}); err != nil {
		t.Fatal(err)
	}

	planGraph := &plan.Graph{Goal: "the goal", Nodes: []plan.Node{
		{ID: 9, Title: "Synthesis", Kind: plan.KindSynthesis},
		{ID: 1, Title: "Approach A", State: plan.StateDone},
		{ID: 2, Title: "Build on B instead", Summary: "approach A was a dead end; build on B"},
		{ID: 3, Title: "Summarize", Summary: "summarize what B produced"},
		{ID: 4, Title: "Research approach B", Summary: "the result showed B is viable", Needs: []int{1}},
	}}

	operations := []plan.Operation{
		{Op: "add", Node: 4, Reason: "result contradicts approach A", Applied: true},
		{Op: "remove", Node: 2, Reason: "A is dead", Applied: true},
		{Op: "rewire", Node: 3, Needs: []int{4}, Reason: "summary now reads from B", Applied: true},
		{Op: "retitle", Node: 3, Reason: "scope shifted", Applied: true},
	}
	applied, notes := ApplyRevision(graph, planGraph, "job", "job-n9", operations)
	if applied != 4 {
		t.Fatalf("applied=%d notes=%v", applied, notes)
	}

	added, ok, _ := graph.Node("job-n4")
	if !ok || added.Parent != "job-n9" || added.Title != "Research approach B" {
		t.Fatalf("added node wrong: ok=%t %+v", ok, added)
	}
	removed, _, _ := graph.Node("job-n2")
	if removed.Status != store.Cancelled {
		t.Fatalf("removed node status = %s", removed.Status)
	}
	amended, _, _ := graph.Node("job-n3")
	if amended.Title != "Summarize" || amended.Brief == "summarize findings" {
		t.Fatalf("retitle did not amend: %+v", amended)
	}

	edges, _ := graph.ActiveEdges()
	oldEdge, newEdge := false, false
	for _, edge := range edges {
		if edge.To == "job-n3" && edge.From == "job-n2" {
			oldEdge = true
		}
		if edge.To == "job-n3" && edge.From == "job-n4" {
			newEdge = true
		}
	}
	if oldEdge || !newEdge {
		t.Fatalf("rewire wrong: old=%t new=%t %v", oldEdge, newEdge, edges)
	}

	// The journal reproduces every revision.
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild after revision: %v", err)
	}
	rebuilt, _, _ := graph.Node("job-n2")
	if rebuilt.Status != store.Cancelled {
		t.Fatalf("rebuild lost the cancellation: %s", rebuilt.Status)
	}

	// The store is the law: editing work that started is refused, not applied.
	claim, ok, err := graph.Claim("job-n1", "w1")
	if err != nil || !ok {
		t.Fatalf("claim: %v", err)
	}
	_ = graph.Start(claim)
	applied, notes = ApplyRevision(graph, planGraph, "job", "job-n9", []plan.Operation{
		{Op: "remove", Node: 1, Reason: "should be refused", Applied: true},
		{Op: "rewire", Node: 1, Refused: "it is already running"},
	})
	if applied != 0 || len(notes) < 2 || !strings.Contains(strings.Join(notes, "\n"), "it is already running") {
		t.Fatalf("started node was edited: applied=%d notes=%v", applied, notes)
	}
}

// §5d. The redirection receipt prints these notes straight under its own, so
// they are read by the person who asked for the change. What they read was
// "· retitle task-8-n4: node 4 is running": the op's own verb, the store's id
// for the row, and the word the product has ruled out — three pieces of
// machinery in one line, saying nothing anyone could act on.
//
// The note names the step by what the step is for, and says what happened to
// it. The composition is what is pinned, not a list of words to avoid.
func TestARevisionNoteNamesTheStepAndNotTheMachinery(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-8", Brief: "deliver together"},
		{ID: "task-8-n4", Parent: "task-8", Brief: "assemble", Title: "Assemble the sections"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "test"}); err != nil {
		t.Fatal(err)
	}
	planGraph := &plan.Graph{Goal: "deliver together", Nodes: []plan.Node{
		{ID: 4, Title: "Assemble the sections", State: plan.StateRunning},
	}}

	_, notes := ApplyRevision(graph, planGraph, "task-8", "task-8", []plan.Operation{
		{Op: "retitle", Node: 4, Refused: "it is already running"},
	})
	if len(notes) != 1 {
		t.Fatalf("notes = %v", notes)
	}
	if notes[0] != "Assemble the sections: it is already running" {
		t.Fatalf("the note reads %q", notes[0])
	}
	for _, machinery := range []string{"retitle", "task-8-n4", "node "} {
		if strings.Contains(notes[0], machinery) {
			t.Fatalf("the note carries %q into the room: %q", machinery, notes[0])
		}
	}
}

// The sentinel used to be told a failure happened and nothing about it. "It
// FAILED" says the plan's next steps have nothing to consume; the reason says
// which assumption died, and that is the entire question it was convened to
// answer.
func TestRevisionEventCarriesTheFailureReasonAndTheFiles(t *testing.T) {
	node := store.Node{ID: "task-1-n2", Title: "Survey the API", Brief: "read the docs"}
	event := RevisionEvent(node, "wrote the notes to api-notes.md",
		[]string{"/workspace/job/api-notes.md"}, "every v2 endpoint answers 410 Gone")
	for _, want := range []string{"Survey the API", "FAILED", "410 Gone",
		"/workspace/job/api-notes.md", "wrote the notes to api-notes.md"} {
		if !strings.Contains(event, want) {
			t.Errorf("event is missing %q:\n%s", want, event)
		}
	}
	// A successful landing says so, and says nothing about a failure.
	settled := RevisionEvent(node, "the notes are written", nil, "")
	if !strings.Contains(settled, "finished") || strings.Contains(settled, "FAILED") {
		t.Fatalf("settled event = %q", settled)
	}
	// Prompt-bound, and never cut through a character.
	long := RevisionEvent(node, strings.Repeat("é", 4000), nil, strings.Repeat("ü", 900))
	if !utf8.ValidString(long) {
		t.Fatal("the event cut a character in half")
	}
	if len(long) > revisionResultBytes+revisionFailureBytes+512 {
		t.Fatalf("event is %d bytes; it is meant to be bounded", len(long))
	}
}
