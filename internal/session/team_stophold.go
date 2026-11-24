package session

// A stop in a team holds until a turn next begins.
//
// An interrupt leaves background commands running (jobstop.go), and a
// command's ending is news that starts a turn ([Agent.enqueueJobNote]). In a
// team the two together undid the stop. Measured 2026-11-24 in the desktop
// app: the person stopped two members that had each started `sleep 200` with
// notify, the manager was told the work was stopped and not to hand it out
// again, and three minutes later both commands ended, woke their members and
// each reported the stopped work done. A crew's Stop used to leave a pause
// that ended such wakes (cmd/codeaf's appwakes.go); it no longer pauses.
//
// So in a team, from a stop of a running turn until a turn next begins, the
// ending of a background command and a watch's firing are kept as a record and
// start no turn. The stop is the one that comes through the person's door: the
// person's own, and a manager's team_stop, which an interface performs through
// that door (teamevent.go). The command itself is left running, as an interrupt
// always leaves it. What starts the conversation again is somebody meaning to:
// the person's own words, or the manager's next hand-off.
//
// A conversation in no team is untouched: there the ending of a command the
// person asked to hear about still starts its turn.

import (
	"strings"
	"sync/atomic"
)

// teamStopHeldRule is said with a command's ending that started no turn.
const teamStopHeldRule = "This arrived after this conversation's turn was stopped, so it started no turn. It is a record of what happened: do not take the stopped work up again because of it."

// teamStopHold is one conversation's hold.
type teamStopHold struct {
	// seated is whether this conversation has a role in any team, as of the
	// last time its roles were read. Read without a lock or a disk.
	seated atomic.Bool
	// held is set by a stop of a running turn through the person's door and
	// cleared when a turn next begins.
	held atomic.Bool
}

// teamStopHoldLocked begins the hold when the stop came through the person's
// door and a turn is running. The caller holds a.mu.
func (a *Agent) teamStopHoldLocked(door StopDoor) {
	if door != StopByPerson || !a.running || !a.team.stopHold.seated.Load() {
		return
	}
	a.team.stopHold.held.Store(true)
}

// teamStopHoldOver ends the hold: a turn has begun.
func (a *Agent) teamStopHoldOver() { a.team.stopHold.held.Store(false) }

// teamStopHeldNote keeps one piece of background news as a record when the
// hold stands, and reports whether it did. A worker parked on the command is
// released with it, as the waking note would have released it.
func (a *Agent) teamStopHeldNote(text string) bool {
	if !a.team.stopHold.held.Load() {
		return false
	}
	a.mu.Lock()
	a.releaseTaskWaitLocked()
	a.mu.Unlock()
	a.enqueueJobNoteQuiet(strings.TrimSpace(text) + "\n" + teamStopHeldRule)
	return true
}
