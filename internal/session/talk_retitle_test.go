package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A CONVERSATION NOBODY HAS OPEN CAN BE RENAMED WITHOUT BEING OPENED.
//
// The factory renames a manager it replaced, and that conversation's folder is
// the one it was moved out of: assembling it there again to write one line
// was the cost the move exists to end. The name goes on the journal as a
// title line, which is what the next open reads, so the name survives it.
func TestRetitleConversationNamesAJournalNobodyHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := SeedConversation(path, t.TempDir(), "Factory manager", "the opening note"); err != nil {
		t.Fatal(err)
	}
	if err := RetitleConversation(path, "  Factory manager   (earlier) "); err != nil {
		t.Fatalf("RetitleConversation: %v", err)
	}
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.title != "Factory manager (earlier)" {
		t.Fatalf("the next open reads the name %q; want the new one", replayed.title)
	}
	// Nothing of the conversation is rewritten: the note it opened with is
	// still its first words.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "the opening note") || strings.Count(string(data), `"type":"session"`) != 1 {
		t.Fatalf("the journal was rewritten:\n%s", data)
	}
}

// A JOURNAL SOMEBODY HOLDS IS REFUSED, AND SO IS A PATH WITH NO CONVERSATION:
// the held one is renamed through its own door, and this door never makes a
// conversation by naming one.
func TestRetitleConversationRefusesAHeldJournalAndAnEmptyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := SeedConversation(path, t.TempDir(), "Factory manager", "the opening note"); err != nil {
		t.Fatal(err)
	}
	held, _, err := openSessionFile(path, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := RetitleConversation(path, "another name"); err == nil {
		t.Error("a journal somebody holds was renamed under them")
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if replayed, err := replaySessionFile(path); err != nil || replayed.title != "Factory manager" {
		t.Fatalf("the refused rename left the name %q (%v)", replayed.title, err)
	}

	missing := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := RetitleConversation(missing, "a name"); err == nil {
		t.Error("a path with no conversation was renamed")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("renaming a path with no conversation made a journal there")
	}
	if err := RetitleConversation(path, "   "); err == nil {
		t.Error("an empty name was accepted")
	}
}
