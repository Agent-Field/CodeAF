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
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// THE INK RUN OF 2026-08-29, AT THE MOMENT IT LOST ITS MEMORY.
//
// `bench/deepswe/results/ink-grid-box-layout-…-s6` spent $0.850 and its whole
// ninety-minute wall on one leaf that started five times, each time from turn
// one, each time by exploring a repository it had already been editing for
// twenty-two minutes. testdata/ink-s6-task-2-transcript.json is that node's
// record as the store held it at the first release: attempt one's seventy-two
// turns (ending on a recorded fault, "context deadline exceeded") followed by
// attempt two's first forty-five.
//
// The three things a resumption owes its successor are asserted against it: that
// the seed is ONE attempt, that it carries what was DECIDED across the whole of
// that attempt rather than only its last twelve turns, and that the run says out
// loud how much it resumed from.
func TestTheInkLeafResumesFromItsOwnRecord(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "ink-s6-task-2-transcript.json"))
	if err != nil {
		t.Fatalf("read the recorded transcript: %v", err)
	}
	var recorded []store.TranscriptEntry
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatalf("decode the recorded transcript: %v", err)
	}
	if len(recorded) != 342 {
		t.Fatalf("the fixture holds %d entries, want the 342 the store held", len(recorded))
	}

	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: "Update the display style property to accept \"grid\"", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "CSS Grid layout support"}); err != nil {
		t.Fatalf("splice: %v", err)
	}
	// Replayed in the flushes the recorder actually wrote, because the batch
	// boundary is what the store's total order is built on.
	for start := 0; start < len(recorded); start += store.MaxTranscriptBatch {
		end := start + store.MaxTranscriptBatch
		if end > len(recorded) {
			end = len(recorded)
		}
		if err := graph.RecordTranscript("task-2", "deepseek/deepseek-v4-flash", recorded[start:end]); err != nil {
			t.Fatalf("record flush at %d: %v", start, err)
		}
	}

	// The workspace as the world holds it: the files that attempt changed, read
	// back from the tree rather than from what any tool claimed (FAILSAFE.md
	// rule 2). `aforge do` runs in a shared workspace — the user's checkout —
	// which is why this reading is asked for by the leaf's own key.
	jobDir := t.TempDir()
	space, err := exec.NewWorkspace(jobDir)
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	space.WatchTree("task-2")
	for _, name := range []string{"src/styles.ts", "src/grid-layout.ts", "src/dom.ts"} {
		path := filepath.Join(jobDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("workspace dir: %v", err)
		}
		writeFile(t, path, "// the attempt's own edits, still on disk\n")
	}
	space.RecordChanges("task-2")

	node, _, err := graph.Node("task-2")
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	bank, resumed := leafBank(graph, node, space, jobDir, false, nil, nil)
	if bank.Empty() {
		t.Fatal("a node holding 342 recorded entries banked nothing — this is the defect")
	}

	// ONE ATTEMPT. The record holds two; the one being resumed reached turn 45.
	if resumed != 45 {
		t.Fatalf("resumed from %d turns, want the 45 of the attempt that was interrupted", resumed)
	}
	seed := bank.Input().Result

	// WHAT THE WORKSPACE ALREADY HOLDS, from the world's own reading.
	for _, name := range []string{"src/styles.ts", "src/grid-layout.ts", "src/dom.ts"} {
		if !strings.Contains(seed, filepath.Join(jobDir, name)) {
			t.Fatalf("the seed does not say the workspace already holds %s:\n%s", name, first(seed, 2000))
		}
	}

	// WHAT WAS DECIDED, across the whole attempt and not only its tail. The
	// attempt created src/grid-layout.ts on turn 25 — twenty turns before it was
	// interrupted, and so entirely outside the twelve-turn verbatim window that
	// was the only thing the seed used to carry.
	outline, tail, split := strings.Cut(seed, resident.BankedRunTailLead)
	if !split {
		t.Fatalf("the seed has no verbatim tail:\n%s", first(seed, 2000))
	}
	if !strings.Contains(outline, "grid-layout.ts") {
		t.Fatalf("the outline lost the file the attempt created on turn 25:\n%s", first(outline, 3000))
	}
	if strings.Contains(tail, "grid-layout.ts") {
		t.Skip("turn 25 has drifted inside the verbatim window; the outline assertion above is what this test is for")
	}
	for _, turn := range []int{1, 20, 25, 45} {
		if !strings.Contains(outline, "turn "+strconv.Itoa(turn)+" ·") {
			t.Fatalf("turn %d is missing from the outline of the whole run:\n%s", turn, first(outline, 3000))
		}
	}
	// And nothing from the attempt before it, whose turns ran to 72.
	if strings.Contains(outline, "turn 60 ·") || strings.Contains(outline, "turn 72 ·") {
		t.Fatalf("the seed mixed the previous attempt into this one:\n%s", first(outline, 3000))
	}

	// AND THE RUN SAYS SO. This is the line the headless stream prints, and it
	// is a count rather than a claim because "resumed" was exactly the promise
	// that was made and silently not kept.
	line := resumedWords(store.LeafResumed{Turns: resumed, Files: bank.Artifacts})
	if !strings.Contains(line, "resumed from 45 recorded turns") {
		t.Fatalf("the stream line reads %q", line)
	}
	if !strings.Contains(line, "grid-layout.ts") {
		t.Fatalf("the stream line does not say what the workspace already holds: %q", line)
	}
}

