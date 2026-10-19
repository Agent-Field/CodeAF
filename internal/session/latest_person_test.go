package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// G5: only messages shaped as the person's own user line reset a promise.
// Queued records are not conversation; a queued message actually admitted into
// the journal is the person's line and does count, as do their consumed steers.
func TestLatestPersonAuthorshipMatchesShaping(t *testing.T) {
	at := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)
	tests := []struct {
		name, kind, role, text string
		note, queued, person   bool
	}{
		{name: "person", kind: "message", role: "user", text: "carry on", person: true},
		{name: "aside", kind: "message", role: "aside", text: "news"},
		{name: "wake", kind: "message", role: "user", text: ResumeAfterCloseNote, note: true},
		{name: "brief", kind: "message", role: "user", text: "worker instructions", note: true},
		{name: "system", kind: "message", role: "system", text: "instructions"},
		{name: "queued record", kind: "queued", role: "user", text: "later"},
		{name: "note", kind: "message", role: "note", text: "history"},
		{name: "summary", kind: "message", role: "user", text: summaryNotePrefix + " earlier work"},
		{name: "volatile", kind: "message", role: "user", text: volatileNoteOpening + " work"},
		{name: "continuation", kind: "message", role: "user", text: checkpointCarryOnLead + " work"},
		{name: "admitted queued person", kind: "message", role: "user", text: "next", queued: true, person: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			entry := sessionEntry{Type: tc.kind, Role: tc.role, Content: tc.text, Note: tc.note, Timestamp: at.Format(time.RFC3339Nano)}
			if tc.queued {
				entry.Presentation = &messagePresentation{Audience: audiencePerson, Queued: true}
			}
			raw, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			got, found, err := LatestPersonMessageAt(path)
			if err != nil || found != tc.person || (found && !got.Equal(at)) {
				t.Fatalf("latest=%v,%v,%v", got, found, err)
			}
			shaped := false
			for _, e := range ReadTranscript(path).Entries {
				shaped = shaped || e.Role == "user" && !e.ForModel
			}
			if shaped != tc.person {
				t.Fatalf("query=%v shaping=%v", tc.person, shaped)
			}
		})
	}
}

// H1: a real compaction re-journals the retained window with fresh times. Only
// the original person occurrence counts; a message beyond that window does too.
func TestLatestPersonCompactionUsesOriginalTime(t *testing.T) {
	for _, laterPerson := range []bool{false, true} {
		t.Run(map[bool]string{false: "copied person", true: "person after window"}[laterPerson], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			journal, _, err := openSessionFile(path, "/w", "m", "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = journal.Close() })
			original := time.Date(2000, 1, 1, 15, 0, 0, 0, time.UTC)
			if !journal.writeLine(sessionEntry{Type: "message", Role: "user", Content: "finish the report", Timestamp: original.Format(time.RFC3339Nano)}) {
				t.Fatal("original person line was not written")
			}
			journal.appendMessage(textMessage("assistant", "the first file is ready"))
			replayed, err := replaySessionFile(path)
			if err != nil {
				t.Fatal(err)
			}
			window := append([]ai.Message(nil), replayed.messages...)
			journal.appendCompaction(compactionPass{at: original.Add(11 * time.Minute)}, 0, window)
			lines := readLines(t, path)
			var marker, copy sessionEntry
			if err := json.Unmarshal([]byte(lines[3]), &marker); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(lines[4]), &copy); err != nil {
				t.Fatal(err)
			}
			if marker.Type != "compaction" || marker.Window != len(window) || copy.Role != "user" || !lineInstant(copy.Timestamp).After(original) {
				t.Fatalf("fixture did not re-journal the person with a later time: marker=%+v copy=%+v", marker, copy)
			}
			want := original
			if laterPerson {
				want = lineInstant(copy.Timestamp).Add(time.Minute)
				if !journal.writeLine(sessionEntry{Type: "message", Role: "user", Content: "also check the appendix", Timestamp: want.Format(time.RFC3339Nano)}) {
					t.Fatal("later person line was not written")
				}
			}
			got, found, err := LatestPersonMessageAt(path)
			if err != nil || !found || !got.Equal(want) {
				t.Fatalf("latest=%v,%v,%v; want original surviving occurrence %v", got, found, err, want)
			}
		})
	}
}

