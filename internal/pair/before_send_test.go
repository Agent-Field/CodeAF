package pair

// The computer that shows a code shares its identity only in the answer to a
// device a person approved. A grant's BeforeSend, which ends solo in every real
// door, runs then and only then: at most once, before the answer is written,
// and never for a code that was taken back, ran out, was declined or was used
// up by a wrong code.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/pairbox"
)

// sendCount is a BeforeSend that counts its calls and answers fail.
type sendCount struct {
	n    atomic.Int32
	fail error
}

func (c *sendCount) hook() error { c.n.Add(1); return c.fail }

func (c *sendCount) grant(r *chatRig) Grant { return Grant{Identity: r.a, BeforeSend: c.hook} }

func (c *sendCount) want(t *testing.T, n int32, when string) {
	t.Helper()
	if got := c.n.Load(); got != n {
		t.Fatalf("%s: BeforeSend ran %d times, want %d", when, got, n)
	}
}

func TestBeforeSendWaitsForTheAnswer(t *testing.T) {
	t.Run("a no", func(t *testing.T) {
		r := newChatRig(t)
		var c sendCount
		a := newScreen()
		a.answer = func(context.Context, string, string) bool { return false }
		offer := startOffer(t, r, bounded(t, time.Minute), c.grant(r), a)
		code := a.nextCode(t)
		if _, err := r.join(bounded(t, 20*time.Second), r.homeB, code.Shown(), newScreen()); !errors.Is(err, ErrRefused) {
			t.Fatalf("B was told %v, want refused", err)
		}
		if end := offer.wait(t); !errors.Is(end.err, ErrRefused) {
			t.Fatalf("A ended with %v, want refused", end.err)
		}
		c.want(t, 0, "after a no")
	})

	t.Run("a wrong code, then a yes", func(t *testing.T) {
		r := newChatRig(t)
		var c sendCount
		a := newScreen()
		offer := startOffer(t, r, bounded(t, time.Minute), c.grant(r), a)
		first := a.nextCode(t)
		if _, err := r.join(bounded(t, 10*time.Second), t.TempDir(), typedWith(first, otherDigits(first)), newCountingJoin()); !errors.Is(err, ErrCodeDidNotWork) {
			t.Fatalf("the wrong code was told %v", err)
		}
		second := a.nextCode(t)
		c.want(t, 0, "after a code was used up by a wrong one")
		if _, err := r.join(bounded(t, 20*time.Second), r.homeB, second.Shown(), newScreen()); err != nil {
			t.Fatalf("B was told %v", err)
		}
		if end := offer.wait(t); end.err != nil {
			t.Fatalf("A ended with %v", end.err)
		}
		c.want(t, 1, "after the yes on the next code")
	})

	t.Run("the code runs out", func(t *testing.T) {
		r, clock := clockRig(t)
		var c sendCount
		a := newScreen()
		offer := startOffer(t, r, bounded(t, time.Minute), c.grant(r), a)
		code := a.nextCode(t)
		clock.Advance(pairbox.DefaultLimits.TTL + time.Second)
		// A device that arrives too late finds nothing, as TestUnknownNameplate says.
		_, err := r.join(bounded(t, 10*time.Second), r.homeB, code.Shown(), newCountingJoin())
		wantNothingWaiting(t, err, code.Plate())
		if end := offer.wait(t); !errors.Is(end.err, ErrDidNotFinish) {
			t.Fatalf("A ended with %v, want the pairing did not finish", end.err)
		}
		c.want(t, 0, "after the code ran out")
	})

	t.Run("the code is taken back", func(t *testing.T) {
		r := newChatRig(t)
		var c sendCount
		a := newScreen()
		offer := startOffer(t, r, bounded(t, time.Minute), c.grant(r), a)
		a.nextCode(t)
		offer.stop()
		if end := offer.wait(t); !errors.Is(end.err, context.Canceled) {
			t.Fatalf("A ended with %v, want cancelled", end.err)
		}
		c.want(t, 0, "after the code was taken back")
	})

	t.Run("a yes that came after the code was taken back", func(t *testing.T) {
		r, hostile := hostileRig(t)
		var c sendCount
		parent, takeBack := context.WithCancel(context.Background())
		t.Cleanup(takeBack)
		a := newScreen()
		a.answer = func(context.Context, string, string) bool { takeBack(); return true }
		offer := startOffer(t, r, parent, c.grant(r), a)
		code := a.nextCode(t)
		if _, err := r.join(bounded(t, 20*time.Second), r.homeB, code.Shown(), newScreen()); err == nil {
			t.Fatal("B was paired by a code that was taken back")
		}
		if end := offer.wait(t); !errors.Is(end.err, context.Canceled) {
			t.Fatalf("A ended with %v, want cancelled", end.err)
		}
		c.want(t, 0, "after a yes that the cancel overtook")
		if wrote := len(hostile.wroteBy(pairbox.SideA)); wrote != 1 {
			t.Fatalf("A wrote %d messages, want only message 2 and no answer", wrote)
		}
		untouched(t, r.homeB)
	})

	t.Run("a yes", func(t *testing.T) {
		r, hostile := hostileRig(t)
		var c sendCount
		atAnswer := int32(-1)
		hostile.on(4, func(h *hostileBox, w write) (int, error) {
			atAnswer = c.n.Load()
			return h.storeAndRelease(w)
		})
		a := newScreen()
		offer := startOffer(t, r, bounded(t, time.Minute), c.grant(r), a)
		code := a.nextCode(t)
		if _, err := r.join(bounded(t, 20*time.Second), r.homeB, code.Shown(), newScreen()); err != nil {
			t.Fatalf("B was told %v", err)
		}
		if end := offer.wait(t); end.err != nil {
			t.Fatalf("A ended with %v", end.err)
		}
		if atAnswer != 1 {
			t.Fatalf("BeforeSend had run %d times when the answer was written, want 1", atAnswer)
		}
		c.want(t, 1, "after the pairing")
	})

	t.Run("a yes whose word is lost", func(t *testing.T) {
		r, hostile := hostileRig(t)
		hostile.on(5, swallow)
		var c sendCount
		a := newScreen()
		offer := startOffer(t, r, bounded(t, refuseWithin), c.grant(r), a)
		code := a.nextCode(t)
		if _, err := r.join(bounded(t, 20*time.Second), r.homeB, code.Shown(), newScreen()); err != nil {
			t.Fatalf("B was told %v", err)
		}
		wantNotConfirmed(t, offer.wait(t), "laptop")
		c.want(t, 1, "once the identity had left")
	})
}

