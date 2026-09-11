package standing

// store.go is the file half of the contract: one JSON document per item, an
// item folder beside it for runs and the log, and nothing else. Every path is
// answered by standing.go's methods and every byte is written through this
// file, so the day an index is wanted it can be added here without a caller
// hearing about it.
//
// TEMP + RENAME UNDER A PER-ITEM FLOCK. Three writers can reach one document —
// the ticker, the person's window, a firing that just finished — and the flock
// is what orders them while the rename is what makes a half-written document
// impossible to read.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"

	"github.com/Agent-Field/aforge-v2/internal/effort"
)

// Create validates, assigns an id when there is none, stamps Created, Updated
// and Status active, writes the document, and answers the item as written.
// IT IS ONLY CALLED AFTER A YES.
func (s *Store) Create(item Item) (Item, error) {
	if err := item.Validate(); err != nil {
		return Item{}, err
	}
	if err := item.CheckWatch(); err != nil {
		return Item{}, err
	}
	now := s.now()
	if strings.TrimSpace(item.ID) == "" {
		item.ID = newID()
	}
	if err := checkID(item.ID); err != nil {
		return Item{}, err
	}
	item.Schema = Schema
	item.Revision = 1
	item.SpecRevision = 1
	item.Status = StatusActive
	item.RetiredWhy = ""
	item.Created = now
	item.Updated = now
	due, err := firstDue(item, now)
	if err != nil {
		return Item{}, err
	}
	item.NextDue = due
	if err := s.write(item); err != nil {
		return Item{}, err
	}
	return item, nil
}

// firstDue is when a fresh item first wants attention. A moment is its own
// moment; a rhythm's first firing is the next one after now, never now itself,
// because "every Monday at 9" agreed to on a Monday at 10 does not mean today;
// a probe wants its first look on the very next pass, because "tell me when CI
// goes red" means starting now.
func firstDue(item Item, now time.Time) (time.Time, error) {
	switch item.When.Kind {
	case WhenAt:
		return item.When.At, nil
	case WhenEvery:
		next, err := ParseEvery(item.When.Every)
		if err != nil {
			return time.Time{}, err
		}
		return next(now), nil
	case WhenProbe:
		return now, nil
	}
	return time.Time{}, nil
}

// ErrConflict means a whole-document edit was based on an older reading.
// The caller must read again rather than overwrite newer control or progress.
var ErrConflict = errors.New("standing item changed; read it again before editing")

// Save replaces a document only while its revision is still current. Narrow
// gestures use the owner operations below so unrelated progress cannot reject
// a pause or be overwritten by one. It never recreates a missing item.
func (s *Store) Save(item Item) error {
	_, err := s.mutate(item.ID, func(current *Item) error {
		if current.Revision != item.Revision {
			return ErrConflict
		}
		*current = item
		return nil
	})
	return err
}

// mutate holds the item lock only while reading and replacing one document.
// NO MODEL OR NETWORK CALL MAY RUN INSIDE THIS OPERATION.
func (s *Store) mutate(id string, change func(*Item) error) (Item, error) {
	if err := checkID(id); err != nil {
		return Item{}, ErrNotFound
	}
	var result Item
	err := s.underItemLock(id, func() error {
		current, err := s.read(s.ItemPath(id))
		if err != nil {
			return err
		}
		revision := current.Revision
		if err := change(&current); err != nil {
			return err
		}
		if err := current.Validate(); err != nil {
			return err
		}
		current.Schema = Schema
		current.Revision = revision + 1
		current.Updated = s.now()
		if err := s.writeUnlocked(current); err != nil {
			return err
		}
		result = current
		return nil
	})
	return result, err
}

