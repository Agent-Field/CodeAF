package directory

import (
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// OfflineDebounce is how long a device may hold no watch socket before its
// peers are told it is offline (docs/ux-pairing-contract.md, section 5).
const OfflineDebounce = 15 * time.Second

// eventBuffer is how many event frames wait for one socket. A socket that
// falls further behind loses the newest ones: events are hints, and the
// directory list is the truth a client re-reads.
const eventBuffer = 32

// After runs fn once after d and answers a stop that reports whether it beat
// the run. The relay's own is the wall clock's; a test that moves a fake clock
// hands the directory timers that move with it.
type After func(d time.Duration, fn func()) (stop func() bool)

// presence knows which devices hold a live watch socket, and tells the sockets
// that asked for events when that changes. A socket is live while its pings
// keep coming (contract 21.12.2), and a device is online while it holds one.
// A device whose last live socket closed is announced offline only after
// OfflineDebounce, and a reconnect inside that gap announces nothing. A device
// whose sockets simply fell silent is announced offline by the sweep at the
// moment its last one lapsed, and its sockets are told to go.
type presence struct {
	clock   func() time.Time
	started time.Time
	// after is the timers the debounce and the sweep run on.
	after After

	mu       sync.Mutex
	socks    map[string]map[*Sub]struct{} // sockets held, by device, live or not
	waiting  map[string]*gap              // devices in their debounce: still told online
	gone     map[string]bool              // devices already announced offline since they last arrived
	lastSeen map[string]int64             // newest sign of life observed for each device
	watching map[string]*watchClient      // scoped clients' watching state and dampen timers
	watchers map[*Sub]struct{}            // the sockets that asked for events
	seen     func(device string, at int64)
	alarm    alarm
}

// watchClient tracks one scoped client's watching state and its 2-second dampen window.
type watchClient struct {
	current   bool
	sent      bool
	hasSent   bool
	lastSent  int64
	pending   bool
	stopTimer func() bool
}

// gap is one device's debounce. Identity is the pointer: a timer that fires
// after its device returned finds another gap, or none, and does nothing.
type gap struct{ stop func() bool }

// seenAt is one last_seen stamp the presence owes the OnSeen hook, which must
// not be called while the lock is held.
type seenAt struct {
	device string
	at     int64
}

func newPresence(clock func() time.Time) *presence {
	return &presence{
		clock:   clock,
		started: clock(),
		after: func(d time.Duration, fn func()) func() bool {
			return time.AfterFunc(d, fn).Stop
		},
		socks:    map[string]map[*Sub]struct{}{},
		waiting:  map[string]*gap{},
		gone:     map[string]bool{},
		lastSeen: map[string]int64{},
		watching: map[string]*watchClient{},
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

// stamp calls the OnSeen hook for each stamp, outside every lock.
func (p *presence) stamp(hook func(string, int64), stamps ...seenAt) {
	if hook == nil {
		return
	}
	for _, s := range stamps {
		hook(s.device, s.at)
	}
}

// opened counts a new socket of s. The first socket of a device is news to the
// others unless the device is only reconnecting inside its gap; a device that
// was announced offline is news again whenever it returns.
func (p *presence) opened(s *Sub) {
	p.mu.Lock()
	now := p.now()
	had := len(p.socks[s.device]) > 0
	if p.socks[s.device] == nil {
		p.socks[s.device] = map[*Sub]struct{}{}
	}
	p.socks[s.device][s] = struct{}{}
	p.lastSeen[s.device] = max(p.lastSeen[s.device], now)
	p.arrived(s.device, had)
	p.arm()
	hook := p.seen
	p.mu.Unlock()
	p.stamp(hook, seenAt{s.device, now})
}

func (p *presence) arrived(device string, had bool) {
	if p.gone[device] {
		delete(p.gone, device)
		p.tell(dirwatch.PresenceEvent(device, true, p.now()), device)
		return
	}
	if g := p.waiting[device]; g != nil {
		g.stop()
		delete(p.waiting, device)
		return
	}
	if !had {
		p.tell(dirwatch.PresenceEvent(device, true, p.now()), device)
	}
}

// closed uncounts a socket of s. When it leaves its device with no live socket,
// that device's gap starts if the socket was live, and the device is announced
// offline at once if it was not: a socket that was already silent has waited its
// debounce out, and one the sweep lapsed was announced then.
func (p *presence) closed(s *Sub) {
	p.mu.Lock()
	now := p.now()
	wasLive := s.liveAt(now)
	if wasLive {
		p.lastSeen[s.device] = max(p.lastSeen[s.device], now)
	} else {
		p.lastSeen[s.device] = max(p.lastSeen[s.device], s.alive.Load())
	}
	delete(p.socks[s.device], s)
	if len(p.socks[s.device]) == 0 {
		delete(p.socks, s.device)
	}
	delete(p.watchers, s)
	stamps := p.departed(s, wasLive, now)
	p.arm()
	hook := p.seen
	p.mu.Unlock()
	p.stamp(hook, stamps...)
}

// departed settles the device of s after s left it, and answers the last_seen
// stamp that is owed, if any. The caller holds p.mu.
func (p *presence) departed(s *Sub, wasLive bool, now int64) []seenAt {
	d := s.device
	if p.liveSocks(d, now) > 0 || s.lapsed.Load() {
		return nil
	}
	if wasLive {
		p.depart(d)
		return []seenAt{{d, now}}
	}
	return p.expire(d, s.alive.Load())
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
	p.expire(device, 0)
}

// expire announces device offline once, tells every socket it still holds to go
// and answers the last_seen stamp owed: its newest sign of life, or last when
// that is newer (a socket that already closed). The caller holds p.mu.
func (p *presence) expire(device string, last int64) []seenAt {
	if g := p.waiting[device]; g != nil {
		g.stop()
		delete(p.waiting, device)
	}
	if p.gone[device] {
		return nil
	}
	p.gone[device] = true
	for s := range p.socks[device] {
		last = max(last, s.alive.Load())
		s.lapse()
	}
	p.tell(dirwatch.PresenceEvent(device, false, p.now()), device)
	if last == 0 {
		return nil
	}
	return []seenAt{{device, last}}
}

// liveSocks counts the live sockets of device at unix ms now. The caller holds p.mu.
func (p *presence) liveSocks(device string, now int64) int {
	n := 0
	for s := range p.socks[device] {
		if s.liveAt(now) {
			n++
		}
	}
	return n
}

// liveServingSocks counts the live sockets of device opened with serving=1 at unix ms now. The caller holds p.mu.
func (p *presence) liveServingSocks(device string, now int64) int {
	n := 0
	for s := range p.socks[device] {
		if s.serving.Load() && s.liveAt(now) {
			n++
		}
	}
	return n
}

// subscribe makes s an event socket and queues who is online and which clients
// are watching for it, itself excluded from presence. The sweep exists from
// here on, until the last event socket leaves.
func (p *presence) subscribe(s *Sub) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.watchers[s] = struct{}{}
	now := p.now()
	for _, device := range slices.Sorted(maps.Keys(p.believed())) {
		if device != s.device {
			s.offer(dirwatch.PresenceEvent(device, true, now))
		}
	}
	for _, device := range slices.Sorted(maps.Keys(p.watching)) {
		if w := p.watching[device]; w != nil && w.current {
			s.offer(dirwatch.WatchingEvent(device, true, now))
		}
	}
	p.arm()
}

// believed is the devices peers are told are online: those holding a live
// socket and those still inside their gap. The caller holds p.mu.
func (p *presence) believed() map[string]bool {
	set := p.liveDevices()
	for d := range p.waiting {
		set[d] = true
	}
	return set
}

// liveDevices is the devices that hold a live socket. The caller holds p.mu.
func (p *presence) liveDevices() map[string]bool {
	now := p.now()
	set := map[string]bool{}
	for d := range p.socks {
		if p.liveSocks(d, now) > 0 {
			set[d] = true
		}
	}
	return set
}

// online is whether device holds a live watch socket now.
func (p *presence) online(device string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.liveSocks(device, p.now()) > 0
}

// onlineSet is the devices that hold a live watch socket now.
func (p *presence) onlineSet() map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.liveDevices()
}

func (p *presence) presentSet() map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.believed()
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

// stop cancels every gap, watching timer and the sweep, for the directory's own Close.
func (p *presence) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for d, g := range p.waiting {
		g.stop()
		delete(p.waiting, d)
	}
	for _, w := range p.watching {
		if w != nil && w.stopTimer != nil {
			w.stopTimer()
			w.stopTimer = nil
		}
	}
	p.alarm.cancel()
}