// first is the head of a long block, for a failure message that has to be read.
func first(text string, bytes int) string {
	if len(text) <= bytes {
		return text
	}
	return text[:bytes] + "\n…"
}

// AN ATTEMPT THAT RAN OUT OF ROOM IS NOT A RESTART, AND IT SAYS SO.
//
// The ink leaf's first attempt died on its own fifteen-minute deadline — its
// record ends on a fault reading "context deadline exceeded" — and the store's
// only account of it was a gap between two transcript flushes. Four minutes
// later a second attempt began inside the same claim, and from outside that is
// indistinguishable from the claim reaper taking the node away. They want
// opposite responses, so they are told apart in the journal.
func TestAnAttemptThatRanOutOfRoomIsJournaledAsExhaustion(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: "CSS Grid layout support", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "grid"}); err != nil {
		t.Fatalf("splice: %v", err)
	}

	// The executor noticing its own deadline between turns.
	journalLeafExhaustion(graph, "task-2", 1, 15*time.Minute, 17*time.Minute,
		&exec.Outcome{Stop: exec.StopDeadline, Turns: 72}, nil)
	// The watchdog above it, on a leaf that never came back at all.
	journalLeafExhaustion(graph, "task-2", 2, 15*time.Minute, 17*time.Minute,
		nil, &exec.Abandoned{After: 17 * time.Minute})
	// And a leaf that simply finished, which is not this fact and writes nothing.
	journalLeafExhaustion(graph, "task-2", 3, 15*time.Minute, 17*time.Minute,
		&exec.Outcome{Stop: exec.StopDone, Turns: 12}, nil)

	events, err := graph.Events(0, 400)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var exhausted []store.LeafExhausted
	for _, event := range events {
		if event.Kind != store.EventLeafExhausted {
			continue
		}
		var record store.LeafExhausted
		if err := json.Unmarshal(event.Payload, &record); err != nil {
			t.Fatalf("decode exhaustion: %v", err)
		}
		exhausted = append(exhausted, record)
	}
	if len(exhausted) != 2 {
		t.Fatalf("journaled %d exhaustions, want the two that ran out of room", len(exhausted))
	}
	if exhausted[0].Bound != string(exec.StopDeadline) || exhausted[0].Allowed != "15m0s" || exhausted[0].Turns != 72 {
		t.Fatalf("the first attempt's row is %+v — it must say what ran out, how much it had, and how far it got", exhausted[0])
	}
	if !strings.Contains(exhausted[0].Reason, "still working") {
		t.Fatalf("reason = %q — exhaustion is not failure and must not read as it", exhausted[0].Reason)
	}
	if exhausted[1].Attempt != 2 || !strings.Contains(exhausted[1].Reason, "did not come back within 17m0s") {
		t.Fatalf("the abandoned attempt's row is %+v", exhausted[1])
	}
}
