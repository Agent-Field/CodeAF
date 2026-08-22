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

	"golang.org/x/sys/unix"
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
	item.Schema = Schema
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

// Save rewrites one item's document, temp+rename under its flock, and stamps
// Updated. It validates first.
func (s *Store) Save(item Item) error {
	if err := item.Validate(); err != nil {
		return err
	}
	if err := checkID(item.ID); err != nil {
		return err
	}
	item.Schema = Schema
	item.Updated = s.now()
	return s.write(item)
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
		return Item{}, errors.New("standing: " + filepath.Base(path) + " was written by a newer aforge")
	}
	if item.ID == "" {
		item.ID = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	return item, nil
}

func (s *Store) write(item Item) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	return s.underItemLock(item.ID, func() error {
		return writeAtomic(s.ItemPath(item.ID), data)
	})
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
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = unix.Flock(int(lock.Fd()), unix.LOCK_UN) }()
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
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrHeld
		}
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
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
