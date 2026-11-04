package main

import (
	"context"
	"errors"
	"math"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// A client that stopped waiting for agents.take reads the row's taking flag:
// set while the take runs, cleared when it ends without moving the chat.
func TestAppTakeInFlightRowClearsWhenTheTakeFails(t *testing.T) {
	celltest.Available(t)
	appDevicesRelay(t)
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	a, b := appDeviceSync(t, aHome), appDeviceSync(t, bHome)
	for _, s := range []*syncsetup.Sync{a, b} {
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, devname.Name(s.Home)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "saved-head"}); err != nil {
		t.Fatal(err)
	}
	_, rec, wire := appDeviceBackend(t, bHome)
	var row appwire.ChatRow
	appSyncWait(t, "the other computer's chat is listed", func() bool {
		for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
			if r.Cell == c.ID && r.CanTake {
				row = r
				return true
			}
		}
		return false
	})
	if row.Taking {
		t.Fatalf("no take has started: %+v", row)
	}
	stale := row.Seen
	stale.Head = "another-head"
	if _, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: stale}); refusal == nil || refusal.Code != appwire.CodeConflict {
		t.Fatalf("stale take: %+v", refusal)
	}
	started := rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		return name == appwire.EventChatChanged && data.(appwire.ChatChanged).Chat.Cell == c.ID && data.(appwire.ChatChanged).Chat.Taking
	})
	heard, ended := -1, appwire.ChatRow{}
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		heard++
		changed, ok := data.(appwire.ChatChanged)
		if heard <= started || name != appwire.EventChatChanged || !ok || changed.Chat.Cell != c.ID || changed.Chat.Taking {
			return false
		}
		ended = changed.Chat
		return true
	})
	if ended.Here {
		t.Fatalf("a failed take left the chat here: %+v", ended)
	}
	for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
		if r.Cell == c.ID && r.Taking {
			t.Fatalf("the list still says a take is running: %+v", r)
		}
	}
}

// A take that acquired the chat keeps saying it runs on every row from before
// it, whatever ends the call: a client that stopped waiting reads taking:false
// on a chat still elsewhere as a take that did not move it.
func TestAppTakeInFlightRowsFromBeforeALandedTakeStillSayItRuns(t *testing.T) {
	const id = "chat"
	var c appChats
	mark := func(row appwire.ChatRow) bool {
		rows := []appwire.ChatRow{row}
		c.markTaking(rows, true)
		return rows[0].Taking
	}
	before := appwire.ChatRow{Cell: id, Seen: appwire.Seen{Device: "other", Fence: 4}}
	if mark(before) {
		t.Fatal("no take has started")
	}
	c.countTake(id, 1)
	c.landTake(id, 5)
	c.countTake(id, -1)
	if !mark(before) {
		t.Fatal("a row from before the landed take says it ended")
	}
	if !mark(appwire.ChatRow{Cell: id, Seen: appwire.Seen{Device: "this", Fence: 5}}) {
		t.Fatal("the taken fence not yet installed here says the take ended")
	}
	if mark(appwire.ChatRow{Cell: id, Here: true, Seen: appwire.Seen{Device: "this", Fence: 5}}) {
		t.Fatal("the row that says the chat is here still says a take runs")
	}
	if mark(before) {
		t.Fatal("a take already seen here kept counting")
	}
	// A later take elsewhere ends it too, so the flag never sticks.
	c.landTake(id, 5)
	if mark(appwire.ChatRow{Cell: id, Seen: appwire.Seen{Device: "other", Fence: 6}}) || mark(before) {
		t.Fatal("a chat another computer took again still says this take runs")
	}
	// Cached rows can be empty or stale, so a chat missing from them keeps
	// its landed take.
	c.landTake(id, 5)
	c.markTaking(nil, false)
	c.markTaking([]appwire.ChatRow{{Cell: "another"}}, false)
	if !c.landedTake(id) {
		t.Fatal("cached rows without the chat dropped its landed take")
	}
	// A chat that leaves a directory reading takes its landed take with it.
	c.markTaking([]appwire.ChatRow{{Cell: "another"}}, true)
	if mark(before) {
		t.Fatal("a chat that left the list came back saying an old take runs")
	}
}

