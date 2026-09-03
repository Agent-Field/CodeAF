package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The runner's half of the transcript covenant. The worker records; this file
// is about the promise that what it recorded is DURABLE by the time the node
// settles, on every one of the three ways a leaf can end here — landing,
// panicking into the guard, and being given up on by the watchdog. Only the
// first is an ending the worker itself can write about, which is why the flush
// lives in runLeafWithWatchdog and not in the worker.

func watchdogFixture(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "leaf", Brief: "do the work", Title: "Leaf"},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "do the work"}); err != nil {
		t.Fatal(err)
	}
	return graph, armTranscript(context.Background(), graph, "leaf", "worker/model")
}

// scriptedLeaf is a worker that records a turn and then ends the way the test
// asks it to.
type scriptedLeaf struct {
	ending func(ctx context.Context) (*exec.Outcome, error)
}

func (scriptedLeaf) Subharness() string { return "test" }

func (l scriptedLeaf) Run(ctx context.Context, task exec.Task) (*exec.Outcome, error) {
	exec.TranscriptFrom(ctx).Record(store.TranscriptEntry{
		Turn: 1, Kind: store.TranscriptAssistant, Text: "I got this far."})
	return l.ending(ctx)
}

func recordedKinds(t *testing.T, graph *store.Store) []store.TranscriptEntry {
	t.Helper()
	entries, err := graph.TranscriptFor("leaf", 0)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestALeafThatLandsHasItsTranscriptOnDisk(t *testing.T) {
	graph, ctx := watchdogFixture(t)
	worker := scriptedLeaf{ending: func(context.Context) (*exec.Outcome, error) {
		return &exec.Outcome{Stop: exec.StopDone, Text: "done"}, nil
	}}
	if _, err := runLeafWithWatchdog(ctx, worker, exec.Task{NodeID: 1}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if entries := recordedKinds(t, graph); len(entries) != 1 || entries[0].Text != "I got this far." {
		t.Fatalf("a leaf that landed left %+v behind", entries)
	}
}

// The panic path. A worker that dies cannot flush its own record, and the
// guard below it turns the panic into a returned error — so if the flush were
// the worker's job, the one ending nobody can reconstruct from the outside
// would also be the one ending with no record.
func TestALeafThatPanicsStillLeavesTheTurnsBeforeTheFault(t *testing.T) {
	graph, ctx := watchdogFixture(t)
	worker := scriptedLeaf{ending: func(context.Context) (*exec.Outcome, error) {
		panic("the tool loop fell over")
	}}
	_, err := runLeafWithWatchdog(ctx, worker, exec.Task{NodeID: 1}, time.Minute)
	if err == nil {
		t.Fatal("a panicking leaf came back without an error")
	}
	if entries := recordedKinds(t, graph); len(entries) != 1 || entries[0].Text != "I got this far." {
		t.Fatalf("a leaf that panicked left %+v behind; the turns before the fault are the whole point", entries)
	}
}

// The watchdog path. The leaf is still running when the clock runs out, so the
// runner returns without it — and what it had already done has to be readable
// straight away rather than whenever the abandoned goroutine happens to end.
func TestAnAbandonedLeafHasItsTranscriptFlushedAnyway(t *testing.T) {
	graph, ctx := watchdogFixture(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	worker := scriptedLeaf{ending: func(context.Context) (*exec.Outcome, error) {
		<-release
		return &exec.Outcome{Stop: exec.StopDone}, nil
	}}
	_, err := runLeafWithWatchdog(ctx, worker, exec.Task{NodeID: 1}, 50*time.Millisecond)
	var abandoned *exec.Abandoned
	if !errors.As(err, &abandoned) {
		t.Fatalf("the watchdog did not fire: %v", err)
	}
	if entries := recordedKinds(t, graph); len(entries) != 1 {
		t.Fatalf("an abandoned leaf left %d entries behind, want the one it had already recorded", len(entries))
	}
}

// The reader. A record nobody can get at is not a record, so the command that
// prints one is part of the fix rather than a convenience on top of it.
func TestWhyPrintsWhatTheLeafDid(t *testing.T) {
	graph, _ := watchdogFixture(t)
	if err := graph.RecordTranscript("leaf", "worker/model", []store.TranscriptEntry{
		{Turn: 1, Kind: store.TranscriptAssistant, Text: "I'll read the notes."},
		{Turn: 1, Kind: store.TranscriptToolCall, Tool: "read", CallID: "c1", Text: `{"filePath":"notes.md"}`},
		{Turn: 1, Kind: store.TranscriptToolResult, Tool: "read", CallID: "c1", Text: "the notes", Millis: 1200},
		{Turn: 2, Kind: store.TranscriptFault, Text: "the provider gave up", Failed: true},
	}); err != nil {
		t.Fatal(err)
	}
	var printed strings.Builder
	if err := writeNodeTranscript(&printed, graph, "leaf"); err != nil {
		t.Fatal(err)
	}
	page := printed.String()
	for _, want := range []string{
		"turn 1 · said", "I'll read the notes.",
		"turn 1 · read ←", "notes.md",
		"turn 1 · read → 1.2s", "the notes",
		"turn 2 · stopped", "the provider gave up",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the printed record does not say %q:\n%s", want, page)
		}
	}
}

// A node nobody has run, or one run by a worker that keeps no record, has to
// say which of those it is rather than printing nothing and letting a reader
// conclude the leaf did nothing.
//
// AND IT IS NOT A SUCCESS. The sentence is for the person; the non-zero rung
// beside it is for the script, which otherwise reads exit 0 and concludes the
// id exists and is empty (row 27, [TestAskingWhyAboutAnIdThatIsNotThereIsNotASuccess]).
func TestWhySaysSoWhenThereIsNoTranscript(t *testing.T) {
	graph, _ := watchdogFixture(t)
	var printed strings.Builder
	if code := exitCodeOf(writeNodeTranscript(&printed, graph, "leaf")); code == 0 {
		t.Fatalf("a node with no record left with 0: %q", printed.String())
	}
	if !strings.Contains(printed.String(), "no transcript") {
		t.Fatalf("an empty record printed %q", printed.String())
	}
}
