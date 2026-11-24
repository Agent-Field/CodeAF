package main

import (
	"context"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/viewcache"
)

const (
	livePruneScope  = "chatv3/live-prune"
	liveEventsScope = "chatv3/live-events"
)

// remoteControlWanted says whether this build's remote control is switched on.
// Today it reads CODEAF_REMOTE_CONTROL (1, on, true, yes; anything else, unset
// or empty is off); it is the ONE place that later calls
// config.RemoteControlOn(). Default OFF.
func remoteControlWanted() bool {
	switch strings.ToLower(strings.TrimSpace(env.Get("CODEAF_REMOTE_CONTROL"))) {
	case "1", "on", "true", "yes":
		return true
	default:
		return false
	}
}

// liveRoad is everything the terminal needs from the viewer side of one other
// computer's connection, in this package's words.
type liveRoad interface {
	// Dial returns the dialler remote.Roam redials by for one chat.
	Dial(chat string) remote.Dialer
	// Read is the state now, answered from fields already held because it is asked on the draw path.
	Read() tui3.LiveRead
	// Changed is closed the next time Read's answer moves.
	Changed() <-chan struct{}
	// Touch tells the link a person or a running turn is active so idle detach can read it.
	Touch()
	// Reconnect reattaches after an idle detach, off the update loop.
	Reconnect()
	// Road is the word for how the bytes travel, said by the door so this package never spells it.
	Road() string
	// Permit is what the other computer lets this one do: may it type, and the sentence that says why not.
	Permit() (drive bool, readOnly string)
	// Older returns up to limit entries before the absolute index, oldest first, whether older ones remain, and the absolute index of the first returned.
	Older(ctx context.Context, chat string, before, limit int) (entries []session.DisplayEntry, base int, more bool, err error)
}

// liveRoads finds the road to one other computer by its directory device id,
// and whether one exists. It is the one injection point: nil (the state of
// every build until the adapter lands) means no road, so remote control is
// absent.
var liveRoads func(device string) (road liveRoad, name string, ok bool)

// liveDoor opens a chat held on another of this person's computers as a live
// view when that computer can be reached, and falls back to the viewer's
// last-seen copy when it is offline.
type liveDoor struct {
	home  string
	cache *viewcache.Cache
	roads func(device string) (road liveRoad, name string, ok bool)
	clock viewcache.Clock
	dial  func(road liveRoad, row chatlist.Row) (tui3.Agent, remote.Welcome, error)
}

func (d *liveDoor) lookupRoad(device string) (liveRoad, string, bool) {
	if d.roads != nil {
		return d.roads(device)
	}
	if liveRoads != nil {
		return liveRoads(device)
	}
	return nil, "", false
}

