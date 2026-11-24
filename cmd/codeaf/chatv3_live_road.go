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
	r.mu.Lock()
	r.link = link
	r.touch = touch
	r.state = st
	if st.Kind == livedoor.Live || st.Kind == livedoor.WatchOnly {
		r.permit = st.Permit
	}
	r.mu.Unlock()
}

func (r *syncRoad) onState(st livedoor.State) {
	r.mu.Lock()
	r.state = st
	if st.Kind == livedoor.Live || st.Kind == livedoor.WatchOnly {
		r.permit = st.Permit
	}
	prev := r.changed
	r.changed = make(chan struct{})
	r.mu.Unlock()
	if prev != nil {
		close(prev)
	}
}

func (r *syncRoad) Read() tui3.LiveRead {
	r.mu.Lock()
	st := r.state
	r.mu.Unlock()
	switch st.Kind {
	case livedoor.Live, livedoor.WatchOnly:
		return tui3.LiveRead{Kind: tui3.LiveOn}
	case livedoor.Connecting, livedoor.Reconnecting:
		return tui3.LiveRead{Kind: tui3.LiveReconnecting}
	case livedoor.WentOffline:
		return tui3.LiveRead{Kind: tui3.LiveWentOffline}
	case livedoor.Idle:
		return tui3.LiveRead{Kind: tui3.LiveIdle}
	case livedoor.Revoked:
		return tui3.LiveRead{Kind: tui3.LiveRevoked, Reason: st.Reason}
	case livedoor.StateBudget:
		return tui3.LiveRead{Kind: tui3.LiveBudget}
	default:
		return tui3.LiveRead{Kind: tui3.LiveOn}
	}
}

func (r *syncRoad) Changed() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.changed
}

func (r *syncRoad) Touch() {
	r.mu.Lock()
	idle := r.state.Kind == livedoor.Idle
	touch := r.touch
	if touch == nil && r.link != nil {
		touch = r.link.Touch
	}
	r.mu.Unlock()
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

func (r *syncRoad) Permit() (drive bool, readOnly string) {
	r.mu.Lock()
	p := r.permit
	host := r.host
	r.mu.Unlock()
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
