package tui3

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// fakeFeed is the change feed a test moves by hand. Its Changes channel is
// closed, so the command that waits on it ends at once and the drive harness
// is never left waiting on a socket; the tests call tookWatch themselves.
type fakeFeed struct {
	state  dirwatch.State
	closed bool
	probes int
}

func (f *fakeFeed) State() dirwatch.State { return f.state }
func (f *fakeFeed) Probe()                { f.probes++ }
func (f *fakeFeed) Close()                { f.closed = true }
func (f *fakeFeed) Changes() <-chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// watchedSource is a versioned chat list source that hands out a feed and
// counts how often it is read. It answers with `version` unless told to lag.
type watchedSource struct {
	feed    *fakeFeed
	asks    int
	version uint64
	err     error
}

func (w *watchedSource) Rows(ctx context.Context) ([]chatlist.Row, error) {
	rows, _, err := w.RowsAt(ctx)
	return rows, err
}

func (w *watchedSource) RowsAt(context.Context) ([]chatlist.Row, uint64, error) {
	w.asks++
	return []chatlist.Row{{Cell: "c1", Title: "one", Status: chatlist.Idle}}, w.version, w.err
}

func (w *watchedSource) Follow() dirwatch.Follower { return w.feed }

// watchHome is home on a clock the test owns, with the feed joined by the
// first beat and the read counter at zero.
func watchHome(t *testing.T, up bool) (*app, *watchedSource, *time.Time) {
	t.Helper()
	src := &watchedSource{feed: &fakeFeed{state: dirwatch.State{Up: up}}}
	a := machinesHome(t, src, 200)
	now := time.Unix(1_000_000, 0)
	a.clock = func() time.Time { return now }
	a.lastQuestionKey = now
	a.machinePoll.hurry()
	beat(t, a, &now, 0)
	if a.dirFeed == nil {
		t.Fatal("home did not join the feed")
	}
	src.asks = 0
	return a, src, &now
}

// announceVersion announces version v and lets home take the signal.
func announceVersion(t *testing.T, a *app, src *watchedSource, v uint64) {
	t.Helper()
	src.feed.state.Version = v
	drain(t, a, a.tookWatch(dirWatchMsg{from: src.feed}))
}

func TestAFrameAheadOfTheReadingIsOneRead(t *testing.T) {
	a, src, _ := watchHome(t, true)
	src.version = 5
	announceVersion(t, a, src, 5)
	if src.asks != 1 {
		t.Fatalf("a new version caused %d reads, want 1", src.asks)
	}
}

func TestAFrameForTheVersionAlreadyReadCausesNothing(t *testing.T) {
	a, src, _ := watchHome(t, true)
	src.version = 5
	announceVersion(t, a, src, 5)
	announceVersion(t, a, src, 5)
	announceVersion(t, a, src, 4)
	if src.asks != 1 {
		t.Fatalf("frames at or below the version read caused %d reads in all, want 1", src.asks)
	}
}

// fiftyFrames delivers versions 1 to 50 while the first read is still in
// flight, then lets that read land, and returns how many reads were made.
func fiftyFrames(t *testing.T, a *app, src *watchedSource) int {
	t.Helper()
	first := a.announce(1, src)
	for v := uint64(2); v <= 50; v++ {
		src.feed.state.Version = v
		if cmd := a.readForFrame(); cmd != nil {
			t.Fatalf("frame %d started a second read while one was in flight", v)
		}
	}
	drain(t, a, first)
	return src.asks
}

// readForFrameAt announces version v and returns the read it asks for, unrun.
func (a *app) announce(v uint64, src *watchedSource) tea.Cmd {
	src.feed.state.Version = v
	return a.readForFrame()
}

func TestABurstOfFiftyFramesIsAtMostTwoReads(t *testing.T) {
	a, src, _ := watchHome(t, true)
	src.version = 50
	if got := fiftyFrames(t, a, src); got != 1 {
		t.Fatalf("a burst read the list %d times when the first read already saw the end of it, want 1", got)
	}
	a, src, _ = watchHome(t, true)
	src.version = 10 // the first read was taken before most of the burst
	if got := fiftyFrames(t, a, src); got != 2 {
		t.Fatalf("a burst read the list %d times when the first read was early, want 2", got)
	}
}

func TestAFailedReadOwesNothingMore(t *testing.T) {
	a, src, _ := watchHome(t, true)
	src.err = errors.New("unreachable")
	if got := fiftyFrames(t, a, src); got != 1 {
		t.Fatalf("a failed read was followed by %d more", got-1)
	}
}

func TestSocketUpReadsAtMostOncePerBackstopInAnOpenHour(t *testing.T) {
	a, src, now := watchHome(t, true)
	for elapsed := time.Duration(0); elapsed < time.Hour; elapsed += homeEvery {
		beat(t, a, now, homeEvery)
	}
	t.Logf("directory lists in one open hour, socket up, nothing changing: %d", src.asks)
	// The first read on arrival, then one per backstop.
	if limit := 1 + int(time.Hour/machinesBackstop); src.asks > limit {
		t.Fatalf("an hour with the socket up read the list %d times, want at most %d", src.asks, limit)
	}
}

func TestSocketDownIsTheAdaptiveScheduleAsBefore(t *testing.T) {
	a, src, now := watchHome(t, false)
	for elapsed := time.Duration(0); elapsed < time.Hour; elapsed += homeEvery {
		beat(t, a, now, homeEvery)
	}
	if src.asks < 55 || src.asks > 70 {
		t.Fatalf("an hour with no socket read the list %d times, want the ladder's 64 or so", src.asks)
	}
}

func TestADroppedSocketGoesBackToTheFastPaceAtOnce(t *testing.T) {
	a, src, now := watchHome(t, true)
	for i := 0; i < 600; i++ {
		beat(t, a, now, homeEvery)
	}
	src.feed.state.Up = false
	announceVersion(t, a, src, 0)
	before := src.asks
	beat(t, a, now, homeEvery)
	if src.asks != before+1 {
		t.Fatalf("the beat after the socket dropped made %d reads, want 1", src.asks-before)
	}
	beat(t, a, now, machinesFast)
	if src.asks != before+2 {
		t.Fatalf("the list was not at its fast pace after the drop")
	}
}

func TestTheFeedIsLetGoWhenHomeStopsBeingLookedAt(t *testing.T) {
	a, src, now := watchHome(t, true)
	a.page = pageChats
	beat(t, a, now, homeEvery)
	if !src.feed.closed || a.dirFeed != nil {
		t.Fatal("the feed was held after home closed")
	}
}

func TestTheFeedIsLetGoWhileTheWindowIsUnattended(t *testing.T) {
	a, src, now := watchHome(t, true)
	a.seenFocus, a.focused = true, false
	beat(t, a, now, homeEvery)
	if !src.feed.closed {
		t.Fatal("the feed was held while nobody was at the window")
	}
}

func TestReturningToTheWindowProbesTheSocket(t *testing.T) {
	a, src, _ := watchHome(t, true)
	a.probeWatch()
	if src.feed.probes != 1 {
		t.Fatalf("returning to the window probed the socket %d times, want 1", src.feed.probes)
	}
}
