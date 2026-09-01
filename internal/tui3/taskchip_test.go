package tui3

// The roster's own laws — the ones the strip used to carry — live here now.
// The strip is gone (ISSUE-126); what survived of it is the family key, the
// parent link, the pause, and the ordering the rail still draws by.

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestStripKeyFallsBackToID(t *testing.T) {
	node := &taskNode{id: 42}
	if got := stripKey(node); got != "42" {
		t.Fatalf("stripKey without a run = %q, want %q", got, "42")
	}
	node.run = "run-abc"
	if got := stripKey(node); got != "run-abc" {
		t.Fatalf("stripKey with a run = %q, want %q", got, "run-abc")
	}
}

func TestParentIDFallsBackToRun(t *testing.T) {
	node := &taskNode{id: 7, run: "run-root"}
	if got := node.ParentID(); got != "run-root" {
		t.Fatalf("ParentID without a parent = %q, want the run %q", got, "run-root")
	}
	node.parent = "run-parent"
	if got := node.ParentID(); got != "run-parent" {
		t.Fatalf("ParentID with a parent = %q, want %q", got, "run-parent")
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

func TestStripNodesOrdersByArrival(t *testing.T) {
	a := &app{tasks: map[uint64]*taskNode{}, taskOrder: []uint64{3, 1, 2}}
	a.tasks[1] = &taskNode{id: 1}
	a.tasks[2] = &taskNode{id: 2}
	a.tasks[3] = &taskNode{id: 3}
	nodes := a.stripNodes()
	if len(nodes) != 3 || nodes[0].id != 3 || nodes[1].id != 1 || nodes[2].id != 2 {
		t.Fatalf("stripNodes order = %v, want taskOrder", idsOf(nodes))
	}
}

func TestStripNodesSkipsTheGone(t *testing.T) {
	a := &app{tasks: map[uint64]*taskNode{}, taskOrder: []uint64{1, 2}}
	a.tasks[2] = &taskNode{id: 2}
	nodes := a.stripNodes()
	if len(nodes) != 1 || nodes[0].id != 2 {
		t.Fatalf("stripNodes with a missing id = %v, want only the live node", idsOf(nodes))
	}
}

func idsOf(nodes []*taskNode) []uint64 {
	out := make([]uint64, len(nodes))
	for i, n := range nodes {
		out[i] = n.id
	}
	return out
}

// liveShowing is the phone deck or a running harness — the strip's old
// meaning ("any task exists") is gone with the strip.
func TestLiveShowingMeansDeckOrHarness(t *testing.T) {
	a := &app{tasks: map[uint64]*taskNode{}, taskOrder: []uint64{1}}
	a.tasks[1] = &taskNode{id: 1, state: session.TaskRunning}
	if a.liveShowing() {
		t.Fatal("a running task alone must not raise liveShowing: the strip is gone")
	}
	a.deck = deckState{open: true}
	if !a.liveShowing() {
		t.Fatal("an open phone deck must raise liveShowing")
	}
	a.deck = deckState{}
	a.harnesses = map[string]*harnessCard{"h": {state: harnessRunning}}
	if !a.liveShowing() {
		t.Fatal("a running harness must raise liveShowing")
	}
}

// The paused glyph is the rail's now: a paused node reads as held, not as
// running, wherever the roster draws it.
func TestGlyphPausedIsTheHeldMark(t *testing.T) {
	a := &app{}
	node := &taskNode{id: 1, state: session.TaskRunning, paused: true}
	if got := a.glyphPaused(node); got == "" {
		t.Fatal("a paused node must carry the paused glyph")
	}
	node.paused = false
	if got := a.glyphPaused(node); got != "" {
		t.Fatalf("an unpaused node carried %q", got)
	}
}

// A paused node still counts as live for the fold's default: a family with
// nothing but paused work must not fold itself away.
func TestPausedKeepsTheFamilyOpen(t *testing.T) {
	a := &app{tasks: map[uint64]*taskNode{}, taskOrder: []uint64{1, 2}}
	a.tasks[1] = &taskNode{id: 1, state: session.TaskDone, began: time.Now()}
	a.tasks[2] = &taskNode{id: 2, state: session.TaskRunning, paused: true, parent: stripKey(a.tasks[1])}
	if a.railShut(a.tasks[1]) {
		t.Fatal("a family with a paused child must not default to folded")
	}
}
