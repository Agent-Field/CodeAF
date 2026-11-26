package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// ── OPENING ANOTHER COMPUTER'S CHAT LIVE ─────────────────────────────────────
//
// A chat a paired computer is running is served to the desktop AS A LOCAL-LOOKING
// AGENT: the engine dials that computer's live door, wraps the connection the
// way a join of a window's host is wrapped (appJoinExisting), and files it as an
// agent, so the chat page, its composer, asks, steer, queue and stop work
// unchanged. A live send is therefore `agents.send`, NEVER `chats.send`: the
// computer is on and the words reach its running conversation, so nothing is
// stored on a relay and the storage refusal that belongs to a waiting note does
// not apply (app_notes.go keeps it for the OFFLINE case).
//
// Everything is behind `config.RemoteControlOn()`, which is OFF by default: with
// it off the desktop is never told the feature exists (ChatsLiveOffered), so it
// sends no new call and the engine behaves as it did before this file.

// appLiveWindow is how many display entries one older-page read asks the host
// for. It is the host's own cap, so one read never materializes an unbounded
// batch over a live link.
const appLiveWindow = 200

// appLivePlaceholder marks an entry that stands in for a message of the
// person's older than the window held here: it keeps every id counted from the
// first message, and it is never shown.
const appLivePlaceholder = "\x00live"

// Tests stand in for the relay: the dialer a host's link is built from and the
// answer to "can this engine dial the live door at all".
var (
	appLiveDialable = func(b *appBackend) bool { return syncsetup.MayTalk(b.relayHome()) }
	appLiveNewLink  = func(b *appBackend, hostID, hostName string, active func() bool, onState func(livedoor.State)) (*livedoor.Link, error) {
		s, ok, err := syncsetup.Open(b.relayHome())
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("this computer is not paired")
		}
		return s.LiveLink(hostID, hostName, active, onState), nil
	}
	appLiveRoam = remote.Roam
)

var (
	_ appwire.ChatsLiveOpener = (*appBackend)(nil)
	_ appwire.ChatsLiveOffer  = (*appBackend)(nil)
	_ appwire.CallNoter       = (*appBackend)(nil)
)

// appLiveHost is the one link to a paired computer, shared by every chat this
// process views on it. users counts the connections and dials in flight: the
// link is closed, and the relay's tunnel with it, the moment the last is gone,
// so a view nobody is using costs the relay nothing (H1).
type appLiveHost struct {
	id, name string
	link     *livedoor.Link
	users    int // guarded by appBackend.liveMu
}

// appLiveRemote is what makes an agent a live view. Its fields are guarded by
// b.mu except win, which has its own lock.
type appLiveRemote struct {
	cell     string
	hostID   string
	hostName string
	// pure says no folder of this chat is on this computer: the agent has only
	// this view, and agents.list leaves it out.
	pure bool
	// state is the link's last word about the road to the host, and driver the
	// host's last word about who has the keyboard.
	state    livedoor.State
	hasState bool
	driver   appwire.LiveDriver

	win appLiveWindowCache
}

// appLiveWindowCache keeps the older part of the host's transcript already
// read, so scrolling back does not read it again. The tail is always read
// fresh: only the past is immutable.
type appLiveWindowCache struct {
	mu      sync.Mutex
	entries []session.DisplayEntry // host entries [base, base+len)
	base    int
	total   int
}

// liveOnly reports a view with no folder here. Callers hold b.mu.
func (a *appAgent) liveOnly() bool { return a.liveRemote != nil && a.liveRemote.pure }

// appFenceError is the refusal for a change that is not the app's to make: a
// lent factory stage's is the factory's, a live view's is the other computer's.
func appFenceError(conn *appConn, what string) error {
	if conn != nil && conn.Remote {
		return appwire.Errorf(appwire.CodeConflict, "this chat is running on another computer: its %s is set there, not from here", what)
	}
	return errLentStage(what)
}

// ChatsLiveOffered says the desktop may be told about chats.live: remote
// control is on and this engine can dial a live door.
func (b *appBackend) ChatsLiveOffered() bool {
	return config.RemoteControlOn() && appLiveDialable(b)
}

