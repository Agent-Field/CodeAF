package main

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

const appWindowConflict = "this conversation is open in another codeaf window that the app cannot reach; close it there, or open it in that window"

type appSync struct {
	mu   sync.Mutex
	owed *syncsetup.Sync
	last appwire.Sync
}

type appSyncFact struct {
	line     string
	location *appwire.Location
	located  bool
}

var (
	_ appwire.Syncer      = (*appBackend)(nil)
	_ appwire.ChatLocator = (*appBackend)(nil)
)

func (*appBackend) ChatLocationsKnown() bool { return true }

// useSyncDrives routes only this assembly's drives to the app. The terminal
// keeps the default desk, including its home notices and FirstRun marker.
func (b *appBackend) useSyncDrives(book *driveBook) {
	b.locked(func() { b.drives = book })
	book.notices = &driveNotices{
		firstRun: b.syncStarted,
		say:      func(c cell.Cell, line string) { b.syncFact(c, appSyncFact{line: line}) },
		viewer: func(c cell.Cell, viewer syncsetup.Viewer) {
			location := &appwire.Location{Here: false, Line: viewer.Line}
			if viewer.ID != "" {
				location.Holder = &appwire.LocationHolder{ID: viewer.ID, Name: viewer.Name}
			}
			b.syncFact(c, appSyncFact{located: true, location: location})
		},
		driving: func(c cell.Cell) { b.syncFact(c, appSyncFact{located: true}) },
	}
}

// syncFact uses the physical folder, rather than assuming the app id and the
// cell id agree. A conversion can start a drive before its new transcript has
// reached hold; those facts wait here until the connection names its folder.
func (b *appBackend) syncFact(c cell.Cell, fact appSyncFact) {
	var found *appAgent
	b.locked(func() {
		if b.syncPending == nil {
			b.syncPending = map[string][]appSyncFact{}
		}
		b.syncPending[c.Root] = append(b.syncPending[c.Root], fact)
		for _, a := range b.agents {
			if dir, ok := session.FolderOf(a.transcript); ok && dir == c.Root {
				found = a
				return
			}
		}
	})
	if found != nil {
		b.flushSync(found)
	}
}

func (b *appBackend) flushSync(a *appAgent) {
	a.syncGate.Lock()
	defer a.syncGate.Unlock()
	var facts []appSyncFact
	b.locked(func() {
		dir, _ := session.FolderOf(a.transcript)
		facts = b.syncPending[dir]
		delete(b.syncPending, dir)
	})
	for _, fact := range facts {
		b.applySyncFact(a, fact)
	}
}

func (b *appBackend) applySyncFact(a *appAgent, fact appSyncFact) {
	if fact.located {
		b.located(a, fact.location)
		return
	}
	kind := appwire.NoticeSync
	b.locked(func() {
		if location := a.view.Location; location != nil && location.Line == fact.line {
			kind = appwire.NoticeMoved
		}
	})
	b.sayLocalNotice(a, kind, fact.line, fact.line)
}

func appLocalNoticeID(agent, kind, key string) string {
	return fmt.Sprintf("local-%x", sha256.Sum256([]byte(agent+"\x00"+kind+"\x00"+key)))
}

// sayLocalNotice emits a visible sentence once without writing to the synced
// journal. A viewer's local journal entry would be preserved as unsaved work.
func (b *appBackend) sayLocalNotice(a *appAgent, kind, key, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	var entry appwire.Entry
	var say bool
	b.locked(func() {
		id := appLocalNoticeID(a.view.ID, kind, key)
		if a.localNotices[id] {
			return
		}
		if a.localNotices == nil {
			a.localNotices = map[string]bool{}
		}
		a.localNotices[id] = true
		entry = appwire.Entry{ID: id, Agent: a.view.ID, Kind: appwire.EntryNotice, NoticeKind: kind, Quiet: false, Unrecorded: true, At: b.now(), Text: line}
		say = true
	})
	if say {
		b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: entry})
	}
}

