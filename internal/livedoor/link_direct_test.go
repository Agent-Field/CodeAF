package livedoor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// The tests in this file are derived from the validation contract (SPEC-P2 V30 to
// V39 and section 3.2) and from the Link's specified behaviour (SPEC-LINK A, C to
// L), never from the Link's code. Time is the FakeClock and nothing else; every
// path is a net.Pipe with a real Serve on the far end.

// V30: once the direct tunnel is up, the view's channels move onto it. Each relay
// channel ends with a planned redial that is never spoken, and the next open of
// the same chat is carried by the direct road, all inside the 3 s the contract
// allows (here, the settle delay and not one tick more).
func TestADirectLinkTakesTheChannelsOnceItsTunnelIsUp(t *testing.T) {
	rig := newDirectRig(t, rigOpts{})
	chA := rig.openChat("chat-a")
	chB := rig.openChat("chat-b")
	if road := rig.speak(chA); road != RoadRelay {
		t.Fatalf("the first paint was carried on %q, want the relay", road)
	}
	if road := rig.speak(chB); road != RoadRelay {
		t.Fatalf("the second chat was carried on %q, want the relay", road)
	}
	if got := rig.link.State(); got.Kind != Live || got.Road != RoadRelay {
		t.Fatalf("before the swap State() = %+v, want Live on the relay", got)
	}
	if n := rig.directDials.Load(); n != 0 {
		t.Fatalf("a direct attempt began %d times before the settle delay", n)
	}

	// The first attempt is due DirectSettle after the first channel opened, and
	// not a millisecond before.
	rig.waitAttemptArmed(DirectSettle)
	rig.fc.Advance(DirectSettle - time.Millisecond)
	quiesce()
	if n := rig.directDials.Load(); n != 0 {
		t.Fatalf("the attempt began %v early", time.Millisecond)
	}
	rig.fc.Advance(time.Millisecond)
	rig.waitRoad(RoadDirect)

	if got := rig.link.State(); got.Kind != Live || got.Road != RoadDirect || got.Reason != "" {
		t.Fatalf("after the swap State() = %+v, want Live on direct with no reason", got)
	}
	for name, ch := range map[string]io.ReadWriteCloser{"chat-a": chA, "chat-b": chB} {
		err := rig.endOf(ch)
		if !isPlanned(err) {
			t.Fatalf("%s's relay channel ended with %v, want a planned redial", name, err)
		}
		if why, ok := ReasonOf(err); !ok || why != ReasonRoadChange {
			t.Fatalf("%s ended for %q (%v), want %q", name, why, ok, ReasonRoadChange)
		}
		if _, werr := ch.Write([]byte("typed after the swap\n")); !isPlanned(werr) {
			t.Fatalf("a write on %s's ended relay channel returned %v, want the same planned redial", name, werr)
		}
	}
	// THE PLANNED END IS NEVER SPOKEN: OnChannelEnd is not told a road change.
	if ended := rig.endedList(); len(ended) != 0 {
		t.Fatalf("OnChannelEnd was told %v for a swap, want nothing", ended)
	}

	// What a roaming client does next: open the same chats again. Both land on
	// the direct road, and the holder says so.
	if road := rig.speak(rig.openChat("chat-a")); road != RoadDirect {
		t.Fatalf("chat-a reopened on %q, want direct", road)
	}
	if road := rig.speak(rig.openChat("chat-b")); road != RoadDirect {
		t.Fatalf("chat-b reopened on %q, want direct", road)
	}
	if got := rig.opensOn(RoadRelay); len(got) != 2 {
		t.Fatalf("the relay host resolved %v, want only the two first opens", got)
	}
	if got := rig.opensOn(RoadDirect); len(got) != 2 {
		t.Fatalf("the direct host resolved %v, want the two reopens", got)
	}
	if n := rig.relayDials.Load(); n != 1 {
		t.Fatalf("the relay was dialled %d times, want once", n)
	}
	if n := rig.directDials.Load(); n != 1 {
		t.Fatalf("the direct road was dialled %d times, want once", n)
	}
}

// V30: the road word flips in the state with exactly ONE OnState call in each
// direction, with no intermediate Reconnecting or Idle, and no banner.
func TestTheRoadWordFlipsInTheStateAndNotifiesOnce(t *testing.T) {
	t.Run("relay to direct", func(t *testing.T) {
		rig := newDirectRig(t, rigOpts{})
		rig.speak(rig.openChat("chat-a"))
		rig.waitState(Live, RoadRelay)
		quiesce()
		base := rig.link.State()
		n := rig.stateCount()

		rig.swapToDirect()
		within(t, "the flip to be notified", func() bool { return rig.stateCount() >= n+1 })
		quiesce()
		got := rig.statesFrom(n)
		if len(got) != 1 {
			t.Fatalf("the flip notified %d times (%+v), want exactly once", len(got), got)
		}
		if got[0].Kind != Live || got[0].Road != RoadDirect || got[0].Reason != "" {
			t.Fatalf("the notification was %+v, want Live on direct with no reason", got[0])
		}
		if got[0].Permit.Watch != base.Permit.Watch || got[0].Permit.Drive != base.Permit.Drive {
			t.Fatalf("the permit changed across the swap: %+v then %+v", base.Permit, got[0].Permit)
		}
		// Time on a healthy direct road raises nothing more.
		rig.advanceHealthy(rig.latest(RoadDirect), 3*time.Second)
		if more := rig.statesFrom(n); len(more) != 1 {
			t.Fatalf("a quiet direct road notified again: %+v", more)
		}
		if st := rig.link.State(); st.Kind != Live || st.Road != RoadDirect {
			t.Fatalf("State() = %+v, want Live on direct", st)
		}
	})

	t.Run("direct back to relay", func(t *testing.T) {
		rig := newDirectRig(t, rigOpts{})
		rig.speak(rig.openChat("chat-a"))
		rig.swapToDirect()
		rig.speak(rig.openChat("chat-a"))
		quiesce()
		n := rig.stateCount()

		rig.swapToRelay()
		within(t, "the flip back to be notified", func() bool { return rig.stateCount() >= n+1 })
		quiesce()
		got := rig.statesFrom(n)
		if len(got) != 1 {
			t.Fatalf("the fallback notified %d times (%+v), want exactly once with no Reconnecting or WentOffline between", len(got), got)
		}
		if got[0].Kind != Live || got[0].Road != RoadRelay {
			t.Fatalf("the notification was %+v, want Live on the relay", got[0])
		}
	})
}

