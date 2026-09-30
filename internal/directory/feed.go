package directory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
)

// MaxWatchers is how many watch sockets one identity may hold at once. An
// identity has a handful of devices; a thousand is a runaway client, not a
// person. The hosted relay reads the same number as maxWatchers in limits.js.
const MaxWatchers = 1000

// VersionHeader carries the directory version on a list answer.
const VersionHeader = "Codeaf-Dir-Version"

// ErrTooManyWatchers refuses a watch socket beyond the identity's cap.
var ErrTooManyWatchers = errors.New("directory: too many watchers")

// errFeedClosed is what a subscriber hears when its directory is closed.
var errFeedClosed = errors.New("directory: closed")

// Status is what a watcher may learn about an identity: the directory's
// version, which devices are stopped and whether the identity was replaced. It
// is immutable once published, so it is shared without copying or locking.
type Status struct {
	Version  uint64
	Stopped  map[string]bool
	Rotation *Rotation
}

// statusOf derives the Status of a directory at version v from its records.
func statusOf(v uint64, rec IdentityRec, devices map[string]Device) Status {
	stopped := map[string]bool{}
	for id, d := range devices {
		if d.Revoked {
			stopped[id] = true
		}
	}
	return Status{Version: v, Stopped: stopped, Rotation: rec.Rotation}
}

// Watchable is a Directory that can tell devices it changed. A relay whose
// directory is not Watchable has no watch route and answers 404.
type Watchable interface {
	Directory
	Feed() *Feed
}

// Feed is one directory's published Status and the watchers waiting on it. A
// backend publishes once per committed change, and never anywhere else, so a
// watcher hears every change and nothing that only moved a lease's clock.
type Feed struct {
	cur atomic.Pointer[Status]

	mu     sync.Mutex
	cap    int
	subs   map[*Sub]struct{}
	closed chan struct{}
}

// NewFeed returns a feed at version 0 with the default cap.
func NewFeed() *Feed {
	f := &Feed{cap: MaxWatchers, subs: map[*Sub]struct{}{}, closed: make(chan struct{})}
	f.cur.Store(&Status{Stopped: map[string]bool{}})
	return f
}

// Status is the newest published Status.
func (f *Feed) Status() Status { return *f.cur.Load() }

// SetMaxWatchers changes the cap; a test or a relay that wants another says so once.
func (f *Feed) SetMaxWatchers(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cap = n
}

// Publish makes st current and wakes every watcher. A Status older than the
// current one is dropped, so two commits finishing out of order cannot make
// the version go down.
func (f *Feed) Publish(st Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if st.Version < f.cur.Load().Version {
		return
	}
	f.cur.Store(&st)
	for s := range f.subs {
		s.wake()
	}
}

// Close ends every watch; it is for the directory's own Close.
func (f *Feed) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
}

// Subscribe adds a watcher, or refuses with ErrTooManyWatchers at the cap.
func (f *Feed) Subscribe() (*Sub, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.subs) >= f.cap {
		return nil, ErrTooManyWatchers
	}
	s := &Sub{f: f, poke: make(chan struct{}, 1)}
	f.subs[s] = struct{}{}
	return s, nil
}

// Sub is one watcher. It keeps no queue: it always reads the newest Status, so
// a slow watcher skips intermediate versions and never sees one go down.
type Sub struct {
	f       *Feed
	poke    chan struct{}
	sent    uint64
	started bool
}

func (s *Sub) wake() {
	select {
	case s.poke <- struct{}{}:
	default:
	}
}

// Next waits for a Status this watcher has not heard: the first call answers
// at once with the current one, which is what closes the gap between a list
// read and a subscription.
func (s *Sub) Next(ctx context.Context) (Status, error) {
	for {
		if st := s.f.Status(); !s.started || st.Version > s.sent {
			s.started, s.sent = true, st.Version
			return st, nil
		}
		select {
		case <-ctx.Done():
			return Status{}, ctx.Err()
		case <-s.f.closed:
			return Status{}, errFeedClosed
		case <-s.poke:
		}
	}
}

// Close removes the watcher, which frees its place under the cap at once.
func (s *Sub) Close() {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	delete(s.f.subs, s)
}

// visible is a record that can say which of its fields a person sees.
type visible interface{ visibleAt(now int64) any }

// visibleAt is the Cell as the home list shows it: whether the lease is held
// counts, and when it runs out does not, so a heartbeat that only pushes the
// expiry later changes nothing a person could see.
func (c Cell) visibleAt(now int64) any {
	if c.Lease.Expires > now {
		c.Lease.Expires = 1
	} else {
		c.Lease.Expires = 0
	}
	return c
}

// fingerprint is the bytes two records are compared by.
func fingerprint(v any, now int64) []byte {
	if p, ok := v.(visible); ok {
		v = p.visibleAt(now)
	}
	b, _ := json.Marshal(v)
	return b
}

// differs says whether next is a change a person could see from old.
func differs(old, next any, now int64) bool {
	return !bytes.Equal(fingerprint(old, now), fingerprint(next, now))
}
