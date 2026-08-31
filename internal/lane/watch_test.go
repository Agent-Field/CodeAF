package lane

import (
	"math"
	"testing"
	"time"
)

// The watch is pure, so every test here states a moment rather than waiting for
// one: the whole file runs in microseconds and none of it is about the machine
// it ran on.

func msIn(base time.Time, ms int) time.Time {
	return base.Add(time.Duration(ms) * time.Millisecond)
}

// beliefOf builds a belief whose median first token is ttft milliseconds and
// whose median rate is tokens a second, both believed reasonably firmly.
func beliefOf(ttft, rate float64) Belief {
	return Belief{
		TTFT: Posterior{X: math.Log(ttft), P: 0.04},
		Rate: Posterior{X: math.Log(rate), P: 0.04},
	}
}

func TestTheDeadlineIsTheChoicesWhenItNamedOne(t *testing.T) {
	start := time.Now()
	watch := NewWatch(Choice{Deadline: 1200 * time.Millisecond, Alt: "B"}, beliefOf(400, 50), start)
	if got := watch.Deadline(); got != 1200*time.Millisecond {
		t.Fatalf("deadline = %s, want the choice's 1.2s", got)
	}
}

func TestADerivedDeadlineIsTheBeliefsNinetiethClampedBothWays(t *testing.T) {
	start := time.Now()
	for _, test := range []struct {
		name string
		ttft float64
		want time.Duration
	}{
		{"a fast lane hedges at the floor", 400, deadlineFloor},
		{"a middling lane hedges at its own p90", 2000, 2585 * time.Millisecond},
		{"a slow lane hedges at the ceiling", 9000, deadlineCeiling},
	} {
		t.Run(test.name, func(t *testing.T) {
			watch := NewWatch(Choice{Alt: "B"}, beliefOf(test.ttft, 50), start)
			got := watch.Deadline()
			if delta := got - test.want; delta > 5*time.Millisecond || delta < -5*time.Millisecond {
				t.Fatalf("deadline = %s, want about %s", got, test.want)
			}
		})
	}
}

func TestALaneNothingIsBelievedAboutGetsNoDerivedDeadline(t *testing.T) {
	watch := NewWatch(Choice{Alt: "B"}, Belief{}, time.Now())
	if got := watch.Deadline(); got != 0 {
		t.Fatalf("deadline = %s, want none: a hedge fired on no evidence is a bill for a guess", got)
	}
}

func TestAFirstTokenPastTheDeadlineIsHedgedExactlyOnce(t *testing.T) {
	start := time.Now()
	watch := NewWatch(Choice{Deadline: 1200 * time.Millisecond, Alt: "B"}, beliefOf(400, 50), start)

	if verdict := watch.Silence(msIn(start, 1100)); verdict.Hedge {
		t.Fatalf("hedged at 1.1s, before the 1.2s deadline")
	}
	verdict := watch.Silence(msIn(start, 1300))
	if !verdict.Hedge || verdict.Reason != "first token late" {
		t.Fatalf("verdict at 1.3s = %+v, want a hedge for a late first token", verdict)
	}
	if again := watch.Silence(msIn(start, 1400)); again.Hedge {
		t.Fatalf("hedged twice; a request gets one hedge and not a race")
	}
	if watch.PathFault() {
		t.Fatalf("a late first token is a slow lane, not a dead path")
	}
}

func TestNoHeartbeatAndNoByteIsAPathFaultTheLaneIsNotChargedFor(t *testing.T) {
	start := time.Now()
	watch := NewWatch(Choice{Deadline: 2 * time.Second, Alt: "B"}, beliefOf(400, 50), start)

	// Three seconds in, the deadline has passed but the dead-path bound
	// (2 × 2s) has not, so what fires is the late first token.
	if verdict := watch.Silence(msIn(start, 3000)); !verdict.Hedge || verdict.Reason != "first token late" {
		t.Fatalf("verdict at 3s = %+v, want the late first token", verdict)
	}

	// And on a watch that has not spent its hedge, the same silence past the
	// dead-path bound is the other claim entirely.
	quiet := NewWatch(Choice{Deadline: 2 * time.Second, Alt: "B"}, beliefOf(400, 50), start)
	verdict := quiet.Silence(msIn(start, 4100))
	if !verdict.Hedge || verdict.Reason != "no heartbeat" {
		t.Fatalf("verdict at 4.1s = %+v, want a dead path", verdict)
	}
	if !quiet.PathFault() {
		t.Fatalf("PathFault = false; a stream with no sign of life says nothing about the lane")
	}
}

