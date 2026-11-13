package serve

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/fsnotify/fsnotify"
)

// A store write is a temp file, a rename, and a lock file beside the
// document. Those are one change to a client, so the watch batches them
// within changeDebounce before it looks.
// The manual's event-stream section states this duration and changePoll.
const changeDebounce = 100 * time.Millisecond

// changePoll is the fallback when the filesystem watch is missing or silent.
// The poll asks the same question the watch does — did the root change —
// and sends a tick only when the answer is yes. While the watch is
// installed, that question is the directory's own mtime: a rename changes
// it, and a quiet floor is not read file by file. Without a watch, each
// tick reads the files, so a missed notification still arrives inside one
// second.
const changePoll = 500 * time.Millisecond

// changePollWatched is the same look with a live filesystem watch (inotify,
// kqueue), which hears every write already: it only covers a watch that went
// silent without saying so. A watch that reports an error (an overflowed
// queue) is looked behind at once.
//
// THE WATCHED ROOT IS NOT RE-READ EVERY TWO SECONDS. The look lists the root
// and stats every job in it; with 86 jobs that was five per cent of the
// eighth live round's idle engine on macOS, where a stat is dear, for an
// answer the watch had already given.
const changePollWatched = 30 * time.Second

// rootWatch tells subscribers when the factory root's files change.
type rootWatch struct {
	root   string
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	fs     *fsnotify.Watcher
	hub    *hub
	// floor hears only changes a worker's look reads ([touchesFloor]).
	floor *hub
	kick  chan struct{}
	due   chan struct{}
	timer *time.Timer
	snap  fileSnap
	// leases is the lease document's [lease.RenewalShape] as the floor last
	// heard it, so a rewrite that only renewed a hold wakes no pull.
	leases   floorLeases
	healthy  atomic.Bool
	full     bool
	rootMark int64
	seenRoot bool
	looks    atomic.Int64
}

// floorLeases is one reading of the lease document for the floor.
type floorLeases struct {
	shape string
	ok    bool
}

func readFloorLeases(root string) floorLeases {
	data, err := os.ReadFile(filepath.Join(root, lease.File))
	if err != nil {
		return floorLeases{ok: os.IsNotExist(err)}
	}
	shape, ok := lease.RenewalShape(data)
	return floorLeases{shape: shape, ok: ok}
}

// newFSWatcher opens the filesystem watch; tests stand in a host without one.
var newFSWatcher = fsnotify.NewWatcher

// watchWarned says the missing watch once per process.
var watchWarned sync.Once

func watchRoot(root string) *rootWatch {
	ctx, cancel := context.WithCancel(context.Background())
	w := &rootWatch{
		root:   root,
		ctx:    ctx,
		cancel: cancel,
		hub:    newHub(),
		floor:  newHub(),
		kick:   make(chan struct{}, 1),
		due:    make(chan struct{}, 1),
		snap:   snapshot(root),
		leases: readFloorLeases(root),
	}
	fs, err := newFSWatcher()
	if err == nil {
		if err = fs.Add(root); err != nil {
			_ = fs.Close()
		} else {
			w.fs = fs
			w.healthy.Store(true)
			w.healthy.Store(true)
			go w.readFS()
		}
	}
	if err != nil {
		watchWarned.Do(func() {
			log.Printf("factory: no filesystem watch on %s (%s); open pulls look once a second instead", root, watchCause(err))
		})
	}
	w.noteRoot()
	go w.loop()
	return w
}

// watchCause names why a watch could not be made. The inotify pool is per
// user and shared by every process, so a busy machine runs out of it.
func watchCause(err error) string {
	switch {
	case errors.Is(err, syscall.EMFILE):
		return "inotify instances exhausted: raise fs.inotify.max_user_instances"
	case errors.Is(err, syscall.ENOSPC):
		return "inotify watches exhausted: raise fs.inotify.max_user_watches"
	}
	return err.Error()
}

func (w *rootWatch) stop() {
	w.once.Do(func() {
		w.cancel()
		if w.fs != nil {
			_ = w.fs.Close()
		}
	})
}

