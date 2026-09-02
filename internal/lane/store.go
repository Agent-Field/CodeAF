package lane

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

// ── WHERE A BELIEF SLEEPS ───────────────────────────────────────────────────
//
// The strike ledger this package replaces was in memory on purpose, and the
// reasoning was sound as far as it went: endpoint speed is a fact about the
// last few minutes, and a ledger that survived a restart would open every
// session acting on a claim it could no longer see.
//
// A FILTER MAKES THAT ARGUMENT UNNECESSARY. A belief that is written down is
// also written down with the moment it was true, and [Posterior.Predict] widens
// it on the way back in until, some hours later, it is worth about as much as
// the public sheet. So what survives a restart is not yesterday's verdict; it
// is yesterday's evidence, correctly discounted.
//
// THE AGEING HAPPENS AT READ TIME AND NOT HERE. This file loads what was
// written, with the moment each belief was true attached, and reads a clock for
// exactly one thing: stamping a compaction with the moment it happened, which
// is a fact about the FILE and about no belief in it. Whoever asks the ledger a
// question knows what time it is and ages the answer with [Posterior.Predict] —
// the same arithmetic a belief that has merely been sitting in memory for ten
// minutes needs, so there is one place that does it rather than one for each way
// a belief got old.
//
// ── STATE, AND THE OBSERVATIONS SINCE ───────────────────────────────────────
//
// This file is the COMPACTED half: the chains, the beliefs, the quality
// evidence, and the moment it was written. The other half is the append-only
// journal beside it (journal.go), which is where an observation goes the
// instant it is made and which is replayed over this file on every load.
//
// The split exists because the hierarchy has SHARED components. Two processes
// each hold μ, a[lane] and b[model] for the same subjects; "newer wins" would
// discard one of them outright, and a belief set cannot be merged component-
// wise without knowing which evidence went into it. Observations can: they
// replay. So beliefs are written whole, on a beat, and observations are written
// one line at a time, and a cold process reads both.

// ── TWO PROCESSES, ONE FILE ─────────────────────────────────────────────────
//
// A PERSON RUNS AFORGE TWICE. One window is talking to a remote host and the
// other is a plain session in another directory; both are this program, both
// hold a ledger of their own, and both write the whole set to the same file.
// Written as a bare replace, THE LAST ONE TO SAVE WINS OUTRIGHT — and it wins
// with a file that knows nothing about the other process's models.
//
// That is not a hypothetical. On 2026-08-30 a machine with two sessions open
// had a seventeen-lane sheet for `moonshotai/kimi-k3` fetched, decoded and
// primed at 10:31; at 10:34 the other session saved its own ledger — eighteen
// beliefs about a different model — and the kimi lanes were gone from the file.
// The next process loaded the survivor, found two lanes it had merely SEEN and
// no lanes it could choose between, and answered every request with no opinion
// at all. Nothing failed and nothing was logged: a whole mechanism was simply
// not there, and the only symptom was five seconds of somebody waiting.
//
// SO NOTHING IS EVER WRITTEN AS A BARE REPLACE. A write of the whole set is a
// READ-MERGE-WRITE UNDER A LOCK and the merge is by lane id: what one process
// has never heard of, it may not delete ([mergeBeliefs]). The lock is advisory
// and process-wide ([internal/filelock]), the read inside it is the file as it
// stands, and the write out of it is an atomic rename — so a reader that takes
// no lock at all still never sees a half-written set.
//
// AND WHAT CROSSES BETWEEN THE TWO PROCESSES IS OBSERVATIONS, NOT BELIEFS. A
// merge by lane id cannot reconcile the hierarchy, whose μ, a[lane] and b[model]
// are held by both processes with different evidence folded into each. So the
// two write one line per observation to the journal beside this file
// (journal.go) and both replay it, which is a merge that needs no rule: the
// same observations in the same order are the same belief.
//
// AND THE COMPACTION IS SOMEBODY ELSE'S GOROUTINE. An observation is one
// O_APPEND write on the send path, under a lock that is asked for and never
// waited on; the compaction that folds the journal into this file takes the
// exclusive lock and therefore runs only on the writer ([ledger.Persist]).
// Holding both that lock and the ledger's mutex is issue #264, and it cost a
// person twenty-nine silent minutes.

