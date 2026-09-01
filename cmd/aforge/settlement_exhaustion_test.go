package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A LEAF THAT RAN OUT HAS NO ACCOUNT.
//
// The measured run: a leaf hit its token grant mid-`sh` with a red build, took
// its four landing turns, printed `⏳ ran out of tokens — 33 turns` — and two
// seconds later the node was ✓ done, its summary its own last sentence, "All 722
// tests pass. Let me verify the dry-run tests specifically:". One 10k-token judge
// call, shown the brief and that sentence and nothing else, had said the leaf was
// finished, and `remainder.Done` overwrote the verdict. The two sibling tasks
// then briefed on truncated work. The turn the ✓ bought away — running the
// binary — is where the run's fatal defect would have surfaced.
//
// Here the judge is scripted to give exactly that answer, over a leaf that
// really does run out. Doneness is a claim; running out is measured; a measured
// fact is never overturned by an unmeasured claim.
func TestALeafThatRanOutIsNeverSettledOnAJudgesSayySo(t *testing.T) {
	script := newScriptedBrain(t)
	script.runawayLeaf = true
	// Two turns cross the 150,000-token leaf grant, and the landing reserve runs
	// four more — so what lands this leaf is its budget and nothing else.
	script.turnTokens = 80_000
	script.remainderDone = true
	script.gatePasses = true
	defer script.close()

	workspace := t.TempDir()
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note and include the migration steps", keep: true,
		workspace: workspace, timeout: 120 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	if script.count("remainder") == 0 {
		t.Fatalf("the remainder judge was never asked, so this run is not the one under test:\n%s",
			stderr.String())
	}
	var status exitStatus
	if err == nil {
		t.Fatalf("a run whose every leaf was cut off left with exit 0:\n%s", stderr.String())
	}
	if asExitStatus(err, &status) && status == 0 {
		t.Fatalf("a run whose every leaf was cut off left with exit 0:\n%s", stderr.String())
	}

	// THE STREAM SAYS WHAT HAPPENED. A ⏳ is answered by a ↻ — the work carried
	// on — and never by this node's own ✓ with nothing between them.
	assertRanOutThenCarriedOn(t, stderr.String())

	home := keptHome(stderr.String())
	if home == "" {
		t.Fatalf("the run kept no store to read back:\n%s", stderr.String())
	}
	defer os.RemoveAll(home)
	graph, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	// No node that ran out is completed without something taking its work on:
	// a splice under it, or its own release back to the queue.
	assertNothingCutOffWasCompletedAlone(t, graph)
}

// assertRanOutThenCarriedOn reads the headless stream in order and holds it to
// the surface law: once something has run out, the next thing the stream says
// about the work is that it carried on — a ↻ — and never a ✓.
//
// It is read as one stream rather than node by node because that is what the
// law is about: a spliced continuation's ↻ carries a different node's name from
// the ⏳ it answers, and it is still the answer. The signature this refuses is
// the measured one exactly — `⏳ … ran out of its tokens` at 18m56s and `✓` at
// 18m58s, with nothing between them.
func assertRanOutThenCarriedOn(t *testing.T, stream string) {
	t.Helper()
	ranOut, carriedOn := false, false
	for _, line := range strings.Split(stream, "\n") {
		switch {
		case strings.Contains(line, "⏳"):
			ranOut = true
		case strings.Contains(line, "↻"):
			carriedOn = true
		case strings.Contains(line, "✓"):
			if ranOut && !carriedOn {
				t.Fatalf("a leaf ran out and the next thing said about the work was a tick:\n%s",
					stream)
			}
		}
	}
	if !ranOut {
		t.Fatalf("no leaf ran out, so this run is not the one under test:\n%s", stream)
	}
	if !carriedOn {
		t.Fatalf("a leaf ran out and nothing said the work carried on:\n%s", stream)
	}
}

// assertNothingCutOffWasCompletedAlone walks the whole journal once. A node that
// journaled `leaf_exhausted` and then `node_completed` must have journaled a
// splice or a release between the two — the successor that holds its work.
func assertNothingCutOffWasCompletedAlone(t *testing.T, graph *store.Store) {
	t.Helper()
	events, err := graph.Events(0, 5000)
	if err != nil {
		t.Fatal(err)
	}
	cut := map[string]bool{}
	covered := map[string]bool{}
	exhausted := 0
	for _, event := range events {
		switch event.Kind {
		case store.EventLeafExhausted:
			cut[event.NodeID], covered[event.NodeID] = true, false
			exhausted++
		case store.EventSubtreeSpliced, store.EventNodeReleased:
			for id := range cut {
				if event.NodeID == id || strings.HasPrefix(event.NodeID, id) {
					covered[id] = true
				}
			}
		case store.EventNodeCompleted:
			if cut[event.NodeID] && !covered[event.NodeID] {
				t.Fatalf("%s ran out of room and was completed with nothing carrying its work on",
					event.NodeID)
			}
		}
	}
	if exhausted == 0 {
		t.Fatal("no leaf ran out, so this run is not the one under test")
	}
}

// AND WHAT RAN OUT IS REPORTED AS WHAT IT REACHED. `leaf_exhausted.reached` was
// the spend at the moment the landing reserve was GRANTED, and then the landing
// turns ran and were never counted: recomputed from one run's own usage rows,
// 172,791 tokens were reported as 152,090 and 199,131 as 178,086 — every line
// under-reporting by 12-14%.
func TestTheExhaustionRecordSaysWhatTheLeafActuallyReached(t *testing.T) {
	script := newScriptedBrain(t)
	script.runawayLeaf = true
	script.turnTokens = 80_000
	script.remainderDone = true
	script.gatePasses = true
	defer script.close()

	workspace := t.TempDir()
	var stdout, stderr strings.Builder
	_ = doErrand(doRequest{
		task: "write the release note and include the migration steps", keep: true,
		workspace: workspace, timeout: 120 * time.Second,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	home := keptHome(stderr.String())
	if home == "" {
		t.Fatalf("the run kept no store to read back:\n%s", stderr.String())
	}
	defer os.RemoveAll(home)
	graph, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	events, err := graph.Events(0, 5000)
	if err != nil {
		t.Fatal(err)
	}
	records := 0
	for _, event := range events {
		if event.Kind != store.EventLeafExhausted {
			continue
		}
		var record store.LeafExhausted
		if err := json.Unmarshal(event.Payload, &record); err != nil {
			t.Fatal(err)
		}
		if record.Meter != "cost" {
			continue
		}
		records++
		// Two turns cross the grant and the landing reserve runs four more, each
		// billing turnTokens — so a reading taken at the grant cannot be within
		// one turn of what the leaf actually spent.
		if record.Reached <= record.Allowance+script.turnTokens {
			t.Fatalf("the record says the leaf reached %d of %d, which is the reading taken "+
				"when the landing was granted rather than what it spent landing", record.Reached,
				record.Allowance)
		}
	}
	if records == 0 {
		t.Fatalf("no leaf was landed by its budget, so this run is not the one under test:\n%s",
			stderr.String())
	}
}
