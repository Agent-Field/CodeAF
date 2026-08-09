package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// seedErrandGraph is one settled errand in a real store: a root that carries a
// worker choice, and its leaf. The watcher reads nodes exactly as it does in a
// live run, which is the only way this proves anything about the field.
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
		{"swe", "swe"},
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

// The understood line names a specialist once and stays exactly as it was for
// the errand that took the default — which is nearly every errand.
func TestUnderstoodLineNamesANonDefaultWorkerOnce(t *testing.T) {
	for _, probe := range []struct {
		chosen string
		want   string
		absent string
	}{
		{"", "· understood · 2 tasks", "("},
		{exec.LinearSubharness, "· understood · 2 tasks", "("},
		{"swe", "· understood · 2 tasks (swe)", ""},
	} {
		session := "headless-understood"
		graph := seedErrandGraph(t, session, probe.chosen)
		var progress strings.Builder
		watcher := errandWatcher(graph, session, &progress)
		nodes, err := watcher.sessionNodes()
		if err != nil {
			t.Fatal(err)
		}
		watcher.report(nodes)
		watcher.report(nodes)
		said := progress.String()
		if !strings.Contains(said, probe.want) {
			t.Fatalf("chosen %q said:\n%s\nwant %q", probe.chosen, said, probe.want)
		}
		if strings.Count(said, "understood") != 1 {
			t.Fatalf("chosen %q said understood %d times:\n%s", probe.chosen, strings.Count(said, "understood"), said)
		}
		if probe.absent != "" && strings.Contains(said, probe.absent) {
			t.Fatalf("chosen %q decorated the default line:\n%s", probe.chosen, said)
		}
	}
}

// Degradation is the law; silent degradation is a measurement of the wrong
// thing. A node promised a worker this build was not compiled with says so once
// — and a build that does have the worker says nothing at all.
func TestDegradationIsSaidOncePerNode(t *testing.T) {
	session := "headless-degraded"
	graph := seedErrandGraph(t, session, "swe")
	var progress strings.Builder
	watcher := errandWatcher(graph, session, &progress)
	nodes, err := watcher.sessionNodes()
	if err != nil {
		t.Fatal(err)
	}
	watcher.report(nodes)
	watcher.report(nodes)
	said := progress.String()
	if count := strings.Count(said, `not in this build`); count != len(nodes) {
		t.Fatalf("said the note %d times for %d nodes:\n%s", count, len(nodes), said)
	}
	if !strings.Contains(said, `note: worker "swe" not in this build; ran linear`) {
		t.Fatalf("the note does not say what happened:\n%s", said)
	}

	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "coding"})
	var honored strings.Builder
	kept := errandWatcher(graph, session, &honored)
	kept.report(nodes)
	if strings.Contains(honored.String(), "not in this build") {
		t.Fatalf("a build that has the worker apologized for it:\n%s", honored.String())
	}
}

// The conversational surface has no stderr anybody reads, and the note is not
// conversation. It goes where every other machinery fact about one leaf goes:
// the node's own flight recorder, once, before the worker writes a turn into it.
func TestDegradationReachesTheNodesFlightRecorder(t *testing.T) {
	session := "chat-degraded"
	graph := seedErrandGraph(t, session, "swe")
	workspace := t.TempDir()
	seatLeafWorkerNotes(workspace, "", graph)
	t.Cleanup(func() { seatLeafWorkerNotes("", "", nil) })

	node, found, err := graph.Node("task-1-leaf")
	if err != nil || !found {
		t.Fatalf("read the leaf: found=%t err=%v", found, err)
	}
	if got := leafSubharness(node); got != "swe" {
		t.Fatalf("leaf worker = %q", got)
	}
	leafSubharness(node)

	trace := filepath.Join(workspace, "task-1", ".obs",
		strconv.FormatInt(node.CreatedSeq, 10)+".trace.log")
	body, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("read the trace: %v", err)
	}
	if want := `note: worker "swe" not in this build; ran linear`; !strings.Contains(string(body), want) {
		t.Fatalf("the recorder does not carry the note: %q", string(body))
	}
	if strings.Count(string(body), "not in this build") != 1 {
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