// ErrNoStore is what a store with nowhere to write answers with, so that
// "nothing was kept" and "nothing can be kept" are never confused for each
// other. A file that is simply not there yet is the first of those and reads
// back as no beliefs and no error.
var ErrNoStore = errors.New("lane: no belief store")

// StorePath is the file beliefs sleep in, `~/.aforge/v3/lanes.json` under the
// home this process was pointed at. It is stated once, here, because a path
// that appears twice is a path that drifts.
func StorePath() string { return home.Join("v3", "lanes.json") }

// store is the belief file.
//
// The path is resolved on every call rather than captured at construction
// because AFORGE_HOME is allowed to move the whole state root under a running
// process — a disposable run does exactly that — and a store holding the path
// it was born with would keep writing into the home it was pointed at first.
type store struct {
	mu sync.Mutex
	// writing serialises this process's own whole-file writers, which the file
	// lock cannot: flock is a property of an OPEN DESCRIPTION, so two
	// goroutines here would each take the gate on a handle of their own and
	// each believe itself alone with the file.
	writing sync.Mutex
	// path overrides the file, for a test that must not write into the state
	// root of whoever is running it. Empty is [StorePath].
	path string
}

// newStore builds the store on the real file. It is called from the registry
// and nowhere else.
func newStore() *store { return &store{} }

// at points a store at another file. It exists for tests.
func (s *store) at(path string) *store {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = path
	return s
}

// file is where this store writes. It is called with the lock held.
func (s *store) file() string {
	if path := strings.TrimSpace(s.path); path != "" {
		return path
	}
	return StorePath()
}

// where is [store.file] as everything outside this file's own accessors asks
// it: the path alone, with the lock held for exactly as long as reading it
// takes and never across the disk underneath.
func (s *store) where() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file()
}

// ── WHAT THE FILE HOLDS ─────────────────────────────────────────────────────

// stateVersion is the shape this build writes. Version 1 was a bare JSON array
// of beliefs and nothing else; version 2 is an object, so that the hierarchy
// and the quality evidence above a pair have somewhere to sleep and so that the
// next thing to be kept does not need a third file.
const stateVersion = 2

// storeState is the compacted half of the store.
type storeState struct {
	Version int       `json:"version"`
	At      time.Time `json:"at,omitzero"`
	Beliefs []Belief  `json:"beliefs"`
	// Priors are the sheet's own spreads per lane: the noise a sighting is
	// weighed against and the floor ageing stops at.
	Priors []spread `json:"priors,omitempty"`
	// Wait, Rate and Think are the three hierarchies: ln milliseconds to a
	// first token, ln tokens a second, and ln seconds of a whole thinking
	// phase. Judged is the quality evidence a provider and a model carry above
	// any one pair.
	Wait   chains  `json:"wait,omitzero"`
	Rate   chains  `json:"rate,omitzero"`
	Think  chains  `json:"think,omitzero"`
	Judged tallies `json:"judged,omitzero"`
}

// decodeState reads either shape of the file.
//
// ── MIGRATION LOSES NOTHING AND CLAIMS NOTHING ──────────────────────────────
//
// A version 1 file is a flat array of beliefs: one pair, one first-token
// filter, one rate filter, and no account at all of which part of that was the
// provider and which the model. Read as version 2 it keeps every belief exactly
// as it was written — the ledger still answers [Ledger.Belief] from it — and
// the hierarchy starts empty, which is the honest state: yesterday's file says
// nothing about μ, a[lane] or b[model] because nothing that wrote it was
// keeping them. What the levels then learn, they learn from those beliefs being
// replayed into the chain at the certainty they were recorded with, which is
// the ledger's business and not this file's ([ledger.adopt]).
func decodeState(data []byte) (storeState, error) {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		beliefs, err := decodeBeliefs(trimmed)
		return storeState{Version: 1, Beliefs: beliefs}, err
	}
	var held storeState
	if err := json.Unmarshal(data, &held); err != nil {
		return storeState{}, err
	}
	held.Beliefs = attributed(held.Beliefs)
	return held, nil
}