// A BEFORESEND THAT FAILS SENDS NO IDENTITY. The joining device hears no and
// keeps nothing; the computer that showed the code says why in its own words,
// which are not that a person declined.
func TestBeforeSendFailureAnswersNo(t *testing.T) {
	r, hostile := hostileRig(t)
	stuck := errors.New("could not record that this computer now shares its chats")
	c := sendCount{fail: stuck}
	a := newScreen()
	offer := startOffer(t, r, bounded(t, time.Minute), c.grant(r), a)
	code := a.nextCode(t)
	if _, err := r.join(bounded(t, 20*time.Second), r.homeB, code.Shown(), newScreen()); !errors.Is(err, ErrRefused) {
		t.Fatalf("B was told %v, want refused", err)
	}
	end := offer.wait(t)
	if !errors.Is(end.err, stuck) || errors.Is(end.err, ErrRefused) {
		t.Fatalf("A ended with %v, want the BeforeSend failure and not a refusal", end.err)
	}
	if wrote := len(hostile.wroteBy(pairbox.SideB)); wrote != 3 {
		t.Fatalf("B wrote %d messages, want the word that it read the no", wrote)
	}
	c.want(t, 1, "for the one yes")
	untouched(t, r.homeB)
}

// BEFORESEND NEVER TRAVELS: it is this computer's own step, and a grant read off
// the wire has none.
func TestBeforeSendNeverTravels(t *testing.T) {
	r := newChatRig(t)
	got := roundTrip(t, Grant{Identity: r.a, BeforeSend: func() error { return nil }})
	if got.BeforeSend != nil {
		t.Fatal("a grant read off the wire carried a BeforeSend")
	}
}
