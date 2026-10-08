// Package store keeps the factory's items on disk: one JSON document per
// item, under a root the caller names, and nothing else. It is the local
// engine's memory: what a person put on the floor from the terminal, and what
// a chat put there after a yes.
//
// IT MIRRORS internal/standing/store.go IN ITS DISCIPLINE, on purpose, so a
// reader who knows one knows the other: a document is written TEMP + RENAME
// UNDER A PER-ITEM FLOCK, a document from a newer build is refused rather than
// misread, and a read-modify-write is one critical section so two windows
// changing one item cannot undo each other.
//
// THE ROOT IS GIVEN, NEVER COMPUTED HERE. The caller passes <codeaf home>/...
// in, so a test's store is a temp directory and nothing else.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

// Schema is the newest document version this build reads and the one it
// writes. Bump it when a field changes meaning; a build that meets a document
// above it refuses that document rather than guessing.
const Schema = 1

// doc is the envelope on disk: the version it was written at, how many times
// it has been written, and the item. The revision lives here rather than on
// [factory.Item] because it is a fact about the document, not about the work.
type doc struct {
	Schema   int          `json:"schema"`
	Revision int          `json:"revision"`
	Item     factory.Item `json:"item"`
}

// ErrNotFound is the answer for an id that is not here.
var ErrNotFound = errors.New("factory store: no such item")

// ErrNewer is the answer for a document a newer codeaf wrote. The document is
// left on disk untouched, so the build that understands it still can.
var ErrNewer = errors.New("factory store: written by a newer codeaf")

// Store is the factory's items under one root.
type Store struct {
	root string
	// clock is the store's now. A test may hold it still; it is nil in every
	// real build, which reads as time.Now.
	clock func() time.Time
}

