package session

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

type fakeDoorWatcher struct {
	mu       sync.Mutex
	events   chan doorEvent
	errors   chan error
	adds     map[string]int
	removes  map[string]int
	addError map[string]error
	closes   int
}

func newFakeDoorWatcher() *fakeDoorWatcher {
	return &fakeDoorWatcher{
		events: make(chan doorEvent), errors: make(chan error),
		adds: map[string]int{}, removes: map[string]int{}, addError: map[string]error{},
	}
}

func (w *fakeDoorWatcher) Add(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.adds[dir]++
	return w.addError[dir]
}

func (w *fakeDoorWatcher) Remove(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.removes[dir]++
	return nil
}

func (w *fakeDoorWatcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closes++
	if w.closes == 1 {
		close(w.events)
		close(w.errors)
	}
	return nil
}

func (w *fakeDoorWatcher) Events() <-chan doorEvent { return w.events }
func (w *fakeDoorWatcher) Errors() <-chan error     { return w.errors }

func (w *fakeDoorWatcher) event(t *testing.T, event doorEvent) {
	t.Helper()
	select {
	case w.events <- event:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shared loop did not receive an event")
	}
}

// The next receive proves the shared loop finished handling the previous input.
func (w *fakeDoorWatcher) flush(t *testing.T) {
	t.Helper()
	w.event(t, doorEvent{})
}

func waitDoorRequest(t *testing.T, requests <-chan struct{}) {
	t.Helper()
	select {
	case <-requests:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("request waited for the five-second safety pass")
	}
}

func TestPresenceDoorC2C3RoutesRequestEvents(t *testing.T) {
	for _, name := range []string{takeoverName, conversationDeleteRequest, ".task-delete-request-123"} {
		for _, op := range []fsnotify.Op{fsnotify.Create, fsnotify.Write} {
			t.Run(name+"/"+op.String(), func(t *testing.T) {
				fake := newFakeDoorWatcher()
				watch := presenceDoorWatch{newWatcher: func() (doorWatcher, error) { return fake, nil }}
				dir := t.TempDir()
				requests, beats, _, cancel := watch.subscribe(dir)
				t.Cleanup(cancel)
				sibling, _, _, siblingCancel := watch.subscribe(dir)
				t.Cleanup(siblingCancel)
				other, _, _, otherCancel := watch.subscribe(t.TempDir())
				t.Cleanup(otherCancel)
				path := filepath.Join(dir, name)
				if err := writePresenceRequest(path, []byte("request")); err != nil {
					t.Fatal(err)
				}
				fake.event(t, doorEvent{Name: path, Op: op})
				waitDoorRequest(t, requests)
				waitDoorRequest(t, sibling)
				fake.flush(t)
				select {
				case <-other:
					t.Fatal("C3: request woke a different folder")
				default:
				}
				select {
				case <-beats:
					t.Fatal("C2: request delivery needed a safety pass")
				default:
				}
			})
		}
	}
}

func TestPresenceDoorC4SafetyPassReaddsDroppedFolders(t *testing.T) {
	for _, op := range []fsnotify.Op{fsnotify.Remove, fsnotify.Rename} {
		t.Run(op.String(), func(t *testing.T) {
			fake := newFakeDoorWatcher()
			watch := presenceDoorWatch{newWatcher: func() (doorWatcher, error) { return fake, nil }}
			dir := t.TempDir()
			_, _, _, cancel := watch.subscribe(dir)
			t.Cleanup(cancel)
			run := watch.run
			fake.event(t, doorEvent{Name: dir, Op: op})
			fake.flush(t)
			watch.mu.Lock()
			watched := run.watched[dir]
			watch.mu.Unlock()
			if watched {
				t.Fatal("removed folder remained watched")
			}
			watch.rescan(run, time.Now())
			watch.mu.Lock()
			watched = run.watched[dir]
			watch.mu.Unlock()
			if !watched || fake.adds[dir] != 2 {
				t.Fatal("safety pass did not re-add the folder")
			}
		})
	}
}

