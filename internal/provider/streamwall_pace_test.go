package provider

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ── THE WALL BOUNDS A REPLY THAT IS NOT WORKING, NEVER A REPLY THAT IS LONG ─
//
// THE MEASUREMENT THESE PIN, from 2026-09-10: a task asked `moonshotai/kimi-k3`
// for one self-contained HTML file. Three attempts on three endpoints streamed a
// single `write` call at the lane's ordinary rate — 5,510 tokens, 3,576, then
// 26,145 — and each was cut at its wall with nothing wrong with it, because the
// wall was an elapsed-time timer derived from the five-second tool-call turns
// the lane had finished before, and a nine-thousand-token file does not fit in
// two and a half minutes at forty tokens a second. The journal rows said zero
// tokens, because the cut counted answer text and the reply was a call.
//
// The lane in these tests writes four hundred tokens a second ([steadyClient]
// tells the ledger so with one [velocityLedger.brisk] sighting), and its walls
// are shortened to milliseconds by [shortenWall] so the machinery runs in real
// time. The stream is [writeCallStream]'s.

// TestAToolCallKeepingItsLanesPaceOutlivesItsWall is the defect's fix at the
// wire: a `write` streaming at its lane's own rate for three times its wall is
// long, not wedged. The wall fires, finds the stream kept pace, and re-arms; the
// call lands whole. And the ledger learns the long reply it could never have
// learned before — the wall this lane is given next is derived from it.
func TestAToolCallKeepingItsLanesPaceOutlivesItsWall(t *testing.T) {
	const wall = 300 * time.Millisecond
	defer shortenWall(t, wall, time.Minute)()
	// Forty bytes — ten tokens — every twenty-five milliseconds is the lane's
	// own four hundred a second.
	piece := strings.Repeat("a", 40)
	server := httptest.NewServer(writeCallStream("steady", piece, 25*time.Millisecond, 3*wall))
	defer server.Close()
	client := steadyClient(t, server.URL)

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	began := time.Now()
	response, err := client.CompleteWithMessages(ctx, userMessages("write the page"))
	if err != nil {
		t.Fatalf("a call streaming at its lane's pace was cut: %v", err)
	}
	if ran := time.Since(began); ran < 3*wall {
		t.Fatalf("the stream ran %s, want past three walls of %s — the test proved nothing", ran, wall)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 1 || !strings.HasSuffix(calls[0].Function.Arguments, piece+`"}`) {
		t.Fatalf("the call did not land whole: %+v", calls)
	}
	if got := client.velocity.wall("sim/model", "steady"); got <= 3*wall {
		t.Fatalf("wall after the long reply = %s, want it derived from a reply past %s", got, 3*wall)
	}
}

// TestAToolCallAtATenthOfItsLanesPaceIsCutAtTheFirstWall is the other half of
// the law, and the reason the wall exists at all: an endpoint that keeps writing
// but writes a tenth of what its lane writes is not going to finish. It is cut
// at its FIRST wall, with the sentence it always had — and the row carries how
// much of the call had arrived, which it used to journal as nothing.
func TestAToolCallAtATenthOfItsLanesPaceIsCutAtTheFirstWall(t *testing.T) {
	const wall = time.Second
	defer shortenWall(t, wall, time.Minute)()
	// Ten tokens every quarter second is forty a second: a tenth of the lane.
	server := httptest.NewServer(writeCallStream("steady", strings.Repeat("a", 40), 250*time.Millisecond, 0))
	defer server.Close()
	client := steadyClient(t, server.URL)

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	_, err := client.CompleteWithMessages(ctx, userMessages("write the page"))
	cut, ok := CutFrom(err)
	if !ok || cut.Reason != CutOverrun {
		t.Fatalf("err = %v, want an overrun cut", err)
	}
	if cut.Waited != wall {
		t.Fatalf("waited = %s, want the first wall %s: a stream off its pace buys no second period", cut.Waited, wall)
	}
	if got, want := cut.Error(), "the reply ran past 1s without finishing and was cut"; got != want {
		t.Fatalf("sentence = %q, want the unchanged %q", got, want)
	}
	if cut.Provider != "steady" {
		t.Fatalf("provider = %q, want the lane the stream named", cut.Provider)
	}
	// NOT ONE BYTE OF ANSWER TEXT WAS SENT. Every token on the cut is a token of
	// the call's arguments, which is the figure the autopsy of 2026-09-10 could
	// not see.
	if cut.Tokens < 10 {
		t.Fatalf("tokens = %d, want the call's arguments counted on the cut", cut.Tokens)
	}
}

// pacedWatch is a stall watch on a hand-held clock, for the arithmetic of the
// re-arm stated in hours rather than raced in milliseconds. Its timers are
// stopped and every period is an hour, so nothing real fires under the test,
// and the ceiling is the one stated — a day unless the test is about it.
func pacedWatch(t *testing.T, pace float64, ceiling time.Duration) (*stallWatch, func(time.Duration)) {
	t.Helper()
	old := stallWallCeiling
	stallWallCeiling = ceiling
	t.Cleanup(func() { stallWallCeiling = old })
	watch := newStallWatch(talking(), func() {}, time.Hour, pace)
	watch.stop()
	t.Cleanup(watch.stop)
	now := watch.born
	watch.clock = func() time.Time { return now }
	return watch, func(to time.Duration) { now = watch.born.Add(to) }
}

// TestARearmedWallNamesTheBoundItReached is the sentence law after a re-arm:
// the figure a person is told is the figure the timer was set to. A stream that
// kept pace for two periods and then fell to a drip is cut at the end of the
// third, and "ran past 3h0m0s" is what it did — not the one-hour wall it opened
// under.
func TestARearmedWallNamesTheBoundItReached(t *testing.T) {
	// One token a second, so a period owes 3,600 and a fifth of that is 720.
	watch, advance := pacedWatch(t, 1, 24*time.Hour)
	watch.progress(charsPerToken)
	for hour := 1; hour <= 2; hour++ {
		advance(time.Duration(hour) * time.Hour)
		watch.progress(3600 * charsPerToken)
		if cancel := watch.overrunVerdict(); cancel != nil {
			t.Fatalf("hour %d: a stream that kept its lane's pace was cut", hour)
		}
		if want := time.Duration(hour+1) * time.Hour; watch.walled != want {
			t.Fatalf("hour %d: the wall re-armed to %s, want %s", hour, watch.walled, want)
		}
	}
	advance(3 * time.Hour)
	watch.progress(100 * charsPerToken) // a hundred tokens in an hour: a drip
	if cancel := watch.overrunVerdict(); cancel == nil {
		t.Fatal("a period that delivered a thirty-sixth of its pace was re-armed")
	}
	if watch.tripped.Waited != 3*time.Hour {
		t.Fatalf("waited = %s, want the whole bound the timer reached", watch.tripped.Waited)
	}
	if got, want := watch.tripped.Error(), "the reply ran past 3h0m0s without finishing and was cut"; got != want {
		t.Fatalf("sentence = %q, want %q", got, want)
	}
	if got := watch.tokens(); got != 1+2*3600+100 {
		t.Fatalf("tokens = %d, want every token the stream wrote in the one estimate", got)
	}
}

// TestAStreamAtPaceIsStillCutAtTheCeiling is the one bound no evidence moves.
// The re-arm adds a period and never more than the ceiling allows, and a wall
// that has reached the ceiling cuts a stream that is still keeping pace.
func TestAStreamAtPaceIsStillCutAtTheCeiling(t *testing.T) {
	watch, advance := pacedWatch(t, 1, 150*time.Minute)
	watch.progress(charsPerToken)
	for _, at := range []time.Duration{time.Hour, 2 * time.Hour} {
		advance(at)
		watch.progress(3600 * charsPerToken)
		if cancel := watch.overrunVerdict(); cancel != nil {
			t.Fatalf("at %s: a stream keeping pace was cut short of the ceiling", at)
		}
	}
	if watch.walled != stallWallCeiling {
		t.Fatalf("the wall re-armed to %s, want it clamped at the ceiling %s", watch.walled, stallWallCeiling)
	}
	advance(stallWallCeiling)
	watch.progress(1800 * charsPerToken)
	if cancel := watch.overrunVerdict(); cancel == nil {
		t.Fatal("a stream at the ceiling was re-armed because it kept pace — the ceiling moved on evidence")
	}
	if watch.tripped.Reason != CutOverrun || watch.tripped.Waited != stallWallCeiling {
		t.Fatalf("tripped = %+v, want an overrun naming the ceiling", watch.tripped)
	}
}

// TestAStrangerIsHeldToTheLagRate pins the pace a lane with no measured rate is
// judged at: [LagRate], so a fifth of it is six tokens a second. A stranger
// writing at twenty-four — the slowest healthy lane of 2026-09-10 — keeps pace;
// one dripping a token a second does not, and is cut at its first wall exactly
// as it always was.
func TestAStrangerIsHeldToTheLagRate(t *testing.T) {
	if got := paceFor(0); got != LagRate {
		t.Fatalf("paceFor(0) = %v, want the lag law's rate %v", got, LagRate)
	}
	if got := paceFor(250); got != 250 {
		t.Fatalf("paceFor(250) = %v, want the lane's own rate", got)
	}
	healthy, advance := pacedWatch(t, paceFor(0), 24*time.Hour)
	healthy.progress(charsPerToken)
	advance(time.Hour)
	healthy.progress(24 * 3600 * charsPerToken)
	if healthy.overrunVerdict() != nil {
		t.Fatal("a stranger writing twenty-four tokens a second was cut")
	}
	drip, advance := pacedWatch(t, paceFor(0), 24*time.Hour)
	drip.progress(charsPerToken)
	advance(time.Hour)
	drip.progress(3600 * charsPerToken)
	if drip.overrunVerdict() == nil {
		t.Fatal("a stranger dripping a token a second was re-armed")
	}
}

// TestAWallThatWasReArmedIsNotNarrowedByALateName keeps [stallWatch.rewall]'s
// once-only rule honest after a re-arm: a wall already extended on a
// measurement of pace is not pulled back by a lane named afterwards.
func TestAWallThatWasReArmedIsNotNarrowedByALateName(t *testing.T) {
	watch, advance := pacedWatch(t, 1, 24*time.Hour)
	watch.progress(charsPerToken)
	advance(time.Hour)
	watch.progress(3600 * charsPerToken)
	if watch.overrunVerdict() != nil {
		t.Fatal("a stream keeping pace was cut")
	}
	watch.rewall(time.Minute, 1000)
	if watch.walled != 2*time.Hour || watch.pace != 1 {
		t.Fatalf("walled %s at pace %v after a late name, want the re-armed 2h at 1", watch.walled, watch.pace)
	}
}
