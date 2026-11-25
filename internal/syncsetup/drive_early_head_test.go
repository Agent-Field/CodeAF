package syncsetup

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// These tests are the other half of the early directory record (see
// drive_early_record_test.go). That file proves the record is made from a head
// the window sealed at startup. A chat opened with /new has no such head: the
// warm capture seals no empty turn and a journal that holds only its header is
// not grown. So the first spoken turn of a new chat started with NO head, the
// early record had nothing to stand on, and the row appeared when the turn
// ended. The drive now says, once, that a head is owed; the window then seals
// the chat through its own recorder and that seal makes the record.

// freshChat is a drive on a chat that has only been opened: nothing sealed,
// nobody spoken, the state /new leaves.
func freshChat(t *testing.T, r *twoHomes, early func() bool) *Drive {
	t.Helper()
	d, err := r.a.Drive(t.Context(), r.engine, r.cell, DriveOptions{EarlyRecord: early})
	mustNil(t, err)
	t.Cleanup(func() { _ = d.Close(t.Context()) })
	if head, err := cellstore.Head(r.cell); err != nil || head != nil {
		t.Fatalf("the fixture's new chat already has a head (%v, %v)", head, err)
	}
	return d
}

// sealsAsTheWindowDoes is what the window's recorder does when asked to seal a
// chat now: a seal through the drive's own store.
func (r *twoHomes) sealsAsTheWindowDoes(t *testing.T, d *Drive) {
	t.Helper()
	_, err := d.Store(r.engine).Seal(t.Context(), r.cell, cellstore.TurnInfo{})
	mustNil(t, err)
}

// C1: remote control on, a new chat whose first turn runs. The drive says a
// head is owed, once; the seal that follows puts the chat on the other
// computer's list as working while the turn is still running.
func TestTheFirstTurnOfANewChatAsksForAHeadAndThenHasItsRecord(t *testing.T) {
	r := newTwoHomes(t)
	d := freshChat(t, r, func() bool { return true })
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"hello"}`+"\n")
	d.Working(true)
	if _, found := r.recordOn(t, r.cell.ID); found {
		t.Fatal("a record exists before any head was sealed")
	}
	if !d.EarlyHeadOwed() {
		t.Fatal("the first spoken turn of a chat with no head did not ask for one")
	}
	if d.EarlyHeadOwed() {
		t.Fatal("the drive asked for the head twice")
	}
	r.sealsAsTheWindowDoes(t, d)
	waitFor(t, "the working chat's record on the other computer", func() bool {
		view, found := r.recordOn(t, r.cell.ID)
		return found && view.Cell.Lease.Working
	})
	rows, err := r.b.Rows(t.Context())
	mustNil(t, err)
	if len(rows) != 1 || rows[0].Cell != r.cell.ID || !rows[0].Working {
		t.Fatalf("the other computer lists %+v, want the one working chat", rows)
	}
}

// C2: a chat nobody typed in asks for nothing and appears nowhere.
func TestAChatNobodyTypedInNeverAsksForAHead(t *testing.T) {
	r := newTwoHomes(t)
	d := freshChat(t, r, func() bool { return true })
	d.Working(true)
	if d.EarlyHeadOwed() {
		t.Fatal("a chat nobody has spoken in asked for a head")
	}
	r.sealsAsTheWindowDoes(t, d)
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

// C3: with the option off, or never given, or off by the time the turn starts,
// nothing is asked for and the record comes when the turn is sealed, as before.
func TestWithTheOptionOffTheFirstTurnAsksForNoHead(t *testing.T) {
	on := true
	for name, early := range map[string]func() bool{
		"not given":                          nil,
		"off":                                func() bool { return false },
		"turned off after the drive started": func() bool { return on },
	} {
		t.Run(name, func(t *testing.T) {
			r := newTwoHomes(t)
			d := freshChat(t, r, early)
			on = false
			appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"hello"}`+"\n")
			d.Working(true)
			if d.EarlyHeadOwed() {
				t.Fatal("a head was asked for with the option off")
			}
			time.Sleep(200 * time.Millisecond)
			if _, found := r.recordOn(t, r.cell.ID); found {
				t.Fatal("a record appeared before the turn was sealed with the option off")
			}
			r.sealsAsTheWindowDoes(t, d)
			r.durable(r.cell.ID, r.cell)
			if _, found := r.recordOn(t, r.cell.ID); !found {
				t.Fatal("the sealed turn did not make the record")
			}
		})
	}
}

// A chat that already has a head is served by the record made from that head
// (drive_early_record_test.go) and asks for nothing.
func TestAChatWithAHeadAlreadyAsksForNoOther(t *testing.T) {
	r := newTwoHomes(t)
	d := startupChat(t, r, func() bool { return true })
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"hello"}`+"\n")
	d.Working(true)
	if d.EarlyHeadOwed() {
		t.Fatal("a chat with a head asked for another")
	}
}

// C1 and C4 with relay storage off and blind pass-through the road, which is
// the hosted default: the record appears while the turn runs, carries no title
// or folder, and the relay is sent no object.
func TestTheEarlyRecordOfANewChatSendsNothingButTheRecordWhilePassThroughIsTheRoad(t *testing.T) {
	r := newLiveRig(t)
	d := freshChat(t, r.twoHomes, func() bool { return true })
	appendTo(t, transcriptOf(r.cell), `{"type":"message","role":"user","content":"the first words"}`+"\n")
	d.Working(true)
	if !d.EarlyHeadOwed() {
		t.Fatal("the first spoken turn of a chat with no head did not ask for one")
	}
	r.sealsAsTheWindowDoes(t, d)
	waitFor(t, "the working chat's record on the other computer", func() bool {
		view, found := r.recordOn(t, r.cell.ID)
		return found && view.Cell.Lease.Working
	})
	view, _ := r.recordOn(t, r.cell.ID)
	if view.Cell.Title != "" || view.Cell.Folder != "" {
		t.Fatalf("the early record carries a title %q or folder %q", view.Cell.Title, view.Cell.Folder)
	}
	if named := r.wire.recordsWithNames(); len(named) != 0 {
		t.Fatalf("a record carried a name: %v", named)
	}
	if n := r.wire.storeWrites(); n != 0 {
		t.Fatalf("the relay was sent %d objects while pass-through was the road", n)
	}
}
