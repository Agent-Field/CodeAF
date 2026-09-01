package control

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

// Every test here states a moment rather than waiting for one. The controller
// holds no clock, so minutes of scenario run in microseconds and none of it is
// about the machine it ran on.

var epoch = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return epoch.Add(time.Duration(ms) * time.Millisecond) }

// logNormal is a survival with a given median in SECONDS and a spread in nats.
func logNormal(median, sigma float64) Survival {
	return Survival{Mu: math.Log(median), Sigma: sigma}
}

// remaining is W(s) written out again from the definition, so that the
// controller's closed form is checked against arithmetic and not against
// itself. It is the ratio of two integrals of the log-normal, which is what
// [Survival.Remaining] collapses.
func remaining(s Survival, silence float64) float64 {
	if silence <= 0 {
		return math.Exp(s.Mu + s.Sigma*s.Sigma/2)
	}
	z := (math.Log(silence) - s.Mu) / s.Sigma
	tail := 0.5 * math.Erfc(z/math.Sqrt2)
	mean := math.Exp(s.Mu + s.Sigma*s.Sigma/2)
	above := mean * 0.5 * math.Erfc(((math.Log(silence)-s.Mu)/s.Sigma-s.Sigma)/math.Sqrt2)
	return above/tail - silence
}

// plan is an ordinary interactive request, with the numbers a frontier really
// produces: the lane that was chosen is believed to start in about six hundred
// milliseconds, and the alternative — which is the alternative precisely
// because it is NOT better — in about nine hundred, from cold, for a third of a
// cent. A person is reading, so a dollar is worth ninety seconds.
func plan() Plan {
	return Plan{
		Lane:    "head",
		Ceiling: 10 * time.Second,
		Floor:   700 * time.Millisecond,
		Lambda:  90,
		Margin:  0.05,
		First:   logNormal(0.6, 0.8),
		Gap:     logNormal(0.02, 0.6),
		Alts:    []Alternative{{Lane: "other", First: logNormal(0.85, 0.5), Rate: 45, Extra: 0.003}},
		Began:   epoch,
	}
}

// ── THE ARITHMETIC ──────────────────────────────────────────────────────────

// TestTheExpectedRemainingWaitRisesWithTheWait is the property the whole design
// turns on: a stream four seconds late is not four seconds from finishing.
//
// IT RISES OVER THE RANGE A LATE REQUEST LIVES IN, which is what the design
// claims and all it claims. A log-normal's remaining wait falls at first — very
// early on, the answer really is getting closer — turns once, and climbs from
// there without ever turning back. The turn is inside the belief's own ninety-
// fifth percentile, and it moves EARLIER as the spread widens, which is why the
// predictive floor of one nat matters: at the spread this build actually waits
// against, the whole of the interesting range is the rising half.
func TestTheExpectedRemainingWaitRisesWithTheWait(t *testing.T) {
	for _, sigma := range []float64{0.4, 0.8, 1.2, 1.6} {
		belief := logNormal(1.0, sigma)
		// Four standard deviations out the survival is a millionth and the
		// closed form is two vanishing numbers divided by each other. Nothing
		// still running there was ever going to be waited on.
		far := int(math.Min(30, belief.Quantile(4)) * 1000)
		turn, least := 0.0, math.Inf(1)
		for ms := 1; ms <= far; ms++ {
			if wait := belief.Remaining(float64(ms) / 1000); wait < least {
				turn, least = float64(ms)/1000, wait
			}
		}
		if p95 := belief.Quantile(1.6449); turn > p95 {
			t.Errorf("σ=%g: W turns at %.3fs, past its own ninety-fifth percentile of %.3fs", sigma, turn, p95)
		}
		previous := least
		for ms := int(turn*1000) + 1; ms <= far; ms++ {
			wait := belief.Remaining(float64(ms) / 1000)
			if wait < previous-1e-9 {
				t.Fatalf("σ=%g: W fell from %g to %g at s=%.3f", sigma, previous, wait, float64(ms)/1000)
			}
			previous = wait
		}
		// AND AT THE SPREAD THIS BUILD WAITS AGAINST it climbs past where it
		// started: a stream that is late is expected to take longer than one
		// that has just been sent. That is the whole argument for hedging, and
		// it is why the predictive spread has a floor of one nat — a belief
		// narrower than that has no tail to be surprised by.
		if sigma >= 1 && previous <= belief.Remaining(0) {
			t.Fatalf("σ=%g: a long silence bought no pessimism at all", sigma)
		}
	}
}

