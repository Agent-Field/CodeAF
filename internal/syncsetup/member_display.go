package syncsetup

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
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

// followStream keeps advisory presence on the relay watch even while Home's
// directory reads use the local learner. A held lease cannot show that its
// computer is connected; only the relay's watch sockets supply that evidence.
func (s *Sync) followStream(ctx context.Context, w directory.Watcher) (dirwatch.Stream, error) {
	if !s.memberAllowed() {
		return nil, ErrQuiet
	}
	stream, err := w.Watch(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	display := &memberDisplayStream{
		Stream: stream, owner: s, cancel: cancel, closed: ctx.Done(), replies: make(chan displayReply),
	}
	// Capture the initial roster before returning, so a device committed after
	// this open is still announced even if the first read happens later.
	if _, _, err := display.refresh(ctx); err != nil {
		display.Close()
		return nil, err
	}
	go display.readRelay(ctx)
	return quietStream{Stream: display, home: s.Home, owner: s}, nil
}

var errMemberDisplayChanged = errors.New("the local directory follower changed")

type displayReply struct {
	frame dirwatch.Frame
	err   error
}

// memberDisplayStream combines committed learner changes with the relay's
// presence and pongs. Local state never supplies online evidence or a pong.
type memberDisplayStream struct {
	dirwatch.Stream
	owner       *Sync
	cancel      context.CancelFunc
	closed      <-chan struct{}
	once        sync.Once
	replies     chan displayReply
	member      *personalMember
	applied     uint64
	initialized bool
	version     uint64
	devices     map[string]directory.Device
	frames      []dirwatch.Frame
}

func (s *memberDisplayStream) Close() {
	s.once.Do(func() {
		s.cancel()
		s.Stream.Close()
	})
}

func (s *memberDisplayStream) readRelay(ctx context.Context) {
	for {
		frame, err := s.Stream.Next(ctx)
		select {
		case s.replies <- displayReply{frame: frame, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func (s *memberDisplayStream) Next(ctx context.Context) (dirwatch.Frame, error) {
	for {
		changed, local, err := s.refresh(ctx)
		if err != nil {
			return dirwatch.Frame{}, err
		}
		if len(s.frames) > 0 {
			frame := s.frames[0]
			s.frames[0] = dirwatch.Frame{}
			s.frames = s.frames[1:]
			return frame, nil
		}
		select {
		case reply := <-s.replies:
			if reply.err != nil {
				return dirwatch.Frame{}, reply.err
			}
			frame := reply.frame
			if local {
				// The learner supplies versions and committed device news once
				// available, avoiding duplicate join announcements from the relay.
				if frame.Event == nil && !frame.Pong && frame.Holds == nil {
					continue
				}
				if frame.Event != nil && (frame.Event.T == dirwatch.EventJoined || frame.Event.T == dirwatch.EventRevoked) {
					continue
				}
			}
			return frame, nil
		case <-changed:
		case <-s.closed:
			return dirwatch.Frame{}, context.Canceled
		case <-ctx.Done():
			return dirwatch.Frame{}, ctx.Err()
		}
	}
}

// refresh follows the terminal-owned learner, including one admitted after
// Home opened. Its notification is captured before the view to avoid lost news.
func (s *memberDisplayStream) refresh(ctx context.Context) (<-chan struct{}, bool, error) {
	if !s.owner.memberAllowed() {
		return nil, false, ErrQuiet
	}
	m := s.owner.displayMember()
	if m == nil {
		if s.member != nil {
			return nil, false, errMemberDisplayChanged
		}
		return nil, false, nil
	}
	m.mu.Lock()
	changed, applied := m.changed, m.status.Applied
	valid := !m.closed && m.failure == nil
	m.mu.Unlock()
	if !valid {
		if s.member != nil {
			return nil, false, errMemberDisplayChanged
		}
		return nil, false, nil
	}
	if s.member != m {
		s.member, s.initialized = m, false
	}
	if !s.initialized || s.applied != applied {
		listing, local, err := s.owner.displayListing(ctx)
		if err != nil {
			return nil, false, err
		}
		if !local {
			return nil, false, errMemberDisplayChanged
		}
		if listing.Identity.Rotation != nil {
			return nil, false, dirwatch.ErrRotated
		}
		if listing.Devices[s.owner.Device.ID()].Revoked {
			return nil, false, dirwatch.ErrRevoked
		}
		s.project(listing)
		s.applied = applied
	}
	return changed, true, nil
}

// project retains committed display versions and joined/revoked news. Neither
// leases nor device last-seen records can announce a computer online.
func (s *memberDisplayStream) project(l directory.Listing) {
	if !s.initialized || l.Version != s.version {
		s.frames = append(s.frames, dirwatch.Frame{Version: l.Version})
	}
	ids := make([]string, 0, len(l.Devices))
	for id := range l.Devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := l.Devices[id]
		old, existed := s.devices[id]
		if s.initialized && !existed && !d.Revoked && id != s.owner.Device.ID() {
			e := dirwatch.JoinedEvent(id, d.Name, d.Platform, d.Created)
			s.frames = append(s.frames, dirwatch.Frame{Event: &e})
		}
		if d.Revoked && !old.Revoked {
			e := dirwatch.RevokedEvent(id, l.Now)
			s.frames = append(s.frames, dirwatch.Frame{Event: &e})
		}
	}
	s.initialized, s.version, s.devices = true, l.Version, l.Devices
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