// appLiveRefusal puts a live-door failure in the host's own words, with the
// closed reason in the detail so the desktop can act on it without reading the
// sentence. A host that is simply not answering is unreachable; one that
// answered and said no is a conflict.
func appLiveRefusal(err error, host string) error {
	reason, ok := livedoor.ReasonOf(err)
	if !ok && errors.Is(err, livedoor.ErrHostOffline) {
		reason, ok = livedoor.ReasonNotRunning, true
	}
	if !ok {
		return &appwire.Error{Code: appwire.CodeUnreachable, Message: chatlist.LiveSaid("", host), Detail: appwire.LiveRefused{Kind: "live_refused", Reason: ""}}
	}
	code := appwire.CodeUnreachable
	switch reason {
	case livedoor.ReasonNotAllowed, livedoor.ReasonRemoved, livedoor.ReasonPermitChanged, livedoor.ReasonRemoteControlOff:
		code = appwire.CodeConflict
	}
	return &appwire.Error{Code: code, Message: chatlist.LiveSaid(reason, host), Detail: appwire.LiveRefused{Kind: "live_refused", Reason: reason}}
}

// OpenLive opens the chat in row cell live. The agent it answers is the agent
// the app opens for it: its id is the chat's local copy when this computer has
// one, else the cell's own id.
func (b *appBackend) OpenLive(p appwire.ChatOpenLiveParams) (appwire.Agent, error) {
	if !b.ChatsLiveOffered() {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeUnknownMethod, "this engine does not serve %s", "chats.open_live")
	}
	if !cell.ValidID(p.Cell) {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeNotFound, "that chat is not available")
	}
	row, found := b.chats.rowFor(p.Cell)
	if !found {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeNotFound, "This chat is no longer listed.")
	}
	if row.Here {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeConflict, "This chat is on this computer.")
	}
	if row.Holder == nil || row.Holder.ID == "" {
		return appwire.Agent{}, appwire.Errorf(appwire.CodeNotFound, "that chat is not available")
	}
	name := row.Holder.Name
	if row.Status != string(chatlist.Running) || (row.Online != nil && !*row.Online) {
		return appwire.Agent{}, appLiveRefusal(&livedoor.ReasonError{Reason: livedoor.ReasonNotRunning, Host: name}, name)
	}
	id := row.LocalAgent
	if id == "" {
		id = row.Cell
	}
	return b.openLiveAs(id, row)
}