// Open dials a live view of row on the computer holding it, or opens the
// local last-seen copy read-only when that computer is offline.
func (d *liveDoor) Open(ctx context.Context, row chatlist.Row) (tui3.Conversation, error) {
	road, name, ok := d.lookupRoad(row.DeviceID)
	host := strings.TrimSpace(name)
	if host == "" {
		host = strings.TrimSpace(row.Device)
	}
	if row.Status == chatlist.Off {
		return d.openOffline(host, row)
	}
	if !ok || road == nil {
		return tui3.Conversation{}, tui3.ErrNoLiveView
	}

	var (
		baseAgent tui3.Agent
		welcome   remote.Welcome
		link      tui3.LinkSeam
		taskRoom  func(uint64, int) (session.TaskRecord, error)
		shut      func() error
	)
	if d.dial != nil {
		var err error
		baseAgent, welcome, err = d.dial(road, row)
		if err != nil {
			if isNotRunningError(err) {
				return d.openOffline(host, row)
			}
			return tui3.Conversation{}, err
		}
		if remoteAgent, ok := baseAgent.(*remote.Agent); ok {
			client := remoteAgent.Client()
			seams := newHostSeams(client)
			link = hostLink(seams)
			taskRoom = remoteAgent.TaskRoom
			shut = client.Close
			baseAgent = countedAgent(remoteAgent)
		} else if room, ok := baseAgent.(interface {
			TaskRoom(uint64, int) (session.TaskRecord, error)
		}); ok {
			taskRoom = room.TaskRoom
		}
		link.Driving = liveDriving(link.Driving, road)
	} else {
		dialer := road.Dial(row.Cell)
		if dialer == nil {
			return d.openOffline(host, row)
		}
		// THE HOST ROUTER REWRITES THE HELLO to bind the holder's transcript and
		// workspace, so the viewer sends only an unwindowed hello and lets Roam
		// carry resume cursors across redials.
		client, err := remote.Roam(host, remote.Hello{Window: false}, remote.Roaming{Dial: dialer})
		if err != nil {
			if isNotRunningError(err) {
				return d.openOffline(host, row)
			}
			return tui3.Conversation{}, err
		}
		welcome = client.Welcome()
		if shown := strings.TrimSpace(welcome.Machine); shown != "" && host == "" {
			host = shown
		}
		if host != "" {
			client.SetDisplayName(host)
		}
		seams := newHostSeams(client)
		link = hostLink(seams)
		link.Driving = liveDriving(link.Driving, road)
		remoteAgent := client.Agent()
		taskRoom = remoteAgent.TaskRoom
		baseAgent = countedAgent(remoteAgent)
		shut = client.Close
	}

	agent := &liveAgent{
		Agent:     baseAgent,
		road:      road,
		cache:     d.cache,
		host:      host,
		device:    row.DeviceID,
		cell:      row.Cell,
		title:     row.Title,
		workspace: welcome.Workspace,
		shut:      shut,
	}
	return tui3.Conversation{
		Agent:       agent,
		SessionFile: welcome.SessionFile,
		Workspace:   welcome.Workspace,
		Resumed:     welcome.Resumed,
		Notice:      hostEntryNotice(welcome),
		Link:        &link,
		TaskRoom:    taskRoom,
		Live: &tui3.LiveSeam{
			Host:      host,
			Device:    row.DeviceID,
			Cell:      row.Cell,
			Road:      road.Road(),
			Row:       row,
			Read:      road.Read,
			Touch:     road.Touch,
			Reconnect: road.Reconnect,
			Changed:   road.Changed,
		},
	}, nil
}

func (d *liveDoor) openOffline(name string, row chatlist.Row) (tui3.Conversation, error) {
	if d.cache == nil {
		return tui3.Conversation{}, tui3.ErrNoLiveView
	}
	snapshot, ok := d.cache.Load(row.DeviceID, row.Cell)
	if !ok {
		return tui3.Conversation{}, tui3.ErrNoLiveView
	}
	host := strings.TrimSpace(name)
	if host == "" {
		host = strings.TrimSpace(snapshot.HostName)
	}
	if host == "" {
		host = strings.TrimSpace(row.Device)
	}
	seenAt := snapshot.SeenAt
	agent := &viewcacheAgent{snapshot: snapshot, host: host}
	link := tui3.LinkSeam{
		SendRefusal: func() string { return chatlist.LiveNotSentOffline(host) },
	}
	return tui3.Conversation{
		Agent:     agent,
		Workspace: snapshot.Workspace,
		Resumed:   true,
		Link:      &link,
		Live: &tui3.LiveSeam{
			Host:   host,
			Device: row.DeviceID,
			Cell:   row.Cell,
			Row:    row,
			Read: func() tui3.LiveRead {
				return tui3.LiveRead{Kind: tui3.LiveCopy, SeenAt: seenAt}
			},
		},
	}, nil
}

func isNotRunningError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not running") || strings.Contains(msg, "not-running")
}

// liveDriving combines the remote client's keyboard arbitration with the road's
// watch/type permit so a watch-only viewer sees why typing is closed and a
// drive-capable viewer sees which other computer holds the keyboard.
func liveDriving(base func() tui3.Driving, road liveRoad) func() tui3.Driving {
	return func() tui3.Driving {
		driving := tui3.Driving{Yours: true}
		if base != nil {
			driving = base()
		}
		if !driving.Yours && driving.Holder == "" {
			driving.Holder = driving.Machine
		}
		if road != nil {
			if drive, readOnly := road.Permit(); !drive {
				driving.ReadOnly = readOnly
			} else {
				driving.ReadOnly = ""
			}
		}
		return driving
	}
}

