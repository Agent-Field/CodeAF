package exec

import (
	"path/filepath"
	"strings"
	"testing"
)

// Two leaves of one splice are two workers, and everything they write has to be
// told apart.
//
// The resident surface has no per-node number to name them by. It has a store
// node, and a store node's creation sequence is its whole splice's — one
// transaction, one sequence, every sibling stamped with it. Passing that down as
// the leaf's identity gave four concurrently running workers one artifact
// bucket, one flight recorder and one set of .obs spill names. The bucket made
// each of them claim the others' files; the recorder made a reader asking what
// one worker did receive four workers' turns interleaved; and the spill names
// were the one that actually destroyed something, because a spilled observation
// is written under a name and then pointed at from a stub in the worker's own
// context — so a sibling writing the same name replaced the bytes a live
// transcript was still promising.
//
// This is the same defect SuggestPathFor was fixed for, in the same place, and
// it is fixed the same way: the identity is the node's own id, which is unique
// by construction.
func TestTwoSiblingsOfOneSpliceShareNoWrittenName(t *testing.T) {
	const splice = 1974 // one splice, one creation sequence, both siblings
	first := Task{NodeID: splice, NodeKey: "task-1961-n1"}
	second := Task{NodeID: splice, NodeKey: "task-1961-n2"}

	if first.leafKey() == second.leafKey() {
		t.Fatalf("both siblings answer to %q, so nothing below can tell them apart", first.leafKey())
	}

	space := workspace(t)
	for name, pair := range map[string][2]string{
		"the flight recorder":  {traceName(first.leafKey()), traceName(second.leafKey())},
		"the raw event stream": {streamName(first.leafKey()), streamName(second.leafKey())},
		"the spilled observation": {
			spilledName(t, space, first.leafKey()),
			spilledName(t, space, second.leafKey()),
		},
	} {
		if pair[0] == pair[1] {
			t.Errorf("%s: both siblings write %q, so one of them overwrites the other", name, pair[0])
		}
	}

	// The bucket, which is the half that misreports rather than destroys: each
	// sibling must be told about its own file and no one else's.
	space.Record(first.leafKey(), filepath.Join(space.Root(), "one.md"))
	space.Record(second.leafKey(), filepath.Join(space.Root(), "two.md"))
	if got := space.Artifacts(first.leafKey()); len(got) != 1 || got[0] != "one.md" {
		t.Errorf("the first sibling's artifacts are %v, want only its own file", got)
	}
	if got := space.Artifacts(second.leafKey()); len(got) != 1 || got[0] != "two.md" {
		t.Errorf("the second sibling's artifacts are %v, want only its own file", got)
	}
}

// spilledName is the name one leaf's decayed observation lands under. It goes
// through the toolbox rather than formatting the name here, because the whole
// point of the test is that the writer and the identity agree.
func spilledName(t *testing.T, space *Workspace, leaf string) string {
	t.Helper()
	path, ok := NewToolbox(space, leaf, nil).decaySpill("call_a1", "a body worth keeping")
	if !ok {
		t.Fatalf("the observation for %q was not spilled at all", leaf)
	}
	if !strings.Contains(path, obsDir) {
		t.Fatalf("the observation for %q landed outside %s: %q", leaf, obsDir, path)
	}
	return path
}

// A headless run has a per-node number already, and it keeps it. The numeric
// spelling is what every recorder, spill and bucket on that path has always
// been named by, and a leaf that set no key must not quietly move.
func TestALeafWithNoKeyKeepsTheNumericSpellingItAlwaysHad(t *testing.T) {
	task := Task{NodeID: 7}
	if got := task.leafKey(); got != "7" {
		t.Fatalf("a keyless leaf answers to %q, want the plan node's own number", got)
	}
	if got := traceName(task.leafKey()); got != filepath.Join(traceDir, "7.trace.log") {
		t.Fatalf("the recorder is at %q, want the spelling it has always had", got)
	}
}
