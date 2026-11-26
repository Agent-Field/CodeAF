package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

const permitTestIdentity = "id_account"

// permitBookWithLegacy is a host book whose snapshot names two older pairings
// (this computer and "dev_old"); "dev_new" paired after it, so it stands outside
// the snapshot.
func permitBookWithLegacy(t *testing.T) *pair.PermitBook {
	t.Helper()
	book := pair.PermitsAt(filepath.Join(t.TempDir(), "permits.json"))
	if err := book.SnapshotLegacy(permitTestIdentity, []string{"dev_self", "dev_old", "dev_gone"}); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return book
}

func permitRoster() []tui3.DeviceRow {
	return []tui3.DeviceRow{
		{ID: "dev_self", Name: "this mac", Self: true},
		{ID: "dev_old", Name: "tower"},
		{ID: "dev_new", Name: "desk"},
		{ID: "dev_gone", Name: "attic", Revoked: true},
	}
}

// permitAskerOver builds an asker whose background work waits to be run by the
// test, so a test decides exactly when the read and the write happen.
func permitAskerOver(book *pair.PermitBook, rows []tui3.DeviceRow, clock func() time.Time) (*permitAsker, *[]func()) {
	var queued []func()
	return &permitAsker{
		who:     func() string { return permitTestIdentity },
		devices: func(context.Context) ([]tui3.DeviceRow, error) { return rows, nil },
		book:    func() *pair.PermitBook { return book },
		now:     clock,
		spawn:   func(_ string, fn func()) { queued = append(queued, fn) },
	}, &queued
}

func runQueued(queued *[]func()) {
	work := *queued
	*queued = nil
	for _, fn := range work {
		fn()
	}
}

// V25: Home never waits for the answer. The first ask returns "not due" at
// once and starts exactly one background read; asking again while that read is
// out starts no second one.
func TestHomeAsksTheBookInTheBackgroundAndNeverWaitsForIt(t *testing.T) {
	asker, queued := permitAskerOver(permitBookWithLegacy(t), permitRoster(), time.Now)
	if names, ask := asker.AskOnce(); ask || len(names) != 0 {
		t.Fatalf("the first ask answered (%v, %t) before anything was read", names, ask)
	}
	if len(*queued) != 1 {
		t.Fatalf("the first ask started %d reads, want 1", len(*queued))
	}
	asker.AskOnce()
	asker.AskOnce()
	if len(*queued) != 1 {
		t.Fatalf("asking while a read was out started %d reads, want still 1", len(*queued))
	}
}

// V25: once read, Home asks about the older computers by name, and only those:
// not this computer, not one removed, not one paired after remote control.
func TestTheQuestionNamesOnlyTheOlderComputersStillOnTheRoster(t *testing.T) {
	asker, queued := permitAskerOver(permitBookWithLegacy(t), permitRoster(), time.Now)
	asker.AskOnce()
	runQueued(queued)
	names, ask := asker.AskOnce()
	if !ask || !reflect.DeepEqual(names, []string{"tower"}) {
		t.Fatalf("after the read Home asks (%v, %t), want (tower, true)", names, ask)
	}
}

// V25: answering yes lets the older computers type and never asks again; the
// book keeps the decision.
func TestAnsweringYesLetsTheOlderComputersTypeAndEndsTheQuestion(t *testing.T) {
	book := permitBookWithLegacy(t)
	asker, queued := permitAskerOver(book, permitRoster(), time.Now)
	asker.AskOnce()
	runQueued(queued)
	if err := asker.Answer(true); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if names, ask := asker.AskOnce(); ask || len(names) != 0 {
		t.Fatalf("Home still asks (%v, %t) the moment after the answer", names, ask)
	}
	runQueued(queued)
	if got, _ := book.For(permitTestIdentity, "dev_old"); !got.Drive || !got.Watch {
		t.Fatalf("an older computer after yes may %+v, want watch and type", got)
	}
	if got, _ := book.For(permitTestIdentity, "dev_gone"); got.Drive {
		t.Fatal("yes gave typing to a computer that was not on the roster")
	}
	if book.NeedsAsk(permitTestIdentity) {
		t.Fatal("the book would ask again after an answer")
	}
}