// TestTheClosedFormIsTheDefinition checks [Survival.Remaining] against the
// ratio of integrals it collapses, written out separately above.
func TestTheClosedFormIsTheDefinition(t *testing.T) {
	belief := logNormal(1.2, 0.9)
	for _, silence := range []float64{0.01, 0.5, 1.2, 4, 12, 40} {
		want := remaining(belief, silence)
		got := belief.Remaining(silence)
		if math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
			t.Errorf("W(%g) = %g, want %g", silence, got, want)
		}
	}
}

// TestNothingIsBelievedAboutAnEmptySurvival is the emptiness law in one place:
// a spread of zero is a claim of certainty about a single draw and nothing here
// may make one.
func TestNothingIsBelievedAboutAnEmptySurvival(t *testing.T) {
	var nothing Survival
	if nothing.Known() || nothing.Mean() != 0 || nothing.Quantile(1.2816) != 0 || nothing.Remaining(4) != 0 {
		t.Fatal("an empty survival invented a number")
	}
}

// ── THE CROSSING ────────────────────────────────────────────────────────────

// TestTheCrossingIsWhereTheClosedFormSaysItIs solves the inequality
// independently, on a fine grid, and insists the controller acts at the same
// moment to within the grid.
func TestTheCrossingIsWhereTheClosedFormSaysItIs(t *testing.T) {
	for _, test := range []struct {
		name    string
		first   Survival
		altTTFT float64
		extra   float64
	}{
		{"a quick lane and a quick alternative", logNormal(0.4, 0.8), 0.4, 0.001},
		{"a slow lane hedges later", logNormal(3.0, 0.8), 0.9, 0.001},
		{"a dear alternative is bought later", logNormal(0.4, 0.8), 0.4, 0.02},
		{"a wide belief crosses sooner", logNormal(1.0, 1.4), 0.9, 0.001},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := plan()
			p.First = test.first
			p.Alts = []Alternative{{Lane: "other", First: logNormal(test.altTTFT, 0.6), Rate: 50, Extra: test.extra}}

			cost := logNormal(test.altTTFT, 0.6).Mean() + p.Lambda*test.extra
			ceiling := int(p.Ceiling / time.Millisecond)
			want := ceiling
			for ms := int(p.Floor / time.Millisecond); ms < ceiling; ms++ {
				if remaining(test.first, float64(ms)/1000) > cost+p.Margin {
					want = ms
					break
				}
			}

			watch := New(p)
			got := -1
			for ms := 0; ms <= ceiling; ms++ {
				if watch.Quiet(at(ms)).Kind != None {
					got = ms
					break
				}
			}
			if got != want {
				t.Fatalf("acted at %dms, the closed form says %dms", got, want)
			}
			// And the deadline named that moment before it arrived.
			ahead := New(p)
			ahead.Quiet(epoch)
			if named := ahead.Deadline().Sub(epoch); named < at(want-1).Sub(epoch) || named > at(want).Sub(epoch) {
				t.Fatalf("the deadline named %s, the crossing is at %dms", named, want)
			}
		})
	}
}

