package directory

import (
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"testing"
	"time"
)

// moving is a clock a test moves by hand, with timers that remember how long
// they were set for, so a case can read when the sweep is armed.
type moving struct {
	now     time.Time
	armed   []time.Duration
	pending []*fakeTimer
}

func (m *moving) clock() time.Time { return m.now }

func (m *moving) after(d time.Duration, fn func()) func() bool {
	m.armed = append(m.armed, d)
	ft := &fakeTimer{fn: fn}
	m.pending = append(m.pending, ft)
	return func() bool { was := !ft.stopped; ft.stopped = true; return was }
}

// live is the timers not stopped and not yet run.
func (m *moving) live() (n int) {
	for _, ft := range m.pending {
		if !ft.stopped {
			n++
		}
	}
	return n
}

// advance moves the clock by d and runs the timers set so far, as one wake would.
func (m *moving) advance(d time.Duration) {
	m.now = m.now.Add(d)
	pending := m.pending
	for _, ft := range pending {
		if !ft.stopped {
			ft.stopped = true
			ft.fn()
		}
	}
}

func newMovingFeed() (*Feed, *moving) {
	m := &moving{now: time.UnixMilli(1_000_000)}
	f := NewFeed(m.clock)
	f.SetAfter(m.after)
	return f, m
}

func joinBeat(t *testing.T, f *Feed, device string, beat time.Duration, events bool) (*Sub, <-chan dirwatch.Event) {
	t.Helper()
	s, err := f.SubscribeBeat(device, nil, beat)
	if err != nil {
		t.Fatal(err)
	}
	if !events {
		return s, nil
	}
	return s, s.Events()
}

func TestParseBeat(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want time.Duration
		ok   bool
	}{
		{nil, DefaultBeat, true},
		{[]string{"1"}, time.Second, true},
		{[]string{"60"}, 60 * time.Second, true},
		{[]string{"10", "10"}, 10 * time.Second, true},
		{[]string{"0"}, 0, false},
		{[]string{"61"}, 0, false},
		{[]string{"x"}, 0, false},
		{[]string{""}, 0, false},
		{[]string{"+5"}, 0, false},
		{[]string{"1.5"}, 0, false},
		{[]string{"5", "6"}, 0, false},
	} {
		got, err := ParseBeat(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ParseBeat(%q) = %v, %v; want %v ok=%v", tc.in, got, err, tc.want, tc.ok)
		}
	}
}

func TestWindowIsTwoAndAHalfBeats(t *testing.T) {
	if got := Window(10 * time.Second); got != 25*time.Second {
		t.Fatalf("window of a 10 s beat is %v, want 25s", got)
	}
	if got := Window(DefaultBeat); got != 75*time.Second {
		t.Fatalf("window of the default beat is %v, want 75s", got)
	}
}

func TestASocketIsLiveForItsWindowAndThenCountsForNothing(t *testing.T) {
	f, m := newMovingFeed()
	joinBeat(t, f, "dev_a", 2*time.Second, false)
	m.advance(5 * time.Second)
	if !f.Online("dev_a") {
		t.Fatal("a socket is live at exactly its window")
	}
	m.advance(time.Millisecond)
	if f.Online("dev_a") || len(f.OnlineSet()) != 0 {
		t.Fatal("a socket past its window is not online, and a read needs no timer to say so")
	}
}

func TestAPingKeepsASocketLive(t *testing.T) {
	f, m := newMovingFeed()
	s, _ := joinBeat(t, f, "dev_a", 2*time.Second, false)
	for range 5 {
		m.advance(2 * time.Second)
		s.Ping()
	}
	if !f.Online("dev_a") {
		t.Fatal("a socket that pings every beat stays online")
	}
}

func TestNoSweepWithoutAViewer(t *testing.T) {
	f, m := newMovingFeed()
	joinBeat(t, f, "dev_a", 2*time.Second, false)
	if len(m.armed) != 0 {
		t.Fatalf("an identity with no event socket set an alarm: %v", m.armed)
	}
}

func TestTheSweepIsSetAtTheEarliestLapse(t *testing.T) {
	f, m := newMovingFeed()
	joinBeat(t, f, "dev_v", 60*time.Second, true)
	joinBeat(t, f, "dev_a", 2*time.Second, false)
	last := m.armed[len(m.armed)-1]
	if want := 5*time.Second + time.Millisecond; last != want {
		t.Fatalf("sweep set for %v, want %v", last, want)
	}
}

func TestTheSweepIsNeverSoonerThanASecond(t *testing.T) {
	f, m := newMovingFeed()
	joinBeat(t, f, "dev_a", 2*time.Second, false)
	m.advance(time.Minute) // long past its lapse, nobody swept
	joinBeat(t, f, "dev_v", 60*time.Second, true)
	if last := m.armed[len(m.armed)-1]; last != time.Second {
		t.Fatalf("sweep set for %v, want 1s", last)
	}
}

