package provider

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

// ── PATIENCE FOLLOWS THE MEASURED RATE ──────────────────────────────────────
//
// The measured failure these pin, in one line: on 2026-08-31 two streams hung
// for the full five minutes before anything cut them, on endpoints that were
// demonstrably sustaining between 83 and 270 tokens a second all run. Five
// minutes was the FLOOR under a bound that was supposed to be derived from what
// the lane had done, so the derivation could never make anything shorter than
// the figure a stranger got — and every measurement this process had taken was
// worth nothing to the clock that mattered.

// TestAMeasuredLaneIsGivenLessPatienceThanAColdOne is the first half of the
// acceptance, at the arithmetic: knowing something about a lane has to buy a
// tighter bound than knowing nothing, or knowing is worthless.
func TestAMeasuredLaneIsGivenLessPatienceThanAColdOne(t *testing.T) {
	cold := wallFor(0)
	if cold != streamWallFloor {
		t.Fatalf("a cold lane's wall = %s, want the outer bound %s", cold, streamWallFloor)
	}
	// A lane whose longest finished reply was twenty-four seconds — the figure
	// measured on a headless worker on 2026-08-29, and the case the old floor
	// swallowed whole.
	measured := wallFor(24 * time.Second)
	if measured >= cold {
		t.Fatalf("a lane measured at 24s got %s and a stranger gets %s — the measurement bought nothing",
			measured, cold)
	}
	if measured != streamWallMeasuredFloor {
		t.Fatalf("a lane measured at 24s got %s, want the measured floor %s", measured, streamWallMeasuredFloor)
	}

	// And the same on the silence bound: a fast lane's gap is a fraction of what
	// a stranger is given.
	if got := gapFor(0); got != midStreamGapBound {
		t.Fatalf("an unrated lane's gap = %s, want the flat bound %s", got, midStreamGapBound)
	}
	if got := gapFor(250); got >= midStreamGapBound {
		t.Fatalf("a lane measured at 250 tok/s waits %s, the same as a stranger", got)
	}
	// The dogfood run's own sentence: a stream silent for sixty seconds from an
	// endpoint sustaining 250 tokens a second is dead, not patient.
	if got := gapFor(250); got >= time.Minute {
		t.Fatalf("a lane at 250 tok/s is given %s of silence; sixty seconds of nothing is already dead", got)
	}
}

// TestTheSilenceBoundIsInProportionToTheLanesOwnRate is the derivation itself:
// twice the rate is half the patience, and both clamps hold.
func TestTheSilenceBoundIsInProportionToTheLanesOwnRate(t *testing.T) {
	fast, half := gapFor(200), gapFor(100)
	if fast >= half {
		t.Fatalf("200 tok/s waits %s and 100 tok/s waits %s — the bound is not following the rate", fast, half)
	}
	// A lane so fast that the derived gap would be a blink is held at the lag
	// law's own figure for "quiet enough to complain about", because below that
	// the layer next door is still calling the same silence healthy.
	if got := gapFor(100_000); got != LagGap {
		t.Fatalf("gapFor(100000) = %s, want the floor %s", got, LagGap)
	}
	// And a lane slower than the flat bound's own implied rate gets the flat
	// bound: this law may tighten patience and never loosen it.
	if got := gapFor(1); got != midStreamGapBound {
		t.Fatalf("gapFor(1) = %s, want the flat bound %s — patience was loosened", got, midStreamGapBound)
	}
}

// TestTheLedgerHandsOutTheLanesOwnRate pins the seam between the measurement and
// the bound: the tokens-per-second the status line prints is the figure the
// watchdog is set from, rather than a second reading of the same wire.
func TestTheLedgerHandsOutTheLanesOwnRate(t *testing.T) {
	ledger := newVelocityLedger()
	const model = "vendor/two-lane-model"
	// Two lanes at very different speeds: one answering 400 tokens in a second,
	// one answering 400 tokens in ten.
	ledger.observe(model, "quicksilver", 200*time.Millisecond, 400, time.Second, 0)
	ledger.observe(model, "molasses", 200*time.Millisecond, 400, 10*time.Second, 0)

	if got := ledger.rate(model, "quicksilver"); got < 350 || got > 450 {
		t.Fatalf("quicksilver's rate = %v, want about 400", got)
	}
	if ledger.rate(model, "quicksilver") <= ledger.rate(model, "molasses") {
		t.Fatal("the fast lane is not reported as faster than the slow one")
	}
	// A lane nobody has seen falls back to the SLOWEST rate known for the model,
	// because before the first chunk names who is serving, the most generous
	// honest answer is the one that grants the most patience.
	if got := ledger.rate(model, "newcomer"); got != ledger.rate(model, "molasses") {
		t.Fatalf("an unseen lane's rate = %v, want the slowest known %v", got, ledger.rate(model, "molasses"))
	}
	if got := ledger.rate("vendor/never-heard-of", "anybody"); got != 0 {
		t.Fatalf("an unheard model's rate = %v, want nothing known", got)
	}
	// And the bound really is derived from it rather than from a constant.
	if gapFor(ledger.rate(model, "quicksilver")) >= gapFor(ledger.rate(model, "molasses")) {
		t.Fatal("the fast lane was not given a tighter silence bound than the slow one")
	}
}