func TestAHeartbeatKeepsThePathAliveWhileTheLaneIsStillJudged(t *testing.T) {
	start := time.Now()
	watch := NewWatch(Choice{Deadline: 2 * time.Second, Alt: "B"}, beliefOf(400, 50), start)
	for ms := 500; ms <= 4000; ms += 500 {
		watch.Heartbeat(msIn(start, ms))
	}
	verdict := watch.Silence(msIn(start, 4100))
	if !verdict.Hedge || verdict.Reason != "first token late" {
		t.Fatalf("verdict = %+v, want the LANE blamed: the router kept saying it was alive", verdict)
	}
	if watch.PathFault() {
		t.Fatalf("PathFault = true on a path that was heartbeating all along")
	}
}

func TestOneVeryLongGapAlarmsOnItsOwn(t *testing.T) {
	start := time.Now()
	watch := NewWatch(Choice{Deadline: time.Second, Alt: "B"}, beliefOf(400, 50), start)
	watch.Token(1, 1, msIn(start, 500))
	verdict := watch.Token(2, 2, msIn(start, 500+16_000))
	if !verdict.Hedge || verdict.Reason != "gap" {
		t.Fatalf("verdict after a sixteen-second gap = %+v, want a gap alarm", verdict)
	}
}

func TestDriftAccumulatesOverGapsThatAreEachOnlySomewhatSlow(t *testing.T) {
	start := time.Now()
	// Fifty tokens a second is a gap of twenty milliseconds.
	watch := NewWatch(Choice{Deadline: time.Second, Alt: "B"}, beliefOf(400, 50), start)
	watch.Token(1, 1, msIn(start, 400))

	// Gaps of two hundred milliseconds: ten times slower than believed, which
	// is ln(10) − 0.5 ≈ 1.8 nats of surprise each. One is not enough on its
	// own — a lane is allowed a bad moment — and two are.
	moment := 400 + 200
	if verdict := watch.Token(2, 2, msIn(start, moment)); verdict.Hedge {
		t.Fatalf("hedged on one slow gap; a lane is allowed a bad moment")
	}
	moment += 200
	if verdict := watch.Token(3, 3, msIn(start, moment)); !verdict.Hedge || verdict.Reason != "drift" {
		t.Fatalf("verdict on the second slow gap = %+v, want a drift alarm", verdict)
	}
}

func TestASteadyLaneNeverDrifts(t *testing.T) {
	start := time.Now()
	watch := NewWatch(Choice{Deadline: time.Second, Alt: "B"}, beliefOf(400, 50), start)
	moment := 400
	watch.Token(1, 1, msIn(start, moment))
	for token := 2; token <= 200; token++ {
		moment += 20
		if verdict := watch.Token(token, token, msIn(start, moment)); verdict.Hedge {
			t.Fatalf("hedged at token %d on a lane writing exactly as believed", token)
		}
	}
}

func TestASilenceMidStreamIsJudgedWhileItIsStillHappening(t *testing.T) {
	start := time.Now()
	watch := NewWatch(Choice{Deadline: time.Second, Alt: "B"}, beliefOf(400, 1000), start)
	watch.Token(1, 1, msIn(start, 400))
	// A believed gap of one millisecond. The stall is judged from the silence
	// beat rather than from the token that will eventually end it.
	if verdict := watch.Silence(msIn(start, 410)); verdict.Hedge {
		t.Fatalf("hedged ten milliseconds into a stall")
	}
	verdict := watch.Silence(msIn(start, 400+60))
	if !verdict.Hedge || verdict.Reason != "drift" {
		t.Fatalf("verdict sixty milliseconds into the stall = %+v, want a drift alarm", verdict)
	}
}

func TestPastTheCommitmentPointAnAlmostFinishedAnswerIsNotAbandoned(t *testing.T) {
	start := time.Now()
	choice := Choice{
		Deadline: time.Second,
		Alt:      "B",
		Frontier: []Scored{{ID: ID{Model: "m", Lane: "B"}, TTFT: 500, Rate: 1000}},
	}
	watch := NewWatch(choice, beliefOf(400, 1000), start)
	watch.SetExpectedTokens(220)
	moment := 400
	watch.Token(1, 1, msIn(start, moment))
	for token := 2; token <= 200; token++ {
		moment++
		watch.Token(token, token, msIn(start, moment))
	}
	// Twenty tokens from the end, a stall that would alarm anywhere else.
	if verdict := watch.Silence(msIn(start, moment+20_000)); verdict.Hedge {
		t.Fatalf("abandoned an answer with twenty tokens to go: %+v", verdict)
	}
	if watch.Hedged() {
		t.Fatalf("Hedged = true on a committed stream")
	}
}

func TestBeforeTheCommitmentPointTheSameStallIsHedged(t *testing.T) {
	start := time.Now()
	choice := Choice{
		Deadline: time.Second,
		Alt:      "B",
		Frontier: []Scored{{ID: ID{Model: "m", Lane: "B"}, TTFT: 500, Rate: 1000}},
	}
	watch := NewWatch(choice, beliefOf(400, 1000), start)
	watch.SetExpectedTokens(220)
	moment := 400
	watch.Token(1, 1, msIn(start, moment))
	for token := 2; token <= 30; token++ {
		moment++
		watch.Token(token, token, msIn(start, moment))
	}
	verdict := watch.Silence(msIn(start, moment+20_000))
	if !verdict.Hedge {
		t.Fatalf("no hedge thirty tokens in with the answer stalled: %+v", verdict)
	}
}

