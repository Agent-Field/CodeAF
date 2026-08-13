package resident

import (
	"context"
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

// The sink is the one node a splice names with the bare prefix, and the mirror
// used to spell every node "<prefix>-n<id>", root included. So a revision that
// rewired the deliverable's own inputs addressed "<prefix>-n<root>" — an id no
// store has ever held — and was refused as an unknown node, into a batch note
// nobody reads, while the plan document recorded the edit as applied.
//
// What that costs is not an edge. It is the whole answer: a job delivered an
// empty deliverable with the finished report sitting on disk, because the sink
// was still waiting on the work the revision had moved it off.
//
// The store's edges are what is pinned here, not the plan document — the
// document was never the thing that was wrong.
func TestARevisionRewiresTheSinkInTheStoreAndNotOnlyInThePlan(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	planGraph := &plan.Graph{Goal: "report on the API", Nodes: []plan.Node{
		{ID: 1, Title: "Read the API docs", Kind: plan.KindWork},
		{ID: 2, Title: "Read the changelog", Kind: plan.KindWork},
		{ID: 3, Title: "Write the report", Kind: plan.KindSynthesis, Stage: 2, Needs: []int{1}},
	}}
	subtree, err := SubtreeFromPlan(planGraph, "task-4")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, subtree,
		store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "report on the API"}); err != nil {
		t.Fatal(err)
	}
	// The premise: the sink carries the bare prefix, which is the fact the two
	// spellings disagreed about.
	if _, ok, err := graph.Node("task-4"); err != nil || !ok {
		t.Fatalf("the sink is not named by the bare prefix: ok=%t err=%v", ok, err)
	}

	applied, notes := ApplyRevision(graph, planGraph, "task-4", "task-4", []plan.Operation{
		{Op: "rewire", Node: 3, Needs: []int{1, 2}, Reason: "the report needs the changelog too", Applied: true},
	})
	if applied != 1 {
		t.Fatalf("applied=%d notes=%v", applied, notes)
	}
	edges, err := graph.ActiveEdges()
	if err != nil {
		t.Fatal(err)
	}
	wired := false
	for _, edge := range edges {
		if edge.From == "task-4-n2" && edge.To == "task-4" {
			wired = true
		}
	}
	if !wired {
		t.Fatalf("the rewire never reached the edges table: %v (notes=%v)", edges, notes)
	}
}

// The other half of the same law: a node this batch adds cannot be the root.
//
// The root is decided by reading the plan for the sink nothing consumes, and a
// revision that adds a node consuming the deliverable makes that reading answer
// differently a minute after the store minted the names. The store cannot rename
// a node, so the reading has to be taken as of admission — which is what makes
// the shared mapper a shared law rather than a shared expression.
func TestARevisionsOwnAdditionNeverRenamesTheSink(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	planGraph := &plan.Graph{Goal: "report on the API", Nodes: []plan.Node{
		{ID: 1, Title: "Read the API docs", Kind: plan.KindWork},
		{ID: 2, Title: "Write the report", Kind: plan.KindSynthesis, Stage: 2, Needs: []int{1}},
	}}
	subtree, err := SubtreeFromPlan(planGraph, "task-7")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, subtree,
		store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "report on the API"}); err != nil {
		t.Fatal(err)
	}

	// The sentinel adds a check that reads the finished report, and retitles the
	// report in the same breath. The add makes node 2 a consumed node.
	planGraph.Nodes = append(planGraph.Nodes, plan.Node{
		ID: 3, Title: "Check the report against the docs", Kind: plan.KindWork, Stage: 3, Needs: []int{2},
	})
	planGraph.Nodes[1].Title = "Write the report, with versions"
	applied, notes := ApplyRevision(graph, planGraph, "task-7", "task-7", []plan.Operation{
		{Op: "add", Node: 3, Reason: "the report should be checked", Applied: true},
		{Op: "retitle", Node: 2, Reason: "versions matter", Applied: true},
	})
	if applied != 2 {
		t.Fatalf("applied=%d notes=%v", applied, notes)
	}
	sink, ok, err := graph.Node("task-7")
	if err != nil || !ok {
		t.Fatalf("the sink lost its name: ok=%t err=%v", ok, err)
	}
	if sink.Title != "Write the report, with versions" {
		t.Fatalf("the retitle missed the sink: %q (notes=%v)", sink.Title, notes)
	}
	if _, ok, _ := graph.Node("task-7-n3"); !ok {
		t.Fatalf("the addition was not named as a child: %v", notes)
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

// ── the two splice paths, and what they used to disagree about ───────────────
//
// There are two ways a repair reaches a running graph. The overrun splice wires
// its entry nodes to the node it repairs and to the parts that node was
// assembled from, points the waiting consumers at it, and stands it where a
// deliverable stands when there is nothing left to gather it. The revision
// sentinel did none of that: its additions arrived with whatever inputs a model
// had named by integer, dropped the ones it could not resolve without a word,
// and were parented under a deliverable that was not waiting for them. These
// pin the parity.

// openRevisionGraph is one planned job as the store holds it: a deliverable sink
// standing on the spine, one work leaf under it, and the plan document both were
// minted from.
func openRevisionGraph(t *testing.T, prefix string) (*store.Store, *plan.Graph) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	planGraph := &plan.Graph{Goal: "report on the API", Nodes: []plan.Node{
		{ID: 1, Title: "Survey the API", Kind: plan.KindWork},
		{ID: 2, Title: "Write the report", Kind: plan.KindSynthesis, Stage: 2, Needs: []int{1}},
	}}
	subtree, err := SubtreeFromPlan(planGraph, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, subtree,
		store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "report on the API"}); err != nil {
		t.Fatal(err)
	}
	return graph, planGraph
}

