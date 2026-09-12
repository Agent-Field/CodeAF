package control

import (
	"testing"
	"time"
)

// collapsedRate drives visible readings at a tenth of the believed rate and
// asks Quiet on the same fifty-millisecond beat as the healthy-lane scenarios.
func collapsedRate(p Plan) (Controller, Act, time.Time, time.Time) {
	watch := New(p)
	watch.Note(Reading{At: epoch, Visible: 1})
	var established time.Time
	for step := 50; step <= int(5*p.Ceiling/time.Millisecond); step += 50 {
		var act Act
		if step%500 == 0 {
			act = watch.Note(Reading{At: at(step), Visible: 1})
			if established.IsZero() && act.Silence > 0 {
				established = at(step)
			}
		} else {
			act = watch.Quiet(at(step))
		}
		if act.Kind != None {
			return watch, act, established, at(step)
		}
	}
	return watch, Act{}, established, time.Time{}
}

// TestAStreamThatTricklesIsActedOnWithinTheCeilingAndNotAtTheWall is C1: a
// visible stream at a tenth of its believed rate stops earning progress, so
// the ordinary ceiling acts instead of leaving the transport's wall to cut it.
func TestAStreamThatTricklesIsActedOnWithinTheCeilingAndNotAtTheWall(t *testing.T) {
	p := plan()
	p.Gap = logNormal(0.05, 1)
	p.Ceiling = 2 * time.Second
	p.Lambda = 0

	_, act, established, actedAt := collapsedRate(p)
	if act.Kind == None {
		// Before the aggregate credit gate, this same script could keep writing
		// for minutes without an act; only the transport wall eventually cut it.
		t.Fatal("a stream writing at a tenth of its believed rate was never acted on")
	}
	if act.Reason != RateReason {
		t.Fatalf("reason = %q, want %q", act.Reason, RateReason)
	}
	if established.IsZero() {
		t.Fatal("the stream acted before its collapsed rate became established")
	}
	if delay := actedAt.Sub(established); delay > p.Ceiling {
		t.Fatalf("acted %s after the collapse became established, past the %s ceiling", delay, p.Ceiling)
	}
	if wall := 5 * p.Ceiling; !actedAt.Before(p.Began.Add(wall)) {
		t.Fatalf("acted at %s, not before the transport wall at %s", actedAt.Sub(p.Began), wall)
	}
}

// TestAStreamKeepingUpIsNeverCutForItsRate is C2 and the unknown-belief
// invariant: ordinary or faster visible progress is always credited, and no
// rate judgment is made from a gap nobody measured.
func TestAStreamKeepingUpIsNeverCutForItsRate(t *testing.T) {
	for _, test := range []struct {
		name string
		gap  Survival
		each int
	}{
		{"at the believed rate", logNormal(0.05, 1), 50},
		{"at twice the believed rate", logNormal(0.05, 1), 25},
		{"with no measured rate", Survival{}, 50},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := plan()
			p.Gap = test.gap
			p.Ceiling = 500 * time.Millisecond
			// This isolates the ceiling path by switching λ off;
			// TestAHealthyStreamIsNeverActedOnBeforeTheCeiling covers the payoff
			// path stochastically.
			p.Lambda = 0
			watch := New(p)
			watch.Note(Reading{At: epoch, Visible: 1})
			for step := 5; step <= int(11*p.Ceiling/time.Millisecond); step += 5 {
				var act Act
				if step%test.each == 0 {
					act = watch.Note(Reading{At: at(step), Visible: 1})
				} else {
					act = watch.Quiet(at(step))
				}
				if act.Kind != None {
					t.Fatalf("a healthy stream was acted on at %dms: %+v", step, act)
				}
			}
		})
	}
}

// TestAtOneGapTheAggregateIsTheTestBesideIt is C6: on the first measured gap,
// the aggregate credit decision flips on exactly the same readings as the
// existing single-gap abnormality decision.
func TestAtOneGapTheAggregateIsTheTestBesideIt(t *testing.T) {
	p := plan()
	p.Gap = logNormal(0.05, 1)
	p.Ceiling = 10 * time.Second
	p.Floor = 0
	p.Lambda = 1
	p.Margin = 0
	p.Alts = []Alternative{{Lane: "other", First: logNormal(0.000001, 0.1), Rate: 1e9}}

	var sawKeepingUp, sawCollapsed bool
	for gap := 1; gap <= 1000; gap++ {
		aggregate := New(p)
		aggregate.Note(Reading{At: epoch, Visible: 1})
		credited := aggregate.Note(Reading{At: at(gap), Visible: 1}).Silence == 0

		single := New(p)
		single.Note(Reading{At: epoch, Visible: 1})
		abnormal := single.Quiet(at(gap)).Kind != None
		if credited == abnormal {
			t.Fatalf("at one %dms gap, aggregate credited=%v while the single-gap test acted=%v", gap, credited, abnormal)
		}
		sawKeepingUp = sawKeepingUp || credited
		sawCollapsed = sawCollapsed || abnormal
	}
	if !sawKeepingUp || !sawCollapsed {
		t.Fatalf("the driven range never crossed the shared boundary (keeping up %v, collapsed %v)", sawKeepingUp, sawCollapsed)
	}
}