// SetStatus applies the person's control to the latest document, preserving
// every configuration field and every completed run. Retired items stay retired.
func (s *Store) SetStatus(id string, status Status, reason string) (Item, error) {
	return s.mutate(id, func(item *Item) error {
		if status != StatusActive && status != StatusPaused && status != StatusRetired {
			return errors.New("unknown standing status")
		}
		if item.Status == StatusRetired && status != StatusRetired {
			return errors.New("a stopped item must be set up afresh")
		}
		item.Status = status
		item.RetiredWhy = ""
		if status == StatusRetired {
			item.RetiredWhy = reason
		}
		return nil
	})
}

// AddException narrows the current item without replacing a concurrent edit or
// runtime receipt. Its validation remains the item's own validation.
func (s *Store) AddException(id string, exception Exception) error {
	_, err := s.mutate(id, func(item *Item) error {
		if !item.ExceptedFrom(exception.Workspace, exception.SessionID) {
			item.Exceptions = append(item.Exceptions, exception)
		}
		return nil
	})
	return err
}

// FileExchange changes only the two paths moved by the home exchange filing.
func (s *Store) FileExchange(id, directory, transcript string) (Item, error) {
	return s.mutate(id, func(item *Item) error {
		item.Origin.Exchange = directory
		item.Origin.Transcript = transcript
		return nil
	})
}

// SetStandingEffort sets how hard one item's firings and its checks think, and
// is the door a surface calls to move that rung.
//
// It is a read-modify-write under the item's own flock rather than a field on a
// whole item somebody hands back, for the reason [Store.Save] takes a whole
// document and this does not: a surface holding an item it read a minute ago
// would write back the check results, the spend and the next-due that the ticker
// has moved since, and quietly undo a firing.
//
// The rung is validated against the ladder here, so a word nothing can parse is
// refused at the door instead of landing on disk and reading back as absence
// forever. Absence itself IS settable — "off" and "" both clear the field — and
// clearing it puts the item back on the standing role's own floor.
func (s *Store) SetStandingEffort(id string, rung effort.Rung) error {
	if !rung.Valid() && rung != effort.None {
		return fmt.Errorf("%q is not a thinking level", rung)
	}
	_, err := s.mutate(id, func(item *Item) error {
		item.Does.Effort = rung.String()
		return nil
	})
	return err
}

// Get reads one item. A missing id is [ErrNotFound].
func (s *Store) Get(id string) (Item, error) {
	if err := checkID(id); err != nil {
		return Item{}, ErrNotFound
	}
	item, err := s.read(s.ItemPath(id))
	if err != nil {
		return Item{}, err
	}
	return item, nil
}

// List reads every item, newest first. An unreadable or newer-schema document
// is skipped, not fatal.
//
// SKIPPING IS THE POINT. One document written by a build from the future, or
// one truncated by a full disk, must not be able to stop every other standing
// thing a person owns from being checked.
func (s *Store) List() ([]Item, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	items := make([]Item, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		item, err := s.read(filepath.Join(s.root, entry.Name()))
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].Created.Equal(items[b].Created) {
			return items[a].ID < items[b].ID
		}
		return items[a].Created.After(items[b].Created)
	})
	return items, nil
}

// ForWorkspace is List filtered to one project, the grouping home draws.
func (s *Store) ForWorkspace(workspace string) ([]Item, error) {
	items, err := s.List()
	if err != nil {
		return nil, err
	}
	want := filepath.Clean(workspace)
	kept := items[:0]
	for _, item := range items {
		if filepath.Clean(item.Workspace) == want {
			kept = append(kept, item)
		}
	}
	return kept, nil
}

// Applicable is THE ONE RESOLVER: which standing things reach this place, in
// the order a person reads them. Every seam that asks "what stands over here?"
// asks this and nothing else, so a conversation, a page and a task's world can
// never come to three different answers about one item.
//
// IT IS THREE SHELVES AND NOT A SORT KEY. This conversation's own orders lead,
// then this project's, then the machine's — narrowest first, because the
// nearest one is the one somebody just made and the one they mean when they say
// "not here". Within a shelf the most recently touched leads, and that is
// [Item.Updated] rather than Created: an order paused, excepted or edited this
// morning is the one they are thinking about.
//
// ONLY WHAT IS ACTUALLY STANDING. A paused order governs nothing while it is
// paused and a retired one is over, so neither is here — a list that included
// them would be saying something is true of this place that is not.
//
// Callers pass what they know; an empty sessionID is a place with no
// conversation ([Item.AppliesTo]), which is what a task's worktree and a firing
// both are.
func (s *Store) Applicable(workspace, sessionID string) ([]Item, error) {
	return s.ApplicableScope(workspace, sessionID, nil)
}

