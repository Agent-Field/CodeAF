package store

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// TestTheMailboxCarriesAnAskAndItsReply: a post is numbered and stamped, a
// take drains it, the reply is collected once, and a second wait for it hears
// nobody answered.
func TestTheMailboxCarriesAnAskAndItsReply(t *testing.T) {
	st := open(t)
	mb := st.Mailbox()
	seq, err := mb.Post(Ask{ID: 4, Verb: factory.VerbSteer, Words: "smaller"})
	if err != nil || seq != 1 {
		t.Fatalf("first post = %d, %v", seq, err)
	}
	asks, err := mb.Take()
	if err != nil || len(asks) != 1 || asks[0].Seq != 1 || asks[0].Words != "smaller" || asks[0].At.IsZero() {
		t.Fatalf("take = %+v, %v", asks, err)
	}
	if again, _ := mb.Take(); len(again) != 0 {
		t.Fatalf("a second take found %+v", again)
	}
	if err := mb.Answer(seq, Reply{Err: "#4 is not running", HabitDue: true}); err != nil {
		t.Fatal(err)
	}
	reply, err := mb.Wait(seq, time.Second)
	if err != nil || reply.Seq != seq || reply.Err != "#4 is not running" || !reply.HabitDue {
		t.Fatalf("wait = %+v, %v", reply, err)
	}
	if _, err := mb.Wait(seq, 0); !errors.Is(err, factory.ErrRunnerSilent) {
		t.Fatalf("a collected reply was found again: %v", err)
	}
}

// TestAWaitSeesAReplyWrittenWhileItWaits: the waiting side polls.
func TestAWaitSeesAReplyWrittenWhileItWaits(t *testing.T) {
	st := open(t)
	mb := st.Mailbox()
	seq, _ := mb.Post(Ask{ID: 1, Verb: factory.VerbLaunch})
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = mb.Answer(seq, Reply{})
	}()
	if reply, err := mb.Wait(seq, 2*time.Second); err != nil || reply.Seq != seq {
		t.Fatalf("wait = %+v, %v", reply, err)
	}
}

// TestUncollectedRepliesAreDroppedAfterAMinute: a reply older than a minute
// goes when the next reply is written.
func TestUncollectedRepliesAreDroppedAfterAMinute(t *testing.T) {
	st := open(t)
	mb := st.Mailbox()
	if err := mb.Answer(1, Reply{At: time.Now().Add(-2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := mb.Answer(2, Reply{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mb.Wait(1, 0); !errors.Is(err, factory.ErrRunnerSilent) {
		t.Fatalf("a reply two minutes old was still there: %v", err)
	}
	if _, err := mb.Wait(2, 0); err != nil {
		t.Fatalf("a fresh reply was dropped: %v", err)
	}
}

// TestClearEmptiesTheMailboxAndKeepsTheNumbering: the owner's start clears
// both files, and the next ask never reuses a number.
func TestClearEmptiesTheMailboxAndKeepsTheNumbering(t *testing.T) {
	st := open(t)
	mb := st.Mailbox()
	first, _ := mb.Post(Ask{ID: 1, Verb: factory.VerbStop})
	_ = mb.Answer(first, Reply{})
	if err := mb.Clear(); err != nil {
		t.Fatal(err)
	}
	if asks, _ := mb.Take(); len(asks) != 0 {
		t.Fatalf("a clear left %+v", asks)
	}
	if _, err := mb.Wait(first, 0); !errors.Is(err, factory.ErrRunnerSilent) {
		t.Fatalf("a clear left a reply: %v", err)
	}
	if next, _ := mb.Post(Ask{ID: 1, Verb: factory.VerbStop}); next <= first {
		t.Fatalf("after a clear the number went from %d to %d", first, next)
	}
}

// TestPostsFromManyWindowsNeverShareANumber: the flock makes the numbering one
// critical section.
func TestPostsFromManyWindowsNeverShareANumber(t *testing.T) {
	st := open(t)
	other, err := Open(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[int]bool{}
	)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mb := st.Mailbox()
			if i%2 == 1 {
				mb = other.Mailbox()
			}
			seq, err := mb.Post(Ask{ID: i, Verb: factory.VerbLaunch})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if seen[seq] {
				t.Errorf("number %d was given twice", seq)
			}
			seen[seq] = true
		}()
	}
	wg.Wait()
	if asks, _ := st.Mailbox().Take(); len(asks) != 20 {
		t.Fatalf("took %d asks, want 20", len(asks))
	}
}

// TestATornMailboxIsAnEmptyOne: garbage in either file does not stop a post.
func TestATornMailboxIsAnEmptyOne(t *testing.T) {
	st := open(t)
	for _, path := range []string{st.AsksPath(), st.RepliesPath()} {
		if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Mailbox().Post(Ask{ID: 1, Verb: factory.VerbLaunch}); err != nil {
		t.Fatalf("a torn mailbox refused a post: %v", err)
	}
}

// TestTheMailboxIsNotAnItem: neither file is read back as a row on the floor.
func TestTheMailboxIsNotAnItem(t *testing.T) {
	st := open(t)
	_, _ = st.Mailbox().Post(Ask{ID: 1, Verb: factory.VerbLaunch})
	_ = st.Mailbox().Answer(1, Reply{})
	items, err := st.List()
	if err != nil || len(items) != 0 {
		t.Fatalf("the floor read %d items from the mailbox: %v", len(items), err)
	}
}
