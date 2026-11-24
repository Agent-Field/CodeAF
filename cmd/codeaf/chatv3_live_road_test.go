package main

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

func newTestLiveLink() *livedoor.Link {
	return livedoor.NewLink(livedoor.LinkConfig{
		Dial: func(context.Context) (io.ReadWriteCloser, error) {
			return nil, errors.New("test link never dials")
		},
	})
}

// V1/V14: syncLiveRoads answers no road when remote control is off, for the empty device, and for this computer's own id; it answers a road (same pointer) twice for the same device and name and a different road for a new name.
func TestSyncLiveRoadsCachesOneRoadPerDeviceAndNameOnlyWhenRemoteControlIsOn(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("CODEAF_HOME", homeDir)

	id, err := identity.Ensure(homeDir)
	if err != nil {
		t.Fatalf("ensure identity: %v", err)
	}
	dev, err := identity.Device(homeDir)
	if err != nil {
		t.Fatalf("ensure device: %v", err)
	}
	s := &syncsetup.Sync{
		Home:     homeDir,
		Identity: id,
		Device:   dev,
	}
	roads := syncLiveRoads(s)

	// Remote control off: never returns a road.
	t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	if road, name, ok := roads("dev_peer", "Desktop"); ok || road != nil || name != "" {
		t.Fatalf("roads with remote control off = (%v, %q, %v), want (nil, \"\", false)", road, name, ok)
	}

	// Remote control on: empty device and this computer's own device id return no road.
	t.Setenv("CODEAF_REMOTE_CONTROL", "1")
	if road, name, ok := roads("", "Desktop"); ok || road != nil || name != "" {
		t.Fatalf("roads for empty device = (%v, %q, %v), want (nil, \"\", false)", road, name, ok)
	}
	if road, name, ok := roads(dev.ID(), "This Computer"); ok || road != nil || name != "" {
		t.Fatalf("roads for own device id = (%v, %q, %v), want (nil, \"\", false)", road, name, ok)
	}

	// Same device and name returns the exact same road pointer twice.
	first, name1, ok1 := roads("dev_peer", "Desktop")
	if !ok1 || first == nil || name1 != "Desktop" {
		t.Fatalf("first roads(dev_peer, Desktop) = (%v, %q, %v), want non-nil road for Desktop", first, name1, ok1)
	}
	t.Cleanup(first.(*syncRoad).Close)

	second, name2, ok2 := roads("dev_peer", "Desktop")
	if !ok2 || second != first || name2 != "Desktop" {
		t.Fatalf("second roads(dev_peer, Desktop) = (%p, %q, %v), want (%p, \"Desktop\", true)", second, name2, ok2, first)
	}

	// A new name for the same device makes a fresh road.
	renamed, name3, ok3 := roads("dev_peer", "Studio")
	if !ok3 || renamed == nil || renamed == first || name3 != "Studio" {
		t.Fatalf("roads(dev_peer, Studio) = (%p, %q, %v), want distinct non-nil road for Studio (first=%p)", renamed, name3, ok3, first)
	}
	t.Cleanup(renamed.(*syncRoad).Close)
}