// Open answers the store at root, creating the directory. It holds no handles.
func Open(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("factory store: an empty root")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

// Root is the directory the store was opened on. A nil store answers "", which
// is how a seam built over no store knows to draw nothing.
func (st *Store) Root() string {
	if st == nil {
		return ""
	}
	return st.root
}

// ItemPath is <root>/<id>.json.
func (st *Store) ItemPath(id int) string {
	return filepath.Join(st.root, strconv.Itoa(id)+".json")
}

// Create mints the next id, stamps Created and Changed when they are zero, and
// writes the item as the document's first revision. It answers the item as
// written. IT IS ONLY CALLED AFTER A YES: a person typed the words on the
// floor, or answered a chat's card. Nothing writes an item on a guess.
//
// An item that already carries an id is refused: an id is the store's to mint,
// and a caller that brings one is either replaying a document or about to
// overwrite somebody else's.
func (st *Store) Create(it factory.Item) (factory.Item, error) {
	if it.ID != 0 {
		return factory.Item{}, fmt.Errorf("factory store: a new item already has id %d", it.ID)
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return factory.Item{}, err
	}
	now := st.now()
	if it.Created.IsZero() {
		it.Created = now
	}
	if it.Changed.IsZero() {
		it.Changed = it.Created
	}
	// THE MINT AND THE FIRST WRITE ARE ONE CRITICAL SECTION. Reading the highest
	// id and writing the next one under separate locks would let two creators
	// read the same highest id and both write the next, one over the other.
	err := st.underLock(st.mintLockPath(), func() error {
		highest, err := st.highestID()
		if err != nil {
			return err
		}
		it.ID = highest + 1
		data, err := marshalDoc(doc{Revision: 1, Item: it})
		if err != nil {
			return err
		}
		return st.underItemLock(it.ID, func() error {
			return writeAtomic(st.ItemPath(it.ID), data)
		})
	})
	if err != nil {
		return factory.Item{}, err
	}
	return it, nil
}

// Add is the one door the chat's tool calls after a yes: it fills what a
// conversation does not say and creates the item. The origin is the chat
// unless the caller says otherwise; the item arrives new; the author is
// trusted as the owner unless a tier is given; and its stages are the default
// recipe for its kind when it brings none. The id comes back so the chat can
// name the row it made.
//
// THE SIGNATURE IS A CONTRACT. The tool lane holds an interface of exactly
// `Add(context.Context, factory.Item) (int, error)` against it.
func (st *Store) Add(ctx context.Context, it factory.Item) (int, error) {
	if st == nil {
		return 0, errors.New("factory store: no store")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if strings.TrimSpace(it.Title) == "" {
		return 0, errors.New("factory store: an item needs a title")
	}
	if it.Origin == "" {
		it.Origin = factory.OriginChat
	}
	if it.Tier == "" {
		it.Tier = factory.TierOwner
	}
	if it.Kind == "" {
		it.Kind = factory.KindIssue
	}
	if len(it.Stages) == 0 {
		it.Stages = factory.CopyStages(factory.DefaultRecipe().For(it.Kind))
	}
	if it.Repo != "" && len(it.Places) == 0 {
		it.Places = []string{it.Repo}
	}
	it.State = factory.StateNew
	it.ID = 0
	made, err := st.Create(it)
	if err != nil {
		return 0, err
	}
	return made.ID, nil
}

// Get reads one item. A missing id is [ErrNotFound]; a document a newer build
// wrote is [ErrNewer].
func (st *Store) Get(id int) (factory.Item, error) {
	if err := checkID(id); err != nil {
		return factory.Item{}, ErrNotFound
	}
	d, err := st.read(st.ItemPath(id))
	if err != nil {
		return factory.Item{}, err
	}
	return d.Item, nil
}

// List reads every item, newest first by Created. An unreadable or
// newer-schema document is skipped rather than fatal: ONE DOCUMENT FROM THE
// FUTURE, OR ONE TORN BY A FULL DISK, MUST NOT EMPTY THE WHOLE FLOOR.
func (st *Store) List() ([]factory.Item, error) {
	entries, err := os.ReadDir(st.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	items := make([]factory.Item, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		// ONLY A NUMBER NAMES AN ITEM. The folder also keeps the watched repos
		// and the sources' poll record (repos.go, meta.go), and those documents
		// must never be read back as a blank row on the floor.
		if _, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json")); err != nil {
			continue
		}
		d, err := st.read(filepath.Join(st.root, entry.Name()))
		if err != nil {
			continue
		}
		items = append(items, d.Item)
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].Created.Equal(items[b].Created) {
			return items[a].ID > items[b].ID
		}
		return items[a].Created.After(items[b].Created)
	})
	return items, nil
}