// V25 / V11: answering no keeps the older computers watching only, and ends the
// question just the same.
func TestAnsweringNoKeepsTheOlderComputersWatchingAndEndsTheQuestion(t *testing.T) {
	book := permitBookWithLegacy(t)
	asker, queued := permitAskerOver(book, permitRoster(), time.Now)
	asker.AskOnce()
	runQueued(queued)
	if err := asker.Answer(false); err != nil {
		t.Fatalf("answer: %v", err)
	}
	runQueued(queued)
	got, class := book.For(permitTestIdentity, "dev_old")
	if got.Drive || class != pair.ClassLegacy {
		t.Fatalf("an older computer after no may %+v (%v), want watch only and still older", got, class)
	}
	if book.NeedsAsk(permitTestIdentity) {
		t.Fatal("the book would ask again after no")
	}
}

// V25: a decision that could not be written does not end the question; Home
// asks again, and the older computers keep watching only until it is.
func TestAnAnswerThatCouldNotBeKeptBringsTheQuestionBack(t *testing.T) {
	// A book at a path whose parent is a file cannot be written.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	broken := pair.PermitsAt(filepath.Join(blocker, "permits.json"))
	clock := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	asker, queued := permitAskerOver(broken, permitRoster(), func() time.Time { return clock })
	asker.mu.Lock()
	asker.ask, asker.names, asker.ids, asker.identity = true, []string{"tower"}, []string{"dev_old"}, permitTestIdentity
	asker.checkedAt = clock
	asker.mu.Unlock()
	if err := asker.Answer(true); err != nil {
		t.Fatalf("answer: %v", err)
	}
	runQueued(queued)
	clock = clock.Add(permitAskEvery)
	asker.AskOnce()
	if len(*queued) != 1 {
		t.Fatalf("after a failed write the next ask started %d reads, want 1 so the question can come back", len(*queued))
	}
}

// V25: with nothing to ask about (no snapshot, or no older computer left on the
// roster) Home is never asked.
func TestHomeIsNotAskedWhenNoOlderComputerIsLeft(t *testing.T) {
	onlyNew := []tui3.DeviceRow{{ID: "dev_self", Name: "this mac", Self: true}, {ID: "dev_new", Name: "desk"}}
	asker, queued := permitAskerOver(permitBookWithLegacy(t), onlyNew, time.Now)
	asker.AskOnce()
	runQueued(queued)
	if names, ask := asker.AskOnce(); ask || len(names) != 0 {
		t.Fatalf("Home asks (%v, %t) with no older computer on the roster", names, ask)
	}
	empty := pair.PermitsAt(filepath.Join(t.TempDir(), "permits.json"))
	asker, queued = permitAskerOver(empty, permitRoster(), time.Now)
	asker.AskOnce()
	runQueued(queued)
	if _, ask := asker.AskOnce(); ask {
		t.Fatal("Home asks with no legacy snapshot in the book")
	}
}

// V25: a roster that cannot be read asks nothing now and is tried again later.
func TestAnUnreadableRosterAsksNothingNow(t *testing.T) {
	asker, queued := permitAskerOver(permitBookWithLegacy(t), nil, time.Now)
	asker.devices = func(context.Context) ([]tui3.DeviceRow, error) { return nil, errors.New("directory did not answer") }
	asker.AskOnce()
	runQueued(queued)
	if _, ask := asker.AskOnce(); ask {
		t.Fatal("Home asked from a roster it could not read")
	}
}

