package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seededTalk is an item's conversation as the Talk door leaves it: a journal
// opening on its brief, which is the session's note and not the person's.
func seededTalk(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "talk", "session.jsonl")
	if err := SeedConversation(path, t.TempDir(), "#12 · fix the ledger", "[factory item #12]\nYou are the manager of this item."); err != nil {
		t.Fatal(err)
	}
	return path
}

// typedLine appends what a person's line looks like in the journal.
func typedLine(t *testing.T, path, words string, at time.Time) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	line := `{"type":"message","role":"user","content":"` + words + `","timestamp":"` + at.UTC().Format(time.RFC3339Nano) + `"}` + "\n"
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

func TestAProgressLineIsTheManagersOwnMessageMarkedForTheTimeline(t *testing.T) {
	path := seededTalk(t)
	if err := AppendProgress(path, "plan done · 2m · $0.04 · read the ledger first"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"presentation":{"audience":"human","kind":"factory-progress"}`) {
		t.Fatalf("the line is not marked as the runner's:\n%s", data)
	}
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	last := replayed.messages[len(replayed.messages)-1]
	if last.Role != "assistant" || messageContentText(last) != "plan done · 2m · $0.04 · read the ledger first" {
		t.Fatalf("the conversation ends on %s %q", last.Role, messageContentText(last))
	}
	if mark := replayed.presentation.of(last); mark == nil || mark.Kind != FactoryProgressKind || mark.Audience != "human" {
		t.Fatalf("the replayed line is marked %+v", mark)
	}
	if got := LastSaid(path); got != "plan done · 2m · $0.04 · read the ledger first" {
		t.Fatalf("LastSaid read %q", got)
	}
}

func TestPersonLinesAreWhatThePersonTypedOnceEachAfterTheMark(t *testing.T) {
	path := seededTalk(t)
	t0 := time.Now().Add(-time.Minute)
	typedLine(t, path, "keep the old API", t0)
	if err := AppendProgress(path, "plan started"); err != nil {
		t.Fatal(err)
	}
	typedLine(t, path, "use the second ledger", t0.Add(time.Second))
	// A COMPACTION'S COPY of the window is not typed again.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`{"type":"compaction","window":1,"timestamp":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}` + "\n")
	_ = f.Close()
	typedLine(t, path, "keep the old API", time.Now())

	all, err := PersonLines(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Words != "keep the old API" || all[1].Words != "use the second ledger" {
		t.Fatalf("the person typed %+v", all)
	}
	after, _ := PersonLines(path, all[0].At)
	if len(after) != 1 || after[0].Words != "use the second ledger" {
		t.Fatalf("after the first line the person typed %+v", after)
	}
}

func TestAProgressLineForADeletedConversationIsGone(t *testing.T) {
	path := seededTalk(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), conversationDeletedFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendProgress(path, "plan started"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a deleted conversation answered %v", err)
	}
	if err := AppendProgress(filepath.Join(t.TempDir(), "nowhere.jsonl"), "plan started"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a missing conversation answered %v", err)
	}
}