// liveAgent wraps a live remote agent with older-history paging through the
// road, quiet-beat viewer caching, and local-only client teardown so leaving a
// live view never stops the chat on the computer running it.
type liveAgent struct {
	tui3.Agent
	road      liveRoad
	cache     *viewcache.Cache
	host      string
	device    string
	cell      string
	title     string
	workspace string
	shut      func() error
	shutOnce  sync.Once
	shutErr   error

	mu          sync.Mutex
	done        chan struct{}
	base        int
	turns       int
	savedLen    int
	lastEntries []session.DisplayEntry
}

var _ tui3.OlderReader = (*liveAgent)(nil)

// Older fetches a page of entries before the absolute index through the road
// and updates TranscriptBase to the absolute index of the first entry returned.
func (a *liveAgent) Older(ctx context.Context, before, limit int) ([]session.DisplayEntry, bool, error) {
	if a.road == nil {
		return nil, false, nil
	}
	entries, base, more, err := a.road.Older(ctx, a.cell, before, limit)
	if err != nil {
		return nil, false, err
	}
	a.setBase(base)
	return entries, more, nil
}

// TranscriptBase returns the absolute transcript index of the first entry held.
func (a *liveAgent) TranscriptBase() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.base
}

func (a *liveAgent) setBase(base int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.base = base
}

func (a *liveAgent) beginTurn() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turns++
}

func (a *liveAgent) finishTurn() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.turns > 0 {
		a.turns--
	}
	return a.turns == 0
}

func (a *liveAgent) noteEntries(entries []session.DisplayEntry) (grew, idle bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(entries) > 0 {
		a.lastEntries = append([]session.DisplayEntry(nil), entries...)
	}
	idle = a.turns == 0
	if idle && len(entries) > a.savedLen {
		a.savedLen = len(entries)
		grew = true
	}
	return grew, idle
}

func (a *liveAgent) rememberSaved(entries []session.DisplayEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(entries) > 0 {
		a.lastEntries = append([]session.DisplayEntry(nil), entries...)
	}
	if len(entries) > a.savedLen {
		a.savedLen = len(entries)
	}
}

func (a *liveAgent) heldEntries() []session.DisplayEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]session.DisplayEntry(nil), a.lastEntries...)
}

func (a *liveAgent) snapshot(entries []session.DisplayEntry) viewcache.Snapshot {
	title := strings.TrimSpace(a.Agent.Title())
	if title == "" {
		title = a.title
	}
	base := a.TranscriptBase()
	return viewcache.Snapshot{
		Host:      a.device,
		HostName:  a.host,
		Chat:      a.cell,
		Title:     title,
		Model:     a.Agent.Model(),
		Workspace: a.workspace,
		Base:      base,
		Total:     base + len(entries),
		Entries:   append([]session.DisplayEntry(nil), entries...),
	}
}

func (a *liveAgent) saveQuiet(entries []session.DisplayEntry) {
	if a.cache == nil {
		return
	}
	_ = a.cache.Save(a.device, a.cell, a.snapshot(entries))
}

func (a *liveAgent) saveAfterTurn() {
	if !a.finishTurn() || a.cache == nil {
		return
	}
	entries := a.Agent.Transcript()
	a.rememberSaved(entries)
	if len(entries) == 0 {
		entries = a.heldEntries()
	}
	a.saveQuiet(entries)
}

func (a *liveAgent) saveNow() {
	if a.cache == nil {
		return
	}
	entries := a.Agent.Transcript()
	a.rememberSaved(entries)
	if len(entries) == 0 {
		entries = a.heldEntries()
	}
	_ = a.cache.SaveNow(a.device, a.cell, a.snapshot(entries))
}

// closing is closed once this view lets go of its connection, so a forwarder
// that is waiting on a reader that has gone away stops instead of parking for
// the life of the process.
func (a *liveAgent) closing() chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done == nil {
		a.done = make(chan struct{})
	}
	return a.done
}