// TestAHungStreamIsCutInProportionToItsBaselineAndNotAtTheColdFloor is the
// acceptance end to end, over a real request against a real endpoint that never
// stops writing.
//
// THE TWO FLOORS ARE SET APART ON PURPOSE. Every other test in this package
// moves them together, because it is asking about the derivation rather than
// about which floor applied; this one is asking exactly that, so a cut at the
// cold floor and a cut at the measured one are told apart by the clock.
func TestAHungStreamIsCutInProportionToItsBaselineAndNotAtTheColdFloor(t *testing.T) {
	oldFloor, oldMeasured, oldCeiling := stallWallFloor, stallWallMeasured, stallWallCeiling
	stallWallFloor = 4 * time.Second
	stallWallMeasured = 50 * time.Millisecond
	stallWallCeiling = 10 * time.Second
	defer func() {
		stallWallFloor, stallWallMeasured, stallWallCeiling = oldFloor, oldMeasured, oldCeiling
	}()

	const model = "openrouter/hung-model"
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		go func() {
			// The first chunk names the lane, which is the moment both bounds
			// narrow onto it.
			_, _ = writer.Write([]byte(`data: {"id":"one","provider":"gusher","choices":` +
				`[{"index":0,"delta":{"content":"start "}}]}` + "\n\n"))
			for {
				if request.Context().Err() != nil {
					_ = writer.CloseWithError(request.Context().Err())
					return
				}
				if _, err := writer.Write([]byte("data: " + deltaChunk("and on ") + "\n\n")); err != nil {
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       reader,
			Request:    request,
		}, nil
	})}
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "https://openrouter.ai/api/v1", Model: model,
		Routing: StaticRouting(RoutingLatency), HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	// THE BASELINE: this endpoint has finished a reply for us in ten
	// milliseconds. Five times that is fifty, which is the measured floor, and
	// it is eighty times shorter than what a stranger would be given.
	client.velocity.noteRun(model, "gusher", 10*time.Millisecond)

	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	began := time.Now()
	_, err = client.CompleteWithMessages(ctx, userMessages("hello"))
	elapsed := time.Since(began)

	cut, ok := CutFrom(err)
	if !ok || cut.Reason != CutOverrun {
		t.Fatalf("err = %v, want an overrun cut", err)
	}
	if cut.Waited >= stallWallFloor {
		t.Fatalf("the cut names %s as the bound that fired, which is the cold floor %s — the lane's own history was ignored",
			cut.Waited, stallWallFloor)
	}
	if elapsed >= stallWallFloor {
		t.Fatalf("the stream ran %s before it was cut; a lane with a baseline must not wait out the cold floor %s",
			elapsed, stallWallFloor)
	}
}

// TestAKeepaliveStillBuysPatienceOnAFastLane is the guard on the change.
//
// Narrowing the silence bound must not narrow the window a keepalive has to
// land in, or every buffering endpoint on a fast lane becomes a guaranteed
// failure — the exact regression the buffered cap was written to prevent. The
// two are different questions: whether the endpoint is on the line, and how
// fast the model behind it writes.
func TestAKeepaliveStillBuysPatienceOnAFastLane(t *testing.T) {
	restore := shortenStallBounds(t, 200*time.Millisecond, 120*time.Millisecond)
	defer restore()
	cut := false
	watch := newStallWatch(func() { cut = true }, time.Hour)
	// The timers are stopped and the verdict is asked for directly: this is a
	// test about the REASONING, and a timer firing under it would be a second
	// caller of the same once-only decision.
	watch.stop()
	watch.spoken = true
	// The lane is fast, so the silence bound narrows hard — well inside the
	// interval between two of this endpoint's keepalives.
	watch.gap = 10 * time.Millisecond
	// The endpoint spoke a moment ago, which is outside the narrowed bound and
	// comfortably inside the flat one, and the accumulated quiet is still under
	// the cap.
	watch.lastAlive = watch.clock()
	watch.quietSince = watch.clock()
	if cancel := watch.verdict(); cancel != nil {
		t.Fatal("a stream whose endpoint is still speaking was cut because its lane is fast")
	}
	if cut || watch.tripped != nil {
		t.Fatalf("the watch tripped: %v", watch.tripped)
	}
	watch.stop()

	// And the other half: the same narrowed bound DOES cut a stream whose
	// endpoint has gone silent as well, which is the whole point of narrowing.
	silent := newStallWatch(func() {}, time.Hour)
	silent.stop()
	silent.spoken = true
	silent.gap = 10 * time.Millisecond
	silent.quietSince = silent.clock().Add(-50 * time.Millisecond)
	if cancel := silent.verdict(); cancel == nil {
		t.Fatal("a stream silent for five times its lane's bound was not cut")
	}
	if silent.tripped == nil || silent.tripped.Reason != CutStalled {
		t.Fatalf("tripped = %v, want a stalled cut", silent.tripped)
	}
	if silent.tripped.Waited != 10*time.Millisecond {
		t.Fatalf("the cut names %s as the bound that fired, want the lane's own %s",
			silent.tripped.Waited, 10*time.Millisecond)
	}
	silent.stop()
}

// TestTheGapNarrowsOnceAndOnlyDownwards pins [stallWatch.regap]'s two rules,
// which are [stallWatch.rewall]'s: a bound a chunk could push out repeatedly
// would not be a bound.
func TestTheGapNarrowsOnceAndOnlyDownwards(t *testing.T) {
	watch := newStallWatch(func() {}, time.Hour)
	defer watch.stop()
	if watch.gap != stallGapBound {
		t.Fatalf("a fresh watch opens at %s, want the flat bound %s", watch.gap, stallGapBound)
	}
	watch.regap(5 * time.Second)
	if watch.gap != 5*time.Second {
		t.Fatalf("gap = %s, want the narrowed bound", watch.gap)
	}
	watch.regap(time.Second)
	if watch.gap != 5*time.Second {
		t.Fatalf("gap = %s after a second narrowing, want the first one to stand", watch.gap)
	}

	fresh := newStallWatch(func() {}, time.Hour)
	defer fresh.stop()
	fresh.regap(time.Hour)
	if fresh.gap != stallGapBound {
		t.Fatalf("gap = %s after a WIDER derivation, want patience never loosened", fresh.gap)
	}
}
