package thread

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSpokenLinesKeepsClocksAndStableIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	const body = `{"type":"session","version":1,"id":"n","cwd":"/tmp","timestamp":"2026-10-11T12:00:00Z"}
{"type":"message","role":"user","content":"ship the proof","timestamp":"2026-10-11T12:00:01.5Z"}
{"type":"message","role":"assistant","content":"Proof is ready.","timestamp":"2026-10-11T12:01:00Z"}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	lines := Read(path)
	if len(lines) != 2 || lines[0].From != "user" || lines[1].From != "manager" || lines[0].At.IsZero() || lines[1].ID == "" {
		t.Fatalf("spoken lines: %+v", lines)
	}
	again := Read(path)
	if lines[1].ID != again[1].ID {
		t.Fatal("reread changed the message ID")
	}
	if got := Read(filepath.Join(t.TempDir(), "absent")); len(got) != 0 {
		t.Fatalf("missing journal: %+v", got)
	}
}

// AN UNCHANGED JOURNAL IS PARSED ONCE. A held job's runner reads its manager's
// journal on every two-second beat; the answer read last is handed back while
// the file and its folder are as they were, and a line appended is read.
func TestAnUnchangedJournalIsParsedOnceAndAnAppendedLineIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	const body = `{"type":"session","version":1,"id":"n","cwd":"/tmp","timestamp":"2026-10-11T12:00:00Z"}
{"type":"message","role":"user","content":"ship the proof","timestamp":"2026-10-11T12:00:01.5Z"}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	was := readParses.Load()
	first := Read(path)
	for range 10 {
		if got := Read(path); len(got) != len(first) || got[0] != first[0] {
			t.Fatalf("a reread changed the answer: %+v", got)
		}
	}
	if n := readParses.Load() - was; n != 1 {
		t.Fatalf("eleven reads of an unchanged journal parsed it %d times; want 1", n)
	}
	first[0].Text = "scribbled"
	if Read(path)[0].Text != "ship the proof" {
		t.Fatal("a caller's change to its answer reached the next reader")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"message","role":"assistant","content":"Proof is ready.","timestamp":"2026-10-11T12:01:00Z"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if got := Read(path); len(got) != 2 || got[1].Text != "Proof is ready." {
		t.Fatalf("the appended line was not read: %+v", got)
	}
}