// The cached row republished after a landed take whose reading failed still
// shows the chat elsewhere, so it still says the take runs.
func TestAppTakeInFlightCacheAfterALandedTakeStillSaysItRuns(t *testing.T) {
	celltest.Available(t)
	appDevicesRelay(t)
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	a, b := appDeviceSync(t, aHome), appDeviceSync(t, bHome)
	for _, s := range []*syncsetup.Sync{a, b} {
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, devname.Name(s.Home)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "saved-head"}); err != nil {
		t.Fatal(err)
	}
	backend, rec, wire := appDeviceBackend(t, bHome)
	var row appwire.ChatRow
	appSyncWait(t, "the other computer's chat is listed", func() bool {
		for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
			if r.Cell == c.ID && r.CanTake {
				row = r
				return true
			}
		}
		return false
	})
	backend.chats.countTake(c.ID, 1)
	backend.publishTaking()
	backend.chats.landTake(c.ID, row.Seen.Fence+1)
	// The take placed its agent here; the cache still holds the row from
	// before it, naming that agent.
	placed := backend.agent("taken-here")
	here := &appwire.Location{Here: true, Cell: c.ID}
	backend.located(placed, here)
	backend.chats.mu.Lock()
	for i := range backend.chats.rows {
		if backend.chats.rows[i].Cell == c.ID {
			backend.chats.rows[i].LocalAgent = placed.view.ID
		}
	}
	backend.chats.mu.Unlock()
	backend.chats.countTake(c.ID, -1)
	rec.mu.Lock()
	from := len(rec.data)
	rec.mu.Unlock()
	backend.publishTaking()
	backend.locked(func() {
		if placed.view.Location != here {
			t.Errorf("republishing taking moved the taken agent back: %+v", placed.view.Location)
		}
	})
	listed := false
	for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
		if r.Cell == c.ID {
			listed = true
			if !r.Taking {
				t.Fatal("the list read after a landed take says it ended with the chat elsewhere")
			}
		}
	}
	if !listed {
		t.Fatal("the chat left the list")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, data := range rec.data[from:] {
		if changed, ok := data.(appwire.ChatChanged); ok && changed.Chat.Cell == c.ID && !changed.Chat.Here && !changed.Chat.Taking {
			t.Fatalf("a landed take was announced as one that did not move the chat: %+v", changed.Chat)
		}
	}
}

// A watch that has announced no newer version than the cache, so its next
// frame is presence only.
type presenceOnlyFeed struct{ changes chan struct{} }

func (f presenceOnlyFeed) State() dirwatch.State    { return dirwatch.State{Up: true} }
func (f presenceOnlyFeed) Changes() <-chan struct{} { return f.changes }
func (presenceOnlyFeed) Probe()                     {}
func (presenceOnlyFeed) Close()                     {}

