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
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"

	"github.com/Agent-Field/codeaf/internal/effort"
)

// Create validates, assigns an id when there is none, stamps Created, Updated
// and Status active, writes the document, and answers the item as written.
// IT IS ONLY CALLED AFTER A YES.
func (s *Store) Create(item Item) (Item, error) {
	if err := item.Validate(); err != nil {
		return Item{}, err
	}
	now := s.now()
	if strings.TrimSpace(item.ID) == "" {
		item.ID = newID()
	}
	if err := checkID(item.ID); err != nil {
		return Item{}, err
	}
	item.Status = StatusActive
	item.RetiredWhy = ""
	item.Created = now
	item.Updated = now
	item.Revision = 1
	item.Schema = SchemaOf(item)
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

// Save rewrites one item's document, temp+rename under its flock, and stamps
// Updated. It validates first.
//
// IT BUMPS [Item.Revision] FROM THE DOCUMENT ON DISK, read inside the same lock
// as the write. That is what makes a person's edit, pause or resume visible to
// a pass that is holding a copy from before it ([Store.saveActive] refuses to
// write over a revision it did not read).
func (s *Store) Save(item Item) error {
	if err := item.Validate(); err != nil {
		return err
	}
	if err := checkID(item.ID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	return s.underItemLock(item.ID, func() error {
		current, err := s.read(s.ItemPath(item.ID))
		switch {
		case err == nil:
			item.Revision = current.Revision + 1
		case errors.Is(err, ErrNotFound):
			item.Revision = 1
		default:
			return err
		}
		item.Updated = s.now()
		data, err := marshalItem(item)
		if err != nil {
			return err
		}
		return writeAtomic(s.ItemPath(item.ID), data)
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
	if err := checkID(id); err != nil {
		return ErrNotFound
	}
	item, err := s.read(s.ItemPath(id))
	if err != nil {
		return err
	}
	item.Does.Effort = rung.String()
	item.Updated = s.now()
	return s.write(item)
}

// saveActive is the ticker's guarded write. It refuses in the three cases where
// a whole-document write from a copy read at the top of a walk would do harm:
//
//   - THE ITEM IS GONE. A save must not resurrect a document somebody deleted.
//   - THE ITEM CHANGED. A probe can take minutes; a pause, a stop, an edit or a
//     resume landing during it is newer than the copy in hand, and
//     [Item.Revision] is how that is seen. A status-only check is not enough:
//     pause-then-resume leaves the status active, and only the revision number
//     remembers that a person acted.
//   - THE ITEM IS NO LONGER ACTIVE. A paused or retired item is not written to
//     by a pass at all.
//
// On success it advances item's [Item.Revision] to match the document it just
// wrote, so a caller that writes twice in one firing (the intent, then the
// result) stays in step with disk. It answers [errConsentChanged] when it
// refuses, which the ticker reads as "the person's act wins", not as a failure.
func (s *Store) saveActive(item *Item) error {
	if err := item.Validate(); err != nil {
		return err
	}
	if err := checkID(item.ID); err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	return s.underItemLock(item.ID, func() error {
		current, err := s.read(s.ItemPath(item.ID))
		if errors.Is(err, ErrNotFound) {
			return errConsentChanged
		}
		if err != nil {
			return err
		}
		if current.Status != StatusActive || current.Revision != item.Revision {
			return errConsentChanged
		}
		item.Revision = current.Revision + 1
		item.Updated = s.now()
		data, err := marshalItem(*item)
		if err != nil {
			return err
		}
		return writeAtomic(s.ItemPath(item.ID), data)
	})
}

// Arm captures a WhenFile item's baseline NOW, at ratification, and writes it,
// so the reading the first tick compares against is the world as it stood when
// the person said yes rather than the world five minutes later.
//
// IT IS A SEPARATE CALL AND NOT A LINE IN [Store.Create] ON PURPOSE. Create is
// passive: it validates a document and writes it, and nothing about a passive
// write may touch the filesystem the item points at or run a probe. Arming is
// the ratifier's act — the session lane calls it after the card is answered —
// and it is what makes "a change between the card and the first wake" a change
// rather than the baseline. An item that is never armed keeps the old, silent
// first-reading semantics exactly.
//
// IT READS AND WRITES UNDER THE ITEM'S OWN LOCK, so an edit that lands while
// the scan runs is not overwritten by the whole document this call holds: the
// document written is the one on disk with only the fingerprint set. A scan
// that could not read everything it matched ([fingerprint]'s truncated answer)
// sets NO baseline and answers [errBaselineIncomplete] — a partial reading
// passed off as the baseline would make "nothing has changed" a lie.
func (s *Store) Arm(id string) (Item, error) {
	if err := checkID(id); err != nil {
		return Item{}, ErrNotFound
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return Item{}, err
	}
	var out Item
	err := s.underItemLock(id, func() error {
		current, err := s.read(s.ItemPath(id))
		if err != nil {
			return err
		}
		if current.Status != StatusActive {
			return errConsentChanged
		}
		if current.When.Kind != WhenFile {
			out = current
			return nil
		}
		digest, _, truncated, err := fingerprint(current.Workspace, current.When.Glob)
		if err != nil {
			return err
		}
		if truncated {
			return ErrBaselineIncomplete
		}
		current.Fingerprint = digest
		current.Revision++
		current.Schema = SchemaOf(current)
		current.Updated = s.now()
		data, err := marshalItem(current)
		if err != nil {
			return err
		}
		if err := writeAtomic(s.ItemPath(id), data); err != nil {
			return err
		}
		out = current
		return nil
	})
	return out, err
}

// NoteNeedsPerson makes a line visible on an item that has none, under the
// item's own lock, and answers the item as written.
//
// IT NEVER OVERWRITES A QUESTION. A firing's own question is the item's words
// put to the person; a ratifier that could not establish a file watch's baseline
// is a lesser fact and must not replace it. An empty field is filled and the
// document re-marshalled; the revision is bumped because a person's later edit
// must still be seen as newer than a pass holding a copy from before.
func (s *Store) NoteNeedsPerson(id, note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}
	if err := checkID(id); err != nil {
		return ErrNotFound
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	return s.underItemLock(id, func() error {
		current, err := s.read(s.ItemPath(id))
		if err != nil {
			return err
		}
		if strings.TrimSpace(current.NeedsPerson) != "" {
			return nil
		}
		current.NeedsPerson = note
		current.Revision++
		current.Updated = s.now()
		data, err := marshalItem(current)
		if err != nil {
			return err
		}
		return writeAtomic(s.ItemPath(id), data)
	})
}

// NoteSpend adds a judged check's cost to the item's lifetime figure, under the
// item's own lock, without touching anything else on the document.
//
// WHY IT EXISTS BESIDE THE LEDGER. The daily rail is read from the LEDGER, so a
// paid judgment that could not be decided already counts against the day. The
// item's own lifetime figure is read from [Item.SpentUSD] and a card quotes it,
// and an undecided check writes NOTHING on the item ([Ticker.one]'s
// stateUndecided) — so before this door the item's figure silently lost every
// refusal, timeout and ambiguity that was still billed. Adding it HERE, without
// NextDue, Fingerprint, NeedsPerson, LastChecked or a status change, keeps the
// undecided look from consuming the opportunity while still recording what it
// cost. A failed write is returned, so the pass counts it rather than losing it
// in silence.
func (s *Store) NoteSpend(id string, usd float64) error {
	if usd <= 0 {
		return nil
	}
	if err := checkID(id); err != nil {
		return ErrNotFound
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	return s.underItemLock(id, func() error {
		current, err := s.read(s.ItemPath(id))
		if err != nil {
			return err
		}
		current.SpentUSD += usd
		current.Revision++
		current.Updated = s.now()
		data, err := marshalItem(current)
		if err != nil {
			return err
		}
		return writeAtomic(s.ItemPath(id), data)
	})
}

// errConsentChanged is the ticker's answer when a guarded write ([saveActive])
// found that the person paused, stopped, resumed or edited the item while a
// pass had it in its hands. It is not a failure: it is the person's act winning.
var errConsentChanged = errors.New("standing: the item was changed while it was being worked on")

// ErrBaselineIncomplete is [Store.Arm] refusing to invent an [Item.Fingerprint]
// from a scan that could not read everything it matched. It is exported because
// the RATIFIER must see it: the session lane calls Arm when the card is answered
// and turns this refusal into a visible [Item.NeedsPerson] rather than letting a
// truncated scan pass as a baseline nobody was told was not set.
var ErrBaselineIncomplete = errors.New("standing: the baseline scan could not read everything it matched, so no baseline was set")

// errBaselineIncomplete is kept as an in-package spelling for the tests and the
// store's own reading, so there is still exactly one error value.
var errBaselineIncomplete = ErrBaselineIncomplete

// markPendingOrphan records on a non-active item that an authorized delivery is
// still waiting, so a paused, retired or expired item's unresolved line is
// visible rather than silently forgotten. It reads the document UNDER THE LOCK
// and writes it back whole, so a person's concurrent edit is not clobbered, and
// it never overwrites a question the item already carries.
func (s *Store) markPendingOrphan(id, note string) error {
	if err := checkID(id); err != nil {
		return ErrNotFound
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	return s.underItemLock(id, func() error {
		current, err := s.read(s.ItemPath(id))
		if err != nil {
			return err
		}
		if strings.TrimSpace(current.NeedsPerson) == "" {
			current.NeedsPerson = note
		}
		current.Revision++
		current.Schema = SchemaOf(current)
		current.Updated = s.now()
		data, err := marshalItem(current)
		if err != nil {
			return err
		}
		return writeAtomic(s.ItemPath(id), data)
	})
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
	items, err := s.List()
	if err != nil {
		return nil, err
	}
	kept := items[:0]
	for _, item := range items {
		if item.Status != StatusActive || !item.AppliesTo(workspace, sessionID) {
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

// newRunDir makes the next numbered folder under the item's runs/, zero-padded
// so the folders sort the way they happened.
func (s *Store) newRunDir(id string) (string, error) {
	runs := s.RunsDir(id)
	if err := os.MkdirAll(runs, 0o700); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(runs)
	if err != nil {
		return "", err
	}
	highest := 0
	for _, entry := range entries {
		number := 0
		if _, err := fmt.Sscanf(entry.Name(), "%d", &number); err != nil {
			continue
		}
		if number > highest {
			highest = number
		}
	}
	// A folder that already exists is a run somebody made between the read and
	// the make, so the next number is tried rather than trampled.
	for attempt := highest + 1; attempt <= highest+64; attempt++ {
		path := filepath.Join(runs, fmt.Sprintf("%04d", attempt))
		if err := os.Mkdir(path, 0o700); err == nil {
			return path, nil
		} else if !os.IsExist(err) {
			return "", err
		}
	}
	return "", errors.New("standing: cannot make a run folder")
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
		return Item{}, errors.New("standing: " + filepath.Base(path) + " was written by a newer codeaf")
	}
	if item.ID == "" {
		item.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	return item, nil
}

func (s *Store) write(item Item) error {
	data, err := marshalItem(item)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	return s.underItemLock(item.ID, func() error {
		return writeAtomic(s.ItemPath(item.ID), data)
	})
}

// marshalItem is the one place an item becomes bytes on disk, so the append-only
// writers and the whole-document writers cannot disagree about the shape.
func marshalItem(item Item) ([]byte, error) {
	// THE VERSION IS CHOSEN HERE, at the last moment before the bytes exist, so
	// it reflects the pending, fingerprint and revision this very write mints
	// rather than whatever the caller happened to hold when it started.
	item.Schema = SchemaOf(item)
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// underItemLock holds the item's own flock for the length of one write. It
// BLOCKS rather than refusing: the contention is between a person's window and
// a tick that are both about to finish in microseconds, and a refusal there
// would lose an edit for no reason a person could understand.
func (s *Store) underItemLock(id string, write func() error) error {
	lock, err := os.OpenFile(s.itemLockPath(id), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(lock) }()
	return write()
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
	// THE RENAME MUST NOT PUBLISH BYTES STILL IN THE PAGE CACHE: a crash after
	// the rename but before the write reaches disk would leave an empty or torn
	// document behind the name. Sync before Close, and report a Sync failure.
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// newID is 16 random hex characters, the same shape and the same reasoning as a
// session id: enough to name every item a machine will ever hold with nobody
// coordinating.
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
