package cellstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
)

func writeRecord(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readRecord(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Contract: when a chat's records are restored behind an open chat, a record
// that is still exactly the older copy this computer carried over from before
// the move is replaced by the head's version, even when the two have the same
// length; a record the person or the chat wrote since the chat opened is live
// work and stays; and a record the head has and this computer lacks arrives.
func TestRestoreReplacesACarriedOlderRecordButKeepsOneWrittenSince(t *testing.T) {
	e := Engine{DataRoot: t.TempDir()}
	c := cell.Cell{ID: "chat", Root: t.TempDir()}
	state := filepath.Join(c.Root, cell.StateDir)
	head := t.TempDir()

	// The head's records, as the far side last wrote them.
	writeRecord(t, filepath.Join(head, "tasks", "older.jsonl"), "head version of a task\n")
	writeRecord(t, filepath.Join(head, "tasks", "edited.jsonl"), "head version of another\n")
	writeRecord(t, filepath.Join(head, "tasks", "arriving.jsonl"), "a task only the head has\n")

	// This computer's older copies, carried over by the take (same lengths).
	writeRecord(t, filepath.Join(state, "tasks", "older.jsonl"), "OLDER version of a task\n")
	writeRecord(t, filepath.Join(state, "tasks", "edited.jsonl"), "OLDER version of another\n")
	carried := map[string]CarriedFile{}
	for _, name := range []string{"older.jsonl", "edited.jsonl"} {
		info, err := os.Stat(filepath.Join(state, "tasks", name))
		if err != nil {
			t.Fatal(err)
		}
		carried["tasks/"+name] = FingerprintOf(info)
	}
	if err := e.NoteCarried(c, carried); err != nil {
		t.Fatal(err)
	}

	// The chat opens; one carried record is then written locally.
	edited := filepath.Join(state, "tasks", "edited.jsonl")
	writeRecord(t, edited, "LOCAL edit after the open\n")
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(edited, later, later); err != nil {
		t.Fatal(err)
	}

	if err := mergeMissingState(head, state, e.carriedStale(c)); err != nil {
		t.Fatal(err)
	}

	if got := readRecord(t, filepath.Join(state, "tasks", "older.jsonl")); got != "head version of a task\n" {
		t.Errorf("a carried older record was kept: %q", got)
	}
	if got := readRecord(t, edited); got != "LOCAL edit after the open\n" {
		t.Errorf("a record written since the chat opened was overwritten: %q", got)
	}
	if got := readRecord(t, filepath.Join(state, "tasks", "arriving.jsonl")); got != "a task only the head has\n" {
		t.Errorf("a record only the head has did not arrive: %q", got)
	}
}

// With no note of what was carried, an existing record is never replaced: it
// may be anything the chat has written.
func TestRestoreKeepsEveryExistingRecordWhenNothingWasCarried(t *testing.T) {
	e := Engine{DataRoot: t.TempDir()}
	c := cell.Cell{ID: "chat", Root: t.TempDir()}
	state := filepath.Join(c.Root, cell.StateDir)
	head := t.TempDir()
	writeRecord(t, filepath.Join(head, "tasks", "a.jsonl"), "head\n")
	writeRecord(t, filepath.Join(state, "tasks", "a.jsonl"), "mine\n")
	if err := mergeMissingState(head, state, e.carriedStale(c)); err != nil {
		t.Fatal(err)
	}
	if got := readRecord(t, filepath.Join(state, "tasks", "a.jsonl")); got != "mine\n" {
		t.Errorf("a record with no carried note was replaced: %q", got)
	}
}