func (b *appBackend) openLiveAs(id string, row appwire.ChatRow) (appwire.Agent, error) {
	hostID, hostName := row.Holder.ID, row.Holder.Name
	created := false
	var a *appAgent
	b.locked(func() {
		a = b.agents[id]
		if a == nil {
			a = b.agentLocked(id)
			created = true
		}
	})
	a.gate.Lock()
	defer a.gate.Unlock()
	var refusal error
	b.locked(func() {
		if a.live != nil && a.liveRemote == nil {
			refusal = appwire.Errorf(appwire.CodeConflict, "This chat is open on this computer.")
			return
		}
		if a.liveRemote == nil {
			r := &appLiveRemote{cell: row.Cell, hostID: hostID, hostName: hostName}
			if a.transcript == "" {
				// A FOLDER NO ONE WILL EVER FILL: every read of the disk this
				// agent's record makes lands on nothing, never on the working
				// directory an empty path would mean (session.DirOf("") is ".").
				a.transcript = filepath.Join(b.relayHome(), "live-views", row.Cell, cell.TranscriptPath)
				r.pure = true
			}
			a.liveRemote = r
			b.liveViews.Add(1)
		} else {
			a.liveRemote.hostID, a.liveRemote.hostName = hostID, hostName
		}
		a.windowMoved = false
		if a.view.Title == "" {
			a.view.Title = row.Title
		}
		if a.view.UpdatedAt.IsZero() {
			a.view.UpdatedAt = row.SavedAt
		}
		location := chatLocation(row)
		if old := a.view.Location; old != nil && old.Live != nil {
			location.Live = old.Live
		}
		a.view.Location = location
	})
	if refusal != nil {
		return appwire.Agent{}, refusal
	}
	view, opened, err := b.ensureLocked(a)
	if err != nil {
		if created {
			b.locked(func() {
				if a.live == nil && a.liveOnly() {
					delete(b.agents, id)
					b.liveViews.Add(-1)
				}
			})
		}
		return appwire.Agent{}, err
	}
	b.locked(func() { a.viewOpen = true })
	if opened {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	b.dress(&view)
	return view, nil
}

// liveLocked runs f under liveMu, released from a defer even if f panics.
func (b *appBackend) liveLocked(f func()) {
	b.liveMu.Lock()
	defer b.liveMu.Unlock()
	f()
}

// liveHostOf answers the shared link to a host, making it on first use, and
// counts the caller as a user of it; liveHostDone gives that back.
func (b *appBackend) liveHostOf(hostID, hostName string) (*appLiveHost, error) {
	var shared *appLiveHost
	b.liveLocked(func() {
		if h := b.liveHosts[hostID]; h != nil {
			h.users++
			shared = h
		}
	})
	if shared != nil {
		return shared, nil
	}
	h := &appLiveHost{id: hostID, name: hostName}
	link, err := appLiveNewLink(b, hostID, hostName, func() bool { return b.liveActive(hostID) }, func(st livedoor.State) { b.liveStateMoved(hostID, st) })
	if err != nil {
		return nil, err
	}
	h.link = link
	link.OnChannelEnd(func(chat, reason string) { b.liveChannelEnded(hostID, chat, reason) })
	var other *appLiveHost
	b.liveLocked(func() {
		if other = b.liveHosts[hostID]; other != nil {
			other.users++
			return
		}
		if b.liveHosts == nil {
			b.liveHosts = map[string]*appLiveHost{}
		}
		h.users = 1
		b.liveHosts[hostID] = h
	})
	if other != nil {
		link.Close()
		return other, nil
	}
	return h, nil
}

// liveHostDone gives one use of the link back, and closes the link when none is
// left: nothing holds the relay's tunnel open for a view nobody uses.
func (b *appBackend) liveHostDone(h *appLiveHost) {
	closing := false
	b.liveLocked(func() {
		h.users--
		closing = h.users <= 0
		if closing && b.liveHosts[h.id] == h {
			delete(b.liveHosts, h.id)
		}
	})
	if closing {
		h.link.Close()
	}
}

// closeLive closes every link when the app leaves.
func (b *appBackend) closeLive() {
	var hosts map[string]*appLiveHost
	b.liveLocked(func() {
		hosts = b.liveHosts
		b.liveHosts = nil
	})
	for _, h := range hosts {
		h.link.Close()
	}
}

// liveActive is the link's "is something happening" question: a turn running
// on a chat viewed through it. The link asks it holding its own lock, so it
// never waits for b.mu: a busy answer is the safe one, since it only keeps the
// connection a little longer.
func (b *appBackend) liveActive(hostID string) bool {
	if !b.mu.TryLock() {
		return true
	}
	defer b.mu.Unlock()
	for _, a := range b.agents {
		if r := a.liveRemote; r != nil && r.hostID == hostID && a.live != nil && (a.live.running || a.live.compacting || a.live.adopting > 0) {
			return true
		}
	}
	return false
}

// NoteCall is the idle rule's other half: any agents.* call that names a live
// agent counts as the person touching it, so the link does not detach under
// them. The call itself reconnects through the usual open when it needs to.
func (b *appBackend) NoteCall(method string, params json.RawMessage) {
	if b.liveViews.Load() == 0 {
		return
	}
	var named struct {
		ID    string `json:"id"`
		Agent string `json:"agent"`
	}
	if json.Unmarshal(params, &named) != nil {
		return
	}
	id := named.ID
	if id == "" {
		id = named.Agent
	}
	if id == "" {
		return
	}
	var hostID string
	b.locked(func() {
		if a := b.agents[id]; a != nil && a.liveRemote != nil && a.live != nil {
			hostID = a.liveRemote.hostID
		}
	})
	if hostID == "" {
		return
	}
	var h *appLiveHost
	b.liveLocked(func() { h = b.liveHosts[hostID] })
	if h != nil {
		guard.Go("app/live-touch", h.link.Touch)
	}
}

// ensureLive holds the live conversation for a: the connection another
// computer's chat is reached through, opened if none is held. Callers hold
// a.gate.
func (b *appBackend) ensureLive(a *appAgent, r *appLiveRemote) (appwire.Agent, bool, error) {
	var view appwire.Agent
	var held bool
	b.locked(func() { view, held = a.view, a.live != nil })
	if held {
		return view, false, nil
	}
	if b.isClosing() {
		return appwire.Agent{}, false, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	}
	if !config.RemoteControlOn() {
		return appwire.Agent{}, false, appwire.Errorf(appwire.CodeConflict, "Remote control is off on this computer.")
	}
	b.locked(func() {
		a.opening = true
		if a.openDone == nil {
			a.openDone = make(chan struct{})
		}
	})
	conn, err := b.dialLive(a, r)
	if err != nil {
		return appwire.Agent{}, false, err
	}
	b.unmarkTidy(view.ID)
	b.hold(a, conn, view.Workspace)
	b.watchLiveDriver(a, conn)
	return b.liveSay(a), true, nil
}

// dialLive opens the connection: a roaming client over the host's link, so a
// dropped connection is redialled by the client for itself, and the same
// wrapping a join of a window's host gets.
func (b *appBackend) dialLive(a *appAgent, r *appLiveRemote) (*appConn, error) {
	h, err := b.liveHostOf(r.hostID, r.hostName)
	if err != nil {
		return nil, appLiveRefusal(err, r.hostName)
	}
	hello := remote.Hello{Surface: devname.Name(b.relayHome()), Join: true}
	client, err := appLiveRoam(r.hostName, hello, remote.Roaming{Dial: h.link.Opener(r.cell), MaxLine: livedoor.MaxLine, ResendUnanswered: true, SpreadReturns: true})
	if err != nil {
		b.liveHostDone(h)
		return nil, appLiveRefusal(err, r.hostName)
	}
	var once sync.Once
	released := make(chan struct{})
	release := func() {
		once.Do(func() {
			close(released)
			_ = client.Close()
			b.liveHostDone(h)
			b.liveDetached(r)
		})
	}
	conn := appConnOf(client, release, false)
	// NOTHING OF THE HOST'S PATHS IS KEPT: its transcript and workspace are
	// folders on another computer, and every disk read the app makes of an
	// agent's own would otherwise be a read of a path that means something
	// else here.
	conn.Transcript, conn.Workspace, conn.Remote = "", "", true
	conn.Gone = released
	// THE ROAD'S STATE IS READ NOW: a link already open for another chat on this
	// host says nothing new when this chat joins it.
	road := h.link.State()
	b.locked(func() {
		r.driver = appLiveDriverOf(client.Driver())
		r.state, r.hasState = road, true
	})
	return conn, nil
}

func appLiveDriverOf(d remote.Driver) appwire.LiveDriver {
	return appwire.LiveDriver{Yours: d.Yours, Machine: d.Machine, Here: d.Here}
}

// watchLiveDriver says who has the keyboard whenever the host's answer moves.
func (b *appBackend) watchLiveDriver(a *appAgent, conn *appConn) {
	agent, ok := conn.Conv.(*remote.Agent)
	if !ok {
		return
	}
	client := agent.Client()
	guard.Go("app/live-driver", func() {
		for {
			changed := client.DriverChanged()
			select {
			case <-conn.Gone:
				return
			case <-changed:
			}
			b.locked(func() {
				if r := a.liveRemote; r != nil {
					r.driver = appLiveDriverOf(client.Driver())
				}
			})
			b.emitLive(a)
		}
	})
}

// liveStateMoved is the link's word about a host reaching every live view on
// it.
func (b *appBackend) liveStateMoved(hostID string, st livedoor.State) {
	var moved []*appAgent
	b.locked(func() {
		for _, a := range b.agents {
			if r := a.liveRemote; r != nil && r.hostID == hostID {
				r.state, r.hasState = st, true
				moved = append(moved, a)
			}
		}
	})
	for _, a := range moved {
		b.emitLive(a)
		if st.Kind == livedoor.Revoked {
			b.sayLiveNotice(a, st.Reason)
		}
	}
}

// liveChannelEnded is a view's channel closing. A quiet spell is the link's
// own detach: the connection is let go the way an idle conversation is, and the
// next call to the agent opens it again. Every other reason the client's own
// redial answers, or says.
func (b *appBackend) liveChannelEnded(hostID, chat, reason string) {
	var a *appAgent
	b.locked(func() {
		for _, other := range b.agents {
			if r := other.liveRemote; r != nil && r.hostID == hostID && r.cell == chat {
				a = other
				return
			}
		}
	})
	if a == nil {
		return
	}
	if reason == livedoor.ReasonIdle {
		guard.Go("app/live-idle", func() { b.liveIdle(a) })
		return
	}
	switch reason {
	case livedoor.ReasonHolderGone, livedoor.ReasonNotRunning, livedoor.ReasonNotAllowed, livedoor.ReasonRemoved, livedoor.ReasonHostClosing, livedoor.ReasonFrameTooLarge:
		b.sayLiveNotice(a, reason)
	}
}

func (b *appBackend) liveIdle(a *appAgent) {
	a.gate.Lock()
	defer a.gate.Unlock()
	running := false
	b.locked(func() { running = a.live != nil && a.live.running })
	if running {
		return
	}
	b.release(a)
	// The window is still looking at it: letting go of the connection is not
	// closing the view.
	b.locked(func() { a.viewOpen = true })
}

// liveDetached is a connection let go. A view that was healthy reads idle from
// then on, which is the truth: nothing is held, and the next touch reconnects.
// A view whose road already said offline or revoked keeps saying that.
func (b *appBackend) liveDetached(r *appLiveRemote) {
	var a *appAgent
	b.locked(func() {
		for _, other := range b.agents {
			if other.liveRemote == r {
				a = other
				switch r.state.Kind {
				case livedoor.Connecting, livedoor.Live, livedoor.WatchOnly, livedoor.StateBudget, livedoor.Idle:
					r.state, r.hasState = livedoor.State{Kind: livedoor.Idle, Reason: livedoor.ReasonIdle, Since: time.Now(), Permit: r.state.Permit, Road: r.state.Road}, true
				}
				return
			}
		}
	})
	if a != nil {
		guard.Go("app/live-detached", func() { b.emitLive(a) })
	}
}

func (b *appBackend) sayLiveNotice(a *appAgent, reason string) {
	var host string
	b.locked(func() {
		if a.liveRemote != nil {
			host = a.liveRemote.hostName
		}
	})
	line := chatlist.LiveSaid(reason, host)
	b.sayLocalNotice(a, appwire.NoticeSync, "live:"+reason, line)
}

// appLiveOf is the Location.live fact of a view, spelled as the desktop reads
// it: a closed set of states, who has the keyboard, and, for a view that may
// read but not type, the sentence that says why exactly as the host's words
// give it.
func appLiveOf(r *appLiveRemote) *appwire.LocationLive {
	st := r.state
	live := &appwire.LocationLive{Road: st.Road}
	if !r.hasState {
		live.State = appwire.LiveStateReconnecting
	} else {
		switch st.Kind {
		case livedoor.Live:
			live.State = appwire.LiveStateLive
		case livedoor.WatchOnly, livedoor.StateBudget:
			live.State = appwire.LiveStateWatching
			words := chatlist.LiveWatchOnly(r.hostName)
			switch {
			case st.Kind == livedoor.StateBudget || st.Reason == livedoor.ReasonBudget:
				words = chatlist.LiveBudget
			case st.Reason == livedoor.ReasonPermitChanged:
				words = chatlist.LiveSaid(livedoor.ReasonPermitChanged, r.hostName)
			}
			live.Permit = &appwire.LivePermit{Watch: true, Drive: false, Reason: words}
		case livedoor.WentOffline, livedoor.Revoked:
			live.State = appwire.LiveStateOffline
			live.SeenAt = st.Since.UTC().Format(time.RFC3339)
		case livedoor.Idle:
			live.State = appwire.LiveStateIdle
		default:
			live.State = appwire.LiveStateReconnecting
		}
	}
	if r.driver != (appwire.LiveDriver{}) {
		d := r.driver
		live.Driver = &d
	}
	return live
}

// liveSay lays the view's live fact on its Location and answers the agent as it
// now stands, emitting nothing.
func (b *appBackend) liveSay(a *appAgent) (view appwire.Agent) {
	b.locked(func() { view, _ = b.liveSayLocked(a) })
	return view
}

func (b *appBackend) liveSayLocked(a *appAgent) (appwire.Agent, bool) {
	r := a.liveRemote
	if r == nil {
		return a.view, false
	}
	live := appLiveOf(r)
	location := a.view.Location
	if location == nil {
		location = &appwire.Location{Cell: r.cell}
	}
	if equalLive(location.Live, live) {
		return a.view, false
	}
	next := *location
	next.Live = live
	a.view.Location = &next
	return a.view, true
}

func equalLive(x, y *appwire.LocationLive) bool {
	if x == nil || y == nil {
		return x == y
	}
	a, _ := json.Marshal(x)
	c, _ := json.Marshal(y)
	return string(a) == string(c)
}

// emitLive says the view's agent when its live fact moved.
func (b *appBackend) emitLive(a *appAgent) {
	var view appwire.Agent
	changed := false
	b.locked(func() { view, changed = b.liveSayLocked(a) })
	if changed {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
}

// LiveKeyboard answers who has the keyboard of a live view and, when asked,
// takes it. The host's own words come back unchanged when it refuses.
func (b *appBackend) LiveKeyboard(p appwire.ChatLiveKeyboardParams) (appwire.ChatLiveKeyboardResult, error) {
	a, err := b.find(p.Agent)
	if err != nil {
		return appwire.ChatLiveKeyboardResult{}, err
	}
	var r *appLiveRemote
	b.locked(func() { r = a.liveRemote })
	if r == nil {
		// NOT A VIEW OF ANOTHER COMPUTER'S CHAT: it may still be a chat THIS
		// desktop holds for others, whose keyboard the desktop's own window can
		// take back (app_held_keyboard.go).
		return b.heldKeyboardCall(a, p)
	}
	if _, _, err := b.ensure(a); err != nil {
		return appwire.ChatLiveKeyboardResult{}, err
	}
	live := b.liveOf(a)
	if live == nil {
		return appwire.ChatLiveKeyboardResult{}, appwire.Errorf(appwire.CodeUnreachable, "%s", chatlist.LiveSaid("", r.hostName))
	}
	agent, ok := live.conn.Conv.(*remote.Agent)
	if !ok {
		return appwire.ChatLiveKeyboardResult{}, appwire.Errorf(appwire.CodeConflict, "this chat is not open live")
	}
	client := agent.Client()
	if p.Take {
		if err := client.Take(); err != nil {
			return appwire.ChatLiveKeyboardResult{}, appwire.Errorf(appwire.CodeConflict, "%s", err.Error())
		}
	}
	driver := appLiveDriverOf(client.Driver())
	b.locked(func() { r.driver = driver })
	b.emitLive(a)
	return appwire.ChatLiveKeyboardResult{Driver: driver}, nil
}

// ── READING A LIVE CHAT'S HISTORY ───────────────────────────────────────────

// liveEntries serves agents.entries for a live view: the host's tail, read
// fresh, laid under the older part already read. Entry ids count the person's
// messages from the first, so the part of the past not held here is stood in
// for by placeholders that are never shown.
func (b *appBackend) liveEntries(a *appAgent, before string, limit int) (appwire.EntriesResult, error) {
	if _, _, err := b.ensure(a); err != nil {
		return appwire.EntriesResult{}, err
	}
	live := b.liveOf(a)
	if live == nil {
		return appwire.EntriesResult{}, appwire.Errorf(appwire.CodeUnreachable, "the chat is not connected right now; try again in a moment")
	}
	agent, ok := live.conn.Conv.(*remote.Agent)
	if !ok {
		return appwire.EntriesResult{}, appwire.Errorf(appwire.CodeConflict, "this chat is not open live")
	}
	var r *appLiveRemote
	var id string
	var at time.Time
	var users int
	marks := map[int]appTurnMark{}
	b.locked(func() {
		r, id, at, users = a.liveRemote, a.view.ID, a.view.UpdatedAt, a.users
		for n, mark := range a.marks {
			marks[n] = mark
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), chatPreviewWithin)
	defer cancel()
	tail, err := agent.TranscriptPage(ctx, 0, appLiveWindow)
	if err != nil {
		return appwire.EntriesResult{}, appLiveReadError(err, r.hostName)
	}
	r.win.mu.Lock()
	defer r.win.mu.Unlock()
	if tail.Total < r.win.total {
		r.win.entries, r.win.base = nil, 0
	}
	r.win.total = tail.Total
	for range 16 {
		record, held := r.win.record(tail, users)
		page, more, err := appEntryPage(id, session.Record{Entries: record}, at, marks, before, limit, nil)
		if err != nil {
			return appwire.EntriesResult{}, appEntriesBusy(err)
		}
		touched := false
		kept := page[:0]
		for _, entry := range page {
			if entry.Text == appLivePlaceholder {
				touched = true
				continue
			}
			kept = append(kept, entry)
		}
		if touched && held > 0 {
			older, err := agent.TranscriptPage(ctx, held, appLiveWindow)
			if err != nil {
				return appwire.EntriesResult{}, appLiveReadError(err, r.hostName)
			}
			if len(older.Entries) == 0 {
				return appwire.EntriesResult{Entries: kept}, nil
			}
			r.win.prepend(older)
			continue
		}
		// Placeholders left over with nothing older to read are the messages a
		// compaction took: there is nothing more to show.
		return appwire.EntriesResult{Entries: kept, More: more && !(touched && held == 0)}, nil
	}
	return appwire.EntriesResult{}, appwire.Errorf(appwire.CodeBusy, "this chat's history is still being read; ask again in a moment")
}

func appLiveReadError(err error, host string) error {
	var wire *appwire.Error
	if errors.As(err, &wire) {
		return wire
	}
	return appwire.Errorf(appwire.CodeUnreachable, "%s", chatlist.LiveSaid("", host))
}

// record is the host's transcript as the app tells it: placeholders for the
// messages older than what is held, then the held entries, cut at the first
// message of the person's so every answer counts from its own message. held is
// the host's index of the first entry read, cut or not: an older page is read
// ending there.
func (w *appLiveWindowCache) record(tail remote.Page, users int) (entries []session.DisplayEntry, held int) {
	read, base := tail.Entries, tail.Base
	if n := len(w.entries); n > 0 && w.base+n == tail.Base {
		read = append(append([]session.DisplayEntry(nil), w.entries...), tail.Entries...)
		base = w.base
	}
	cut := 0
	for cut < len(read) && read[cut].Role != "user" {
		cut++
	}
	if cut == len(read) && base > 0 {
		// No message of the person's in what is read: it is all the end of an
		// answer whose message is older, and none of it can be told alone.
		return placeholders(users), base
	}
	kept := read[cut:]
	in := 0
	for _, entry := range kept {
		if entry.Role == "user" {
			in++
		}
	}
	return append(placeholders(users-in), kept...), base
}

func placeholders(n int) []session.DisplayEntry {
	if n <= 0 {
		return nil
	}
	out := make([]session.DisplayEntry, n)
	for i := range out {
		out[i] = session.DisplayEntry{Role: "user", Text: appLivePlaceholder}
	}
	return out
}

// prepend keeps an older page as the held past, joined to what was held.
func (w *appLiveWindowCache) prepend(older remote.Page) {
	if n := len(w.entries); n > 0 && w.base == older.Base+len(older.Entries) {
		w.entries = append(append([]session.DisplayEntry(nil), older.Entries...), w.entries...)
	} else {
		w.entries = append([]session.DisplayEntry(nil), older.Entries...)
	}
	w.base = older.Base
}

// ── HEARING WHO HOLDS A CHAT, AND SWAPPING TO A VIEW OF THEM ────────────────

// liveSwapWanted says a window that was viewing a chat the desktop held, and
// had it taken by another computer, should now view the new holder instead of
// a stale copy (Superseded -> live). Callers hold no lock.
func (b *appBackend) liveSwapWanted(a *appAgent, r appwire.ChatRow) bool {
	if !r.CanLive || r.Here {
		return false
	}
	wanted := false
	b.locked(func() { wanted = a.windowMoved && a.viewOpen && a.live == nil })
	return wanted && b.ChatsLiveOffered()
}

func (b *appBackend) liveSwap(r appwire.ChatRow) {
	guard.Go("app/live-swap", func() {
		// A swap that fails leaves the window as the move left it: a copy that
		// says who holds the chat, with Enter to ask again.
		_, _ = b.OpenLive(appwire.ChatOpenLiveParams{Cell: r.Cell})
	})
}
