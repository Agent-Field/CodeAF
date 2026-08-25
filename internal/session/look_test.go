package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Seven places share one file, and each one has to come back as its own instant
// — the whole reason a per-place stamp exists is that the machine-wide one says
// the same thing about all of them at once.
func TestEachPlaceKeepsItsOwnLookStamp(t *testing.T) {
	root := t.TempDir()
	morning := time.Date(2026, 8, 25, 9, 15, 0, 0, time.UTC)
	afternoon := morning.Add(5 * time.Hour)

	NoteLookAt(root, "memory", morning)
	NoteLookAt(root, "tasks", afternoon)

	if got := LastLookAt(root, "memory"); !got.Equal(morning) {
		t.Fatalf("memory was left at %s, want %s", got, morning)
	}
	if got := LastLookAt(root, "tasks"); !got.Equal(afternoon) {
		t.Fatalf("tasks was left at %s, want %s", got, afternoon)
	}
	// A place nobody has stood in has no origin, so nothing in it is news.
	if got := LastLookAt(root, "spend"); !got.IsZero() {
		t.Fatalf("a place nobody has opened claims a look at %s", got)
	}
	// One file, not seven.
	if _, err := os.Stat(filepath.Join(root, looksStampName)); err != nil {
		t.Fatalf("the stamps are not in %s: %v", looksStampName, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the places root holds %d files, want the one stamp file: %+v", len(entries), entries)
	}
}

// The stamp moves forward when a place is left again, and the other places do
// not move with it.
func TestLeavingOnePlaceLeavesTheOthersWhereTheyWere(t *testing.T) {
	root := t.TempDir()
	first := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	NoteLookAt(root, "memory", first)
	NoteLookAt(root, "standing", first)
	NoteLookAt(root, "memory", first.Add(time.Hour))

	if got := LastLookAt(root, "memory"); !got.Equal(first.Add(time.Hour)) {
		t.Fatalf("memory is stamped %s", got)
	}
	if got := LastLookAt(root, "standing"); !got.Equal(first) {
		t.Fatalf("standing moved to %s when memory was left", got)
	}
}

// THE MACHINE-WIDE STAMP GOES ON MEANING WHAT IT MEANT. The whole "since you
// left" ledger is measured from it, and a per-place stamp must not touch it.
func TestThePerPlaceStampsLeaveTheMachineWideOneAlone(t *testing.T) {
	root := t.TempDir()
	closed := time.Date(2026, 8, 24, 22, 0, 0, 0, time.UTC)
	NoteLook(root, closed)
	NoteLookAt(root, "memory", closed.Add(12*time.Hour))

	if got := LastLook(root); !got.Equal(closed) {
		t.Fatalf("the machine-wide stamp moved to %s, want %s", got, closed)
	}
	if got := LastLookAt(root, "home"); !got.IsZero() {
		t.Fatalf("home borrowed the machine-wide stamp: %s", got)
	}
}

// Every failure is one fact for every caller — there is no origin — so none of
// them may be an error path.
func TestAnUnreadableStampFileIsSimplyNoOrigin(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, looksStampName), []byte("this is not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := LastLookAt(root, "memory"); !got.IsZero() {
		t.Fatalf("a torn file answered %s", got)
	}
	// And writing over it still works: the next look repairs the file.
	at := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	NoteLookAt(root, "memory", at)
	if got := LastLookAt(root, "memory"); !got.Equal(at) {
		t.Fatalf("a torn file was not repaired by the next look: %s", got)
	}
}

// A root that does not exist is a machine that has held no conversation, and a
// stamp must not be the thing that creates it.
func TestAStampNeverInventsThePlacesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never-made")
	NoteLookAt(root, "memory", time.Now())
	if _, err := os.Stat(root); err == nil {
		t.Fatal("stamping a place created the places root")
	}
	if got := LastLookAt(root, "memory"); !got.IsZero() {
		t.Fatalf("a root that is not there answered %s", got)
	}
}

// Nothing that is not a look gets written down: an empty place, an empty root
// and the zero instant are all refusals rather than rows.
func TestNothingThatIsNotALookIsWrittenDown(t *testing.T) {
	root := t.TempDir()
	NoteLookAt(root, "", time.Now())
	NoteLookAt("", "memory", time.Now())
	NoteLookAt(root, "memory", time.Time{})
	if _, err := os.Stat(filepath.Join(root, looksStampName)); err == nil {
		t.Fatal("a stamp file was written for a look that did not happen")
	}
	if got := LastLookAt(root, ""); !got.IsZero() {
		t.Fatalf("the empty place answered %s", got)
	}
}

// A place is one word and the surface may spell it either way; a tab wearing a
// count must not depend on somebody's capital letter.
func TestAPlaceIsTheSamePlaceHoweverItIsSpelled(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	NoteLookAt(root, "  Memory ", at)
	if got := LastLookAt(root, "memory"); !got.Equal(at) {
		t.Fatalf("memory answered %s after being stamped as \" Memory \"", got)
	}
}