// readState is what the state file holds right now, and an empty state when it
// holds nothing readable.
//
// A file that cannot be read is a file with nothing in it to preserve. Refusing
// to write over it would strand the process holding the only good copy, which
// is the opposite of what a store is for.
func readState(path string) storeState {
	data, err := os.ReadFile(path)
	if err != nil {
		return storeState{}
	}
	held, err := decodeState(data)
	if err != nil {
		return storeState{}
	}
	return held
}

// decodeBeliefs reads a version 1 file's bytes.
func decodeBeliefs(data []byte) ([]Belief, error) {
	var beliefs []Belief
	if err := json.Unmarshal(data, &beliefs); err != nil {
		return nil, err
	}
	return attributed(beliefs), nil
}

// attributed drops the rows that name no lane.
//
// A row that names no lane is a fact about a machine that was never involved,
// whether it got into the file by hand or by a version of this code that no
// longer exists.
func attributed(beliefs []Belief) []Belief {
	kept := beliefs[:0]
	for _, belief := range beliefs {
		if belief.ID.Zero() {
			continue
		}
		kept = append(kept, belief)
	}
	return kept
}

// ── THE DOORS ───────────────────────────────────────────────────────────────

// Load reads yesterday's beliefs from the compacted state.
//
// It answers the [Store] seam and therefore answers in beliefs, which is the
// compacted half alone: the observations in the journal beside it are folded by
// whoever owns the arithmetic ([ledger.readBack]), because turning an
// observation into a belief is exactly what a store may not decide.
//
// A missing file is no beliefs and no error: it is the normal state of a
// machine that has not routed anything yet, and a caller that had to tell that
// apart from a real failure would end up treating both as neither.
func (s *store) Load() ([]Belief, error) {
	path := s.where()
	if path == "" {
		return nil, ErrNoStore
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	held, err := decodeState(data)
	if err != nil {
		return nil, err
	}
	return held.Beliefs, nil
}

// Save writes the whole set, atomically, over the merge of it with whatever
// another process has written since this one last looked. The journal is left
// alone: a caller handing over a set of beliefs is asserting what it believes,
// not what anybody observed.
func (s *store) Save(beliefs []Belief) error {
	_, err := s.saveMerging(beliefs)
	return err
}

// saveMerging is [store.Save] with the merged set handed back, which is what
// makes the save a load as well: the caller adopts what the file now holds and
// learns, for free, everything the other process wrote.
//
// It is one critical section: take the lock, read the file, fold this process's
// set over it, write, release.
func (s *store) saveMerging(beliefs []Belief) ([]Belief, error) {
	path := s.where()
	if path == "" {
		return nil, ErrNoStore
	}
	s.writing.Lock()
	defer s.writing.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	var merged []Belief
	err := s.locked(path, func() error {
		held := readState(path)
		held.Beliefs = mergeBeliefs(held.Beliefs, beliefs)
		merged = held.Beliefs
		return writeState(path, held)
	})
	if err != nil {
		return nil, err
	}
	return merged, nil
}

// ── THE DEEP DOORS: STATE, JOURNAL, COMPACTION ──────────────────────────────

// deepStore is a store that keeps a whole hierarchy and the observations since
// it was written down. It is an interface rather than the concrete type so that
// a bench or a test may answer the plain [Store] seam and get today's
// write-everything behaviour instead, which is what the seam has always
// promised.
type deepStore interface {
	Store
	// state is the compacted state, the observations appended since, and how
	// many journal lines would not parse.
	state() (storeState, []record, int)
	// log appends one observation. It is on the send path — an answer is
	// folded in the moment it finishes — and it therefore waits on nothing.
	log(entry record) error
	// hold replays the journal under the exclusive lock and writes what fold
	// returns; the journal is emptied only when fold says the state it folded
	// into has been written.
	hold(fold func(storeState, []record, int) (storeState, bool)) error
}

// state reads the compacted file and the journal beside it.
func (s *store) state() (storeState, []record, int) {
	path := s.where()
	if path == "" {
		return storeState{}, nil, 0
	}
	held := readState(path)
	records, skipped := journal{path: journalPath(path)}.read()
	return held, records, skipped
}

// log appends one observation to the journal.
func (s *store) log(entry record) error {
	path := s.where()
	if path == "" {
		return ErrNoStore
	}
	return journal{path: journalPath(path)}.add(entry)
}

// hold is the compaction: the exclusive lock on the state, the exclusive lock
// on the journal, one replay, one atomic write, one truncation.
//
// THE TWO LOCKS ARE TAKEN IN ONE ORDER AND NEVER THE OTHER. An appender takes
// the journal's shared lock and nothing else, so it can never be holding one of
// these while waiting for the other.
func (s *store) hold(fold func(storeState, []record, int) (storeState, bool)) error {
	path := s.where()
	if path == "" {
		return ErrNoStore
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	s.writing.Lock()
	defer s.writing.Unlock()
	var failed error
	return s.locked(path, func() error {
		if err := (journal{path: journalPath(path)}).drain(func(records []record, skipped int) bool {
			next, write := fold(readState(path), records, skipped)
			if !write {
				return false
			}
			failed = writeState(path, next)
			return failed == nil
		}); err != nil {
			return err
		}
		return failed
	})
}

// writeState stamps and writes one state atomically.
func writeState(path string, held storeState) error {
	held.Version, held.At = stateVersion, time.Now().UTC()
	data, err := json.Marshal(held)
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// lockSuffix names the file that serialises writers of the belief file.
//
// IT IS A FILE OF ITS OWN because the write is a RENAME. A lock taken on the
// belief file is a lock on an inode the next write replaces, so two processes
// would each hold an exclusive lock on a different file with the same name and
// believe themselves alone. The lock file is created once and never replaced,
// which is the only thing a lock needs to be.
const lockSuffix = ".lock"

// ── NOTHING HERE WAITS ON A LOCK (issue #264) ───────────────────────────────
//
// flock(2) IS UNINTERRUPTIBLE. It takes no deadline, it reads no context, and
// Go's scheduler cannot break a goroutine out of it. So a lock taken blocking
// is a lock held for exactly as long as whoever else has it — and on 2026-09-01
// that was 29m49s, during which this process sent nothing at all, because the
// wait was entered while the ledger's own mutex was held and every encode reads
// that mutex.
//
// SO EVERY LOCK BELOW IS ASKED FOR AND NEVER WAITED ON. A few tries, a short
// backoff, and then the write is DEFERRED and said so. Deferring is safe by
// construction: an observation is in the journal before any of this is reached
// (journal.go), so what a held lock costs is a compaction and a longer replay,
// never a belief.

// lockAttempts and lockBackoff bound the asking. Five tries at twenty
// milliseconds doubling is about three hundred milliseconds in the worst case,
// spent on the writer goroutine and never in front of a request.
const (
	lockAttempts = 5
	lockBackoff  = 20 * time.Millisecond
)

// errLockBusy is a lock somebody else is holding.
//
// IT IS TOLD APART FROM A FILESYSTEM WITH NO ADVISORY LOCKING, and the
// difference is what the two callers do about it: a busy lock defers the write,
// and a filesystem that cannot lock at all writes anyway — which is what a lone
// process has always done and what a person on a network home is entitled to.
var errLockBusy = errors.New("lane: the belief file's lock is held elsewhere")

// take asks for a lock a few times and never waits on it.
//
// It answers three ways: nil is the lock and the caller must release it,
// [errLockBusy] is somebody else's and the caller defers, and anything else is
// a filesystem with no flock and the caller carries on unlocked.
func take(file *os.File, exclusive bool) error {
	for attempt, backoff := 1, lockBackoff; ; attempt, backoff = attempt+1, backoff*2 {
		err := filelock.Lock(file, exclusive, true)
		if err == nil {
			return nil
		}
		if !filelock.IsBusy(err) {
			return err
		}
		if attempt >= lockAttempts {
			return errLockBusy
		}
		time.Sleep(backoff)
	}
}

// locked runs fn with the exclusive right to read and replace path, and answers
// [errLockBusy] without running it when another process holds that right.
//
// A LOCK THAT CANNOT BE TAKEN IS NOT A REASON TO LOSE A BELIEF. A read-only
// directory, a filesystem with no advisory locking, a home on a share that
// refuses flock: on any of them fn still runs, unlocked, which is exactly the
// behaviour this file had before the lock existed. A lock that is merely BUSY
// is the other case, and it is the one that is deferred: somebody else is
// writing this file right now and waiting for them is the defect.
func (s *store) locked(path string, fn func() error) error {
	gate, err := os.OpenFile(path+lockSuffix, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fn()
	}
	defer gate.Close()
	switch err := take(gate, true); {
	case err == nil:
		defer filelock.Unlock(gate)
	case errors.Is(err, errLockBusy):
		return errLockBusy
	}
	return fn()
}

// ── RECONCILING TWO ACCOUNTS OF ONE LANE ────────────────────────────────────

// mergeBeliefs folds ours over what was already on disk, by lane id.
//
// AN ID THIS PROCESS HAS NEVER HEARD OF SURVIVES UNTOUCHED. That is the whole
// point: a session that has only ever talked to one model must not be able to
// delete what another session learned about a different one. The result is
// sorted, so that two processes writing the same set write the same bytes.
func mergeBeliefs(onDisk, ours []Belief) []Belief {
	held := make(map[ID]Belief, len(onDisk)+len(ours))
	fold := func(belief Belief) {
		if belief.ID.Zero() {
			return
		}
		if seen, ok := held[belief.ID]; ok {
			held[belief.ID] = fresher(seen, belief)
			return
		}
		held[belief.ID] = belief
	}
	for _, belief := range onDisk {
		fold(belief)
	}
	for _, belief := range ours {
		fold(belief)
	}
	all := make([]Belief, 0, len(held))
	for _, belief := range held {
		all = append(all, belief)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].ID.Model != all[j].ID.Model {
			return all[i].ID.Model < all[j].ID.Model
		}
		return all[i].ID.Lane < all[j].ID.Lane
	})
	return all
}

// fresher is one lane's belief as two processes TOGETHER know it.
//
// THE HALVES ARE RECONCILED SEPARATELY, and that is the correction. A belief is
// not one observation with one moment: the timing filters move on a SIGHTING
// and carry [Belief.At], the quality filter moves on an OUTCOME and carries
// [Belief.QualityAt], and what the lane IS moves on a SHEET ROW and carries no
// moment at all. One process that measured a lane a minute ago and another that
// primed it from the sheet a minute ago each hold a different half, and keeping
// only the newer of the two WHOLES throws the other half away — which is how a
// lane with a fresh seventeen-row sheet behind it ends up in the file with
// every fact blank.
//
// So: the newer sighting wins the timing, the newer outcome wins the quality,
// and a fact that was published beats a fact that was never asked for. A tie on
// a moment keeps the account already held, which is arbitrary and deterministic
// — the two processes converge on the next save either way.
func fresher(held, other Belief) Belief {
	kept, older := held, other
	if other.At.After(held.At) {
		kept, older = other, held
	}
	// A BELIEF PRIMED FROM THE SHEET AND NEVER MEASURED CARRIES NO MOMENT and a
	// filter worth having, so the loser's timing is taken where the winner has
	// none rather than dropped for having no clock on it.
	if !kept.TTFT.Known() && older.TTFT.Known() {
		kept.TTFT = older.TTFT
	}
	if !kept.Rate.Known() && older.Rate.Known() {
		kept.Rate = older.Rate
	}
	if !kept.Facts.Known() && older.Facts.Known() {
		kept.Facts = older.Facts
	}
	if older.QualityAt.After(kept.QualityAt) || (!kept.Quality.Known() && older.Quality.Known()) {
		kept.Quality, kept.QualityAt = older.Quality, older.QualityAt
	}
	return kept
}

// writeAtomic writes data at path through a temporary file in the same
// directory and a rename, which is the one write this repo makes to a small
// state file everywhere it makes one.
//
// A reader of either of these files is a cold process deciding where to send
// its first request, and a half-written one would be a belief nobody ever held.
func writeAtomic(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
