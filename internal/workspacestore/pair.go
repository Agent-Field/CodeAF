package workspacestore

// A transfer moves tabs from one place to another, which means writing TWO
// records that must never be seen apart: a reader (or a crash) that finds the
// tab in both places has duplicated it, and one that finds it in neither has
// lost it, drafts and all. Two renames cannot be atomic, so the pair is
// published through a journal:
//
//  1. Under the global pair lock and then both key locks (sorted, so two
//     transfers can never deadlock), re-read both records and compare both
//     revisions. Any mismatch changes nothing and returns both current records.
//  2. Write ONE file, .pair-pending, holding both complete next records and the
//     relocation metadata, by temp file, fsync and rename. That rename is the
//     LOGICAL COMMIT. From that instant Get overlays the journal read-only, so
//     every reader sees both new halves even if the process dies before either
//     record file is replaced.
//  3. Materialize the destination record, then the source record, then append
//     the intent to the bounded completion ledger (.pair-ledger), and only then
//     remove the journal. Every step is idempotent, so any process that finds a
//     journal (the next transfer, or the next ordinary Put) finishes it first.
//
// Ordinary Put takes the same pair lock before its key lock, so a write cannot
// slip between the two halves. Get takes no lock and creates nothing.
//
// The ledger keeps the last maxLedgerEntries intents with their binding hash
// and the ids that left each source. The binding is what makes a retry safe: the
// same intent with the same bytes after a commit is acknowledged and changes
// nothing; the same intent with different bytes is refused (ErrIntentChanged).
// A refusal before the commit reserves nothing, so a window may rebase onto the
// latest documents and retry under the same intent.
//
// A file this code cannot read (damaged bytes, a newer schema) is refused and
// left exactly where it is.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// maxLedgerEntries is how many completed transfers are remembered, for
	// replay detection and for relocation hints.
	maxLedgerEntries = 20
	// maxMovedPerEntry bounds the relocation hints one transfer records.
	maxMovedPerEntry = (MaxTabs + MaxClosed) * (1 + MaxPanes)
	// Every moved identity occurs in the validated destination document, so
	// its escaped key bytes fit that document budget. Each map entry adds only
	// a bounded destination key plus punctuation; metadata adds a small envelope.
	maxMovedBytes = MaxDocumentBytes + maxMovedPerEntry*(len("pl_0000000000000000")+8)
	// Both encoded records plus complete relocation metadata, still bounded.
	maxJournalBytes = 2*maxFileBytes + maxMovedBytes + 16<<10
	maxLedgerBytes  = maxLedgerEntries * (maxMovedBytes + 4<<10)

	pairJournalName = ".pair-pending"
	pairLedgerName  = ".pair-ledger"
	pairLockName    = ".pair.lock"
)

var (
	// ErrIntentChanged is a transfer id that was already used for different bytes.
	ErrIntentChanged = errors.New("workspacestore: that transfer id was already used for different tabs")
	// ErrDamaged is a transfer whose source or destination file cannot be read.
	// Unlike Put, a transfer never sets a damaged file aside.
	ErrDamaged = errors.New("workspacestore: a saved tab set is unreadable and was left as it is")
	// ErrPairState is a pending-transfer or completion file this build cannot
	// read. It is left in place and writes that depend on it are refused.
	ErrPairState = errors.New("workspacestore: the pending transfer record is unreadable and was left as it is")
)

// PairConflictError is a transfer made from revisions that are no longer
// current. Nothing was written.
type PairConflictError struct{ Source, Destination Record }

func (e *PairConflictError) Error() string {
	return fmt.Sprintf("workspacestore: tabs moved to revisions %d and %d", e.Source.Revision, e.Destination.Revision)
}

// PairRequest is one transfer: both complete next documents and the revisions
// each was made from.
type PairRequest struct {
	Intent, Writer, Source, Destination   string
	SourceRevision, DestinationRevision   uint64
	SourceWorkspace, DestinationWorkspace json.RawMessage
}

// PairResult acknowledges a committed transfer with both current records.
type PairResult struct {
	Intent      string
	Source      Record
	Destination Record
	// Already is true when the transfer had been committed before this call.
	Already bool
}

// journal is the committed, not yet fully materialized, transfer.
type pairJournal struct {
	Schema      int               `json:"schema"`
	Intent      string            `json:"intent"`
	Binding     string            `json:"binding"`
	Writer      string            `json:"writer,omitempty"`
	At          time.Time         `json:"at"`
	Source      file              `json:"source"`
	Destination file              `json:"destination"`
	Moved       map[string]string `json:"moved,omitempty"`
}

