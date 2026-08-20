package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTranscript lays a journal down by hand, which is what a peek reads: the
// lines, not an agent.
func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	peekHeader = `{"type":"session","version":1,"id":"s-1","cwd":"/lab","model":"test/model","timestamp":"2026-08-16T10:00:00Z"}`
	peekAsked  = `{"type":"message","role":"user","content":"port the resume picker\nand the second line nobody reads","timestamp":"2026-08-16T10:00:01Z"}`
	peekSaid   = `{"type":"message","role":"assistant","content":"done — it lists by name now","timestamp":"2026-08-16T10:00:02Z"}`
	peekAgain  = `{"type":"message","role":"user","content":"  now run   the migration  ","timestamp":"2026-08-16T10:05:00Z"}`
	peekTitled = `{"type":"title","title":"port the resume picker","timestamp":"2026-08-16T10:05:01Z"}`
)

func TestPeekReadsTheNameTheOpeningAndTheLastThingSaid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeTranscript(t, path, peekHeader, peekAsked, peekSaid, peekAgain, peekTitled,
		// A name written twice is a name that was changed, exactly as the
		// replay reads it: the last one wins.
		`{"type":"title","title":"the resume picker","timestamp":"2026-08-16T10:05:02Z"}`)

	summary, ok := Peek(path)
	if !ok {
		t.Fatal("a conversation has to peek")
	}
	if summary.Title != "the resume picker" {
		t.Fatalf("title is %q", summary.Title)
	}
	// One line, whitespace folded — a picker row is one line.
	if summary.Opening != "port the resume picker" {
		t.Fatalf("opening is %q", summary.Opening)
	}
	if summary.Last != "now run the migration" {
		t.Fatalf("last is %q", summary.Last)
	}
	if want := time.Date(2026, 8, 16, 10, 5, 2, 0, time.UTC); !summary.At.Equal(want) {
		t.Fatalf("at is %v, want the newest line's stamp %v", summary.At, want)
	}
	if summary.File != path {
		t.Fatalf("file is %q", summary.File)
	}
}

// The last thing that happened is what the PERSON said, and what the agent
// answered only when they said it with a picture and no words.
func TestPeekFallsBackToTheAnswerWhenTheLastMessageHadNoWords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeTranscript(t, path, peekHeader,
		`{"type":"message","role":"user","content":"","parts":[{"type":"image","path":"/tmp/a.png","sha256":"deadbeef"}],"timestamp":"2026-08-16T10:00:01Z"}`,
		`{"type":"message","role":"assistant","content":"that is a picture of a cat","timestamp":"2026-08-16T10:00:02Z"}`)

	summary, ok := Peek(path)
	if !ok {
		t.Fatal("a conversation with a picture in it is still a conversation")
	}
	if summary.Last != "that is a picture of a cat" {
		t.Fatalf("last is %q", summary.Last)
	}
}

// A file nobody ever spoke in is not a row in a picker: it has no words on it
// and nothing behind it.
func TestPeekRefusesAFileThatIsNotAConversation(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.jsonl")
	writeTranscript(t, empty, peekHeader)
	if _, ok := Peek(empty); ok {
		t.Fatal("a header with nothing under it is not a conversation")
	}
	if _, ok := Peek(filepath.Join(dir, "gone.jsonl")); ok {
		t.Fatal("a missing file is not a conversation")
	}
	// A half-written tail is the one line a crash can corrupt, and it costs the
	// line rather than the row.
	torn := filepath.Join(dir, "torn.jsonl")
	writeTranscript(t, torn, peekHeader, peekAsked, `{"type":"mess`)
	if summary, ok := Peek(torn); !ok || summary.Opening != "port the resume picker" {
		t.Fatalf("a torn tail lost the conversation: %+v", summary)
	}
}

// Peeking never takes the lock, which is what lets a picker list the very
// session the window it is drawn in is holding open.
func TestPeekDoesNotClaimTheFileAWindowIsHolding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeTranscript(t, path, peekHeader, peekAsked)

	journal, _, err := openSessionFile(path, "/lab", "test/model", "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = journal.Close() }()

	if _, ok := Peek(path); !ok {
		t.Fatal("a live session has to be listable")
	}
}

