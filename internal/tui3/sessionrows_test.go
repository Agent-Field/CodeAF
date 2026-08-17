package tui3

import (
	"strings"
	"testing"
	"time"
)

// The two lists of past conversations, now that a session is a folder.

// A SESSION IS CALLED WHAT IT CALLED ITSELF, and the title a folder's meta.json
// carries arrives here as [Session.Title] — the top rung of the ladder, ahead
// of the opening line and ahead of anything derived from a path.
func TestAFolderSessionIsNamedByItsOwnTitle(t *testing.T) {
	row := Session{
		Title:   "port the resume picker",
		Opening: "please port the resume picker to tui3",
		File:    "/home/p/.aforge/v3/projects/lab/0123456789abcdef/transcript.jsonl",
		At:      time.Now().Add(-2 * time.Hour),
	}
	if got, want := humanName(row), "Port the Resume Picker"; got != want {
		t.Fatalf("the row is called %q, want %q", got, want)
	}
}

// A SESSION THAT NAMED ITSELF NOTHING FALLS BACK TO THE FOLDER AND NOT TO THE
// FILE. Every folder session's transcript is called the same thing, so the old
// last rung would have called every unnamed conversation "Transcript".
func TestAnUnnamedFolderSessionFallsBackToItsFolder(t *testing.T) {
	row := Session{File: "/home/p/.aforge/v3/projects/lab/0123456789abcdef/transcript.jsonl"}
	name := humanName(row)
	if strings.Contains(strings.ToLower(name), "transcript") {
		t.Fatalf("the row is called %q — the file's name identifies nothing", name)
	}
	if !strings.Contains(strings.ToLower(name), "0123456789abcdef") {
		t.Fatalf("the row is called %q, want the session's own folder", name)
	}
}

// A LEGACY ROW KEEPS EXACTLY TODAY'S RENDERING. The flat layout is on the same
// disk until the migration has run everywhere, and a picker that renamed those
// rows would be a picker that lost somebody their work on the day they
// upgraded.
func TestALegacySessionRowIsUnchanged(t *testing.T) {
	flat := "/home/p/.aforge/v3/sessions/lab/20260816-150405_a3f2.jsonl"
	if got, want := humanName(Session{File: flat}), "20260816 150405 A3f2"; got != want {
		t.Fatalf("the legacy row is called %q, want %q", got, want)
	}
	// The welcome box's own rung is the base name, extension and all.
	if got, want := sessionStem(flat), "20260816-150405_a3f2.jsonl"; got != want {
		t.Fatalf("the box's fallback is %q, want %q", got, want)
	}
}

// THE AGE IS THE PERSON'S OWN LAST TURN. The listing takes it from the folder's
// meta.json rather than from the file (internal/session's recentplace.go), and
// the row draws it dim beside what was last happening.
func TestASessionRowDrawsItsAge(t *testing.T) {
	row := Session{
		Title: "port the resume picker",
		Last:  "now run the migration",
		File:  "/home/p/.aforge/v3/projects/lab/0123456789abcdef/transcript.jsonl",
		At:    time.Now().Add(-2 * time.Hour),
	}
	note := sessionNote(row, 100, humanName(row))
	if !strings.Contains(note, "2h ago") {
		t.Fatalf("the row's tail is %q, want the age on it", note)
	}
	if !strings.Contains(note, "now run the migration") {
		t.Fatalf("the row's tail is %q, want what was last happening", note)
	}
}
