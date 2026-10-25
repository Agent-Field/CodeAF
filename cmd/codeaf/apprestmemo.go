package main

import (
	"container/list"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/cachedir"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/filememo"
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
	if stamp == "" {
		return nil, false
	}
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
func (m *appRestMemo) put(agent, stamp string, tasks []appwire.Task) bool {
	size := appRestEntrySize(agent, stamp, tasks)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries, m.order = map[string]*list.Element{}, list.New()
	}
	if el, ok := m.entries[agent]; ok {
		m.dropLocked(el)
	}
	if stamp == "" || size > appRestMemoBytes {
		return false
	}
	m.entries[agent] = m.order.PushFront(&appRestEntry{agent: agent, stamp: stamp, tasks: append([]appwire.Task{}, tasks...), size: size})
	m.bytes += size
	for m.bytes > appRestMemoBytes {
		m.dropLocked(m.order.Back())
	}
	return true
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

// appTasksSize estimates retained Go memory, not its JSON encoding. It counts
// struct and slice storage (including spare capacity), pointed-to values and
// string bytes recursively. Aliases are counted twice, conservatively; allocator
// rounding and map buckets are covered by the entry allowance below.
func appTasksSize(tasks []appwire.Task) int {
	return int(unsafe.Sizeof(tasks)) + appReferencedSize(reflect.ValueOf(tasks))
}

func appReferencedSize(v reflect.Value) int {
	if v.Type() == reflect.TypeFor[time.Time]() {
		return 0
	}
	switch v.Kind() {
	case reflect.String:
		return v.Len()
	case reflect.Pointer:
		if !v.IsNil() {
			return int(v.Type().Elem().Size()) + appReferencedSize(v.Elem())
		}
	case reflect.Slice:
		size := v.Cap() * int(v.Type().Elem().Size())
		for i := 0; i < v.Len(); i++ {
			size += appReferencedSize(v.Index(i))
		}
		return size
	case reflect.Struct:
		size := 0
		for i := 0; i < v.NumField(); i++ {
			size += appReferencedSize(v.Field(i))
		}
		return size
	}
	return 0
}

func appRestEntrySize(agent, stamp string, tasks []appwire.Task) int {
	// Entry, LRU element, map key/value and conservative bucket/allocator space.
	return appTasksSize(tasks) + len(agent) + len(stamp) + int(unsafe.Sizeof(appRestEntry{})) + int(unsafe.Sizeof(list.Element{})) + 128
}

// Two seconds covers coarse filesystem timestamp ticks. A recent or future
// mtime is racy-clean even if both stat calls return the same stamp.
const appRestSettle = filememo.TimestampSettle

// appRestFiles includes the journal, checkpoints and plan stores used by the
// owner-backed task view. Task and process directories include their contents:
// changing an existing child does not change the directory's own timestamp.
var appRestFiles = []string{
	"presence.json", "tasks.json", "state.json", "meta.json", "session.json", "told.json", "team.json",
	"plandb.db", "plandb.db-wal", "tasks", "processes",
}

// restStamp changes whenever a file a member's task list is built from does:
// the folder's own files, the project's task index and the spend cap.
func (b *appBackend) restStamp(a *appAgent) string {
	var transcript string
	b.locked(func() { transcript = a.transcript })
	root, folder := session.FolderOf(transcript)
	// Flat legacy journals can reference checkpoints and node journals outside
	// their own folder. Keep their reads bounded without certifying partial inputs.
	if !folder {
		return ""
	}
	dirs := []string{filepath.Dir(transcript)}
	if root != dirs[0] {
		dirs = append(dirs, root)
	}
	paths := []string{transcript, config.BudgetConfigPath(b.crewProfile()), session.TaskIndexPath(transcript)}
	for _, dir := range dirs {
		files, ok := appRestPaths(dir)
		if !ok {
			return ""
		}
		paths = append(paths, files...)
	}
	var stamp strings.Builder
	now := b.now()
	for _, path := range paths {
		if !appRestStampPath(&stamp, path, now) {
			return ""
		}
	}
	return stamp.String()
}

// appRestPaths includes both a cell's truth folder and its session root.
// Plans and presence stay at the root while task records travel with .cell.
func appRestPaths(dir string) ([]string, bool) {
	paths := make([]string, 0, len(appRestFiles)+2)
	for _, name := range appRestFiles {
		paths = append(paths, filepath.Join(dir, name))
	}
	// Archived plans stop at the first missing suffix, as their reader does.
	for suffix := 1; ; suffix++ {
		path := filepath.Join(dir, "plandb.db."+strconv.Itoa(suffix))
		paths = append(paths, path, path+"-wal")
		if _, err := os.Stat(path); err != nil {
			return paths, os.IsNotExist(err)
		}
	}
}

// appRestStampPath uses one stat per entry for identity and racy-clean checks.
func appRestStampPath(stamp *strings.Builder, path string, now time.Time) bool {
	err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			stamp.WriteString(path + ":-;")
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return os.ErrInvalid
		}
		part := appInfoStamp(path, info)
		_, _, ctime, _ := appFileIdentity(info)
		if part == "" || !filememo.TimestampsSettled(now, info.ModTime(), time.Unix(0, ctime), appRestSettle) {
			return os.ErrInvalid
		}
		stamp.WriteString(part)
		return nil
	})
	return err == nil
}

