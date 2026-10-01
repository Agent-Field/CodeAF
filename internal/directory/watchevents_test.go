package directory_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// eventRig is a real handler over real sockets, with the feeds in reach.
type eventRig struct {
	rig
	dirs *namespaces
}

func newEventRig(t *testing.T) eventRig {
	t.Helper()
	clock := directorytest.NewFakeClock()
	dirs := newNamespaces(clock)
	srv := httptest.NewServer(directory.Handler(fakeAuth, dirs.open))
	t.Cleanup(srv.Close)
	return eventRig{rig{srv: srv, clock: clock}, dirs}
}

func read(t *testing.T, s dirwatch.Stream) dirwatch.Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f, err := s.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func watcher(t *testing.T, c directory.Client) dirwatch.Stream {
	t.Helper()
	s, err := c.(directory.Watcher).Watch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestAnEventSocketHearsTheVersionThenWhoIsOnlineThenWhoComes(t *testing.T) {
	g := newEventRig(t)
	a := watcher(t, g.as("id_one", devA))
	if f := read(t, a); f.Event != nil || f.Pong {
		t.Fatalf("the first frame was not a version: %+v", f)
	}
	watcher(t, g.as("id_one", devB))
	f := read(t, a)
	if f.Event == nil || f.Event.T != dirwatch.EventPresence || f.Event.Device != devB || !*f.Event.Online {
		t.Fatalf("a was not told b is online: %+v", f)
	}
}

func TestANewEventSocketIsToldWhoWasAlreadyOnlineRightAfterTheVersion(t *testing.T) {
	g := newEventRig(t)
	watcher(t, g.as("id_one", devA))
	b := watcher(t, g.as("id_one", devB))
	if f := read(t, b); f.Event != nil {
		t.Fatalf("an event came before the version: %+v", f)
	}
	f := read(t, b)
	if f.Event == nil || f.Event.Device != devA || !*f.Event.Online {
		t.Fatalf("b was not told a is online: %+v", f)
	}
}

func TestASocketThatNamesHoldsAndSkipsEventsHearsNoEvent(t *testing.T) {
	g := newEventRig(t)
	holder, err := g.as("id_one", devA).(directory.HoldWatcher).WatchHolding(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	read(t, holder)
	watcher(t, g.as("id_one", devB))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if f, err := holder.Next(ctx); err == nil {
		t.Fatalf("a socket without events heard %+v", f)
	}
}

func TestAFollowerStateShowsOnlinePeers(t *testing.T) {
	g := newEventRig(t)
	c := g.as("id_one", devA)
	f := dirwatch.Follow(t.Name(), c.(directory.Watcher).Watch)
	defer f.Close()
	watcher(t, g.as("id_one", devB))
	waitFor(t, f, func(s dirwatch.State) bool { return len(s.Online) == 1 && s.Online[0] == devB })
}

func waitFor(t *testing.T, f dirwatch.Follower, ok func(dirwatch.State) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !ok(f.State()) {
		select {
		case <-f.Changes():
		case <-deadline:
			t.Fatalf("state never matched: %+v", f.State())
		}
	}
}

func TestPresenceAnswersWhoIsOnlineAndWhenTheOthersWereSeen(t *testing.T) {
	g := newEventRig(t)
	ctx := context.Background()
	a, b := g.as("id_one", devA), g.as("id_one", devB)
	for _, c := range []directory.Client{a, b} {
		if err := c.PutDevice(ctx, map[directory.Client]string{a: devA, b: devB}[c], directory.Device{V: 1}); err != nil {
			t.Fatal(err)
		}
	}
	s := watcher(t, b)
	read(t, s)
	g.clock.Advance(time.Minute)
	v, err := a.(interface {
		Presence(context.Context) (directory.PresenceView, error)
	}).Presence(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Devices[devB].Online || v.Devices[devB].LastSeen != v.Now || v.Devices[devA].Online || v.Version == 0 {
		t.Fatalf("presence is %+v", v)
	}
}
