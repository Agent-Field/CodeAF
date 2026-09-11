package provider

// A CANCELLED RACE ANSWERS ITS CALLER AT ONCE, AND SETTLES UP BEHIND THEM.
//
// The caller of a cancelled race is almost always a person who has just typed a
// correction into a running turn: internal/session cut the generation on their
// steer and is blocked in this package until it comes back, and the next thing
// that should reach the wire is their own words. So the law is about WHO PAYS
// the close-out, not about whether it happens — the arms still get drained, the
// offer still comes down and the money is still noted, on a goroutine nobody is
// waiting on.
//
// [abandonGrace] is a whole second, which is the entire allowance
// [lane.SpokenWithin] gives the person: a caller that paid it had nothing left
// to spend on the request they were waiting for.

import (
	"context"
	"errors"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

func TestACancelledRaceAnswersAtOnceAndDrainsItsArmsBehindTheCaller(t *testing.T) {
	// TWO ARMS AND ONE REPORT is the shape that makes the grace real. The drain
	// waits until it has heard from every arm, so an arm that never speaks is
	// the whole second — which is exactly the arm [abandonGrace] was written
	// for, and exactly the one a caller must not be behind.
	race := &hedgeRace{
		budget:  lanes.DefaultBudget(),
		results: make(chan armResult, 2),
		arms:    []*hedgeArm{{index: 0}, {index: 1}},
		winner:  -1,
		speaker: -1,
	}
	race.results <- armResult{index: 0}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	began := time.Now()
	response, _, err := race.abandon(ctx, map[int]armResult{})
	took := time.Since(began)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("abandon answered %v, want the caller's own cancellation", err)
	}
	if response != nil {
		t.Fatalf("a cancelled race answered a response: %+v", response)
	}
	// The figure is a tenth of the grace rather than the grace itself, so a slow
	// box cannot make a caller that really is waiting out the drain look fast.
	if allowed := abandonGrace / 10; took > allowed {
		t.Fatalf("the caller waited %s for a cancelled race, want under %s: the close-out is on their path again", took, allowed)
	}

	// AND THE ACCOUNT IS STILL KEPT. The arm that did report is collected, which
	// is the observable half of [hedgeRace.accountForTheAbandoned] having run;
	// the row it writes is its own and it writes it as it unwinds (calllog.go).
	deadline := time.After(2 * abandonGrace)
	for len(race.results) > 0 {
		select {
		case <-deadline:
			t.Fatal("nothing drained the arms of the abandoned race: its requests are left with a start row and no end")
		case <-time.After(time.Millisecond):
		}
	}
}