func settleRevisionNode(t *testing.T, graph *store.Store, id, summary, failure string) store.Node {
	t.Helper()
	claim, ok, err := graph.Claim(id, "w1")
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if failure != "" {
		if err := graph.Fail(claim, failure); err != nil {
			t.Fatal(err)
		}
	} else if err := graph.Complete(claim, summary); err != nil {
		t.Fatal(err)
	}
	settled, found, err := graph.Node(id)
	if err != nil || !found {
		t.Fatalf("read %s: found=%t err=%v", id, found, err)
	}
	return settled
}

func revisionEdge(t *testing.T, graph *store.Store, from, to string) bool {
	t.Helper()
	edges, err := graph.ActiveEdges()
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Kind != store.Suggests {
			return true
		}
	}
	return false
}

// A replacement for a failed leaf has to be able to read the leaf it replaces
// and the parts that leaf had already assembled, or it is a node asked to redo
// work with no access to any of it. The overrun splice has wired exactly that
// since it existed (repairSources, attachNeeds); the sentinel's own splice wired
// nothing, and the honest replacements said so and refused to invent the rest.
//
// The other half of the same edge is the reader: the deliverable that was
// waiting on the failed leaf now waits on its replacement, which is what puts
// the repair in front of the gate at all.
func TestARevisionsRepairReadsTheWorkItReplaces(t *testing.T) {
	graph, planGraph := openRevisionGraph(t, "task-1")
	for _, part := range []struct{ id, summary string }{
		{"task-1-n1-p1", "the v1 endpoints, listed"},
		{"task-1-n1-p2", "the auth flow, described"},
	} {
		if err := graph.Splice("task-1-n1", store.Subtree{Nodes: []store.NodeSpec{
			{ID: part.id, Brief: "part of the survey"},
		}}, store.Provenance{Origin: store.OriginSelf, SessionID: "s1", Intent: "split the survey"}); err != nil {
			t.Fatal(err)
		}
		settleRevisionNode(t, graph, part.id, part.summary, "")
	}
	failed := settleRevisionNode(t, graph, "task-1-n1", "", "every v2 endpoint answers 410 Gone")

	planGraph.Nodes[0].State = plan.StateFailed
	planGraph.Nodes[0].Failure = "every v2 endpoint answers 410 Gone"
	planGraph.Nodes = append(planGraph.Nodes, plan.Node{
		ID: 3, Title: "Survey the API through v1", Summary: "v2 is gone; survey v1 instead", Stage: 1})

	applied, notes := ApplyRevisionGoverned(context.Background(),
		Growth{Reason: GrowRevision, After: failed}, graph, planGraph, "task-1", "task-1",
		[]plan.Operation{{Op: "add", Node: 3, Reason: "v2 answers 410 for every endpoint", Applied: true}})
	if applied != 1 {
		t.Fatalf("applied=%d notes=%v", applied, notes)
	}

	if !revisionEdge(t, graph, "task-1-n1", "task-1-n3") {
		t.Fatal("the replacement cannot see the leaf it replaces")
	}
	for _, part := range []string{"task-1-n1-p1", "task-1-n1-p2"} {
		if !revisionEdge(t, graph, part, "task-1-n3") {
			t.Fatalf("the replacement cannot see %s, which the failed leaf had already assembled", part)
		}
	}
	if !revisionEdge(t, graph, "task-1-n3", "task-1") {
		t.Fatal("the deliverable is not waiting for the repair, so the repair is delivered to nobody")
	}
	added, ok, err := graph.Node("task-1-n3")
	if err != nil || !ok {
		t.Fatalf("the repair was not admitted: ok=%t err=%v", ok, err)
	}
	if added.Parent != "task-1" {
		t.Fatalf("a repair the deliverable gathers stands outside its job: parent=%q", added.Parent)
	}
}

