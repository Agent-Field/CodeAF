package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// factoryStopCheckDecodes counts the job documents the stop check decoded.
var factoryStopCheckDecodes atomic.Int64

// AutomaticRefusal reads the current durable job for every automatic wake.
// A person opening or reading the conversation does not change that job.
//
// IT DECODES NOTHING THAT HAS NOT CHANGED. Every job's manager asks this ten
// times a second while it is open (session's watchJobBudget), and each ask used
// to list and decode every job document on the floor: the third live run's
// engine idled at 145–188% of a core. The answer is remembered with the file
// it was read from — the job's own document, or, when the conversation manages
// no job, every job document on the floor ([floorJobFiles]) — and only a
// document that changed is read again. Every store write renames a fresh file
// into place, so a Stop is seen at the next ask. PERF.md's "Idle factory
// manager checks" states the cost.
//
// A CONVERSATION THAT MANAGES NO JOB IS NEVER KEYED ON THE FLOOR'S FOLDER.
// Every open step chat is one, and the folder's own time moves with every
// file renamed into it — the runner registry each pull touched, a lease's
// heartbeat, the sources' poll record — so every few seconds each open step
// chat listed and decoded every job on the floor again: the fourth live run's
// engine idled at 150% of a core. It reads only the job documents that
// changed, from one look at the floor that every conversation shares.
func (d seamRunDoor) AutomaticRefusal(conversation string) string {
	key := cachedFileKey(conversation)
	if key == "" || d.st == nil {
		return ""
	}
	key = d.st.Root() + "\x00" + key
	if why, ok := stopChecks.answer(key, conversation, d); ok {
		return why
	}
	// The files are stat'd before they are read, so a write landing between
	// the two leaves the newer document under the older file's identity and
	// the next ask reads it again rather than keeping a stale answer. A job
	// found by listing the floor is remembered without its file, so the next
	// ask reads it once more, by id, the same safe way.
	floor, floorErr := floorJobFiles(d.st.Root())
	hint := stopChecks.job(key)
	var job os.FileInfo
	if hint != 0 {
		job, _ = os.Stat(d.st.ItemPath(hint))
	}
	it, err := d.managedFrom(conversation, hint)
	if err == factory.ErrNotManager {
		if floorErr != nil {
			floor = nil
		}
		stopChecks.keep(key, stopCheck{jobs: floor})
		return ""
	}
	if err != nil {
		stopChecks.forget(key)
		return "job could not be read; automatic work stopped"
	}
	why := ""
	if err := factory.CheckPersonStop(it); err != nil {
		why = err.Error()
	}
	if it.ID != hint {
		job = nil
	}
	stopChecks.keep(key, stopCheck{id: it.ID, file: job, refusal: why})
	return why
}

// stopCheck is one conversation's last answer and the files it came from: the
// job's document (id), or, for a conversation that manages no job, every job
// document on the floor as it last looked at them.
type stopCheck struct {
	id      int
	file    os.FileInfo
	jobs    map[int]os.FileInfo
	refusal string
}

// floorLookEvery is how long one look at the floor's job files is shared. Every
// open conversation that manages no job asks ten times a second; one look per
// interval answers all of them, so their number costs nothing more.
const floorLookEvery = 100 * time.Millisecond

// floorNow is the clock a shared look is dated by; a test moves it.
var floorNow = time.Now

type floorLook struct {
	at   time.Time
	dir  os.FileInfo
	jobs map[int]os.FileInfo
}

var floorLooks = struct {
	mu sync.Mutex
	by map[string]floorLook
}{by: map[string]floorLook{}}

// floorJobFiles is each job document under root, by job, as one directory
// read and one stat of each saw it: no document is opened. The map is shared
// and never changed after it is made.
func floorJobFiles(root string) (map[int]os.FileInfo, error) {
	floorLooks.mu.Lock()
	defer floorLooks.mu.Unlock()
	now := floorNow()
	dir, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	// A job can gain this chat during the shared interval. Its atomic rename
	// invalidates the look immediately, before a negative memo can admit work.
	if l, ok := floorLooks.by[root]; ok && now.Sub(l.at) < floorLookEvery && sameFileInfo(l.dir, dir) {
		return l.jobs, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	jobs := make(map[int]os.FileInfo, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
		if err != nil || id <= 0 {
			continue
		}
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			continue
		}
		jobs[id] = info
	}
	floorLooks.by[root] = floorLook{at: now, dir: dir, jobs: jobs}
	return jobs, nil
}