func (j *pairJournal) record(key string) (Record, bool) {
	for _, f := range []*file{&j.Source, &j.Destination} {
		if f.Key == key {
			at := f.UpdatedAt
			return Record{Key: key, Revision: f.Revision, UpdatedAt: &at, Writer: f.Writer, Workspace: f.Workspace}, true
		}
	}
	return Record{}, false
}

func (j *pairJournal) entry() ledgerEntry {
	return ledgerEntry{
		Intent: j.Intent, Binding: j.Binding, Writer: j.Writer, At: j.At,
		Source: j.Source.Key, Destination: j.Destination.Key,
		SourceRevision: j.Source.Revision, DestinationRevision: j.Destination.Revision,
		Moved: j.Moved,
	}
}

type ledgerEntry struct {
	Intent              string            `json:"intent"`
	Binding             string            `json:"binding"`
	Writer              string            `json:"writer,omitempty"`
	At                  time.Time         `json:"at"`
	Source              string            `json:"source"`
	Destination         string            `json:"destination"`
	SourceRevision      uint64            `json:"sourceRevision"`
	DestinationRevision uint64            `json:"destinationRevision"`
	Moved               map[string]string `json:"moved,omitempty"`
}

type pairLedger struct {
	Schema  int           `json:"schema"`
	Entries []ledgerEntry `json:"entries"`
}

func (l *pairLedger) find(intent string) *ledgerEntry {
	if l == nil {
		return nil
	}
	for i := range l.Entries {
		if l.Entries[i].Intent == intent {
			return &l.Entries[i]
		}
	}
	return nil
}

func (s *Store) journalPath() string { return filepath.Join(s.opts.Dir, pairJournalName) }
func (s *Store) ledgerPath() string  { return filepath.Join(s.opts.Dir, pairLedgerName) }

// PairStamp is a stat-only fingerprint of the pending-transfer and completion
// files. It changes the moment a transfer is logically committed (the journal
// rename) and again when it completes, so a read-only cache of Get results
// (for example a signature of the tab sets) can fold it in and be invalidated
// by a publication that has not yet touched any record file. It reads no
// content and creates nothing.
func (s *Store) PairStamp() string {
	stamp := func(path string) string {
		fi, err := os.Stat(path)
		if err != nil {
			return "-"
		}
		return fmt.Sprintf("%d.%d", fi.Size(), fi.ModTime().UnixNano())
	}
	return stamp(s.journalPath()) + "/" + stamp(s.ledgerPath())
}

// readStrict reads a small state file. A missing file is (nil, nil).
func readStrict(path string, limit int64, into any, schemaOf func() int) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.ErrNotExist
	}
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("%w: larger than the store accepts", ErrPairState)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%w: not valid JSON", ErrPairState)
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("%w: unexpected data after the document", ErrPairState)
	}
	if v := schemaOf(); v > SchemaVersion {
		return fmt.Errorf("%w (file schema %d, this build %d)", ErrUnsupportedVersion, v, SchemaVersion)
	} else if v != SchemaVersion {
		return fmt.Errorf("%w: unknown schema", ErrPairState)
	}
	return nil
}

// readJournal returns the committed pending transfer, or nil when there is none.
func (s *Store) readJournal() (*pairJournal, error) {
	var j pairJournal
	err := readStrict(s.journalPath(), int64(maxJournalBytes), &j, func() int { return j.Schema })
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	bad := func(why string) (*pairJournal, error) { return nil, fmt.Errorf("%w: %s", ErrPairState, why) }
	if !writerPattern.MatchString(j.Intent) || j.Binding == "" {
		return bad("no intent")
	}
	if !ValidKey(j.Source.Key) || !ValidKey(j.Destination.Key) || j.Source.Key == j.Destination.Key {
		return bad("keys")
	}
	for _, f := range []*file{&j.Source, &j.Destination} {
		if f.Schema != SchemaVersion || f.Revision == 0 || Validate(f.Workspace) != nil {
			return bad("a record is not a workspace document")
		}
	}
	return &j, nil
}

