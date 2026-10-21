package dirwatch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeClock is a clock the test moves by hand. A timer fires only when
// Advance carries the clock past it, so no test waits on real time.
type fakeClock struct {
	mu      sync.Mutex
	cond    *sync.Cond
	now     time.Time
	pending []*fakeTimer
}

type fakeTimer struct {
	clock *fakeClock
	at    time.Time
	ch    chan time.Time
}

func newFakeClock() *fakeClock {
	c := &fakeClock{now: time.Unix(1_000_000, 0)}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, at: c.now.Add(d), ch: make(chan time.Time, 1)}
	c.pending = append(c.pending, t)
	c.cond.Broadcast()
	return t
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	t.clock.drop(t)
}

// drop removes a timer; the caller holds the lock.
func (c *fakeClock) drop(t *fakeTimer) {
	for i, p := range c.pending {
		if p == t {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			break
		}
	}
	c.cond.Broadcast()
}

// await blocks until the feed is at rest on exactly one timer set for span.
// That is the only moment moving the clock is meaningful: a timer set a moment
// too late would otherwise be jumped over.
func (c *fakeClock) await(span time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.pending) != 1 || !c.pending[0].at.Equal(c.now.Add(span)) {
		c.cond.Wait()
	}
}

// advance waits for the timer of the given span, moves the clock by `by`, and
// fires every timer that is now due.
func (c *fakeClock) advance(span, by time.Duration) {
	c.await(span)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(by)
	for _, t := range append([]*fakeTimer(nil), c.pending...) {
		if !t.at.After(c.now) {
			t.ch <- c.now
			c.drop(t)
		}
	}
}

// tick lets one timer of the given span run out exactly.
func (c *fakeClock) tick(span time.Duration) { c.advance(span, span) }

// fakeStream is one socket the test speaks and listens on.
type fakeStream struct {
	in     chan reply
	pings  chan struct{}
	closed chan struct{}
	once   sync.Once
	vouch  bool // what Vouching answers
}

func newFakeStream() *fakeStream {
	return &fakeStream{in: make(chan reply), pings: make(chan struct{}, 8), closed: make(chan struct{})}
}

func (s *fakeStream) Next(ctx context.Context) (Frame, error) {
	select {
	case r := <-s.in:
		return r.frame, r.err
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case <-s.closed:
		return Frame{}, errors.New("closed")
	}
}

func (s *fakeStream) Ping(context.Context) error { s.pings <- struct{}{}; return nil }
func (s *fakeStream) Vouching() bool             { return s.vouch }
func (s *fakeStream) Close()                     { s.once.Do(func() { close(s.closed) }) }

// send delivers one frame, or fails the read with err.
func (s *fakeStream) send(f Frame, err error) { s.in <- reply{f, err} }

// rig is a feed on a fake clock dialling scripted sockets.
type rig struct {
	t       *testing.T
	clock   *fakeClock
	feed    *Feed
	sub     *sub
	dials   chan time.Time
	streams chan *fakeStream
	outcome func() (Stream, error)
}

