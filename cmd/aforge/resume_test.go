package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The door's half of the picker: what is on disk, listed as rows the surface
// can draw — newest first, named, with the last thing that happened in each.
func TestRecentSessionsListsThisDirectorysConversations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()

	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	// One folder per conversation, named by its id, with the journal inside it:
	// the layout the door writes (docs/CHAT-V3.md, Decision 26).
	write := func(id string, lines ...string) string {
		folder := filepath.Join(bucket, id)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(folder, "transcript.jsonl")
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	older := write("00000000000000a1",
		`{"type":"session","version":1,"id":"s-1","timestamp":"2026-08-15T09:01:02Z"}`,
		`{"type":"message","role":"user","content":"why does the box flicker?","timestamp":"2026-08-15T09:01:03Z"}`)
	newer := write("00000000000000a2",
		`{"type":"session","version":1,"id":"s-2","timestamp":"2026-08-16T15:04:05Z"}`,
		`{"type":"message","role":"user","content":"port the resume picker","timestamp":"2026-08-16T15:04:06Z"}`,
		`{"type":"message","role":"assistant","content":"done","timestamp":"2026-08-16T15:04:07Z"}`,
		`{"type":"message","role":"user","content":"now run the migration","timestamp":"2026-08-16T15:05:00Z"}`,
		`{"type":"title","title":"port the resume picker","timestamp":"2026-08-16T15:05:01Z"}`)
	// Opened and never spoken in — the file a `aforge resume` that was escaped
	// out of leaves behind. It is not a conversation and not a row.
	write("00000000000000a3",
		`{"type":"session","version":1,"id":"s-3","timestamp":"2026-08-16T16:00:00Z"}`)

	rows := v3RecentSessions(bucket)
	if len(rows) != 2 {
		t.Fatalf("listed %d rows, want the two conversations: %+v", len(rows), rows)
	}
	if rows[0].File != newer || rows[1].File != older {
		t.Fatalf("listed %s then %s, want newest first", rows[0].File, rows[1].File)
	}
	if rows[0].Title != "port the resume picker" {
		t.Fatalf("the named session is called %q", rows[0].Title)
	}
	if rows[0].Last != "now run the migration" {
		t.Fatalf("the description is %q, want the last thing said", rows[0].Last)
	}
	if rows[1].Title != "" || rows[1].Opening != "why does the box flicker?" {
		t.Fatalf("an unnamed session lost its opening line: %+v", rows[1])
	}
	if rows[0].At.IsZero() || rows[1].At.IsZero() {
		t.Fatal("a row with no age is a row a picker cannot order")
	}
}

// A directory that has never held a session lists nothing, and lists it without
// failing: the answer feeds a picker that says so in words.
func TestRecentSessionsIsEmptyBeforeTheFirstConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fresh, err := v3ProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rows := v3RecentSessions(fresh); len(rows) != 0 {
		t.Fatalf("a fresh directory listed %+v", rows)
	}
}

// `aforge resume` opens a picker, and a picker needs somebody watching it.
func TestResumeRefusesTheHeadlessDoor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	err := runResumeV3([]string{"--once", "hello"})
	if err == nil || !strings.Contains(err.Error(), "aforge chat --once") {
		t.Fatalf("resume --once said %v, want the door it should have used", err)
	}
}