// TestTheCeilingFiresWithNoBeliefAtAll is the invariant from zero history, and
// it is the one clause of this design that is not arithmetic.
func TestTheCeilingFiresWithNoBeliefAtAll(t *testing.T) {
	p := plan()
	p.First, p.Gap, p.Think = Survival{}, Survival{}, Survival{}
	p.Alts[0].First = Survival{}
	watch := New(p)
	for step := 0; step <= 20_000; step += 50 {
		act := watch.Quiet(at(step))
		if act.Kind == None {
			continue
		}
		if want := int(p.Ceiling / time.Millisecond); step != want {
			t.Fatalf("acted at %dms, want the ceiling at %dms", step, want)
		}
		if act.Reason != "ceiling" {
			t.Fatalf("reason = %q, want the ceiling", act.Reason)
		}
		return
	}
	t.Fatal("twenty seconds of nothing at all was never acted on")
}

// TestNothingIsActedOnUnderTheFloor: below it a second request is racing the
// network rather than the lane.
func TestNothingIsActedOnUnderTheFloor(t *testing.T) {
	p := plan()
	// A lane believed to answer in a millisecond and an alternative believed
	// to answer in another one, for nothing, so the arithmetic would fire at
	// once if the floor let it.
	p.First = logNormal(0.001, 1.0)
	p.Alts = []Alternative{{Lane: "other", First: logNormal(0.001, 0.5), Rate: 50}}
	watch := New(p)
	for step := 0; step < int(p.Floor/time.Millisecond); step += 5 {
		if act := watch.Quiet(at(step)); act.Kind != None {
			t.Fatalf("acted at %dms, under a floor of %s", step, p.Floor)
		}
	}
	if watch.Quiet(at(int(p.Floor/time.Millisecond))).Kind == None {
		t.Fatal("the floor held past itself")
	}
}

// ── WHAT MOVES THE CLOCK AND WHAT DOES NOT ──────────────────────────────────

// TestAHeartbeatMovesNothing keeps the two claims apart: a comment line is
// proof about the PATH, and a silence clock a router could hold open by saying
// nothing in a well-formed way is not a clock.
func TestAHeartbeatMovesNothing(t *testing.T) {
	watch := New(plan())
	bare := New(plan())
	for step := 100; step <= 20_000; step += 100 {
		beaten := watch.Note(Reading{At: at(step), Beat: true})
		quiet := bare.Quiet(at(step))
		if beaten.Kind != quiet.Kind || beaten.Silence != quiet.Silence {
			t.Fatalf("at %dms a heartbeat changed the answer: %+v against %+v", step, beaten, quiet)
		}
		if beaten.Kind != None {
			return
		}
	}
	t.Fatal("a stream that only ever heartbeat was never acted on")
}

// TestAThinkingDeltaMovesThePhaseAndNotTheSilence is the measured defect, in
// one assertion. A run of reasoning is the endpoint writing where nobody can
// read: it is not progress, and a person is still watching an empty line.
func TestAThinkingDeltaMovesThePhaseAndNotTheSilence(t *testing.T) {
	watch := New(plan())
	act := watch.Note(Reading{At: at(200), Hidden: 1})
	if watch.Phase() != PhaseThinking {
		t.Fatalf("phase = %d after a thought, want thinking", watch.Phase())
	}
	if act.Kind != None {
		t.Fatalf("a thought was itself acted on: %+v", act)
	}
	// The silence is still measured from the request, not from the thought.
	if got := watch.Quiet(at(1000)).Silence; got != time.Second {
		t.Fatalf("silence = %s at one second in, want the whole second", got)
	}
	if got := watch.Note(Reading{At: at(1200), Visible: 1}); got.Kind != None {
		t.Fatalf("a visible token was acted on: %+v", got)
	}
	if watch.Phase() != PhaseWriting {
		t.Fatalf("phase = %d after a word, want writing", watch.Phase())
	}
	if got := watch.Quiet(at(1300)).Silence; got != 100*time.Millisecond {
		t.Fatalf("silence = %s after a word at 1.2s, want it reset to 100ms", got)
	}
}