func TestRecentListsConversationsNewestFirstAndSkipsTheRest(t *testing.T) {
	dir := t.TempDir()
	writeTranscript(t, filepath.Join(dir, "old.jsonl"), peekHeader, peekAsked)
	writeTranscript(t, filepath.Join(dir, "new.jsonl"),
		`{"type":"session","version":1,"id":"s-2","timestamp":"2026-08-17T09:00:00Z"}`,
		`{"type":"message","role":"user","content":"the newer one","timestamp":"2026-08-17T09:00:01Z"}`)
	writeTranscript(t, filepath.Join(dir, "empty.jsonl"), peekHeader)
	// Not a transcript at all, and not the walk's business.
	writeTranscript(t, filepath.Join(dir, "notes.txt"), "hello")
	// The candidates are ordered by modification time before any of them is
	// read, so the test says what those are rather than relying on how fast the
	// three writes above happened to run.
	for name, age := range map[string]time.Duration{
		"old.jsonl": 3 * time.Hour, "new.jsonl": time.Hour, "empty.jsonl": time.Minute,
	} {
		when := time.Now().Add(-age)
		if err := os.Chtimes(filepath.Join(dir, name), when, when); err != nil {
			t.Fatal(err)
		}
	}

	found := Recent(dir, 10)
	if len(found) != 2 {
		t.Fatalf("listed %d sessions, want the two that were spoken in: %+v", len(found), found)
	}
	if found[0].Opening != "the newer one" {
		t.Fatalf("the list opens with %q, want the newest", found[0].Opening)
	}
	if found[1].Opening != "port the resume picker" {
		t.Fatalf("the second row is %q", found[1].Opening)
	}

	if got := Recent(dir, 1); len(got) != 1 || got[0].Opening != "the newer one" {
		t.Fatalf("a bounded listing gave %+v", got)
	}
	if got := Recent(dir, 0); got != nil {
		t.Fatalf("a listing of nothing gave %+v", got)
	}
	if got := Recent(filepath.Join(dir, "nowhere"), 10); got != nil {
		t.Fatalf("an unreadable directory gave %+v", got)
	}
}

// A file whose lines carry no timestamp still sorts, from the one fact the
// filesystem can always answer.
func TestRecentDatesAStamplessFileFromTheFilesystem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stampless.jsonl")
	writeTranscript(t, path, `{"type":"message","role":"user","content":"no stamps here"}`)
	when := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	found := Recent(dir, 10)
	if len(found) != 1 {
		t.Fatalf("listed %+v", found)
	}
	if found[0].At.IsZero() {
		t.Fatal("a row with no age is a row a picker cannot order")
	}
}

// ── THE LAST THING THE AGENT SAID ───────────────────────────────────────────

// A NODE'S REPORT IS ITS FINAL ASSISTANT MESSAGE, and [PeekReport] is how a
// surface holding one row of the project's record follows that row's transcript
// address to the whole of it. [TaskIndexEntry.Outcome] is the same message's
// first sentence, cut — one source, read twice.
func TestPeekReportIsTheLastThingTheAgentSaid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.jsonl")
	writeTranscript(t, path,
		peekHeader,
		peekAsked,
		`{"type":"message","role":"assistant","content":"looking at the grammar first"}`,
		`{"type":"message","role":"assistant","content":"Ported the parser.\nThe suite passes."}`,
	)
	said, ok := PeekReport(path)
	if !ok {
		t.Fatal("a journal with an answer in it read back nothing")
	}
	if said != "Ported the parser.\nThe suite passes." {
		t.Fatalf("the report reads %q", said)
	}
}

// A JOURNAL WITH NOTHING SAID IN IT ANSWERS FALSE, and so does one that is not
// there — a surface that drew an empty quotation would be claiming the task
// finished without a word.
func TestPeekReportRefusesAJournalWithNoAnswerInIt(t *testing.T) {
	dir := t.TempDir()
	quiet := filepath.Join(dir, "quiet.jsonl")
	writeTranscript(t, quiet, peekHeader, peekAsked)
	if said, ok := PeekReport(quiet); ok {
		t.Fatalf("a journal nobody answered in read back %q", said)
	}
	if _, ok := PeekReport(filepath.Join(dir, "never-written.jsonl")); ok {
		t.Fatal("a journal that is not there read back a report")
	}
}

// AND IT IS BOUNDED. The report is a page or two; a journal line somebody pasted
// a whole file into is not a thing a card scrolls through.
func TestPeekReportIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.jsonl")
	writeTranscript(t, path, peekHeader, peekAsked,
		`{"type":"message","role":"assistant","content":"`+strings.Repeat("a", reportPeekMax*2)+`"}`)
	said, ok := PeekReport(path)
	if !ok {
		t.Fatal("a long answer read back nothing")
	}
	if len(said) > reportPeekMax+len("…") {
		t.Fatalf("the report came back %d bytes long", len(said))
	}
	if !strings.HasSuffix(said, "…") {
		t.Fatal("a report that was cut does not say so")
	}
}
