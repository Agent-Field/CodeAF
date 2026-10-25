package main

import (
	"container/list"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/cachedir"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appRestMemoBytes bounds what the engine keeps of answers about members at
// rest. Past it the least recently read answer goes first and is rebuilt from
// the member's files on the next read.
const appRestMemoBytes = 24 << 20

// appRestReads is how many members at rest are held at once to be read. Each
// is a whole journal replayed into memory for the length of one read, so the
// peak of a view of every member is this many journals and not all of them.
const appRestReads = 2

// appRestMemo remembers an answer that only a held conversation could give,
// for as long as the files that answer was built from have not changed. It is
// what lets a member at rest stay closed: opening one replays its whole
// journal into memory, and a view of every member of every open crew asks
// each of them for the same task list over and over.
type appRestMemo struct {
	mu      sync.Mutex
	bytes   int
	order   *list.List // front is the most recently read
	entries map[string]*list.Element
}

type appRestEntry struct {
	agent string
	stamp string
	tasks []appwire.Task
	size  int
}

// get is the remembered tasks of agent when they were built from files that
// still read as stamp. The slice is the caller's own; the rows it holds are
// shared and never written after they are remembered.
func (m *appRestMemo) get(agent, stamp string) ([]appwire.Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.entries[agent]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*appRestEntry)
	if entry.stamp != stamp {
		m.dropLocked(el)
		return nil, false
	}
	m.order.MoveToFront(el)
	return append([]appwire.Task{}, entry.tasks...), true
}

// put remembers tasks under stamp. An answer larger than the whole bound is
// not kept, so one huge member cannot push every other out and be rebuilt.
func (m *appRestMemo) put(agent, stamp string, tasks []appwire.Task) {
	size := appTasksSize(tasks)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries, m.order = map[string]*list.Element{}, list.New()
	}
	if el, ok := m.entries[agent]; ok {
		m.dropLocked(el)
	}
	if size > appRestMemoBytes {
		return
	}
	m.entries[agent] = m.order.PushFront(&appRestEntry{agent: agent, stamp: stamp, tasks: append([]appwire.Task{}, tasks...), size: size})
	m.bytes += size
	for m.bytes > appRestMemoBytes {
		m.dropLocked(m.order.Back())
	}
}

func (m *appRestMemo) dropLocked(el *list.Element) {
	entry := m.order.Remove(el).(*appRestEntry)
	delete(m.entries, entry.agent)
	m.bytes -= entry.size
}

// forget drops what is remembered about agent.
func (m *appRestMemo) forget(agent string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.entries[agent]; ok {
		m.dropLocked(el)
	}
}

// appTasksSize is what tasks weigh as the wire says them, which is close
// enough to what they hold and does not depend on the layout of the structs.
func appTasksSize(tasks []appwire.Task) int {
	raw, _ := json.Marshal(tasks)
	return len(raw)
}

// appRestFiles are the files of a conversation's folder a task list is built
// from. The two folders are stamped by their own entries: only a list of
// settled tasks is remembered, and a settled task's records are not written
// again, so a new or removed entry is the only change that can matter. A name here that does not exist reads as absent, which is a stamp too.
var appRestFiles = []string{
	"transcript.jsonl", "tasks.json", "state.json", "meta.json", "told.json", "team.json",
	"plandb.db", "plandb.db-wal", "tasks", "processes",
}

// restStamp changes whenever a file a member's task list is built from does:
// the folder's own files, the project's task index and the spend cap.
func (b *appBackend) restStamp(a *appAgent) string {
	var transcript string
	b.locked(func() { transcript = a.transcript })
	dir := filepath.Dir(transcript)
	profile := b.crewProfile()
	stamp := lazyStamp(config.BudgetConfigPath(profile)) + lazyStamp(session.TaskIndexPath(transcript))
	for _, name := range appRestFiles {
		stamp += lazyStamp(filepath.Join(dir, name))
	}
	// An ended run's plan is renamed beside the live one: plandb.db.1, .2, ...
	// and the first name that is absent ends the list, as it does for the reader.
	for suffix := 1; ; suffix++ {
		archived := lazyStamp(filepath.Join(dir, "plandb.db."+strconv.Itoa(suffix)))
		stamp += archived
		if archived == "-" {
			break
		}
	}
	return stamp
}