// TestALongThinkIsNotAStallAndAStalledThinkIs is the pair the thinking phase
// exists for. The alternative would have to think the same thought, so a think
// that is merely long buys nothing by being hedged; one that has stopped
// arriving is a stall like any other.
func TestALongThinkIsNotAStallAndAStalledThinkIs(t *testing.T) {
	base := plan()
	base.Think = logNormal(30, 0.6) // this model deliberates for half a minute
	base.Gap = logNormal(0.05, 0.7) // and writes its thoughts twenty a second
	base.Ceiling = 90 * time.Second // a role with the patience for it
	base.First = logNormal(1.0, 0.8)

	t.Run("a model deliberating steadily is left alone", func(t *testing.T) {
		watch := New(base)
		for step := 200; step <= 25_000; step += 50 {
			if act := watch.Note(Reading{At: at(step), Hidden: 1}); act.Kind != None {
				t.Fatalf("hedged a healthy thought at %dms: %+v", step, act)
			}
		}
	})

	t.Run("a model that stops writing its thoughts is acted on", func(t *testing.T) {
		watch := New(base)
		for step := 200; step <= 2_000; step += 50 {
			watch.Note(Reading{At: at(step), Hidden: 1})
		}
		acted := 0
		for step := 2_050; step <= 30_000; step += 50 {
			if watch.Quiet(at(step)).Kind != None {
				acted = step
				break
			}
		}
		if acted == 0 {
			t.Fatal("a run of thought that went quiet was never acted on")
		}
		if acted > 2_000+int(base.Ceiling/time.Millisecond) {
			t.Fatalf("the stall was acted on at %dms, past its own ceiling", acted)
		}
	})
}

// TestReasoningAfterAWordIsWritingAndNotDrift is the other half of
// [TestAThinkingDeltaMovesThePhaseAndNotTheSilence], and the two together are
// the whole of the two silences.
//
// WHAT A PERSON WAITS THROUGH AND WHAT THE WIRE IS DOING ARE DIFFERENT
// QUANTITIES. The silence is the person's and it is what the floor and the
// ceiling ask about — a thought is not progress, and the clock a person is
// watching does not reset for one. The stall clocks ask the wire: an endpoint
// that has written three words and is now reasoning has NOT stopped, and a
// second request bought for it would be bought for a lane that never stalled.
func TestReasoningAfterAWordIsWritingAndNotDrift(t *testing.T) {
	base := plan()
	base.Think = logNormal(30, 0.6)
	base.Gap = logNormal(0.05, 0.7) // twenty deltas a second, believed tightly

	t.Run("a lane that keeps writing where nobody can read is left alone", func(t *testing.T) {
		watch := New(base)
		watch.Note(Reading{At: at(200), Visible: 1})
		for step := 250; step <= 8_000; step += 50 {
			if act := watch.Note(Reading{At: at(step), Hidden: 1}); act.Kind != None {
				t.Fatalf("a lane still writing was acted on at %dms: %+v", step, act)
			}
		}
	})

	t.Run("and the silence it is waiting through is still the person's", func(t *testing.T) {
		watch := New(base)
		watch.Note(Reading{At: at(200), Visible: 1})
		watch.Note(Reading{At: at(400), Hidden: 1})
		if got := watch.Quiet(at(1_400)).Silence; got != 1_200*time.Millisecond {
			t.Fatalf("silence = %s, want the 1.2s since the last word a person could read", got)
		}
	})

	t.Run("a lane that stops writing altogether is still acted on", func(t *testing.T) {
		watch := New(base)
		watch.Note(Reading{At: at(200), Visible: 1})
		watch.Note(Reading{At: at(400), Hidden: 1})
		acted := 0
		for step := 450; step <= 30_000; step += 50 {
			if watch.Quiet(at(step)).Kind != None {
				acted = step
				break
			}
		}
		if acted == 0 {
			t.Fatal("a stream that went quiet after one thought was never acted on")
		}
		if acted > 200+int(base.Ceiling/time.Millisecond) {
			t.Fatalf("the stall was acted on at %dms, past the ceiling of the silence", acted)
		}
	})
}