// H1: the backwards block reader follows replay at torn and CRLF boundaries,
// including a person line whose bytes cross a read-block boundary.
func TestLatestPersonJournalLineEdges(t *testing.T) {
	person := []byte(`{"type":"message","role":"user","content":"carry on","timestamp":"2000-01-01T15:00:00Z"}`)
	tool := []byte(`{"type":"message","role":"tool","content":"` + strings.Repeat("x", personJournalBlockBytes-100) + `"}` + "\n")
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"torn final line", append(append([]byte(nil), person...), []byte("\n{\"type\":\"message\",\"role\":\"user\",\"timestamp\":\"2099-")...)},
		{"CRLF", append(append([]byte(nil), person...), []byte("\r\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"done\"}\r\n")...)},
		{"block edge", append(append(append([]byte(nil), person...), '\n'), tool...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "block edge" {
				boundary := len(tc.raw) - personJournalBlockBytes
				if boundary <= 0 || boundary >= len(person) {
					t.Fatalf("person line does not straddle the read boundary: %d", boundary)
				}
			}
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, tc.raw, 0600); err != nil {
				t.Fatal(err)
			}
			got, found, err := LatestPersonMessageAt(path)
			if err != nil || !found || got.Format(time.RFC3339) != "2000-01-01T15:00:00Z" {
				t.Fatalf("latest=%v,%v,%v", got, found, err)
			}
			if record := ReadTranscript(path); len(record.Entries) == 0 || !record.Entries[0].At.Equal(got) {
				t.Fatalf("query disagrees with replay: %+v", record)
			}
		})
	}
}

// G5: rewind cuts are applied without retaining messages. A placed compaction
// retains original person activity; an older unplaced marker hides that region.
func TestLatestPersonRewindAndCompaction(t *testing.T) {
	old := `{"type":"message","role":"user","content":"old","timestamp":"2026-10-19T15:00:00Z"}` + "\n"
	newer := `{"type":"message","role":"user","content":"new","timestamp":"2026-10-19T16:00:00Z"}` + "\n"
	for _, tc := range []struct{ name, journal, want string }{
		{"rewind", old + newer + `{"type":"rewind","dropped":1}` + "\n", "2026-10-19T15:00:00Z"},
		{"placed compaction", old + `{"type":"compaction","window":1,"summary":"history"}` + "\n" + `{"type":"message","role":"user","content":"[context compacted] history"}` + "\n", "2026-10-19T15:00:00Z"},
		{"unplaced compaction", old + `{"type":"compaction","summary":"history"}` + "\n", ""},
		{"rewind across compaction", old + `{"type":"compaction","window":1,"summary":"history"}` + "\n" + newer + `{"type":"rewind","dropped":2}` + "\n", "2026-10-19T15:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(tc.journal), 0600); err != nil {
				t.Fatal(err)
			}
			got, found, err := LatestPersonMessageAt(path)
			if err != nil || found != (tc.want != "") || (found && got.Format(time.RFC3339) != tc.want) {
				t.Fatalf("latest=%v,%v,%v", got, found, err)
			}
		})
	}
}

// G5, H1: ruling out a rewritten copy requires reaching the preceding marker or
// file start. Both queries cross the journal with the same bounded scratch BYTES.
func TestLatestPersonLongJournalBoundsBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	person := []byte(`{"type":"message","role":"user","content":"finish","timestamp":"2026-10-19T16:00:00Z"}` + "\n")
	tool := []byte(`{"type":"message","role":"tool","content":"` + strings.Repeat("x", 4096) + `"}` + "\n")
	if _, err = file.Write(person); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4096; i++ {
		if _, err = file.Write(tool); err != nil {
			t.Fatal(err)
		}
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	scan := personJournalScan{file: file, end: info.Size()}
	if _, found, err := scan.latest(); err != nil || !found {
		t.Fatalf("long tail=%v,%v", found, err)
	}
	if scan.visited != 4097 || scan.retained > 3*personJournalBlockBytes {
		t.Fatalf("visited=%d scratch bytes=%d", scan.visited, scan.retained)
	}
	if _, err = file.Write(person); err != nil {
		t.Fatal(err)
	}
	info, err = file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	tail := personJournalScan{file: file, end: info.Size()}
	if _, found, err := tail.latest(); err != nil || !found {
		t.Fatalf("latest tail=%v,%v", found, err)
	}
	if tail.visited != 4098 || tail.retained > 2*personJournalBlockBytes {
		t.Fatalf("unexpected scan bounds: visited=%d bytes=%d", tail.visited, tail.retained)
	}
}

func TestLatestPersonRefusesOversizeLine(t *testing.T) {
	scan := personJournalScan{file: bytes.NewReader(bytes.Repeat([]byte("x"), journalLineMax+1)), end: journalLineMax + 1}
	if _, _, err := scan.latest(); err == nil {
		t.Fatal("unbounded line was accepted")
	}
}

// A person line without a usable time cannot establish that an old promise is
// still current. Recovery must refuse the reading rather than guess an age.
func TestLatestPersonMissingTimestampRefusesReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"message","role":"user","content":"new direction"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LatestPersonMessageAt(path); err == nil {
		t.Fatal("unknown person time was treated as safe for replay")
	}
}
