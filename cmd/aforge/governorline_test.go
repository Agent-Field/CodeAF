package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A RUN THAT STOPPED BECAUSE IT HAD STOPPED GETTING ANYWHERE SAYS SO, LAST,
// WHERE THE ANSWER IS.
//
// ink s10 and happy-dom s10 both hold a standstill refusal in the growth
// journal — on a node inside a job nobody opens — and both ended with a last
// line about the clock. The clock is what the run walked into afterwards; the
// news is that a governor had already found the work had stopped moving
// (FAILSAFE clause 3).
func TestTheClosingLineSaysTheGovernorFoundNoProgress(t *testing.T) {
	graph, watcher, said := narrationFixture(t)
	for _, row := range []store.JobGrowth{
		{Reason: "overrun", Lineage: "task-1", Round: 1, Allowed: true,
			Measured: true, Produced: 1, Moved: []string{"src/grid.ts"}},
		{Reason: "overrun", Lineage: "task-1", Round: 2, Allowed: true,
			Measured: true, Produced: 0, Scratch: 3,
			Wrote: []string{"debug-grid.ts", "debug-yoga.ts", "debug-test2.tsx"}},
		{Reason: "overrun", Lineage: "task-1", Round: 3, Allowed: false,
			Cause: "standstill", Refused: "carrying on has stopped changing anything",
			Measured: true, Produced: 0, Scratch: 2,
			Wrote: []string{"debug-grid10.ts", "debug-grid11.ts"}},
	} {
		if err := graph.RecordJobGrowth("task-1", row); err != nil {
			t.Fatal(err)
		}
	}
	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	said.Reset()
	watcher.sayStanding(node)
	standing := said.String()
	if !strings.Contains(standing, "partial — no relevant progress in 2 rounds") {
		t.Fatalf("the run does not say what stopped it:\n%s", standing)
	}
	if !strings.Contains(standing, "last change: src/grid.ts") {
		t.Fatalf("a person told nothing has moved is owed the last thing that did:\n%s", standing)
	}
}

// And a job no governor ever refused keeps the gate's own reservation, which is
// the line every run before this one printed.
func TestAJobNoGovernorRefusedKeepsTheGatesOwnWords(t *testing.T) {
	graph, watcher, said := narrationFixture(t)
	if err := graph.RecordDeliveryGate("task-1", store.DeliveryGate{
		Gap: "the deliverable does not contain the implementation", Unclosed: true,
	}); err != nil {
		t.Fatal(err)
	}
	node, ok, err := graph.Node("task-1")
	if err != nil || !ok {
		t.Fatal(err)
	}
	said.Reset()
	watcher.sayStanding(node)
	if standing := said.String(); !strings.Contains(standing, "partial — gate:") {
		t.Fatalf("the gate's own finding was displaced:\n%s", standing)
	}
}
