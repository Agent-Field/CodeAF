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
//
// The stop may also find no turn running. A member commonly starts a long
// command in the background, asks to be told when it ends, and ends its turn:
// measured the same day, a crew's Stop passed over such a member and it
// reported the work done when its command ended. So a stop that finds no turn
// but a command or a watch still running begins the hold too, and the manager
// that handed out the work the last turn carried is told it was stopped, in
// the words a stopped turn uses.

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// teamStopHeldRule is said with a command's ending that started no turn.
const teamStopHeldRule = "This arrived after this conversation's turn was stopped, so it started no turn. It is a record of what happened: do not take the stopped work up again because of it."

// teamStopHold is one conversation's hold.
type teamStopHold struct {
	// seated is whether this conversation has a role in any team, as of the
	// last time its roles were read. Read without a lock or a disk.
	seated atomic.Bool
	// held is set by a stop through the person's door and cleared when a turn
	// next begins.
	held atomic.Bool

	mu sync.Mutex
	// last is what the turn that last finished had been handed, per team, and
	// origin is that turn's. It is what a stop that finds no turn running is
	// the stop of. A turn beginning forgets it.
	last   map[string]time.Time
	origin turnOrigin
}

// teamStopHoldLocked begins the hold when the stop came through the person's
// door and there is something to hold: a turn running, or a command or watch
// whose ending would be news. It reports whether it began with no turn running,
// which is the stop the manager has yet to be told of ([Agent.teamStopHoldSaid]).
// The caller holds a.mu.
func (a *Agent) teamStopHoldLocked(door StopDoor) bool {
	hold := &a.team.stopHold
	if door != StopByPerson || !hold.seated.Load() {
		return false
	}
	if a.running {
		hold.held.Store(true)
		return false
	}
	if a.jobs == nil || !a.jobs.newsPending() {
		return false
	}
	return !hold.held.Swap(true)
}

// teamStopHoldOver ends the hold: a turn has begun.
func (a *Agent) teamStopHoldOver() {
	hold := &a.team.stopHold
	hold.held.Store(false)
	hold.mu.Lock()
	hold.last = nil
	hold.mu.Unlock()
}

// teamStopHoldFinished remembers what a turn that finished had been handed.
func (a *Agent) teamStopHoldFinished(handed map[string]time.Time, origin turnOrigin) {
	hold := &a.team.stopHold
	hold.mu.Lock()
	hold.last, hold.origin = handed, origin
	hold.mu.Unlock()
}

// teamStopHoldSaid tells the managers whose work the last turn carried that
// it was stopped while it waited on a command. It reads the teams' logs, so it
// runs away from the stop's own path.
func (a *Agent) teamStopHoldSaid() {
	hold := &a.team.stopHold
	hold.mu.Lock()
	handed, origin := hold.last, hold.origin
	hold.last = nil
	hold.mu.Unlock()
	if len(handed) == 0 {
		return
	}
	guard.Go("team stop of waiting work", func() {
		by := a.teamStoppedByPerson(StopByPerson, handed)
		if len(by) == 0 {
			return
		}
		a.teamEventOwedOnly(origin, teams.StateIdle, "stopped", by)
	})
}

// newsPending reports whether a background command or a watch is still
// running: something whose ending would start a turn.
func (r *jobRegistry) newsPending() bool {
	for _, one := range r.all() {
		if one.kind != jobKindBash && one.kind != jobKindWatch {
			continue
		}
		select {
		case <-one.done:
		default:
			return true
		}
	}
	return false
}

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