// ── WHICH ACT ───────────────────────────────────────────────────────────────

// TestAtTheCeilingAnAffordableAlternativeIsTakenAndNotReported is the one
// ruling that separates the ceiling from the arithmetic above it.
//
// λ decides how early a wait is worth money, and for a role nobody is watching
// it is zero: no amount of money buys speed, so nothing fires while the
// inequality is what is being asked. THE CEILING ASKS A DIFFERENT QUESTION. It
// is the promise that no call this build makes waits longer than that, whatever
// is believed and whatever a second is worth — so a wait that reaches it with
// somewhere affordable to go takes it, and a report there would be the build
// saying the wait is real while holding the answer to it.
//
// A PIN IS STILL ASKED AND NEVER OVERRIDDEN, which is the second half: the same
// moment, the same alternative, and a question instead of a rescue.
func TestAtTheCeilingAnAffordableAlternativeIsTakenAndNotReported(t *testing.T) {
	unwatched := plan()
	unwatched.Lambda = 0

	// NOTHING IS BOUGHT BEFORE THE CEILING. With nobody waiting the inequality
	// can never fire, so every moment short of the bound is a moment of waiting.
	watch := New(unwatched)
	for step := 0; step < int(unwatched.Ceiling/time.Millisecond); step += 50 {
		if act := watch.Quiet(at(step)); act.Kind != None {
			t.Fatalf("with nobody waiting, %v was bought at %dms — before the ceiling", act.Kind, step)
		}
	}
	act := watch.Quiet(at(int(unwatched.Ceiling / time.Millisecond)))
	if act.Kind != Hedge || act.Lane != "other" {
		t.Fatalf("at the ceiling the act was %v to %q, want a rescue to the lane the frontier named", act.Kind, act.Lane)
	}
	if act.Reason != "ceiling" {
		t.Fatalf("reason = %q, want the bound that raised it", act.Reason)
	}

	// AND A PINNED LANE IS ASKED THERE, not overridden.
	pinned := unwatched
	pinned.Pinned = true
	asked := New(pinned).Quiet(at(int(pinned.Ceiling/time.Millisecond) + 50))
	if asked.Kind != Ask || asked.Lane != "other" {
		t.Fatalf("a pinned lane at its ceiling raised %v to %q, want the question", asked.Kind, asked.Lane)
	}

	// AND WITH NOWHERE AFFORDABLE TO GO IT IS STILL A REPORT. Silence is never
	// an option; a wait that is real is said out loud.
	unaffordable := unwatched
	unaffordable.Purse = broke{}
	if told := New(unaffordable).Quiet(at(int(unaffordable.Ceiling/time.Millisecond) + 50)); told.Kind != Report {
		t.Fatalf("a ceiling with a refusing purse raised %v, want the wait reported", told.Kind)
	}
}

func TestWhichActIsRaised(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(Plan) Plan
		want Kind
		lane string
	}{
		{"an ordinary slow lane is hedged", func(p Plan) Plan { return p }, Hedge, "other"},
		{"a pinned lane is asked", func(p Plan) Plan { p.Pinned = true; return p }, Ask, "other"},
		{"nothing to hedge to is reported", func(p Plan) Plan { p.Alts = nil; return p }, Report, ""},
		{"nobody waiting is still rescued at the ceiling", func(p Plan) Plan { p.Lambda = 0; return p }, Hedge, "other"},
		{"a purse that refuses is reported", func(p Plan) Plan { p.Purse = broke{}; return p }, Report, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			watch := New(test.make(plan()))
			for step := 0; step <= 20_000; step += 50 {
				act := watch.Quiet(at(step))
				if act.Kind == None {
					continue
				}
				if act.Kind != test.want {
					t.Fatalf("act = %d at %dms, want %d", act.Kind, step, test.want)
				}
				if act.Lane != test.lane {
					t.Fatalf("lane = %q, want %q", act.Lane, test.lane)
				}
				if !watch.Acted(test.want) {
					t.Fatalf("Acted(%d) = false after raising one", test.want)
				}
				return
			}
			t.Fatalf("nothing was raised in twenty seconds")
		})
	}
}

