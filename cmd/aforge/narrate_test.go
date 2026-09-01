package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// narrationFixture is one errand with one node, which is the shape of the run
// that produced the incident: a single leaf, working, saying nothing a status
// column could show.
func narrationFixture(t *testing.T) (*store.Store, *settlementWatch, *strings.Builder) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	session := "headless-narration"
	command, err := graph.RequestCommand(store.Command{
		SessionID: session, Kind: store.CommandSplice, Instruction: "fix the failing test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Title: "fix the failing test", Brief: "fix the failing test", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session,
		Intent: "fix the failing test"}); err != nil {
		t.Fatal(err)
	}
	said := &strings.Builder{}
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: make(chan planEstimate, 1), progress: said,
		started: time.Now(), quiet: time.Millisecond,
	}
	return graph, watcher, said
}

// narrated runs one pass of the printer over whatever the journal now holds and
// answers what it wrote. It reads the nodes the way the watcher itself does, so
// the membership test under examination is the real one.
func narrated(t *testing.T, watcher *settlementWatch, said *strings.Builder) string {
	t.Helper()
	said.Reset()
	nodes, err := watcher.sessionNodes()
	if err != nil {
		t.Fatal(err)
	}
	watcher.narrate(nodes)
	return said.String()
}

// THE FOUR FACTS THAT USED TO REACH NOBODY.
//
// Every one of these was in the store on 2026-08-28 while the headless stream
// said `still waiting: 0 tasks pending, 1 running — 10m57s`, and the operator
// killed a run that was recovering correctly. FAILSAFE.md rule 3.
func TestThePrinterSaysTheFactsAStatusColumnCannot(t *testing.T) {
	graph, watcher, said := narrationFixture(t)

	// A caught fault. The leaf did not fail — it faulted, and the recovery ran on
	// for minutes — so nothing about the node's status says this happened.
	if err := graph.RecordNodeFault("task-1", "leaf",
		"internal fault in chat/leaf executor: runtime error: index out of range [0] with length 0\nand a stack nobody reads here"); err != nil {
		t.Fatal(err)
	}
	line := narrated(t, watcher, said)
	if !strings.Contains(line, "✗") || !strings.Contains(line, "runtime error: index out of range") {
		t.Fatalf("the fault was not said:\n%s", line)
	}
	if strings.Contains(line, "and a stack nobody reads here") {
		t.Fatalf("the fault line carried more than its first line:\n%s", line)
	}

	// A change of worker, out of a journal written when a run could have one.
	// Nothing writes this event any more — this build has one worker — but every
	// graph.db that carries it still has to read, so the event is handed to the
	// printer the way the journal hands it over.
	said.Reset()
	watcher.narrateOne(store.Event{
		Kind: store.EventNodeWorkerChanged, NodeID: "task-1",
		Payload: []byte(`{"subharness":"second","previous":"first",` +
			`"reason":"escalated from first after a failed attempt"}`),
	}, store.Node{ID: "task-1", Title: "fix the failing test"}, nil)
	line = said.String()
	if !strings.Contains(line, "↻") || !strings.Contains(line, "escalated first → second") {
		t.Fatalf("an older journal's worker change was not said:\n%s", line)
	}
	if !strings.Contains(line, "escalated from first after a failed attempt") {
		t.Fatalf("the worker change did not say why:\n%s", line)
	}

	// A phase. The seven minutes of silence in the incident were one of these,
	// and the hint is what stops somebody killing it at minute four.
	if _, err := graph.PostMessage(store.Message{
		SessionID: watcher.session, Role: store.RoleSystem, NodeID: "task-1",
		Body:     "baseline",
		Progress: &store.MessageProgress{Phase: "baseline"},
	}); err != nil {
		t.Fatal(err)
	}
	line = narrated(t, watcher, said)
	if !strings.Contains(line, "baseline") {
		t.Fatalf("the phase was not said:\n%s", line)
	}
	if !strings.Contains(line, "this can take minutes") {
		t.Fatalf("the phase did not say what to expect of it:\n%s", line)
	}
	// A replaceable row repeats itself until it moves; the stream must not.
	if _, err := graph.PostMessage(store.Message{
		SessionID: watcher.session, Role: store.RoleSystem, NodeID: "task-1",
		Body:     "baseline",
		Progress: &store.MessageProgress{Phase: "baseline"},
	}); err != nil {
		t.Fatal(err)
	}
	if repeat := narrated(t, watcher, said); strings.Contains(repeat, "baseline") {
		t.Fatalf("the same phase was said twice:\n%s", repeat)
	}

	// The delivery gate's judgement, which decides what the person is handed.
	if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
		Pass: false, Gap: "the plan promised out.txt and the workspace does not hold it",
		Refused: "the repair round was refused: nothing in the request is quoted",
	}); err != nil {
		t.Fatal(err)
	}
	line = narrated(t, watcher, said)
	if !strings.Contains(line, "gate: refused") {
		t.Fatalf("the gate's refusal was not said:\n%s", line)
	}
	if !strings.Contains(line, "nothing in the request is quoted") {
		t.Fatalf("the gate's refusal did not say why:\n%s", line)
	}
}