func (b *appBackend) located(a *appAgent, location *appwire.Location) {
	var view appwire.Agent
	changed := false
	b.locked(func() {
		old := a.view.Location
		if old == nil && location == nil {
			return
		}
		if old != nil && location != nil && old.Here == location.Here && old.Line == location.Line &&
			(old.Holder == nil && location.Holder == nil || old.Holder != nil && location.Holder != nil && *old.Holder == *location.Holder) {
			return
		}
		a.view.Location = location
		view, changed = a.view, true
	})
	if changed {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
}

func (b *appBackend) syncStarted(s *syncsetup.Sync) {
	b.sync.mu.Lock()
	defer b.sync.mu.Unlock()
	if _, owed := s.PeekFirstRun(); owed {
		b.sync.owed = s
	}
	b.syncChangedLocked()
}

// syncStatusLocked is a local read. In particular it neither opens Sync nor
// creates an identity, starts a drive or contacts a relay.
func (b *appBackend) syncStatusLocked() appwire.Sync {
	state := appwire.Sync{Mode: appwire.SyncQuiet}
	switch {
	case syncsetup.Resolve(home.Dir()).Off:
		state.Mode, state.Reason = appwire.SyncOff, chatlist.SyncOff
	case !cell.Available():
		state.Mode, state.Reason = appwire.SyncUnavailable, cell.UnavailableReason()
	case syncsetup.MayTalk(home.Dir()):
		state.Mode = appwire.SyncOn
	}
	if b.sync.owed != nil {
		if line, owed := b.sync.owed.PeekFirstRun(); owed {
			state.Disclosure = &appwire.SyncDisclosure{ID: appLocalNoticeID(b.sync.owed.Home, appwire.NoticeSync, syncsetup.FirstRunLine), Text: line}
		} else {
			b.sync.owed = nil
		}
	}
	return state
}

func (b *appBackend) SyncStatus() (appwire.Sync, error) {
	b.sync.mu.Lock()
	defer b.sync.mu.Unlock()
	return b.syncChangedLocked(), nil
}

func (b *appBackend) syncChangedLocked() appwire.Sync {
	state := b.syncStatusLocked()
	old := b.sync.last
	sameDisclosure := old.Disclosure == nil && state.Disclosure == nil || old.Disclosure != nil && state.Disclosure != nil && *old.Disclosure == *state.Disclosure
	if old.Mode != state.Mode || old.Reason != state.Reason || !sameDisclosure {
		b.sync.last = state
		b.emit.Emit(appwire.EventSyncChanged, appwire.SyncChanged{Sync: state})
	}
	return state
}

func (b *appBackend) AcknowledgeSync(id string) error {
	b.sync.mu.Lock()
	defer b.sync.mu.Unlock()
	state := b.syncStatusLocked()
	if state.Disclosure == nil || state.Disclosure.ID != id {
		return appwire.Errorf(appwire.CodeNotFound, "that sync disclosure is not waiting to be shown")
	}
	told, err := b.sync.owed.AcknowledgeFirstRun()
	if err != nil {
		return appEngineError(err)
	}
	b.syncChangedLocked()
	if !told {
		return appwire.Errorf(appwire.CodeNotFound, "that sync disclosure is not waiting to be shown")
	}
	return nil
}

func appLocationRefusal(location *appwire.Location) error {
	if location == nil || location.Here {
		return nil
	}
	return &appwire.Error{Code: appwire.CodeConflict, Message: location.Line, Detail: appwire.HeldElsewhere{Kind: "held_elsewhere", Holder: location.Holder}}
}

func (b *appBackend) knownWorkRefusal(a *appAgent) error {
	var refusal error
	b.locked(func() {
		if a.windowMoved {
			refusal = appwire.Errorf(appwire.CodeConflict, "%s", appWindowConflict)
		} else {
			refusal = appLocationRefusal(a.view.Location)
		}
	})
	return refusal
}

// workAllowed asks the same live drive the tool gate does, before handing any
// user work to the conversation. Callers hold a.gate, never b.mu across I/O.
func (b *appBackend) workAllowed(a *appAgent) error {
	if err := b.knownWorkRefusal(a); err != nil {
		return err
	}
	var location *appwire.Location
	var transcript string
	var drives *driveBook
	b.locked(func() { transcript, drives = a.transcript, b.drives })
	dir, folder := session.FolderOf(transcript)
	if drives != nil && folder {
		drives.mu.Lock()
		d := drives.drive[filepath.Base(dir)]
		drives.mu.Unlock()
		if d != nil {
			err := d.Gate()
			b.locked(func() { location = a.view.Location })
			if refusal := appLocationRefusal(location); refusal != nil {
				return refusal
			}
			if err != nil {
				return appEngineError(err)
			}
		}
	}
	var live *appLive
	b.locked(func() { live = a.live })
	if live != nil {
		if door, ok := live.conn.Conv.(interface{ CheckWork() error }); ok {
			if err := door.CheckWork(); err != nil {
				return appEngineError(err)
			}
		}
	}
	return nil
}

// openingNotices retains the engine welcome, including one line per command
// interrupted by a crash. The ordinal distinguishes identical command lines.
func (b *appBackend) openingNotices(a *appAgent, conn *appConn) {
	note := conn.Note
	for i, line := range conn.Interrupted {
		b.sayLocalNotice(a, appwire.NoticeInterrupted, fmt.Sprintf("%d:%s", i, line), line)
		note = strings.Replace(note, line, "", 1)
	}
	for i, line := range strings.Split(note, "\n") {
		b.sayLocalNotice(a, "", fmt.Sprintf("%d:%s", i, line), line)
	}
}

// moveWindow runs outside the standing lane so finishing can join that lane.
// A takeover stops the owned conversation with the terminal's stop cause;
// EventMoved only detaches a view of a conversation still running elsewhere.
func (b *appBackend) moveWindow(id string, live *appLive, takeover bool, line string) {
	a, err := b.find(id)
	if err != nil {
		return
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	mine := false
	b.locked(func() {
		if a.live != live {
			return
		}
		a.live, a.windowMoved = nil, true
		a.view.State = appwire.StateIdle
		live.stopping, mine = true, true
	})
	if !mine {
		return
	}
	if strings.TrimSpace(line) == "" {
		line = session.TakeoverWord
	}
	b.located(a, &appwire.Location{Here: false, Line: line})
	b.sayLocalNotice(a, appwire.NoticeMoved, line, line)
	if takeover {
		if door, ok := live.conn.Conv.(interface{ InterruptFor(session.StopDoor) }); ok {
			door.InterruptFor(session.StopByTakeover)
		} else {
			live.conn.Conv.Interrupt()
		}
	}
	b.finish(live)
}

func (b *appBackend) windowMoved(id string, live *appLive, takeover bool, line string) {
	guard.Go("app/chat-moved", func() { b.moveWindow(id, live, takeover, line) })
}