// An input the sentinel names by integer and the store cannot resolve was
// dropped in silence: the addition landed one input short, and nothing anywhere
// recorded which. A container that was expanded away at admission is the
// everyday shape of it — the id is real in the plan document and has never named
// a store node — and the addition wired to it is a repair reading none of the
// work it was convened over.
func TestARevisionSurfacesAnInputItCannotResolve(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	// Node 1 is a container: the adapter expands it into its child and admits no
	// store node of its own.
	planGraph := &plan.Graph{Goal: "report on the API", Nodes: []plan.Node{
		{ID: 1, Title: "Explore the API surface"},
		{ID: 2, Title: "Read the docs", Kind: plan.KindWork, Parent: 1},
		{ID: 3, Title: "Write the report", Kind: plan.KindSynthesis, Stage: 2, Needs: []int{1}},
	}}
	subtree, err := SubtreeFromPlan(planGraph, "task-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, subtree,
		store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "report on the API"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := graph.Node("task-2-n1"); ok {
		t.Fatal("the container was admitted; the premise of this test is gone")
	}

	planGraph.Nodes = append(planGraph.Nodes, plan.Node{
		ID: 4, Title: "Check the docs against the changelog", Stage: 2, Needs: []int{1}})
	applied, notes := ApplyRevisionGoverned(context.Background(),
		Growth{Reason: GrowRevision}, graph, planGraph, "task-2", "task-2",
		[]plan.Operation{{Op: "add", Node: 4, Reason: "the docs and the changelog disagree", Applied: true}})
	if applied != 1 {
		t.Fatalf("the addition itself was lost: applied=%d notes=%v", applied, notes)
	}
	if len(notes) != 1 {
		t.Fatalf("the dropped input was not surfaced: notes=%v", notes)
	}
	if !strings.Contains(notes[0], "Explore the API surface") {
		t.Fatalf("the note does not say which input went missing: %q", notes[0])
	}
	// §5d: the note is read by a person, so it names the step and not the row.
	for _, machinery := range []string{"task-2-n1", "node "} {
		if strings.Contains(notes[0], machinery) {
			t.Fatalf("the note carries %q into the room: %q", machinery, notes[0])
		}
	}
}

// The gate and the announcer look at exactly one place: the nodes standing on
// the spine. A repair the sentinel splices under a deliverable that has already
// started is therefore work nothing is waiting for and nothing will ever judge —
// it finishes correctly and is delivered to nobody, which is the failure
// overrun.go names in its own words and avoids by standing such a repair beside
// its job rather than inside it.
//
// The shape is the everyday one: a leaf fails, its failure settles the
// dependency, the deliverable that was waiting on it starts, and only then does
// the sentinel decide the failure needs replacing.
func TestARevisionRepairNobodyWillGatherStandsOnTheSpine(t *testing.T) {
	graph, planGraph := openRevisionGraph(t, "task-3")
	failed := settleRevisionNode(t, graph, "task-3-n1", "", "every v2 endpoint answers 410 Gone")
	claim, ok, err := graph.Claim("task-3", "w1")
	if err != nil || !ok {
		t.Fatalf("claim the deliverable: ok=%t err=%v", ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}

	planGraph.Nodes[0].State = plan.StateFailed
	planGraph.Nodes = append(planGraph.Nodes, plan.Node{
		ID: 3, Title: "Survey the API through v1", Summary: "v2 is gone; survey v1 instead", Stage: 1})
	applied, notes := ApplyRevisionGoverned(context.Background(),
		Growth{Reason: GrowRevision, After: failed}, graph, planGraph, "task-3", "task-3",
		[]plan.Operation{{Op: "add", Node: 3, Reason: "v2 answers 410 for every endpoint", Applied: true}})
	if applied != 1 {
		t.Fatalf("applied=%d notes=%v", applied, notes)
	}
	added, ok, err := graph.Node("task-3-n3")
	if err != nil || !ok {
		t.Fatalf("the repair was not admitted: ok=%t err=%v", ok, err)
	}
	// This is the whole of it: cmd/aforge's shouldGate admits a node whose
	// parent is the spine and nothing else, and announceNode reads the same
	// column.
	if added.Parent != store.RootID {
		t.Fatalf("the repair is parented at %q, where neither the gate nor the announcer looks", added.Parent)
	}
	// It keeps the job's id namespace, which is what carries its workspace, and
	// it still reads the work it replaces.
	if !revisionEdge(t, graph, "task-3-n1", "task-3-n3") {
		t.Fatal("the repair standing beside its job lost sight of the work it replaces")
	}
	if revisionEdge(t, graph, "task-3-n3", "task-3") {
		t.Fatal("a running deliverable was wired to wait for work it will never read")
	}
}

// The cycle the reader law must not close. A sentinel that adds a check reading
// the finished deliverable is adding a node the deliverable must not then be
// made to wait for: the store's AddEdge asks whether a consumer is pending and
// nothing else, so that edge is accepted and both ends wait forever.
func TestARevisionNeverWiresTheDeliverableToWaitForItsOwnChecker(t *testing.T) {
	graph, planGraph := openRevisionGraph(t, "task-5")
	planGraph.Nodes = append(planGraph.Nodes, plan.Node{
		ID: 3, Title: "Check the report against the docs", Stage: 3, Needs: []int{2}})
	applied, notes := ApplyRevisionGoverned(context.Background(),
		Growth{Reason: GrowRevision}, graph, planGraph, "task-5", "task-5",
		[]plan.Operation{{Op: "add", Node: 3, Reason: "the report should be checked", Applied: true}})
	if applied != 1 {
		t.Fatalf("applied=%d notes=%v", applied, notes)
	}
	if !revisionEdge(t, graph, "task-5", "task-5-n3") {
		t.Fatal("the checker lost the deliverable it reads")
	}
	if revisionEdge(t, graph, "task-5-n3", "task-5") {
		t.Fatal("the deliverable and its checker are waiting for each other")
	}
}

// The sentinel used to be told a failure happened and nothing about it. "It
// FAILED" says the plan's next steps have nothing to consume; the reason says
// which assumption died, and that is the entire question it was convened to
// answer.
func TestRevisionEventCarriesTheFailureReasonAndTheFiles(t *testing.T) {
	node := store.Node{ID: "task-1-n2", Title: "Survey the API", Brief: "read the docs"}
	event := RevisionEvent(node, "wrote the notes to api-notes.md",
		[]string{"/workspace/job/api-notes.md"}, "every v2 endpoint answers 410 Gone", 0)
	for _, want := range []string{"Survey the API", "FAILED", "410 Gone",
		"/workspace/job/api-notes.md", "wrote the notes to api-notes.md"} {
		if !strings.Contains(event, want) {
			t.Errorf("event is missing %q:\n%s", want, event)
		}
	}
	// A successful landing says so, and says nothing about a failure.
	settled := RevisionEvent(node, "the notes are written", nil, "", 0)
	if !strings.Contains(settled, "finished") || strings.Contains(settled, "FAILED") {
		t.Fatalf("settled event = %q", settled)
	}
	// Prompt-bound, and never cut through a character.
	long := RevisionEvent(node, strings.Repeat("é", 4000), nil, strings.Repeat("ü", 900), 0)
	if !utf8.ValidString(long) {
		t.Fatal("the event cut a character in half")
	}
	if len(long) > revisionResultBytes+revisionFailureBytes+512 {
		t.Fatalf("event is %d bytes; it is meant to be bounded", len(long))
	}
}
