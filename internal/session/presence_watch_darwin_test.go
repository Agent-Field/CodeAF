//go:build darwin

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// doorFDCount counts this process's descriptors by name only. os.ReadDir would
// stat every entry, and on macOS some of /dev/fd cannot be stat'ed (EBADF).
func doorFDCount(t *testing.T) int {
	t.Helper()
	dir, err := os.Open("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return len(names)
}

func watchedDoorFDCount(t *testing.T, backend *darwinDoorWatcher) int {
	t.Helper()
	// Exclude the short-lived ReadDir descriptor from the steady watch count.
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return doorFDCount(t)
}

func darwinDoorRun(t *testing.T, watch *presenceDoorWatch) (*presenceDoorRun, *darwinDoorWatcher) {
	t.Helper()
	watch.mu.Lock()
	defer watch.mu.Unlock()
	if watch.run == nil || watch.run.watcher == nil {
		t.Fatal("directory watcher did not start")
	}
	return watch.run, watch.run.watcher.(*darwinDoorWatcher)
}

func writeDoorOrdinaryFiles(t *testing.T, dir string, start, end int) {
	t.Helper()
	for i := start; i < end; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("ordinary-%d", i)), []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDarwinPresenceDoorC1OneHandlePerFolder(t *testing.T) {
	dir := t.TempDir()
	writeDoorOrdinaryFiles(t, dir, 0, 20)
	before := doorFDCount(t)
	var watch presenceDoorWatch
	_, _, _, cancel := watch.subscribe(dir)
	t.Cleanup(cancel)
	_, backend := darwinDoorRun(t, &watch)
	after := watchedDoorFDCount(t, backend)
	if delta := after - before; delta < 1 || delta > 2 {
		t.Fatalf("watching a folder with 20 files added %d descriptors, want at most 2", delta)
	}
	writeDoorOrdinaryFiles(t, dir, 20, 40)
	withMoreFiles := watchedDoorFDCount(t, backend)
	if withMoreFiles != after {
		t.Fatalf("20 more files changed descriptors from %d to %d", after, withMoreFiles)
	}
	otherDir := t.TempDir()
	_, _, _, otherCancel := watch.subscribe(otherDir)
	t.Cleanup(otherCancel)
	withOtherFolder := watchedDoorFDCount(t, backend)
	if withOtherFolder != after+1 {
		t.Fatalf("second folder added %d descriptors, want 1", withOtherFolder-after)
	}
}

func TestDarwinPresenceDoorC2PublishedRequestsWakeByEvent(t *testing.T) {
	for _, name := range []string{takeoverName, conversationDeleteRequest, ".task-delete-request-123"} {
		for _, replace := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/replace=%t", name, replace), func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, name)
				if replace {
					if err := writePresenceRequest(path, []byte("old request")); err != nil {
						t.Fatal(err)
					}
				}
				var watch presenceDoorWatch
				requests, beats, _, cancel := watch.subscribe(dir)
				t.Cleanup(cancel)
				started := time.Now()
				if err := writePresenceRequest(path, []byte("new request")); err != nil {
					t.Fatal(err)
				}
				waitDoorRequest(t, requests)
				if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
					t.Fatalf("request took %s, want less than 500ms", elapsed)
				}
				select {
				case <-beats:
					t.Fatal("request needed a safety pass")
				default:
				}
			})
		}
	}
}

func waitDoorDropped(t *testing.T, watch *presenceDoorWatch, run *presenceDoorRun, dir string) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		watch.mu.Lock()
		watched := run.watched[dir]
		watch.mu.Unlock()
		if !watched {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("removed or renamed folder stayed watched")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDarwinPresenceDoorC4ReplacedFolderIsWatchedAgain(t *testing.T) {
	for _, rename := range []bool{false, true} {
		t.Run(fmt.Sprintf("rename=%t", rename), func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "session")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			var watch presenceDoorWatch
			requests, _, _, cancel := watch.subscribe(dir)
			t.Cleanup(cancel)
			run, backend := darwinDoorRun(t, &watch)
			backend.mu.Lock()
			fd := backend.dirs[dir].fd
			backend.mu.Unlock()
			var err error
			if rename {
				err = os.Rename(dir, filepath.Join(root, "old-session"))
			} else {
				err = os.Remove(dir)
			}
			if err != nil {
				t.Fatal(err)
			}
			waitDoorDropped(t, &watch, run, dir)
			backend.mu.Lock()
			oldWatch := backend.dirs[dir]
			backend.mu.Unlock()
			if oldWatch != nil {
				t.Fatal("old folder retained its kernel watch")
			}
			if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != unix.EBADF {
				t.Fatalf("old folder descriptor is still open: %v", err)
			}
			watch.rescan(run, time.Now())
			watch.mu.Lock()
			watched := run.watched[dir]
			watch.mu.Unlock()
			if watched {
				t.Fatal("safety pass marked a missing folder watched")
			}
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			watch.rescan(run, time.Now())
			watch.mu.Lock()
			watched = run.watched[dir]
			watch.mu.Unlock()
			if !watched {
				t.Fatal("safety pass did not watch the replacement folder")
			}
			if err := writePresenceRequest(filepath.Join(dir, takeoverName), []byte("request")); err != nil {
				t.Fatal(err)
			}
			waitDoorRequest(t, requests)
		})
	}
}

