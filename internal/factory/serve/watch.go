package serve

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

// rootWatch tells subscribers when the factory root's files change.
type rootWatch struct {
	root    string
	ctx     context.Context
	cancel  context.CancelFunc
	once    sync.Once
	fs      *fsnotify.Watcher
	healthy atomic.Bool
	hub     *hub
	kick    chan struct{}
	due     chan struct{}
	timer   *time.Timer
	snap    fileSnap
	// full is set when a filesystem event asked for a real read. The
	// fallback tick leaves it false and may then skip the read.
	full bool
	// rootMark is the directory mtime of the last full read. seenRoot is
	// false until that read managed to stat the directory.
	rootMark int64
	seenRoot bool
	// looks counts full reads of the directory. A quiet floor must not
	// add to it on the fallback tick.
	looks atomic.Int64
}

func watchRoot(root string) *rootWatch {
	ctx, cancel := context.WithCancel(context.Background())
	w := &rootWatch{
		root:   root,
		ctx:    ctx,
		cancel: cancel,
		hub:    newHub(),
		kick:   make(chan struct{}, 1),
		due:    make(chan struct{}, 1),
	}
	w.noteRoot()
	w.snap = snapshot(root)
	if fs, err := fsnotify.NewWatcher(); err == nil {
		if err := fs.Add(root); err != nil {
			_ = fs.Close()
		} else {
			w.fs = fs
			w.healthy.Store(true)
			go w.readFS()
		}
	}
	go w.loop()
	return w
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

func (w *rootWatch) loop() {
	tick := time.NewTicker(changePoll)
	defer tick.Stop()
	defer w.stopTimer()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.kick:
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
	w.snap = next
	w.hub.send(data)
}

// fileSnap is one look at the root. sig is the whole look, so a second look
// that matches sends nothing; files is how that look names a single job.
type fileSnap struct {
	sig   string
	files map[string]string
}

func snapshot(root string) fileSnap {
	entries, err := os.ReadDir(root)
	files := map[string]string{}
	if err != nil {
		return fileSnap{files: files}
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
	return fileSnap{sig: b.String(), files: files}
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