// restingTasks answers a member at rest from its remembered task list, and
// builds the list the one time its files have changed by holding the
// conversation just long enough to read it. The answer is the owner-backed
// one either way; the conversation's journal is not kept in memory for it.
func (b *appBackend) restingTasks(a *appAgent, id string) ([]appwire.Task, error) {
	if b.restSlots != nil {
		b.restSlots <- struct{}{}
		defer func() { <-b.restSlots }()
	}
	a.gate.Lock()
	stamp := b.restStamp(a)
	live := b.liveOf(a)
	if live == nil && b.memberQuiet(a) {
		if tasks, ok := b.restMemo.get(id, stamp); ok {
			a.gate.Unlock()
			return tasks, nil
		}
		if tasks, ok := appRestRead(id, stamp); ok && b.restStamp(a) == stamp && b.restMemo.put(id, stamp, tasks) {
			a.gate.Unlock()
			return tasks, nil
		}
	}
	if _, _, err := b.heldLiveLocked(a); err != nil {
		a.gate.Unlock()
		return nil, err
	}
	endRead := b.pinReader(a)
	defer endRead()
	b.locked(func() { a.restRead = true })
	a.gate.Unlock()
	tasks, err := b.listTasksHeld(a, id)
	// Returning the last reference may close the conversation. Its writes must
	// also match the pre-open snapshot; no post-release stamp can certify rows.
	endRead()
	if err != nil {
		return nil, err
	}
	if live == nil && stamp != "" && b.restStamp(a) == stamp && b.memberRests(a) && b.restMemo.put(id, stamp, tasks) {
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

// appRestDiskBytes also bounds a corrupt cache before decoding. The decoder
// streams from a limited reader, so early syntax errors do not read the tail.
const appRestDiskBytes = appRestMemoBytes

func appRestRead(agent, stamp string) ([]appwire.Task, bool) {
	path := appRestPath(agent)
	if path == "" || stamp == "" {
		return nil, false
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false
	}
	if !info.Mode().IsRegular() || info.Size() > appRestDiskBytes {
		_ = os.Remove(path)
		return nil, false
	}
	file, err := openAppRestFile(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > appRestDiskBytes {
		_ = os.Remove(path)
		return nil, false
	}
	decoder := json.NewDecoder(io.LimitReader(file, appRestDiskBytes+1))
	tasks, ok := appRestDecode(decoder, agent, stamp)
	if !ok {
		_ = os.Remove(path)
	}
	return tasks, ok
}

// appRestDecode accepts only this build's envelope. Rows are admitted one at
// a time so a compact array of empty objects cannot expand past the Go budget.
func appRestDecode(decoder *json.Decoder, agent, stamp string) ([]appwire.Task, bool) {
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, false
	}
	var file appRestFile
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return nil, false
		}
		seen[key] = true
		if !file.readField(decoder, key, agent, stamp) {
			return nil, false
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false
	}
	return file.Tasks, seen["tasks"] && file.Build == buildinfo.Identity() && file.Stamp == stamp
}

// readField keeps the envelope's accepted fields and their admission together.
func (file *appRestFile) readField(decoder *json.Decoder, key, agent, stamp string) bool {
	switch key {
	case "build":
		return decoder.Decode(&file.Build) == nil && file.Build == buildinfo.Identity()
	case "stamp":
		return decoder.Decode(&file.Stamp) == nil && file.Stamp == stamp
	case "tasks":
		var ok bool
		file.Tasks, ok = appRestDecodeTasks(decoder, agent, stamp)
		return ok
	default:
		return false
	}
}

func appRestDecodeTasks(decoder *json.Decoder, agent, stamp string) ([]appwire.Task, bool) {
	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}
	if token == nil {
		return nil, true
	}
	if token != json.Delim('[') {
		return nil, false
	}
	var tasks []appwire.Task
	references := appRestEntrySize(agent, stamp, nil)
	rowBytes := int(unsafe.Sizeof(appwire.Task{}))
	for decoder.More() {
		var task appwire.Task
		if decoder.Decode(&task) != nil {
			return nil, false
		}
		references += appReferencedSize(reflect.ValueOf(task))
		if references+(len(tasks)+1)*rowBytes > appRestMemoBytes {
			return nil, false
		}
		if len(tasks) == cap(tasks) {
			capacity := min(max(16, cap(tasks)*2), (appRestMemoBytes-references)/rowBytes)
			rows := make([]appwire.Task, len(tasks), capacity)
			copy(rows, tasks)
			tasks = rows
		}
		if references+cap(tasks)*rowBytes > appRestMemoBytes {
			return nil, false
		}
		tasks = append(tasks, task)
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
		return nil, false
	}
	return tasks, true
}

