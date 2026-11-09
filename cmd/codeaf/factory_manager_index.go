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

// THE MANAGER WATCH COSTS NOTHING WHILE THE FLOOR IS STILL (PERF.md, the
// idle-cost law). Every job chat asks ten times a second whether a person has
// stopped its job (session.watchJobBudget), and answering that by decoding
// every job on the floor kept an idle engine with seven hundred jobs busy on
// several cores. So which job a conversation manages is an index over the
// floor, shared by every watcher of it, and the job's answer is read again
// only when its own document changes. A person's Stop rewrites the job's
// document, so it is still seen on the first look after it lands: the watch's
// bound is its own tick, as it was.
//
// THE INDEX IS CHECKED AGAINST THE FLOOR, NOT AGAINST ITS FOLDER'S CLOCK. The
// first index trusted the folder's modification time, but the floor folder
// also holds the factory's busy, poll and runner records, which a live engine
// rewrites every second or two, so on a real machine the folder was never
// still and the index was built again on every look of every chat. Now a look
// answers from the index when no job document has been written by this
// process since it was checked (store.Written) and the check is younger than
// managerRecheck; otherwise it is checked again, which lists the folder and
// `stat`s each job but reads only a document that changed (store.Managers).
// A job written by another process is therefore seen within managerRecheck.

// managerRecheck is how long an index checked against the floor answers for
// writes another process makes. This process's own writes are seen on the
// next look whatever its age.
const managerRecheck = time.Second

// managerClock is the index's now; a test holds it still.
var managerClock = time.Now

// managerIndexes holds one index per floor folder, shared by every door over
// it, since each chat opens a door of its own.
var managerIndexes sync.Map // root -> *managerIndex

type managerIndex struct {
	mu sync.Mutex
	// checked is when the index was last checked against the floor, and
	// written is the floor's store.Written count as that check began; a zero
	// checked is an index that must be checked before it answers.
	checked time.Time
	written uint64
	// checks counts the times the index was checked against the floor. THE
	// IDLE-COST LAW IS WRITTEN AGAINST IT (factory_idle_cost_test.go).
	checks int
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
	now := managerClock()
	// The count is taken before the floor is looked at, so a write landing
	// during the check leaves the count newer than the index and the next
	// look checks again.
	written := st.Written()
	if !x.checked.IsZero() && written == x.written && !now.Before(x.checked) && now.Sub(x.checked) < managerRecheck {
		id, ok := x.byTalk[want]
		return id, ok, nil
	}
	x.checks++
	managers, err := st.Managers()
	if err != nil {
		x.checked = time.Time{}
		return 0, false, err
	}
	byTalk := make(map[string]int)
	for _, m := range managers {
		if strings.TrimSpace(m.Talk) == "" {
			continue
		}
		key := sameFileKey(m.Talk)
		if _, seen := byTalk[key]; !seen {
			byTalk[key] = m.ID
		}
	}
	x.byTalk, x.written, x.checked = byTalk, written, now
	id, ok := byTalk[want]
	return id, ok, nil
}

// forget drops the index, so the next look checks the floor again.
func (x *managerIndex) forget() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.checked = time.Time{}
}

// lastStop is the last answer read for job id.
func (x *managerIndex) lastStop(id int) (stopRead, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	r, ok := x.stops[id]
	return r, ok
}

// keepStop remembers r as the last answer read for job id.
func (x *managerIndex) keepStop(id int, r stopRead) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.stops == nil {
		x.stops = map[int]stopRead{}
	}
	x.stops[id] = r
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
		last, seen := x.lastStop(id)
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
			x.keepStop(id, stopRead{file: info, talk: want, refusal: refusal})
		}
		return refusal
	}
	// The document went away under the index: look again from a fresh check.
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
// renames a fresh file into place and so changes the file's identity; size and
// modification time also catch a write in place.
func sameFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
