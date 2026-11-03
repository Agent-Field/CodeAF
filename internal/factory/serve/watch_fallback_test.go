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
