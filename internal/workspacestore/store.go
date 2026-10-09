// Package workspacestore keeps the desktop's tab sets: one small document per
// window place ("now", or a place-graph id such as "pl_0123456789abcdef"), so
// two windows on the same place show the same tabs, groups, pins and splits,
// live (design Places 6d "Same place, two windows").
//
// It is a compare-and-swap register, not a merge engine. Every write names the
// revision it was made from, and a write made from anything but the current
// revision is REFUSED with the current document, so the writer can replay its
// own intent over it. Nothing here ever chooses between two windows' edits; the
// renderer's controller does, by re-running the actions a person took
// (desktop/src/features/workspace-sync). That is the whole point: a refused write
// is visible to its author, never a silently lost update.
//
// The store owns no path policy. Its directory is injected, the key is checked
// against a closed pattern before it becomes a file name, and nothing a client
// sends can name a file. A read takes no lock and creates nothing, so a GET is
// free of side effects; only a write takes the per-key file lock
// (internal/filelock), re-reads, checks the revision, and replaces the file by
// temp file, fsync and rename.
//
// A move between two places is the one write that needs two files; pair.go
// publishes it through a journal and takes the global pair lock that every Put
// takes too.
package workspacestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// SchemaVersion is the document schema this build reads and writes. A file from
// a newer build is refused and left alone, never rewritten.
const SchemaVersion = 1

// The ceilings below refuse runaway input; they are not budgets a person meets.
// A tab set at design scale (a few dozen tabs, a few drafts) is a few KiB.
// CHANGING ONE CHANGES desktop/docs/WORKSPACE-SYNC.md IN THE SAME COMMIT, and
// the renderer's mirror in desktop/src/features/workspace-sync/limits.ts.
const (
	// MaxDocumentBytes caps one tab set as the client sends it. It matches the
	// 256 KB the architecture names for a workspace (PLACES-ARCHITECTURE §3.4).
	MaxDocumentBytes = 256 << 10
	// maxFileBytes is the document plus its envelope on disk.
	maxFileBytes = MaxDocumentBytes + 4<<10
	// MaxTabs is how many open tabs one place may hold.
	MaxTabs = 400
	// MaxClosed is how many closed tabs Reopen keeps (the renderer keeps 20).
	MaxClosed = 50
	// MaxGroups is how many groups one strip may hold.
	MaxGroups = 100
	// MaxPanes is how many panes one split tab holds (tabs/helpers.ts splitCapacity).
	MaxPanes = 4
	// MaxIDBytes caps a tab, pane or group id.
	MaxIDBytes = 128
	// MaxTitleBytes caps a tab or group title.
	MaxTitleBytes = 4 << 10
	// MaxWriterBytes caps the writer tag a window sends with its write.
	MaxWriterBytes = 64
)

// keyPattern is every key the store accepts: the Now strip, or a place-graph
// id exactly as internal/placegraph mints them. Nothing else becomes a file.
var keyPattern = regexp.MustCompile(`^(now|pl_[0-9a-f]{16})$`)

// writerPattern is the tag a window puts on its writes so it can recognise its
// own echo. It is an opaque token, never shown to a person.
var writerPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var (
	// ErrInvalidKey is a key outside the closed pattern.
	ErrInvalidKey = errors.New("workspacestore: unknown workspace key")
	// ErrInvalid is a document or writer that does not validate.
	ErrInvalid = errors.New("workspacestore: invalid workspace")
	// ErrTooLarge is a document over MaxDocumentBytes.
	ErrTooLarge = errors.New("workspacestore: workspace too large")
	// ErrLocked is a write that could not take the file lock in time.
	ErrLocked = errors.New("workspacestore: workspace is busy")
	// ErrUnsupportedVersion is a file written by a newer build.
	ErrUnsupportedVersion = errors.New("workspacestore: workspace written by a newer codeaf")
)

// ConflictError is a write made from a revision that is no longer current. It
// carries the current record so the writer can replay its intent over it.
type ConflictError struct{ Current Record }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("workspacestore: workspace moved to revision %d", e.Current.Revision)
}