// ApplicableScope is the same resolver with explicit governing folder bindings.
// A caller without those bindings cannot infer them from navigation membership.
func (s *Store) ApplicableScope(workspace, sessionID string, collections map[string]int) ([]Item, error) {
	items, err := s.List()
	if err != nil {
		return nil, err
	}
	kept := items[:0]
	for _, item := range items {
		if item.Status != StatusActive || !item.AppliesToScope(workspace, sessionID, collections) {
			continue
		}
		kept = append(kept, item)
	}
	sort.SliceStable(kept, func(a, b int) bool {
		if shelf(kept[a]) != shelf(kept[b]) {
			return shelf(kept[a]) < shelf(kept[b])
		}
		return kept[a].Updated.After(kept[b].Updated)
	})
	return kept, nil
}

// shelf is which of the three shelves an item is drawn on, nearest first. It is
// [Item.Level] as a number, because the order is arithmetic and the altitudes
// are words; it is unexported because a surface that wants the shelf itself
// already has it by name.
func shelf(item Item) int {
	switch item.Level() {
	case AltitudeConversation:
		return 0
	case AltitudeProject:
		return 1
	}
	return 2
}

// Log adds one line to an item's own log: a check that found something, a
// firing, a pause, a stop. NEVER A LINE PER QUIET CHECK — a watch that looks
// every five minutes for a year would otherwise leave a hundred thousand lines
// saying nothing happened.
func (s *Store) Log(id, line string) error {
	if err := checkID(id); err != nil {
		return err
	}
	if err := os.MkdirAll(s.ItemDir(id), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.LogPath(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := fmt.Fprintf(file, "%s  %s\n", s.now().Format(time.RFC3339), oneLine(line)); err != nil {
		return err
	}
	return file.Close()
}

// runCounterFile is where an item records the last run number it minted,
// beside its runs/ folder rather than in it, so nothing that walks the runs
// ever meets it.
const runCounterFile = "last-run"

// RunName is the folder name of run number n: zero-padded to six places, so a
// listing sorts the way the runs happened for the first million of them. The
// width is for people reading a listing; this package never trusts it, and
// orders runs by their number ([newestFirst]), so a folder named by the older
// four-place format sits in its right place beside the newer ones.
func RunName(n int) string { return fmt.Sprintf("%06d", n) }

// newRunDir mints the item's next run number and makes its folder.
//
// A RUN NUMBER IS MINTED ONCE AND NEVER REUSED (the scale audit's law L8,
// finding F7). It used to be the highest folder on disk plus one — so when the
// sweep reaped the newest run for coming to nothing, the next firing was given
// its number, and the ledger's run path, the item's last run, a retry's
// "supersedes" and a journal's cause could each name a different firing than
// the one they were written about. The number now comes from the item's own
// counter, advanced under the item's lock and written durably BEFORE the folder
// is made: a crash between the two skips a number, and never hands one out
// twice.
func (s *Store) newRunDir(id string) (string, error) {
	runs := s.RunsDir(id)
	if err := os.MkdirAll(runs, 0o700); err != nil {
		return "", err
	}
	var path string
	err := s.underItemLock(id, func() error {
		last, err := s.lastMinted(id)
		if err != nil {
			return err
		}
		// A folder that already exists is a run made by a process that died
		// before its counter was written, so its number is passed over rather
		// than trampled.
		for next := last + 1; next <= last+64; next++ {
			if err := writeAtomic(filepath.Join(s.ItemDir(id), runCounterFile), []byte(strconv.Itoa(next)+"\n")); err != nil {
				return err
			}
			candidate := filepath.Join(runs, RunName(next))
			if err := os.Mkdir(candidate, 0o700); err == nil {
				path = candidate
				return nil
			} else if !os.IsExist(err) {
				return err
			}
		}
		return errors.New("standing: cannot make a run folder")
	})
	return path, err
}

// lastMinted is the last run number the item minted. An item made before the
// counter existed has none, and its highest folder on disk is where the
// counter starts — the one time that folder is read for a number.
func (s *Store) lastMinted(id string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(s.ItemDir(id), runCounterFile))
	if err == nil {
		last, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || last < 0 {
			return 0, fmt.Errorf("standing: the run counter of %s is unreadable", id)
		}
		return last, nil
	}
	if !os.IsNotExist(err) {
		return 0, err
	}
	names, err := runFolders(s.RunsDir(id))
	if err != nil || len(names) == 0 {
		return 0, err
	}
	number, _ := runNumber(names[0])
	return number, nil
}

// runFolders answers the run folders under runs, newest first.
func runFolders(runs string) ([]string, error) {
	entries, err := os.ReadDir(runs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, ok := runNumber(entry.Name()); ok && entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	newestFirst(names)
	return names, nil
}

// newestFirst orders run folder names by their NUMBER, highest first. A string
// sort put "9999" ahead of "10000", so past ten thousand runs the recovery
// window read the oldest records as the newest (F7).
func newestFirst(names []string) {
	sort.SliceStable(names, func(i, j int) bool {
		a, _ := runNumber(names[i])
		b, _ := runNumber(names[j])
		return a > b
	})
}

// runNumber reads a run folder's number off its name.
func runNumber(name string) (int, bool) {
	number, err := strconv.Atoi(name)
	return number, err == nil && number > 0
}

// UnlessStopped runs act under the item's own flock — the lock a person's stop
// is written under ([Store.SetStatus]) — and only while the item is not
// stopped, answering whether it was.
//
// A CONTROL IS RECHECKED WHERE THE EFFECT HAPPENS (the scale audit's law L2).
// Asked once before a firing's last acts, a stop that landed between the
// question and the act was missed, and the report of a stopped item was still
// published. Under the one lock both take, a stop lands either before the act,
// which then does not happen, or after it, which then already has. An item the
// store does not hold is not stopped: nobody could have said stop to it.
// NO MODEL OR NETWORK CALL MAY RUN INSIDE act.
func (s *Store) UnlessStopped(id string, act func() error) (stopped bool, err error) {
	if err := checkID(id); err != nil {
		return false, err
	}
	err = s.underItemLock(id, func() error {
		current, err := s.read(s.ItemPath(id))
		if err == nil && current.Status == StatusRetired {
			stopped = true
			return nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		return act()
	})
	return stopped, err
}

// now is the store's clock. It is a field rather than a call to time.Now so a
// test can hold a whole store still, exactly as the ticker's Now does.
func (s *Store) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

func (s *Store) read(path string) (Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Item{}, ErrNotFound
		}
		return Item{}, err
	}
	var item Item
	if err := json.Unmarshal(data, &item); err != nil {
		return Item{}, fmt.Errorf("standing: cannot read %s: %w", filepath.Base(path), err)
	}
	if item.Schema > Schema {
		return Item{}, errors.New("standing: " + filepath.Base(path) + " was written by a newer aforge")
	}
	if item.ID == "" {
		item.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	return item, nil
}

func (s *Store) write(item Item) error {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	return s.underItemLock(item.ID, func() error { return s.writeUnlocked(item) })
}

// writeUnlocked is used only under the item's lock, including read-modify-write.
func (s *Store) writeUnlocked(item Item) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.ItemPath(item.ID), append(data, '\n'))
}

