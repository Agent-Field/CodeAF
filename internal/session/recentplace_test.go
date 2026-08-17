package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A directory holding both shapes at once is the state every machine passes
// through on the day it upgrades, so it is the state these tests are written
// about.

// writeSession lays one conversation down by hand, in whichever shape its path
// puts it: one thing the person said, and — where it is given — the name the
// session settled on. It is [writeTranscript] with the lines written for it.
func writeSession(t *testing.T, path, said, title string, at time.Time) {
	t.Helper()
	stamp := at.Format(time.RFC3339Nano)
	lines := []string{`{"type":"message","role":"user","content":"` + said + `","timestamp":"` + stamp + `"}`}
	if title != "" {
		lines = append(lines, `{"type":"title","title":"`+title+`","timestamp":"`+stamp+`"}`)
	}
	writeTranscript(t, path, lines...)
	// The listing's cheap first pass is on modification time, so a journal
	// written for an hour ago has to LOOK an hour old on the disk too.
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("dating %s: %v", path, err)
	}
}

// BOTH SHAPES ARE LISTED, and a folder session is offered by its TRANSCRIPT —
// which is the path a caller opens.
func TestRecentSessionsReadsBothLayouts(t *testing.T) {
	dir := t.TempDir()
	flat := filepath.Join(dir, "20260816-150405_a3f2.jsonl")
	writeSession(t, flat, "the older conversation", "", time.Now().Add(-4*time.Hour))

	place := Place{Dir: filepath.Join(dir, "0123456789abcdef")}
	writeSession(t, place.Transcript(), "the newer conversation", "", time.Now().Add(-time.Hour))

	found := RecentSessions(dir, 10)
	if len(found) != 2 {
		t.Fatalf("%d sessions listed, want both shapes:\n%+v", len(found), found)
	}
	if found[0].File != place.Transcript() {
		t.Fatalf("the newest row is %q, want the folder's transcript", found[0].File)
	}
	if found[1].File != flat {
		t.Fatalf("the older row is %q, want the flat transcript", found[1].File)
	}
}

// THE FOLDER'S OWN RECORD OUTRANKS THE JOURNAL: meta.json is written for this
// reading, its title is the name the session settled on, and its last-user
// stamp is what the order is on — a background write touching a file is not a
// person returning to a conversation.
func TestTheFoldersOwnTitleAndStampWin(t *testing.T) {
	dir := t.TempDir()

	newer := Place{Dir: filepath.Join(dir, "aaaaaaaaaaaaaaaa")}
	writeSession(t, newer.Transcript(), "hello", "what the journal called it", time.Now().Add(-3*time.Hour))
	if err := SaveMeta(newer.Dir, Meta{
		ID:         "aaaaaaaaaaaaaaaa",
		Title:      "what the folder calls it",
		LastUserAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("saving meta: %v", err)
	}

	older := Place{Dir: filepath.Join(dir, "bbbbbbbbbbbbbbbb")}
	writeSession(t, older.Transcript(), "hello again", "", time.Now().Add(-time.Minute))
	if err := SaveMeta(older.Dir, Meta{
		ID:         "bbbbbbbbbbbbbbbb",
		LastUserAt: time.Now().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("saving meta: %v", err)
	}

	found := RecentSessions(dir, 10)
	if len(found) != 2 {
		t.Fatalf("%d sessions listed, want 2", len(found))
	}
	if got := found[0].Title; got != "what the folder calls it" {
		t.Fatalf("the row is titled %q, want the folder's own name", got)
	}
	// The one whose journal was written a minute ago is second, because its
	// PERSON last spoke two hours ago.
	if found[0].File != newer.Transcript() {
		t.Fatalf("the order followed the file and not the person: %q first", found[0].File)
	}
}

// A DIRECTORY WITH NO TRANSCRIPT IN IT IS NOT A SESSION. The v3 home holds
// locks, an index and whatever a person made by hand beside the folders.
func TestADirectoryWithNoTranscriptIsNotListed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "locks"), 0o700); err != nil {
		t.Fatalf("making the stray directory: %v", err)
	}
	place := Place{Dir: filepath.Join(dir, "cccccccccccccccc")}
	writeSession(t, place.Transcript(), "the only conversation", "", time.Now())

	found := RecentSessions(dir, 10)
	if len(found) != 1 || found[0].File != place.Transcript() {
		t.Fatalf("the listing is %+v", found)
	}
}
