package main

import "testing"

// The delivery law's carve-out is selected from two facts and never from a
// guess, and the first of them is the gate: without a shared workspace there is
// nowhere a file could be the deliverable, so the answer is the message and the
// prompts are the prompts they have always been.
//
// The second is whether the ask said a filename out loud. It is a regexp rather
// than a call because it decides which half of plan.DeliveryLaw every brief and
// every contract in the job is written against, and it would otherwise be bought
// once per plan and once per replan for a bit that spelling settles. What it must
// not do is read prose as a filename: an ask that says "e.g." or quotes a version
// number would otherwise silently switch every worker in the job onto the wrong
// half of the law.
func TestTheFileShapedBitIsGatedOnAWorkspaceAndAName(t *testing.T) {
	for name, testcase := range map[string]struct {
		terrain string
		goal    string
		want    bool
	}{
		"no workspace, no carve-out": {
			terrain: "", goal: "update REPORT.md with the new figures", want: false,
		},
		"a workspace and a named file": {
			terrain: "/tmp/work", goal: "update REPORT.md with the new figures", want: true,
		},
		"a workspace and a named path": {
			terrain: "/tmp/work", goal: "rewrite docs/JOURNEY.md", want: true,
		},
		"a workspace and a question": {
			terrain: "/tmp/work", goal: "which of these two designs is faster, and why", want: false,
		},
		"prose that is not a filename": {
			terrain: "/tmp/work", goal: "compare the two, e.g. on cost and on latency", want: false,
		},
		"a version number is not a filename": {
			terrain: "/tmp/work", goal: "summarise what changed in 1.5 and 2.11", want: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := fileShapedAsk(testcase.terrain, testcase.goal); got != testcase.want {
				t.Errorf("fileShapedAsk(%q, %q) = %v, want %v",
					testcase.terrain, testcase.goal, got, testcase.want)
			}
		})
	}
}
