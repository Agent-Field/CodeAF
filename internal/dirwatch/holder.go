package dirwatch

import (
	"context"
	"math/rand/v2"
	"slices"
	"sync"
)

// ── THE HOLDING SOCKET ──────────────────────────────────────────────────────
//
// A process that holds chats keeps one socket of its own that names them, so
// the relay can take the socket's pings as proof that the holder is alive and
// the holder need not send a heartbeat while it is (STAGE-1-CONTRACTS.md
// §21.11). It is kept apart from the home screen's feed on purpose: the home
// socket names no holds, so it vouches for nothing and a screen that is only
// looked at cannot be mistaken for a holder.
//
// The socket is the Feed that already dials, pings, notices death and backs off
// with jitter. The holder adds only what a hold needs: the set it names, a
// redial when that set changes, and the question "is this hold covered".

// HoldDialer opens one socket that names holds. A hold is whatever the dialler
// and the relay agree names a lease; this package only compares them.
type HoldDialer[H comparable] func(ctx context.Context, holds []H) (Stream, error)

// Holder is one identity's holding socket. The socket exists only while
// somebody holds a hold.
type Holder[H comparable] struct {
	clock  Clock
	jitter func() float64
	max    int
	key    string
	dial   HoldDialer[H]

	mu      sync.Mutex
	feed    *Feed         // nil while no hold is taken
	order   []*Holding[H] // the holds asked for, in the order they were asked
	dialled []H           // what the open, or last opened, socket names
}

// holders is the process's holders by key, one per identity as feeds are.
var (
	holdersMu sync.Mutex
	holders   = map[string]any{}
)

// Hold returns the process's holding socket for key, making it if this is the
// first to ask. It dials nothing until somebody takes a hold, and it never
// names more than max holds. The key names the identity on its relay; dial is
// used only by the caller that makes it.
func Hold[H comparable](key string, max int, dial HoldDialer[H]) *Holder[H] {
	holdersMu.Lock()
	defer holdersMu.Unlock()
	if h, ok := holders[key].(*Holder[H]); ok {
		return h
	}
	h := newHolder(key, max, dial, realClock{}, rand.Float64)
	holders[key] = h
	return h
}

func newHolder[H comparable](key string, max int, dial HoldDialer[H], clock Clock, jitter func() float64) *Holder[H] {
	return &Holder[H]{clock: clock, jitter: jitter, max: max, key: key, dial: dial}
}

// open is the feed's dialler: it opens a socket naming the holds asked for now
// and remembers what it named, so "is this hold covered" is about this socket.
func (h *Holder[H]) open(ctx context.Context) (Stream, error) {
	named := h.named()
	s, err := h.dial(ctx, named)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.dialled = named
	h.mu.Unlock()
	return s, nil
}

func (h *Holder[H]) named() []H {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.namedLocked()
}

// namedLocked is the distinct holds asked for, the first max of them in the
// order they were asked. The rest are never named, so they are never covered.
func (h *Holder[H]) namedLocked() []H {
	var out []H
	for _, w := range h.order {
		if len(out) < h.max && !slices.Contains(out, w.hold) {
			out = append(out, w.hold)
		}
	}
	return out
}

// Holding is one holder's hold, as it reads the socket.
type Holding[H comparable] struct {
	holder *Holder[H]
	feed   *Feed
	hold   H
	sub    *sub
	once   sync.Once
}

// Take asks the socket to name hold and returns the handle to read it by. The
// first hold opens the socket; one that changes what is named ends the socket
// and dials again.
func (h *Holder[H]) Take(hold H) *Holding[H] {
	h.mu.Lock()
	before := h.namedLocked()
	fresh := h.feed == nil
	if fresh {
		h.feed = newFeed(h.open, h.clock, h.jitter)
		h.dialled = nil
	}
	w := &Holding[H]{holder: h, feed: h.feed, hold: hold, sub: h.feed.follow(h.key)}
	h.order = append(h.order, w)
	changed := !slices.Equal(before, h.namedLocked())
	h.mu.Unlock()
	switch {
	case fresh:
		w.feed.start()
	case changed:
		w.feed.redialNow()
	}
	return w
}

// Healthy says whether the relay is vouching for the hold: the socket is open,
// its server said it vouches, the socket was dialled naming this hold, and it
// showed a sign of life within DeadAfter. Every part matters. A socket that is
// down proves nothing; a server that did not say so is an old one; a socket
// dialled before the hold was taken does not name it; and a link that went
// quiet may be half open, which the feed only notices at its own deadline.
func (w *Holding[H]) Healthy() bool {
	st := w.feed.snapshot()
	if !st.Up || !st.Vouching {
		return false
	}
	h := w.holder
	h.mu.Lock()
	named := slices.Contains(h.dialled, w.hold)
	h.mu.Unlock()
	return named && h.clock.Now().Sub(w.feed.lastHeard()) <= DeadAfter
}

// Changes signals that the socket's state changed, coalesced as a Follower's
// are, and is closed by Release.
func (w *Holding[H]) Changes() <-chan struct{} { return w.sub.changes }

// Release gives the hold up. The last to be released takes the socket down; a
// release that changes what is named dials again.
func (w *Holding[H]) Release() { w.once.Do(w.release) }

func (w *Holding[H]) release() {
	h := w.holder
	h.mu.Lock()
	before := h.namedLocked()
	h.order = slices.DeleteFunc(h.order, func(o *Holding[H]) bool { return o == w })
	last := len(h.order) == 0
	changed := !slices.Equal(before, h.namedLocked())
	if last {
		h.feed = nil
	}
	h.mu.Unlock()
	w.feed.leave(w.sub)
	switch {
	case last:
		w.feed.stop()
	case changed:
		w.feed.redialNow()
	}
}