// broke is a purse with nothing in it.
type broke struct{}

func (broke) Allows(float64, time.Time) bool { return false }

// TestAPinNeverSpendsThePurse. Asking the budget is spending it, so a
// controller that polled it while deciding to raise an offer would take
// somebody's allowance for an arm nobody sent.
func TestAPinNeverSpendsThePurse(t *testing.T) {
	asked := &counted{}
	p := plan()
	p.Pinned, p.Purse = true, asked
	watch := New(p)
	for step := 0; step <= 30_000; step += 50 {
		watch.Quiet(at(step))
	}
	if asked.asks != 0 {
		t.Fatalf("the purse was asked %d times for an offer nobody paid for", asked.asks)
	}
}

// TestThePurseIsAskedOncePerArm: a refusal is final for the request, and a
// controller that polled it every beat would be asking the same question of the
// same numbers.
func TestThePurseIsAskedOncePerArm(t *testing.T) {
	asked := &counted{}
	p := plan()
	p.Purse = asked
	watch := New(p)
	for step := 0; step <= 30_000; step += 50 {
		watch.Quiet(at(step))
	}
	if asked.asks != 1 {
		t.Fatalf("the purse was asked %d times for one arm", asked.asks)
	}
}

// counted is a purse that says no and remembers being asked.
type counted struct{ asks int }

func (c *counted) Allows(float64, time.Time) bool {
	c.asks++
	return false
}

// TestAnOfferIsRaisedOnce: a second question about one request is nagging.
func TestAnOfferIsRaisedOnce(t *testing.T) {
	p := plan()
	p.Pinned = true
	watch := New(p)
	offers := 0
	for step := 0; step <= 30_000; step += 50 {
		if watch.Quiet(at(step)).Kind == Ask {
			offers++
		}
	}
	if offers != 1 {
		t.Fatalf("%d offers for one request, want exactly one", offers)
	}
}

// TestManyArmsUnderOnePurse: a hedge may fire more than once, and what bounds
// it is the alternatives and the money rather than a counter.
func TestManyArmsUnderOnePurse(t *testing.T) {
	p := plan()
	p.Alts = []Alternative{
		{Lane: "b", First: logNormal(0.5, 0.6), Rate: 50, Extra: 0.001},
		{Lane: "c", First: logNormal(0.6, 0.6), Rate: 50, Extra: 0.001},
	}
	watch := New(p)
	var lanes []string
	kinds := map[Kind]int{}
	for step := 0; step <= 30_000; step += 50 {
		act := watch.Quiet(at(step))
		kinds[act.Kind]++
		if act.Kind == Hedge {
			lanes = append(lanes, act.Lane)
		}
	}
	if len(lanes) != 2 || lanes[0] != "b" || lanes[1] != "c" {
		t.Fatalf("arms went to %v, want each alternative once and in order", lanes)
	}
	if kinds[Escalate] != 1 {
		t.Fatalf("%d escalations after every lane was tried, want one", kinds[Escalate])
	}
	if kinds[Report] != 1 {
		t.Fatalf("%d reports after the ladder was spent, want one", kinds[Report])
	}
}

