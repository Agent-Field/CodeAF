package lane

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
// written, with the moment it was written attached; nothing in it looks at a
// clock. Whoever asks the ledger a question knows what time it is and ages the
// answer with [Posterior.Predict] — which is the same arithmetic a belief that
// has merely been sitting in memory for ten minutes needs, so there is exactly
// one place that does it rather than one for each way a belief got old.
//
// ── WHY THE WHOLE SET, EVERY TIME ───────────────────────────────────────────
//
// A few hundred lanes of a few dozen models is tens of kilobytes. Writing all
// of it on every update is one small atomic write, and it buys the property
// that matters more than the bytes: there is no partial state, no append log to
// compact, and no window in which the file describes a ledger that never
// existed. When it stops being small — the day this build is talking to
// thousands of lanes — the fix is a debounce, not a format.

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
// SO A SAVE IS A READ-MERGE-WRITE UNDER A LOCK, and the merge is by lane id:
// what one process has never heard of, it may not delete. The lock is advisory
// and process-wide ([internal/filelock]), the read inside it is the file as it
// stands, and the write out of it is the same atomic rename as before — so a
// reader that takes no lock at all still never sees a half-written set.
//
// AND THE MERGE IS THE LOAD. What comes back from a save is what the file now
// holds, which the ledger folds into memory: one lock, one read, one write, and
// both processes know what the other learned. Nothing here stats a file on the
// send path — a save happens after an answer, and a load happens at open and on
// the beat.

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

// file is where this store writes.
func (s *store) file() string {
	if path := strings.TrimSpace(s.path); path != "" {
		return path
	}
	return StorePath()
}

// Load reads yesterday's beliefs.
//
// A missing file is no beliefs and no error: it is the normal state of a
// machine that has not routed anything yet, and a caller that had to tell that
// apart from a real failure would end up treating both as neither.
func (s *store) Load() ([]Belief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.file()
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
	return decodeBeliefs(data)
}

// Save writes the whole set, atomically, over the merge of it with whatever
// another process has written since this one last looked.
func (s *store) Save(beliefs []Belief) error {
	_, err := s.saveMerging(beliefs)
	return err
}

// saveMerging is [store.Save] with the merged set handed back, which is what
// makes the save a load as well: the caller adopts what the file now holds and
// learns, for free, everything the other process wrote.
//
// It is the whole of the multi-process contract and it is one critical section:
// take the lock, read the file, fold this process's set over it, write, release.
func (s *store) saveMerging(beliefs []Belief) ([]Belief, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.file()
	if path == "" {
		return nil, ErrNoStore
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	var merged []Belief
	err := s.locked(path, func() error {
		merged = mergeBeliefs(onDisk(path), beliefs)
		data, err := json.Marshal(merged)
		if err != nil {
			return err
		}
		return writeAtomic(path, data)
	})
	if err != nil {
		return nil, err
	}
	return merged, nil
}

// lockSuffix names the file that serialises writers of the belief file.
//
// IT IS A FILE OF ITS OWN because the write is a RENAME. A lock taken on the
// belief file is a lock on an inode the next write replaces, so two processes
// would each hold an exclusive lock on a different file with the same name and
// believe themselves alone. The lock file is created once and never replaced,
// which is the only thing a lock needs to be.
const lockSuffix = ".lock"

// locked runs fn with the exclusive right to read and replace path.
//
// A LOCK THAT CANNOT BE TAKEN IS NOT A REASON TO LOSE A BELIEF. A read-only
// directory, a filesystem with no advisory locking, a home on a share that
// refuses flock: on any of them fn still runs, unlocked, which is exactly the
// behaviour this file had before the lock existed. The lock makes concurrent
// saves safe where it works; it may not make a lone process refuse to write.
func (s *store) locked(path string, fn func() error) error {
	gate, err := os.OpenFile(path+lockSuffix, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fn()
	}
	defer gate.Close()
	if err := filelock.Lock(gate, true, false); err != nil {
		return fn()
	}
	defer filelock.Unlock(gate)
	return fn()
}

// onDisk is what the belief file holds right now, and nothing when it holds
// nothing readable. It is the body of [store.Load] without the store's own
// mutex or an error to report: inside a read-merge-write a file that cannot be
// read is a file with nothing in it to preserve, and refusing to write over it
// would strand the process holding the only good copy.
func onDisk(path string) []Belief {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	beliefs, err := decodeBeliefs(data)
	if err != nil {
		return nil
	}
	return beliefs
}

// decodeBeliefs reads the file's bytes and drops the rows that name no lane.
//
// A row that names no lane is a fact about a machine that was never involved,
// whether it got into the file by hand or by a version of this code that no
// longer exists.
func decodeBeliefs(data []byte) ([]Belief, error) {
	var beliefs []Belief
	if err := json.Unmarshal(data, &beliefs); err != nil {
		return nil, err
	}
	kept := beliefs[:0]
	for _, belief := range beliefs {
		if belief.ID.Zero() {
			continue
		}
		kept = append(kept, belief)
	}
	return kept, nil
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
