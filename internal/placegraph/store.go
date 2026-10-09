package placegraph

// The persistence engine: one JSON document, one lock, one atomic replace.
//
// EVERY OPERATION READS THE FILE FRESH UNDER AN EXCLUSIVE LOCK. A Store keeps no
// copy of the graph between calls, so two windows (two processes) that share the
// path can never act on a stale picture; the cost is one small file read, which
// at the design's scale (a few hundred places) is far below the cost of a screen
// repaint. Reads take the exclusive lock too: a read is also where a damaged file
// is noticed and set aside, and that is a write.

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// Options configures a Store. Only Path is required.
type Options struct {
	// Path is the graph file, chosen by the caller (the desktop bridge puts it in
	// the codeaf home). The directory is created if missing; "<Path>.lock" is the
	// lock file beside it.
	Path string
	// Now is the clock. Tests inject a deterministic one.
	Now func() time.Time
	// NewID mints ids for places ("pl_"), sources ("src_") and receipts ("rc_").
	// The default is 64 bits of crypto/rand in hex. Ids are never derived from a
	// name or reused, so renaming or deleting never changes another place's id.
	NewID func(prefix string) string
	// LockTimeout bounds the wait for another process's lock. Default 10s.
	LockTimeout time.Duration
}

// Store is the handle. It is safe for concurrent use and for use by several
// processes at once on the same Path.
type Store struct {
	opts Options
	mu   sync.Mutex // the cheap first gate for this process's own goroutines

	receipts []*receiptEntry // oldest first, at most MaxUndo
	recovery *Recovery

	// beforeRename is a test seam: it runs after the temp file is complete and
	// before it replaces the real one, so a test can prove a failure at that
	// instant leaves the previous document intact.
	beforeRename func() error
}

type receiptEntry struct {
	Receipt
	before *State
}

const lockPoll = 10 * time.Millisecond

// Open prepares the store and performs the load-time recovery described on
// Recovery, so a damaged file is dealt with once, at start, and reported.
func Open(opts Options) (*Store, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("%w: empty path", ErrInvalid)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NewID == nil {
		opts.NewID = randomID
	}
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = 10 * time.Second
	}
	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{opts: opts}
	release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	if _, err := s.loadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// LastRecovery returns what the most recent load in this process had to do about
// a damaged file, or nil. A caller that wants to tell the person ("your places
// file was damaged; the old copy is at …") reads it once after Open.
func (s *Store) LastRecovery() *Recovery {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recovery == nil {
		return nil
	}
	r := *s.recovery
	r.Repairs = append([]string(nil), r.Repairs...)
	return &r
}

// Snapshot returns an immutable copy of the graph with its query methods.
func (s *Store) Snapshot() (*Snapshot, error) {
	release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	st, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	return newSnapshot(st), nil
}

// Revision returns the current structural revision.
func (s *Store) Revision() (uint64, error) {
	snap, err := s.Snapshot()
	if err != nil {
		return 0, err
	}
	return snap.Revision, nil
}

// Receipts lists the undoable commits made through THIS Store, oldest first, at
// most MaxUndo. Receipts are not persisted: they are the undo stack of a running
// window, as the design has it ("for each window"), and a restart starts empty.
func (s *Store) Receipts() []Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Receipt, len(s.receipts))
	for i, e := range s.receipts {
		out[i] = e.Receipt
	}
	return out
}

// Undo restores the graph to the state before the commit the receipt names and
// returns the new revision. It refuses with ErrRevisionConflict unless the graph
// is exactly as that commit left it: any commit since, by this window or another
// process, makes the undo ambiguous, and a silent partial undo is worse than a
// refusal. Undo is itself a commit (the revision rises) but is not undoable; there
// is no redo. Place visit times (LastOpenedAt) are carried over, not rolled back.
func (s *Store) Undo(receiptID string) (uint64, error) {
	release, err := s.acquire()
	if err != nil {
		return 0, err
	}
	defer release()
	at := -1
	for i, e := range s.receipts {
		if e.ID == receiptID {
			at = i
		}
	}
	if at < 0 {
		return 0, ErrNoReceipt
	}
	e := s.receipts[at]
	cur, err := s.loadLocked()
	if err != nil {
		return 0, err
	}
	if cur.Revision != e.AfterRevision {
		return 0, ErrRevisionConflict
	}
	restored := e.before.clone()
	opened := map[string]time.Time{}
	for _, p := range cur.Places {
		opened[p.ID] = p.LastOpenedAt
	}
	for i := range restored.Places {
		if t, ok := opened[restored.Places[i].ID]; ok {
			restored.Places[i].LastOpenedAt = t
		}
	}
	restored.Revision = cur.Revision + 1
	if _, err := validateState(restored, false); err != nil {
		return 0, err
	}
	if err := s.writeLocked(restored); err != nil {
		return 0, err
	}
	s.receipts = s.receipts[:at]
	// The commit before this one now ends at the revision we just wrote, if it was
	// contiguous with the one we took back; that is what lets ⌘Z be pressed twice.
	if n := len(s.receipts); n > 0 && s.receipts[n-1].AfterRevision == e.BeforeRevision {
		s.receipts[n-1].AfterRevision = restored.Revision
	}
	return restored.Revision, nil
}

// change is what a mutator reports; nil means "nothing changed".
type change struct {
	action  Action
	subject string
}

