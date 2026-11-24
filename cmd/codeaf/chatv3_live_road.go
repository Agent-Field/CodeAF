package main

import (
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

const liveTouchScope = "chatv3/live-touch"

// syncLiveRoads returns the function wireLive installs as liveRoads: the road
// to one other computer of this person's, made on first use and kept for the
// process.
func syncLiveRoads(s *syncsetup.Sync) func(device, name string) (liveRoad, string, bool) {
	var (
		mu    sync.Mutex
		roads = make(map[string]*syncRoad)
	)
	return func(device, name string) (liveRoad, string, bool) {
		if s == nil || !config.RemoteControlOn() || device == "" || device == s.Device.ID() {
			return nil, "", false
		}
		key := device + "\x00" + name
		mu.Lock()
		defer mu.Unlock()
		if road, ok := roads[key]; ok {
			return road, name, true
		}
		road := &syncRoad{
			host:    name,
			changed: make(chan struct{}),
		}
		road.bind(s.LiveLink(device, name, nil, road.onState))
		roads[key] = road
		return road, name, true
	}
}

// syncRoad adapts one multiplexed livedoor.Link to the surface's liveRoad
// seam, holding the latest reported state in memory so the draw path never
// locks or polls the link.
type syncRoad struct {
	link  *livedoor.Link
	host  string
	touch func()

	mu      sync.Mutex
	state   livedoor.State
	permit  pair.Permit
	changed chan struct{}

	touching atomic.Bool
}

var _ liveRoad = (*syncRoad)(nil)

func newSyncRoad(link *livedoor.Link, host string) *syncRoad {
	r := &syncRoad{
		host:    host,
		changed: make(chan struct{}),
	}
	r.bind(link)
	return r
}

func (r *syncRoad) bind(link *livedoor.Link) {
	var (
		st    livedoor.State
		touch func()
	)
	if link != nil {
		st = link.State()
		touch = link.Touch
	}
	r.attach(link, touch, st)
}

// attach holds a link and the state it started in. The lock is released by a defer
// so a panic absorbed further up cannot leave the draw path waiting on it.
func (r *syncRoad) attach(link *livedoor.Link, touch func(), st livedoor.State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.link = link
	r.touch = touch
	r.state = st
	if st.Kind == livedoor.Live || st.Kind == livedoor.WatchOnly {
		r.permit = st.Permit
	}
}

func (r *syncRoad) onState(st livedoor.State) {
	if prev := r.record(st); prev != nil {
		close(prev)
	}
}

// record holds the newest state and answers the channel that was closed over by it,
// which the caller closes outside the lock.
func (r *syncRoad) record(st livedoor.State) (prev chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = st
	if st.Kind == livedoor.Live || st.Kind == livedoor.WatchOnly {
		r.permit = st.Permit
	}
	prev = r.changed
	r.changed = make(chan struct{})
	return prev
}

// current is the state held now.
func (r *syncRoad) current() livedoor.State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Read is what the door says about the view on every draw. The ROAD rides along
// with it, read from the link's own state each time, so that a swap between the
// relay and the direct road shows on the next frame and the seam's static word
// is only ever the first paint.
func (r *syncRoad) Read() tui3.LiveRead {
	st := r.current()
	read := tui3.LiveRead{Kind: tui3.LiveOn, Road: st.Road}
	switch st.Kind {
	case livedoor.Live, livedoor.WatchOnly:
		read.Kind = tui3.LiveOn
	case livedoor.Connecting, livedoor.Reconnecting:
		read.Kind = tui3.LiveReconnecting
	case livedoor.WentOffline:
		read.Kind = tui3.LiveWentOffline
	case livedoor.Idle:
		read.Kind = tui3.LiveIdle
	case livedoor.Revoked:
		read.Kind = tui3.LiveRevoked
		read.Reason = st.Reason
	case livedoor.StateBudget:
		read.Kind = tui3.LiveBudget
	}
	return read
}

func (r *syncRoad) Changed() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changed
}

// toucher says whether the link is idle and what touches it.
func (r *syncRoad) toucher() (idle bool, touch func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	touch = r.touch
	if touch == nil && r.link != nil {
		touch = r.link.Touch
	}
	return r.state.Kind == livedoor.Idle, touch
}

func (r *syncRoad) Touch() {
	idle, touch := r.toucher()
	if idle || touch == nil {
		return
	}
	if !r.touching.CompareAndSwap(false, true) {
		return
	}
	guard.Go(liveTouchScope, func() {
		defer r.touching.Store(false)
		touch()
	})
}

func (r *syncRoad) Reconnect() {
	if r.link != nil {
		r.link.Touch()
	}
}

func (r *syncRoad) Road() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.Road
}

// allowed is what the other computer last said this one may do, and its name.
func (r *syncRoad) allowed() (pair.Permit, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.permit, r.host
}

func (r *syncRoad) Permit() (drive bool, readOnly string) {
	p, host := r.allowed()
	if p == (pair.Permit{}) {
		return false, ""
	}
	if p.Drive {
		return true, ""
	}
	if p.Watch {
		return false, chatlist.LiveWatchOnly(host)
	}
	return false, chatlist.LiveNotAllowedHere(host)
}

func (r *syncRoad) Dial(chat string) remote.Dialer {
	if r.link == nil {
		return nil
	}
	return remote.Dialer(r.link.Opener(chat))
}

func (r *syncRoad) Close() {
	if r.link != nil {
		r.link.Close()
	}
}
