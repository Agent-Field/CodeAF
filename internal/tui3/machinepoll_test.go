package tui3

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// countingSource is a chat list source that counts how often it is asked and
// answers with whatever rows it holds, so a test can read the bill directly.
type countingSource struct {
	asks int
	rows chatlist.Static
}

func (c *countingSource) Rows(ctx context.Context) ([]chatlist.Row, error) {
	c.asks++
	return c.rows.Rows(ctx)
}

// pollHome is a home on a clock the test owns, reading a counting source.
func pollHome(t *testing.T) (*app, *countingSource, *time.Time) {
	t.Helper()
	src := &countingSource{rows: chatlist.Static{{Cell: "c1", Title: "one", Status: chatlist.Idle}}}
	a := machinesHome(t, src, 200)
	now := time.Unix(1_000_000, 0)
	a.clock = func() time.Time { return now }
	a.lastQuestionKey = now
	// The fixture's first ask was scheduled on the wall clock; the script starts
	// from a schedule that is due on the test's own.
	a.machinePoll.hurry()
	src.asks = 0
	return a, src, &now
}

// beat runs one home beat after the clock moved by d.
func beat(t *testing.T, a *app, now *time.Time, d time.Duration) {
	t.Helper()
	*now = now.Add(d)
	a.lastQuestionKey = *now
	drain(t, a, a.homeBeat(a.homeGen))
}

// TestPollsPerOpenHour is the scripted home session: an hour on home with
// nothing changing, one beat every homeEvery. It logs the bill.
func TestPollsPerOpenHour(t *testing.T) {
	a, src, now := pollHome(t)
	for elapsed := time.Duration(0); elapsed < time.Hour; elapsed += homeEvery {
		beat(t, a, now, homeEvery)
	}
	t.Logf("directory lists in one open hour, nothing changing: %d (1200 at one ask per beat)", src.asks)
	if src.asks > 70 {
		t.Fatalf("an unchanging hour asked the directory %d times, want about one a minute", src.asks)
	}
}

// TestHiddenListNeverPolls: with home not showing, a beat that is due, in a
// window a person is at, still asks nothing.
func TestHiddenListNeverPolls(t *testing.T) {
	a, src, now := pollHome(t)
	a.page = pageChats
	for i := 0; i < 100; i++ {
		*now = now.Add(machinesCap)
		drain(t, a, a.pollMachines(*now))
	}
	if src.asks != 0 {
		t.Fatalf("a list nobody can see was asked for %d times", src.asks)
	}
}

// TestUnattendedListNeverPolls: on home but with the terminal reporting that
// it has lost focus, nothing asks; with focus back, it asks at once.
func TestUnattendedListNeverPolls(t *testing.T) {
	a, src, now := pollHome(t)
	drive(t, a, tea.BlurMsg{})
	for i := 0; i < 50; i++ {
		beat(t, a, now, machinesCap)
	}
	if src.asks != 0 {
		t.Fatalf("an unfocused window asked %d times", src.asks)
	}
}

// TestKeylessTerminalGoesQuietAfterAwayWindow: a terminal that never reports
// focus is judged by its last key.
func TestKeylessTerminalGoesQuietAfterAwayWindow(t *testing.T) {
	a, src, now := pollHome(t)
	*now = now.Add(awayAfter + time.Second)
	drain(t, a, a.homeBeat(a.homeGen))
	if src.asks != 0 {
		t.Fatalf("a window nobody has touched for %v asked %d times", awayAfter, src.asks)
	}
}

// TestUnchangedListBacksOffToTheCap: repeats climb the ladder and stay on the
// top rung.
func TestUnchangedListBacksOffToTheCap(t *testing.T) {
	a, src, now := pollHome(t)
	var gaps []time.Duration
	last := *now
	for elapsed := time.Duration(0); elapsed < 20*time.Minute; elapsed += time.Second {
		before := src.asks
		beat(t, a, now, time.Second)
		if src.asks > before {
			gaps = append(gaps, now.Sub(last))
			last = *now
		}
	}
	want := []time.Duration{3, 6, 12, 24, 48, 60, 60, 60}
	// The first ask is the one made on arrival, so the ladder starts after it.
	gaps = gaps[1:]
	for i, w := range want {
		if gaps[i] != w*time.Second {
			t.Fatalf("gap %d was %v, want %v (all: %v)", i, gaps[i], w*time.Second, gaps)
		}
	}
}

// TestChangeResetsToFast: an answer that differs sends the wait back to the
// fast pace after a long calm.
func TestChangeResetsToFast(t *testing.T) {
	a, src, now := pollHome(t)
	for i := 0; i < 600; i++ {
		beat(t, a, now, time.Second)
	}
	src.rows = chatlist.Static{{Cell: "c1", Title: "one", Status: chatlist.Idle, DurableAt: 2000}}
	before := src.asks
	for src.asks == before {
		beat(t, a, now, time.Second)
	}
	changedAt := *now
	before = src.asks
	for src.asks == before {
		beat(t, a, now, time.Second)
	}
	if got := now.Sub(changedAt); got != machinesFast {
		t.Fatalf("after a change the next ask came %v later, want %v", got, machinesFast)
	}
}

// TestFocusRefreshesAtOnce: after a long calm, gaining focus asks in the same
// message, not on the next beat.
func TestFocusRefreshesAtOnce(t *testing.T) {
	a, src, now := pollHome(t)
	for i := 0; i < 600; i++ {
		beat(t, a, now, time.Second)
	}
	drive(t, a, tea.BlurMsg{})
	before := src.asks
	drive(t, a, tea.FocusMsg{})
	if src.asks != before+1 {
		t.Fatalf("focus asked %d times, want 1", src.asks-before)
	}
}

// TestRowAgeComesFromTheTurnNotThePoll: a row read once and drawn much later
// shows the time since the turn, not the time since the ask.
func TestRowAgeComesFromTheTurnNotThePoll(t *testing.T) {
	readAt := time.Unix(1_000_000, 0)
	row := chatlist.Row{Cell: "c9", Title: "Elsewhere", Status: chatlist.Idle, DurableAgo: 5 * time.Minute}
	soon := machineLine(row, readAt, readAt, false).cell.right
	later := machineLine(row, readAt, readAt.Add(30*time.Minute), false).cell.right
	if soon != "5m" || later != "35m" {
		t.Fatalf("age drawn at the read was %q and half an hour on %q, want 5m and 35m", soon, later)
	}
}

// TestRowAgeNeverYoungerThanTheDirectorySaid: a draw stamped a hair before the
// read still shows the age the directory gave.
func TestRowAgeNeverYoungerThanTheDirectorySaid(t *testing.T) {
	readAt := time.Unix(1_000_000, 0)
	row := chatlist.Row{Cell: "c9", Title: "Elsewhere", Status: chatlist.Idle, DurableAgo: 5 * time.Hour}
	if got := machineLine(row, readAt, readAt.Add(-time.Millisecond), false).cell.right; got != "5h" {
		t.Fatalf("age drawn a millisecond before the read was %q, want 5h", got)
	}
}