// appRestWrite keeps a list for the next launch. A failure is silence: the
// list was answered and the cache is only a saving.
func appRestWrite(agent, stamp string, tasks []appwire.Task) {
	path := appRestPath(agent)
	if path == "" || stamp == "" || appRestEntrySize(agent, stamp, tasks) > appRestMemoBytes {
		return
	}
	raw, err := json.Marshal(appRestFile{Build: buildinfo.Identity(), Stamp: stamp, Tasks: tasks})
	if err != nil || len(raw) > appRestDiskBytes || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	appRestDisk.mu.Lock()
	defer appRestDisk.mu.Unlock()
	appRestDisk.loadLocked(time.Now())
	if appRestDisk.root != filepath.Dir(path) || appRestDisk.entries == nil {
		return
	}
	temp, err := os.CreateTemp(filepath.Dir(path), agent+".*.tmp")
	if err != nil {
		return
	}
	_, werr := temp.Write(raw)
	if cerr := temp.Close(); werr != nil || cerr != nil || fsreplace.Rename(temp.Name(), path) != nil {
		_ = os.Remove(temp.Name())
		return
	}
	if info, err := os.Stat(path); err == nil {
		appRestDisk.rememberLocked(filepath.Base(path), info)
		appRestDisk.trimLocked()
	}
}

// letRestingGo returns an idle owner only after its last reader and a fresh
// live-work check, under the same gate as every admission.
func (b *appBackend) letRestingGo(a *appAgent) bool {
	if !a.gate.TryLock() {
		return false
	}
	defer a.gate.Unlock()
	var live *appLive
	var id string
	running := false
	b.locked(func() {
		live, id = a.live, a.view.ID
		running = a.readers > 0 || live != nil && (live.running || live.compacting)
	})
	if live == nil {
		return true
	}
	if running || b.liveNeedsYou(live) || b.serviceKeeps(id) || !appLiveWorkSettled(live) {
		return false
	}
	b.locked(func() { a.crewHeld = false })
	b.release(a)
	return true
}

// appLiveWorkSettled re-reads work under the admission gate. Foreground state and
// an earlier list say nothing about a just-admitted background task or job.
func appLiveWorkSettled(live *appLive) bool {
	tasks, err := live.conn.Conv.AppTasks()
	if err != nil {
		return false
	}
	for _, task := range tasks {
		if !appTaskSettled(task.State) && task.State != "your_call" {
			return false
		}
	}
	quicks, err := live.conn.Conv.AppQuickTasks()
	if err != nil {
		return false
	}
	for _, quick := range quicks {
		if quick.State == "running" {
			return false
		}
	}
	jobs, err := live.conn.Conv.JobNotices()
	if err != nil {
		return false
	}
	for _, job := range jobs {
		if job.State == session.JobRunning {
			return false
		}
	}
	return true
}