// Save rewrites one item's whole document, temp+rename under its flock. The
// revision is bumped FROM THE DOCUMENT ON DISK, read inside the same lock as
// the write, so two saves in a row are two revisions whoever held which copy.
// It does not stamp Changed: the caller says when the work changed, and a
// save that only re-files a document is not a change a person made.
//
// A document that is not on disk is written as revision one, which is the
// creation shape; production creates through [Store.Create], and this branch
// only keeps a whole-document write of a missing document from minting a
// wrong revision.
func (st *Store) Save(it factory.Item) error {
	if err := checkID(it.ID); err != nil {
		return err
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underItemLock(it.ID, func() error {
		rev := 1
		current, err := st.read(st.ItemPath(it.ID))
		switch {
		case err == nil:
			rev = current.Revision + 1
		case errors.Is(err, ErrNotFound):
		default:
			return err
		}
		data, err := marshalDoc(doc{Revision: rev, Item: it})
		if err != nil {
			return err
		}
		return writeAtomic(st.ItemPath(it.ID), data)
	})
}

// Update is a read-modify-write of one item under its own flock: change reads
// the document on disk and changes it in place, and the result is written as
// the next revision with Changed stamped now. A change that answers an error
// writes nothing.
//
// THE READ AND THE WRITE ARE ONE CRITICAL SECTION. A surface that read an item
// a minute ago and handed back the whole of it would undo whatever another
// window did since; a door that moves one field moves it on the document on
// disk instead.
func (st *Store) Update(id int, change func(*factory.Item) error) error {
	return st.update(id, true, change)
}

// Annotate is [Store.Update] for a write that is not a change to the work: the
// same read-modify-write under the item's flock, the same next revision, and
// Changed LEFT AS IT WAS. The cheap read made on arrival (internal/factory/
// triage) writes through it, because a row's age counts from Changed, and a
// read the factory made by itself is not the item moving: every row on a
// freshly connected floor would otherwise say `now` the minute it was read.
func (st *Store) Annotate(id int, change func(*factory.Item) error) error {
	return st.update(id, false, change)
}

func (st *Store) update(id int, stamp bool, change func(*factory.Item) error) error {
	if err := checkID(id); err != nil {
		return ErrNotFound
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underItemLock(id, func() error {
		current, err := st.read(st.ItemPath(id))
		if err != nil {
			return err
		}
		it := current.Item
		if err := change(&it); err != nil {
			return err
		}
		it.ID = id
		if stamp {
			it.Changed = st.now()
		}
		data, err := marshalDoc(doc{Revision: current.Revision + 1, Item: it})
		if err != nil {
			return err
		}
		return writeAtomic(st.ItemPath(id), data)
	})
}

// Revision is how many times one item's document has been written. It is a
// fact about the document, so it is read off the envelope and never off the
// item.
func (st *Store) Revision(id int) (int, error) {
	if err := checkID(id); err != nil {
		return 0, ErrNotFound
	}
	d, err := st.read(st.ItemPath(id))
	if err != nil {
		return 0, err
	}
	return d.Revision, nil
}

// now is the store's clock.
func (st *Store) now() time.Time {
	if st == nil || st.clock == nil {
		return time.Now()
	}
	return st.clock()
}

func (st *Store) read(path string) (doc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc{}, ErrNotFound
		}
		return doc{}, err
	}
	var d doc
	if err := json.Unmarshal(data, &d); err != nil {
		return doc{}, fmt.Errorf("factory store: cannot read %s: %w", filepath.Base(path), err)
	}
	if d.Schema > Schema {
		return doc{}, fmt.Errorf("%w: %s", ErrNewer, filepath.Base(path))
	}
	// THE FILE NAME IS THE ID. A document whose body lost its id, or never had
	// one, is still the item its name says, so a read cannot answer an item
	// that a later write would file under a different name.
	if n, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(path), ".json")); err == nil {
		d.Item.ID = n
	}
	return d, nil
}

// highestID is the largest id with a document under the root, read off the
// file names alone, so a document this build cannot read still holds its id
// and is never written over by a fresh one.
func (st *Store) highestID() (int, error) {
	entries, err := os.ReadDir(st.root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	highest := 0
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() {
			continue
		}
		if n, err := strconv.Atoi(name); err == nil && n > highest {
			highest = n
		}
	}
	return highest, nil
}

// marshalDoc is the one place a document becomes bytes on disk, and the one
// place its version is chosen.
func marshalDoc(d doc) ([]byte, error) {
	d.Schema = Schema
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// underItemLock holds the item's own flock for the length of one write. It
// BLOCKS rather than refusing: the contention is between two windows that are
// both about to finish in microseconds, and a refusal would lose an edit for no
// reason a person could understand.
func (st *Store) underItemLock(id int, write func() error) error {
	return st.underLock(filepath.Join(st.root, strconv.Itoa(id)+".lock"), write)
}

// mintLockPath is the lock every Create holds while it reads the highest id
// and writes the next. Like an item's lock it deliberately does NOT end in
// .json, so List never meets it.
func (st *Store) mintLockPath() string { return filepath.Join(st.root, "mint.lock") }

func (st *Store) underLock(path string, write func() error) error {
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
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
	// the rename but before the write reaches disk would leave a torn document
	// behind the name. Sync before Close, and report a Sync failure.
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// checkID refuses an id the store could not have minted. Ids are positive
// integers, so one can never name a file outside the root, but they arrive
// back through callers and a zero or a negative is a caller's mistake that
// must not become a file called 0.json.
func checkID(id int) error {
	if id <= 0 {
		return fmt.Errorf("factory store: %d is not an item id", id)
	}
	return nil
}