// underItemLock holds the item's own flock for the length of one write. It
// BLOCKS rather than refusing: the contention is between a person's window and
// a tick that are both about to finish in microseconds, and a refusal there
// would lose an edit for no reason a person could understand.
func (s *Store) underItemLock(id string, write func() error) error {
	return underLock(s.itemLockPath(id), write)
}

// underLock holds the flock on the file at path, made if it is missing, for the
// length of act, and blocks until it can. It is the one way this package takes
// a lock on a record — an item's document or a report path's receipt.
//
// A PATH'S LOCK IS TAKEN BEFORE AN ITEM'S AND NEVER AFTER ONE. A publication
// holds its report path's lock and asks the item's stop inside it
// ([Store.AtReport], [Store.UnlessStopped]); nothing holding an item's lock
// reaches for a path's, so the two orders can never meet.
func underLock(path string, act func() error) error {
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(lock) }()
	return act()
}

// itemLockPath deliberately does NOT end in .json, so List never meets it.
func (s *Store) itemLockPath(id string) string { return filepath.Join(s.root, id+".lock") }

// takeTickLock is the one-ticker-at-a-time flock. It never waits: a pass held
// by a live window is a pass that is already happening, and the timer's answer
// to that is to go back to sleep.
func (s *Store) takeTickLock() (func(), error) {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(s.LockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(lock, true, true); err != nil {
		lock.Close()
		if filelock.IsBusy(err) {
			return nil, ErrHeld
		}
		return nil, err
	}
	return func() {
		_ = filelock.Unlock(lock)
		_ = lock.Close()
	}, nil
}

// writeAtomic replaces path whole: a unique temporary file in the same
// folder, synced, renamed over it, and the folder synced after (the scale
// audit's law L4). The syncs are what make "written" survive a power cut: a
// rename that reached the disk ahead of its file's bytes comes back as an
// empty document, and a run counter that comes back older than the folders
// it numbered would hand a number out twice.
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
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// syncDir makes a rename inside dir durable, and answers whether it did.
//
// A FAILED SYNC IS A FAILED WRITE (the third review, B4). It used to be read as
// success whatever it said, so a run counter could come back from a power cut
// older than the folders it numbered — the exact thing the counter's own sync
// exists to prevent. An I/O error is returned now, and the caller's write
// fails with it. A filesystem that cannot sync a folder at all answers the
// call as unsupported, and has nothing more to offer: the rename stands.
func syncDir(dir string) error {
	folder, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer folder.Close()
	if err := syncFolder(folder); err != nil && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOTSUP) {
		return err
	}
	return nil
}

