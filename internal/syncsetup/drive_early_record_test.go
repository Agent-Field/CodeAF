package syncsetup

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
)

// These tests are the early directory record of remote control (contract E1 to
// E6 in NOTES-POLISH). A chat appears on the other computers' Home only once its
// directory record exists, and the record has always been created by the first
// SEALED turn, so a chat whose first turn was still running appeared nowhere. With
// the option on, the turn's START creates the record from the head the window
// already sealed at startup; with it off, nothing here changes by a byte.

// startupChat is a window that has just opened a chat: the startup head is
// sealed (bookkeeping only, nobody has spoken yet) and the drive is up.
func startupChat(t *testing.T, r *twoHomes, early func() bool) *Drive {
	t.Helper()
	ctx := t.Context()
	d, err := r.a.Drive(ctx, r.engine, r.cell, DriveOptions{EarlyRecord: early})
	mustNil(t, err)
	t.Cleanup(func() { _ = d.Close(ctx) })
	appendTo(t, transcriptOf(r.cell), `{"type":"session","version":1,"id":"early"}`+"\n")
	_, err = d.Store(r.engine).Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	d.CatchUp()
	return d
}

func (r *twoHomes) recordOn(t *testing.T, id string) (directory.CellView, bool) {
	t.Helper()
	view, err := r.b.Dir.Cell(t.Context(), id)
	if errors.Is(err, directory.ErrNotFound) {
		return view, false
	}
	mustNil(t, err)
	return view, true
}

// E1, E2, E4: with the option on, the first turn's START puts the chat on the
// other computer's list as working, from the head the window sealed at startup
// and carrying no words of the turn; when the turn ends the row moves on as it
// always did, head first and the working flag clearing after.
func TestTheFirstTurnStartingCreatesTheDirectoryRecordWhenTheOptionIsOn(t *testing.T) {
	r := newTwoHomes(t)
	ctx := t.Context()
	d := startupChat(t, r, func() bool { return true })
	startup := r.head()
	if _, found := r.recordOn(t, r.cell.ID); found {
		t.Fatal("a chat nobody has spoken in has a record before its turn started")
	}

	// The person's message is journaled before the turn goroutine starts, and the
	// turn tells the drive it is working. Nothing has been sealed since startup.
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"hello"}`+"\n")
	d.Working(true)

	waitFor(t, "the working chat's record on the other computer", func() bool {
		view, found := r.recordOn(t, r.cell.ID)
		return found && view.Cell.Lease.Working
	})
	view, _ := r.recordOn(t, r.cell.ID)
	if view.Cell.Head != startup {
		t.Fatalf("the early record carries head %s, want the startup head %s", view.Cell.Head, startup)
	}
	if view.Cell.Lease.Device != r.a.Device.ID() {
		t.Fatalf("the early record is held by %q, want this computer", view.Cell.Lease.Device)
	}
	if view.Cell.Title != "" {
		t.Fatalf("the early record carries the title %q before anything was named", view.Cell.Title)
	}
	rows, err := r.b.Rows(ctx)
	mustNil(t, err)
	if len(rows) != 1 || rows[0].Cell != r.cell.ID {
		t.Fatalf("the other computer lists %+v, want the one working chat", rows)
	}

	// The turn ends: the head advances to the sealed turn and working clears.
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"assistant","content":"hi"}`+"\n")
	_, err = d.Store(r.engine).Seal(ctx, r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
	d.Working(false)
	r.durable(r.cell.ID, r.cell)
	waitFor(t, "the working flag to clear", func() bool {
		view, found := r.recordOn(t, r.cell.ID)
		return found && !view.Cell.Lease.Working
	})
	if view, _ := r.recordOn(t, r.cell.ID); view.Cell.Head == startup {
		t.Fatal("the head did not move on when the first turn ended")
	}
}

