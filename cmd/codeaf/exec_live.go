package main

// ── codeaf exec, WATCHED AND STEERED WHILE IT RUNS ──────────────────────────
//
// A harness that runs `codeaf exec --json` gets one object at the end and
// nothing before it, so a person watching the run sees a spinner for ten
// minutes and has no way to say "also add a doc comment" while it works. Two
// flags close that, and both are FILES rather than a second protocol on stdin,
// because stdin already carries the prompt and a harness on any platform can
// append to a file and tail one:
//
//   - --events PATH: every transcript entry the run's one leaf records — the
//     model's words, each tool call and its result, the harness's own notes —
//     is appended to PATH as one JSON line the moment it is recorded. It is the
//     same record the store's transcript keeps (exec.TranscriptSink), so a
//     watcher reads exactly what an autopsy would, not a second account.
//   - --steer PATH: every complete line appended to PATH while the run works is
//     one note from a person, read at the run's next turn boundary through the
//     executor's own steering mailbox (exec.Task.Steer) — the same path the
//     chat surface's redirect takes, so a note is shown to the model as the
//     user's guidance and the run's transcript says "steered: …" when it lands.
//     A line is either plain text or {"text": "…"}.
//
// NEITHER FLAG CHANGES WHAT THE RUN DOES WHEN IT IS NOT GIVEN. No file is
// opened, no mailbox is armed, and the envelope is the same object.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/store"
)

// execEvent is one line of --events.
type execEvent struct {
	At     string `json:"at"`
	Kind   string `json:"kind"`
	Turn   int    `json:"turn,omitempty"`
	Tool   string `json:"tool,omitempty"`
	CallID string `json:"call_id,omitempty"`
	Text   string `json:"text,omitempty"`
	Millis int64  `json:"ms,omitempty"`
	Failed bool   `json:"failed,omitempty"`
}

// eventFile is the --events sink. Every line is written whole and at once
// (one O_APPEND write per entry), so a tail never reads half an entry.
type eventFile struct {
	mu   sync.Mutex
	file *os.File
	next exec.TranscriptSink
}

// openEventFile opens path for appending; next is a sink already armed on the
// context, which still receives every entry.
func openEventFile(path string, next exec.TranscriptSink) (*eventFile, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &eventFile{file: file, next: next}, nil
}

func (e *eventFile) Record(entry store.TranscriptEntry) {
	if e.next != nil {
		e.next.Record(entry)
	}
	e.write(execEvent{Kind: string(entry.Kind), Turn: entry.Turn, Tool: entry.Tool, CallID: entry.CallID, Text: entry.Text, Millis: entry.Millis, Failed: entry.Failed})
}

// Status writes a line the harness speaks itself: the run began, the run ended.
func (e *eventFile) Status(text string) { e.write(execEvent{Kind: "status", Text: text}) }

func (e *eventFile) write(ev execEvent) {
	ev.At = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(ev)
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.file != nil {
		_, _ = e.file.Write(raw)
	}
}

func (e *eventFile) Flush() {
	if e.next != nil {
		e.next.Flush()
	}
}

func (e *eventFile) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.file != nil {
		_ = e.file.Close()
		e.file = nil
	}
}

// steerFile reads the notes appended to one file since the last read. It is
// polled by the executor at each turn boundary, so it never blocks and holds
// no goroutine: a read that finds nothing new returns nothing.
type steerFile struct {
	path    string
	mu      sync.Mutex
	offset  int64
	partial string
}

func newSteerFile(path string) *steerFile { return &steerFile{path: path} }

// Read is exec.Task.Steer.
func (s *steerFile) Read() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.Open(s.path)
	if err != nil {
		return nil
	}
	defer file.Close()
	if _, err := file.Seek(s.offset, io.SeekStart); err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil || len(raw) == 0 {
		return nil
	}
	s.offset += int64(len(raw))
	text := s.partial + string(raw)
	// Only complete lines are notes; a writer caught mid-line is finished on
	// the next read rather than delivered as half a sentence.
	cut := strings.LastIndexByte(text, '\n')
	if cut < 0 {
		s.partial = text
		return nil
	}
	s.partial = text[cut+1:]
	var notes []string
	scanner := bufio.NewScanner(strings.NewReader(text[:cut]))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		if note := steerLine(scanner.Text()); note != "" {
			notes = append(notes, note)
		}
	}
	return notes
}

// steerLine is one line's note: {"text": "…"} or the line itself.
func steerLine(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "{") {
		var obj struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(line), &obj) == nil {
			return strings.TrimSpace(obj.Text)
		}
	}
	return line
}
