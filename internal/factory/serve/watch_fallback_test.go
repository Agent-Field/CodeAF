package serve

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// A host whose inotify instances are spent opens no watch. The factory says
// so once, naming the cause, and its open pulls look on a slow timer rather
// than ten times a second: the idle burn the watch exists to remove.
func TestAFactoryWithoutAWatchWarnsOnceAndLooksSlowly(t *testing.T) {
	newFSWatcher = func() (*fsnotify.Watcher, error) {
		return nil, fmt.Errorf("inotify_init1: %w", syscall.EMFILE)
	}
	t.Cleanup(func() { newFSWatcher = fsnotify.NewWatcher })
	watchWarned = sync.Once{}
	var said bytes.Buffer
	log.SetOutput(&said)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	for range 2 {
		w := watchRoot(t.TempDir())
		t.Cleanup(w.stop)
	}
	if n := strings.Count(said.String(), "inotify instances exhausted"); n != 1 {
		t.Fatalf("warned %d times, want once naming the cause: %q", n, said.String())
	}

	h := &Handler{watch: watchRoot(t.TempDir())}
	t.Cleanup(h.watch.stop)
	blind, stop := h.blindLooks()
	defer stop()
	looks := 0
	deadline := time.After(1500 * time.Millisecond)
	for wait := true; wait; {
		select {
		case <-blind:
			looks++
		case <-deadline:
			wait = false
		}
	}
	// A ticker drops ticks it cannot deliver, so load can only lower this.
	if looks > 1 {
		t.Fatalf("an unwatched pull looked %d times in 1.5s, want at most once a second", looks)
	}
}

// A root with a live filesystem watch is looked at without being told only
// as a safety net, never every two seconds: the watch hears every write, and
// the look lists the root and stats every job in it, which macOS charges for.
// A root with no watch keeps the quick poll.
func TestAWatchedRootIsNotReReadEveryTwoSeconds(t *testing.T) {
	watched := watchRoot(t.TempDir())
	t.Cleanup(watched.stop)
	if watched.fs == nil {
		t.Skip("this host opened no filesystem watch")
	}
	if got := watched.pollEvery(); got != changePollWatched || got < 30*time.Second {
		t.Fatalf("a watched root is looked at every %v, want %v", got, changePollWatched)
	}
	newFSWatcher = func() (*fsnotify.Watcher, error) {
		return nil, fmt.Errorf("inotify_init1: %w", syscall.EMFILE)
	}
	t.Cleanup(func() { newFSWatcher = fsnotify.NewWatcher })
	watchWarned = sync.Once{}
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	blind := watchRoot(t.TempDir())
	t.Cleanup(blind.stop)
	if got := blind.pollEvery(); got != changePoll {
		t.Fatalf("an unwatched root is looked at every %v, want %v", got, changePoll)
	}
}
