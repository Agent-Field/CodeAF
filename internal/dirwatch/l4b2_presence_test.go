package dirwatch

import (
	"testing"
	"time"
)

// L4-M17a, L4-M17b, L4-M17c: A stalled source loses its presence claim even if its timer cannot run.
func TestL4B2PresenceExpiresWithoutTimer(t *testing.T) {
	clock := newFakeClock()
	f := newFeed(nil, clock, func() float64 { return 0 })
	on := PresenceEvent("laptop-a", true, clock.Now().UnixMilli())
	f.hear(Frame{Event: &on}, false)
	if st := f.snapshot(); !st.Up || len(st.Online) != 1 {
		t.Fatalf("online answer: %+v", st)
	}
	clock.mu.Lock()
	clock.now = clock.now.Add(DeadAfter + time.Millisecond)
	clock.mu.Unlock()
	if st := f.snapshot(); st.Up || len(st.Online) != 0 {
		t.Fatalf("stale online claim: %+v", st)
	}
	off := PresenceEvent("laptop-a", false, clock.Now().UnixMilli())
	f.hear(Frame{Event: &off}, false)
	if st := f.snapshot(); !st.Up || len(st.Online) != 0 {
		t.Fatalf("new answer: %+v", st)
	}
	f.hear(Frame{Event: &on}, false)
	if st := f.snapshot(); !st.Up || len(st.Online) != 1 {
		t.Fatalf("peer return: %+v", st)
	}
}

// L4-M17b: A service stall ends the old subscription; a fresh subscription replaces the old online set.
func TestL4B2PresenceResubscribesAfterStall(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	r.dialAt()
	s := <-list
	on := PresenceEvent("laptop-a", true, r.clock.Now().UnixMilli())
	s.send(Frame{Event: &on}, nil)
	r.changed()
	r.clock.tick(KeepEvery)
	<-s.pings
	r.clock.tick(PongWithin)
	<-s.closed
	r.changed()
	if st := r.sub.State(); st.Up || len(st.Online) != 0 {
		t.Fatalf("stale presence: %+v", st)
	}
	r.clock.tick(RetryBase * 2 / 5)
	r.dialAt()
	next := <-list
	next.send(Frame{Version: 2}, nil)
	r.changed()
	if st := r.sub.State(); !st.Up || len(st.Online) != 0 {
		t.Fatalf("old online set survived: %+v", st)
	}
	next.send(Frame{Event: &on}, nil)
	r.changed()
	if st := r.sub.State(); !st.Up || len(st.Online) != 1 {
		t.Fatalf("new online answer missing: %+v", st)
	}
}

// L4-M17b, L4-M17c: A pong delivered after the freshness window cannot revive an old online set.
func TestL4B2LatePongRequiresNewPresence(t *testing.T) {
	list := make(chan *fakeStream, 4)
	r := newRig(t, sockets(list))
	r.dialAt()
	s := <-list
	on := PresenceEvent("laptop-a", true, r.clock.Now().UnixMilli())
	s.send(Frame{Event: &on}, nil)
	r.changed()
	r.clock.await(KeepEvery)
	r.clock.mu.Lock()
	r.clock.now = r.clock.now.Add(DeadAfter + time.Millisecond)
	r.clock.mu.Unlock()
	s.send(Frame{Pong: true}, nil)
	<-s.closed
	r.changed()
	if st := r.sub.State(); st.Up || len(st.Online) != 0 {
		t.Fatalf("late pong revived presence: %+v", st)
	}
	r.clock.tick(RetryBase * 2 / 5)
	r.dialAt()
	next := <-list
	next.send(Frame{Version: 2}, nil)
	r.changed()
	if st := r.sub.State(); !st.Up || len(st.Online) != 0 {
		t.Fatalf("subscription inherited old presence: %+v", st)
	}
}
