package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// ranFixture is one node in one store, claimed and started, which is the state
// the dispatch path finds a leaf in at the moment it builds the leaf's worker.
func ranFixture(t *testing.T) (*store.Store, leafBuild) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "fix the failing test", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s",
		Intent: "fix the failing test"}); err != nil {
		t.Fatal(err)
	}
	build := leafBuild{
		settings: config.Config{ProfileDir: t.TempDir()}, graph: graph,
		maxTurns: 1, maxTokens: 1000, deadline: time.Minute,
	}
	return graph, build
}

func ranOf(t *testing.T, graph *store.Store, id string) store.Node {
	t.Helper()
	node, found, err := graph.Node(id)
	if err != nil || !found {
		t.Fatalf("read %s: found=%t err=%v", id, found, err)
	}
	return node
}

// THE DEFECT, IN ONE ASSERTION. Every node of the s9 sweep's ink and igel stores
// carried a blank worker, because nothing routed them and nothing wrote down
// that the generalist had therefore taken them. A blank is what four different
// things look like — never planned, never claimed, never dispatched, routed to
// something nobody recorded — so the autopsy could not begin.
func TestTheGeneralistWritesItsOwnNameDown(t *testing.T) {
	graph, build := ranFixture(t)
	if worker := runningWorker("task-1", "", build, ""); worker.Subharness() != exec.LinearSubharness {
		t.Fatalf("an unrouted node was built a %q", worker.Subharness())
	}
	node := ranOf(t, graph, "task-1")
	if node.Ran != exec.LinearSubharness {
		t.Fatalf("the node that ran says %q ran it, want %q", node.Ran, exec.LinearSubharness)
	}
	// The ASK is untouched. It is a different fact in a different column: the
	// compiler routed nothing, and that stays true however the node was run.
	if node.Subharness != "" {
		t.Fatalf("recording who ran overwrote what was asked for: %q", node.Subharness)
	}
	// Said once. A released fold and a repair round rebuild the same worker,
	// and a journal that filed each one as a hand-over would read as a run that
	// changed workers three times.
	runningWorker("task-1", "", build, "")
	if events := ranEvents(t, graph, "task-1"); len(events) != 1 {
		t.Fatalf("rebuilding the same worker wrote %d events, want 1", len(events))
	}
}

// The other half of the rule: an escalation is a change of worker, and the
// change is a fact about the run with a reason attached to it.
func TestAnEscalatedNodeRecordsTheWorkerThatTookIt(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole"})
	graph, build := ranFixture(t)

	runningWorker("task-1", "", build, "")
	reason := "escalated from linear after a failed attempt"
	if worker := runningWorker("task-1", "swe", build, reason); worker.Subharness() != "swe" {
		t.Fatalf("the escalation built a %q", worker.Subharness())
	}
	if node := ranOf(t, graph, "task-1"); node.Ran != "swe" {
		t.Fatalf("the escalated node says %q ran it", node.Ran)
	}
	events := ranEvents(t, graph, "task-1")
	if len(events) != 2 {
		t.Fatalf("the hand-over left %d events, want 2", len(events))
	}
	last := events[len(events)-1]
	if last.Subharness != "swe" || last.Previous != exec.LinearSubharness {
		t.Fatalf("the escalation event reads %q from %q", last.Subharness, last.Previous)
	}
	if last.Reason != reason {
		t.Fatalf("the escalation event lost its reason: %q", last.Reason)
	}
	// And it survives the rebuild, because the row is a view of the journal and
	// a leaf reclaimed after a restart must not read back as generalist work.
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if node := ranOf(t, graph, "task-1"); node.Ran != "swe" {
		t.Fatalf("the rebuild forgot who ran it: %q", node.Ran)
	}
}

// A worker this build cannot construct runs the generalist — that has always
// been the registry's promise — and the store now says which of the two
// actually happened, with the promise it could not keep beside it. A benchmark
// cell that silently became a default cell is a measurement of the wrong thing.
func TestADegradedWorkerRecordsWhatActuallyRanAndWhy(t *testing.T) {
	graph, build := ranFixture(t)
	runningWorker("task-1", "swe", build, "")
	node := ranOf(t, graph, "task-1")
	if node.Ran != exec.LinearSubharness {
		t.Fatalf("a worker this build has not says %q ran it", node.Ran)
	}
	events := ranEvents(t, graph, "task-1")
	if len(events) != 1 || !strings.Contains(events[0].Reason, "swe") {
		t.Fatalf("the degradation was recorded as %+v", events)
	}
}