// TestAfterARateActTheClockStartsAgain is C3 and D6: the collapsed rate has
// its own reason, and every answer from the ladder begins another ceiling
// instead of leaving the next deadline in the past.
func TestAfterARateActTheClockStartsAgain(t *testing.T) {
	p := plan()
	p.Gap = logNormal(0.05, 1)
	p.Ceiling = 2 * time.Second
	p.Lambda = 0

	watch, act, _, actedAt := collapsedRate(p)
	if act.Kind == None || act.Reason != RateReason {
		t.Fatalf("rate act = %+v, want a real act naming %q", act, RateReason)
	}
	if act.Kind != Hedge {
		t.Fatalf("first rate act = %v, want the ladder's hedge", act.Kind)
	}
	if got, want := watch.Deadline(), actedAt.Add(p.Ceiling); !got.Equal(want) {
		t.Fatalf("deadline = %s, want a fresh ceiling at %s", got.Sub(actedAt), want.Sub(actedAt))
	}

	want := []Kind{Escalate, Report, None, None}
	for rung, wantKind := range want {
		var got Act
		for elapsed := 500 * time.Millisecond; elapsed <= p.Ceiling; elapsed += 500 * time.Millisecond {
			now := actedAt.Add(time.Duration(rung)*p.Ceiling + elapsed)
			got = watch.Note(Reading{At: now, Visible: 1})
			if deadline := watch.Deadline(); deadline.Before(now) {
				t.Fatalf("after %v at %s, deadline %s is before now", got.Kind, now.Sub(actedAt), deadline.Sub(actedAt))
			}
			if elapsed < p.Ceiling && got.Kind != None {
				t.Fatalf("the ladder acted before its fresh ceiling: %+v", got)
			}
		}
		if got.Kind != wantKind {
			t.Fatalf("ladder answer %d = %v, want %v", rung+2, got.Kind, wantKind)
		}
		if got.Reason != RateReason {
			t.Fatalf("ladder answer %d reason = %q, want %q", rung+2, got.Reason, RateReason)
		}
		if gotDeadline, wantDeadline := watch.Deadline(), actedAt.Add(time.Duration(rung+2)*p.Ceiling); !gotDeadline.Equal(wantDeadline) {
			t.Fatalf("ladder answer %d deadline = %s, want fresh ceiling at %s", rung+2, gotDeadline.Sub(actedAt), wantDeadline.Sub(actedAt))
		}
	}
}

// TestARecoveredStreamStopsBeingJudgedOnItsBadPatch is D3: decay makes the
// rate judgment current, so a sub-ceiling trickle followed by an honest rate
// recovers its progress credit before the frozen clock can act.
func TestARecoveredStreamStopsBeingJudgedOnItsBadPatch(t *testing.T) {
	p := plan()
	p.Gap = logNormal(0.05, 1)
	p.Ceiling = 2 * time.Second
	p.Lambda = 0
	watch := New(p)
	watch.Note(Reading{At: epoch, Visible: 1})
	watch.Note(Reading{At: at(500), Visible: 1})
	bad := watch.Note(Reading{At: at(1000), Visible: 1})
	if bad.Silence == 0 {
		t.Fatal("the bad patch never established a collapsed rate, so recovery was not exercised")
	}

	recovered := false
	for step := 1010; step <= 12_000; step += 10 {
		var act Act
		if (step-1000)%50 == 0 {
			act = watch.Note(Reading{At: at(step), Visible: 1})
			if act.Silence == 0 {
				recovered = true
			}
		} else {
			act = watch.Quiet(at(step))
		}
		if act.Kind != None {
			t.Fatalf("a stream that recovered was acted on at %dms: %+v", step, act)
		}
	}
	if !recovered {
		t.Fatal("a healthy rate never shed the bad patch within the ceiling's decay horizon")
	}
}

