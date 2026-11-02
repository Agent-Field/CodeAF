package serve

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/fsnotify/fsnotify"
)

// A store write is a temp file, a rename, and a lock file beside the
// document. Those are one change to a client, so the watch batches them
// within changeDebounce before it looks.
// The manual's event-stream section states this duration and changePoll.
const changeDebounce = 100 * time.Millisecond

// changePoll is the fallback when the filesystem watch is missing or silent.
// The poll asks the same question the watch does — did the root change —
// and sends a tick only when the answer is yes.
const changePoll = 2 * time.Second

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
}

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
	}
	if fs, err := fsnotify.NewWatcher(); err == nil {
		if err := fs.Add(root); err != nil {
			_ = fs.Close()
		} else {
			w.fs = fs
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
	for {
		select {
		case <-w.ctx.Done():
			return
		case _, ok := <-w.fs.Events:
			if !ok {
				return
			}
			w.nudge()
		case _, ok := <-w.fs.Errors:
			if !ok {
				return
			}
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
	next := snapshot(w.root)
	if next.sig == w.snap.sig {
		return
	}
	data := changedData(w.snap.files, next.files)
	if touchesFloor(w.snap.files, next.files) {
		w.floor.send(data)
	}
	w.snap = next
	w.hub.send(data)
}

// touchesFloor says whether a change reached a file other than the ones a
// pull's own bookkeeping writes: the runner registry, where a pull records
// its heartbeat, and the live record. Neither is read by a look, and waking
// the open pulls on them made every pull wake every other one, forever.
func touchesFloor(before, after map[string]string) bool {
	return changedBeyond(before, after) || changedBeyond(after, before)
}

// changedBeyond says whether a file of from, other than the bookkeeping, is
// missing from to or differs there.
func changedBeyond(from, to map[string]string) bool {
	for name, sig := range from {
		if name == registryFile || name == live.File {
			continue
		}
		if other, ok := to[name]; !ok || other != sig {
			return true
		}
	}
	return false
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
		files[name] = strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10)
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

// worthWatching drops the store's own scratch. A document is written as a
// dotfile and renamed, and a flock sits beside it; neither is a change a
// client re-reads.
func worthWatching(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && !strings.HasSuffix(name, ".lock")
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
