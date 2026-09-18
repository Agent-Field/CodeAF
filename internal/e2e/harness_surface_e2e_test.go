//go:build e2e

package e2e

import "testing"

// harness_surface_e2e_test.go holds the words the tmux suite will read off a run
// engine's plan row once a run scenario drives one end to end (docs/design/worker-harness
// /SURFACE.md §2A, §4 Cell 2 — the rail's live step line).
//
// THE THREE NEEDLES BELOW ARE THE PLAN ROW'S OWN, and the sources they stand on
// are landed: the engine publishes the in-flight step on
// session.PlanTaskRow.Live (the run records it on session.EventToolBegin and
// clears it at the step's end line) and the surface draws it (task.go's
// planUnderRows, taskplan.go's planFigures). What this file keeps is the NAMES,
// which is what the untagged word gate
// (tuiwords_test.go's TestEveryWordInTheTableIsWaitedForBySomething) needs to know
// they are alive; the scenario that waits for them on a real screen belongs in the
// run subtest beside the plan row's own words.
func TestThePlanRowsLiveWordsAreNamed(t *testing.T) {
	for _, name := range []string{"planQueuedWord", "planLiveLead", "planStepsSpend"} {
		if say(t, name) == "" {
			t.Fatalf("the needle %q is in the table but spells nothing", name)
		}
	}
}
