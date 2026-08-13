package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
)

// A node nobody sized is not a missing measurement. It is the shape one worker
// takes over a whole job — an undivided goal, a single-leaf remainder, a node
// spliced in after planning — and it is the commonest thing the product runs.
// Written as the empty string it landed in a bucket no reader looks up, so the
// shape could never gather the eight samples an expectation needs.
func TestAnUnsizedNodeIsRecordedAsTheWholeShape(t *testing.T) {
	if got := recordedSize(plan.Node{}); got != profile.BucketWhole {
		t.Errorf("an unsized node was recorded as %q, want the whole bucket", got)
	}
	for _, size := range []plan.Size{plan.SizeAtomic, plan.SizeBorderline, plan.SizeOversized} {
		if got := recordedSize(plan.Node{Size: size}); got != string(size) {
			t.Errorf("a %s node was recorded as %q", size, got)
		}
	}
}

// The touch-list and the fan-in are two different facts, and reading the first
// as the second is what priced reassembly at the price of an atomic. The plan's
// dependency list is the fallback; a surface that measured what actually landed
// wins over it.
func TestTheRecordedFanInIsDependenciesAndNeverTheTouchList(t *testing.T) {
	gathering := plan.Node{
		Needs:   []int{1, 2, 3},
		Sources: []string{"a page", "another page", "a dataset", "a fourth thing", "a fifth"},
	}
	if got := recordedFanIn(gathering); got != 3 {
		t.Errorf("fan-in = %d, want the 3 dependencies rather than the 5 sources", got)
	}
	measured := gathering
	measured.FanIn = profile.FanInOf(2)
	if got := recordedFanIn(measured); got != 2 {
		t.Errorf("fan-in = %d, want what the claiming surface actually measured", got)
	}
	atomic := plan.Node{Sources: []string{"a page", "another page", "a third"}}
	if got := recordedFanIn(atomic); got != 0 {
		t.Errorf("an atomic leaf that touches three things was priced as a %d-way join", got)
	}
}

// Attribution: the record has to name the worker that actually ran the leaf.
// The plan node carried the planner's intention and only an escalation ever
// updated it, so a leaf that ran on a coding pipeline for forty minutes was
// journaled into the generalist's file — the one the generalist's ruler is
// rewritten from.
func TestTheRecordFollowsTheWorkerThatActuallyRan(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{
		Name: "swe", Purpose: "an end-to-end software-engineering pipeline",
	})
	document := &plan.Graph{Goal: "ship it", NextID: 1}
	document.Add(plan.Node{Kind: plan.KindWork, Stage: 1, Title: "Change the code",
		Summary: "edit and test", Subharness: ""})
	node := document.Node(1)
	plans := &jobPlans{graphs: map[string]plannedJob{}}

	plans.markClaimed(document, node, "swe", 4)
	if node.Subharness != "swe" {
		t.Fatalf("the plan node was not settled onto the worker the store promised: %q", node.Subharness)
	}
	if got := recordedFanIn(*node); got != 4 {
		t.Fatalf("the measured fan-in did not reach the plan node: %d", got)
	}
	if profileSubharness(node.Subharness) != "swe" {
		t.Fatalf("the record would be filed under %q", profileSubharness(node.Subharness))
	}
}
