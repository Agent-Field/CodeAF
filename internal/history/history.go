// Package history is the input-history store: the lines the person typed, kept
// across sessions so a surface can walk them back with the up arrow.
//
// The file is JSONL at a caller-given path — one line per entry, append-only —
// for the same reason the session journal is: a crash mid-write costs the last
// line and nothing before it, and reading it back is a forward scan with no
// rewrite. A line that does not parse is skipped rather than fatal.
//
// Two properties shape the rest of the package:
//
//   - Capture must never block the input path. Append puts an entry on an
//     in-memory queue and returns; one goroutine drains the queue to disk every
//     flushInterval, so a person typing fast pays a slice append per prompt and
//     the file pays one write per batch. Close drains synchronously.
//   - History is a CONVENIENCE, not an archive. The file is capped (see
//     maxEntries) and a write that fails is dropped in silence — the session
//     JSONL is the record of what happened, this is a recall list. Nothing here
//     is worth interrupting a person mid-turn for.
//
// This package is storage only. It renders nothing, knows no key bindings, and
// imports no surface.
//
// KNOWN LIMITATION: the file is not locked, and a Store caches its contents in
// memory after the first read. Two aforge processes on the same path interleave
// their appends (harmless — every line is independent) but each keeps serving
// recall from the snapshot it loaded, so neither sees the other's new lines
// until it is restarted. That is the right trade for a recall list; the fix,
// if a shared history ever matters, is a stat-and-reload on Recent.
package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// flushInterval is how often the drain goroutine wakes. A tenth of a second
	// is below the threshold where a person could observe the delay and small
	// enough that a kill -9 loses at most one prompt.
	flushInterval = 100 * time.Millisecond

	// maxEntries is where the file is rotated, keepEntries is what survives.
	// Ten thousand prompts is far past the depth any up-arrow walk reaches; the
	// cap exists so a machine running aforge for a year does not carry an
	// unbounded file for a feature whose whole value is in its recent tail.
	maxEntries  = 10000
	keepEntries = 5000
)

// Entry is one captured input: what was typed, where, and when.
type Entry struct {
	Text string    `json:"text"`
	Cwd  string    `json:"cwd"`
	Ts   time.Time `json:"ts"`
}

// Store is an open history file. Use New, and Close it when the surface exits.
//
// Two locks, deliberately. queueMu guards the handoff between the input path
// and the drain goroutine and is never held across I/O, so Append cannot be
// made to wait on a disk write; mu guards the file and the loaded snapshot.
// They are taken in that order (queueMu, released, then mu) and never nested.
type Store struct {
	path string

	queueMu sync.Mutex
	queue   []Entry
	last    string // consecutive-identical dedupe, per instance
	closed  bool

	mu      sync.Mutex
	entries []Entry // file order, oldest first; valid once loaded
	loaded  bool

	stop     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

// New opens a history store at path. Nothing is read or created here: the file
// is loaded on the first Recent (or the first flush), so a surface that never
// recalls and never types pays nothing, and a path under a directory that does
// not exist yet is created on the first write rather than at startup.
func New(path string) *Store {
	store := &Store{
		path:    path,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go store.run()
	return store
}

// Append records one input. It returns immediately — the entry is written by
// the drain goroutine.
//
// Two entries never reach the file: blank text, and a repeat of the line this
// store recorded last. Consecutive-identical dedupe is what makes the up-arrow
// walk useful — a person who runs the same command three times wants one step
// back through it, not three.
func (s *Store) Append(text, cwd string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if s.closed || text == s.last {
		return
	}
	s.last = text
	s.queue = append(s.queue, Entry{Text: text, Cwd: cwd, Ts: time.Now().UTC()})
}

// Recent returns the newest entries first, across every cwd. A limit of zero or
// less returns all of them.
func (s *Store) Recent(limit int) []Entry {
	return s.recent(limit, "", false)
}

// RecentFor is Recent narrowed to one working directory — the recall list for
// the project the person is standing in.
func (s *Store) RecentFor(cwd string, limit int) []Entry {
	return s.recent(limit, cwd, true)
}

func (s *Store) recent(limit int, cwd string, byCwd bool) []Entry {
	// Drain first: a prompt submitted a moment ago is the one most likely to be
	// recalled, and reading past it would be a visible bug for the sake of a
	// write the queue was going to do within 100ms anyway.
	_ = s.flush()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil
	}

	var recent []Entry
	for index := len(s.entries) - 1; index >= 0; index-- {
		entry := s.entries[index]
		if byCwd && entry.Cwd != cwd {
			continue
		}
		recent = append(recent, entry)
		if limit > 0 && len(recent) == limit {
			break
		}
	}
	return recent
}

// Close stops the drain goroutine and writes whatever it was holding. It is
// safe to call twice; Appends after it are dropped rather than queued for a
// goroutine that will never run again.
func (s *Store) Close() error {
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.stopped

	s.queueMu.Lock()
	s.closed = true
	s.queueMu.Unlock()

	return s.flush()
}

// run is the drain goroutine: one per store, one wakeup per flushInterval.
func (s *Store) run() {
	defer close(s.stopped)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = s.flush()
		case <-s.stop:
			return
		}
	}
}

