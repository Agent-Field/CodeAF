package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// ── WHEN HOME ASKS THE DIRECTORY ────────────────────────────────────────────
//
// The list of chats on other machines is a network read, and on a hosted relay
// every read is billed. Home's own beat re-reads the local disk every three
// seconds because that is free; the directory is not, so it gets its own pace
// and its own permission to ask, decided here and nowhere else.
//
// IT ASKS ONLY WHILE SOMEBODY IS LOOKING. The list is drawn on home, so away
// from home nothing asks; and on home it asks only while the window is
// attended, which is the terminal's own focus report where there is one and
// otherwise a key pressed lately.
//
// IT SLOWS DOWN WHILE NOTHING CHANGES. A list that answers the same twice is a
// list nobody is editing, so the wait doubles from the fast pace up to a
// minute, and the first answer that differs drops it back to fast.
//
// IT HURRIES WHEN A PERSON ARRIVES. Opening home or coming back to the window
// asks at once, so the screen is never a minute stale at the moment it is read.

const (
	// machinesFast is the wait after an answer that changed, and it is home's
	// own beat: no faster than the local read that sits beside it.
	machinesFast = homeEvery
	// machinesCap is the longest wait, one ask a minute.
	machinesCap = time.Minute
	// machinesBackstop is the wait between asks while the change socket is
	// healthy. It is the same minute as the cap: a list nobody is editing is
	// read once a minute whether or not a socket is listening.
	machinesBackstop = machinesCap
)

// pace is the ladder of waits. Its zero value is ready to use and starts fast.
type pace struct{ wait time.Duration }

// settle takes what the last answer said and returns how long to wait before
// the next ask: fast after a change, twice as long after a repeat, never past
// the cap.
func (p *pace) settle(changed bool) time.Duration {
	if changed || p.wait == 0 {
		p.wait = machinesFast
	} else {
		p.wait *= 2
	}
	p.wait = cappedAt(p.wait, machinesCap)
	return p.wait
}

// cappedAt is the shorter of two waits.
func cappedAt(wait, limit time.Duration) time.Duration {
	if wait > limit {
		return limit
	}
	return wait
}

// machinePoll is the schedule: the ladder and the moment the next ask is due.
type machinePoll struct {
	pace pace
	due  time.Time
}

// ready reports whether the wait is over.
func (m *machinePoll) ready(now time.Time) bool { return !now.Before(m.due) }

// landed files an answer: the next ask is due one rung after it arrived, or,
// while the change socket is healthy, at the backstop, because the socket says
// when something changed and the ask is only there for a frame that was lost.
func (m *machinePoll) landed(now time.Time, changed, watched bool) {
	if watched {
		m.due = now.Add(machinesBackstop)
		return
	}
	m.due = now.Add(m.pace.settle(changed))
}

// hurry makes the next ask due now, at the fast pace.
func (m *machinePoll) hurry() { *m = machinePoll{} }

// attended says whether a person is at this window. A terminal that reports
// focus is believed; one that does not is judged by its last key.
func (a *app) attended(now time.Time) bool {
	if a.seenFocus {
		return a.focused
	}
	return now.Sub(a.lastQuestionKey) < awayAfter
}

// pollMachines asks the directory if, and only if, the schedule allows: home is
// showing, somebody is at the window, and the wait is over.
func (a *app) pollMachines(now time.Time) tea.Cmd {
	wanted := a.at(pageHome) && a.attended(now)
	followed := a.tendWatch(wanted)
	if !wanted || !a.machinePoll.ready(now) {
		return followed
	}
	return tea.Batch(followed, a.askMachines())
}

// hurryMachines asks now, for a person who has just arrived.
func (a *app) hurryMachines() tea.Cmd {
	a.machinePoll.hurry()
	return a.pollMachines(a.now())
}

// fileMachines records an answer's effect on the schedule and the reading.
// An answer that failed says nothing new, so it backs off like a repeat.
func (a *app) fileMachines(msg homeMachinesMsg) {
	now := a.now()
	changed := msg.err == nil && !chatlist.Same(a.machineRead.rows, msg.rows)
	a.machinePoll.landed(now, changed, a.socketUp())
	if msg.err == nil {
		a.machineRead = machineReading{rows: msg.rows, at: now, version: msg.version}
	}
}