func TestPresenceDoorC5CancelClosesOnceAndCanRestart(t *testing.T) {
	var watchers []*fakeDoorWatcher
	watch := presenceDoorWatch{newWatcher: func() (doorWatcher, error) {
		fake := newFakeDoorWatcher()
		watchers = append(watchers, fake)
		return fake, nil
	}}
	dir, otherDir := t.TempDir(), t.TempDir()
	_, _, _, cancel := watch.subscribe(dir)
	t.Cleanup(cancel)
	_, _, _, siblingCancel := watch.subscribe(dir)
	t.Cleanup(siblingCancel)
	_, _, _, otherCancel := watch.subscribe(otherDir)
	t.Cleanup(otherCancel)
	run, fake := watch.run, watchers[0]
	if len(watchers) != 1 || fake.adds[dir] != 1 || fake.adds[otherDir] != 1 {
		t.Fatal("subscriptions did not share one watcher and one watch per folder")
	}
	cancel()
	cancel()
	if fake.removes[dir] != 0 || fake.closes != 0 {
		t.Fatal("cancel closed a folder still used by another client")
	}
	siblingCancel()
	siblingCancel()
	if fake.removes[dir] != 1 || fake.closes != 0 {
		t.Fatal("last folder client did not remove exactly one watch")
	}
	otherCancel()
	otherCancel()
	if fake.removes[otherDir] != 1 || fake.closes != 1 || watch.run != nil {
		t.Fatal("last client did not close and release the shared watcher exactly once")
	}
	select {
	case <-run.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shared loop did not stop")
	}
	requests, _, _, restartCancel := watch.subscribe(dir)
	t.Cleanup(restartCancel)
	if len(watchers) != 2 || watch.run == run {
		t.Fatal("subscribe after full stop reused the old run")
	}
	watchers[1].event(t, doorEvent{Name: filepath.Join(dir, takeoverName), Op: fsnotify.Create})
	waitDoorRequest(t, requests)
}

func TestPresenceDoorC6ErrorsRunBoundedSafetyPassAndKnock(t *testing.T) {
	fake := newFakeDoorWatcher()
	dir, missing := t.TempDir(), t.TempDir()
	fake.addError[missing] = errors.New("folder not yet present")
	watch := presenceDoorWatch{newWatcher: func() (doorWatcher, error) { return fake, nil }}
	requests, beats, refreshes, cancel := watch.subscribe(dir)
	t.Cleanup(cancel)
	_, missingBeats, _, missingCancel := watch.subscribe(missing)
	t.Cleanup(missingCancel)
	fake.mu.Lock()
	delete(fake.addError, missing)
	fake.mu.Unlock()
	for i := 0; i < 3; i++ {
		select {
		case fake.errors <- errors.New("watcher overflow"):
		case <-time.After(500 * time.Millisecond):
			t.Fatal("watcher error blocked the shared loop")
		}
		fake.flush(t)
	}
	for _, ticks := range []<-chan time.Time{beats, missingBeats} {
		if len(ticks) != 1 {
			t.Fatal("safety pass did not coalesce into one pending heartbeat per client")
		}
		if (<-ticks).IsZero() {
			t.Fatal("safety pass delivered an empty heartbeat")
		}
	}
	watch.mu.Lock()
	watchingMissing := watch.run.watched[missing]
	watch.mu.Unlock()
	if !watchingMissing || fake.adds[missing] != 2 {
		t.Fatal("error safety pass did not retry a failed folder watch")
	}
	// An error can have dropped any folder's event, so every session is knocked
	// once to look at its doorstep now; the knocks coalesce like the beats.
	if len(requests) != 1 {
		t.Fatalf("watcher error must knock every session once: %d request(s) pending", len(requests))
	}
	if len(refreshes) != 0 {
		t.Fatal("watcher error changed the refresh cadence")
	}
}

func TestPresenceDoorC8IgnoresOrdinaryFileEvents(t *testing.T) {
	fake := newFakeDoorWatcher()
	watch := presenceDoorWatch{newWatcher: func() (doorWatcher, error) { return fake, nil }}
	dir := t.TempDir()
	requests, beats, refreshes, cancel := watch.subscribe(dir)
	t.Cleanup(cancel)
	for _, name := range []string{"transcript.jsonl", presenceName, ".request-temporary"} {
		fake.event(t, doorEvent{Name: filepath.Join(dir, name), Op: fsnotify.Create | fsnotify.Write})
	}
	fake.flush(t)
	if len(requests) != 0 || len(beats) != 0 || len(refreshes) != 0 {
		t.Fatal("ordinary file event woke a client")
	}
}