// A take that acquired the chat but whose reconcile read failed leaves the
// cache saying the chat is elsewhere; a device coming or going must not move
// the agent the take put here back there.
func TestAppTakeInFlightPresenceAfterALandedTakeLeavesTheAgentHere(t *testing.T) {
	celltest.Available(t)
	appDevicesRelay(t)
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	a, b := appDeviceSync(t, aHome), appDeviceSync(t, bHome)
	for _, s := range []*syncsetup.Sync{a, b} {
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, devname.Name(s.Home)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "saved-head"}); err != nil {
		t.Fatal(err)
	}
	backend, _, wire := appDeviceBackend(t, bHome)
	var row appwire.ChatRow
	appSyncWait(t, "the other computer's chat is listed", func() bool {
		for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
			if r.Cell == c.ID && r.CanTake {
				row = r
				return true
			}
		}
		return false
	})
	// This computer already holds a local copy, so the cached row names its
	// agent while saying the chat is elsewhere.
	placed := backend.agent("taken-here")
	backend.chats.mu.Lock()
	for i := range backend.chats.rows {
		if backend.chats.rows[i].Cell == c.ID {
			backend.chats.rows[i].LocalAgent = placed.view.ID
		}
	}
	backend.chats.mu.Unlock()
	// agents.take acquires the chat and places the agent here; settleTake
	// stops counting it, and its reading fails, leaving the cache as it was.
	backend.chats.countTake(c.ID, 1)
	backend.chats.landTake(c.ID, row.Seen.Fence+1)
	here := &appwire.Location{Here: true, Cell: c.ID}
	backend.located(placed, here)
	backend.chats.countTake(c.ID, -1)

	if !backend.refreshChatInventory(t.Context(), presenceOnlyFeed{changes: make(chan struct{})}) {
		t.Fatal("the presence refresh stopped the follower")
	}
	backend.locked(func() {
		if placed.view.Location != here {
			t.Errorf("a presence change moved the taken agent back: %+v", placed.view.Location)
		}
	})
}

// A watch that has announced a directory version newer than any cache, so its
// next frame reads the rows again.
type newerVersionFeed struct{ presenceOnlyFeed }

func (newerVersionFeed) State() dirwatch.State {
	return dirwatch.State{Up: true, Version: math.MaxUint64}
}

// appLandedTakeFixture lists another computer's chat here and has agents.take
// acquire it without an engine: the copy answers with the chat's own folder at
// the next fence, and open stands in for opening the moved chat.
func appLandedTakeFixture(t *testing.T, open func(*appBackend, string) (appwire.Agent, error)) (*appBackend, *appRecorder, *actionWire, *atomic.Bool, appwire.ChatRow) {
	t.Helper()
	celltest.Available(t)
	_, _, down := appDevicesRelay(t)
	aHome, bHome := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(aHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	a, b := appDeviceSync(t, aHome), appDeviceSync(t, bHome)
	for _, s := range []*syncsetup.Sync{a, b} {
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, devname.Name(s.Home)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: "moved", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "saved-head"}); err != nil {
		t.Fatal(err)
	}
	backend, rec, wire := appDeviceBackend(t, bHome)
	var row appwire.ChatRow
	appSyncWait(t, "the other computer's chat is listed", func() bool {
		for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
			if r.Cell == c.ID && r.CanTake {
				row = r
				return true
			}
		}
		return false
	})
	seen, opened := appTakeSeen, appTakeOpen
	t.Cleanup(func() { appTakeSeen, appTakeOpen = seen, opened })
	appTakeSeen = func(_ context.Context, _ *syncsetup.Sync, id string, _ *directory.LeaseExpectation) (syncsetup.Continued, error) {
		if id != c.ID {
			t.Errorf("took %s, want %s", id, c.ID)
		}
		return syncsetup.Continued{Device: "this computer", Taken: handoff.Taken{Cell: c, Driving: cellsync.Driving{Cell: c, Fence: row.Seen.Fence + 1, Head: "saved-head"}}}, nil
	}
	appTakeOpen = open
	return backend, rec, wire, down, row
}

// appNoEndedTake fails when the take never said it runs, or when a chat.changed
// row after it said so shows the chat elsewhere without saying the take runs.
// The chat feed's follower may publish the chat just before the take starts;
// that row is right when it is sent, so it is not an ended take.
func appNoEndedTake(t *testing.T, rec *appRecorder, from int, id string) {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	row, started, ended := appEndedTakeRow(rec.data[from:], id)
	if !started {
		t.Fatalf("no chat.changed row said the take of %s runs", id)
	}
	if ended {
		t.Fatalf("a take that acquired the chat was announced as one that did not move it: %+v", row)
	}
}

