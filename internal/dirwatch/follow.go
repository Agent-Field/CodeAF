package dirwatch

import (
	"math/rand/v2"
	"sync"
)

// feeds is the process's open feeds by key. One identity's socket is shared by
// every surface that reads its directory, so a second follower joins the first
// feed and does not open a second socket.
var (
	feedsMu sync.Mutex
	feeds   = map[string]*Feed{}
)

// Follow joins the process's feed for key, opening its socket if nobody else
// has. The key names the identity on its relay; dial is used only by the
// follower that opens the feed.
func Follow(key string, dial Dialer) Follower {
	return follow(key, func() *Feed { return newFeed(dial, realClock{}, rand.Float64) })
}

// follow joins, or makes and starts, the feed for key.
func follow(key string, build func() *Feed) Follower {
	feedsMu.Lock()
	defer feedsMu.Unlock()
	f, ok := feeds[key]
	if !ok {
		f = build()
		feeds[key] = f
		f.start()
	}
	return f.follow(key)
}

// sub is one follower.
type sub struct {
	feed    *Feed
	key     string
	changes chan struct{}
	once    sync.Once
}

func (s *sub) State() State             { return s.feed.snapshot() }
func (s *sub) Changes() <-chan struct{} { return s.changes }
func (s *sub) Probe()                   { s.feed.probe() }
func (s *sub) Close()                   { s.once.Do(s.release) }

// release leaves the feed, and the last to leave takes the socket down and
// retires the feed so the next follower starts a fresh one.
func (s *sub) release() {
	feedsMu.Lock()
	last := s.feed.leave(s)
	if last {
		delete(feeds, s.key)
	}
	feedsMu.Unlock()
	if last {
		s.feed.stop()
	}
}

// probe asks the serving loop to ping now; one waiting request is enough.
func (f *Feed) probe() {
	select {
	case f.kick <- struct{}{}:
	default:
	}
}