func TestPastTheCommitmentPointAStalledStreamWithMostOfTheAnswerLeftIsStillHedged(t *testing.T) {
	start := time.Now()
	choice := Choice{
		Deadline: time.Second,
		Alt:      "B",
		Frontier: []Scored{{ID: ID{Model: "m", Lane: "B"}, TTFT: 50, Rate: 1000}},
	}
	// A lane believed to write one token a second, with two thousand to go.
	watch := NewWatch(choice, beliefOf(400, 1), start)
	watch.SetExpectedTokens(2100)
	moment := 400
	watch.Token(1, 1, msIn(start, moment))
	for token := 2; token <= 100; token++ {
		moment += 1000
		watch.Token(token, token, msIn(start, moment))
	}
	verdict := watch.Silence(msIn(start, moment+20_000))
	if !verdict.Hedge {
		t.Fatalf("stayed on a lane that would take two thousand more seconds: %+v", verdict)
	}
}

func TestAWatchWithNobodyToHedgeToNeverAsks(t *testing.T) {
	start := time.Now()
	watch := NewWatch(Choice{Deadline: 100 * time.Millisecond}, beliefOf(400, 50), start)
	if verdict := watch.Silence(msIn(start, 30_000)); verdict.Hedge {
		t.Fatalf("hedged with no alternative named: %+v", verdict)
	}
}

// ── THE BUDGET ──────────────────────────────────────────────────────────────

func TestTheAllowanceIsCountedInRequestsAndNotInMinutes(t *testing.T) {
	now := time.Now()
	budget := NewBudget(2, 0)
	if !budget.Allow(now, 0) || !budget.Allow(now, 0) {
		t.Fatalf("a fresh budget refused one of its first two hedges")
	}
	if budget.Allow(now, 0) {
		t.Fatalf("a third hedge went out inside the same twenty requests")
	}
	// TIME ALONE BUYS NOTHING. This is the correction the simulator forced: a
	// slow batch of requests must not refill its own allowance while it runs.
	if budget.Allow(now.Add(time.Hour), 0) {
		t.Fatalf("an hour of waiting refilled an allowance that is counted in requests")
	}
	// Requests do. Nineteen of them still hold the first two hedges inside the
	// window; the twentieth slides the first one out.
	for range 19 {
		budget.NoteRequest(now)
	}
	if budget.Allow(now, 0) {
		t.Fatalf("a hedge went out with two still inside the last twenty requests")
	}
	budget.NoteRequest(now)
	if !budget.Allow(now, 0) {
		t.Fatalf("the oldest hedge never slid out of the window")
	}
}

func TestTheAllowanceHoldsAtAboutOneHedgeInTen(t *testing.T) {
	now := time.Now()
	budget := NewBudget(2, 0)
	hedges := 0
	for range 200 {
		if budget.Allow(now, 0) {
			hedges++
		}
		budget.NoteRequest(now)
	}
	// Two in the first twenty and one in every ten after them.
	if hedges < 18 || hedges > 22 {
		t.Fatalf("%d hedges in two hundred requests, want about one in ten", hedges)
	}
}

func TestAZeroAllowanceIsHowHedgingIsSwitchedOff(t *testing.T) {
	if NewBudget(0, 0.5).Allow(time.Now(), 0) {
		t.Fatalf("a budget of nothing allowed a hedge")
	}
	var nothing *Budget
	if nothing.Allow(time.Now(), 0) {
		t.Fatalf("a nil budget allowed a hedge")
	}
}

func TestTheShareRefusesOnceHedgingHasHadItsTenthOfTheBill(t *testing.T) {
	now := time.Now()
	budget := NewBudget(60, 0.10)
	budget.NoteSpend(1.00, now)
	if !budget.Allow(now, 0.05) {
		t.Fatalf("refused a five-cent hedge against a dollar of spending")
	}
	budget.NoteHedge(0.09, now)
	if budget.Allow(now, 0.05) {
		t.Fatalf("allowed a hedge that would take the share past a tenth")
	}
	// And an hour later the window has rolled: neither the spending nor the
	// hedging that was in it is judged any more.
	later := now.Add(2 * time.Hour)
	if !budget.Allow(later, 0.05) {
		t.Fatalf("the share was still judging an hour-old bill")
	}
}

func TestWithNothingSpentTheRequestAllowanceAloneGoverns(t *testing.T) {
	now := time.Now()
	budget := NewBudget(2, 0.10)
	if !budget.Allow(now, 0.02) {
		t.Fatalf("refused the first hedge of a session that has billed nothing yet")
	}
}
