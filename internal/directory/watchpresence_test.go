package directory

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// timers stands in for the clock's timers: it holds the gaps started and runs them on demand.
type timers struct{ pending []*fakeTimer }

type fakeTimer struct {
	fn      func()
	stopped bool
}

func (t *timers) after(_ time.Duration, fn func()) func() bool {
	ft := &fakeTimer{fn: fn}
	t.pending = append(t.pending, ft)
	return func() bool { was := !ft.stopped; ft.stopped = true; return was }
}

// fire runs every timer not stopped, as the clock would once the debounce passed.
func (t *timers) fire() {
	for _, ft := range t.pending {
		if !ft.stopped {
			ft.stopped = true
			ft.fn()
		}
	}
}

func newTestFeed() (*Feed, *timers) {
	f := NewFeed(func() time.Time { return time.UnixMilli(5000) })
	ts := &timers{}
	f.pres.after = ts.after
	return f, ts
}

// join subscribes device, as an event socket when events is set, and answers the sub and its frames.
func join(t *testing.T, f *Feed, device string, events bool) (*Sub, <-chan dirwatch.Event) {
	t.Helper()
	s, err := f.Subscribe(device, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !events {
		return s, nil
	}
	return s, s.Events()
}

// drain returns what is queued without waiting.
func drain(ch <-chan dirwatch.Event) (got []string) {
	for {
		select {
		case e := <-ch:
			got = append(got, describe(e))
		default:
			return got
		}
	}
}

func describe(e dirwatch.Event) string {
	switch {
	case e.Online != nil && *e.Online:
		return e.T + ":" + e.Device + ":on"
	case e.Online != nil:
		return e.T + ":" + e.Device + ":off"
	}
	return e.T + ":" + e.Device
}

func same(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFirstSocketIsNewsAndTheNewcomerHearsWhoIsOnline(t *testing.T) {
	f, _ := newTestFeed()
	_, a := join(t, f, "dev_a", true)
	_, b := join(t, f, "dev_b", true)
	same(t, drain(a), []string{"presence:dev_b:on"})
	same(t, drain(b), []string{"presence:dev_a:on"})
	join(t, f, "dev_b", true)
	same(t, drain(a), nil) // a second socket of a device is not news
	if !f.Online("dev_b") || len(f.OnlineSet()) != 2 {
		t.Fatalf("online set is %v", f.OnlineSet())
	}
}

func TestASocketWithoutEventsHearsNothing(t *testing.T) {
	f, _ := newTestFeed()
	old, _ := join(t, f, "dev_a", false)
	join(t, f, "dev_b", true)
	f.announceJoin(Joined{Device: "dev_c", Name: "bg==", Platform: "linux"})
	if len(old.events) != 0 {
		t.Fatalf("a socket that did not opt in has %d queued events", len(old.events))
	}
}

func TestOfflineWaitsForTheGapAndOnlyTheLastSocketStartsIt(t *testing.T) {
	f, ts := newTestFeed()
	_, a := join(t, f, "dev_a", true)
	b1, _ := join(t, f, "dev_b", true)
	b2, _ := join(t, f, "dev_b", true)
	drain(a)
	b1.Close()
	b1.Close() // closing twice counts once
	ts.fire()
	same(t, drain(a), nil)
	if !f.Online("dev_b") {
		t.Fatal("one socket left, the device is online")
	}
	b2.Close()
	same(t, drain(a), nil) // still inside the gap
	if f.Online("dev_b") {
		t.Fatal("no socket, the device is not online even inside the gap")
	}
	ts.fire()
	same(t, drain(a), []string{"presence:dev_b:off"})
}

func TestAReconnectInsideTheGapSaysNothing(t *testing.T) {
	f, ts := newTestFeed()
	_, a := join(t, f, "dev_a", true)
	b, _ := join(t, f, "dev_b", true)
	drain(a)
	b.Close()
	join(t, f, "dev_b", true)
	ts.fire()
	same(t, drain(a), nil)
}

func TestASocketOpenedInsideAGapStillHearsTheDeviceOnline(t *testing.T) {
	f, ts := newTestFeed()
	join(t, f, "dev_a", true)
	b, _ := join(t, f, "dev_b", true)
	b.Close()
	_, c := join(t, f, "dev_c", true)
	same(t, drain(c), []string{"presence:dev_a:on", "presence:dev_b:on"})
	ts.fire()
	same(t, drain(c), []string{"presence:dev_b:off"})
}

func TestJoinedGoesToOthersNotToTheNewDevice(t *testing.T) {
	f, _ := newTestFeed()
	_, a := join(t, f, "dev_a", true)
	_, c := join(t, f, "dev_c", true)
	drain(a)
	drain(c)
	f.announceJoin(Joined{Device: "dev_c", Name: "bg==", Platform: "linux"})
	same(t, drain(a), []string{"joined:dev_c"})
	same(t, drain(c), nil)
}

func TestPublishAnnouncesANewlyRevokedDeviceOnce(t *testing.T) {
	f, _ := newTestFeed()
	_, a := join(t, f, "dev_a", true)
	drain(a)
	f.Publish(Status{Version: 1, Stopped: map[string]bool{"dev_b": true}})
	f.Publish(Status{Version: 2, Stopped: map[string]bool{"dev_b": true}})
	same(t, drain(a), []string{"revoked:dev_b"})
}

func TestSeenIsStampedAtEachConnectAndAtTheLastCloseOnly(t *testing.T) {
	f, _ := newTestFeed()
	var stamps []string
	f.OnSeen(func(d string, at int64) { stamps = append(stamps, d) })
	a1, _ := join(t, f, "dev_a", false)
	a2, _ := join(t, f, "dev_a", false)
	same(t, stamps, []string{"dev_a", "dev_a"})
	a1.Close()
	same(t, stamps, []string{"dev_a", "dev_a"})
	a2.Close()
	same(t, stamps, []string{"dev_a", "dev_a", "dev_a"})
}