// V30: the first paint never waits on the direct attempt. The first relay channel
// opens and reads before any attempt exists, and while an attempt's dial is stuck
// the relay still opens channels, reads them and answers every call the Link has.
func TestTheFirstPaintNeverWaitsOnTheDirectAttempt(t *testing.T) {
	rig := newDirectRig(t, rigOpts{})
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	rig.setDirect(func(ctx context.Context, _ int, _ Signaller) (io.ReadWriteCloser, error) {
		select {
		case <-release:
			return nil, errors.New("released")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	// The first paint: opened and read with the clock never moved and no
	// attempt begun.
	if road := rig.speak(rig.openChat("chat-a")); road != RoadRelay {
		t.Fatalf("the first paint was carried on %q, want the relay", road)
	}
	if n := rig.directDials.Load(); n != 0 {
		t.Fatalf("a direct dial began (%d) before the first paint was done", n)
	}

	// The attempt begins and its dial is stuck.
	rig.waitAttemptArmed(DirectSettle)
	rig.fc.Advance(DirectSettle)
	within(t, "the attempt's dial to begin", func() bool { return rig.directDials.Load() == 1 })
	nStates := rig.stateCount()

	// With the dial stuck, a second chat opens and paints on the relay, and the
	// Link answers its own calls.
	if road := rig.speak(rig.openChat("chat-b")); road != RoadRelay {
		t.Fatalf("a chat opened during the attempt was carried on %q, want the relay", road)
	}
	bounded(t, "State, Road and Touch during an attempt", func() {
		_ = rig.link.State()
		_ = rig.link.Road()
		rig.link.Touch()
	})
	if rig.link.Road() != RoadRelay {
		t.Fatalf("the road read %q while the attempt was stuck, want the relay", rig.link.Road())
	}
	if n := rig.directDials.Load(); n != 1 {
		t.Fatalf("the stuck attempt was dialled %d times, want once", n)
	}
	if got := rig.statesFrom(nStates); len(got) != 0 {
		t.Fatalf("a stuck attempt raised %+v, want nothing", got)
	}

	once.Do(func() { close(release) })
	within(t, "the released dial to return", func() bool { return rig.directDone.Load() == 1 })
	quiesce()
	if got := rig.statesFrom(nStates); len(got) != 0 {
		t.Fatalf("a failed attempt raised %+v, want nothing", got)
	}
}

// V32: an attempt that never connects is abandoned after DirectConnectWithin on
// the fake clock and leaves the view exactly as it was: the same State, no
// OnState, no channel ended, no relay redial, and the retries are as silent.
func TestADirectAttemptThatNeverConnectsLeavesTheViewOnTheRelayWithNoStateChange(t *testing.T) {
	rig := newDirectRig(t, rigOpts{})
	rig.setDirect(func(ctx context.Context, _ int, _ Signaller) (io.ReadWriteCloser, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ch := rig.openChat("chat-a")
	if road := rig.speak(ch); road != RoadRelay {
		t.Fatalf("the first paint was carried on %q, want the relay", road)
	}
	quiesce()
	before := rig.link.State()
	nStates := rig.stateCount()

	assertUntouched := func(when string) {
		t.Helper()
		got := rig.link.State()
		if got.Kind != before.Kind || got.Reason != before.Reason || got.Road != before.Road || !got.Since.Equal(before.Since) || got.Permit.Watch != before.Permit.Watch || got.Permit.Drive != before.Permit.Drive {
			t.Fatalf("%s: State() = %+v, was %+v", when, got, before)
		}
		if more := rig.statesFrom(nStates); len(more) != 0 {
			t.Fatalf("%s: OnState was called with %+v", when, more)
		}
		if ended := rig.endedList(); len(ended) != 0 {
			t.Fatalf("%s: channels ended: %v", when, ended)
		}
		if _, err := ch.Write([]byte("the view is still here\n")); err != nil {
			t.Fatalf("%s: the view's channel no longer takes writes: %v", when, err)
		}
		if n := rig.relayDials.Load(); n != 1 {
			t.Fatalf("%s: the relay was dialled %d times, want once", when, n)
		}
	}

	rig.waitAttemptArmed(DirectSettle)
	rig.fc.Advance(DirectSettle)
	within(t, "the first attempt to begin", func() bool { return rig.directDials.Load() == 1 })

	// It is abandoned after DirectConnectWithin of the fake clock, not before.
	rig.waitAttemptArmed(DirectConnectWithin)
	rig.fc.Advance(DirectConnectWithin - time.Millisecond)
	quiesce()
	if n := rig.directDone.Load(); n != 0 {
		t.Fatalf("the attempt was abandoned %v early", time.Millisecond)
	}
	assertUntouched("while the attempt is still within its time")
	rig.fc.Advance(time.Millisecond)
	within(t, "the attempt to be abandoned", func() bool { return rig.directDone.Load() == 1 })
	quiesce()
	assertUntouched("after the first attempt was abandoned")

	// The retry comes after the first rung and is as silent.
	rig.ladderStep(DirectBackoff[0])
	rig.waitAttemptArmed(DirectConnectWithin)
	rig.fc.Advance(DirectConnectWithin)
	within(t, "the second attempt to be abandoned", func() bool { return rig.directDone.Load() == 2 })
	quiesce()
	assertUntouched("after the second attempt was abandoned")
}

// V32: the attempt times on the fake clock climb DirectSettle, then
// DirectBackoff (5 s, 15 s, 60 s, 5 min, 10 min, 10 min again); the ladder starts
// over after a direct link stayed up DirectStableAfter and on every new relay
// tunnel, and a direct link that dies young does not start it over.
func TestDirectRetriesBackOffFiveFifteenSixtyThreeHundredSixHundredAndResetAfterAMinuteUp(t *testing.T) {
	t.Run("the climb", func(t *testing.T) {
		rig := newDirectRig(t, rigOpts{idleDetach: 24 * time.Hour})
		rig.failDirect()
		rig.speak(rig.openChat("chat-a"))
		for _, want := range []time.Duration{
			DirectSettle,
			DirectBackoff[0], DirectBackoff[1], DirectBackoff[2], DirectBackoff[3], DirectBackoff[4], DirectBackoff[4],
		} {
			rig.ladderStep(want)
		}
		quiesce()
		if got := rig.link.State(); got.Kind != Live || got.Road != RoadRelay {
			t.Fatalf("after seven failed attempts State() = %+v, want Live on the relay", got)
		}
		if n := rig.stateCount(); n > 2 {
			t.Fatalf("failed attempts raised OnState: %+v", rig.statesFrom(0))
		}
	})

	// connectOnFourth makes the first three attempts fail and the fourth succeed,
	// so the ladder has climbed to its 60 s rung when the road comes up.
	connectOnFourth := func(t *testing.T) *directRig {
		rig := newDirectRig(t, rigOpts{idleDetach: 24 * time.Hour})
		rig.setDirect(func(_ context.Context, n int, _ Signaller) (io.ReadWriteCloser, error) {
			if n <= 3 {
				return nil, errors.New("no direct path")
			}
			return rig.newTunnel(RoadDirect), nil
		})
		rig.speak(rig.openChat("chat-a"))
		rig.ladderStep(DirectSettle)
		rig.ladderStep(DirectBackoff[0])
		rig.ladderStep(DirectBackoff[1])
		rig.ladderStep(DirectBackoff[2])
		rig.waitRoad(RoadDirect)
		return rig
	}

	t.Run("a direct link that stayed up a minute starts the ladder over", func(t *testing.T) {
		rig := connectOnFourth(t)
		direct := rig.latest(RoadDirect)
		rig.advanceHealthy(direct, DirectStableAfter+time.Second)
		before := rig.directDials.Load()
		rig.swapToRelay()
		// An attempt needs a channel to move, so the roaming client's redial is
		// played here: the chat is opened again, and now on the relay.
		if road := rig.speak(rig.openChat("chat-b")); road != RoadRelay {
			t.Fatalf("the reopened chat was carried on %q, want the relay", road)
		}
		// A reset ladder tries again within the first rung; one that kept
		// climbing would wait minutes.
		for i := 0; i < int(DirectBackoff[0]/(100*time.Millisecond))+10 && rig.directDials.Load() == before; i++ {
			rig.advanceFine(100 * time.Millisecond)
		}
		within(t, "a new attempt within the first rung after a stable link", func() bool { return rig.directDials.Load() > before })
	})

	t.Run("a direct link that died young keeps the ladder climbing", func(t *testing.T) {
		rig := connectOnFourth(t)
		direct := rig.latest(RoadDirect)
		rig.advanceHealthy(direct, 10*time.Second)
		rig.swapToRelay()
		// An attempt needs a channel to move, so the roaming client's redial is
		// played here: the chat is opened again, and now on the relay.
		if road := rig.speak(rig.openChat("chat-b")); road != RoadRelay {
			t.Fatalf("the reopened chat was carried on %q, want the relay", road)
		}
		// It was a failure like the three before it: the next wait is the
		// fourth rung, five minutes, not the first.
		rig.ladderStep(DirectBackoff[3])
	})

	t.Run("a new relay tunnel starts the ladder over", func(t *testing.T) {
		rig := newDirectRig(t, rigOpts{idleDetach: 24 * time.Hour})
		rig.failDirect()
		rig.speak(rig.openChat("chat-a"))
		rig.ladderStep(DirectSettle)
		rig.ladderStep(DirectBackoff[0])
		rig.ladderStep(DirectBackoff[1])
		rig.waitAttemptArmed(DirectBackoff[2])

		rig.tunnel(RoadRelay, 1).kill()
		within(t, "the Link to notice its relay tunnel is gone", func() bool { return rig.link.State().Kind != Live })
		if road := rig.speak(rig.askAgainAfterDrop("chat-b")); road != RoadRelay {
			t.Fatalf("the reopened chat was carried on %q, want the relay", road)
		}
		// How many dials it takes to get a new tunnel (the Link's own redial and a
		// roaming client's open may both dial) is phase 1's business; that there is
		// a new one is the premise of this case.
		if n := rig.relayDials.Load(); n < 2 {
			t.Fatalf("the relay was dialled %d times, want a second tunnel", n)
		}
		// The first attempt of the new tunnel is the settle delay again, and
		// not the 60 s the old one had climbed to.
		rig.ladderStep(DirectSettle)
	})
}

// V33: a direct path that dies without a close is noticed at lastHeard+3 s, the
// channels end with the planned redial, and the view is back on the relay by 3.1 s
// from the stall when the relay standby is alive, with the standby probed once at
// the first missed pong and no relay dial, no state but the one road flip. An
// abrupt close of the direct stream falls back at once.
func TestADeadDirectLinkEndsItsChannelsPlannedAndTheRelayTakesThemWithinFiveSeconds(t *testing.T) {
	t.Run("a stalled path with a live standby", func(t *testing.T) {
		rig := newDirectRig(t, rigOpts{})
		rig.speak(rig.openChat("chat-a"))
		rig.swapToDirect()
		ch := rig.openChat("chat-a")
		if road := rig.speak(ch); road != RoadDirect {
			t.Fatalf("chat-a was carried on %q after the swap, want direct", road)
		}
		direct := rig.latest(RoadDirect)
		relay := rig.tunnel(RoadRelay, 1)
		rig.advanceHealthy(direct, 3*time.Second)
		quiesce()
		nStates := rig.stateCount()
		relayMark := relay.viewer.sent.mark()

		rig.alignOnPong(direct)
		direct.stall()
		stalledAt := rig.fc.Now()

		// Nothing is probed before a pong has been missed.
		rig.advanceFine(900 * time.Millisecond)
		if n := relay.viewer.sent.opsSince(relayMark, OpPing); n != 0 {
			t.Fatalf("the standby was probed %d times before any pong was missed", n)
		}
		// The probe goes out once, at the first missed pong, well before the
		// road is declared dead.
		rig.advanceFine(1600 * time.Millisecond)
		within(t, "the standby probe", func() bool { return relay.viewer.sent.opsSince(relayMark, OpPing) >= 1 })
		if rig.link.Road() != RoadDirect {
			t.Fatalf("the road was declared dead %v into the silence, want after three seconds", rig.fc.Now().Sub(stalledAt))
		}
		rig.advanceFine(600 * time.Millisecond)
		rig.waitRoad(RoadRelay)
		if elapsed := rig.fc.Now().Sub(stalledAt); elapsed > 3100*time.Millisecond {
			t.Fatalf("the view was back on the relay %v after the stall, want within 3.1 s with a live standby", elapsed)
		}
		if n := relay.viewer.sent.opsSince(relayMark, OpPing); n != 1 {
			t.Fatalf("the standby was probed %d times in one outage, want exactly once", n)
		}

		err := rig.endOf(ch)
		if !isPlanned(err) {
			t.Fatalf("the direct channel ended with %v, want a planned redial", err)
		}
		within(t, "the fallback to be notified", func() bool { return rig.stateCount() > nStates })
		quiesce()
		got := rig.statesFrom(nStates)
		if len(got) != 1 || got[0].Kind != Live || got[0].Road != RoadRelay {
			t.Fatalf("the fallback raised %+v, want exactly one Live on the relay (no Reconnecting, no WentOffline)", got)
		}
		if n := rig.relayDials.Load(); n != 1 {
			t.Fatalf("the relay was dialled %d times, want the standby reused", n)
		}
		// The roaming client's redial lands on the standby, with no dial.
		if road := rig.speak(rig.openChat("chat-a")); road != RoadRelay {
			t.Fatalf("chat-a reopened on %q, want the relay", road)
		}
		if rec := rig.lastOpen(); rec.road != RoadRelay || rec.tunnel != 1 {
			t.Fatalf("the reopen reached the host as %+v, want the first relay tunnel", rec)
		}
		if n := rig.relayDials.Load(); n != 1 {
			t.Fatalf("reopening on the standby dialled the relay %d times", n)
		}
		if ended := rig.endedList(); len(ended) != 0 {
			t.Fatalf("OnChannelEnd was told %v, want nothing for a planned redial", ended)
		}
	})

	t.Run("an abrupt close of the direct stream", func(t *testing.T) {
		rig := newDirectRig(t, rigOpts{})
		rig.speak(rig.openChat("chat-a"))
		rig.swapToDirect()
		ch := rig.openChat("chat-a")
		rig.speak(ch)
		relay := rig.tunnel(RoadRelay, 1)
		quiesce()
		nStates := rig.stateCount()
		relayMark := relay.viewer.sent.mark()

		rig.swapToRelay()
		if !isPlanned(rig.endOf(ch)) {
			t.Fatal("an abruptly closed direct stream did not end its channel with the planned redial")
		}
		quiesce()
		if got := rig.statesFrom(nStates); len(got) != 1 || got[0].Kind != Live || got[0].Road != RoadRelay {
			t.Fatalf("the fallback raised %+v, want exactly one Live on the relay", got)
		}
		if n := relay.viewer.sent.opsSince(relayMark, OpPing); n > 1 {
			t.Fatalf("the standby was probed %d times, want at most once", n)
		}
		if n := rig.relayDials.Load(); n != 1 {
			t.Fatalf("the relay was dialled %d times, want the standby reused", n)
		}
		if road := rig.speak(rig.openChat("chat-a")); road != RoadRelay {
			t.Fatalf("chat-a reopened on %q, want the relay", road)
		}
	})
}

// V33: when the relay standby is gone while direct carries, nothing is done about
// it quietly; at the FIRST missed pong the relay is dialled in parallel, so it is
// already up when the direct road is declared dead at lastHeard+3 s, and the
// view is on the relay by 3.1 s with no further dial.
func TestADeadStandbyIsRedialledInParallelAtTheFirstMissedPong(t *testing.T) {
	rig := newDirectRig(t, rigOpts{})
	rig.speak(rig.openChat("chat-a"))
	rig.swapToDirect()
	ch := rig.openChat("chat-a")
	if road := rig.speak(ch); road != RoadDirect {
		t.Fatalf("chat-a was carried on %q after the swap, want direct", road)
	}
	direct := rig.latest(RoadDirect)
	relay := rig.tunnel(RoadRelay, 1)
	rig.advanceHealthy(direct, time.Second)
	quiesce()
	nStates := rig.stateCount()

	// The standby relay tunnel dies while direct carries. The viewer drops it
	// silently: no dial, no state, no notification.
	relay.kill()
	rig.advanceHealthy(direct, 5*time.Second)
	if n := rig.relayDials.Load(); n != 1 {
		t.Fatalf("the relay was redialled (%d dials) while direct carried and was healthy", n)
	}
	if st := rig.link.State(); st.Kind != Live || st.Road != RoadDirect {
		t.Fatalf("State() = %+v after the standby died, want Live on direct", st)
	}
	if got := rig.statesFrom(nStates); len(got) != 0 {
		t.Fatalf("the standby's death raised %+v, want nothing", got)
	}

	// Now the direct path goes silent.
	rig.alignOnPong(direct)
	direct.stall()
	stalledAt := rig.fc.Now()
	rig.advanceFine(900 * time.Millisecond)
	if n := rig.relayDials.Load(); n != 1 {
		t.Fatalf("the relay was dialled (%d) before any pong was missed", n)
	}
	// At the first missed pong the relay is dialled, in parallel, while direct
	// is still the road.
	rig.advanceFine(1600 * time.Millisecond)
	within(t, "the parallel relay dial", func() bool { return rig.relayDials.Load() == 2 })
	if rig.link.Road() != RoadDirect {
		t.Fatalf("the road was declared dead %v into the silence, want after three seconds", rig.fc.Now().Sub(stalledAt))
	}
	rig.advanceFine(600 * time.Millisecond)
	rig.waitRoad(RoadRelay)
	if elapsed := rig.fc.Now().Sub(stalledAt); elapsed > 3100*time.Millisecond {
		t.Fatalf("the view was back on the relay %v after the stall, want about 3.1 s", elapsed)
	}
	if n := rig.relayDials.Load(); n != 2 {
		t.Fatalf("the relay was dialled %d times, want the one parallel dial", n)
	}
	if !isPlanned(rig.endOf(ch)) {
		t.Fatal("the dead direct channel did not end with the planned redial")
	}
	within(t, "the fallback to be notified", func() bool { return rig.stateCount() > nStates })
	quiesce()
	got := rig.statesFrom(nStates)
	if len(got) != 1 || got[0].Kind != Live || got[0].Road != RoadRelay {
		t.Fatalf("the fallback raised %+v, want exactly one Live on the relay (no WentOffline)", got)
	}
	// The roaming client's redial opens on the relay that was dialled in
	// parallel and dials nothing more.
	if road := rig.speak(rig.openChat("chat-a")); road != RoadRelay {
		t.Fatalf("chat-a reopened on %q, want the relay", road)
	}
	if rec := rig.lastOpen(); rec.road != RoadRelay || rec.tunnel != 2 {
		t.Fatalf("the reopen reached the host as %+v, want the second relay tunnel", rec)
	}
	if n := rig.relayDials.Load(); n != 2 {
		t.Fatalf("the relay was dialled %d times in all, want 2", n)
	}
}

// V33 and D7: while the direct link carries and is healthy the relay standby is
// left alone. It is never redialled, never pinged and never sent any frame, even
// across a span longer than every keepalive the relay has had, and even while the
// view is active (a running turn), which on the relay alone pings every 3 s.
func TestTheRelayLinkIsNotRedialledWhileTheDirectLinkCarries(t *testing.T) {
	rig := newDirectRig(t, rigOpts{active: true})
	rig.speak(rig.openChat("chat-a"))
	rig.swapToDirect()
	if road := rig.speak(rig.openChat("chat-a")); road != RoadDirect {
		t.Fatalf("chat-a was carried on %q after the swap, want direct", road)
	}
	direct := rig.latest(RoadDirect)
	relay := rig.tunnel(RoadRelay, 1)
	rig.advanceHealthy(direct, time.Second)
	quiesce()
	relayMark := relay.viewer.sent.mark()
	hostMark := relay.hostTap.mark()
	nStates := rig.stateCount()

	rig.advanceHealthy(direct, 6*time.Minute)

	if n := rig.relayDials.Load(); n != 1 {
		t.Fatalf("the relay was dialled %d times while direct carried, want once in all", n)
	}
	if frames := relay.viewer.sent.since(relayMark); len(frames) != 0 {
		t.Fatalf("the standby sent %v in six minutes on direct, want nothing", frames)
	}
	if frames := relay.hostTap.since(hostMark); len(frames) != 0 {
		t.Fatalf("the host sent %v on the standby in six minutes, want nothing", frames)
	}
	if n := relay.viewer.sent.count(OpPing); n != 0 {
		t.Fatalf("the standby was pinged %d times while direct was healthy", n)
	}
	if relay.viewer.closed.Load() {
		t.Fatal("the standby relay tunnel was closed while direct carried")
	}
	if st := rig.link.State(); st.Kind != Live || st.Road != RoadDirect {
		t.Fatalf("State() = %+v, want Live on direct", st)
	}
	if got := rig.statesFrom(nStates); len(got) != 0 {
		t.Fatalf("six quiet minutes raised %+v", got)
	}
}

// V39: a direct view with no key, scroll or send for IdleDetach closes both roads,
// one close each and nothing sent first, tells OnChannelEnd `idle` once for the
// one channel, sends nothing on the relay while it is idle, cancels the ladder,
// and a key afterwards reattaches on the relay and upgrades again.
func TestAnIdleDirectViewDetachesBothRoadsWithOneCloseAndNothingSentOnTheRelay(t *testing.T) {
	rig := newDirectRig(t, rigOpts{})
	rig.speak(rig.openChat("chat-a"))
	t0 := rig.fc.Now()
	rig.swapToDirect()
	ch := rig.openChat("chat-a")
	if road := rig.speak(ch); road != RoadDirect {
		t.Fatalf("chat-a was carried on %q after the swap, want direct", road)
	}
	direct := rig.latest(RoadDirect)
	relay := rig.tunnel(RoadRelay, 1)
	rig.advanceHealthy(direct, time.Second)
	quiesce()
	relayMark := relay.viewer.sent.mark()

	// Ten minutes less a second of nothing: still live, still direct, nothing on
	// the relay.
	rig.advanceHealthy(direct, t0.Add(IdleDetach-time.Second).Sub(rig.fc.Now()))
	if st := rig.link.State(); st.Kind != Live || st.Road != RoadDirect {
		t.Fatalf("at 9m59s State() = %+v, want Live on direct", st)
	}
	if ended := rig.endedList(); len(ended) != 0 {
		t.Fatalf("a channel ended before the idle time: %v", ended)
	}
	if frames := relay.viewer.sent.since(relayMark); len(frames) != 0 {
		t.Fatalf("the relay carried %v during the idle spell, want nothing", frames)
	}

	// Past ten minutes both roads detach.
	directMark := direct.viewer.sent.mark()
	rig.advanceFine(3 * time.Second)
	within(t, "the view to go idle", func() bool { return rig.link.State().Kind == Idle })
	if st := rig.link.State(); st.Reason != ReasonIdle || st.Road != RoadRelay {
		t.Fatalf("State() = %+v after the detach, want Idle for %q with the road back to relay", st, ReasonIdle)
	}
	within(t, "the relay tunnel to be closed", relay.viewer.closed.Load)
	within(t, "the direct tunnel to be closed", direct.viewer.closed.Load)
	if frames := relay.viewer.sent.since(relayMark); len(frames) != 0 {
		t.Fatalf("the detach put %v on the relay, want one close and nothing sent first", frames)
	}
	for _, op := range []uint8{OpClose, OpReset, OpData, OpOpen, OpSignal} {
		if n := direct.viewer.sent.opsSince(directMark, op); n != 0 {
			t.Fatalf("the detach put %d frames of op %d on the direct tunnel, want one close and nothing sent first", n, op)
		}
	}
	within(t, "OnChannelEnd to be told the idle end", func() bool { return len(rig.endedList()) >= 1 })
	quiesce()
	if ended := rig.endedList(); len(ended) != 1 || ended[0] != "chat-a:"+ReasonIdle {
		t.Fatalf("OnChannelEnd was told %v, want exactly [chat-a:%s]", ended, ReasonIdle)
	}
	err := rig.endOf(ch)
	if why, ok := ReasonOf(err); !ok || why != ReasonIdle || isPlanned(err) {
		t.Fatalf("the direct channel ended with %v, want the spoken reason %q", err, ReasonIdle)
	}
	within(t, "every timer to be gone once idle", func() bool { return rig.fc.BlockedTimers() == 0 })

	// A key reattaches on the relay and the first channel after it upgrades again.
	rig.link.Touch()
	within(t, "the relay to be dialled again", func() bool { return rig.relayDials.Load() == 2 })
	rig.waitState(Live, RoadRelay)
	if road := rig.speak(rig.openChat("chat-a")); road != RoadRelay {
		t.Fatalf("the first channel after the key was carried on %q, want the relay", road)
	}
	if moved := rig.runToNextAttempt(); moved != DirectSettle {
		t.Fatalf("the first attempt after the key came after %v, want the settle delay %v", moved, DirectSettle)
	}
	rig.waitRoad(RoadDirect)
	if n := rig.directDials.Load(); n != 2 {
		t.Fatalf("the direct road was dialled %d times, want a second attempt after the key", n)
	}
}

// K1 (road generation): a channel whose relay open completes AFTER the swap
// committed never stays on the relay. The open is retried on the direct road, the
// channel it hands back is a direct one, and the relay channel it abandoned is
// closed without ever being reported as ended.
func TestAChannelOpenedAsTheSwapCommitsNeverStaysOnTheRelay(t *testing.T) {
	rig := newDirectRig(t, rigOpts{})
	rig.speak(rig.openChat("chat-a"))

	// The host holds the relay's resolution of chat-x, so the viewer's open is in
	// flight on the relay while the swap commits.
	gate := rig.holdNextOpen(RoadRelay, "chat-x")
	type result struct {
		ch  io.ReadWriteCloser
		err error
	}
	done := make(chan result, 1)
	guard.Go("livedoor.test.direct.k1.open", func() {
		ch, err := rig.openChatErr("chat-x")
		done <- result{ch: ch, err: err}
	})
	select {
	case <-gate.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the relay open never reached the host")
	}
	rig.swapToDirect()

	// The open completes on the relay now.
	close(gate.release)
	var res result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the open that straddled the swap never returned")
	}
	if res.err != nil {
		t.Fatalf("the open that straddled the swap failed: %v", res.err)
	}
	if road := rig.speak(res.ch); road != RoadDirect {
		t.Fatalf("the channel that was opening on the relay as the swap committed is carried on %q, want direct", road)
	}
	if got := rig.opensOn(RoadDirect); len(got) == 0 || got[len(got)-1] != "chat-x" {
		t.Fatalf("the direct host resolved %v, want chat-x retried there", got)
	}
	// The abandoned relay channel was closed: one CLOSE for chat-a at the swap, and
	// one for the channel that must not stay.
	relay := rig.tunnel(RoadRelay, 1)
	within(t, "the abandoned relay channel to be closed", func() bool {
		return relay.viewer.sent.count(OpClose)+relay.viewer.sent.count(OpReset) >= 2
	})
	if ended := rig.endedList(); len(ended) != 0 {
		t.Fatalf("OnChannelEnd was told %v, want nothing for a channel that was never published", ended)
	}
	if st := rig.link.State(); st.Kind != Live || st.Road != RoadDirect {
		t.Fatalf("State() = %+v, want Live on direct", st)
	}
}

// K2 (attempt token): a candidate direct tunnel that finishes dialling after the
// view went idle, after its relay tunnel was replaced, after the link was
// revoked or after Close never commits a swap: the road does not change, nothing
// is notified, and the stale candidate's tunnel is closed.
func TestAStaleAttemptCannotCommitAfterIdleOrANewRelayTunnel(t *testing.T) {
	// staleRig is a rig whose first direct dial ignores its context and returns a
	// ready tunnel only when released: a candidate that finishes late. Later
	// dials fail at once, so only that one candidate is ever in play.
	staleRig := func(t *testing.T) (*directRig, func()) {
		rig := newDirectRig(t, rigOpts{})
		release := make(chan struct{})
		var once sync.Once
		releaseFn := func() { once.Do(func() { close(release) }) }
		t.Cleanup(releaseFn)
		rig.setDirect(func(_ context.Context, n int, _ Signaller) (io.ReadWriteCloser, error) {
			if n > 1 {
				return nil, errors.New("no direct path")
			}
			<-release
			return rig.newTunnel(RoadDirect), nil
		})
		rig.speak(rig.openChat("chat-a"))
		rig.runToNextAttempt()
		return rig, releaseFn
	}
	// settled is what must hold once the late candidate has been dealt with.
	settled := func(t *testing.T, rig *directRig) {
		t.Helper()
		within(t, "the late candidate's tunnel to exist", func() bool { return len(rig.tunnelsOn(RoadDirect)) == 1 })
		candidate := rig.tunnelsOn(RoadDirect)[0]
		within(t, "the stale candidate to be closed", candidate.viewer.closed.Load)
		quiesce()
		if rig.link.Road() == RoadDirect {
			t.Fatal("a stale candidate committed the swap")
		}
		for _, st := range rig.statesFrom(0) {
			if st.Road == RoadDirect {
				t.Fatalf("a stale candidate was notified as the road: %+v", rig.statesFrom(0))
			}
		}
		for _, ended := range rig.endedList() {
			if bytes.Contains([]byte(ended), []byte(ReasonRoadChange)) {
				t.Fatalf("a stale candidate swapped a channel: %v", rig.endedList())
			}
		}
	}

	t.Run("after the view went idle", func(t *testing.T) {
		rig, release := staleRig(t)
		rig.fc.Advance(IdleDetach + time.Second)
		within(t, "the view to go idle", func() bool { return rig.link.State().Kind == Idle })
		release()
		settled(t, rig)
		if st := rig.link.State(); st.Kind != Idle {
			t.Fatalf("State() = %+v after a stale candidate, want it still Idle", st)
		}
	})

	t.Run("after a new relay tunnel replaced the old one", func(t *testing.T) {
		rig, release := staleRig(t)
		rig.tunnel(RoadRelay, 1).kill()
		within(t, "the Link to notice its relay tunnel is gone", func() bool { return rig.link.State().Kind != Live })
		ch := rig.askAgainAfterDrop("chat-b")
		if road := rig.speak(ch); road != RoadRelay {
			t.Fatalf("the reopened chat was carried on %q, want the relay", road)
		}
		// How many dials it takes to get a new tunnel (the Link's own redial and a
		// roaming client's open may both dial) is phase 1's business; that there is
		// a new one is the premise of this case.
		if n := rig.relayDials.Load(); n < 2 {
			t.Fatalf("the relay was dialled %d times, want a second tunnel", n)
		}
		release()
		settled(t, rig)
		if st := rig.link.State(); st.Kind != Live || st.Road != RoadRelay {
			t.Fatalf("State() = %+v after a stale candidate, want Live on the relay", st)
		}
		if _, err := ch.Write([]byte("still on the relay\n")); err != nil {
			t.Fatalf("the new tunnel's channel was swapped away by a stale candidate: %v", err)
		}
	})

	t.Run("after Close", func(t *testing.T) {
		rig, release := staleRig(t)
		rig.link.Close()
		release()
		settled(t, rig)
	})

	t.Run("after a revocation", func(t *testing.T) {
		rig, release := staleRig(t)
		rig.relayReg.Revoke(rig.peer.Device)
		within(t, "the view to be revoked", func() bool { return rig.link.State().Kind == Revoked })
		release()
		settled(t, rig)
		if st := rig.link.State(); st.Kind != Revoked {
			t.Fatalf("State() = %+v after a stale candidate, want it still Revoked", st)
		}
	})
}

// K3 (standby probe has a deadline): when the direct road dies together with the
// relay standby (a whole network gone), the standby cannot answer its probe, so at
// the fallback it is retired, ONE relay dial is started and no more, and the
// channels the roaming client redials open on the new tunnel, never on the
// standby that is known unanswered.
func TestAnUnansweredStandbyIsRetiredAtTheFallbackAndOneRelayDialIsStarted(t *testing.T) {
	rig := newDirectRig(t, rigOpts{})
	rig.speak(rig.openChat("chat-a"))
	rig.swapToDirect()
	ch := rig.openChat("chat-a")
	if road := rig.speak(ch); road != RoadDirect {
		t.Fatalf("chat-a was carried on %q after the swap, want direct", road)
	}
	direct := rig.latest(RoadDirect)
	relay1 := rig.tunnel(RoadRelay, 1)
	rig.advanceHealthy(direct, 3*time.Second)
	quiesce()
	nStates := rig.stateCount()
	relayMark := relay1.viewer.sent.mark()

	// Everything goes dark at once.
	rig.alignOnPong(direct)
	direct.stall()
	relay1.stall()
	stalledAt := rig.fc.Now()

	rig.advanceFine(2500 * time.Millisecond)
	within(t, "the standby to be probed", func() bool { return relay1.viewer.sent.opsSince(relayMark, OpPing) >= 1 })
	if n := relay1.viewer.sent.opsSince(relayMark, OpPing); n != 1 {
		t.Fatalf("the standby was probed %d times in one outage, want exactly once", n)
	}
	if rig.link.Road() != RoadDirect {
		t.Fatalf("the road was declared dead %v into the silence, want after three seconds", rig.fc.Now().Sub(stalledAt))
	}

	// The direct road is declared dead; the standby that never answered is
	// retired and exactly one relay dial replaces it.
	rig.advanceFine(600 * time.Millisecond)
	rig.waitRoad(RoadRelay)
	within(t, "the unanswered standby to be retired", relay1.viewer.closed.Load)
	within(t, "the relay to be dialled once", func() bool { return rig.relayDials.Load() == 2 })
	rig.advanceFine(10 * time.Second)
	if n := rig.relayDials.Load(); n != 2 {
		t.Fatalf("the relay was dialled %d times, want the one dial and no second", n)
	}
	if !isPlanned(rig.endOf(ch)) {
		t.Fatal("the dead direct channel did not end with the planned redial")
	}
	rig.waitState(Live, RoadRelay)
	for _, st := range rig.statesFrom(nStates) {
		if st.Kind == WentOffline {
			t.Fatalf("a network that came back raised a went-offline banner: %+v", rig.statesFrom(nStates))
		}
	}

	// The opener never uses the standby that did not answer.
	if road := rig.speak(rig.openChat("chat-a")); road != RoadRelay {
		t.Fatalf("chat-a reopened on %q, want the relay", road)
	}
	if rec := rig.lastOpen(); rec.road != RoadRelay || rec.tunnel != 2 {
		t.Fatalf("the reopen reached the host as %+v, want the NEW relay tunnel, never the retired standby", rec)
	}
	if elapsed := rig.fc.Now().Sub(stalledAt); elapsed < 3*time.Second {
		t.Fatalf("the test measured %v, want a span past the three-second bound", elapsed)
	}
}

// spokenOutcome is everything a person or a caller can see of a channel that the
// host ended with a reason.
type spokenOutcome struct {
	readReason   string
	planned      bool
	ended        []string
	kind         StateKind
	reason       string
	watch, drive bool
	reopenOK     bool
	reopenReason string
}

// settledStates waits until the Link has stopped changing state, which is the
// moment a comparison between two Links is fair.
func settledStates(rig *directRig) {
	for stable := 0; stable < 4; {
		n := rig.stateCount()
		quiesce()
		if rig.stateCount() == n {
			stable++
		} else {
			stable = 0
		}
	}
}

// spokenEnd runs one scenario: a chat is open on the carrying road (the relay for
// a link with no direct config, direct once swapped otherwise), the host speaks
// reason on that road's channels and leaves the tunnel up, and the result is what
// was seen.
func spokenEnd(t *testing.T, onDirect bool, reason string) (*directRig, spokenOutcome) {
	t.Helper()
	rig := newDirectRig(t, rigOpts{noDirect: !onDirect})
	ch := rig.openChat("chat-a")
	rig.speak(ch)
	reg := rig.relayReg
	if onDirect {
		rig.swapToDirect()
		ch = rig.openChat("chat-a")
		if road := rig.speak(ch); road != RoadDirect {
			t.Fatalf("the chat was carried on %q, want direct", road)
		}
		reg = rig.directReg
	}
	hostSays(t, reg, rig.peer.Device, reason)
	err := rig.endOf(ch)
	var out spokenOutcome
	out.readReason, _ = ReasonOf(err)
	out.planned = isPlanned(err)
	within(t, "OnChannelEnd to be told", func() bool { return len(rig.endedList()) >= 1 })
	settledStates(rig)
	out.ended = rig.endedList()
	st := rig.link.State()
	out.kind, out.reason, out.watch, out.drive = st.Kind, st.Reason, st.Permit.Watch, st.Permit.Drive
	reopened, rerr := rig.askAgainAfterDropErr("chat-a")
	out.reopenOK = rerr == nil
	out.reopenReason, _ = ReasonOf(rerr)
	if reopened != nil {
		_ = reopened.Close()
	}
	return rig, out
}

// E: a reason the HOST speaks on a direct channel takes the same handling as on a
// relay channel. Each reason is run on a link with no direct config (the phase 1
// behaviour, the oracle) and on a link carried by the direct road, and what the
// person or caller can see must agree. A spoken removal also revokes the whole
// link, closes the nested one and never schedules another attempt.
func TestAHostSpokenReasonOnADirectChannelTakesTheSameHandlingAsOnARelayChannel(t *testing.T) {
	for _, reason := range []string{ReasonRemoved, ReasonPermitChanged, ReasonBudget, ReasonLagging} {
		reason := reason
		t.Run(reason, func(t *testing.T) {
			_, want := spokenEnd(t, false, reason)
			rig, got := spokenEnd(t, true, reason)
			if want.readReason != reason {
				t.Fatalf("the oracle's channel ended for %q, want %q: the scenario is wrong", want.readReason, reason)
			}
			if got.readReason != reason || got.planned {
				t.Fatalf("the direct channel ended for %q (planned %v), want the spoken %q", got.readReason, got.planned, reason)
			}
			if len(got.ended) != 1 || got.ended[0] != "chat-a:"+reason {
				t.Fatalf("OnChannelEnd was told %v, want exactly [chat-a:%s]", got.ended, reason)
			}
			if got.kind != want.kind || got.reason != want.reason || got.watch != want.watch || got.drive != want.drive {
				t.Fatalf("the direct link settled as kind=%v reason=%q watch=%v drive=%v; the relay link, given the same words, as kind=%v reason=%q watch=%v drive=%v",
					got.kind, got.reason, got.watch, got.drive, want.kind, want.reason, want.watch, want.drive)
			}
			if got.reopenOK != want.reopenOK || got.reopenReason != want.reopenReason {
				t.Fatalf("reopening after %q: direct link ok=%v reason=%q; relay link ok=%v reason=%q", reason, got.reopenOK, got.reopenReason, want.reopenOK, want.reopenReason)
			}
			if reason == ReasonLagging && rig.link.Road() != RoadDirect {
				t.Fatalf("a lagging channel moved the road to %q; the tunnel stays and so does the road", rig.link.Road())
			}
			if reason != ReasonRemoved {
				return
			}
			// A removal ends it all: the link is revoked for good, nothing is
			// scheduled and no road is ever dialled again.
			if got.kind != Revoked || got.reason != ReasonRemoved {
				t.Fatalf("State() = kind %v reason %q after a removal, want Revoked for %q", got.kind, got.reason, ReasonRemoved)
			}
			dials := rig.relayDials.Load() + rig.directDials.Load()
			rig.fc.Advance(2 * time.Hour)
			quiesce()
			if after := rig.relayDials.Load() + rig.directDials.Load(); after != dials {
				t.Fatalf("a revoked link dialled %d more times in two hours", after-dials)
			}
			if n := rig.fc.BlockedTimers(); n != 0 {
				t.Fatalf("a revoked link still holds %d timers", n)
			}
			if st := rig.link.State(); st.Kind != Revoked {
				t.Fatalf("State() = %+v two hours after a removal, want Revoked", st)
			}
		})
	}
}

// J1: with no DirectConfig the Link behaves as it did before the direct road
// existed even when the host offers it: no signalling frame, no attempt timer,
// no ping, the road word is the relay's, and the only timer a view holds is the
// idle detach.
func TestWithNoDirectConfigTheLinkNeverSignalsNeverTimersAndReportsTheRelayRoad(t *testing.T) {
	rig := newDirectRig(t, rigOpts{noDirect: true})
	chA := rig.openChat("chat-a")
	chB := rig.openChat("chat-b")
	if road := rig.speak(chA); road != RoadRelay {
		t.Fatalf("chat-a was carried on %q, want the relay", road)
	}
	rig.speak(chB)
	relay := rig.tunnel(RoadRelay, 1)
	if st := rig.link.State(); st.Kind != Live || st.Road != RoadRelay || rig.link.Road() != RoadRelay {
		t.Fatalf("State() = %+v, Road() = %q, want Live on the relay", st, rig.link.Road())
	}
	within(t, "the idle timer to be the only timer", func() bool { return rig.fc.BlockedTimers() == 1 })

	rig.fc.Advance(5 * time.Minute)
	quiesce()
	for name, op := range map[string]uint8{"signal": OpSignal, "ping": OpPing, "close": OpClose, "reset": OpReset} {
		if n := relay.viewer.sent.count(op); n != 0 {
			t.Fatalf("a view with no direct config sent %d %s frames in five quiet minutes", n, name)
		}
	}
	if n := rig.fc.BlockedTimers(); n != 1 {
		t.Fatalf("a view with no direct config holds %d timers, want only the idle detach", n)
	}
	if n := rig.host.offers(); n != 0 {
		t.Fatalf("the host was offered a direct road %d times by a link that has none", n)
	}
	if n := rig.relayDials.Load(); n != 1 {
		t.Fatalf("the relay was dialled %d times, want once", n)
	}
	for _, st := range rig.statesFrom(0) {
		if st.Road == RoadDirect {
			t.Fatalf("a link with no direct config reported %+v", st)
		}
	}
	if ended := rig.endedList(); len(ended) != 0 {
		t.Fatalf("channels ended: %v", ended)
	}
}

// A (the public surface): the signalling a direct-road implementation needs rides
// the relay tunnel. SendSignal puts one OpSignal frame on channel 0 of the relay
// tunnel and fails when there is none; the host's frames come back through
// LinkConfig.OnSignal; and the Signaller a direct dial is handed is the same road.
func TestSignalsRideTheRelayTunnelBothWaysAndFailWithNoTunnel(t *testing.T) {
	rig := newDirectRig(t, rigOpts{})
	if err := rig.link.SendSignal([]byte("x")); err == nil {
		t.Fatal("SendSignal succeeded on a link with no relay tunnel")
	}

	offer, err := EncodeSignal(Signal{T: SignalOffer, ID: "attempt-1", SDP: "v=0"})
	if err != nil {
		t.Fatal(err)
	}
	cand, err := EncodeSignal(Signal{T: SignalCandidate, ID: "attempt-1", C: "candidate:1"})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := EncodeSignal(Signal{T: SignalAnswer, ID: "attempt-1", SDP: "v=0 answered"})
	if err != nil {
		t.Fatal(err)
	}
	sent := make(chan error, 1)
	rig.setDirect(func(ctx context.Context, _ int, s Signaller) (io.ReadWriteCloser, error) {
		sent <- s.Send(offer)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	rig.speak(rig.openChat("chat-a"))
	rig.waitAttemptArmed(DirectSettle)
	rig.fc.Advance(DirectSettle)
	select {
	case err := <-sent:
		if err != nil {
			t.Fatalf("the direct dial's Signaller failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the direct dial never sent its offer")
	}
	within(t, "the host to be offered", func() bool { return rig.host.offers() == 1 })
	rig.host.mu.Lock()
	gotOffer := append([]byte(nil), rig.host.payloads[0]...)
	hostSide := rig.host.signals[0]
	rig.host.mu.Unlock()
	if !bytes.Equal(gotOffer, offer) {
		t.Fatalf("the host was offered %q, want %q", gotOffer, offer)
	}

	// The host's answer reaches OnSignal.
	if err := hostSide.Send(answer); err != nil {
		t.Fatal(err)
	}
	within(t, "the answer to reach OnSignal", func() bool { return len(rig.signalsGot()) == 1 })
	if got := rig.signalsGot()[0]; !bytes.Equal(got, answer) {
		t.Fatalf("OnSignal was handed %q, want %q", got, answer)
	}

	// The link's own SendSignal reaches the host's session for that attempt.
	if err := rig.link.SendSignal(cand); err != nil {
		t.Fatalf("SendSignal failed with a relay tunnel up: %v", err)
	}
	within(t, "the candidate to reach the host's session", func() bool { return len(rig.host.session(0).deliveries()) == 1 })
	if got := rig.host.session(0).deliveries()[0]; !bytes.Equal(got, cand) {
		t.Fatalf("the host's session was delivered %q, want %q", got, cand)
	}

	// Both frames crossed the relay tunnel, on channel 0.
	relay := rig.tunnel(RoadRelay, 1)
	var signals []tapFrame
	for _, fr := range relay.viewer.sent.since(0) {
		if fr.op == OpSignal {
			signals = append(signals, fr)
		}
	}
	if len(signals) != 2 || signals[0].channel != 0 || signals[1].channel != 0 {
		t.Fatalf("the relay tunnel carried %+v, want the offer and the candidate on channel 0", signals)
	}
}