// setWatching updates a scoped client's watching state and broadcasts changes
// to event sockets at most once per WatchingDampen window (contract L4, L7).
func (p *presence) setWatching(device string, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.watching[device]
	if w == nil {
		w = &watchClient{}
		p.watching[device] = w
	}
	if w.current == on {
		return
	}
	w.current = on
	if w.current == w.sent {
		if w.stopTimer != nil {
			w.stopTimer()
			w.stopTimer = nil
		}
		w.pending = false
		return
	}
	now := p.now()
	dampenMs := clientproto.WatchingDampen.Milliseconds()
	if !w.hasSent || now-w.lastSent >= dampenMs {
		if w.stopTimer != nil {
			w.stopTimer()
			w.stopTimer = nil
		}
		w.pending = false
		w.sent = w.current
		w.hasSent = true
		w.lastSent = now
		p.tell(dirwatch.WatchingEvent(device, w.sent, now), "")
		return
	}
	if w.pending {
		return
	}
	w.pending = true
	delay := time.Duration(w.lastSent+dampenMs-now) * time.Millisecond
	w.stopTimer = p.after(delay, func() {
		p.flushWatching(device, w)
	})
}

func (p *presence) flushWatching(device string, w *watchClient) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.watching[device] != w || !w.pending {
		return
	}
	w.pending = false
	w.stopTimer = nil
	if w.current == w.sent {
		return
	}
	now := p.now()
	w.sent = w.current
	w.hasSent = true
	w.lastSent = now
	p.tell(dirwatch.WatchingEvent(device, w.sent, now), "")
}