// Providers may group tokens into one streamed event; batching does not lower
// the visible throughput that the lane's per-token rate belief describes.
func TestHealthyBatchedTokensKeepTheirProgressCredit(t *testing.T) {
	p := plan()
	p.Gap = logNormal(0.05, 1)
	p.Ceiling = 2 * time.Second
	p.Lambda = 0
	watch := New(p)
	watch.Note(Reading{At: epoch, Visible: 10})
	for step := 50; step <= 10000; step += 50 {
		var act Act
		if step%500 == 0 {
			act = watch.Note(Reading{At: at(step), Visible: 10})
		} else {
			act = watch.Quiet(at(step))
		}
		if act.Kind != None {
			t.Fatalf("20 tokens/second in batches was treated as a collapsed rate at %dms: %+v", step, act)
		}
	}
}

// ── A STREAM FROM A MACHINE NOBODY ASKED FOR ───────────────────────────────

// TestARouterSubstitutingATenTimesSlowerMachineIsNoticed is the 2026-09-11
// shape, in the numbers it happened in. The question went out expecting the
// machine the choice named — believed at sixty tokens a second — and the router
// answered from one believed at seven. Every gap is then perfectly ordinary FOR
// THE MACHINE THAT ANSWERED, which is exactly why nothing used to notice: the
// bar moved to whoever picked up.
func TestARouterSubstitutingATenTimesSlowerMachineIsNoticed(t *testing.T) {
	p := plan()
	// What the choice named, and the reason this request went out at all.
	p.Gap = logNormal(1/60.0, 0.6)
	p.Expected = 1200
	p.Ceiling = 10 * time.Second
	p.Lambda = 0

	watch := New(p)
	// And the machine that really answered, with its own — correct, learned —
	// belief that it writes at seven a second.
	watch.Serving("other", Survival{}, logNormal(1/7.3, 0.6), epoch)

	var acted Act
	for token := 1; token <= 604; token++ {
		act := watch.Note(Reading{At: at(token * 137), Visible: 1})
		if act.Kind != None && acted.Kind == None {
			acted = act
		}
	}
	if acted.Kind == None {
		t.Fatal("a stream ten times slower than the machine the question asked for was never acted on")
	}
	if acted.Reason != RateReason {
		t.Fatalf("reason = %q, want %q", acted.Reason, RateReason)
	}
	// The whole wait the person really paid was 86 seconds, because a stream
	// that is writing is not a silence and nothing read it as one. The promise
	// is the role's own ceiling, counted from the last credited progress, and
	// one beat's slack over it is the reading the stream happened to land on.
	if acted.Silence > p.Ceiling+time.Second {
		t.Fatalf("acted after %s of silence, past the %s ceiling", acted.Silence, p.Ceiling)
	}
}

// TestAMachineAnsweringAtTheRateItWasAskedForIsNeverJudgedSlow is the other half
// and the false-positive bound. The same collapsed machine, asked for on
// purpose: nobody was promised anything faster, so nothing here is abnormal and
// the bar and the belief are one number.
func TestAMachineAnsweringAtTheRateItWasAskedForIsNeverJudgedSlow(t *testing.T) {
	p := plan()
	p.Gap = logNormal(1/7.3, 0.6)
	p.Expected = 1200
	p.Ceiling = 10 * time.Second
	p.Lambda = 0

	watch := New(p)
	watch.Serving("head", Survival{}, logNormal(1/7.3, 0.6), epoch)
	for token := 1; token <= 604; token++ {
		if act := watch.Note(Reading{At: at(token * 137), Visible: 1}); act.Reason == RateReason {
			t.Fatalf("a machine answering at exactly the rate it was asked for was called collapsed at token %d: %+v", token, act)
		}
	}
}

// TestAQuestionSentBlindIsEntitledToNothingInParticular is the cold pair, where
// there is no expectation to hold anybody to: the first belief that arrives
// becomes the bar, and a machine matching its own belief is left alone.
func TestAQuestionSentBlindIsEntitledToNothingInParticular(t *testing.T) {
	p := plan()
	p.Gap = Survival{}
	p.Expected = 1200
	p.Ceiling = 10 * time.Second
	p.Lambda = 0

	watch := New(p)
	watch.Serving("other", Survival{}, logNormal(1/7.3, 0.6), epoch)
	for token := 1; token <= 604; token++ {
		if act := watch.Note(Reading{At: at(token * 137), Visible: 1}); act.Reason == RateReason {
			t.Fatalf("a question sent with no belief judged its answer slow at token %d: %+v", token, act)
		}
	}
}
