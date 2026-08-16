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

	dir, err := v3SessionDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, lines ...string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	older := write("20260815-090102_b7c1.jsonl",
		`{"type":"session","version":1,"id":"s-1","timestamp":"2026-08-15T09:01:02Z"}`,
		`{"type":"message","role":"user","content":"why does the box flicker?","timestamp":"2026-08-15T09:01:03Z"}`)
	newer := write("20260816-150405_a3f2.jsonl",
		`{"type":"session","version":1,"id":"s-2","timestamp":"2026-08-16T15:04:05Z"}`,
		`{"type":"message","role":"user","content":"port the resume picker","timestamp":"2026-08-16T15:04:06Z"}`,
		`{"type":"message","role":"assistant","content":"done","timestamp":"2026-08-16T15:04:07Z"}`,
		`{"type":"message","role":"user","content":"now run the migration","timestamp":"2026-08-16T15:05:00Z"}`,
		`{"type":"title","title":"port the resume picker","timestamp":"2026-08-16T15:05:01Z"}`)
	// Opened and never spoken in — the file a `aforge resume` that was escaped
	// out of leaves behind. It is not a conversation and not a row.
	write("20260816-160000_dead.jsonl",
		`{"type":"session","version":1,"id":"s-3","timestamp":"2026-08-16T16:00:00Z"}`)

	rows := v3RecentSessions(workspace)
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
	if rows := v3RecentSessions(t.TempDir()); len(rows) != 0 {
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