// TestCommitIsTheSameInequalityReadBackwards. There is no commitment constant:
// what used to be sixty-four tokens is the rewrite term, which grows with the
// text on the screen.
func TestCommitIsTheSameInequalityReadBackwards(t *testing.T) {
	p := plan()
	p.First = logNormal(0.4, 0.8)
	watch := New(p)
	// Slow enough to be hedged, and then it starts writing.
	hedged := false
	for step := 0; step <= 20_000; step += 50 {
		if watch.Quiet(at(step)).Kind == Hedge {
			hedged = true
			break
		}
	}
	if !hedged {
		t.Fatal("the stream was never hedged, so there is nothing to commit against")
	}
	commits := 0
	for token, step := 1, 3_000; token <= 40; token, step = token+1, step+20 {
		if watch.Note(Reading{At: at(step), Visible: 1}).Kind == Commit {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("%d commits, want exactly one: an arm earns the answer once", commits)
	}
	if !watch.Acted(Commit) {
		t.Fatal("Acted(Commit) = false after committing")
	}
}

// TestNothingCommitsWithNothingRacing: an unraced stream wins by finishing.
func TestNothingCommitsWithNothingRacing(t *testing.T) {
	watch := New(plan())
	for token, step := 1, 100; token <= 200; token, step = token+1, step+20 {
		if act := watch.Note(Reading{At: at(step), Visible: 1}); act.Kind != None {
			t.Fatalf("act %d on a stream nobody is racing", act.Kind)
		}
	}
}

// ── THE DEADLINE ────────────────────────────────────────────────────────────

// TestTheDeadlineIsTheMomentTheAnswerChanges, and it is never in the past — a
// moment already gone is a timer that fires immediately and forever, which is
// how a beat becomes a spin.
func TestTheDeadlineIsTheMomentTheAnswerChanges(t *testing.T) {
	watch := New(plan())
	named := watch.Deadline()
	for step := 0; step <= 20_000; step += 50 {
		now := at(step)
		act := watch.Quiet(now)
		if deadline := watch.Deadline(); deadline.Before(now) {
			t.Fatalf("at %dms the deadline was %s in the past", step, now.Sub(deadline))
		}
		if act.Kind == None {
			if !named.After(now) {
				t.Fatalf("at %dms the deadline had passed and nothing happened", step)
			}
			named = watch.Deadline()
			continue
		}
		// It fired at the moment the deadline named, to within the beat this
		// test walks in.
		if gap := now.Sub(named); gap < 0 || gap > 50*time.Millisecond {
			t.Fatalf("acted at %dms, the deadline said %s", step, named.Sub(epoch))
		}
		return
	}
	t.Fatal("nothing was acted on")
}

// TestTheDeadlineNeverRunsPastTheCeiling, whatever is believed.
func TestTheDeadlineNeverRunsPastTheCeiling(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(Plan) Plan
	}{
		{"no belief at all", func(p Plan) Plan { p.First, p.Gap = Survival{}, Survival{}; return p }},
		{"nobody waiting", func(p Plan) Plan { p.Lambda = 0; return p }},
		{"nowhere to go", func(p Plan) Plan { p.Alts = nil; return p }},
		{"a lane believed to take an hour", func(p Plan) Plan { p.First = logNormal(3600, 0.2); return p }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := test.make(plan())
			watch := New(p)
			watch.Quiet(at(0))
			if got := watch.Deadline(); got.After(epoch.Add(p.Ceiling)) {
				t.Fatalf("deadline = %s, past a ceiling of %s", got.Sub(epoch), p.Ceiling)
			}
		})
	}
}

// TestTheDeadlineMovesWithTheProgress: each visible token starts the wait again,
// so the moment worth waking for moves with it.
func TestTheDeadlineMovesWithTheProgress(t *testing.T) {
	watch := New(plan())
	watch.Note(Reading{At: at(300), Visible: 1})
	first := watch.Deadline()
	watch.Note(Reading{At: at(900), Visible: 1})
	second := watch.Deadline()
	if !second.After(first) {
		t.Fatalf("a second word left the deadline at %s", first.Sub(epoch))
	}
}

// ── THE LANE THAT REALLY ANSWERED ───────────────────────────────────────────