// ranEvent is the payload shape this package reads back out of the journal. It
// is spelled here rather than exported from the store for the same reason the
// store's own payloads are unexported: the event is the contract, and a test
// that read the writer's struct would agree with itself about a field name the
// journal never carried.
type ranEvent struct {
	Subharness string `json:"subharness"`
	Previous   string `json:"previous"`
	Reason     string `json:"reason"`
}

func ranEvents(t *testing.T, graph *store.Store, id string) []ranEvent {
	t.Helper()
	events, err := graph.Events(0, 500)
	if err != nil {
		t.Fatal(err)
	}
	var found []ranEvent
	for _, event := range events {
		if event.Kind != store.EventNodeRan || event.NodeID != id {
			continue
		}
		var payload ranEvent
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode %s: %v", event.Kind, err)
		}
		found = append(found, payload)
	}
	return found
}

// THE ONE SEAM. executorFor resolves a name to a worker and writes nothing
// down; runningWorker is that resolution plus the record, and it is the only
// caller the surface has. A second caller would be a dispatch path that ran a
// node and left the column blank — which is the whole defect, reintroduced
// somewhere nobody would think to look.
func TestTheWorkerThatRanIsWrittenAtOneSeam(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	callers := map[string]int{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		// The enclosing function of every call, so the report names the place a
		// person has to go and look rather than a line number.
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(function, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "executorFor" {
					callers[function.Name.Name]++
				}
				return true
			})
		}
	}
	if len(callers) != 1 || callers["runningWorker"] != 1 {
		t.Fatalf("executorFor is called from %v; the surface has exactly one dispatch seam, "+
			"and it is runningWorker — a caller that resolves a worker without journaling it "+
			"puts the blank subharness column back", callers)
	}
}

// The stream says it too, so that a person watching a run does not have to open
// a database to answer "who is doing this". The compile summary has named a
// specialist since it existed; this is the same courtesy on every node, and it
// includes the generalist, because the runs that were unreadable were exactly
// the runs where every worker was the generalist.
func TestTheRunningLineNamesTheWorkerAndWaitsForIt(t *testing.T) {
	said := &strings.Builder{}
	watcher := &settlementWatch{progress: said, started: time.Now(), structured: true}
	node := store.Node{ID: "task-1", Title: "fix the failing test", Status: store.Running}

	// The claim is granted and the row is stamped running a moment before the
	// dispatch path writes the worker down. A ▶ printed in that moment could
	// not answer the question it exists to answer, so it waits.
	if watcher.report([]store.Node{node}) {
		t.Fatalf("a line nobody could write yet was counted as movement: %q", said.String())
	}
	if said.Len() != 0 {
		t.Fatalf("the running line went out without its worker: %q", said.String())
	}

	node.Ran = exec.LinearSubharness
	if !watcher.report([]store.Node{node}) {
		t.Fatal("the running line never arrived")
	}
	if !strings.Contains(said.String(), "▶") || !strings.Contains(said.String(), "(linear)") {
		t.Fatalf("the running line does not name the generalist: %q", said.String())
	}

	// And the finishing line names whoever finished it, which after an
	// escalation is not who started it.
	said.Reset()
	node.Status, node.Ran = store.Done, "swe"
	watcher.report([]store.Node{node})
	if !strings.Contains(said.String(), "✓") || !strings.Contains(said.String(), "(swe)") {
		t.Fatalf("the finishing line does not name the worker: %q", said.String())
	}
}

// The grace has a floor under it: a fact that never arrives must cost a word,
// never the line. A node that started is a node that started, and a stream that
// swallowed its ▶ waiting for a record nothing was going to write would be the
// silence this whole seam exists to end, wearing a different hat.
func TestTheRunningLinePrintsWithoutAWorkerRatherThanNotAtAll(t *testing.T) {
	said := &strings.Builder{}
	watcher := &settlementWatch{progress: said, started: time.Now(), structured: true,
		waiting: map[string]time.Time{"task-1": time.Now().Add(-2 * runningWorkerGrace)}}
	node := store.Node{ID: "task-1", Title: "fix the failing test", Status: store.Running}
	if !watcher.report([]store.Node{node}) {
		t.Fatalf("the line was held past its grace: %q", said.String())
	}
	if !strings.Contains(said.String(), "▶") {
		t.Fatalf("the run lost a line waiting for a worker nobody wrote: %q", said.String())
	}
}