// readLedger returns the completion ledger, empty when there is none.
func (s *Store) readLedger() (*pairLedger, error) {
	var l pairLedger
	err := readStrict(s.ledgerPath(), int64(maxLedgerBytes), &l, func() int { return l.Schema })
	if errors.Is(err, os.ErrNotExist) {
		return &pairLedger{Schema: SchemaVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// overlay shows a committed pending transfer on top of the disk record. It is
// read-only: the journal only wins when it is newer than the file.
func (s *Store) overlay(rec Record, j *pairJournal) Record {
	if j != nil {
		if jr, ok := j.record(rec.Key); ok && jr.Revision > rec.Revision {
			rec = jr
		}
	}
	return s.withMovesJ(rec, j)
}

func (s *Store) withMoves(rec Record) Record {
	j, _ := s.readJournal()
	return s.withMovesJ(rec, j)
}

// withMovesJ attaches the relocation hints for rec's place. Entries are applied
// oldest first: a transfer out of the place records where each id went, and a
// later transfer back in cancels the hint, so a reverse transfer leaves the hint
// on the OTHER place. A hint is also dropped while its id is open or closed in
// the record being returned (an Undo, an explicit restore). A hint that cannot
// be read is simply absent: it never claims a draft was discarded.
func (s *Store) withMovesJ(rec Record, j *pairJournal) Record {
	led, err := s.readLedger()
	var entries []ledgerEntry
	if err == nil {
		entries = led.Entries
	}
	if j != nil && led.find(j.Intent) == nil {
		entries = append(append([]ledgerEntry(nil), entries...), j.entry())
	}
	moved := map[string]string{}
	for _, e := range entries {
		if e.Destination == rec.Key {
			for id := range e.Moved {
				delete(moved, id)
			}
		}
		if e.Source == rec.Key {
			for id, to := range e.Moved {
				moved[id] = to
			}
		}
	}
	if len(moved) > 0 {
		for id := range docIDs(rec.Workspace) {
			delete(moved, id)
		}
	}
	if len(moved) == 0 {
		rec.MovedTo = nil
	} else {
		rec.MovedTo = moved
	}
	return rec
}

// docIDs is every tab and split-pane id in a shared document, open or closed.
// open reports only the open ones.
func docIDs(raw json.RawMessage) map[string]bool {
	ids := map[string]bool{}
	collect(raw, ids, true, true)
	return ids
}

func collect(raw json.RawMessage, into map[string]bool, open, closed bool) {
	if len(raw) == 0 {
		return
	}
	type pane struct {
		ID string `json:"id"`
	}
	type tab struct {
		pane
		Split *struct {
			Panes []pane `json:"panes"`
		} `json:"split"`
	}
	var doc struct {
		Tabs   []tab `json:"tabs"`
		Closed []tab `json:"closed"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return
	}
	add := func(tabs []tab) {
		for _, t := range tabs {
			into[t.ID] = true
			if t.Split != nil {
				for _, p := range t.Split.Panes {
					into[p.ID] = true
				}
			}
		}
	}
	if open {
		add(doc.Tabs)
	}
	if closed {
		add(doc.Closed)
	}
}

// movedIDs is what a transfer took out of the source and put in the destination.
func movedIDs(prevSource, nextSource, nextDestination json.RawMessage, to string) map[string]string {
	after := docIDs(nextSource)
	in := docIDs(nextDestination)
	var ids []string
	for id := range docIDs(prevSource) {
		if !after[id] && in[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > maxMovedPerEntry {
		ids = ids[:maxMovedPerEntry]
	}
	if len(ids) == 0 {
		return nil
	}
	moved := make(map[string]string, len(ids))
	for _, id := range ids {
		moved[id] = to
	}
	return moved
}

// canonical re-encodes a document with sorted keys so that the same document
// serialised by another client binds to the same intent.
func canonical(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func binding(req *PairRequest, source, destination []byte) (string, error) {
	cs, err := canonical(source)
	if err != nil {
		return "", err
	}
	cd, err := canonical(destination)
	if err != nil {
		return "", err
	}
	sum := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	data, err := json.Marshal(struct {
		Intent, Writer, Source, Destination string
		SourceRevision, DestinationRevision uint64
		SourceSHA, DestinationSHA           string
	}{req.Intent, req.Writer, req.Source, req.Destination, req.SourceRevision, req.DestinationRevision, sum(cs), sum(cd)})
	if err != nil {
		return "", err
	}
	return sum(data), nil
}

func (s *Store) acquirePair() (func(), error) {
	return s.lockFile(&s.pairMu, filepath.Join(s.opts.Dir, pairLockName))
}

// acquireKeys takes the key locks in sorted order, whatever order they are named.
func (s *Store) acquireKeys(keys ...string) (func(), error) {
	sort.Strings(keys)
	var releases []func()
	for _, k := range keys {
		r, err := s.acquire(k)
		if err != nil {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
			return nil, err
		}
		releases = append(releases, r)
	}
	return func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}, nil
}

func (s *Store) fault(stage string) error {
	if s.pairFault == nil {
		return nil
	}
	return s.pairFault(stage)
}

func (s *Store) syncDirectory() error {
	if s.syncDir != nil {
		return s.syncDir(s.opts.Dir)
	}
	d, err := os.Open(s.opts.Dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// writeState replaces a state file by temp file, fsync and rename. renamed
// reports that the rename happened, which is the point of no return.
func (s *Store) writeState(path string, data []byte, stage string) (renamed bool, err error) {
	tmp, err := os.CreateTemp(s.opts.Dir, ".pair-*.tmp")
	if err != nil {
		return false, err
	}
	name := tmp.Name()
	fail := func(err error) (bool, error) {
		tmp.Close()
		os.Remove(name)
		return false, err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return false, err
	}
	if err := s.fault(stage); err != nil {
		os.Remove(name)
		return false, err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return false, err
	}
	return true, nil
}

// PutPair publishes a transfer: both documents or neither. See the file comment.
//
// It returns a *PairConflictError (nothing written) when either revision is
// stale, ErrIntentChanged when the intent was committed with other bytes, and
// the usual validation errors. Once the journal is committed it never reports
// failure: a problem finishing is left for the next writer and the committed
// records are acknowledged, because telling a window nothing changed would
// invite a second, duplicating transfer.
func (s *Store) PutPair(req PairRequest) (PairResult, error) {
	if !writerPattern.MatchString(req.Intent) {
		return PairResult{}, invalid("transfer id")
	}
	if req.Writer != "" && !writerPattern.MatchString(req.Writer) {
		return PairResult{}, invalid("writer tag")
	}
	if !ValidKey(req.Source) || !ValidKey(req.Destination) {
		return PairResult{}, ErrInvalidKey
	}
	if req.Source == req.Destination {
		return PairResult{}, invalid("a transfer needs two different places")
	}
	var compact [2]bytes.Buffer
	for i, raw := range []json.RawMessage{req.SourceWorkspace, req.DestinationWorkspace} {
		if len(raw) > MaxDocumentBytes {
			return PairResult{}, ErrTooLarge
		}
		if err := Validate(raw); err != nil {
			return PairResult{}, err
		}
		if err := json.Compact(&compact[i], raw); err != nil {
			return PairResult{}, invalid("%v", err)
		}
	}
	bind, err := binding(&req, compact[0].Bytes(), compact[1].Bytes())
	if err != nil {
		return PairResult{}, invalid("%v", err)
	}

	releasePair, err := s.acquirePair()
	if err != nil {
		return PairResult{}, err
	}
	defer releasePair()

	j, err := s.readJournal()
	if err != nil {
		return PairResult{}, err
	}
	if j != nil {
		if j.Intent == req.Intent {
			if j.Binding != bind {
				return PairResult{}, ErrIntentChanged
			}
			_ = s.recoverLocked() // best effort: the overlay already shows the commit
			return s.acknowledge(req, true)
		}
		if err := s.recoverLocked(); err != nil {
			return PairResult{}, err
		}
	}
	led, err := s.readLedger()
	if err != nil {
		return PairResult{}, err
	}
	if e := led.find(req.Intent); e != nil {
		if e.Binding != bind {
			return PairResult{}, ErrIntentChanged
		}
		return s.acknowledge(req, true)
	}

	releaseKeys, err := s.acquireKeys(req.Source, req.Destination)
	if err != nil {
		return PairResult{}, err
	}
	defer releaseKeys()
	curS, _, err := s.read(req.Source)
	if err != nil {
		return PairResult{}, err
	}
	curD, _, err := s.read(req.Destination)
	if err != nil {
		return PairResult{}, err
	}
	if curS.Damaged || curD.Damaged {
		return PairResult{}, ErrDamaged
	}
	if curS.Revision != req.SourceRevision || curD.Revision != req.DestinationRevision {
		return PairResult{}, &PairConflictError{Source: s.withMoves(curS), Destination: s.withMoves(curD)}
	}

	now := s.opts.Now().UTC()
	next := pairJournal{
		Schema: SchemaVersion, Intent: req.Intent, Binding: bind, Writer: req.Writer, At: now,
		Source:      file{Schema: SchemaVersion, Key: req.Source, Revision: curS.Revision + 1, UpdatedAt: now, Writer: req.Writer, Workspace: compact[0].Bytes()},
		Destination: file{Schema: SchemaVersion, Key: req.Destination, Revision: curD.Revision + 1, UpdatedAt: now, Writer: req.Writer, Workspace: compact[1].Bytes()},
		Moved:       movedIDs(curS.Workspace, compact[0].Bytes(), compact[1].Bytes(), req.Destination),
	}
	// Check the actual encoded form before the public journal rename. Raw
	// JSON documents can fit the input bound yet expand under HTML escaping;
	// publishing one the record/journal readers reject would strand the commit.
	for _, record := range []*file{&next.Source, &next.Destination} {
		encoded, err := json.Marshal(record)
		if err != nil {
			return PairResult{}, err
		}
		if len(encoded) > maxFileBytes {
			return PairResult{}, ErrTooLarge
		}
		var persisted file
		if err := json.Unmarshal(encoded, &persisted); err != nil {
			return PairResult{}, err
		}
		if len(persisted.Workspace) > MaxDocumentBytes {
			return PairResult{}, ErrTooLarge
		}
	}
	data, err := json.Marshal(&next)
	if err != nil {
		return PairResult{}, err
	}
	if len(data) > maxJournalBytes {
		return PairResult{}, ErrTooLarge
	}
	if err := s.fault("before-journal"); err != nil {
		return PairResult{}, err
	}
	if _, err := s.writeState(s.journalPath(), data, "journal-rename"); err != nil {
		return PairResult{}, err
	}
	// A renamed journal is already public to lock-free readers. A failed
	// directory fsync cannot unpublish it: doing so would roll back a commit
	// another window has observed. Finishing retries durable materialization;
	// failures leave the journal for the next writer. An I/O failure still means
	// crash durability cannot be guaranteed, but never means nothing changed.
	_ = s.syncDirectory()

	s.signal()
	_ = s.finishLocked(&next)
	return s.acknowledge(req, false)
}

func (s *Store) acknowledge(req PairRequest, already bool) (PairResult, error) {
	src, err := s.Get(req.Source)
	if err != nil {
		return PairResult{}, err
	}
	dst, err := s.Get(req.Destination)
	if err != nil {
		return PairResult{}, err
	}
	return PairResult{Intent: req.Intent, Source: src, Destination: dst, Already: already}, nil
}

// recoverLocked finishes a pending transfer. The caller holds the pair lock and
// no key lock. It does nothing when no transfer is pending.
func (s *Store) recoverLocked() error {
	j, err := s.readJournal()
	if err != nil || j == nil {
		return err
	}
	release, err := s.acquireKeys(j.Source.Key, j.Destination.Key)
	if err != nil {
		return err
	}
	defer release()
	return s.finishLocked(j)
}

// finishLocked materializes a committed transfer. The caller holds the pair
// lock and both key locks. The destination goes first: a crash between the two
// renames then leaves a tab in both places for the overlay to hide, never in
// neither.
func (s *Store) finishLocked(j *pairJournal) error {
	defer s.signal()
	if err := s.fault("after-journal"); err != nil {
		return err
	}
	if err := s.materialize(&j.Destination); err != nil {
		return err
	}
	if err := s.fault("after-destination"); err != nil {
		return err
	}
	if err := s.materialize(&j.Source); err != nil {
		return err
	}
	if err := s.fault("after-source"); err != nil {
		return err
	}
	if err := s.appendLedger(j); err != nil {
		return err
	}
	if err := s.fault("after-ledger"); err != nil {
		return err
	}
	if err := os.Remove(s.journalPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = s.syncDirectory()
	return nil
}

// materialize writes one record of the journal unless the disk already has it
// (or something newer). A damaged file is set aside, never deleted.
func (s *Store) materialize(f *file) error {
	cur, reason, err := s.read(f.Key)
	if err != nil {
		return err
	}
	if !cur.Damaged && cur.Revision >= f.Revision {
		return nil
	}
	if cur.Damaged {
		if err := s.setAside(f.Key, reason, s.opts.Now().UTC()); err != nil {
			return err
		}
	}
	return s.writeLocked(f.Key, f)
}

func (s *Store) appendLedger(j *pairJournal) error {
	led, err := s.readLedger()
	if err != nil {
		return err
	}
	if led.find(j.Intent) != nil {
		return nil
	}
	led.Schema = SchemaVersion
	led.Entries = append(led.Entries, j.entry())
	if len(led.Entries) > maxLedgerEntries {
		led.Entries = led.Entries[len(led.Entries)-maxLedgerEntries:]
	}
	data, err := json.Marshal(led)
	if err != nil {
		return err
	}
	if _, err := s.writeState(s.ledgerPath(), data, "ledger-rename"); err != nil {
		return err
	}
	return s.syncDirectory()
}