// sameJobFiles is whether two looks saw the same job documents, unchanged.
func sameJobFiles(then, now map[int]os.FileInfo) bool {
	if len(then) != len(now) {
		return false
	}
	for id, info := range now {
		if !sameFileInfo(then[id], info) {
			return false
		}
	}
	return true
}

// fileKeys remembers each conversation's path as [sameFileKey] spells it, so
// a check ten times a second does not resolve the same links each time.
var fileKeys sync.Map

func cachedFileKey(path string) string {
	if key, ok := fileKeys.Load(path); ok {
		return key.(string)
	}
	key := sameFileKey(path)
	if key != "" {
		fileKeys.Store(path, key)
	}
	return key
}

type stopCheckMemo struct {
	mu sync.Mutex
	by map[string]stopCheck
}

var stopChecks = &stopCheckMemo{by: map[string]stopCheck{}}

// answer is the remembered answer for key while its file is unchanged. A
// conversation that manages no job is answered from the floor's job files
// ([floorJobFiles]): it reads again only a document that changed since it last
// looked, to see whether that job is now the one it manages.
func (m *stopCheckMemo) answer(key, conversation string, d seamRunDoor) (string, bool) {
	last, ok := m.last(key)
	if !ok {
		return "", false
	}
	if last.id == 0 {
		if last.jobs == nil {
			return "", false
		}
		now, err := floorJobFiles(d.st.Root())
		if err != nil {
			return "", false
		}
		if sameJobFiles(last.jobs, now) {
			return "", true
		}
		want := cachedFileKey(conversation)
		seen := make(map[int]os.FileInfo, len(now))
		for id, info := range now {
			if prev, ok := last.jobs[id]; ok && sameFileInfo(prev, info) {
				seen[id] = info
				continue
			}
			it, err := d.st.Get(id)
			factoryStopCheckDecodes.Add(1)
			if err != nil {
				// Not remembered, so the next ask reads it again.
				continue
			}
			if strings.TrimSpace(it.Talk) != "" && sameFileKey(it.Talk) == want {
				return "", false
			}
			seen[id] = info
		}
		m.keep(key, stopCheck{jobs: seen})
		return "", true
	}
	info, err := os.Stat(d.st.ItemPath(last.id))
	if err != nil || !sameFileInfo(last.file, info) {
		return "", false
	}
	return last.refusal, true
}

// last is what key's conversation was last answered, and whether it was.
func (m *stopCheckMemo) last(key string) (stopCheck, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.by[key]
	return c, ok
}

// job is the job key's conversation was last found managing, 0 for none.
func (m *stopCheckMemo) job(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.by[key].id
}

func (m *stopCheckMemo) keep(key string, c stopCheck) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.by[key] = c
}

func (m *stopCheckMemo) forget(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.by, key)
}

// sameFileInfo is whether now is still the file then was read from. Every
// store write renames a fresh file into place, so a write always shows as a
// different file; size and modification time also catch a write in place.
func sameFileInfo(then, now os.FileInfo) bool {
	return then != nil && now != nil && os.SameFile(then, now) && then.Size() == now.Size() && then.ModTime().Equal(now.ModTime())
}

// WatchedRefusal is AutomaticRefusal for the watcher that looks ten times a
// second for as long as a job's chat is open. It costs one look at the floor's
// folder, not a read of every job ([store.Store.TalkStop]). What grants work
// still asks AutomaticRefusal.
func (d seamRunDoor) WatchedRefusal(conversation string) string {
	want := sameFileKey(conversation)
	if want == "" || d.st == nil {
		return ""
	}
	manager, stopped, err := d.st.TalkStop(want)
	if err != nil {
		return "job could not be read; automatic work stopped"
	}
	if manager && stopped {
		return factory.PersonStopReason
	}
	return ""
}