// appEndedTakeRow reads recorded events in order. started is the first row for
// id that says the take runs; ended is a later row that shows the chat elsewhere
// without saying so.
func appEndedTakeRow(events []any, id string) (row appwire.ChatRow, started, ended bool) {
	for _, data := range events {
		changed, ok := data.(appwire.ChatChanged)
		if !ok || changed.Chat.Cell != id {
			continue
		}
		if !started {
			started = changed.Chat.Taking
			continue
		}
		if !changed.Chat.Here && !changed.Chat.Taking {
			return changed.Chat, true, true
		}
	}
	return appwire.ChatRow{}, started, false
}

// The check on a real take: a row from before the take starts is accepted, and
// the same row after the take landed is the ended take it exists to catch.
func TestAppNoEndedTakeJudgesOnlyRowsAfterTheTakeStarted(t *testing.T) {
	backend, rec, wire, _, row := appLandedTakeFixture(t, func(*appBackend, string) (appwire.Agent, error) {
		return appwire.Agent{}, errors.New("the moved chat would not open")
	})
	elsewhere := appwire.ChatChanged{Chat: appwire.ChatRow{Cell: row.Cell, Title: row.Title}}
	judged := func() (appwire.ChatRow, bool, bool) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return appEndedTakeRow(slices.Clone(rec.data), row.Cell)
	}
	rec.Emit(appwire.EventChatChanged, elsewhere)
	if _, started, ended := judged(); started || ended {
		t.Fatalf("a row from before the take was judged: started=%v ended=%v", started, ended)
	}
	if _, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}); refusal == nil {
		t.Fatal("a take whose chat would not open succeeded")
	}
	if !backend.chats.landedTake(row.Cell) {
		t.Fatal("a take that acquired the chat recorded no landed take")
	}
	if _, started, ended := judged(); !started || ended {
		t.Fatalf("a real take with an early row: started=%v ended=%v", started, ended)
	}
	rec.Emit(appwire.EventChatChanged, elsewhere)
	if got, started, ended := judged(); !started || !ended || got.Cell != row.Cell {
		t.Fatalf("a row after landing that shows no take running was not caught: %+v started=%v ended=%v", got, started, ended)
	}
}

// agents.take acquired the chat but opening it here failed: every row after
// still says the take runs while the chat shows elsewhere.
func TestAppTakeAcquiredThenOpenFailsKeepsSayingItRuns(t *testing.T) {
	backend, rec, wire, _, row := appLandedTakeFixture(t, func(*appBackend, string) (appwire.Agent, error) {
		return appwire.Agent{}, errors.New("the moved chat would not open")
	})
	rec.mu.Lock()
	from := len(rec.data)
	rec.mu.Unlock()
	if _, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}); refusal == nil {
		t.Fatal("a take whose chat would not open succeeded")
	}
	if !backend.chats.landedTake(row.Cell) {
		t.Fatal("a take that acquired the chat recorded no landed take")
	}
	appNoEndedTake(t, rec, from, row.Cell)
	for _, r := range machineCall[appwire.ChatsResult](t, wire, "chats.list", struct{}{}).Chats {
		if r.Cell == row.Cell && !r.Here && !r.Taking {
			t.Fatalf("the list read after the take says it ended with the chat elsewhere: %+v", r)
		}
	}
	appNoEndedTake(t, rec, from, row.Cell)
}