// mutate is the one write path: lock, read fresh, apply on a copy, validate the
// whole result, bump the revision, replace the file, remember how to undo it.
func (s *Store) mutate(fn func(st *State, now time.Time) (*change, error)) (Receipt, error) {
	release, err := s.acquire()
	if err != nil {
		return Receipt{}, err
	}
	defer release()
	cur, err := s.loadLocked()
	if err != nil {
		return Receipt{}, err
	}
	now := s.opts.Now().UTC()
	work := cur.clone()
	ch, err := fn(work, now)
	if err != nil {
		return Receipt{}, err
	}
	if ch == nil {
		return Receipt{}, nil
	}
	work.Revision = cur.Revision + 1
	if _, err := validateState(work, false); err != nil {
		return Receipt{}, err
	}
	if err := s.writeLocked(work); err != nil {
		return Receipt{}, err
	}
	rc := Receipt{
		ID:             s.opts.NewID("rc_"),
		Action:         ch.action,
		Subject:        ch.subject,
		BeforeRevision: cur.Revision,
		AfterRevision:  work.Revision,
		At:             now,
	}
	s.receipts = append(s.receipts, &receiptEntry{Receipt: rc, before: cur})
	// Provenance only; inability to save it removes the optional note action.
	s.recordContextReceipt(rc)
	if len(s.receipts) > MaxUndo {
		s.receipts = append([]*receiptEntry(nil), s.receipts[len(s.receipts)-MaxUndo:]...)
	}
	return rc, nil
}

// acquire takes the in-process gate and then the file lock, polling so a wedged
// holder yields ErrLocked instead of parking this goroutine forever. Unlike a git
// lock, going ahead unserialized is NOT acceptable here: it would lose updates.
func (s *Store) acquire() (func(), error) {
	s.mu.Lock()
	f, err := os.OpenFile(s.opts.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	deadline := time.Now().Add(s.opts.LockTimeout)
	for {
		err := filelock.Lock(f, true, true)
		if err == nil {
			break
		}
		if !filelock.IsBusy(err) {
			f.Close()
			s.mu.Unlock()
			return nil, err
		}
		if time.Now().After(deadline) {
			f.Close()
			s.mu.Unlock()
			return nil, ErrLocked
		}
		time.Sleep(lockPoll)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
		s.mu.Unlock()
	}, nil
}

// loadLocked reads, parses and validates the file, recovering what it honestly
// can. It must be called with the lock held because recovery writes.
func (s *Store) loadLocked() (*State, error) {
	data, err := readCapped(s.opts.Path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Version: SchemaVersion, Places: []Place{}, Memberships: []Membership{}, Pinned: []string{}}, nil
	}
	if err != nil && !errors.Is(err, errOversize) {
		return nil, err
	}
	var st State
	reason := ""
	switch {
	case errors.Is(err, errOversize):
		reason = fmt.Sprintf("file is larger than %d bytes", MaxFileBytes)
	default:
		dec := json.NewDecoder(bytes.NewReader(data))
		if err := dec.Decode(&st); err != nil {
			reason = "not valid JSON: " + err.Error()
		} else if _, err := dec.Token(); err != io.EOF {
			reason = "unexpected data after the document"
		}
	}
	if reason == "" && st.Version > SchemaVersion {
		return nil, fmt.Errorf("%w (file version %d, this build %d)", ErrUnsupportedVersion, st.Version, SchemaVersion)
	}
	var repairs []string
	if reason == "" {
		repairs, err = validateState(&st, true)
		if err != nil {
			reason = err.Error()
		}
	}
	if reason != "" {
		return s.quarantineLocked(reason)
	}
	if len(repairs) == 0 {
		return &st, nil
	}
	// Repaired: keep a copy of the original beside it and write the clean one.
	now := s.opts.Now().UTC()
	copyTo := s.sidecarName(now)
	if err := os.WriteFile(copyTo, data, 0o600); err != nil {
		return nil, err
	}
	st.Revision++
	if err := s.writeLocked(&st); err != nil {
		return nil, err
	}
	s.recovery = &Recovery{Kind: "repaired", Reason: "dangling references dropped", MovedTo: copyTo, Repairs: repairs, At: now}
	return &st, nil
}

// quarantineLocked moves the unusable file aside, never deleting it, and starts
// an empty graph. If the move itself fails the error is returned and nothing is
// overwritten.
func (s *Store) quarantineLocked(reason string) (*State, error) {
	now := s.opts.Now().UTC()
	to := s.sidecarName(now)
	if err := os.Rename(s.opts.Path, to); err != nil {
		return nil, fmt.Errorf("placegraph: cannot set aside damaged file (%s): %w", reason, err)
	}
	st := &State{Version: SchemaVersion, Revision: 1, Places: []Place{}, Memberships: []Membership{}, Pinned: []string{}}
	if err := s.writeLocked(st); err != nil {
		return nil, err
	}
	s.recovery = &Recovery{Kind: "quarantined", Reason: reason, MovedTo: to, At: now}
	return st, nil
}

func (s *Store) sidecarName(now time.Time) string {
	return fmt.Sprintf("%s.corrupt-%s-%s", s.opts.Path, now.Format("20060102T150405.000000000Z"), s.opts.NewID(""))
}

var errOversize = errors.New("oversize")

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, errOversize
	}
	return data, nil
}

// writeLocked replaces the file atomically: a complete temp file in the same
// directory, fsynced, then renamed over the target, then the directory fsynced.
// A crash at any point leaves either the whole old document or the whole new one.
func (s *Store) writeLocked(st *State) error {
	data, err := json.MarshalIndent(st, "", " ")
	if err != nil {
		return err
	}
	if len(data) > MaxFileBytes {
		return ErrTooLarge
	}
	dir := filepath.Dir(s.opts.Path)
	tmp, err := os.CreateTemp(dir, ".places-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if s.beforeRename != nil {
		if err := s.beforeRename(); err != nil {
			_ = os.Remove(name)
			return err
		}
	}
	if err := os.Rename(name, s.opts.Path); err != nil {
		_ = os.Remove(name)
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func randomID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failing means the platform is broken
	}
	return prefix + hex.EncodeToString(b[:])
}