func (a *liveAgent) watchEvents(src <-chan session.Event) <-chan session.Event {
	if src == nil {
		return nil
	}
	a.beginTurn()
	out := make(chan session.Event)
	done := a.closing()
	guard.Go(liveEventsScope, func() {
		settled := false
		defer func() {
			if !settled {
				a.saveAfterTurn()
			}
			close(out)
		}()
		for ev := range src {
			if ev.Kind == session.EventTurnDone && !settled {
				settled = true
				a.saveAfterTurn()
			}
			select {
			case out <- ev:
			case <-done:
				return
			}
		}
	})
	return out
}

// Submit runs a turn on the remote host and saves the viewer cache once the
// turn settles, never per streaming delta.
func (a *liveAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	events, err := a.Agent.Submit(ctx, text)
	if err != nil {
		return nil, err
	}
	return a.watchEvents(events), nil
}

// SubmitImage runs an image turn on the remote host and saves the viewer cache
// once the turn settles.
func (a *liveAgent) SubmitImage(ctx context.Context, text string, images []session.Image) (<-chan session.Event, error) {
	events, err := a.Agent.SubmitImage(ctx, text, images)
	if err != nil {
		return nil, err
	}
	return a.watchEvents(events), nil
}

// FollowUp queues a follow-up turn and saves the viewer cache when it settles.
func (a *liveAgent) FollowUp(text string) (<-chan session.Event, error) {
	events, err := a.Agent.FollowUp(text)
	if err != nil {
		return nil, err
	}
	return a.watchEvents(events), nil
}

// Transcript reads the conversation so far and updates the viewer cache when
// the transcript has grown on a quiet beat with no turn running.
func (a *liveAgent) Transcript() []session.DisplayEntry {
	entries := a.Agent.Transcript()
	if grew, _ := a.noteEntries(entries); grew {
		a.saveQuiet(entries)
	}
	return entries
}

// AttachReplay joins an in-flight turn when the underlying agent supports it,
// wrapping the event stream so the viewer cache updates when the turn ends.
func (a *liveAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	if door, ok := a.Agent.(interface {
		AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func())
	}); ok {
		entries, events, stop := door.AttachReplay()
		if events != nil {
			a.rememberSaved(entries)
			return entries, a.watchEvents(events), stop
		}
		if grew, _ := a.noteEntries(entries); grew {
			a.saveQuiet(entries)
		}
		return entries, nil, stop
	}
	return a.Transcript(), nil, func() {}
}

// Attach joins an in-flight turn's stream when the underlying agent supports it.
func (a *liveAgent) Attach() (<-chan session.Event, bool, func()) {
	if door, ok := a.Agent.(interface {
		Attach() (<-chan session.Event, bool, func())
	}); ok {
		events, running, stop := door.Attach()
		if !running || events == nil {
			return events, running, stop
		}
		return a.watchEvents(events), true, stop
	}
	return nil, false, func() {}
}

// WorkOutlivesExit reports that a live conversation keeps running on its host
// computer after this viewer leaves.
func (a *liveAgent) WorkOutlivesExit() bool { return true }

func (a *liveAgent) closeClient() error {
	a.shutOnce.Do(func() {
		close(a.closing())
		if a.shut != nil {
			a.shutErr = a.shut()
		}
	})
	return a.shutErr
}

// Detach saves the last-seen copy immediately and closes only the local Roam
// client so the chat continues undisturbed on the other computer.
func (a *liveAgent) Detach() error {
	a.saveNow()
	return a.closeClient()
}

// Close saves the last-seen copy immediately and closes only the local Roam
// client; a viewer never ends the chat on the other computer.
func (a *liveAgent) Close() error {
	a.saveNow()
	return a.closeClient()
}

// errLiveCopy is the refusal returned when a person tries to submit a message
// into a read-only offline copy of another computer's chat.
type errLiveCopy struct {
	host string
}

func (e errLiveCopy) Error() string {
	return chatlist.LiveNotSentOffline(e.host)
}