func TestTheSweepAnnouncesOnceClosesTheSocketsAndStampsLastSign(t *testing.T) {
	f, m := newMovingFeed()
	var stamps []int64
	f.OnSeen(func(d string, at int64) {
		if d == "dev_a" {
			stamps = append(stamps, at)
		}
	})
	_, viewer := joinBeat(t, f, "dev_v", 60*time.Second, true)
	a, _ := joinBeat(t, f, "dev_a", 2*time.Second, false)
	same(t, drain(viewer), []string{"presence:dev_a:on"})
	m.advance(2 * time.Second)
	a.Ping()
	signOfLife := m.now.UnixMilli()
	m.advance(7 * time.Second)
	same(t, drain(viewer), []string{"presence:dev_a:off"})
	if len(stamps) != 2 || stamps[1] != signOfLife {
		t.Fatalf("last_seen stamps %v, want connect then the last sign of life %d", stamps, signOfLife)
	}
	select {
	case <-a.silence:
	default:
		t.Fatal("the lapsed socket was not told to go")
	}
	a.Close() // the goroutine of the closed socket leaves: no gap, nothing announced
	if m.live() != 1 {
		t.Fatalf("%d timers pending after a lapsed socket closed, want only the sweep", m.live())
	}
	m.advance(OfflineDebounce)
	same(t, drain(viewer), nil)
}

func TestALapsedSocketIsNeverLiveAgain(t *testing.T) {
	f, m := newMovingFeed()
	joinBeat(t, f, "dev_v", 60*time.Second, true)
	a, _ := joinBeat(t, f, "dev_a", 2*time.Second, false)
	m.advance(6 * time.Second)
	a.Ping()
	if f.Online("dev_a") {
		t.Fatal("a ping after the lapse brought the socket back")
	}
}

func TestADeviceThatReturnsAfterTheLapseIsAnnouncedOnline(t *testing.T) {
	f, m := newMovingFeed()
	_, viewer := joinBeat(t, f, "dev_v", 60*time.Second, true)
	joinBeat(t, f, "dev_a", 2*time.Second, false)
	m.advance(6 * time.Second)
	drain(viewer)
	joinBeat(t, f, "dev_a", 2*time.Second, false)
	same(t, drain(viewer), []string{"presence:dev_a:on"})
}

func TestOneLiveSocketKeepsTheDeviceOnlineAndUnannounced(t *testing.T) {
	f, m := newMovingFeed()
	_, viewer := joinBeat(t, f, "dev_v", 60*time.Second, true)
	joinBeat(t, f, "dev_a", 2*time.Second, false)
	b, _ := joinBeat(t, f, "dev_a", 2*time.Second, false)
	drain(viewer)
	m.advance(3 * time.Second)
	b.Ping()
	m.advance(3 * time.Second)
	if !f.Online("dev_a") {
		t.Fatal("one live socket is enough")
	}
	same(t, drain(viewer), nil)
}

func TestTheSweepIsSetAgainWhileAViewerAndALiveSocketRemain(t *testing.T) {
	f, m := newMovingFeed()
	joinBeat(t, f, "dev_v", 60*time.Second, true)
	a, _ := joinBeat(t, f, "dev_a", 2*time.Second, false)
	m.advance(4 * time.Second)
	a.Ping()
	n := len(m.armed)
	m.advance(2 * time.Second) // the first alarm: nothing lapsed, a later one is set
	if len(m.armed) != n+1 || m.armed[n] != 3*time.Second+time.Millisecond {
		t.Fatalf("armed %v after the first alarm, want one more at the new earliest lapse", m.armed)
	}
}

func TestNoAlarmRemainsOnceTheLastViewerLeaves(t *testing.T) {
	f, _ := newMovingFeed()
	v, _ := joinBeat(t, f, "dev_v", 60*time.Second, true)
	joinBeat(t, f, "dev_a", 2*time.Second, false)
	if !f.pres.alarm.armed() {
		t.Fatal("no alarm set with a viewer")
	}
	v.Close()
	if f.pres.alarm.armed() {
		t.Fatal("an alarm is left after the last viewer went")
	}
}

func TestAStaleSocketThatClosesBeforeTheSweepIsAnnouncedAtOnce(t *testing.T) {
	f, m := newMovingFeed()
	_, viewer := joinBeat(t, f, "dev_v", 60*time.Second, true)
	a, _ := joinBeat(t, f, "dev_a", 2*time.Second, false)
	drain(viewer)
	m.now = m.now.Add(6 * time.Second) // lapsed, and the alarm has not run
	a.Close()
	same(t, drain(viewer), []string{"presence:dev_a:off"})
}
