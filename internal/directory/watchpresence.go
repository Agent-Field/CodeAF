package directory

import (
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// OfflineDebounce is how long a device may hold no watch socket before its
// peers are told it is offline (docs/ux-pairing-contract.md, section 5).
const OfflineDebounce = 15 * time.Second

// eventBuffer is how many event frames wait for one socket. A socket that
// falls further behind loses the newest ones: events are hints, and the
// directory list is the truth a client re-reads.
const eventBuffer = 32

// presence knows which devices hold a watch socket, and tells the sockets that
// asked for events when that changes. A device is online while it holds at
// least one socket; going offline is announced only after OfflineDebounce
// without one, and a reconnect inside that gap announces nothing.
type presence struct {
	clock func() time.Time
	// after runs fn once after d and answers a stop that reports whether it
	// beat the run; tests replace it.
	after func(d time.Duration, fn func()) (stop func() bool)

	mu       sync.Mutex
	open     map[string]int    // sockets held, by device
	waiting  map[string]*gap   // devices in their debounce: still told online
	watchers map[*Sub]struct{} // the sockets that asked for events
	seen     func(device string, at int64)
}

// gap is one device's debounce. Identity is the pointer: a timer that fires
// after its device returned finds another gap, or none, and does nothing.
type gap struct{ stop func() bool }

func newPresence(clock func() time.Time) *presence {
	return &presence{
		clock: clock,
		after: func(d time.Duration, fn func()) func() bool {
			return time.AfterFunc(d, fn).Stop
		},
		open:     map[string]int{},
		waiting:  map[string]*gap{},
		watchers: map[*Sub]struct{}{},
	}
}

// tell sends e to every event socket except the sockets of device `except`.
// The caller holds p.mu.
func (p *presence) tell(e dirwatch.Event, except string) {
	for s := range p.watchers {
		if s.device != except {
			s.offer(e)
		}
	}
}

func (p *presence) now() int64 { return p.clock().UnixMilli() }

// opened counts a new socket of s. The first socket of a device is news to the
// others unless the device is only reconnecting inside its gap.
func (p *presence) opened(s *Sub) {
	p.mu.Lock()
	p.open[s.device]++
	if p.open[s.device] == 1 {
		p.arrived(s.device)
	}
	hook := p.seen
	p.mu.Unlock()
	if hook != nil {
		hook(s.device, p.now())
	}
}

func (p *presence) arrived(device string) {
	if g := p.waiting[device]; g != nil {
		g.stop()
		delete(p.waiting, device)
		return
	}
	p.tell(dirwatch.PresenceEvent(device, true, p.now()), device)
}

// closed uncounts a socket of s; the last one of a device starts its gap.
func (p *presence) closed(s *Sub) {
	p.mu.Lock()
	delete(p.watchers, s)
	var last bool
	if p.open[s.device]--; p.open[s.device] <= 0 {
		delete(p.open, s.device)
		p.depart(s.device)
		last = true
	}
	hook := p.seen
	p.mu.Unlock()
	if last && hook != nil {
		hook(s.device, p.now())
	}
}

func (p *presence) depart(device string) {
	g := &gap{}
	g.stop = p.after(OfflineDebounce, func() { p.lapsed(device, g) })
	p.waiting[device] = g
}

// lapsed ends g: the device stayed away, so its peers hear that it is offline.
func (p *presence) lapsed(device string, g *gap) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.waiting[device] != g {
		return
	}
	delete(p.waiting, device)
	p.tell(dirwatch.PresenceEvent(device, false, p.now()), device)
}

// subscribe makes s an event socket and queues who is online for it, itself excluded.
func (p *presence) subscribe(s *Sub) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.watchers[s] = struct{}{}
	for _, device := range slices.Sorted(maps.Keys(p.believed())) {
		if device != s.device {
			s.offer(dirwatch.PresenceEvent(device, true, p.now()))
		}
	}
}

// believed is the devices peers are told are online: those holding a socket
// and those still inside their gap. The caller holds p.mu.
func (p *presence) believed() map[string]bool {
	set := map[string]bool{}
	for d := range p.open {
		set[d] = true
	}
	for d := range p.waiting {
		set[d] = true
	}
	return set
}

// online is whether device holds a watch socket now.
func (p *presence) online(device string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.open[device] > 0
}

// onlineSet is the devices that hold a watch socket now.
func (p *presence) onlineSet() map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	set := make(map[string]bool, len(p.open))
	for d := range p.open {
		set[d] = true
	}
	return set
}

// announce tells every event socket except those of `except` about e.
func (p *presence) announce(e dirwatch.Event, except string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tell(e, except)
}

// revokedSince tells the event sockets about each device stopped in next that was not in prev.
func (p *presence) revokedSince(prev, next map[string]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for device := range next {
		if !prev[device] {
			p.tell(dirwatch.RevokedEvent(device, p.now()), device)
		}
	}
}

// stop cancels every gap, for the directory's own Close.
func (p *presence) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for d, g := range p.waiting {
		g.stop()
		delete(p.waiting, d)
	}
}

// offer queues e for the socket. A full buffer drops e: events are hints, and
// the list is the truth a client re-reads.
func (s *Sub) offer(e dirwatch.Event) {
	select {
	case s.events <- e:
	default:
	}
}

// Events makes the watcher an event socket and answers the channel its event
// frames arrive on. It is queued with one presence frame per other online
// device before it is returned, so the first frames after the version are
// those. Call it once, before the first frame is written.
func (s *Sub) Events() <-chan dirwatch.Event {
	s.f.pres.subscribe(s)
	return s.events
}

// Online is whether device holds at least one watch socket now (docs/ux-pairing-contract.md, section 5).
func (f *Feed) Online(device string) bool { return f.pres.online(device) }

// OnlineSet is every device that holds a watch socket now. The caller owns the map.
func (f *Feed) OnlineSet() map[string]bool { return f.pres.onlineSet() }

// AnnounceJoined tells the event sockets of every device but the new one that
// device joined. The backend calls it in the turn that approves the device;
// name is the sealed name as stored.
func (f *Feed) AnnounceJoined(device, name, platform string) {
	f.pres.announce(dirwatch.JoinedEvent(device, name, platform, f.pres.now()), device)
}

// OnSeen registers fn to stamp a device's last_seen: it is called with the
// directory time (unix ms) at each watch connect and when a device closes its
// last socket, never per ping and never while the feed holds a lock. A
// backend sets it once, before it serves.
func (f *Feed) OnSeen(fn func(device string, at int64)) {
	f.pres.mu.Lock()
	defer f.pres.mu.Unlock()
	f.pres.seen = fn
}
