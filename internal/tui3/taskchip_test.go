package tui3

// What is left of the strip's own laws, now that the strip is gone (ISSUE-126):
// the family key, the parent link, the pause, the index three surfaces climb,
// the order the live set comes in, and the one question the paint clock still
// asks about it.

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE KEY IS THE NODE'S OWN ID, spelled the way the parent seam speaks: the
// engine hands this surface a string and a node has to be able to answer to it.
func TestStripKeyIsTheNodesOwnID(t *testing.T) {
	if got := stripKey(&taskNode{id: 42}); got != "42" {
		t.Fatalf("stripKey = %q, want %q", got, "42")
	}
}

// AND "" IS AN HONEST "NOBODY SPAWNED THIS", which is every node in every
// session until one runs something adaptive.
func TestParentIDIsEmptyUntilSomethingSpawnsIt(t *testing.T) {
	node := &taskNode{id: 7}
	if got := node.ParentID(); got != "" {
		t.Fatalf("a node nobody spawned reports the parent %q", got)
	}
	node.parent = "3"
	if got := node.ParentID(); got != "3" {
		t.Fatalf("ParentID = %q, want %q", got, "3")
	}
}

func TestPausedReadsTheNode(t *testing.T) {
	node := &taskNode{}
	if node.Paused() {
		t.Fatal("a fresh node must not read paused")
	}
	node.paused = true
	if !node.Paused() {
		t.Fatal("a paused node must read paused")
	}
}

// ONE INDEX FOR THREE CLIMBS. The roster's forest, the crumb's walk up the
// parents and the one level esc takes all read the same map, and a node this
// session never admitted is not in it — which is what stops the crumb walking
// off the end of a trail.
func TestNodesByKeyIndexesEveryAdmittedNodeAndNothingElse(t *testing.T) {
	a := &app{tasks: map[uint64]*taskNode{}, taskOrder: []uint64{1, 2}}
	a.tasks[1] = &taskNode{id: 1}
	a.tasks[2] = &taskNode{id: 2, parent: "1"}
	byKey := a.nodesByKey()
	if len(byKey) != 2 {
		t.Fatalf("the index holds %d nodes, want 2", len(byKey))
	}
	if byKey["1"] == nil || byKey["1"].id != 1 {
		t.Fatalf("the index lost node 1: %v", byKey)
	}
	if byKey["9"] != nil {
		t.Fatal("the index answered for a node nobody admitted")
	}
	// AND AN ID IN THE ORDER WITH NO NODE BEHIND IT IS SKIPPED, not indexed as
	// a nil that the walk above would then dereference.
	a.taskOrder = append(a.taskOrder, 3)
	if got := len(a.nodesByKey()); got != 2 {
		t.Fatalf("the index holds %d nodes after a missing id, want 2", got)
	}
}

// THE LIVE SET LEADS WITH WHAT IS RUNNING, which is not the roster's order:
// the column leads with what is asking for a decision because a person reads it
// top to bottom looking for work to do, and the live set leads with a presence.
func TestStripNodesLeadsWithWhatIsRunning(t *testing.T) {
	a := &app{tasks: map[uint64]*taskNode{}, taskOrder: []uint64{1, 2}}
	a.tasks[1] = &taskNode{id: 1, state: session.TaskUnverified}
	a.tasks[2] = &taskNode{id: 2, state: session.TaskRunning}
	nodes := a.stripNodes()
	if len(nodes) != 2 {
		t.Fatalf("the live set holds %d nodes, want 2", len(nodes))
	}
	if nodes[0].id != 2 {
		t.Fatalf("the live set leads with node %d, want the running one", nodes[0].id)
	}
}

// AND WHAT IS DONE IS NOT ON IT. The live set is the live set; the roster is
// where a session's history lives.
func TestStripNodesLeavesTheLandedBehind(t *testing.T) {
	a := &app{tasks: map[uint64]*taskNode{}, taskOrder: []uint64{1}}
	a.tasks[1] = &taskNode{id: 1, state: session.TaskDone}
	if nodes := a.stripNodes(); len(nodes) != 0 {
		t.Fatalf("the live set carries %d landed nodes, want none", len(nodes))
	}
}

// THE PAINT CLOCK IS THE PHONE'S NOW. The strip's old meaning — "a task exists
// anywhere, so keep repainting" — went with the strip: a wide frame with the
// roster closed draws the live set NOWHERE, because the top bar's glyph is the
// open room's and not the session's.
func TestLiveShowingIsThePhoneTierAndNotAWideFrame(t *testing.T) {
	a := &app{tasks: map[uint64]*taskNode{}, taskOrder: []uint64{1}, width: 44, height: 30}
	a.tasks[1] = &taskNode{id: 1, state: session.TaskRunning}
	if !a.liveShowing() {
		t.Fatal("a running task at phone width must raise the paint clock: the deck draws it")
	}
	a.width = 120
	if a.liveShowing() {
		t.Fatal("a running task on a wide frame raised the paint clock: nothing up there draws it")
	}
	a.width = 44
	a.tasks[1].state = session.TaskDone
	if a.liveShowing() {
		t.Fatal("a landed task raised the paint clock")
	}
}

// A PAUSED NODE IS HELD, NOT RUNNING, and the roster says so with the transport
// bar rather than with the queued circle.
func TestThePausedGlyphIsDrawnAndIsNotTheQueuedMark(t *testing.T) {
	a := &app{pal: newPalette(0, false)}
	got := plain(a.stripPausedGlyph())
	if got != glyphPaused {
		t.Fatalf("the paused glyph is %q, want %q", got, glyphPaused)
	}
}