func (w *rootWatch) readFS() {
	// A failed stream no longer proves that in-place writes are announced.
	// Keep the pointer for shutdown, but restore stat polling immediately.
	defer func() { w.healthy.Store(false); w.nudge() }()
	for {
		select {
		case <-w.ctx.Done():
			return
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			// A heartbeat rewrites runners.json. That is not a job, and
			// reading the whole floor to confirm it would be the idle cost.
			if !w.worthEvent(ev.Name) {
				continue
			}
			w.nudge()
		case <-w.fs.Errors:
			return
		}
	}
}

func (w *rootWatch) nudge() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// pollEvery is how often the loop looks without being told: rarely behind a
// live watch, often without one.
func (w *rootWatch) pollEvery() time.Duration {
	if w.healthy.Load() {
		return changePollWatched
	}
	return changePoll
}

func (w *rootWatch) loop() {
	tick := time.NewTicker(w.pollEvery())
	defer tick.Stop()
	defer w.stopTimer()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.kick:
			tick.Reset(w.pollEvery())
			w.full = true
			w.arm()
		case <-tick.C:
			w.arm()
		case <-w.due:
			w.publish()
		}
	}
}

// arm bounds a burst to one debounce window. Continuous worker writes must
// not keep an open page waiting indefinitely for the filesystem to go quiet.
func (w *rootWatch) arm() {
	if w.timer != nil {
		return
	}
	w.timer = time.AfterFunc(changeDebounce, func() {
		select {
		case w.due <- struct{}{}:
		default:
		}
	})
}

func (w *rootWatch) stopTimer() {
	if w.timer != nil {
		w.timer.Stop()
	}
}

func (w *rootWatch) publish() {
	w.timer = nil
	full := w.full
	w.full = false
	// The fallback tick, while notifications are working, stats the
	// directory once. A rename moves that mtime; a quiet floor does not,
	// so the tick does not read every job file.
	if !full && w.healthy.Load() && !w.rootMoved() {
		return
	}
	w.looks.Add(1)
	next := snapshot(w.root)
	w.noteRoot()
	if next.sig == w.snap.sig {
		return
	}
	data := changedData(w.snap.files, next.files)
	if w.floorMoved(next) {
		floorWakes.Add(1)
		w.floor.send(data)
	}
	w.snap = next
	w.hub.send(data)
}

// floorWakes counts the times the open pulls were woken, so a test can say a
// renewal woke none.
var floorWakes atomic.Int64

// floorMoved says whether a change reached anything a pull's look reads, and
// keeps the lease document's shape for the next change to compare with.
//
// A RENEWAL IS NOT A CHANGE OF THE FLOOR. Every held job's runner rewrites
// the lease document every two seconds to say it is still there, and every
// open pull woke on each rewrite and looked again: fifteen slots, two holds
// and 86 jobs kept the seventh live run's idle engine at a fifth of a core,
// re-reading a floor on which nothing had changed. A rewrite whose only
// difference is when its holds were heard is answered here, once, for every
// pull. The runner registry, the live record, the sources' poll record, the
// event log and every folder are no input of a look either.
func (w *rootWatch) floorMoved(next fileSnap) bool {
	if touchesFloor(w.snap, next) {
		if next.files[lease.File] != w.snap.files[lease.File] {
			w.leases = readFloorLeases(w.root)
		}
		return true
	}
	if next.files[lease.File] == w.snap.files[lease.File] {
		return false
	}
	was := w.leases
	w.leases = readFloorLeases(w.root)
	return !was.ok || !w.leases.ok || was.shape != w.leases.shape
}

// touchesFloor says whether a change reached a file a look reads, the lease
// document aside ([rootWatch.floorMoved] compares its shape).
func touchesFloor(before, after fileSnap) bool {
	return changedBeyond(before, after) || changedBeyond(after, before)
}

