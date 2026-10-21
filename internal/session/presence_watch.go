package session

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// presenceDoors shares one kernel watcher and two clocks among all live sessions.
// A busy or blocked session cannot hold up another session's request.
var presenceDoors presenceDoorWatch

type presenceDoorWatch struct {
	mu  sync.Mutex
	run *presenceDoorRun
}

type presenceDoorRun struct {
	watcher *fsnotify.Watcher
	stop    chan struct{}
	clients map[*presenceDoorClient]bool
	watched map[string]bool
}

type presenceDoorClient struct {
	dir       string
	requests  chan struct{}
	beats     chan time.Time
	refreshes chan time.Time
}

func (w *presenceDoorWatch) subscribe(dir string) (<-chan struct{}, <-chan time.Time, <-chan time.Time, func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.run == nil {
		watcher, _ := fsnotify.NewWatcher()
		w.run = &presenceDoorRun{watcher: watcher, stop: make(chan struct{}), clients: map[*presenceDoorClient]bool{}, watched: map[string]bool{}}
		go w.loop(w.run)
	}
	run := w.run
	c := &presenceDoorClient{dir: filepath.Clean(dir), requests: make(chan struct{}, 1), beats: make(chan time.Time, 1), refreshes: make(chan time.Time, 1)}
	run.clients[c] = true
	w.add(run, c.dir)
	var once sync.Once
	return c.requests, c.beats, c.refreshes, func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			delete(run.clients, c)
			keep := false
			for other := range run.clients {
				if other.dir == c.dir {
					keep = true
				}
			}
			if !keep && run.watched[c.dir] {
				_ = run.watcher.Remove(c.dir)
				delete(run.watched, c.dir)
			}
			if len(run.clients) == 0 {
				close(run.stop)
				if run.watcher != nil {
					_ = run.watcher.Close()
				}
				w.run = nil
			}
		})
	}
}

// add retries missing directories on the safety pass, including a folder that
// was replaced while watched. Requests already there are checked after subscribe.
func (w *presenceDoorWatch) add(run *presenceDoorRun, dir string) {
	if run.watcher != nil && !run.watched[dir] && run.watcher.Add(dir) == nil {
		run.watched[dir] = true
	}
}

func presenceRequest(name string) bool {
	return name == takeoverName || name == conversationDeleteRequest || strings.HasPrefix(name, ".task-delete-request-")
}

func (w *presenceDoorWatch) loop(run *presenceDoorRun) {
	ticker := time.NewTicker(presenceHeartbeat)
	defer ticker.Stop()
	refresh := time.NewTicker(presenceRefresh)
	defer refresh.Stop()
	var events <-chan fsnotify.Event
	var errors <-chan error
	if run.watcher != nil {
		events, errors = run.watcher.Events, run.watcher.Errors
	}
	for {
		select {
		case <-run.stop:
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			w.mu.Lock()
			if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 && run.watched[event.Name] {
				delete(run.watched, event.Name)
			}
			if event.Op&(fsnotify.Create|fsnotify.Write) != 0 && presenceRequest(filepath.Base(event.Name)) {
				for c := range run.clients {
					if c.dir == filepath.Dir(event.Name) {
						select {
						case c.requests <- struct{}{}:
						default:
						}
					}
				}
			}
			w.mu.Unlock()
		case _, ok := <-errors:
			if !ok {
				errors = nil
				continue
			}
			// Overflow is repaired by the same bounded safety pass as a missed event.
			w.rescan(run, time.Now())
		case now := <-refresh.C:
			w.refresh(run, now)
		case now := <-ticker.C:
			w.rescan(run, now)
		}
	}
}

func (w *presenceDoorWatch) rescan(run *presenceDoorRun, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for c := range run.clients {
		w.add(run, c.dir)
		select {
		case c.beats <- now:
		default:
		}
	}
}

// refresh shares the JSON clock without changing the maintenance cadence.
func (w *presenceDoorWatch) refresh(run *presenceDoorRun, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for c := range run.clients {
		select {
		case c.refreshes <- now:
		default:
		}
	}
}

// writePresenceRequest publishes complete requests, so a directory notification
// cannot lead the owner to consume an empty file before its writer fills it.
func writePresenceRequest(path string, raw []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".request-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
