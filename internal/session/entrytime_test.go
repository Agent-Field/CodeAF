package session

import (
	"testing"
	"time"
)

// EACH ENTRY READS BACK WITH THE INSTANT ITS OWN LINE WAS WRITTEN. A reply that
// came minutes after the question carries its own minute, a call carries its
// message's, and a line with no stamp answers the zero time rather than a
// neighbour's.
func TestEntriesReadBackWithTheirOwnInstants(t *testing.T) {
	const record = `{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}
{"type":"message","role":"user","content":"Fix the nil-map crash","timestamp":"2026-10-15T14:00:00Z"}
{"type":"message","role":"assistant","content":"Looking at the loader.","toolCalls":[{"id":"c1","function":{"name":"read","arguments":"{}"}}],"timestamp":"2026-10-15T14:02:00Z"}
{"type":"message","role":"tool","toolCallId":"c1","content":"189 lines","timestamp":"2026-10-15T14:02:01Z"}
{"type":"message","role":"assistant","content":"Fixed.","timestamp":"2026-10-15T14:09:30Z"}
{"type":"message","role":"user","content":"thanks"}
`
	want := map[string]time.Time{
		"Fix the nil-map crash":  time.Date(2026, 10, 15, 14, 0, 0, 0, time.UTC),
		"Looking at the loader.": time.Date(2026, 10, 15, 14, 2, 0, 0, time.UTC),
		"Fixed.":                 time.Date(2026, 10, 15, 14, 9, 30, 0, time.UTC),
		"thanks":                 {},
	}
	entries := ReadTranscriptBytes([]byte(record)).Entries
	seen := 0
	for _, entry := range entries {
		if entry.Role == "tool" && entry.Tool != "" {
			if !entry.At.Equal(want["Looking at the loader."]) {
				t.Fatalf("the call read back at %v, not its message's instant", entry.At)
			}
			continue
		}
		if entry.Role == "tool" {
			continue
		}
		at, ok := want[entry.Text]
		if !ok {
			continue
		}
		seen++
		if !entry.At.Equal(at) {
			t.Fatalf("%q read back at %v, want %v", entry.Text, entry.At, at)
		}
	}
	if seen != len(want) {
		t.Fatalf("saw %d of %d entries: %#v", seen, len(want), entries)
	}
}

// A MEMBER A MANAGER STARTED IS NAMED BY ITS BRIEF. Its first words are the
// delivery that carried the brief, and the brief, not the delivery's framing,
// is what the conversation is about.
func TestAStartedMembersBriefIsItsNamingQuestion(t *testing.T) {
	delivery := "The manager started you.\n" + teamNewsLead + `"backend"` + ":\n" +
		teamBriefWord + " #7: Rewrite the lexer so tokens carry spans.\n    Keep the tests green.\n"
	if got := teamBrief(delivery); got != "Rewrite the lexer so tokens carry spans.\nKeep the tests green." {
		t.Fatalf("the brief read back as %q", got)
	}
	if got := teamBrief("Team traffic in \"backend\":\n◆ from manager #8: status?\n"); got != "" {
		t.Fatalf("a note that is not a brief named the conversation %q", got)
	}
}