// changedBeyond says whether a file of from that a look reads is missing from
// to or differs there.
func changedBeyond(from, to fileSnap) bool {
	for name, sig := range from.files {
		if !from.floorInput(name) || name == lease.File {
			continue
		}
		if other, ok := to.files[name]; !ok || other != sig {
			return true
		}
	}
	return false
}

// floorInput says whether a look reads name: a document under the root, not
// a folder (a runner's holds and reports live in folders no look opens) and
// not the bookkeeping ([lookBookkeeping]).
func (s fileSnap) floorInput(name string) bool {
	return !s.dirs[name] && !lookBookkeeping(name)
}

// lookBookkeeping is a file under the root that no look reads: the runner
// registry, the live record, the sources' poll record, which the intake
// rewrites on every poll whether or not a job arrived, and the event log.
func lookBookkeeping(name string) bool {
	return name == registryFile || name == live.File || name == sourcesFile || name == eventsFile
}

// eventsFile is the factory's event log ([record.Record]), appended to and
// never read by a look.
const eventsFile = "events.jsonl"

// sourcesFile is the sources' poll record ([store.Store.MetaPath]).
const sourcesFile = "sources.json"

// fileSnap is one look at the root. sig is the whole look, so a second look
// that matches sends nothing; files is how that look names a single job.
type fileSnap struct {
	sig   string
	files map[string]string
	// dirs names the entries of files that are folders.
	dirs map[string]bool
}

func snapshot(root string) fileSnap {
	entries, err := os.ReadDir(root)
	files, dirs := map[string]string{}, map[string]bool{}
	if err != nil {
		return fileSnap{files: files, dirs: dirs}
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !worthWatching(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files[name] = store.FileStamp(info).String()
		if info.IsDir() {
			dirs[name] = true
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte(' ')
		b.WriteString(files[name])
		b.WriteByte('\n')
	}
	return fileSnap{sig: b.String(), files: files, dirs: dirs}
}

func (w *rootWatch) noteRoot() {
	info, err := os.Stat(w.root)
	if err != nil {
		w.seenRoot = false
		return
	}
	w.rootMark = info.ModTime().UnixNano()
	w.seenRoot = true
}

func (w *rootWatch) rootMoved() bool {
	info, err := os.Stat(w.root)
	if err != nil {
		return true
	}
	mark := info.ModTime().UnixNano()
	return !w.seenRoot || mark != w.rootMark
}

func (w *rootWatch) worthEvent(name string) bool {
	base := filepath.Base(name)
	if base == filepath.Base(w.root) {
		return true
	}
	return worthWatching(base)
}

// worthWatching drops the store's own scratch. A document is written as a
// dotfile and renamed, and a flock sits beside it; neither is a change a
// client re-reads. runners.json is presence: an idle heartbeat rewrites it,
// and treating that as a job change wakes every pull.
func worthWatching(name string) bool {
	return name != "" && name != "runners.json" && !strings.HasPrefix(name, ".") && !strings.HasSuffix(name, ".lock")
}

// changedData is the tick body: one job id when a single job file is the
// whole difference, otherwise * so the client re-reads the floor.
func changedData(before, after map[string]string) string {
	var id string
	n := 0
	note := func(name string) {
		n++
		if n > 1 {
			return
		}
		job, ok := jobFile(name)
		if !ok {
			id = "*"
			return
		}
		id = job
	}
	for name, sig := range after {
		if before[name] != sig {
			note(name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			note(name)
		}
	}
	if n == 1 {
		return id
	}
	return "*"
}

func jobFile(name string) (string, bool) {
	id, ok := strings.CutSuffix(name, ".json")
	if !ok || id == "" || id[0] == '0' {
		return "", false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return id, true
}

// hub is the set of open event streams. A subscriber that has not read its
// last tick keeps that tick: one unread changed is enough to re-read, and
// blocking the watch on a slow client would stall every other client.
type hub struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func newHub() *hub {
	return &hub{subs: map[chan string]struct{}{}}
}

func (h *hub) join() chan string {
	ch := make(chan string, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) leave(ch chan string) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *hub) send(data string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- data:
		default:
		}
	}
}
