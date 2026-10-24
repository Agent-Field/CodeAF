package dirwatch

import (
	"context"
	"sync"
	"testing"
	"time"
)

// redialWait is the wait after a good socket ends: the first rung, RetryBase,
// times the fixed jitter of 0.4.
const redialWait = 400 * time.Millisecond

// hold is the test's stand-in for what a socket names.
type hold struct {
	cell  string
	fence uint64
}

// holderRig is a holder on a fake clock dialling scripted sockets, and the
// holds each dial named.
type holderRig struct {
	t       *testing.T
	clock   *fakeClock
	holder  *Holder[hold]
	streams chan *fakeStream
	mu      sync.Mutex
	dialled [][]hold
	vouch   bool // whether the next sockets vouch
}

func newHolderRig(t *testing.T, max int) *holderRig {
	t.Helper()
	r := &holderRig{t: t, clock: newFakeClock(), streams: make(chan *fakeStream, 8), vouch: true}
	dial := func(_ context.Context, holds []hold) (Stream, error) {
		s := newFakeStream()
		r.mu.Lock()
		s.vouch = r.vouch
		r.dialled = append(r.dialled, holds)
		r.mu.Unlock()
		r.streams <- s
		return s, nil
	}
	r.holder = newHolder("k", max, dial, r.clock, func() float64 { return 0.4 })
	return r
}

func (r *holderRig) socket() *fakeStream {
	r.t.Helper()
	select {
	case s := <-r.streams:
		return s
	case <-time.After(5 * time.Second):
		r.t.Fatal("the holder never dialled")
		return nil
	}
}

func (r *holderRig) dials() [][]hold {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]hold(nil), r.dialled...)
}

// speak makes the socket say something and waits until the holder believes it.
func (r *holderRig) speak(s *fakeStream, w *Holding[hold]) {
	r.t.Helper()
	s.send(Frame{Version: 1}, nil)
	for deadline := time.Now().Add(5 * time.Second); !w.feed.snapshot().Up; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			r.t.Fatal("the holder was never told the socket is up")
		}
	}
}

// A hold is healthy only while every part of the proof stands; in each other
// case the holder must report it is not, so that the heartbeats go on.
func TestHolderBeatsWhileSocketDown(t *testing.T) {
	r := newHolderRig(t, 1)
	mine := hold{"c1", 7}
	w := r.holder.Take(mine)
	defer w.Release()
	if w.Healthy() {
		t.Fatal("healthy before the socket was ever open")
	}
	s := r.socket()
	if w.Healthy() {
		t.Fatal("healthy before the socket spoke")
	}
	r.speak(s, w)
	if !w.Healthy() {
		t.Fatal("not healthy with the socket open, vouching, naming the hold and just heard from")
	}

	other := r.holder.Take(hold{"c2", 1}) // past the cap of one: never named
	defer other.Release()
	if other.Healthy() {
		t.Fatal("a hold the socket was not dialled with, and past the cap, is healthy")
	}

	r.clock.mu.Lock()
	r.clock.now = r.clock.now.Add(DeadAfter + time.Second)
	r.clock.mu.Unlock()
	if w.Healthy() {
		t.Fatal("healthy although the last sign of life is older than DeadAfter")
	}
}

func TestHolderIsNotHealthyWithoutTheVouchingWord(t *testing.T) {
	r := newHolderRig(t, 4)
	r.vouch = false
	w := r.holder.Take(hold{"c1", 7})
	defer w.Release()
	r.speak(r.socket(), w)
	if w.Healthy() {
		t.Fatal("healthy on a socket whose server did not say it vouches")
	}
}

func TestHolderIsNotHealthyWhenTheSocketDrops(t *testing.T) {
	r := newHolderRig(t, 4)
	w := r.holder.Take(hold{"c1", 7})
	defer w.Release()
	s := r.socket()
	r.speak(s, w)
	s.send(Frame{}, context.Canceled)
	for deadline := time.Now().Add(5 * time.Second); w.feed.snapshot().Up; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the holder never noticed the socket failed")
		}
	}
	if w.Healthy() {
		t.Fatal("healthy after the socket failed")
	}
}

func TestHolderIsNotHealthyForAnotherFence(t *testing.T) {
	r := newHolderRig(t, 4)
	w := r.holder.Take(hold{"c1", 7})
	defer w.Release()
	r.speak(r.socket(), w)
	stale := &Holding[hold]{holder: r.holder, feed: w.feed, hold: hold{"c1", 6}}
	if stale.Healthy() {
		t.Fatal("a socket that named fence 7 is healthy for fence 6")
	}
}

// What the socket names is fixed at the dial, so a hold that changes the set
// ends the socket and dials again naming the new set; giving a hold up does too.
func TestHolderRedialsWhenTheHoldSetChanges(t *testing.T) {
	r := newHolderRig(t, 4)
	a := r.holder.Take(hold{"c1", 1})
	defer a.Release()
	s := r.socket()
	r.speak(s, a)

	b := r.holder.Take(hold{"c2", 2})
	r.clock.tick(redialWait) // the old socket is ended; the new dial waits the ladder's first, jittered rung
	s2 := r.socket()
	select {
	case <-s.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the old socket was kept")
	}
	r.speak(s2, a) // a socket that worked starts the ladder over
	if got := r.dials(); len(got) != 2 || len(got[1]) != 2 || got[1][0] != (hold{"c1", 1}) || got[1][1] != (hold{"c2", 2}) {
		t.Fatalf("dials = %v, want the second to name both holds", got)
	}

	b.Release()
	r.clock.tick(redialWait)
	r.socket()
	if got := r.dials(); len(got) != 3 || len(got[2]) != 1 || got[2][0] != (hold{"c1", 1}) {
		t.Fatalf("dials = %v, want the third to name the remaining hold", got)
	}
}

// Two holders of the same hold change nothing about what is named.
func TestHolderDoesNotRedialForARepeatedHold(t *testing.T) {
	r := newHolderRig(t, 4)
	a := r.holder.Take(hold{"c1", 1})
	defer a.Release()
	s := r.socket()
	r.speak(s, a)
	b := r.holder.Take(hold{"c1", 1})
	b.Release()
	select {
	case <-s.closed:
		t.Fatal("the socket was ended for a hold it already named")
	case <-time.After(50 * time.Millisecond):
	}
}
