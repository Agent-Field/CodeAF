package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/resident"
	"github.com/Agent-Field/codeaf/internal/store"
)

// TestShouldGateAfterGrownRoot pins the delivery-gate law once pacing lets a
// grown job land its root. The gate reads the root's OWN landing, and at that
// landing the growing flag is false — an overrun sets it only for the leaf
// landing that GROWS, never for the top-level job node that later gathers the
// pieces. So a top-level job node (Parent == store.RootID, non-reflex, non-nil
// outcome) is gated even after the job grew earlier, and only the growing leaf
// landing and the non-deliverable shapes are exempt.
//
// At this revision the growing flag arrives as the `continuing` argument:
// shouldGate(node, outcome, continuing) is !continuing && group != reflex &&
// parent == RootID && outcome != nil.
func TestShouldGateAfterGrownRoot(t *testing.T) {
	settled := &exec.Outcome{Stop: exec.StopDone}
	root := store.Node{ID: "job-1", Parent: store.RootID}
	reflex := store.Node{ID: "reflex-1", Parent: store.RootID, Group: resident.ReflexGroup}
	child := store.Node{ID: "job-1-n1", Parent: "job-1"}

	cases := []struct {
		name       string
		node       store.Node
		outcome    *exec.Outcome
		continuing bool
		want       bool
	}{
		{name: "root settled after a grown job", node: root, outcome: settled, continuing: false, want: true},
		{name: "root landing while growing", node: root, outcome: settled, continuing: true, want: false},
		{name: "reflex group", node: reflex, outcome: settled, continuing: false, want: false},
		{name: "non-root child", node: child, outcome: settled, continuing: false, want: false},
		{name: "nil outcome", node: root, outcome: nil, continuing: false, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldGate(tc.node, tc.outcome, tc.continuing); got != tc.want {
				t.Fatalf("shouldGate(id=%q, outcome=%v, continuing=%v) = %v, want %v",
					tc.node.ID, tc.outcome != nil, tc.continuing, got, tc.want)
			}
		})
	}
}
