package main

import (
	"context"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/tui3"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// countingDir answers every listing as a relay that has stopped this device,
// and counts how often it was asked.
type countingDir struct {
	directory.Client
	asked int
}

func (c *countingDir) List(context.Context) (directory.Listing, error) {
	c.asked++
	return directory.Listing{}, wireauth.ErrRevoked
}

func TestCheckStandingAsksOnlyASharedHome(t *testing.T) {
	cases := []struct {
		name   string
		shared bool
		asked  int
		said   int
	}{{"solo home sends nothing", false, 0, 0}, {"shared home asks once and says so", true, 1, 1}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if _, err := identity.EnsureSolo(home); err != nil {
				t.Fatal(err)
			}
			if tc.shared {
				if err := identity.EndSolo(home); err != nil {
					t.Fatal(err)
				}
			}
			dir, lines := &countingDir{}, []string{}
			checkStanding(home, dir, func(l string) { lines = append(lines, l) })
			if dir.asked != tc.asked || len(lines) != tc.said {
				t.Errorf("asked %d said %v, want %d and %d lines", dir.asked, lines, tc.asked, tc.said)
			}
		})
	}
}

// fakeFeed is a follower the test drives: set the state, then signal.
type fakeFeed struct {
	dirwatch.Follower
	mu      sync.Mutex
	state   dirwatch.State
	changes chan struct{}
	closed  bool
}

func newFakeFeed() *fakeFeed { return &fakeFeed{changes: make(chan struct{}, 4)} }

func (f *fakeFeed) State() dirwatch.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}
func (f *fakeFeed) Changes() <-chan struct{} { return f.changes }
func (f *fakeFeed) Close()                   { f.closed = true }

func (f *fakeFeed) refuse(err error) {
	f.mu.Lock()
	f.state.Refused = err
	f.mu.Unlock()
	f.changes <- struct{}{}
}

func sharedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if _, err := identity.EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	if err := identity.EndSolo(home); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestARevokedSocketIsSaidOnceWhileTheAppRuns(t *testing.T) {
	feed := newFakeFeed()
	desk := tui3.NewNotices()
	var lines []string
	say := func(l string) { lines = append(lines, l); desk.SayOnce(l) }
	done := make(chan struct{})
	go func() { watchStanding(sharedHome(t), func() dirwatch.Follower { return feed }, say); close(done) }()
	feed.changes <- struct{}{} // a presence change is not a refusal
	feed.refuse(dirwatch.ErrRevoked)
	<-done
	// the first refused request finds out the same thing a moment later
	sayRefusalTo(desk.SayOnce, wireauth.ErrRevoked)
	if len(lines) != 1 || lines[0] != chatlist.Removed {
		t.Fatalf("said %q, want the removed sentence once", lines)
	}
	if !feed.closed {
		t.Error("the listener kept the feed after the refusal")
	}
}

func TestAFeedAlreadyRefusedIsSaidAtOnce(t *testing.T) {
	feed := newFakeFeed()
	feed.state.Refused = dirwatch.ErrRotated
	var lines []string
	watchStanding(sharedHome(t), func() dirwatch.Follower { return feed }, func(l string) { lines = append(lines, l) })
	if len(lines) != 1 || lines[0] != chatlist.Replaced {
		t.Fatalf("said %q, want the replaced sentence", lines)
	}
}

func TestASoloHomeOpensNoSocketToListen(t *testing.T) {
	home := t.TempDir()
	if _, err := identity.EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	watchStanding(home, func() dirwatch.Follower { t.Fatal("a solo home followed the feed"); return nil }, nil)
}