// viewcacheAgent serves a last-seen snapshot read-only when the computer that
// holds the chat is offline.
type viewcacheAgent struct {
	snapshot viewcache.Snapshot
	host     string
}

var _ tui3.Agent = (*viewcacheAgent)(nil)

func (v *viewcacheAgent) Submit(context.Context, string) (<-chan session.Event, error) {
	return nil, errLiveCopy{host: v.host}
}

func (v *viewcacheAgent) SubmitImage(context.Context, string, []session.Image) (<-chan session.Event, error) {
	return nil, errLiveCopy{host: v.host}
}

func (v *viewcacheAgent) FollowUp(string) (<-chan session.Event, error) {
	return nil, errLiveCopy{host: v.host}
}

func (v *viewcacheAgent) Interrupt()                                                {}
func (v *viewcacheAgent) InterruptFor(session.StopDoor)                             {}
func (v *viewcacheAgent) Compact(context.Context) error                             { return nil }
func (v *viewcacheAgent) Close() error                                              { return nil }
func (v *viewcacheAgent) Detach() error                                             { return nil }
func (v *viewcacheAgent) WorkOutlivesExit() bool                                    { return true }
func (v *viewcacheAgent) Model() string                                             { return v.snapshot.Model }
func (v *viewcacheAgent) SetModel(string)                                           {}
func (v *viewcacheAgent) SetContextWindow(int)                                      {}
func (v *viewcacheAgent) ReasoningFor(string) string                                { return "" }
func (v *viewcacheAgent) SetReasoningFor(string, string)                            {}
func (v *viewcacheAgent) ResolveConsent(uint64, bool)                               {}
func (v *viewcacheAgent) ResolveConsentRemember(uint64, bool, session.ConsentScope) {}
func (v *viewcacheAgent) ResolveHarness(uint64, bool, string)                       {}
func (v *viewcacheAgent) ResolveConnect(string, bool)                               {}
func (v *viewcacheAgent) ResolveConnectKey(string, string)                          {}
func (v *viewcacheAgent) NoteConnected(string, string)                              {}
func (v *viewcacheAgent) Title() string                                             { return v.snapshot.Title }
func (v *viewcacheAgent) Usage() session.Usage                                      { return session.Usage{} }
func (v *viewcacheAgent) ContextTokens() int                                        { return 0 }
func (v *viewcacheAgent) EarlierHistory() session.EarlierHistory                    { return session.EarlierHistory{} }

func (v *viewcacheAgent) Transcript() []session.DisplayEntry {
	return append([]session.DisplayEntry(nil), v.snapshot.Entries...)
}

func (v *viewcacheAgent) AttachReplay() ([]session.DisplayEntry, <-chan session.Event, func()) {
	ch := make(chan session.Event)
	close(ch)
	return v.Transcript(), ch, func() {}
}

// wireLive attaches the live-view opener to Options when remote control is
// switched on, a road lookup is installed, and the window is not itself a
// hosted session.
func wireLive(o *tui3.Options, home string) {
	if o == nil || o.Host != "" || !remoteControlWanted() || liveRoads == nil {
		return
	}
	cache := viewcache.Open(home)
	guard.Go(livePruneScope, cache.Prune)
	if o.OpenLive == nil {
		door := &liveDoor{
			home:  home,
			cache: cache,
			roads: liveRoads,
		}
		o.OpenLive = door.Open
	}
}

// forgetViewed removes every last-seen copy kept for one computer when that
// computer is removed from the person's devices.
func forgetViewed(home, device string) {
	if strings.TrimSpace(home) == "" || strings.TrimSpace(device) == "" {
		return
	}
	viewcache.Open(home).Forget(device)
}

// forgetViewedChat removes the last-seen copy of one chat when that chat is
// moved to this computer.
func forgetViewedChat(home, device, chat string) {
	if strings.TrimSpace(home) == "" || strings.TrimSpace(device) == "" || strings.TrimSpace(chat) == "" {
		return
	}
	viewcache.Open(home).ForgetChat(device, chat)
}