// V7: the road reads tui3.LiveWentOffline after the link reports livedoor.WentOffline, LiveReconnecting after Reconnecting, LiveIdle after Idle, LiveRevoked with the reason after Revoked, LiveBudget after StateBudget, LiveOn after Live and after WatchOnly; Changed()'s channel is closed by each change and the next call hands back a fresh open one.
func TestSyncRoadMapsLinkStatesAndClosesChangedOnEveryTransition(t *testing.T) {
	link := newTestLiveLink()
	road := newSyncRoad(link, "Desktop")
	t.Cleanup(road.Close)

	if got := road.Read(); got.Kind != tui3.LiveReconnecting {
		t.Fatalf("initial Read() = %+v, want LiveReconnecting", got)
	}

	steps := []struct {
		state livedoor.State
		want  tui3.LiveRead
	}{
		{
			state: livedoor.State{Kind: livedoor.WentOffline},
			want:  tui3.LiveRead{Kind: tui3.LiveWentOffline},
		},
		{
			state: livedoor.State{Kind: livedoor.Reconnecting},
			want:  tui3.LiveRead{Kind: tui3.LiveReconnecting},
		},
		{
			state: livedoor.State{Kind: livedoor.Idle},
			want:  tui3.LiveRead{Kind: tui3.LiveIdle},
		},
		{
			state: livedoor.State{Kind: livedoor.Revoked, Reason: "removed"},
			want:  tui3.LiveRead{Kind: tui3.LiveRevoked, Reason: "removed"},
		},
		{
			state: livedoor.State{Kind: livedoor.StateBudget},
			want:  tui3.LiveRead{Kind: tui3.LiveBudget},
		},
		{
			state: livedoor.State{Kind: livedoor.Live},
			want:  tui3.LiveRead{Kind: tui3.LiveOn},
		},
		{
			state: livedoor.State{Kind: livedoor.WatchOnly},
			want:  tui3.LiveRead{Kind: tui3.LiveOn},
		},
	}

	for i, step := range steps {
		prev := road.Changed()
		select {
		case <-prev:
			t.Fatalf("step %d (%v): Changed() channel was already closed before transition", i, step.state.Kind)
		default:
		}

		road.onState(step.state)

		select {
		case <-prev:
		default:
			t.Fatalf("step %d (%v): Changed() channel was not closed by onState", i, step.state.Kind)
		}

		next := road.Changed()
		if next == prev {
			t.Fatalf("step %d (%v): Changed() returned the same channel after onState", i, step.state.Kind)
		}
		select {
		case <-next:
			t.Fatalf("step %d (%v): fresh Changed() channel is already closed", i, step.state.Kind)
		default:
		}

		if got := road.Read(); got != step.want {
			t.Fatalf("step %d (%v): Read() = %+v, want %+v", i, step.state.Kind, got, step.want)
		}
	}
}

// V11: after WatchOnly with Permit{Watch: true}, Permit() is (false, chatlist.LiveWatchOnly(host)); after Live with Permit{Watch: true, Drive: true} it is (true, ""); after a later Reconnecting (which carries no permit) it keeps the last known answer.
func TestSyncRoadRemembersTheLastKnownPermitAcrossReconnects(t *testing.T) {
	const host = "Desktop"
	link := newTestLiveLink()
	road := newSyncRoad(link, host)
	t.Cleanup(road.Close)

	// Before any Live or WatchOnly state, the permit is the zero value.
	if drive, readOnly := road.Permit(); drive || readOnly != "" {
		t.Fatalf("initial Permit() = (%v, %q), want (false, \"\")", drive, readOnly)
	}

	// WatchOnly with Permit{Watch: true} reports watch-only for host.
	road.onState(livedoor.State{
		Kind:   livedoor.WatchOnly,
		Permit: pair.Permit{Watch: true},
	})
	wantWatchOnly := chatlist.LiveWatchOnly(host)
	if drive, readOnly := road.Permit(); drive || readOnly != wantWatchOnly {
		t.Fatalf("Permit() after WatchOnly = (%v, %q), want (false, %q)", drive, readOnly, wantWatchOnly)
	}

	// A later Reconnecting state carries no permit and keeps the watch-only answer.
	road.onState(livedoor.State{Kind: livedoor.Reconnecting})
	if drive, readOnly := road.Permit(); drive || readOnly != wantWatchOnly {
		t.Fatalf("Permit() during Reconnecting after WatchOnly = (%v, %q), want (false, %q)", drive, readOnly, wantWatchOnly)
	}

	// Live with Permit{Watch: true, Drive: true} allows driving.
	road.onState(livedoor.State{
		Kind:   livedoor.Live,
		Permit: pair.Permit{Watch: true, Drive: true},
	})
	if drive, readOnly := road.Permit(); !drive || readOnly != "" {
		t.Fatalf("Permit() after Live = (%v, %q), want (true, \"\")", drive, readOnly)
	}

	// A later Reconnecting state carries no permit and keeps the drive-permitted answer.
	road.onState(livedoor.State{Kind: livedoor.Reconnecting})
	if drive, readOnly := road.Permit(); !drive || readOnly != "" {
		t.Fatalf("Permit() during Reconnecting after Live = (%v, %q), want (true, \"\")", drive, readOnly)
	}
}

