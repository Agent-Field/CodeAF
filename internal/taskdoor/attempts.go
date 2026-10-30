package taskdoor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// MaxRunning is shared by admission and the sentence describing its limit.
const MaxRunning = 2

// Executor owns a run until it finishes or Serve's context ends. Ending that
// context must leave a surviving child's running state available for Adopt.
// Neither a lost watch nor an origin disconnect cancels a run.
type Executor interface {
	Run(context.Context, *Attempt, Journal) error
	Stop(attemptID string) error
	Adopt(context.Context, *Attempt, Journal) error
}

// Journal saves a step, spend, phase or finish before waking any watchers.
// Executors do not choose sequence numbers; one journal orders all events.
type Journal interface{ Append(event any) error }

type State struct {
	Phase      string    `json:"phase"`
	PID        int       `json:"pid,omitempty"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
}

// Attempt is an admitted immutable request and its private working folder.
// Child metadata goes through UpdateState so adoption cannot see half a write.
type Attempt struct {
	ID      string
	Dir     string
	Request Start
	mu      sync.Mutex
	state   State
	changed chan struct{}
	removed bool
	clock   Clock
}

func (a *Attempt) State() State { a.mu.Lock(); defer a.mu.Unlock(); return a.state }
func (a *Attempt) UpdateState(update func(*State)) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.removed {
		return os.ErrNotExist
	}
	next := a.state
	update(&next)
	if err := atomicJSON(filepath.Join(a.Dir, "state.json"), next); err != nil {
		return err
	}
	a.state = next
	return nil
}
func (a *Attempt) notify() { close(a.changed); a.changed = make(chan struct{}) }

type attemptStore struct {
	root     string
	clock    Clock
	mu       sync.Mutex
	attempts map[string]*Attempt
}

func validAttempt(id string) bool {
	if len(id) != len("att_")+16 || id[:4] != "att_" {
		return false
	}
	_, err := hex.DecodeString(id[4:])
	return err == nil
}

func openAttempts(root string, clock Clock) (*attemptStore, error) {
	if root == "" {
		return nil, errors.New("taskdoor: an attempt store folder is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	store := &attemptStore{root: root, clock: clock, attempts: map[string]*Attempt{}}
	for _, entry := range entries {
		if !validAttempt(entry.Name()) {
			continue
		}
		if !entry.IsDir() {
			return nil, fmt.Errorf("taskdoor: attempt %s is not a folder", entry.Name())
		}
		a := &Attempt{ID: entry.Name(), Dir: filepath.Join(root, entry.Name()), changed: make(chan struct{}), clock: clock}
		if err := readJSON(filepath.Join(a.Dir, "request.json"), &a.Request); err != nil {
			return nil, err
		}
		if err := readJSON(filepath.Join(a.Dir, "state.json"), &a.state); err != nil {
			return nil, err
		}
		events, err := readEvents(a.Dir)
		if err != nil {
			return nil, err
		}
		// A crash after the journal's rename but before its derived state write
		// must not hand a completed child to Adopt or admit another execution.
		if len(events) > 0 && events[len(events)-1].Type == "finished" {
			var end Finished
			if err := events[len(events)-1].Decode(&end); err != nil {
				return nil, err
			}
			if err := a.saveResult(end); err != nil {
				return nil, err
			}
		}
		store.attempts[a.ID] = a
	}
	return store, nil
}
func (s *attemptStore) running() []*Attempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Attempt
	for _, a := range s.attempts {
		if a.State().Phase == "running" {
			out = append(out, a)
		}
	}
	return out
}
func (s *attemptStore) get(id string) *Attempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.attempts[id]
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.removed {
		return nil
	}
	return a
}

// admit publishes the complete admission directory before returning permission
// to call Run. A duplicate wins over the concurrency check, even when busy.
func (s *attemptStore) admit(id string, request Start, busy string) (*Attempt, bool, error) {
	if !validAttempt(id) {
		return nil, false, errors.New("that task attempt id is not readable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.attempts[id]; a != nil {
		a.mu.Lock()
		removed := a.removed
		a.mu.Unlock()
		if removed {
			return nil, false, errors.New("that task is still stopping")
		}
		return a, false, nil
	}
	running := 0
	for _, a := range s.attempts {
		if a.State().Phase == "running" {
			running++
		}
	}
	if running >= MaxRunning {
		return nil, false, errors.New(busy)
	}
	a := &Attempt{ID: id, Dir: filepath.Join(s.root, id), Request: request, state: State{Phase: "running", StartedAt: s.clock.Now()}, changed: make(chan struct{}), clock: s.clock}
	staging, err := os.MkdirTemp(s.root, ".admit-*")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(staging)
	if err := atomicJSON(filepath.Join(staging, "request.json"), request); err != nil {
		return nil, false, err
	}
	if err := atomicJSON(filepath.Join(staging, "state.json"), a.state); err != nil {
		return nil, false, err
	}
	if err := atomicWrite(filepath.Join(staging, "events.jsonl"), nil); err != nil {
		return nil, false, err
	}
	if err := os.Rename(staging, a.Dir); err != nil {
		return nil, false, err
	}
	s.attempts[id] = a
	return a, true, nil
}

// forget does not let a late executor recreate an acknowledged directory.
// A still-stopping executor retains its admission slot until it returns.
func (s *attemptStore) forget(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.attempts[id]
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.RemoveAll(a.Dir); err != nil {
		return err
	}
	a.removed = true
	a.notify()
	if a.state.Phase != "running" {
		delete(s.attempts, id)
	}
	return nil
}
func (s *attemptStore) released(a *Attempt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.removed {
		delete(s.attempts, a.ID)
	}
}

type attemptJournal struct{ a *Attempt }

func (j attemptJournal) Append(event any) error {
	a := j.a
	kind := payloadType(event)
	switch kind {
	case "step", "spend", "phase", "finished":
	default:
		return errors.New("taskdoor: only task events belong in a journal")
	}
	if end, ok := event.(Finished); ok {
		switch end.Outcome {
		case "done", "stopped", "incomplete", "interrupted":
		default:
			return errors.New("taskdoor: unknown task outcome")
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.removed {
		return os.ErrNotExist
	}
	if a.state.Phase != "running" {
		return errors.New("taskdoor: that task has already finished")
	}
	events, err := readEvents(a.Dir)
	if err != nil {
		return err
	}
	if len(events) > 0 && events[len(events)-1].Type == "finished" {
		return errors.New("taskdoor: that task has already finished")
	}
	frame, err := makeFrame(a.ID, uint64(len(events)+1), event)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(a.Dir, "events.jsonl"))
	if err != nil {
		return err
	}
	raw = append(raw, frame.Payload...)
	raw = append(raw, '\n')
	// Replacing the JSONL file, rather than appending in place, makes a power
	// loss leave either the previous journal or the whole new event.
	if err := atomicWrite(filepath.Join(a.Dir, "events.jsonl"), raw); err != nil {
		return err
	}
	if end, ok := event.(Finished); ok {
		err = a.saveResult(end)
	}
	// The event is committed even if a derived result write failed. Watchers
	// still receive it, and reopen repairs result.json from the journal.
	a.notify()
	return err
}
func (a *Attempt) saveResult(end Finished) error {
	if err := atomicJSON(filepath.Join(a.Dir, "result.json"), end); err != nil {
		return err
	}
	next := a.state
	next.Phase = end.Outcome
	if next.FinishedAt.IsZero() {
		next.FinishedAt = a.clock.Now()
	}
	if err := atomicJSON(filepath.Join(a.Dir, "state.json"), next); err != nil {
		return err
	}
	a.state = next
	return nil
}

// after takes the replay and the next wakeup under the same lock, so an event
// written as a watch changes from replay to live can neither vanish nor repeat.
func (a *Attempt) after(from uint64) ([]Frame, <-chan struct{}, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.removed {
		return nil, nil, true, os.ErrNotExist
	}
	events, err := readEvents(a.Dir)
	if err != nil {
		return nil, nil, false, err
	}
	finished := len(events) > 0 && events[len(events)-1].Type == "finished"
	first := len(events)
	for i, event := range events {
		if event.Seq > from {
			first = i
			break
		}
	}
	return events[first:], a.changed, finished, nil
}
func readEvents(dir string) ([]Frame, error) {
	file, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), MaxFrameBytes+1)
	var events []Frame
	for scanner.Scan() {
		raw := bytes.Clone(scanner.Bytes())
		if len(raw) > MaxFrameBytes {
			return nil, frameTooLarge()
		}
		event, err := parseFrame(raw)
		if err != nil {
			return nil, err
		}
		if event.Seq != uint64(len(events)+1) {
			return nil, errors.New("taskdoor: the saved task events are out of order")
		}
		if len(events) > 0 && events[len(events)-1].Type == "finished" {
			return nil, errors.New("taskdoor: saved events follow the task's finish")
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}
func readJSON(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}
func atomicJSON(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return atomicWrite(path, raw)
}
func atomicWrite(path string, raw []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return fsreplace.Rename(file.Name(), path)
}