// restingTasks answers a member at rest from its remembered task list, and
// builds the list the one time its files have changed by holding the
// conversation just long enough to read it. The answer is the owner-backed
// one either way; the conversation's journal is not kept in memory for it.
func (b *appBackend) restingTasks(a *appAgent, id string) ([]appwire.Task, error) {
	stamp := b.restStamp(a)
	if tasks, ok := b.restMemo.get(id, stamp); ok {
		return tasks, nil
	}
	if tasks, ok := appRestRead(id, stamp); ok {
		b.restMemo.put(id, stamp, tasks)
		return tasks, nil
	}
	if b.restSlots != nil {
		b.restSlots <- struct{}{}
		defer func() { <-b.restSlots }()
	}
	// A read that waited for a slot may find the answer built meanwhile.
	if tasks, ok := b.restMemo.get(id, stamp); ok {
		return tasks, nil
	}
	tasks, err := b.listTasksOf(id)
	if err != nil {
		return nil, err
	}
	if b.letRestingGo(a, tasks) {
		// The stamp is taken after the release because opening and letting go
		// write the folder; a write by someone else in this short window is
		// seen as soon as the list changes again, never later than that.
		stamp = b.restStamp(a)
		b.restMemo.put(id, stamp, tasks)
		appRestWrite(id, stamp, tasks)
	}
	return tasks, nil
}

// appRestFile is a remembered task list on disk: under the cache root, so
// deleting the cache only costs the next launch a rebuild. It is only read by
// the build that wrote it, because what a list says is that build's reading of
// the files, and only while the files still read as its stamp.
type appRestFile struct {
	Build string         `json:"build"`
	Stamp string         `json:"stamp"`
	Tasks []appwire.Task `json:"tasks"`
}

func appRestPath(agent string) string {
	if agent == "" || filepath.Base(agent) != agent {
		return ""
	}
	return filepath.Join(cachedir.Root(), "app-tasks", agent+".json")
}

func appRestRead(agent, stamp string) ([]appwire.Task, bool) {
	path := appRestPath(agent)
	if path == "" {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var file appRestFile
	if json.Unmarshal(raw, &file) != nil || file.Build != buildinfo.Identity() || file.Stamp != stamp {
		return nil, false
	}
	return file.Tasks, true
}

// appRestWrite keeps a list for the next launch. A failure is silence: the
// list was answered and the cache is only a saving.
func appRestWrite(agent, stamp string, tasks []appwire.Task) {
	path := appRestPath(agent)
	if path == "" {
		return
	}
	raw, err := json.Marshal(appRestFile{Build: buildinfo.Identity(), Stamp: stamp, Tasks: tasks})
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	temp, err := os.CreateTemp(filepath.Dir(path), agent+".*.tmp")
	if err != nil {
		return
	}
	_, werr := temp.Write(raw)
	if cerr := temp.Close(); werr != nil || cerr != nil || fsreplace.Rename(temp.Name(), path) != nil {
		_ = os.Remove(temp.Name())
	}
}

// letRestingGo gives the conversation a read opened back, when the tasks it
// answered with are all settled and nothing else has taken it since. It
// reports whether the conversation was let go, which is when the answer is a
// fact about files alone and may be remembered.
func (b *appBackend) letRestingGo(a *appAgent, tasks []appwire.Task) bool {
	for _, task := range tasks {
		switch task.State {
		case appwire.TaskDone, appwire.TaskFailed, appwire.TaskUnverified:
		default:
			return false
		}
	}
	if !a.gate.TryLock() {
		return false
	}
	defer a.gate.Unlock()
	var live *appLive
	var id string
	running := false
	b.locked(func() {
		live, id = a.live, a.view.ID
		running = live != nil && live.running
	})
	if live == nil {
		return true
	}
	if running || b.liveNeedsYou(live) || b.serviceKeeps(id) {
		return false
	}
	b.locked(func() { a.crewHeld = false })
	b.release(a)
	return true
}
