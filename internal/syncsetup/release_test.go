package syncsetup

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/session"
)

// staleHolder is the engine daemon's in-memory session of a chat that another
// machine has since moved on: it holds the journal's lock and, like the real
// host's doorstep, lets go when a request appears beside the journal.
type staleHolder struct {
	file     *os.File
	released chan string
}

func holdJournal(t *testing.T, root string) *staleHolder {
	t.Helper()
	file, err := os.Open(session.Place{Dir: root}.Transcript())
	if err != nil {
		t.Fatal(err)
	}
	if err := filelock.Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	h := &staleHolder{file: file, released: make(chan string, 1)}
	go h.answer(root)
	t.Cleanup(func() { file.Close() })
	return h
}

// answer lets go the way the host does: the request comes off the disk and the
// journal is unlocked, noting what the journal held at that moment.
func (h *staleHolder) answer(root string) {
	for {
		if _, err := os.Stat(session.TakeoverPath(root)); err == nil {
			raw, _ := os.ReadFile(session.Place{Dir: root}.Transcript())
			session.CancelTakeover(root)
			_ = filelock.Unlock(h.file)
			h.released <- string(raw)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A take-back onto a machine whose engine still holds the old session of the
// chat retires that session first. Left holding, the engine hands the window
// that session again: the conversation as it was before the other machine moved
// it on, with every tool refused as superseded.
func TestTakeBackRetiresTheStaleSessionBeforeTheTranscriptMoves(t *testing.T) {
	h := bContinued(t)
	stale := holdJournal(t, h.cell.Root)
	before, err := os.ReadFile(transcriptOf(h.cell))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.continuerA().Take(context.Background(), h.cell.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case seen := <-stale.released:
		if seen != string(before) {
			t.Fatal("the stale session was retired after the transcript moved, so its close could write over the new one")
		}
	default:
		t.Fatal("the stale session still holds the chat after the take-back")
	}
	if session.InUse(transcriptOf(h.cell)) {
		t.Fatal("the journal is still locked after the take-back")
	}
}

// A chat is fenced against being opened for the whole of its takeover, from the
// retiring of the old session to the last hook, and open again the moment the
// takeover ends. A window that opens the chat in between boots a session on the
// journal as it was before the swap, bound to a drive side that was made while
// the other machine still held the lease: every tool refused, no turn saved.
func TestAChatIsFencedAgainstOpeningWhileItIsTaken(t *testing.T) {
	h := bContinued(t)
	c := h.continuerA()
	roots := c.opt.RootFor
	var seen []bool
	c.opt.RootFor = func(id string) string {
		root := roots(id)
		seen = append(seen, handoff.Arriving(root))
		return root
	}
	if _, err := c.Take(context.Background(), h.cell.ID); err != nil {
		t.Fatal(err)
	}
	if len(seen) < 2 {
		t.Fatalf("the take asked where the chat lives %d times; the test no longer samples its steps", len(seen))
	}
	for i, fenced := range seen[1:] { // the first answer is the one the fence itself is made from
		if !fenced {
			t.Fatalf("the chat could be opened at step %d of its takeover", i+1)
		}
	}
	if handoff.Arriving(h.cell.Root) {
		t.Fatal("the chat is still fenced after the takeover ended")
	}
}
