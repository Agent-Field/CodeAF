package plan

import (
	"strings"
	"testing"
)

// The whole bundle argument in one shape check: declared-independent requests
// share stage one with no edges between them, and the only thing after them is
// the synthesis that reads them all. The layout is geometry, not judgment — a
// measured spine run restated the independence rule and then chained the
// requests anyway, which is why no model is consulted here.
func TestABundleSharesOneStageAndNothingChains(t *testing.T) {
	parts := []string{
		"Fix the failing test in the fixtures repository and say what was wrong.",
		"Compute March revenue from orders.csv and name the top product.",
		"  ", // a blank part is refused structurally, never planned around
		"Draft the sponsorship decline email per the brief.",
	}
	graph := Bundle("Deliver three independent results.", parts)

	var workers, sinks int
	for _, node := range graph.Nodes {
		switch node.Kind {
		case KindWork:
			workers++
			if node.Stage != 1 {
				t.Errorf("part %q sits in stage %d, want 1", node.Title, node.Stage)
			}
			if len(node.Needs) != 0 {
				t.Errorf("part %q was given needs %v — a bundle has no cross-part edges", node.Title, node.Needs)
			}
			if node.Brief == "" || node.Summary == "" {
				t.Errorf("part %q is missing its own words", node.Title)
			}
		case KindSynthesis:
			sinks++
			if node.Stage != 2 {
				t.Errorf("the synthesis sits in stage %d, want 2", node.Stage)
			}
			if len(node.Needs) != 3 {
				t.Errorf("the synthesis reads %d parts, want 3", len(node.Needs))
			}
			if !strings.Contains(node.Brief, "in the order the person asked") {
				t.Errorf("the merge brief lost its ordering law: %q", node.Brief)
			}
		}
	}
	if workers != 3 || sinks != 1 {
		t.Fatalf("bundle shape = %d workers, %d sinks; want 3 and 1", workers, sinks)
	}
	if sink := graph.deliverableSink(); sink == 0 {
		t.Fatal("the bundle has no deliverable owner for the contract and gate to hold")
	}
}