// flushForTest drains the queue synchronously. Tests use it instead of sleeping
// through a tick; nothing outside this package needs it, because Close is the
// only synchronous drain a caller should ever want.
func (s *Store) flushForTest() error { return s.flush() }

// flush moves the queue to disk. An error here is returned but not surfaced by
// the callers that matter — see the package comment on what history is worth.
func (s *Store) flush() error {
	s.queueMu.Lock()
	pending := s.queue
	s.queue = nil
	s.queueMu.Unlock()
	if len(pending) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Load before appending, so the snapshot this store answers recall from
	// includes the file's existing lines and not just this process's.
	if err := s.loadLocked(); err != nil {
		return err
	}
	if err := s.appendLocked(pending); err != nil {
		return err
	}
	s.entries = append(s.entries, pending...)
	if len(s.entries) >= maxEntries {
		return s.rotateLocked()
	}
	return nil
}

// loadLocked reads the file once. A missing file is an empty history, not an
// error: the first prompt ever typed on a machine has nowhere to load from.
func (s *Store) loadLocked() error {
	if s.loaded {
		return nil
	}
	entries, err := readFile(s.path)
	if err != nil {
		return err
	}
	s.entries = entries
	s.loaded = true
	return nil
}

// appendLocked writes one batch. The file is opened per batch rather than held
// open: at one open per flushInterval the cost is nothing, and it means
// rotation is a rename with no descriptor to juggle.
func (s *Store) appendLocked(entries []Entry) error {
	if directory := filepath.Dir(s.path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("history: %w", err)
		}
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, entry := range entries {
		payload, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if _, err := writer.Write(append(payload, '\n')); err != nil {
			return fmt.Errorf("history: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("history: %w", err)
	}
	return nil
}

// rotateLocked rewrites the file as its newest keepEntries lines.
//
// Rewriting through a temp file and a rename is what keeps the append-only
// promise true across the one operation that is not an append: either the old
// file or the whole new one is on disk, never a half-truncated middle.
func (s *Store) rotateLocked() error {
	kept := make([]Entry, keepEntries)
	copy(kept, s.entries[len(s.entries)-keepEntries:])

	temporary, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*")
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	name := temporary.Name()

	writer := bufio.NewWriter(temporary)
	for _, entry := range kept {
		payload, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if _, err := writer.Write(append(payload, '\n')); err != nil {
			temporary.Close()
			os.Remove(name)
			return fmt.Errorf("history: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		temporary.Close()
		os.Remove(name)
		return fmt.Errorf("history: %w", err)
	}
	if err := temporary.Chmod(0o600); err != nil {
		// A tmpfs that refuses chmod is not a reason to lose the rotation.
		_ = err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("history: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		os.Remove(name)
		return fmt.Errorf("history: %w", err)
	}
	s.entries = kept
	return nil
}

// readFile replays the JSONL. A line that does not parse, or that carries no
// text, is skipped: the last line is the one a crash can leave half-written,
// and losing every recall because of it is the wrong trade.
func readFile(path string) ([]Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("history: %w", err)
	}
	defer file.Close()

	var entries []Entry
	scanner := bufio.NewScanner(file)
	// A pasted prompt is routinely larger than the default 64KiB token limit,
	// and hitting it would end the replay at that line rather than skip it.
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Text == "" {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	return entries, nil
}
