package main

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// THE MANAGER WATCH IS O(1) WHILE THE FLOOR IS STILL (PERF.md, the idle-cost
// law). Every job chat asks ten times a second whether a person has stopped its
// job (session.watchJobBudget), and answering that by decoding every job on the
// floor kept an idle engine with seven hundred jobs busy on several cores. So
// which job a conversation manages is an index over the floor, rebuilt only
// when the floor's folder changes, and the job's answer is read again only
// when its own document changes. A person's Stop rewrites the job's document,
// so it is still seen on the first look after it lands: the watch's bound is
// its own tick, as it was.

// floorSettledAfter is how old the floor folder's modification time must be
// before an index built from it is trusted. A folder's clock can be as coarse
// as two seconds, so a write landing in the same tick as the listing could
// otherwise leave the folder looking unchanged; until that window has passed
// the index is used for the look that built it and built again on the next.
const floorSettledAfter = 2 * time.Second

// managerIndexes holds one index per floor folder, shared by every door over
// it, since each chat opens a door of its own.
var managerIndexes sync.Map // root -> *managerIndex

type managerIndex struct {
	mu sync.Mutex
	// folder is the floor folder as the trusted index was built from it; nil
	// while there is no index to trust.
	folder os.FileInfo
	// byTalk is the job each manager conversation manages, keyed as
	// sameFileKey spells a conversation. The first job in the floor's own
	// order wins, as a walk of the listing would find it.
	byTalk map[string]int
	// stops is each job's person-Stop answer and the document it came from.
	stops map[int]stopRead
}

// stopRead is one job's automatic-work refusal as last read.
type stopRead struct {
	file    os.FileInfo
	talk    string
	refusal string
}

func managerIndexOf(st *store.Store) *managerIndex {
	if x, ok := managerIndexes.Load(st.Root()); ok {
		return x.(*managerIndex)
	}
	x, _ := managerIndexes.LoadOrStore(st.Root(), &managerIndex{})
	return x.(*managerIndex)
}

// managerOf is the id of the job conversation (as sameFileKey spells it)
// manages, if any.
func (x *managerIndex) managerOf(st *store.Store, want string) (int, bool, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.managerOfLocked(st, want)
}

func (x *managerIndex) managerOfLocked(st *store.Store, want string) (int, bool, error) {
	folder, err := os.Stat(st.Root())
	if err == nil && x.folder != nil && sameFile(x.folder, folder) {
		id, ok := x.byTalk[want]
		return id, ok, nil
	}
	// The folder is looked at before it is listed, so a write landing between
	// the two leaves the folder newer than the index and the next look builds
	// it again.
	listed := time.Now()
	items, listErr := st.ListSaved()
	if listErr != nil {
		x.folder = nil
		return 0, false, listErr
	}
	byTalk := make(map[string]int)
	for _, it := range items {
		if strings.TrimSpace(it.Talk) == "" {
			continue
		}
		key := sameFileKey(it.Talk)
		if _, seen := byTalk[key]; !seen {
			byTalk[key] = it.ID
		}
	}
	x.byTalk = byTalk
	x.folder = nil
	if err == nil && folder.ModTime().Before(listed.Add(-floorSettledAfter)) {
		x.folder = folder
	}
	id, ok := byTalk[want]
	return id, ok, nil
}

// forget drops the index, so the next look lists the floor again.
func (x *managerIndex) forget() {
	x.mu.Lock()
	x.folder = nil
	x.mu.Unlock()
}

// managedItem is the job want manages, read whole. A job whose document no
// longer names want (it moved between the index and the read) drops the index
// and is looked up once more from a fresh listing.
func (x *managerIndex) managedItem(st *store.Store, want string) (factory.Item, error) {
	for attempt := 0; ; attempt++ {
		id, ok, err := x.managerOf(st, want)
		if err != nil {
			return factory.Item{}, err
		}
		if !ok {
			return factory.Item{}, errNotAManager
		}
		it, err := st.Get(id)
		if err == nil && strings.TrimSpace(it.Talk) != "" && sameFileKey(it.Talk) == want {
			return it, nil
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return factory.Item{}, err
		}
		if attempt > 0 {
			return factory.Item{}, errNotAManager
		}
		x.forget()
	}
}

// automaticRefusal is AutomaticRefusal's answer for want, read from the job's
// document only when that document has changed since the last look.
func (x *managerIndex) automaticRefusal(st *store.Store, want string) string {
	id, ok, err := x.managerOf(st, want)
	if err != nil {
		return refusalUnreadable
	}
	if !ok {
		return ""
	}
	path := st.ItemPath(id)
	if info, err := os.Stat(path); err == nil {
		x.mu.Lock()
		last, seen := x.stops[id]
		x.mu.Unlock()
		if seen && last.talk == want && sameFile(last.file, info) {
			return last.refusal
		}
		// Stat before the read: a write landing between the two leaves the
		// newer document under the older file, and the next look reads again.
		it, err := x.managedItem(st, want)
		if errors.Is(err, errNotAManager) {
			return ""
		}
		if err != nil {
			return refusalUnreadable
		}
		refusal := ""
		if stop := factory.CheckPersonStop(it); stop != nil {
			refusal = stop.Error()
		}
		if it.ID == id {
			x.mu.Lock()
			if x.stops == nil {
				x.stops = map[int]stopRead{}
			}
			x.stops[id] = stopRead{file: info, talk: want, refusal: refusal}
			x.mu.Unlock()
		}
		return refusal
	}
	// The document went away under the index: look again from a fresh listing.
	it, err := x.managedItem(st, want)
	if errors.Is(err, errNotAManager) {
		return ""
	}
	if err != nil {
		return refusalUnreadable
	}
	if stop := factory.CheckPersonStop(it); stop != nil {
		return stop.Error()
	}
	return ""
}

// refusalUnreadable is the automatic-work refusal when the job cannot be read.
const refusalUnreadable = "job could not be read; automatic work stopped"

// sameFile is whether b is still the file a was taken from. Every store write
// renames a fresh file into place and so changes the folder; size and
// modification time also catch a write in place.
func sameFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
