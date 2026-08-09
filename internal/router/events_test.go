package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

func openTestEvents(t *testing.T) (*Events, string) {
	t.Helper()
	dir := t.TempDir()
	events, err := OpenEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	return events, filepath.Join(dir, "router-events.jsonl")
}

// The log is buffered now, so what has to be pinned is that the file is the one
// it always was: the same rows, in order, one JSON object per line.
func TestEventRowsAreUnchangedByTheBuffering(t *testing.T) {
	events, path := openTestEvents(t)

	for turn := range 500 {
		events.Append(Event{
			Call: callID(), Class: string(provider.ClassExecLeaf), Rung: turn % 3,
			Candidates: []string{"cheap/one", "middle/two", "dear/three"},
			Model:      "cheap/one", Verdict: provider.VerdictUnverifiedSuccess,
			PromptTokens: turn * 10, CompletionTokens: turn, LatencyMS: int64(turn),
		})
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 500 {
		t.Fatalf("wrote %d rows, want 500", len(lines))
	}
	for index, line := range lines {
		var row Event
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("row %d is not one whole JSON object: %v\n%s", index, err, line)
		}
		if row.At.IsZero() || row.Model != "cheap/one" || row.PromptTokens != index*10 {
			t.Fatalf("row %d came back as %+v", index, row)
		}
		// Re-encoding a decoded row reproduces the line, which is the format
		// claim: nothing about the buffering changed how a row is written.
		encoded, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != line {
			t.Fatalf("row %d is not what Marshal writes:\n got %s\nwant %s", index, line, encoded)
		}
	}
}

// Two aforge processes append to one file and O_APPEND is atomic per write, so
// a row must never be split across two of them — including a row that is larger
// than the whole buffer.
func TestARowLargerThanTheBufferIsStillWrittenWhole(t *testing.T) {
	events, path := openTestEvents(t)

	huge := make([]string, 4000)
	for index := range huge {
		huge[index] = strings.Repeat("vendor/model-with-a-very-long-name", 4)
	}
	events.Append(Event{Call: callID(), Candidates: huge, Model: "cheap/one"})
	events.Append(Event{Call: callID(), Model: "dear/three", Final: true})
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < eventBuffer {
		t.Fatalf("the oversized row is only %d bytes; it is not testing what it says", len(data))
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2", len(lines))
	}
	for index, line := range lines {
		var row Event
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("line %d is not one whole row: %v", index, err)
		}
	}
}

// The settled verdict closes an account, and an account that has closed is on
// disk without waiting for the buffer to fill.
func TestASettledVerdictLandsWithoutWaitingForTheBuffer(t *testing.T) {
	events, path := openTestEvents(t)
	defer events.Close()

	events.Append(Event{Call: "abc", Model: "cheap/one", Verdict: provider.VerdictUnverifiedSuccess})
	events.Append(Event{Call: "abc", Model: "cheap/one", Verdict: provider.VerdictVerifiedSuccess, Final: true})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 2 {
		t.Fatalf("the closed account left %d rows on disk, want the attempt and the verdict", lines)
	}
}

// A leaf's verdict is reported by whichever goroutine settled it, which may be
// while the run is closing. Run under -race.
func TestAppendAndCloseDoNotRace(t *testing.T) {
	events, _ := openTestEvents(t)

	var writers sync.WaitGroup
	for writer := range 8 {
		writers.Add(1)
		go func(writer int) {
			defer writers.Done()
			for range 200 {
				events.Append(Event{Call: callID(), Model: "cheap/one", Final: writer%2 == 0})
			}
		}(writer)
	}
	go func() { events.Close() }()
	writers.Wait()
	_ = events.Close()
}

// The ids are handed out of a pool now. Two units of work must still never
// collapse into one row in the analysis.
func TestCallIDsStayUnique(t *testing.T) {
	seen := make(map[string]bool, 20_000)
	for range 20_000 {
		id := callID()
		if len(id) != 2*idBytes {
			t.Fatalf("id %q is not eight bytes of hex", id)
		}
		if seen[id] {
			t.Fatalf("id %q was handed out twice", id)
		}
		seen[id] = true
	}
}

func TestCallIDsAreUniqueAcrossGoroutines(t *testing.T) {
	var ids sync.Map
	var callers sync.WaitGroup
	for range 8 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			for range 2000 {
				id := callID()
				if _, taken := ids.LoadOrStore(id, true); taken {
					t.Errorf("id %q was handed out twice", id)
					return
				}
			}
		}()
	}
	callers.Wait()
}

// A nil log is usable: a run whose diary could not be opened still routes.
func TestANilEventLogIsUsable(t *testing.T) {
	var events *Events
	events.Append(Event{Model: "cheap/one"})
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
}
