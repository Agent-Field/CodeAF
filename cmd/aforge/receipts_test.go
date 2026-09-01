package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// seedErrandGraph is one settled errand in a real store: a root, its leaf, and
// whatever worker name the splice was written with — the empty string on
// anything this build writes, and a stored name on a graph that predates it.
// The watcher reads nodes exactly as it does in a live run, which is the only
// way this proves anything about the field.
func seedErrandGraph(t *testing.T, session, worker string) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "fix the failing test", Stage: 0},
		{ID: "task-1-leaf", Parent: "task-1", Brief: "make it pass", Stage: 1},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: session, Intent: "fix the failing test",
		Subharness: worker,
	}); err != nil {
		t.Fatal(err)
	}
	return graph
}

func errandWatcher(graph *store.Store, session string, progress *strings.Builder) *settlementWatch {
	return &settlementWatch{
		graph: graph, session: session, refused: make(chan planEstimate, 1),
		progress: progress, started: time.Now(),
	}
}

// The bench harness was reading the chosen worker out of a kept sqlite file
// because nothing on stdout named it. The field is always present, and the
// default is spelled out: an absent field and an older binary look the same to
// a machine caller, and the whole point of the column is telling cells apart.
func TestHeadlessOutcomeAlwaysNamesTheWorker(t *testing.T) {
	for _, probe := range []struct{ chosen, want string }{
		{"", exec.LinearSubharness},
		{exec.LinearSubharness, exec.LinearSubharness},
		// A row an older build wrote. The column says what the store says; the
		// note on the stream (below) is where the run admits it ran linear.
		{"retired-worker", "retired-worker"},
	} {
		session := "headless-worker-" + probe.want
		graph := seedErrandGraph(t, session, probe.chosen)
		var progress strings.Builder
		watcher := errandWatcher(graph, session, &progress)
		nodes, err := watcher.sessionNodes()
		if err != nil {
			t.Fatal(err)
		}
		outcome := watcher.compose(nodes)
		if outcome.Subharness != probe.want {
			t.Fatalf("chosen %q composed as %q, want %q", probe.chosen, outcome.Subharness, probe.want)
		}
		encoded, err := json.Marshal(outcome)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["subharness"] != probe.want {
			t.Fatalf("--json carried %v, want %q", decoded["subharness"], probe.want)
		}
	}
}

// The understood line is said once and carries no worker at all — there is one,
// and a line that named it would be decoration on every errand this program
// runs.
func TestUnderstoodLineIsSaidOnceAndNamesNoWorker(t *testing.T) {
	for _, chosen := range []string{"", exec.LinearSubharness, "retired-worker"} {
		session := "headless-understood"
		graph := seedErrandGraph(t, session, chosen)
		var progress strings.Builder
		watcher := errandWatcher(graph, session, &progress)
		nodes, err := watcher.sessionNodes()
		if err != nil {
			t.Fatal(err)
		}
		watcher.report(nodes)
		watcher.report(nodes)
		said := progress.String()
		if !strings.Contains(said, "· understood · 2 tasks") {
			t.Fatalf("stored worker %q said:\n%s", chosen, said)
		}
		if strings.Count(said, "understood") != 1 {
			t.Fatalf("stored worker %q said understood %d times:\n%s",
				chosen, strings.Count(said, "understood"), said)
		}
		if strings.Contains(said, "understood · 2 tasks (") {
			t.Fatalf("stored worker %q decorated the line with a worker:\n%s", chosen, said)
		}
	}
}

// Degradation is the law; silent degradation is a measurement of the wrong
// thing. A node from an older graph, whose row names a worker that no longer
// exists, says so once per node — and an ordinary node says nothing at all.
func TestDegradationIsSaidOncePerNode(t *testing.T) {
	session := "headless-degraded"
	graph := seedErrandGraph(t, session, "retired-worker")
	var progress strings.Builder
	watcher := errandWatcher(graph, session, &progress)
	nodes, err := watcher.sessionNodes()
	if err != nil {
		t.Fatal(err)
	}
	watcher.report(nodes)
	watcher.report(nodes)
	said := progress.String()
	if count := strings.Count(said, "this build has one worker"); count != len(nodes) {
		t.Fatalf("said the note %d times for %d nodes:\n%s", count, len(nodes), said)
	}
	if !strings.Contains(said, `note: "retired-worker" is not a worker; ran linear, and this build has one worker`) {
		t.Fatalf("the note does not say what happened:\n%s", said)
	}

	for _, ordinary := range []string{"", exec.LinearSubharness} {
		plain := seedErrandGraph(t, session, ordinary)
		var quiet strings.Builder
		watching := errandWatcher(plain, session, &quiet)
		plainNodes, err := watching.sessionNodes()
		if err != nil {
			t.Fatal(err)
		}
		watching.report(plainNodes)
		if strings.Contains(quiet.String(), "is not a worker") {
			t.Fatalf("an ordinary run apologized for its worker:\n%s", quiet.String())
		}
	}
}

// The conversational surface has no stderr anybody reads, and the note is not
// conversation. It goes where every other machinery fact about one leaf goes:
// the node's own flight recorder, once, before the worker writes a turn into it.
func TestDegradationReachesTheNodesFlightRecorder(t *testing.T) {
	session := "chat-degraded"
	graph := seedErrandGraph(t, session, "retired-worker")
	workspace := t.TempDir()
	seatLeafWorkerNotes(workspace, "", graph)
	t.Cleanup(func() { seatLeafWorkerNotes("", "", nil) })

	node, found, err := graph.Node("task-1-leaf")
	if err != nil || !found {
		t.Fatalf("read the leaf: found=%t err=%v", found, err)
	}
	if got := leafSubharness(node); got != "retired-worker" {
		t.Fatalf("leaf worker = %q", got)
	}
	leafSubharness(node)

	// Through the shared spelling, not a fourth hand-built copy of it: the note
	// and the recorder have to land in one file, and a test that builds the path
	// itself would go on passing after they stopped doing so.
	body, err := os.ReadFile(exec.TraceFile(filepath.Join(workspace, "task-1"), node.ID))
	if err != nil {
		t.Fatalf("read the trace: %v", err)
	}
	if want := `note: "retired-worker" is not a worker; ran linear, and this build has one worker`; !strings.Contains(string(body), want) {
		t.Fatalf("the recorder does not carry the note: %q", string(body))
	}
	if strings.Count(string(body), "is not a worker") != 1 {
		t.Fatalf("the note was said twice: %q", string(body))
	}

	// An ordinary leaf leaves no trace of a limit that was never reached.
	plain := seedErrandGraph(t, "chat-plain", "")
	seatLeafWorkerNotes(workspace, "", plain)
	ordinary, _, err := plain.Node("task-1-leaf")
	if err != nil {
		t.Fatal(err)
	}
	if got := leafSubharness(ordinary); got != "" {
		t.Fatalf("an ordinary leaf named a worker: %q", got)
	}
}
