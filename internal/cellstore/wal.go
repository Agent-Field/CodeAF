package cellstore

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
)

// Intent is a call's promise to run (SCHEMAS.md 6): written before the call,
// so process death leaves it behind. C28 groups power-loss durability separately.
type Intent struct {
	V uint16 `json:"V"`
	// ID distinguishes identical calls started in the same millisecond; absent on older records.
	ID         string `json:"id,omitempty"`
	Tool       string `json:"tool"`
	ArgsHash   string `json:"args_hash"`
	Started    int64  `json:"started"`
	SideEffect string `json:"side_effect"`
	// Brief is the call's arguments in a few words, for a person to recognise it
	// by; the hash cannot be read back. Version 2.
	Brief string `json:"brief,omitempty"`
	// Groups are the process groups the call started, filled in by replay from
	// the records that named them. Version 2.
	Groups []GroupRec `json:"-"`
}

// key is the join between an intent, its completion and its receipt row.
func (i Intent) key() string { return callKey(i.ID, i.ArgsHash, i.Started) }

func callKey(id, argsHash string, started int64) string {
	if id != "" {
		return "id:" + id
	}
	return joinKey(argsHash, started)
}

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
	Models    []ModelCall
}

// walV is the version of the lines this log writes. Version 2 added the intent's
// brief and the group record; a version 1 line reads as one with neither.
const walV = 2
const modelWALVersion = 3

// record is one WAL line. V is first (L11).
type record struct {
	V      uint16     `json:"V"`
	Op     string     `json:"op"`
	Intent Intent     `json:"intent"`
	Done   *Executed  `json:"done,omitempty"`
	Group  *GroupRec  `json:"group,omitempty"`
	Model  *ModelCall `json:"model,omitempty"`
}

// MarshalJSON keeps model lines in their V3 shape during compaction too.
func (r record) MarshalJSON() ([]byte, error) {
	if r.Model != nil {
		return json.Marshal(struct {
			V     uint16     `json:"V"`
			Op    string     `json:"op"`
			Model *ModelCall `json:"model"`
		}{r.V, r.Op, r.Model})
	}
	type wire record
	return json.Marshal(wire(r))
}

// WAL is the device-local call-intent log: append-only JSON lines, outside the
// cell. Model receipts are synced before their handoff returns.
type WAL struct {
	mu    sync.Mutex
	path  string
	owner string
}

// OpenWAL opens the log at path and reports what it holds.
func OpenWAL(path string) (*WAL, Recovery, error) {
	w := &WAL{path: path, owner: path}
	if err := repairLogTail(path); err != nil {
		return nil, Recovery{}, err
	}
	recs, err := w.read()
	if err != nil {
		return nil, Recovery{}, err
	}
	return w, replay(recs), nil
}

// Begin logs the intent to run a call.
func (w *WAL) Begin(i Intent) error { return w.append(record{V: walV, Op: opIntent, Intent: i}) }

// Started logs a process group the call has started, so a reopen can end it if
// the session that owned it is gone.
func (w *WAL) Started(i Intent, g GroupRec) error {
	return w.append(record{V: walV, Op: opGroup, Intent: i, Group: &g})
}

// Finish logs a call's completion with what it produced.
func (w *WAL) Finish(i Intent, e Executed) error {
	return w.append(record{V: walV, Op: opDone, Intent: i, Done: &e})
}

// Resolve closes an incomplete intent once the model or the person has dealt
// with it. It is the only way an incomplete intent leaves the log.
func (w *WAL) Resolve(i Intent) error { return w.append(record{V: walV, Op: opResolved, Intent: i}) }

// Sealed drops the records of calls whose turn has sealed, keeping every other
// record: an intent still in flight, a completion the seal did not take.
func (w *WAL) Sealed(calls []Executed, models ...ModelCall) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	recs, err := w.read()
	if err != nil {
		return err
	}
	gone := map[string]bool{}
	for _, e := range calls {
		gone[callKey(e.Call.ID, e.Call.ArgsHash, e.Call.Started)] = true
	}
	modelGone := map[string]bool{}
	anonymousGone := map[ModelCall]int{}
	for _, m := range models {
		if m.ID != "" {
			modelGone[m.ID] = true
		} else {
			anonymousGone[m]++
		}
	}
	var keep []record
	for _, r := range recs {
		if r.Model != nil {
			if r.Model.ID != "" && modelGone[r.Model.ID] {
				continue
			}
			if r.Model.ID == "" && anonymousGone[*r.Model] > 0 {
				anonymousGone[*r.Model]--
				continue
			}
		}
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
	opGroup    = "group"
	opModel    = "model"
)

func (w *WAL) append(r record) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.appendLocked(r, line)
}