// newRig starts a feed whose every dial runs script, with jitter fixed at 0.4, a fraction that keeps every wait distinct from the keepalive's.
func newRig(t *testing.T, script func(n int) (*fakeStream, error)) *rig {
	t.Helper()
	r := &rig{t: t, clock: newFakeClock(), dials: make(chan time.Time, 64)}
	n := 0
	dial := func(ctx context.Context) (Stream, error) {
		r.dials <- r.clock.Now()
		n++
		s, err := script(n)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	r.feed = newFeed(dial, r.clock, func() float64 { return 0.4 })
	r.sub = r.feed.follow("k")
	r.feed.start()
	t.Cleanup(func() { r.feed.stop() })
	return r
}

// sockets is a script that hands out one new socket per dial and lists them.
func sockets(list chan *fakeStream) func(int) (*fakeStream, error) {
	return func(int) (*fakeStream, error) {
		s := newFakeStream()
		list <- s
		return s, nil
	}
}

// changed waits for the follower's signal.
func (r *rig) changed() {
	r.t.Helper()
	select {
	case <-r.sub.Changes():
	case <-time.After(5 * time.Second):
		r.t.Fatal("the follower was never told of a change")
	}
}

func (r *rig) dialAt() time.Time {
	r.t.Helper()
	select {
	case at := <-r.dials:
		return at
	case <-time.After(5 * time.Second):
		r.t.Fatal("the feed never dialled")
		return time.Time{}
	}
}

func (r *rig) noDial() {
	r.t.Helper()
	select {
	case <-r.dials:
		r.t.Fatal("the feed dialled when it should have waited")
	default:
	}
}

func TestAFrameBringsTheSocketUpWithItsVersion(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	s := <-list
	s.send(Frame{Version: 7}, nil)
	r.changed()
	if got := r.sub.State(); !got.Up || got.Version != 7 {
		t.Fatalf("state after a frame was %+v, want up at 7", got)
	}
}

func TestAnIdleSocketIsNotBelievedUp(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	<-list
	r.clock.await(KeepEvery)
	if r.sub.State().Up {
		t.Fatal("a socket that has said nothing was called up")
	}
}

func TestABurstOfFramesIsOneSignalAndTheNewestVersion(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	s := <-list
	for v := uint64(1); v <= 50; v++ {
		s.send(Frame{Version: v}, nil)
	}
	// The feed has handled every frame once it has taken two more: the reader
	// hands one over only when the loop is back waiting.
	s.send(Frame{Pong: true}, nil)
	s.send(Frame{Pong: true}, nil)
	if got := r.sub.State().Version; got != 50 {
		t.Fatalf("version after fifty frames was %d", got)
	}
	if n := len(r.sub.changes); n != 1 {
		t.Fatalf("%d signals waiting, want the burst to coalesce to 1", n)
	}
}

func TestADroppedSocketComesBackWithJitteredBackoffAndResetsOnSpeech(t *testing.T) {
	list := make(chan *fakeStream, 16)
	r := newRig(t, sockets(list))
	first := r.dialAt()
	(<-list).send(Frame{}, errors.New("reset"))
	// The jitter is 0.4, so the waits are 0.4 of a ceiling that doubles from 1 s to the 60 s cap.
	want := []time.Duration{400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond, 3200 * time.Millisecond, 6400 * time.Millisecond, 12800 * time.Millisecond, 24 * time.Second, 24 * time.Second}
	last := first
	for _, w := range want {
		r.clock.await(w)
		r.noDial()
		r.clock.tick(w)
		at := r.dialAt()
		if at.Sub(last) != w {
			t.Fatalf("redial came %v after the drop, want %v", at.Sub(last), w)
		}
		last = at
		(<-list).send(Frame{}, errors.New("reset"))
	}
	// A socket that speaks puts the ladder back to the bottom.
	r.clock.tick(24 * time.Second)
	r.dialAt()
	speaker := <-list
	speaker.send(Frame{Version: 1}, nil)
	r.changed()
	// The next drop waits from the bottom rung again: 0.4 of one second.
	speaker.send(Frame{}, errors.New("reset"))
	r.clock.tick(400 * time.Millisecond)
	r.dialAt()
}

func TestARelayWithoutTheRouteIsAskedAgainOnlyOncePerHour(t *testing.T) {
	r := newRig(t, func(int) (*fakeStream, error) { return nil, ErrNoRoute })
	r.dialAt()
	for i := 0; i < 3; i++ {
		r.clock.await(ProbeEvery)
		r.noDial()
		r.clock.tick(ProbeEvery)
		r.dialAt()
	}
	if r.sub.State().Up {
		t.Fatal("a relay with no route was called up")
	}
}

func TestARevokedOrRotatedSocketIsNotReconnected(t *testing.T) {
	for _, refusal := range []error{ErrRevoked, ErrRotated} {
		list := make(chan *fakeStream, 4)
		r := newRig(t, sockets(list))
		r.dialAt()
		s := <-list
		s.send(Frame{Version: 1}, nil)
		r.changed()
		s.send(Frame{}, refusal)
		r.changed()
		<-r.feed.done
		if r.sub.State().Up {
			t.Fatalf("%v left the socket up", refusal)
		}
		if got := r.sub.State().Refused; got != refusal {
			t.Fatalf("Refused = %v, want %v", got, refusal)
		}
		r.noDial()
	}
}

func TestADialRefusedAsRevokedStopsTheFeed(t *testing.T) {
	r := newRig(t, func(int) (*fakeStream, error) { return nil, ErrRevoked })
	r.dialAt()
	<-r.feed.done
	r.noDial()
	if r.sub.State().Refused != ErrRevoked {
		t.Fatal("a dial refused as revoked did not reach the followers")
	}
}

func TestAQuietSocketIsPingedAndStaysUpWhenItAnswers(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	r.dialAt()
	s := <-list
	for i := 0; i < 5; i++ {
		r.clock.tick(KeepEvery)
		<-s.pings
		s.send(Frame{Pong: true}, nil)
	}
	r.noDial()
}

func TestAMissedPongWithinTheKeepaliveIsADeadSocket(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	r.dialAt()
	s := <-list
	s.send(Frame{Version: 1}, nil)
	r.changed()
	r.clock.tick(KeepEvery)
	<-s.pings
	r.clock.tick(PongWithin)
	<-s.closed
	r.changed()
	if r.sub.State().Up {
		t.Fatal("a socket that never answered was left up")
	}
	r.clock.tick(RetryBase * 2 / 5)
	r.dialAt()
}

func TestAWakeAfterALongSleepIsADeadSocketWithoutWaitingForAPong(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	r.dialAt()
	s := <-list
	s.send(Frame{Version: 1}, nil)
	r.changed()
	r.clock.advance(KeepEvery, 3*KeepEvery) // the wall clock jumped: the machine slept
	<-s.closed
	if len(s.pings) != 0 {
		t.Fatal("a socket that slept was pinged rather than dropped")
	}
}

func TestAProbeAsksAtOnceAndDropsASocketThatDoesNotAnswer(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	r.dialAt()
	s := <-list
	s.send(Frame{Version: 1}, nil)
	r.changed()
	r.sub.Probe()
	<-s.pings
	r.clock.tick(PongWithin)
	<-s.closed
}

func TestOneSocketServesEveryFollowerAndTheLastToLeaveClosesIt(t *testing.T) {
	list := make(chan *fakeStream, 4)
	clock := newFakeClock()
	dials := 0
	build := func() *Feed {
		return newFeed(func(context.Context) (Stream, error) {
			dials++
			s := newFakeStream()
			list <- s
			return s, nil
		}, clock, func() float64 { return 0 })
	}
	a := follow("one-identity", build)
	b := follow("one-identity", build)
	s := <-list
	s.send(Frame{Version: 3}, nil)
	<-a.Changes()
	<-b.Changes()
	a.Close()
	select {
	case <-s.closed:
		t.Fatal("the socket closed while a follower was left")
	default:
	}
	b.Close()
	<-s.closed
	if dials != 1 {
		t.Fatalf("two followers dialled %d times, want 1", dials)
	}
	c := follow("one-identity", build)
	defer c.Close()
	<-list
	if dials != 2 {
		t.Fatalf("a follower after everyone left dialled %d times in all, want 2", dials)
	}
}

func TestRedialsAtOnceOnSilentClose(t *testing.T) {
	list := make(chan *fakeStream, 8)
	r := newRig(t, sockets(list))
	r.dialAt()
	(<-list).send(Frame{}, errors.New("reset"))
	// An ordinary drop climbs the ladder: 0.4 of the first rung.
	r.clock.tick(400 * time.Millisecond)
	r.dialAt()
	// The relay saying it stopped hearing us is answered with a dial and no
	// wait, so no timer stands between the close and the dial.
	(<-list).send(Frame{}, ErrSilent)
	r.dialAt()
	if r.sub.State().Refused != nil {
		t.Fatal("a silent close was taken as a refusal")
	}
	// The ladder was reset: the next ordinary drop waits from the bottom rung.
	(<-list).send(Frame{}, errors.New("reset"))
	r.clock.tick(400 * time.Millisecond)
	r.dialAt()
}

func TestDeadAfterIsTwoAndAHalfBeats(t *testing.T) {
	if KeepEvery != 10*time.Second {
		t.Fatalf("KeepEvery = %v, want the 10 s beat", KeepEvery)
	}
	if DeadAfter != 25*time.Second {
		t.Fatalf("DeadAfter = %v, want 2.5 beats (25 s)", DeadAfter)
	}
	if PongWithin != DeadAfter-KeepEvery {
		t.Fatalf("PongWithin = %v, want what is left of DeadAfter after one beat", PongWithin)
	}
}