func TestTheControllerFollowsTheLaneTheStreamNames(t *testing.T) {
	p := plan()
	p.First = logNormal(0.4, 0.5)
	p.Alts = []Alternative{{Lane: "other", First: logNormal(4.0, 0.5), Rate: 45, Extra: 0.003}}
	watch := New(p)
	// Asked for a machine believed to start in four hundred milliseconds and
	// served by one believed to take four seconds. The wait is judged against
	// the machine that is really writing: at a second and a half this stream is
	// early for its lane, and calling it late at the head lane's figure would
	// be a surprise about nobody.
	watch.Serving("elsewhere", logNormal(4.0, 0.5), logNormal(0.05, 0.5), at(10))
	for step := 0; step <= 1_500; step += 50 {
		if act := watch.Quiet(at(step)); act.Kind != None {
			t.Fatalf("called the serving lane late at %dms on the head lane's belief: %+v", step, act)
		}
	}
}

func TestAWindowThatHasClosedDoesNotReopen(t *testing.T) {
	watch := New(plan())
	watch.Note(Reading{At: at(300), Visible: 1})
	before := watch.Deadline()
	watch.Serving("elsewhere", logNormal(60, 0.5), Survival{}, at(300))
	if got := watch.Deadline(); !got.Equal(before) {
		t.Fatalf("deadline moved to %s after the answer started", got.Sub(epoch))
	}
}

// ── THE FALSE-HEDGE RATE ────────────────────────────────────────────────────

// TestAHealthyLaneIsNeverHedged is the pass criterion of §K said as a unit
// test. A lane doing what it is believed to do costs nobody a second request,
// and a lane drawing honestly from its own belief costs one in a hundred —
// inside Dean & Barroso's two per cent, which is the figure the whole
// mechanism's economics rest on.
func TestAHealthyLaneIsNeverHedged(t *testing.T) {
	p := plan()
	streams := 500
	for _, test := range []struct {
		name  string
		clamp float64 // the widest draw, in standard deviations
		worst float64 // the share of streams that may be hedged
	}{
		{"a lane doing exactly what it is believed to do", 1.2816, 0},
		{"a lane drawing honestly from its own belief", math.Inf(1), 0.02},
	} {
		t.Run(test.name, func(t *testing.T) {
			random := rand.New(rand.NewSource(11))
			draw := func(belief Survival) time.Duration {
				z := random.NormFloat64()
				if z > test.clamp {
					z = test.clamp
				}
				return time.Duration(math.Exp(belief.Mu+belief.Sigma*z) * float64(time.Second))
			}
			hedges := 0
			for range streams {
				watch := New(p)
				now, acted := epoch, false
				// The beat asks every fifty milliseconds all the way through, so
				// the stall test is driven as often as it would really be.
				arrive := func(wait time.Duration) {
					until := now.Add(wait)
					for now.Add(50 * time.Millisecond).Before(until) {
						now = now.Add(50 * time.Millisecond)
						acted = acted || watch.Quiet(now).Kind == Hedge
					}
					now = until
					acted = acted || watch.Note(Reading{At: now, Visible: 1}).Kind == Hedge
				}
				arrive(draw(p.First))
				for range 200 {
					arrive(draw(p.Gap))
				}
				if acted {
					hedges++
				}
			}
			if share := float64(hedges) / float64(streams); share > test.worst {
				t.Fatalf("%d of %d healthy streams were hedged (%.1f%%), the bound is %.0f%%",
					hedges, streams, 100*share, 100*test.worst)
			}
		})
	}
}

// TestAStalledLaneIsAlwaysActedOnInsideTheCeiling is the other side of the
// same proof, and it is the invariant: not a percentile, every trial.
func TestAStalledLaneIsAlwaysActedOnInsideTheCeiling(t *testing.T) {
	p := plan()
	for _, believed := range []Survival{{}, logNormal(0.4, 0.8), logNormal(3, 1.2), logNormal(600, 0.2)} {
		p.First = believed
		watch := New(p)
		acted := time.Duration(-1)
		for step := 0; step <= 60_000; step += 10 {
			if watch.Quiet(at(step)).Kind != None {
				acted = time.Duration(step) * time.Millisecond
				break
			}
		}
		if acted < 0 || acted > p.Ceiling {
			t.Fatalf("belief %+v: acted after %s, the ceiling is %s", believed, acted, p.Ceiling)
		}
	}
}
