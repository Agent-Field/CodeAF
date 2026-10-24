package syncsetup

import (
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/identity"
	"sort"
	"sync"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
)

// liveMembers shares the existing terminal-owned learner with other Sync
// handles for the same home. It owns no goroutine, store or network lifetime.
var liveMembers = struct {
	sync.Mutex
	members map[string]*personalMember
}{members: map[string]*personalMember{}}

func (m *personalMember) publishDisplay() {
	liveMembers.Lock()
	defer liveMembers.Unlock()
	liveMembers.members[m.owner.memberPath()] = m
}
func (m *personalMember) withdrawDisplay() {
	liveMembers.Lock()
	defer liveMembers.Unlock()
	key := m.owner.memberPath()
	if liveMembers.members[key] == m {
		delete(liveMembers.members, key)
	}
}
func (s *Sync) displayMember() *personalMember {
	liveMembers.Lock()
	defer liveMembers.Unlock()
	return liveMembers.members[s.memberPath()]
}

// displayListing is a hint for Home, its roster and its backstop. Decisions
// still call Dir.Cell/List and the ordinary mutation doors with fresh quorum.
func (s *Sync) displayListing(ctx context.Context) (directory.Listing, bool, error) {
	if !s.memberAllowed() {
		return directory.Listing{}, false, nil
	}
	m := s.displayMember()
	if m == nil {
		return directory.Listing{}, false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.failure != nil {
		return directory.Listing{}, false, nil
	}
	app, _, err := m.store.ApplicationView(ctx)
	if err != nil {
		return directory.Listing{}, true, err
	}
	if app.Applied < m.record.Grant.Barrier {
		return directory.Listing{}, false, nil
	}
	l, err := raftdirectory.DisplayListing(ctx, m.store, app)
	// This clock ages only display hints. It cannot expire a committed lease
	// or authorize a take; those paths use the relay's current-time barrier.
	l.Now = max(l.Now, s.now().UnixMilli())
	return l, true, err
}

var errMemberDisplayChanged = errors.New("the local directory follower changed")

// switchingDisplay ends the old advisory socket at its next use once the
// terminal's learner is available. Its replacement is entirely local.
type switchingDisplay struct {
	dirwatch.Stream
	owner *Sync
}

func (s switchingDisplay) Ping(ctx context.Context) error {
	if !s.owner.memberAllowed() {
		s.Stream.Close()
		return ErrQuiet
	}
	if s.owner.memberAllowed() && s.owner.displayMember() != nil {
		s.Stream.Close()
		return errMemberDisplayChanged
	}
	s.owner.clusterFrames.Add(1)
	return s.Stream.Ping(ctx)
}

func (s *Sync) followStream(ctx context.Context, w directory.Watcher) (dirwatch.Stream, error) {
	if !s.memberAllowed() {
		return nil, ErrQuiet
	}
	if m := s.displayMember(); m != nil {
		m.mu.Lock()
		valid := !m.closed && m.failure == nil
		m.mu.Unlock()
		if valid {
			return &localDisplayStream{owner: s, member: m, pong: make(chan struct{}, 1), closed: make(chan struct{}), devices: map[string]directory.Device{}, online: map[string]bool{}}, nil
		}
	}
	stream, err := w.Watch(ctx)
	if err != nil {
		return nil, err
	}
	return switchingDisplay{Stream: stream, owner: s}, nil
}

type localDisplayStream struct {
	owner       *Sync
	member      *personalMember
	mu          sync.Mutex
	pong        chan struct{}
	closed      chan struct{}
	once        sync.Once
	initialized bool
	applied     uint64
	cached      directory.Listing
	version     uint64
	devices     map[string]directory.Device
	online      map[string]bool
	frames      []dirwatch.Frame
}

func (s *localDisplayStream) Vouching() bool { return false }
func (s *localDisplayStream) Close()         { s.once.Do(func() { close(s.closed) }) }
func (s *localDisplayStream) Ping(context.Context) error {
	if !s.owner.memberAllowed() {
		s.Close()
		return ErrQuiet
	}
	select {
	case s.pong <- struct{}{}:
	default:
	}
	return nil
}
func (s *localDisplayStream) Next(ctx context.Context) (dirwatch.Frame, error) {
	for {
		if !s.owner.memberAllowed() {
			s.Close()
			return dirwatch.Frame{}, ErrQuiet
		}
		m := s.member
		m.mu.Lock()
		changed := m.changed
		applied := m.status.Applied
		closed, failed := m.closed, m.failure
		m.mu.Unlock()
		if closed || failed != nil {
			return dirwatch.Frame{}, errMemberDisplayChanged
		}
		listing, local, err := s.cached, true, error(nil)
		if s.applied != applied || !s.initialized {
			listing, local, err = s.owner.displayListing(ctx)
			if err == nil && local {
				s.cached = listing
				s.applied = applied
			}
		}
		listing.Now = max(listing.Now, s.owner.now().UnixMilli())
		if err != nil {
			return dirwatch.Frame{}, err
		}
		if !local {
			return dirwatch.Frame{}, errMemberDisplayChanged
		}
		if listing.Identity.Rotation != nil {
			return dirwatch.Frame{}, dirwatch.ErrRotated
		}
		if listing.Devices[s.owner.Device.ID()].Revoked {
			return dirwatch.Frame{}, dirwatch.ErrRevoked
		}
		s.mu.Lock()
		s.project(listing)
		if len(s.frames) > 0 {
			frame := s.frames[0]
			s.frames = s.frames[1:]
			s.mu.Unlock()
			return frame, nil
		}
		s.mu.Unlock()
		select {
		case <-changed:
		case <-s.pong:
			return dirwatch.Frame{Pong: true}, nil
		case <-s.closed:
			return dirwatch.Frame{}, context.Canceled
		case <-ctx.Done():
			return dirwatch.Frame{}, ctx.Err()
		}
	}
}

// project produces only display hints and committed joined/revoked news. A
// quiet neighbour's presence ages normally; a live unexpired held chat is
// also recent evidence. Neither hint can suppress an ownership heartbeat.
func (s *localDisplayStream) project(l directory.Listing) {
	if !s.initialized || l.Version != s.version {
		s.frames = append(s.frames, dirwatch.Frame{Version: l.Version})
	}
	ids := make([]string, 0, len(l.Devices))
	for id := range l.Devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	online := map[string]bool{s.owner.Device.ID(): true}
	for _, cell := range l.Cells {
		if cell.Lease.Expires > l.Now {
			online[cell.Lease.Device] = true
		}
	}
	for _, id := range ids {
		d := l.Devices[id]
		active := !d.Revoked && (online[id] || d.LastSeen != 0 && l.Now-d.LastSeen < dirwatch.DeadAfter.Milliseconds())
		if active != s.online[id] {
			e := dirwatch.PresenceEvent(id, active, l.Now)
			s.frames = append(s.frames, dirwatch.Frame{Event: &e})
		}
		old, existed := s.devices[id]
		if s.initialized && !existed && !d.Revoked && id != s.owner.Device.ID() {
			e := dirwatch.JoinedEvent(id, d.Name, d.Platform, d.Created)
			s.frames = append(s.frames, dirwatch.Frame{Event: &e})
		}
		if d.Revoked && !old.Revoked {
			e := dirwatch.RevokedEvent(id, l.Now)
			s.frames = append(s.frames, dirwatch.Frame{Event: &e})
		}
		online[id] = active
	}
	s.initialized = true
	s.version = l.Version
	s.devices = l.Devices
	s.online = online
}

// currentDisplaySync follows a pairing or relay adoption made while Home is
// open. Its old signer and metadata key must not keep displaying the old fleet.
// Hand-built local sources have no relay and retain their supplied behavior.
func (s *Sync) currentDisplaySync() (*Sync, error) {
	if s.Relay == "" || Quiet(s.Home) {
		return s, nil
	}
	id, err := identity.Load(s.Home)
	if err != nil {
		return nil, err
	}
	base, err := relayBase(Resolve(s.Home).URL)
	if err != nil {
		return nil, err
	}
	if id.ID() == s.Identity.ID() && base == s.Relay {
		return s, nil
	}
	next, ok, err := Open(s.Home)
	if err != nil {
		return nil, err
	}
	if !ok {
		return s, nil
	}
	current := buildAt(next.Home, next.Relay, next.Identity, next.Device, next.Interval, s.now)
	current.Hosted = next.Hosted
	return current, nil
}