// syncFolder is the one call that syncs a folder. It is a variable so a test
// can make the disk refuse.
var syncFolder = func(folder *os.File) error { return folder.Sync() }

// newID is 16 random hex characters, the same shape and the same reasoning as a
// session id: enough to name every item a machine will ever hold with nobody
// coordinating.
// NewItemID mints an item id before the item is written, for a door that must
// bind something to the item first: a chat card places its work in a folder
// BEFORE the item exists, so there is never a moment when the work stands
// outside the rules the person agreed to ([Store.Create] keeps an id it is
// handed). A binding left by a crash between the two names an item that never
// existed, and governs nothing.
func NewItemID() string { return newID() }

func newID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

// checkID refuses anything that could name a file outside the store. Ids are
// minted here, but they arrive back through JSON documents and through callers,
// and a path separator in one of them would be a write anywhere on the disk.
func checkID(id string) error {
	if id == "" {
		return errors.New("standing: an item with no id")
	}
	for _, letter := range id {
		switch {
		case letter >= '0' && letter <= '9',
			letter >= 'a' && letter <= 'z',
			letter >= 'A' && letter <= 'Z',
			letter == '-', letter == '_':
		default:
			return errors.New("standing: an id that is not a name: " + id)
		}
	}
	return nil
}

// oneLine keeps a log line a line: whatever a sentinel or a failure had to say,
// flattened, so the log can be read with tail.
func oneLine(text string) string {
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.TrimSpace(text)
}