// computerStatus reports whether device currently holds a live serving=1 watch
// socket and its last_seen timestamp as presence reports it (contract L8).
func (p *presence) computerStatus(device string, storedLastSeen, now int64) (serving bool, at int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	serving = p.liveServingSocks(device, now) > 0
	at = max(storedLastSeen, p.lastSeen[device])
	for s := range p.socks[device] {
		at = max(at, s.alive.Load())
	}
	if serving || p.believed()[device] {
		at = now
	}
	return serving, at
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

// PresentSet agrees with presence events during the reconnect debounce. A
// roster refresh cannot announce offline before the watch would announce it.
func (f *Feed) PresentSet() map[string]bool { return f.pres.presentSet() }

// WithPresence applies this feed's evidence to a roster answer. A fresh relay
// has not observed the stored computers yet; absence becomes offline only
// after a default presence window, or after an observed socket actually lapses.
func (f *Feed) WithPresence(v PresenceView) PresenceView {
	p := f.pres
	p.mu.Lock()
	defer p.mu.Unlock()
	online := p.believed()
	young := p.clock().Sub(p.started) < Window(DefaultBeat)
	for id, seen := range v.Devices {
		if young && !online[id] && len(p.socks[id]) == 0 && !p.gone[id] {
			delete(v.Devices, id)
			continue
		}
		seen.Online = online[id]
		if seen.Online {
			seen.LastSeen = v.Now
		}
		v.Devices[id] = seen
	}
	return v
}

// announceJoin tells the event sockets of every device but the new one that it
// joined. A backend calls it, through SetOnJoin, in the turn that approves the
// device; the name is the sealed name as stored.
func (f *Feed) announceJoin(j Joined) {
	f.pres.announce(dirwatch.JoinedEvent(j.Device, j.Name, j.Platform, j.At), j.Device)
}

// AnnounceJoin publishes an advisory event only after a consensus host has
// committed the device and completed its pairing delivery.
func (f *Feed) AnnounceJoin(j Joined) { f.announceJoin(j) }

// OnSeen registers fn to stamp a device's last_seen: it is called with the
// directory time (unix ms) at each watch connect and when a device closes its
// last socket, never per ping and never while the feed holds a lock. A
// backend sets it once, before it serves.
func (f *Feed) OnSeen(fn func(device string, at int64)) {
	f.pres.mu.Lock()
	defer f.pres.mu.Unlock()
	f.pres.seen = fn
}

// SetWatching updates a scoped client's watching state for event sockets (contract L4, L7).
func (f *Feed) SetWatching(device string, on bool) {
	f.pres.setWatching(device, on)
}

// ComputerStatus reports whether a full device holds a live serving=1 watch socket
// and its last_seen timestamp as presence reports it (contract L8).
func (f *Feed) ComputerStatus(device string, storedLastSeen, now int64) (serving bool, at int64) {
	return f.pres.computerStatus(device, storedLastSeen, now)
}
