package directory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
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

	// clock is the directory's own clock, so a sign of life and a lease expiry
	// are read on one timeline (a test's fake clock moves both).
	clock func() time.Time
	// holders indexes the sockets that name a hold, by what they name, so the
	// evidence for one lease is found without scanning every socket.
	holders map[holdKey]map[*Sub]struct{}
	// pres is which devices hold a socket, and the event frames about it.
	pres *presence
}

// holdKey is what a socket's hold is matched against: the device that opened
// the socket (from its signature) and the cell and fence it named.
type holdKey struct {
	device, cell string
	fence        uint64
}

// NewFeed returns a feed at version 0 with the default cap, stamping sockets
// with clock.
func NewFeed(clock func() time.Time) *Feed {
	f := &Feed{
		cap: MaxWatchers, subs: map[*Sub]struct{}{}, closed: make(chan struct{}),
		clock: clock, holders: map[holdKey]map[*Sub]struct{}{}, pres: newPresence(clock),
	}
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
	f.pres.revokedSince(f.cur.Load().Stopped, st.Stopped)
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
	f.pres.stop()
}

// Subscribe adds a watcher opened by device that names holds, or refuses with
// ErrTooManyWatchers at the cap. Its accept time is its first sign of life.
func (f *Feed) Subscribe(device string, holds []Hold) (*Sub, error) {
	s, err := f.subscribe(device, holds)
	if err == nil {
		f.pres.opened(s)
	}
	return s, err
}

func (f *Feed) subscribe(device string, holds []Hold) (*Sub, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.subs) >= f.cap {
		return nil, ErrTooManyWatchers
	}
	s := &Sub{f: f, poke: make(chan struct{}, 1), device: device, events: make(chan dirwatch.Event, eventBuffer), keys: keysOf(device, holds)}
	s.alive.Store(f.clock().UnixMilli())
	f.subs[s] = struct{}{}
	for _, k := range s.keys {
		if f.holders[k] == nil {
			f.holders[k] = map[*Sub]struct{}{}
		}
		f.holders[k][s] = struct{}{}
	}
	return s, nil
}

func keysOf(device string, holds []Hold) []holdKey {
	keys := make([]holdKey, len(holds))
	for i, h := range holds {
		keys[i] = holdKey{device, h.Cell, h.Fence}
	}
	return keys
}

// VouchedUntil is the time until which the sockets of device that named
// exactly (cell, fence) keep that lease live: the newest sign of life among
// them plus the lease TTL, or 0 when no socket names it.
func (f *Feed) VouchedUntil(device, cell string, fence uint64) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var newest int64
	for s := range f.holders[holdKey{device, cell, fence}] {
		newest = max(newest, s.alive.Load())
	}
	if newest == 0 {
		return 0
	}
	return newest + ttlMs
}

// Lifted is cell c (stored under id) as every reader must see it: with its
// lease kept live by the sockets that vouch for it.
func (f *Feed) Lifted(id string, c Cell) Cell {
	if c.Lease.Expires == 0 {
		return c
	}
	return Lifted(c, f.VouchedUntil(c.Lease.Device, id, c.Lease.Fence))
}

// lift is Lifted for a record of any kind: only a cell has a lease.
func (f *Feed) lift(id string, v any) any {
	if c, ok := v.(Cell); ok {
		return f.Lifted(id, c)
	}
	return v
}

// differs says whether next is a change a person could see from old, reading
// both as the lifted records they appear as, so evidence alone never counts.
func (f *Feed) differs(id string, old, next any, now int64) bool {
	return differs(f.lift(id, old), f.lift(id, next), now)
}

// Sub is one watcher. It keeps no queue: it always reads the newest Status, so
// a slow watcher skips intermediate versions and never sees one go down.
type Sub struct {
	f       *Feed
	poke    chan struct{}
	sent    uint64
	started bool

	device string
	shut   atomic.Bool
	events chan dirwatch.Event // event frames, once Events was called
	keys   []holdKey
	alive  atomic.Int64 // unix ms of the last sign of life
}

// Ping records a sign of life at the directory's time. It changes no version
// and wakes nobody: evidence is not a change a person can see.
func (s *Sub) Ping() { s.alive.Store(s.f.clock().UnixMilli()) }

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
	if !s.shut.CompareAndSwap(false, true) {
		return
	}
	s.leave()
	s.f.pres.closed(s)
}

func (s *Sub) leave() {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	delete(s.f.subs, s)
	for _, k := range s.keys {
		delete(s.f.holders[k], s)
		if len(s.f.holders[k]) == 0 {
			delete(s.f.holders, k)
		}
	}
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
