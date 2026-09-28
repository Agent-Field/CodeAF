package run

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// TestANoteTheSpliceRefusedIsStillUnread is the test the note channel's own
// tests could not be, and the reason it is here is worth stating.
//
// A note is handed to a working turn by a mid-turn splice, and the splice can
// refuse: the worker's own turn ends in the gap between the step that brought
// the note and the steer that would have landed it, and there is then nothing
// to splice into. Marked read on a refusal, the note has been delivered to
// nobody and will never be offered again — the one thing a channel may not do.
//
// THAT BRANCH IS ALMOST UNREACHABLE FROM THE OUTSIDE. Through a real worker it
// showed only as a flake, about one run in twelve under the race detector, and
// the fix for the flake — seats that keep working until they have been told the
// thing, instead of finishing in the same breath — is exactly what stops the
// turn from ending in that gap. So the end-to-end tests are green whether the
// mark is written on a refusal or not, which makes them no proof of this at
// all. Reached through a steer of its own, the branch is one assertion.
func TestANoteTheSpliceRefusedIsStillUnread(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), "run-test", "root", "The run", "drive")
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	if _, err := store.AddPersonNote(store.RootID(), "the fixture regenerates itself"); err != nil {
		t.Fatalf("leave the note: %v", err)
	}
	read := map[string]bool{}

	// THE SPLICE REFUSES. This is a turn that has already ended.
	refused := 0
	deliverNotes(store, store.RootID(), read, func(string) error {
		refused++
		return errors.New("session: nothing to steer")
	})
	if refused != 1 {
		t.Fatalf("the delivery tried to splice %d times, want the one attempt", refused)
	}
	if len(read) != 0 {
		t.Fatalf("a note the splice refused was marked read: %v", read)
	}

	// SO THE NEXT BOUNDARY OFFERS IT AGAIN, which is the whole point of not
	// marking it: an unread note is a note nobody has been told.
	var landed string
	deliverNotes(store, store.RootID(), read, func(words string) error {
		landed = words
		return nil
	})
	if landed == "" {
		t.Fatal("the note the splice refused was never offered again")
	}
	if len(read) != 1 {
		t.Fatalf("a note that landed was not marked read: %v", read)
	}

	// AND NOW IT IS NOT OFFERED A THIRD TIME.
	again := false
	deliverNotes(store, store.RootID(), read, func(string) error {
		again = true
		return nil
	})
	if again {
		t.Fatal("a note already handed over was handed over a second time")
	}

	// AND THE STORE STILL HOLDS IT THROUGHOUT, for the page the person opens:
	// delivery touches the worker's own mark and nothing else.
	if notes := store.Notes(store.RootID(), 0); len(notes) != 1 {
		t.Fatalf("the store's notes after all of that = %#v, want the one note still there", notes)
	}
}
