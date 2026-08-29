package verify

// Taking the first reading, and saying why when there isn't one.
//
// NOBODY LOOKED IS A FACT, AND A FACT ABOUT THE RUN REACHES THE RECORD. The
// first version of this had four ways to return no reading — no entrypoint, a
// wall that could not afford one, a shell the preamble cannot be trusted in, a
// command killed at its ceiling — and all four returned the same zero value
// silently. textual's s6 run hit the fourth: the bare leaf spent five minutes
// and twenty-seven seconds of its wall on a reading that was killed, then made
// its first model call, and the finished store held no row saying any of it had
// happened. From outside it was indistinguishable from a project that declares
// no verification at all, which is the opposite diagnosis.
//
// So there is one entry point, it always returns a Reading, and a Reading that
// was not taken carries the sentence saying why and the command it would have
// run. FAILSAFE.md clause 4.

import (
	"context"
	"fmt"
	"time"
)

// Photograph takes the reading a job is measured against: the tree as it stands
// before anything has changed it.
//
// It walks the strategy ladder (see [ReadingStrategies]) inside ONE budget. A
// rung that names nothing has told us nothing about the suite — textual's own
// `make test` exits in eight seconds on a pytest plugin the image does not have
// — so the next rung is tried with whatever budget is left. The LAST rung's
// answer is taken whatever it named, because a runner that genuinely reports no
// identities is a real reading with an empty roster, and treating that as no
// reading is the exact short-circuit that let fifty-two stated behaviours go
// unasked.
//
// focus is what this job is about, and it is what decides HOW MUCH of the
// project each rung reads — see scope.go. An empty focus is a job that named
// nothing, and every rung of its ladder is a reading of the whole project,
// which is what every reading here was before scopes existed.
//
// The Reading it returns is always meaningful: Taken says a reading exists, and
// Unread says in one sentence why one does not.
func Photograph(ctx context.Context, root string, wall time.Duration, focus Focus) Reading {
	plan := Discover(root)
	ladder, ok := ReadingStrategies(root, plan, focus)
	if !ok {
		return Reading{Plan: plan, Unread: "this project declares no way of checking itself, " +
			"so there is no reading to take"}
	}
	budget, affordable := ReadingBudget(wall)
	if !affordable {
		return Reading{Plan: plan, Strategy: ladder[0], Unread: fmt.Sprintf(
			"a wall of %s cannot afford a reading worth taking (one reading is an eighth "+
				"of it, and the floor is %s), so `%s` was not run",
			wall.Round(time.Second), ShortestUsefulReading, ladder[0].Command)}
	}
	return photograph(ctx, root, plan, ladder, budget)
}

// photograph is the ladder walk, with the budget already decided. It is
// separate so a test can walk the ladder on a budget measured in milliseconds:
// the exported call's floor is a minute, and a test that had to spend one to
// watch a rung fall is a test nobody runs.
func photograph(
	ctx context.Context, root string, plan Plan, ladder []Strategy, budget time.Duration,
) Reading {
	reading := Reading{Plan: plan, Budget: budget, Strategy: ladder[0]}
	left := budget
	for index, rung := range ladder {
		reading.Strategy = rung
		// A RUNG IS ENTITLED TO ITS SHARE OF THE BUDGET OR IT IS NOT STARTED.
		// The ladder is walked inside ONE budget, so a rung reached with a scrap
		// of it left is a command that will be killed before it says anything —
		// which costs the rest of the budget and returns the same silence as
		// not running it. An equal share is the ladder's own arithmetic and
		// scales with it; it is ShortestUsefulReading's rule one level in, where
		// the thing being divided is the reading's budget rather than the wall.
		//
		// The first rung is never refused: ReadingBudget already turned down a
		// wall that could not hold one reading, and a photograph that refuses
		// its own first rung is a photograph that never takes one.
		if index > 0 && left < budget/time.Duration(len(ladder)) {
			reading.Unread = fmt.Sprintf("the reading's budget of %s was spent before `%s` "+
				"could be run, and %s left is under this ladder's share of it",
				budget.Round(time.Second), rung.Command, left.Round(time.Second))
			return reading
		}
		started := time.Now()
		result, ran := RunReading(ctx, root, rung, left)
		spent := time.Since(started)
		left -= spent
		switch {
		case !ran:
			// The command could not be started at all — no shell this
			// preamble can be trusted in. Every rung runs through the same
			// shell, so there is no point trying another.
			reading.Unread = fmt.Sprintf("`%s` could not be started: this machine has no "+
				"%s for the reading's shell preamble", rung.Command, readingShell)
			return reading
		case result.TimedOut:
			// A HUNG SUITE IS AN INCOMPLETE OBSERVATION, NOT A RED ONE — but an
			// incomplete observation is not an absent one. WHAT THE RUNNER
			// NAMED BEFORE THE CEILING FIRED IS KEPT, marked partial, because a
			// roster answers "does a check for this exist" and a partial roster
			// answers it for everything it reached. ink s7 threw away the names
			// ava had streamed before its 1m53s ceiling and passed a deliverable
			// with nothing whatever to weigh.
			//
			// The ceiling has also consumed the budget, so no rung below it can
			// be afforded either way.
			reading.CutAfter = spent
			if len(result.Reported) > 0 {
				reading.Before, reading.Taken, reading.Partial = result, true, true
				reading.Unread = ""
				return reading
			}
			reading.Unread = fmt.Sprintf("`%s` was killed at its ceiling of %s without "+
				"finishing or naming a single check, so nothing it would have named is known",
				rung.Command, budget.Round(time.Second))
			return reading
		case len(result.Reported) == 0 && index+1 < len(ladder):
			// This rung told us nothing about the suite. Keep the sentence in
			// case no rung below it does either, and go on.
			reading.Unread = fmt.Sprintf("`%s` exited %d and named no checks",
				rung.Command, result.Exit)
			continue
		}
		reading.Before, reading.Taken, reading.Unread = result, true, ""
		return reading
	}
	return reading
}
