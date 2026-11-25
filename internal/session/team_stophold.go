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
// ending of a background command, a watch's firing and a background task's
// landing are kept as a record and start no turn. The hold is one row of the table that decides every wake
// ([wakeStanding]) and is read where the ending would be queued, under the
// lock a stop takes ([Agent.enqueueNote]), so no ending slips between a stop
// and its hold. The stop is the person's own or a manager's team_stop, whoever
// performs the latter: an interface, through the person's door (teamevent.go),
// or the member's own session, through the manager's ([Agent.interruptTeamTurn]
// for a turn, [Agent.teamStopWaiting] for a member with none).
// The command itself is left running, as an interrupt always leaves it. Only
// those three lanes are held: the person's own words and the manager's next
// hand-off still start a turn, which ends the hold.
//
// A landing was let through at first, as news of work the stop had not been
// about. It was the same fault one lane over: the stopped member's task came
// back, started its turn, and the turn took the stopped work up again on the
// result. The person's word on it was to keep it quiet, so it is kept as the
// receipt a late stop of the task keeps ([Agent.admitBeltLanding]).
//
// A conversation in no team is untouched: there the ending of a command the
// person asked to hear about still starts its turn.
//
// The stop may also find no turn running. A member commonly starts a long
// command in the background, asks to be told when it ends, and ends its turn:
// measured the same day, a crew's Stop passed over such a member and it
// reported the work done when its command ended. So a stop that finds no turn
// but a command or a watch still running begins the hold too, and withdraws
// the wake a failed command's ending had armed ([Agent.deferJobWakeLocked]).
// A command left running for its own sake (a dev server) is held the same.
//
// A manager's team_stop of such a member holds the same way. The member's own
// session used to look for its manager's stop only while a turn ran, so a
// headless member that waited on a command never read the stop and reported
// the stopped work done when the command ended. Its idle watch reads the stop
// now and begins the hold through the manager's door; the manager is told
// nothing of its own stop, and nothing is written.
//
// No handed-out work stalls unsaid. What a manager handed this conversation
// lasts as long as the work does ([Agent.teamHandedCarried]): through a turn
// that finishes while the member waits on a command ([jobRegistry.awaited])
// and through the turn that command's ending starts. A stop through the
// person's door that begins the hold while such work is alive tells that
// team's manager once, in the words a stopped turn uses, whether the stopped
// turn carried the work, was the person's own, or there was no turn at all.

import (
	"sync"
	"sync/atomic"

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
	// held is set by the person's stop or a manager's and cleared when a turn
	// next begins.
	held atomic.Bool

	mu sync.Mutex
	// origin is the turn that last ended with handed work still alive: what a
	// stop that finds no turn running is the stop of.
	origin turnOrigin
}

// teamStopHoldLocked begins the hold when the stop is the person's or a
// manager's and there is something to hold: a turn running, or a command or
// watch whose ending would be news. It reports whether it began with no turn
// running, and whether it withdrew a wake already armed. Through the person's
// door that is the stop a manager may have yet to be told of
// ([Agent.teamStopHoldSaid]); the manager's own door tells nobody
// ([Agent.teamStopWaiting]). The caller holds a.mu.
func (a *Agent) teamStopHoldLocked(door StopDoor) (waiting, withdrew bool) {
	hold := &a.team.stopHold
	if (door != StopByPerson && door != StopByManager) || !hold.seated.Load() {
		return false, false
	}
	if a.running {
		hold.held.Store(true)
		return false, false
	}
	// A command that failed moments ago has its wake on a clock, and that turn
	// would end the hold: the clock is withdrawn and its notes stay queued
	// for the next turn.
	withdrew = a.jobWake != nil
	if !withdrew && (a.jobs == nil || !a.jobs.newsPending()) {
		return false, false
	}
	a.stopJobWakeLocked()
	return !hold.held.Swap(true), withdrew
}

// teamStopWaiting is a manager's stop that found this member with no turn
// running: the hold begins if a command or a watch is still running here, as
// it does for the person's stop of such a member. A turn that began since the
// look is left alone; a stop written before it began is not addressed to it.
//
// IT SAYS NOTHING. The person's door tells the member's manager that work it
// handed out was stopped ([Agent.teamStopHoldSaid]); this stop is the
// manager's own, which it does not need to be woken to hear of, and
// `stopped by the person` is the person's door's line alone.
func (a *Agent) teamStopWaiting() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		a.teamStopHoldLocked(StopByManager)
	}
}

// teamManagerStops reports whether entry is role's manager stopping this
// membership: a stop line from the manager that names the conversation by its
// key or, carrying none, by its handle. Only a membership that takes this
// team's orders is stopped by it: not the manager's own, and not a shared one,
// whose manager here is a link and whose stops do not reach it.
func teamManagerStops(role teamRole, entry teams.Entry) bool {
	if !role.managed || role.manager || role.shared || role.handle == "" {
		return false
	}
	return entry.Kind == teams.KindStop && entry.From == teams.FromManager &&
		(entry.Member == role.key || entry.Member == "" && entry.To == role.handle)
}

// teamStopHoldOver ends the hold: a turn has begun.
func (a *Agent) teamStopHoldOver() { a.team.stopHold.held.Store(false) }

// teamHandedCarried is a turn's ending that nobody stopped. The work a manager
// handed this conversation ends with it unless the work goes on: the member
// still waits on a command, or that command's ending has already started the
// next turn or is about to.
func (a *Agent) teamHandedCarried(origin turnOrigin) {
	a.team.mu.Lock()
	none := len(a.team.handed) == 0
	a.team.mu.Unlock()
	if none {
		return
	}
	if !a.teamWorkGoesOn() {
		a.teamHandedTake()
		return
	}
	hold := &a.team.stopHold
	hold.mu.Lock()
	hold.origin = origin
	hold.mu.Unlock()
}

// teamWorkGoesOn reports whether a turn that just ended left its work waiting.
// A turn the person's own words opened is not the work going on: the turn that
// ended had nothing left to wait for.
func (a *Agent) teamWorkGoesOn() bool {
	if a.jobs != nil && a.jobs.awaited() {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.jobWake != nil || a.running && a.personWords == ""
}

// teamStopHoldSaid tells the managers whose work this conversation is still on
// that it was stopped while it waited on a command or a watch, or on the wake
// the hold withdrew. It ends that work: the stop is reported. A member waiting
// on nothing finished its work, however long ago, and its managers are told
// nothing. It reads the jobs and the teams' logs, so it runs away from the
// stop's own path and outside a.mu.
func (a *Agent) teamStopHoldSaid(withdrew bool) {
	if !withdrew && (a.jobs == nil || !a.jobs.awaited()) {
		return
	}
	handed := a.teamHandedTake()
	if len(handed) == 0 {
		return
	}
	hold := &a.team.stopHold
	hold.mu.Lock()
	origin := hold.origin
	hold.mu.Unlock()
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

// awaited reports whether this conversation is waiting on something still
// running: a command whose ending it is owed or asked to be told of, the lanes
// [jobRegistry.settleExit] wakes on whatever the exit code, or a watch that
// has not fired. A command started to be left running is not waited on.
func (r *jobRegistry) awaited() bool {
	for _, one := range r.all() {
		select {
		case <-one.done:
			continue
		default:
		}
		if one.kind == jobKindWatch {
			return true
		}
		if one.kind != jobKindBash {
			continue
		}
		one.mu.Lock()
		waits := one.owed || one.notify || one.stageOwned
		one.mu.Unlock()
		if waits {
			return true
		}
	}
	return false
}