// V14, as the 2026-11-24 hand test corrected it: remote control is switched on in
// a window that is already open, so the asker is there whatever the setting says
// at start, and it asks only while the setting is on. Off, it says there is
// nothing to ask and starts no work, which is Home exactly as it was.
func TestTheOneTimeQuestionFollowsRemoteControlWhileTheWindowIsOpen(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	_ = os.Unsetenv("CODEAF_REMOTE_CONTROL")
	if got := newPermitAsker(nil); got != nil {
		t.Fatalf("newPermitAsker with no door = %T, want nil", got)
	}

	// The real wrapper over a real door, with the book's work queued by the test.
	got := newPermitAsker(newApprovalsDoor(t.TempDir()))
	gate, ok := got.(whenRemoteControlIsOn)
	if !ok {
		t.Fatalf("newPermitAsker answered %T, want the asker that follows the setting", got)
	}
	if gate.on() {
		t.Fatal("the gate reads remote control as on in a fresh profile")
	}
	if err := applyRow(profile, config.KeyRemoteControl, "on"); err != nil {
		t.Fatal(err)
	}
	if !gate.on() {
		t.Fatal("the gate still reads remote control as off after /settings switched it on")
	}

	// What the gate does with each answer.
	asker, queued := permitAskerOver(permitBookWithLegacy(t), permitRoster(), time.Now)
	on := false
	gated := whenRemoteControlIsOn{asker: asker, on: func() bool { return on }}
	if names, ask := gated.AskOnce(); ask || len(names) != 0 || len(*queued) != 0 {
		t.Fatalf("with remote control off the asker answered (%v, %t) and queued %d reads, want nothing", names, ask, len(*queued))
	}
	on = true
	gated.AskOnce()
	runQueued(queued)
	if names, ask := gated.AskOnce(); !ask || !reflect.DeepEqual(names, []string{"tower"}) {
		t.Fatalf("after switching remote control on the asker answered (%v, %t), want (tower, true)", names, ask)
	}
	on = false
	if _, ask := gated.AskOnce(); ask {
		t.Fatal("the asker kept asking after remote control was switched off")
	}
}

// V14 / V11: the device list carries each other computer's permit only with
// remote control on; with it off the rows are as they always were.
func TestTheDeviceListCarriesPermitsOnlyWithRemoteControlOn(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	book := permitBookWithLegacy(t)
	door := &approvalsDoor{permits: func() *pair.PermitBook { return book }}

	t.Setenv("CODEAF_REMOTE_CONTROL", "")
	for _, row := range door.withPermits(permitTestIdentity, permitRoster()) {
		if row.Permit != nil {
			t.Fatalf("row %s carried a permit with remote control off", row.ID)
		}
	}

	t.Setenv("CODEAF_REMOTE_CONTROL", "1")
	got := map[string]*tui3.DevicePermit{}
	for _, row := range door.withPermits(permitTestIdentity, permitRoster()) {
		got[row.ID] = row.Permit
	}
	if got["dev_self"] != nil || got["dev_gone"] != nil {
		t.Fatalf("this computer or a removed one carried a permit: %+v %+v", got["dev_self"], got["dev_gone"])
	}
	if p := got["dev_old"]; p == nil || !p.Watch || p.Drive {
		t.Fatalf("an older pairing reads %+v, want watch on and type off", p)
	}
	if p := got["dev_new"]; p == nil || !p.Watch || !p.Drive {
		t.Fatalf("a pairing made after reads %+v, want watch and type on", p)
	}
}

// V11: the switches on /devices write the host's own book and change only the
// half they name.
func TestTheDevicesKeysWriteTheHostsBookAndChangeOnlyTheNamedHalf(t *testing.T) {
	book := pair.PermitsAt(filepath.Join(t.TempDir(), "permits.json"))
	dir := t.TempDir()
	who, err := identity.Ensure(dir)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	door := &approvalsDoor{dir: dir, permits: func() *pair.PermitBook { return book }}
	off, on := false, true

	if err := door.SetPermit("dev_new", nil, &off); err != nil {
		t.Fatalf("type off: %v", err)
	}
	if got, class := book.For(who.ID(), "dev_new"); !got.Watch || got.Drive || class != pair.ClassExplicit {
		t.Fatalf("after type off a computer may %+v (%v), want watch only, set here", got, class)
	}
	if err := door.SetPermit("dev_new", &on, &on); err != nil {
		t.Fatalf("both on: %v", err)
	}
	if got, _ := book.For(who.ID(), "dev_new"); !got.Watch || !got.Drive {
		t.Fatalf("after both on a computer may %+v", got)
	}
	if err := door.SetPermit("dev_new", &off, nil); err != nil {
		t.Fatalf("watch off: %v", err)
	}
	if got, _ := book.For(who.ID(), "dev_new"); got.Watch || !got.Drive {
		t.Fatalf("watch off changed the other half too: %+v", got)
	}
	if err := door.SetPermit("", &on, nil); err == nil {
		t.Fatal("SetPermit accepted an empty computer")
	}
	var _ tui3.PermitSetter = door
}