// Record is one place's tab set as the store holds it. Revision 0 with a nil
// Workspace means nothing has been saved for that key yet.
type Record struct {
	Key       string          `json:"key"`
	Revision  uint64          `json:"revision"`
	UpdatedAt *time.Time      `json:"updatedAt,omitempty"`
	Writer    string          `json:"writer,omitempty"`
	Workspace json.RawMessage `json:"workspace"`
	// Damaged reports a file that could not be read. It is left exactly where it
	// is until the next write moves it aside (never deletes it), so a read never
	// changes the disk.
	Damaged bool `json:"damaged,omitempty"`
	// MovedTo says where tabs and split panes that left this place went: id →
	// destination key. It is the durable acknowledgment a window uses to send a
	// draft it typed into a moved tab after the move to the tab's new home. It
	// is derived from committed transfers (pair.go), never stored in a
	// document, and absent when nothing recent left this place. A missing hint
	// never means a draft was discarded.
	MovedTo map[string]string `json:"movedTo,omitempty"`
}

// file is the on-disk envelope.
type file struct {
	Schema    int             `json:"schema"`
	Key       string          `json:"key"`
	Revision  uint64          `json:"revision"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Writer    string          `json:"writer,omitempty"`
	Workspace json.RawMessage `json:"workspace"`
}

// Options configures a store.
type Options struct {
	// Dir holds one <key>.json per place. Required and absolute; the caller
	// chose it (cmd/codeaf puts it beside the place graph).
	Dir string
	// LockTimeout bounds how long a write waits for another writer. Default 10s.
	LockTimeout time.Duration
	// Now is the clock. Default time.Now.
	Now func() time.Time
	// WatchInterval is how often Wait re-reads the file to notice a writer in
	// another process. In-process writes wake waiters at once. Default 1s.
	WatchInterval time.Duration
}

// Store is safe for concurrent use. Two processes on one directory never lose
// an update: each write is serialised by the per-key file lock and checked
// against the revision it was made from.
type Store struct {
	opts Options
	// gate serialises this process's writes per key before the file lock.
	gate sync.Mutex
	keys map[string]*sync.Mutex

	// wake is closed and replaced on every in-process write.
	wakeMu sync.Mutex
	wake   chan struct{}

	// pairMu serialises this process's pair-lock holders before the file lock.
	pairMu sync.Mutex

	// beforeRename is a test seam run after the temp file is complete.
	beforeRename func() error
	// afterGetRead is a test seam for recovery racing a read-only projection.
	afterGetRead func()
	// pairFault and syncDir are test seams for the pair journal (pair.go).
	pairFault func(stage string) error
	syncDir   func(dir string) error
}

const lockPoll = 10 * time.Millisecond

// Open prepares the directory. It reads nothing.
func Open(opts Options) (*Store, error) {
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%w: the directory must be absolute", ErrInvalid)
	}
	opts.Dir = filepath.Clean(dir)
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = 10 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.WatchInterval <= 0 {
		opts.WatchInterval = time.Second
	}
	if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{opts: opts, keys: map[string]*sync.Mutex{}, wake: make(chan struct{})}, nil
}

// ValidKey reports whether key names a workspace.
func ValidKey(key string) bool { return keyPattern.MatchString(key) }

func (s *Store) path(key string) string { return filepath.Join(s.opts.Dir, key+".json") }

// Get reads one place's tab set. It takes no lock and writes nothing: a file is
// only ever replaced by rename, so a reader sees one whole document or the other.
// A committed but not yet fully materialized pair transfer is overlaid read-only,
// so a reader never sees one half of a transfer (pair.go).
func (s *Store) Get(key string) (Record, error) {
	if !ValidKey(key) {
		return Record{}, ErrInvalidKey
	}
	// Capture publication before the file: recovery may remove the journal
	// after a stale physical read, but this Get still owes the committed view.
	journal, _ := s.readJournal()
	rec, _, err := s.read(key)
	if err != nil {
		return Record{}, err
	}
	if s.afterGetRead != nil {
		s.afterGetRead()
	}
	return s.overlay(rec, journal), nil
}

// read returns the record and, for a damaged file, why it was unreadable.
func (s *Store) read(key string) (Record, string, error) {
	empty := Record{Key: key}
	f, err := os.Open(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return empty, "", nil
	}
	if err != nil {
		return Record{}, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return Record{}, "", err
	}
	damaged := func(reason string) (Record, string, error) {
		return Record{Key: key, Damaged: true}, reason, nil
	}
	if len(data) > maxFileBytes {
		return damaged("larger than the store accepts")
	}
	var doc file
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return damaged("not valid JSON")
	}
	if _, err := dec.Token(); err != io.EOF {
		return damaged("unexpected data after the document")
	}
	if doc.Schema > SchemaVersion {
		return Record{}, "", fmt.Errorf("%w (file schema %d, this build %d)", ErrUnsupportedVersion, doc.Schema, SchemaVersion)
	}
	if doc.Schema != SchemaVersion || doc.Key != key || doc.Revision == 0 {
		return damaged("not a workspace document for this key")
	}
	if err := Validate(doc.Workspace); err != nil {
		return damaged(err.Error())
	}
	at := doc.UpdatedAt
	return Record{Key: key, Revision: doc.Revision, UpdatedAt: &at, Writer: doc.Writer, Workspace: doc.Workspace}, "", nil
}

// Put replaces one place's tab set if, and only if, the stored revision is still
// `expect` (0 for a key with nothing saved, or a damaged file). Anything else is
// a *ConflictError carrying the current record. A write whose document equals
// the stored one returns the stored record unchanged, so an echo never moves
// the revision.
func (s *Store) Put(key string, expect uint64, writer string, workspace json.RawMessage) (Record, error) {
	if !ValidKey(key) {
		return Record{}, ErrInvalidKey
	}
	if writer != "" && !writerPattern.MatchString(writer) {
		return Record{}, fmt.Errorf("%w: writer tag", ErrInvalid)
	}
	if len(workspace) > MaxDocumentBytes {
		return Record{}, ErrTooLarge
	}
	if err := Validate(workspace); err != nil {
		return Record{}, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, workspace); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	// The pair lock comes first, always, so a write never lands between the
	// two halves of a transfer; a pending transfer is finished before this
	// write's own compare-and-swap reads anything.
	releasePair, err := s.acquirePair()
	if err != nil {
		return Record{}, err
	}
	defer releasePair()
	if err := s.recoverLocked(); err != nil {
		return Record{}, err
	}
	release, err := s.acquire(key)
	if err != nil {
		return Record{}, err
	}
	defer release()
	cur, reason, err := s.read(key)
	if err != nil {
		return Record{}, err
	}
	if cur.Revision != expect {
		return Record{}, &ConflictError{Current: s.withMoves(cur)}
	}
	if !cur.Damaged && cur.Revision > 0 && bytes.Equal(cur.Workspace, compact.Bytes()) {
		return s.withMoves(cur), nil
	}
	now := s.opts.Now().UTC()
	next := cur.Revision + 1
	if cur.Damaged {
		// The unreadable file is set aside, never deleted. Its revision is lost,
		// so the next one starts from the clock: a window still holding a revision
		// from before the damage cannot match it by coincidence.
		if err := s.setAside(key, reason, now); err != nil {
			return Record{}, err
		}
		next = uint64(now.UnixMilli())
	}
	doc := file{Schema: SchemaVersion, Key: key, Revision: next, UpdatedAt: now, Writer: writer, Workspace: compact.Bytes()}
	if err := s.writeLocked(key, &doc); err != nil {
		return Record{}, err
	}
	s.signal()
	return s.withMoves(Record{Key: key, Revision: next, UpdatedAt: &now, Writer: writer, Workspace: doc.Workspace}), nil
}

// setAside moves an unreadable record file next to itself. It never deletes.
func (s *Store) setAside(key, reason string, now time.Time) error {
	to := fmt.Sprintf("%s.damaged-%s", s.path(key), now.Format("20060102T150405.000000000Z"))
	if err := os.Rename(s.path(key), to); err != nil {
		return fmt.Errorf("workspacestore: cannot set aside damaged %s (%s): %w", key, reason, err)
	}
	return nil
}

// Wait returns the record as soon as its revision is not `after`, or the current
// record when ctx ends. It is the long poll behind a window's live mirror: one
// read on entry, then a wake per in-process write and one re-read per
// WatchInterval for a writer in another process.
func (s *Store) Wait(ctx context.Context, key string, after uint64) (Record, error) {
	if !ValidKey(key) {
		return Record{}, ErrInvalidKey
	}
	tick := time.NewTicker(s.opts.WatchInterval)
	defer tick.Stop()
	for {
		wake := s.waiter()
		rec, err := s.Get(key)
		if err != nil || rec.Revision != after || ctx.Err() != nil {
			return rec, err
		}
		select {
		case <-ctx.Done():
			return rec, nil
		case <-wake:
		case <-tick.C:
		}
	}
}

func (s *Store) waiter() <-chan struct{} {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	return s.wake
}

func (s *Store) signal() {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	close(s.wake)
	s.wake = make(chan struct{})
}

// acquire takes this process's per-key gate, then the per-key file lock,
// polling so a wedged holder yields ErrLocked instead of parking forever. Going
// ahead without the lock is NOT acceptable: it would lose an update.
func (s *Store) acquire(key string) (func(), error) {
	s.gate.Lock()
	gate := s.keys[key]
	if gate == nil {
		gate = &sync.Mutex{}
		s.keys[key] = gate
	}
	s.gate.Unlock()
	return s.lockFile(gate, s.path(key)+".lock")
}

// lockFile takes an in-process gate and then the file lock at path.
func (s *Store) lockFile(gate *sync.Mutex, path string) (func(), error) {
	gate.Lock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		gate.Unlock()
		return nil, err
	}
	deadline := time.Now().Add(s.opts.LockTimeout)
	for {
		err := filelock.Lock(f, true, true)
		if err == nil {
			break
		}
		if !filelock.IsBusy(err) || time.Now().After(deadline) {
			f.Close()
			gate.Unlock()
			if filelock.IsBusy(err) {
				return nil, ErrLocked
			}
			return nil, err
		}
		time.Sleep(lockPoll)
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
		gate.Unlock()
	}, nil
}

// writeLocked replaces the file atomically: a complete temp file in the same
// directory, fsynced, renamed over the target, then the directory fsynced. A
// crash at any point leaves the whole old document or the whole new one.
func (s *Store) writeLocked(key string, doc *file) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if len(data) > maxFileBytes {
		return ErrTooLarge
	}
	tmp, err := os.CreateTemp(s.opts.Dir, ".workspace-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if s.beforeRename != nil {
		if err := s.beforeRename(); err != nil {
			os.Remove(name)
			return err
		}
	}
	if err := os.Rename(name, s.path(key)); err != nil {
		os.Remove(name)
		return err
	}
	if d, err := os.Open(s.opts.Dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// FileSignature identifies the current atomic workspace file without reading
// its drafts. It is only a cache hint; callers periodically re-read unchanged
// files too. Missing keys without a pending publication have the zero signature.
// Pending logical publications invalidate even before physical materialization.
// Reads create nothing.
type FileSignature struct {
	Bytes    int64
	Modified int64
	Pair     string
}

func (s *Store) Signature(key string) (FileSignature, error) {
	if !ValidKey(key) {
		return FileSignature{}, ErrInvalidKey
	}
	pair := s.PairStamp()
	if pair == "-/-" {
		pair = ""
	}
	info, err := os.Stat(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		// A completion ledger only carries relocation hints, never a missing
		// workspace document. Do not force every absent Place to decode it.
		if pair == "" || strings.HasPrefix(pair, "-/") {
			return FileSignature{}, nil
		}
		return FileSignature{Pair: pair}, nil
	}
	if err != nil {
		return FileSignature{}, err
	}
	return FileSignature{Bytes: info.Size(), Modified: info.ModTime().UnixNano(), Pair: pair}, nil
}
