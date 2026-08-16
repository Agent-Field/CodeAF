package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// Dispatch is the seam between "the work has been decided" and "the work has
// begun", and it is the one place in the product where nothing is happening on
// purpose. A leaf that has been planned, spliced and left pending is a job the
// person is waiting on while the machine is idle.
//
// This is the headless configuration deliberately, because the headless brain is
// where the pathology was caught at its worst: a compiled leaf sitting pending
// with an empty started_at for fifteen minutes, until a 900-second ceiling
// killed the run — while the reconciler beside it ticked happily once a second
// and every other surface reported a healthy resident. Claiming has no
// chat-only nudge behind it and must not acquire one; the guarantee is that the
// dispatch loop, on its own, takes the work the instant it exists.
func TestHeadlessDispatchClaimsACompiledLeafImmediately(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", keep: true,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	home := keptHome(stderr.String())
	if home == "" {
		t.Fatalf("the run did not say where its store is:\n%s", stderr.String())
	}
	defer os.RemoveAll(home)

	graph, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatalf("open the kept store: %v", err)
	}
	defer graph.Close()

	events, err := graph.Events(0, 10_000)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var spliced, claimed time.Time
	var leaf string
	for _, event := range events {
		switch event.Kind {
		case store.EventSubtreeSpliced:
			if spliced.IsZero() {
				spliced, leaf = event.Time, event.NodeID
			}
		case store.EventNodeClaimed:
			if claimed.IsZero() {
				claimed = event.Time
			}
		}
	}
	if spliced.IsZero() {
		t.Fatal("nothing was ever spliced — the ask never became work")
	}
	if claimed.IsZero() {
		t.Fatalf("leaf %q was spliced and never claimed: the dispatch loop stopped and said nothing", leaf)
	}
	// The bar is the poll interval with room to spare. Anything beyond it is
	// not scheduling, it is a stall wearing scheduling's clothes.
	if gap := claimed.Sub(spliced); gap > 2*time.Second {
		t.Fatalf("the leaf waited %s between being planned and being claimed, want under 2s", gap)
	}
}