// agents.take acquired and opened the chat but its follow-up reading failed:
// the rows still say the take runs, and a later reading from before the take
// leaves the agent here.
func TestAppTakeAcquiredThenReadingFailsKeepsTheAgentHere(t *testing.T) {
	var down *atomic.Bool
	backend, rec, wire, relayDown, row := appLandedTakeFixture(t, func(b *appBackend, id string) (appwire.Agent, error) {
		// The relay drops once the chat is open, so settleTake's reading fails.
		down.Store(true)
		return appwire.Agent{ID: id}, nil
	})
	down = relayDown
	rec.mu.Lock()
	from := len(rec.data)
	rec.mu.Unlock()
	got := machineCall[appwire.TakeResult](t, wire, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	if got.Location == nil || !got.Location.Here {
		t.Fatalf("the taken agent is not here: %+v", got.Location)
	}
	appNoEndedTake(t, rec, from, row.Cell)
	here := func(when string) {
		t.Helper()
		var a *appAgent
		backend.locked(func() { a = backend.agents[got.ID] })
		if a == nil {
			t.Fatalf("%s: the taken agent is gone", when)
		}
		backend.locked(func() {
			if a.view.Location == nil || !a.view.Location.Here {
				t.Errorf("%s: the taken agent moved back: %+v", when, a.view.Location)
			}
		})
	}
	here("after the take")
	// The follower's next reading is no older than the cache but older than
	// the take: it still shows the chat elsewhere.
	down.Store(false)
	if !backend.refreshChatInventory(t.Context(), newerVersionFeed{presenceOnlyFeed{changes: make(chan struct{})}}) {
		t.Fatal("the refresh stopped the follower")
	}
	if !backend.chats.landedTake(row.Cell) {
		t.Fatal("a reading from before the take cleared the landed take")
	}
	here("after a reading from before the take")
	appNoEndedTake(t, rec, from, row.Cell)
}

// agents.take acquired the chat while the follower's cache was empty, and
// opening it failed: republishing the empty cache keeps the landed take, so a
// later reading from before the take still says it runs and leaves the agent
// the take placed here.
func TestAppTakeAcquiredWithAnEmptyCacheKeepsTheLandedTake(t *testing.T) {
	backend, rec, wire, _, row := appLandedTakeFixture(t, func(*appBackend, string) (appwire.Agent, error) {
		return appwire.Agent{}, errors.New("the moved chat would not open")
	})
	// The follower runs, but its first reading failed and cached nothing.
	// Its watch goes quiet so no reading of its own refills the cache; the
	// test drives the next reading itself.
	backend.chats.mu.Lock()
	cancel, done := backend.chats.cancel, backend.chats.done
	backend.chats.mu.Unlock()
	if cancel == nil || done == nil {
		t.Fatal("the chat follower is not running")
	}
	cancel()
	<-done
	backend.chats.mu.Lock()
	backend.chats.rows, backend.chats.version = nil, 0
	backend.chats.mu.Unlock()
	rec.mu.Lock()
	from := len(rec.data)
	rec.mu.Unlock()
	if _, refusal := wire.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}); refusal == nil {
		t.Fatal("a take whose chat would not open succeeded")
	}
	if !backend.chats.landedTake(row.Cell) {
		t.Fatal("republishing an empty cache dropped the landed take")
	}
	placed := func() *appAgent {
		var found *appAgent
		backend.locked(func() {
			for _, a := range backend.agents {
				if a.view.Location != nil && a.view.Location.Cell == row.Cell {
					found = a
				}
			}
		})
		return found
	}
	a := placed()
	if a == nil {
		t.Fatal("the take placed no agent for the chat")
	}
	backend.locked(func() {
		if !a.view.Location.Here {
			t.Fatalf("the taken agent is not here: %+v", a.view.Location)
		}
	})
	// The follower's next reading is newer than the cache but older than the
	// take: it still shows the chat elsewhere.
	if !backend.refreshChatInventory(t.Context(), newerVersionFeed{presenceOnlyFeed{changes: make(chan struct{})}}) {
		t.Fatal("the refresh stopped the follower")
	}
	listed := false
	backend.chats.mu.Lock()
	rows := slices.Clone(backend.chats.rows)
	backend.chats.mu.Unlock()
	for _, r := range rows {
		if r.Cell == row.Cell {
			listed = true
			if !r.Here && !r.Taking {
				t.Fatalf("a reading from before the take says it ended with the chat elsewhere: %+v", r)
			}
		}
	}
	if !listed {
		t.Fatal("the reading did not list the taken chat")
	}
	appNoEndedTake(t, rec, from, row.Cell)
	backend.locked(func() {
		if a.view.Location == nil || !a.view.Location.Here {
			t.Errorf("a reading from before the take moved the agent back: %+v", a.view.Location)
		}
	})
}
