package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// These tests are the first chat after a fresh pairing (contract P1 to P3 in
// NOTES-POLISH). A chat opened while its computer had no other computer, or no
// storage, is held back from sharing. The one thing that wakes it for want of
// storage was a timer shared by the whole process that looks once a minute, so a
// computer paired by ANOTHER process left the chat unshared for up to a minute
// unless something in this process said so. The fix is a small local check each
// time the chat is touched, and the clock in these tests is a fake: nothing here
// sleeps.

// recheckClock is the time a liveDrive reads, moved by hand.
type recheckClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *recheckClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *recheckClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// heldBackForStorage is a chat whose startup was refused for closed storage:
// the first start answers wireauth.ErrStorageOff, and a later one succeeds with
// an empty fixture drive. The drive side's own clock and timers are fakes.
func heldBackForStorage(t *testing.T, startErr error) (l *liveDrive, clock *recheckClock, starts *atomic.Int32) {
	t.Helper()
	// A real home that is paired and whose permission file says storage is off:
	// the state a chat opened just before the other computer's pairing started in.
	_ = storageOffNoticeConfig(t)
	clock = &recheckClock{now: time.Unix(1_800_000_000, 0)}
	starts = &atomic.Int32{}
	entered := make(chan struct{})
	l = newLiveDrive(func(func()) (*syncsetup.Drive, error) {
		<-entered
		if starts.Add(1) == 1 {
			return nil, startErr
		}
		return &syncsetup.Drive{}, nil
	})
	l.now = clock.Now
	l.after = func(time.Duration, func()) func() bool { return func() bool { return true } }
	l.say = func(string) {}
	close(entered)
	startupFinished(l)
	t.Cleanup(func() {
		// The empty fixture drive has no process to close.
		l.mu.Lock()
		l.drive = nil
		l.mu.Unlock()
		_ = l.Close(t.Context())
	})
	return l, clock, starts
}

// P1, P2, P3: while storage stays closed a touched chat looks at local truth at
// most once every two seconds and stays held back; the first look after
// storage is open starts it, at the touch and not on the minute timer.
func TestAChatHeldBackForClosedStorageStartsAtTheNextTouchOnceStorageIsOpen(t *testing.T) {
	l, clock, starts := heldBackForStorage(t, wireauth.ErrStorageOff)
	if !l.storageWaiting {
		t.Fatal("a chat refused for closed storage is not waiting for it")
	}
	var reads atomic.Int32
	var open atomic.Bool
	l.storageOpen = func(string) bool { reads.Add(1); return open.Load() }

	// P2: the first touch looks once; touches inside the two seconds do not.
	l.try()
	if reads.Load() != 1 {
		t.Fatalf("the first touch read local truth %d times, want 1", reads.Load())
	}
	clock.advance(time.Second)
	l.try()
	l.Gate()
	if reads.Load() != 1 {
		t.Fatalf("touches within two seconds read local truth %d times, want 1", reads.Load())
	}

	// P3: past the two seconds it looks again, finds storage closed, and the
	// chat stays held back with no new start.
	clock.advance(2 * time.Second)
	l.try()
	if reads.Load() != 2 || !l.storageWaiting || starts.Load() != 1 {
		t.Fatalf("reads=%d waiting=%v starts=%d, want 2 reads, still waiting, 1 start", reads.Load(), l.storageWaiting, starts.Load())
	}

	// Another process opens storage. A touch inside the two seconds still does
	// not look, so the cost bound holds when storage opens too.
	open.Store(true)
	clock.advance(time.Second)
	l.try()
	if reads.Load() != 2 || starts.Load() != 1 {
		t.Fatalf("a touch inside the two seconds looked: reads=%d starts=%d", reads.Load(), starts.Load())
	}

	// P1: the next touch after the two seconds, here the start of a turn,
	// finds it open and starts the drive side.
	clock.advance(2 * time.Second)
	l.Working(true)
	startupFinished(l)
	if starts.Load() != 2 {
		t.Fatalf("the drive side started %d times after storage opened, want 2", starts.Load())
	}
	l.mu.Lock()
	waiting, started := l.storageWaiting, l.drive != nil
	l.mu.Unlock()
	if waiting || !started {
		t.Fatalf("storageWaiting=%v drive running=%v after storage opened", waiting, started)
	}

	// P2: with a drive running, touching the chat reads nothing at all.
	before := reads.Load()
	for i := 0; i < 5; i++ {
		clock.advance(time.Hour)
		l.try()
	}
	if reads.Load() != before {
		t.Fatalf("a running chat read local truth %d more times", reads.Load()-before)
	}
}

// P3: a chat held back for any other reason (the relay full, say) has no
// storage wait, so the new look never happens and its own wait is untouched.
func TestAChatHeldBackForAnotherReasonNeverLooksAtStorage(t *testing.T) {
	l, clock, _ := heldBackForStorage(t, wireauth.ErrRelayFull)
	var reads atomic.Int32
	l.storageOpen = func(string) bool { reads.Add(1); return true }
	for i := 0; i < 3; i++ {
		clock.advance(time.Second)
		l.try()
	}
	if reads.Load() != 0 {
		t.Fatalf("a chat that is not waiting for storage read it %d times", reads.Load())
	}
}

// The look is the same question the drive's own startup asked: local files only.
// Closed while the account says off and no other computer is carrying chats
// live, open the moment the permission file says on.
func TestTheStorageLookReadsTheSameLocalFilesStartupDid(t *testing.T) {
	_ = storageOffNoticeConfig(t)
	if storageOpenAt(home.Dir()) {
		t.Fatal("storage reads as open while the permission file says off")
	}
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.SaveStorage(home.Dir(), syncsetup.Resolve(home.Dir()).URL, id.ID(), "on"); err != nil {
		t.Fatal(err)
	}
	if !storageOpenAt(home.Dir()) {
		t.Fatal("storage reads as closed after the permission file says on")
	}
}