// V24: Touch() returns before link.Touch finishes: with the held state Idle, Touch() does not call the link at all; with it Live, exactly one link.Touch call happens even after ten Touch() calls issued while the first is in flight.
func TestSyncRoadCoalescesInFlightTouchesAndSkipsTouchWhileIdle(t *testing.T) {
	link := newTestLiveLink()
	road := newSyncRoad(link, "Desktop")
	t.Cleanup(road.Close)

	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	road.touch = func() {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			close(finished)
		}
	}

	// While Idle, Touch() is a no-op because Reconnect() is the door out of idle.
	road.onState(livedoor.State{Kind: livedoor.Idle})
	for i := 0; i < 5; i++ {
		road.Touch()
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("link.Touch calls while Idle = %d, want 0", got)
	}

	// While Live, ten Touch() calls while the first is in flight produce one link.Touch call.
	road.onState(livedoor.State{Kind: livedoor.Live})
	road.Touch()
	<-entered
	for i := 0; i < 9; i++ {
		road.Touch()
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("link.Touch calls while first touch in flight = %d, want 1", got)
	}
	close(release)
	<-finished
	if got := calls.Load(); got != 1 {
		t.Fatalf("link.Touch calls after first touch finished = %d, want 1", got)
	}
}

// V4: scrolling up on a live chat pages older entries through the agent's own connection when a pager is present and never calls the road.
func TestALiveAgentPagesOlderEntriesThroughItsOwnConnection(t *testing.T) {
	road := newFakeLiveRoad()
	wantEntries := []session.DisplayEntry{
		{Role: "user", Text: "older question"},
		{Role: "assistant", Text: "older answer", Answer: true},
	}
	var gotBefore, gotLimit int
	agent := &liveAgent{
		Agent: &fakeLivePeerAgent{},
		road:  road,
		cell:  "cell-paged",
		pager: func(_ context.Context, before, limit int) (remote.Page, error) {
			gotBefore = before
			gotLimit = limit
			return remote.Page{
				Entries: wantEntries,
				Base:    158,
				More:    true,
			}, nil
		},
	}

	entries, more, err := agent.Older(context.Background(), 160, 2)
	if err != nil {
		t.Fatalf("Older: %v", err)
	}
	if gotBefore != 160 || gotLimit != 2 {
		t.Fatalf("pager called with (before=%d, limit=%d), want (160, 2)", gotBefore, gotLimit)
	}
	if !more || len(entries) != 2 || entries[0].Text != wantEntries[0].Text || entries[1].Text != wantEntries[1].Text {
		t.Fatalf("Older() = (%+v, more=%v), want (%+v, true)", entries, more, wantEntries)
	}
	if got := agent.TranscriptBase(); got != 158 {
		t.Fatalf("TranscriptBase() = %d, want 158", got)
	}
	if len(road.olderCalls) != 0 {
		t.Fatalf("road.Older was called %d times when pager was set, want 0", len(road.olderCalls))
	}
}

// V30, V41: the road the view reads is the road its link reports, on every draw,
// and a flip of the road with the same kind of state still wakes the surface so
// the next frame shows it; a road the link has not named reads as nothing.
func TestSyncRoadReadsTheRoadTheLinkReportsAndWakesTheSurfaceOnAFlip(t *testing.T) {
	link := newTestLiveLink()
	road := newSyncRoad(link, "Desktop")
	t.Cleanup(road.Close)

	if got := road.Read().Road; got != "" {
		t.Fatalf("before the link says anything the road reads %q, want nothing", got)
	}
	for _, step := range []string{"relay", "direct", "relay", "direct"} {
		woke := road.Changed()
		road.onState(livedoor.State{Kind: livedoor.Live, Road: step})
		select {
		case <-woke:
		default:
			t.Fatalf("a flip to %q did not wake the surface", step)
		}
		if got := road.Read(); got.Road != step || got.Kind != tui3.LiveOn {
			t.Fatalf("after a flip to %q the view reads %+v", step, got)
		}
	}
}
