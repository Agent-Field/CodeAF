package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/provider"
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
	if err := AppendProgress(path, "plan done · 2m · $0.04 · read the ledger first", nil); err != nil {
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
	if err := AppendProgress(path, "plan started", nil); err != nil {
		t.Fatal(err)
	}
	typedLine(t, path, "use the second ledger", t0.Add(time.Second))
	// A CARRY-ON is codeaf's own nudge in the person's seat, not a line typed.
	typedLine(t, path, carryOnLead+"You stopped, but what was asked is not finished.", t0.Add(2*time.Second))
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
	if err := AppendProgress(path, "plan started", nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a deleted conversation answered %v", err)
	}
	if err := AppendProgress(filepath.Join(t.TempDir(), "nowhere.jsonl"), "plan started", nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a missing conversation answered %v", err)
	}
}

// appendMarked writes an assistant message with the given presentation mark
// (nil for none) the way AppendProgress does, so the test reads back what the
// journal really holds.
func appendMarked(t *testing.T, path, text string, mark *messagePresentation) {
	t.Helper()
	journal, _, err := openSessionFile(journalKey(path), "", "", sessionIDOfFolder(journalKey(path)))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	message := textMessage("assistant", text)
	if mark != nil {
		journal.presentation.remember(message, mark)
	}
	journal.appendReasonedMessage(message, provider.MessageReasoning{})
}

func TestOnlyTheRunnersKindMarksAnEntryAsFactoryProgress(t *testing.T) {
	path := seededTalk(t)
	if err := AppendProgress(path, "plan done · 2m", nil); err != nil {
		t.Fatal(err)
	}
	appendMarked(t, path, "An ordinary answer.", nil)
	appendMarked(t, path, "A line of some future kind.", &messagePresentation{Audience: "human", Kind: "factory-progress-v2"})

	// THE MARK SURVIVES A REREAD: each replay of the journal shapes the same.
	for pass := 1; pass <= 2; pass++ {
		replayed, err := replaySessionFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, e := range shapeEntries(replayed.messages, &sessionFile{presentation: replayed.presentation}) {
			if e.Role == "assistant" {
				got[e.Text] = e.FactoryProgress
			}
		}
		want := map[string]bool{
			"plan done · 2m":              true,
			"An ordinary answer.":         false,
			"A line of some future kind.": false,
		}
		if len(got) != len(want) {
			t.Fatalf("pass %d: assistant entries %v", pass, got)
		}
		for text, mark := range want {
			if seen, ok := got[text]; !ok || seen != mark {
				t.Fatalf("pass %d: %q FactoryProgress = %v (present %v), want %v", pass, text, seen, ok, mark)
			}
		}
	}
}

// stepOf is the step mark a record keeps beside the runner's line saying
// text, found at the line's place in the record's own count, and whether the
// line is in the record at all.
func stepOf(t *testing.T, record Record, text string) (*factory.StepProgress, bool) {
	t.Helper()
	told := append(append([]DisplayEntry(nil), record.Earlier...), record.Entries[min(record.Floor, len(record.Entries)):]...)
	for at, e := range told {
		if e.Role == "assistant" && e.Text == text {
			if !e.FactoryProgress {
				t.Fatalf("%q is not marked as the runner's", text)
			}
			if step, ok := record.Progress[at]; ok {
				return &step, true
			}
			return nil, true
		}
	}
	return nil, false
}

// A LINE THAT REPORTS ONE STEP KEEPS THAT STEP BESIDE ITS WORDS, through the
// journal and every reading of it, and a line about the job keeps none. The
// mark is the record's, beside the entry and never on it. The words are the
// line's own, so a surface that reads only the words reads what it always did.
func TestAStepLineKeepsItsStepBesideItsWords(t *testing.T) {
	path := seededTalk(t)
	review := &factory.StepProgress{Step: "review", Round: 2, Event: factory.ProgressDone}
	if err := AppendProgress(path, "review done · 4m · clean", review); err != nil {
		t.Fatal(err)
	}
	if err := AppendProgress(path, "landed · proof sheet ready · your approval", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"step":{"step":"review","round":2,"event":"done"}`) {
		t.Fatalf("the journal does not keep the step:\n%s", data)
	}
	record := ReadTranscript(path)
	if got, ok := stepOf(t, record, "review done · 4m · clean"); !ok || got == nil || *got != *review {
		t.Fatalf("the step line reads back with %+v (present %v)", got, ok)
	}
	if got, ok := stepOf(t, record, "landed · proof sheet ready · your approval"); !ok || got != nil {
		t.Fatalf("the job line reads back with %+v (present %v)", got, ok)
	}
	if len(record.Progress) != 1 {
		t.Fatalf("the record keeps marks %+v", record.Progress)
	}
}

// A STEP LINE'S MARK OUTLIVES THE PROCESS THAT WROTE IT. A line written
// through a conversation open in this process, then another written after that
// conversation closed and a new one opened on the same journal (a restart),
// both read back from disk with their marks, in their places.
func TestAStepLinesMarkIsReadBackFromDiskAfterARestart(t *testing.T) {
	path := seededTalk(t)
	first, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	if _, ok := LiveAgentFor(path); !ok {
		t.Fatal("the conversation is not open on its journal")
	}
	plan := &factory.StepProgress{Step: "plan", Round: 1, Event: factory.ProgressDone}
	if err := AppendProgress(path, "plan done · 2m", plan); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, _ := newTestAgent(t, answering(), func(c *Config) { c.SessionFile = path })
	review := &factory.StepProgress{Step: "review", Round: 1, Event: factory.ProgressStarted}
	if err := AppendProgress(path, "review started", review); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	record := ReadTranscript(path)
	for _, want := range []struct {
		text string
		step *factory.StepProgress
	}{{"plan done · 2m", plan}, {"review started", review}} {
		if got, ok := stepOf(t, record, want.text); !ok || got == nil || *got != *want.step {
			t.Fatalf("%q reads back from disk with %+v (present %v), want %+v", want.text, got, ok, want.step)
		}
	}
}

// A LINE IS TOLD TO THE PROGRESS READER ONCE IT IS ON DISK, from the desk, with
// its words as the journal keeps them and its step; a line that was not written
// is not told.
func TestAWrittenProgressLineIsToldOnceItLands(t *testing.T) {
	path := seededTalk(t)
	var heard []ProgressNews
	var onDisk []bool
	previous := OnProgress(func(news ProgressNews) {
		data, _ := os.ReadFile(news.Path)
		onDisk = append(onDisk, strings.Contains(string(data), news.Text))
		heard = append(heard, news)
	})
	t.Cleanup(func() { OnProgress(previous) })
	started := &factory.StepProgress{Step: "plan", Round: 1, Event: factory.ProgressStarted}
	if err := AppendProgress(path, "plan   started", started); err != nil {
		t.Fatal(err)
	}
	gone := seededTalk(t)
	if err := os.WriteFile(filepath.Join(filepath.Dir(gone), conversationDeletedFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendProgress(gone, "plan started", started); err == nil {
		t.Fatal("a deleted conversation took a line")
	}
	// The reader is told from the desk, never on the writer's goroutine.
	progressDesk.settled()
	if len(heard) != 1 || heard[0].Path != path || heard[0].Text != "plan started" || heard[0].Step != started || !onDisk[0] {
		t.Fatalf("the reader heard %+v (on disk %v)", heard, onDisk)
	}
}