// `still waiting` is what is printed when NOTHING is known. After rule 3 it is
// rarely true that nothing is known, and the line must stand down for a fact
// that has just gone past — otherwise the stream reports silence two lines
// under the evidence that contradicts it.
func TestStillWaitingStandsDownForAFactThatJustArrived(t *testing.T) {
	graph, watcher, said := narrationFixture(t)

	// A run with nothing to say still says so: this is the line's whole job and
	// it is not being taken away.
	watcher.lastMoved, watcher.lastSaid = time.Now().Add(-time.Minute), time.Now().Add(-time.Minute)
	if err := watcher.saySomethingIfQuiet(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(said.String(), "still waiting") {
		t.Fatalf("a run with nothing known said nothing:\n%s", said.String())
	}

	// And now a fact arrives. The quiet line has something better to report than
	// silence, so it does not report silence.
	if err := graph.RecordNodeFault("task-1", "leaf", "runtime error: index out of range [0]"); err != nil {
		t.Fatal(err)
	}
	watcher.lastMoved, watcher.lastSaid = time.Now().Add(-time.Minute), time.Now().Add(-time.Minute)
	narrated(t, watcher, said)
	said.Reset()
	if err := watcher.saySomethingIfQuiet(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(said.String(), "still waiting") {
		t.Fatalf("the run said it was waiting on nothing a moment after saying what it was doing:\n%s", said.String())
	}
}

// Another errand's journal is not this errand's news. The narrator reads the
// whole journal — there is one — and everything it says has to be about the
// nodes this command owns.
func TestTheNarratorOnlySaysThisErrandsFacts(t *testing.T) {
	graph, watcher, said := narrationFixture(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "someone-else", Title: "a different job", Brief: "a different job", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "another-session",
		Intent: "a different job"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordNodeFault("someone-else", "leaf", "runtime error in another run"); err != nil {
		t.Fatal(err)
	}
	if line := narrated(t, watcher, said); strings.Contains(line, "another run") {
		t.Fatalf("the watcher reported a node it does not own:\n%s", line)
	}
}

// THE CHECKLIST HAS TO REACH THE PERSON WATCHING. A fail-safe that does not
// propagate to the verdict a person reads is decoration (FAILSAFE clause 3),
// and a "no check exercises …" arriving forty minutes into a run is illegible
// to somebody who was never told a checklist existed.
//
// It is a count and not a list: forty behaviours printed one per line would bury
// every other line in the stream.
func TestTheStreamSaysTheChecklistExistsOnce(t *testing.T) {
	graph, watcher, said := narrationFixture(t)

	if err := graph.RecordAcceptance("task-1", store.Acceptance{Points: []store.AcceptancePoint{
		{Behaviour: "A rejected non-listed status does not close half-open state",
			Quote: "must not close half-open state"},
		{Behaviour: "A half-open probe holds its slot across internal retries",
			Quote: "keeps its slot for the full logical request"},
	}}); err != nil {
		t.Fatal(err)
	}
	line := narrated(t, watcher, said)
	if !strings.Contains(line, "acceptance") || !strings.Contains(line, "2 points from the request") {
		t.Fatalf("the stream never said the checklist exists:\n%s", line)
	}
	// The behaviours themselves stay out of the stream: the count is the news.
	if strings.Contains(line, "half-open") {
		t.Errorf("the stream printed the checklist itself:\n%s", line)
	}
	// Said once. The journal is read forward from where it stopped, so a second
	// pass over the same event must write nothing at all.
	if again := narrated(t, watcher, said); strings.Contains(again, "acceptance") {
		t.Errorf("the checklist line was said twice:\n%s", again)
	}
}
