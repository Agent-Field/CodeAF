package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

// A RENAME ASKED ONLY WHEN THE NAME IS STILL THE OLD ONE leaves a name the
// person gave alone, and reads the name under the journal's claim.
func TestRetitleConversationWhenLeavesAnotherNameAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := SeedConversation(path, t.TempDir(), "my own name", "the opening note"); err != nil {
		t.Fatal(err)
	}
	isManager := func(current string) bool { return current == "Factory manager" }
	if renamed, err := RetitleConversationWhen(path, "Factory manager (earlier)", isManager); err != nil || renamed {
		t.Fatalf("a conversation the person named was renamed: %v, %v", renamed, err)
	}
	if replayed, err := replaySessionFile(path); err != nil || replayed.title != "my own name" {
		t.Fatalf("the declined rename left the name %q (%v)", replayed.title, err)
	}
	if renamed, err := RetitleConversationWhen(path, "Factory manager (earlier)", func(current string) bool { return current == "my own name" }); err != nil || !renamed {
		t.Fatalf("a rename whose condition held answered %v, %v", renamed, err)
	}
	if replayed, err := replaySessionFile(path); err != nil || replayed.title != "Factory manager (earlier)" {
		t.Fatalf("the rename left the name %q (%v)", replayed.title, err)
	}
}

// THE ROW'S NAME CHANGES IN ONE LOCKED TRANSACTION, the way its archive flag
// does: a load, an edit and a save made separately lost whatever a live
// conversation wrote to the row between them.
func TestSetTitleKeepsAWriteMadeAtTheSameTime(t *testing.T) {
	for round := range 40 {
		dir := t.TempDir()
		if err := SaveMeta(dir, Meta{ID: "c" + strconv.Itoa(round), Title: "Factory manager"}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := SetTitle(dir, "Factory manager (earlier)", func(current string) bool { return current == "Factory manager" }); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := SetArchived(dir, true); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		meta, err := LoadMeta(dir)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Title != "Factory manager (earlier)" || !meta.Archived || !meta.LiteralName {
			t.Fatalf("round %d: the row reads %q, archived %v, literal %v; a write was lost", round, meta.Title, meta.Archived, meta.LiteralName)
		}
	}
	dir := t.TempDir()
	if err := SaveMeta(dir, Meta{ID: "c", Title: "my own name"}); err != nil {
		t.Fatal(err)
	}
	if renamed, err := SetTitle(dir, "Factory manager (earlier)", func(current string) bool { return current == "Factory manager" }); err != nil || renamed {
		t.Fatalf("a row the person named was renamed: %v, %v", renamed, err)
	}
	if meta, _ := LoadMeta(dir); meta.Title != "my own name" {
		t.Fatalf("the declined rename left the row %q", meta.Title)
	}
	if _, err := SetTitle(t.TempDir(), "a name", nil); err == nil {
		t.Error("a folder with no conversation was named")
	}
}
