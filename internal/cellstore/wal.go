package cellstore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// Intent is a call's promise to run (SCHEMAS.md 6): written and fsynced before
// the call, so a death mid-call leaves it behind.
type Intent struct {
	V          uint16 `json:"V"`
	Tool       string `json:"tool"`
	ArgsHash   string `json:"args_hash"`
	Started    int64  `json:"started"`
	SideEffect string `json:"side_effect"`
}

// key is the join between an intent, its completion and its receipt row.
func (i Intent) key() string { return joinKey(i.ArgsHash, i.Started) }

func joinKey(argsHash string, started int64) string {
	return argsHash + "@" + strconv.FormatInt(started, 10)
}

// MayRerun is the recovery rule stated as data: a local call ran with the
// network denied, so its effects were confined to a snapshot that is being
// discarded, and running it again is allowed. An external call is never rerun
// by the harness (L9). Nothing in this package acts on the answer; it exists
// so a surface can say which of the two an incomplete call is.
func (i Intent) MayRerun() bool { return i.SideEffect == string(sideLocal) }

const sideLocal = executor.EffectLocal

// Recovery is what a reopened WAL found.
type Recovery struct {
	// Incomplete intents have no completion: the call may or may not have run.
	// They are surfaced and left in the log until resolved.
	Incomplete []Intent
	// Completed calls finished but were never sealed; the next seal takes them.
	Completed []Executed
}

// record is one WAL line. V is first (L11).
type record struct {
	V      uint16    `json:"V"`
	Op     string    `json:"op"`
	Intent Intent    `json:"intent"`
	Done   *Executed `json:"done,omitempty"`
}

// WAL is the device-local call-intent log: append-only JSON lines, outside the
// cell, never synced.
type WAL struct {
	mu   sync.Mutex
	path string
}

// OpenWAL opens the log at path and reports what it holds.
func OpenWAL(path string) (*WAL, Recovery, error) {
	w := &WAL{path: path}
	recs, err := w.read()
	if err != nil {
		return nil, Recovery{}, err
	}
	return w, replay(recs), nil
}

// Begin logs the intent to run a call.
func (w *WAL) Begin(i Intent) error { return w.append(record{V: schemaV, Op: opIntent, Intent: i}) }

// Finish logs a call's completion with what it produced.
func (w *WAL) Finish(i Intent, e Executed) error {
	return w.append(record{V: schemaV, Op: opDone, Intent: i, Done: &e})
}

// Resolve closes an incomplete intent once the model or the person has dealt
// with it. It is the only way an incomplete intent leaves the log.
func (w *WAL) Resolve(i Intent) error { return w.append(record{V: schemaV, Op: opResolved, Intent: i}) }

// Sealed drops the records of calls whose turn has sealed, keeping every other
// record: an intent still in flight, a completion the seal did not take.
func (w *WAL) Sealed(calls []Executed) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	recs, err := w.read()
	if err != nil {
		return err
	}
	gone := map[string]bool{}
	for _, e := range calls {
		gone[joinKey(e.Call.ArgsHash, e.Call.Started)] = true
	}
	var keep []record
	for _, r := range recs {
		if !gone[r.Intent.key()] {
			keep = append(keep, r)
		}
	}
	return w.rewrite(keep)
}

const (
	opIntent   = "intent"
	opDone     = "done"
	opResolved = "resolved"
)

func (w *WAL) append(r record) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// read returns every record. A final line torn by a crash is dropped: it was
// never acknowledged, so nothing depended on it.
func (w *WAL) read() ([]record, error) {
	raw, err := os.ReadFile(w.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []record
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(nil, len(raw)+1)
	for sc.Scan() {
		var r record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			recs = append(recs, r)
		}
	}
	return recs, sc.Err()
}

func (w *WAL) rewrite(recs []record) error {
	var buf bytes.Buffer
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf.Write(append(line, '\n'))
	}
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, w.path); err != nil {
		return fmt.Errorf("compact wal: %w", err)
	}
	return nil
}

// replay folds the log into what is open, in order. Each op is one small step
// on the state; an unknown op is ignored so a newer writer's records are safe.
func replay(recs []record) Recovery {
	st := &replayState{open: map[string]Intent{}}
	steps := map[string]func(record){
		opIntent:   func(r record) { st.open[r.Intent.key()] = r.Intent; st.order = append(st.order, r.Intent.key()) },
		opDone:     func(r record) { delete(st.open, r.Intent.key()); st.finish(r) },
		opResolved: func(r record) { delete(st.open, r.Intent.key()) },
	}
	for _, r := range recs {
		if step, ok := steps[r.Op]; ok && r.V <= schemaV {
			step(r)
		}
	}
	return st.result()
}

func (s *replayState) finish(r record) {
	if r.Done != nil {
		s.done = append(s.done, *r.Done)
	}
}

type replayState struct {
	open  map[string]Intent
	order []string
	done  []Executed
}

func (s *replayState) result() Recovery {
	rec := Recovery{Completed: s.done}
	seen := map[string]bool{}
	for _, k := range s.order {
		if i, ok := s.open[k]; ok && !seen[k] {
			seen[k] = true
			rec.Incomplete = append(rec.Incomplete, i)
		}
	}
	return rec
}
