package main

import (
	"testing"
	"time"
)

// A skill folder that is a link into a place the operating system gates behind
// a permission prompt makes open(2) block with no way to cancel it. The launch
// must still draw its first frame, so the import pass is given a budget and the
// conversation opens without that shelf when the budget runs out.
func TestSkillImportBudgetReleasesTheLaunchFromAStuckPass(t *testing.T) {
	stuck := make(chan struct{})
	defer close(stuck)

	started := time.Now()
	finished := runWithinBudget(50*time.Millisecond, func() { <-stuck })

	if finished {
		t.Fatal("a pass that never returns was reported as finished")
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Fatalf("the launch waited %v on a stuck pass", took)
	}
}

func TestSkillImportBudgetWaitsForAPassThatFinishes(t *testing.T) {
	ran := false
	if !runWithinBudget(time.Second, func() { ran = true }) || !ran {
		t.Fatal("a quick pass was not waited for")
	}
}