func (w *WAL) appendLocked(r record, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return err
	}
	_, before := os.Stat(w.path)
	created := errors.Is(before, fs.ErrNotExist)
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if r.Model != nil {
		err = f.Sync()
		if err == nil {
			dir, e := os.Open(filepath.Dir(w.path))
			if e != nil {
				err = e
			} else {
				err = dir.Sync()
				dir.Close()
			}
		}
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	commits.mark(w.owner, w.path, created)
	return err
}

// Model logs a receipt before a model-only call can leave recorder memory.
func (w *WAL) Model(m ModelCall) error {
	if m.ID == "" {
		return w.append(record{V: modelWALVersion, Op: opModel, Model: &m})
	}
	return w.recordModels([]ModelCall{m})
}

// Legacy callers have no shared identity. Count equal anonymous occurrences so
// retrying a pending batch does not merge two calls or give them invented IDs.
func (w *WAL) recordModels(models []ModelCall) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := repairLogTail(w.path); err != nil {
		return err
	}
	recs, err := w.read()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	anonymous := map[ModelCall]int{}
	for _, r := range recs {
		if r.Model != nil {
			if r.Model.ID != "" {
				seen[r.Model.ID] = true
			} else {
				anonymous[*r.Model]++
			}
		}
	}
	wanted := map[ModelCall]int{}
	for _, m := range models {
		if m.ID != "" {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
		} else {
			wanted[m]++
			if wanted[m] <= anonymous[m] {
				continue
			}
		}
		r := record{V: modelWALVersion, Op: opModel, Model: &m}
		line, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if err = w.appendLocked(r, line); err != nil {
			return err
		}
	}
	// Existing bytes may be from an earlier failed sync, rather than a durable handoff.
	if len(models) > 0 {
		return syncModelWAL(w.path)
	}
	return nil
}

// A retry must sync even an existing line because the previous sync may have failed.
func syncModelWAL(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
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
	// A transcript-only seal may precede the first call log. The shared engine
	// store no longer creates this private folder as a side effect (D1).
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return err
	}
	if len(recs) == 0 {
		// The sealed turn and its receipt are already durable. With no older
		// unresolved call to retain, truncation needs no replacement-file sync
		// on the next command's path (C28/E1). Recovery never executes calls.
		_, before := os.Stat(w.path)
		f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		err = f.Close()
		commits.mark(w.owner, w.path, errors.Is(before, fs.ErrNotExist))
		return err
	}
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	// The replacement carries old unresolved calls too. It must reach disk
	// before rename, or power loss could erase records older than C28's window.
	commits.mark(w.owner, tmp, false)
	ctx, cancel := context.WithTimeout(context.Background(), SealWithin)
	defer cancel()
	if err := commits.flush(ctx, w.owner); err != nil {
		return err
	}
	if err := fsreplace.Rename(tmp, w.path); err != nil {
		return fmt.Errorf("compact wal: %w", err)
	}
	commits.mark(w.owner, w.path, true)
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
		opGroup:    st.group,
	}
	models := map[string]bool{}
	for _, r := range recs {
		if r.V == modelWALVersion && r.Op == opModel && r.Model != nil && (r.Model.ID == "" || !models[r.Model.ID]) {
			st.models = append(st.models, *r.Model)
			models[r.Model.ID] = true
		}
		if step, ok := steps[r.Op]; ok && r.V <= walV {
			step(r)
		}
	}
	return st.result()
}

// group attaches a started process group to the intent it belongs to. A group
// for a call that is no longer open is dropped: nothing will ask about it.
func (s *replayState) group(r record) {
	in, ok := s.open[r.Intent.key()]
	if !ok || r.Group == nil {
		return
	}
	in.Groups = append(in.Groups, *r.Group)
	s.open[r.Intent.key()] = in
}

func (s *replayState) finish(r record) {
	if r.Done != nil {
		s.done = append(s.done, *r.Done)
	}
}

type replayState struct {
	open   map[string]Intent
	order  []string
	done   []Executed
	models []ModelCall
}

func (s *replayState) result() Recovery {
	rec := Recovery{Completed: s.done, Models: s.models}
	seen := map[string]bool{}
	for _, k := range s.order {
		if i, ok := s.open[k]; ok && !seen[k] {
			seen[k] = true
			rec.Incomplete = append(rec.Incomplete, i)
		}
	}
	return rec
}

// repairLogTail discards only the unacknowledged tail after power loss (C28).
// Appending to a torn line would hide the next complete call on the next open.
func repairLogTail(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	end := bytes.LastIndexByte(raw, '\n') + 1
	for end > 0 {
		start := bytes.LastIndexByte(raw[:end-1], '\n') + 1
		if json.Valid(raw[start : end-1]) {
			break
		}
		end = start
	}
	if end == len(raw) {
		return nil
	}
	return os.Truncate(path, int64(end))
}
