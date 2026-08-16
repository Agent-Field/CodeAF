package tui3

import (
	"strings"
	"testing"
)

// A NAME IS READ BACK AS WORDS, and an IDENTIFIER IS LEFT ALONE. The second half
// is the one that matters: the fallback in the welcome box is a session FILE's
// name, and a humanizer that could not tell a slug from a timestamp would offer
// a person "20260816 150405 A1b2c3" as the title of yesterday's work.
func TestOneTokenNamesAreReadBackAsWords(t *testing.T) {
	for _, row := range []struct{ raw, want string }{
		{"port_b_parser_fix", "Port B Parser Fix"},
		{"fix-the-nil-map", "Fix The Nil Map"},
		{"porting the parser", "porting the parser"},
		{"porting", "porting"},
		{"20260816-150405_a1b2c3", "20260816-150405_a1b2c3"},
		{"session_2026-08-16.jsonl", "session_2026-08-16.jsonl"},
		{"fix_v2_parser", "Fix V2 Parser"},
		{"  spaced_name  ", "Spaced Name"},
		{"", ""},
	} {
		if got := readableName(row.raw); got != row.want {
			t.Fatalf("readableName(%q) = %q, want %q", row.raw, got, row.want)
		}
	}
}

// The status line draws the read-back name, and the session keeps the raw one:
// a name is for a person, an id is for a resume.
func TestTheStatusLineDrawsTheReadableName(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	a := newTestApp(agent)
	a.width = 120
	a.setTitle("port_b_parser_fix")

	if got := plain(a.status(a.width)); !strings.Contains(got, "Port B Parser Fix") {
		t.Fatalf("the status line drew a machine name:\n%s", got)
	}
	if a.title != "port_b_parser_fix" {
		t.Fatalf("the session's own name was rewritten: %q", a.title)
	}
}

// And so does the welcome box's recent list, whose rows still OPEN the file they
// were read from.
func TestTheRecentListDrawsReadableNames(t *testing.T) {
	w := &welcome{sel: -1, recent: []Session{{Title: "fix_the_nil_map", File: "/tmp/lab/20260816.jsonl"}}}
	row := plain(w.recentRow(0, newPalette(0, false), false))
	if !strings.Contains(row, "Fix The Nil Map") {
		t.Fatalf("the recent row drew a machine name: %q", row)
	}
}