// E3: a turn that started with nothing the person said (a bookkeeping
// start) appears nowhere, option on or not. No untitled chats.
func TestAChatNobodyHasSpokenInHasNoEarlyRecord(t *testing.T) {
	r := newTwoHomes(t)
	d := startupChat(t, r, func() bool { return true })
	d.Working(true)
	if n := d.batcher.Pending(); n != 0 {
		t.Fatalf("a chat nobody spoke in has %d turns waiting to be published", n)
	}
	d.Working(false)
	mustNil(t, d.Close(t.Context()))
	if _, found := r.recordOn(t, r.cell.ID); found {
		t.Fatal("a chat nobody has spoken in appeared on the other computer")
	}
	rows, err := r.b.Rows(t.Context())
	mustNil(t, err)
	if len(rows) != 0 {
		t.Fatalf("the other computer lists an unused chat: %+v", rows)
	}
}

// E6: with the option off, or never given, the first turn starting publishes
// nothing: the record still appears only when the first turn is sealed, as it
// does today, and the startup head is not published from an unsealed message.
func TestWithTheOptionOffTheFirstTurnStartingPublishesNothing(t *testing.T) {
	for name, early := range map[string]func() bool{
		"not given": nil,
		"off":       func() bool { return false },
	} {
		t.Run(name, func(t *testing.T) {
			r := newTwoHomes(t)
			d := startupChat(t, r, early)
			appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"hello"}`+"\n")
			d.Working(true)
			if n := d.batcher.Pending(); n != 0 {
				t.Fatalf("the first turn starting queued %d turns with the option off", n)
			}
			d.Working(false)
			mustNil(t, d.Close(t.Context()))
			if view, found := r.recordOn(t, r.cell.ID); found {
				t.Fatalf("a record appeared before any turn was sealed: head=%s", view.Cell.Head)
			}
		})
	}
}

// E6, read the other way: the option is read when the turn starts, not once
// at startup, so turning remote control off between chats stops the early
// record on the very next turn.
func TestTheEarlyRecordFollowsTheOptionAtTheMomentTheTurnStarts(t *testing.T) {
	r := newTwoHomes(t)
	on := true
	d := startupChat(t, r, func() bool { return on })
	on = false
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"hello"}`+"\n")
	d.Working(true)
	if n := d.batcher.Pending(); n != 0 {
		t.Fatalf("a turn that started after the option went off queued %d turns", n)
	}
}

// The advisor's check, and the one risk the early record adds (NOTES-POLISH,
// G3): the row exists while only the startup head is behind it, so another
// computer could land on it. A chat held by a computer that is alive and working
// is never taken silently, and once that computer has died and its lease has
// lapsed the open is still refused until the person confirms the card, as for
// every lapsed chat. In neither case does anything touch the words the first
// turn journaled on the computer that spoke them.
func TestAnEarlyRecordIsNotTakenSilentlyAndTheWordsStayWhereTheyWereSpoken(t *testing.T) {
	r := newTwoHomes(t)
	ctx := t.Context()
	d := startupChat(t, r, func() bool { return true })
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"the only copy of these words"}`+"\n")
	d.Working(true)
	waitFor(t, "the working chat's record", func() bool {
		view, found := r.recordOn(t, r.cell.ID)
		return found && view.Cell.Lease.Working
	})
	words, err := os.ReadFile(transcriptOf(r.cell))
	mustNil(t, err)
	seen, _ := r.recordOn(t, r.cell.ID)
	expect := &directory.LeaseExpectation{Device: seen.Cell.Lease.Device, Fence: seen.Cell.Lease.Fence}
	continuer := r.continuerB()

	// A is alive and working: the free-chat open is refused as held.
	if _, err := continuer.OpenAvailable(ctx, r.cell.ID, expect); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("the other computer opened a chat that is working here: %v", err)
	}

	// A dies in the middle of the first turn and its lease lapses. The row is
	// still not opened without the card.
	d.cancel()
	<-d.done
	r.clock.advance(directory.LeaseTTL + time.Second)
	if _, err := continuer.OpenAvailable(ctx, r.cell.ID, expect); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("the other computer opened the lapsed early record without the card: %v", err)
	}

	after, err := os.ReadFile(transcriptOf(r.cell))
	mustNil(t, err)
	if !bytes.Equal(words, after) || !bytes.Contains(after, []byte("the only copy of these words")) {
		t.Fatal("the words the first turn journaled were changed")
	}
}