func TestDarwinPresenceDoorC5ClosesHandlesAndRestarts(t *testing.T) {
	var watch presenceDoorWatch
	dir := t.TempDir()
	_, _, _, cancel := watch.subscribe(dir)
	t.Cleanup(cancel)
	_, _, _, siblingCancel := watch.subscribe(dir)
	t.Cleanup(siblingCancel)
	run, backend := darwinDoorRun(t, &watch)
	backend.mu.Lock()
	fd, kq := backend.dirs[dir].fd, backend.kq
	backend.mu.Unlock()
	cancel()
	cancel()
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		t.Fatalf("folder closed while a client still needed it: %v", err)
	}
	siblingCancel()
	siblingCancel()
	for _, descriptor := range []int{fd, kq} {
		if _, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0); err != unix.EBADF {
			t.Fatalf("descriptor %d remained open after last cancel: %v", descriptor, err)
		}
	}
	select {
	case <-run.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shared loop remained running after last cancel")
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("second backend close: %v", err)
	}
	requests, _, _, restartCancel := watch.subscribe(dir)
	t.Cleanup(restartCancel)
	_, restarted := darwinDoorRun(t, &watch)
	if restarted == backend {
		t.Fatal("subscribe after a full stop reused a closed backend")
	}
	if err := writePresenceRequest(filepath.Join(dir, takeoverName), []byte("request")); err != nil {
		t.Fatal(err)
	}
	waitDoorRequest(t, requests)
}

func TestDarwinPresenceDoorC5CloseWithoutDrainingEvents(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			watcher, err := newDoorWatcher()
			if err != nil {
				t.Fatal(err)
			}
			closeStarted := false
			t.Cleanup(func() {
				if !closeStarted {
					_ = watcher.Close()
				}
			})
			dir := t.TempDir()
			if err := watcher.Add(dir); err != nil {
				t.Fatal(err)
			}
			if pending {
				if err := writePresenceRequest(filepath.Join(dir, takeoverName), []byte("request")); err != nil {
					t.Fatal(err)
				}
			}
			closed := make(chan error, 1)
			closeStarted = true
			go func() { closed <- watcher.Close() }()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Close left the kernel reader or event sender blocked")
			}
			if _, ok := <-watcher.Events(); ok {
				t.Fatal("event channel remained open after Close")
			}
			if _, ok := <-watcher.Errors(); ok {
				t.Fatal("error channel remained open after Close")
			}
		})
	}
}

func waitDoorFDs(t *testing.T, backend *darwinDoorWatcher, want int, what string) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		got := watchedDoorFDCount(t, backend)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %d descriptors, want %d", what, got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func noDoorRequest(t *testing.T, requests <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-requests:
		t.Fatal(what)
	case <-time.After(200 * time.Millisecond):
	}
}

// C9: an answer appended to answers.jsonl wakes its folder's session by event,
// whether the file is new or already waiting, and a created but still empty
// file does not (deliverAnswer creates it and then writes the line).
// C10: the answers file costs one handle while it exists, and draining it
// gives that handle back.
func TestDarwinPresenceDoorC9C10AnswersWakeAndReleaseTheirHandle(t *testing.T) {
	dir := t.TempDir()
	var watch presenceDoorWatch
	requests, _, _, cancel := watch.subscribe(dir)
	t.Cleanup(cancel)
	_, backend := darwinDoorRun(t, &watch)
	folderOnly := watchedDoorFDCount(t, backend)

	empty, err := os.OpenFile(AnswersPath(dir), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	empty.Close()
	noDoorRequest(t, requests, "C9: an empty answers file woke the session before its line was written")
	waitDoorFDs(t, backend, folderOnly+1, "C10: answers file present")

	if err := deliverAnswer(dir, Answer{At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	waitDoorRequest(t, requests)
	if err := deliverAnswer(dir, Answer{At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	waitDoorRequest(t, requests) // C9: an append to a file already there

	if _, err := DrainAnswers(dir); err != nil {
		t.Fatal(err)
	}
	waitDoorFDs(t, backend, folderOnly, "C10: answers drained")
	// Draining leaves a stray wake at most; take it before the next check.
	select {
	case <-requests:
	case <-time.After(50 * time.Millisecond):
	}

	if err := deliverAnswer(dir, Answer{At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	waitDoorRequest(t, requests) // C9: a fresh file after a drain
	waitDoorFDs(t, backend, folderOnly+1, "C10: a fresh answers file")
}
