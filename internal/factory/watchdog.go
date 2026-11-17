package factory

import (
	"strings"
	"time"
)

// THE STALL WATCHDOG, AS RULES.
//
// A stored wait ([Wait]) names who a job waits on. The watchdog is what looks
// when that owner is late or was never able to act: this file says which waits
// are stalls ([StallOf]), when a recorded stall is over ([StallStands]) and how
// often the job's manager is asked about one ([Stall.Turned]). It reads the
// job and the few facts the service keeps beside it ([WatchFacts]) and writes
// nothing; the service's settle pass (internal/factory/serve's watchdog.go)
// records what it reports and gives the manager its turns.
//
// THE MANAGER HEARS FIRST, AND ONLY THE MANAGER (the owner's decision of
// 2026-11-17). Nothing here notifies the person. A stall the manager's turn
// did not move sets [Stall.GaveUp], and the job then reads "you: decide" with
// the stall's words, the ordinary way ([WaitOf]).

// What tripped, as [Stall.Kind].
const (
	// StallWait is a stored wait that is missing or is not the one the job's
	// fields give.
	StallWait = "wait"
	// StallUnheld is work marked as under way with no live lease: no computer
	// is running it.
	StallUnheld = "unheld"
	// StallOverdue is an act the service itself carries out (an approval or a
	// merge past its undo window) that it never did.
	StallOverdue = "overdue"
	// StallGitHub is a pull request the factory has stopped reading.
	StallGitHub = "github"
	// StallMainAgent is a stop the manager held past its deadline.
	StallMainAgent = "main_agent"
	// StallRunner is a job no computer picked up by its deadline.
	StallRunner = "runner"
	// StallNoOffer is a job that waits on its person and offers them nothing
	// that moves it.
	StallNoOffer = "no_offer"
	// StallAuto is a job that waits on its person for an approval Auto gives
	// by itself.
	StallAuto = "auto"
)

// The watchdog's own clocks, beside the waits' ([WaitMainAgentFor],
// [WaitRunnerFor], [WaitGitHubFor]). They are proposals, as those are.
const (
	// WatchWorkingFor is how long work may be under way with no live lease,
	// and how far past its own deadline an act of the service may run.
	WatchWorkingFor = 2 * time.Minute
	// WatchAutoFor is how long an approval Auto gives by itself may wait.
	WatchAutoFor = 2 * time.Minute
	// WatchConfirmFor is how long a stall is on record before the manager is
	// asked. A LAPTOP THAT SLEPT WAKES WITH EVERY CLOCK LATE: GitHub unread
	// for an hour, a queued job an hour old, every lease expired. The poll and
	// the runner put that right within a minute of waking, and a stall that is
	// gone by then is dropped without a word or a model call.
	WatchConfirmFor = 2 * time.Minute
)

// StallBackoff is the wait before each further turn the manager gets on one
// stall, after its first.
var StallBackoff = []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour}

// The reasons a stall is handed to the person, said after its words.
const (
	// StallUnmoved is a stall still there when the watchdog looked again
	// after the manager's turn.
	StallUnmoved = "The manager could not move it."
	// StallTurnFailed is a manager turn that ended in an error.
	StallTurnFailed = "The manager could not be asked."
	// StallNoManager is a factory with no manager to ask.
	StallNoManager = "This factory has no manager to ask."
	// StallNoBudget is a job whose budget is spent: a turn would spend more.
	StallNoBudget = "The budget is reached, so the manager was not asked."
)

// WatchFacts is what the service knows about a job that the job's own
// document does not say.
type WatchFacts struct {
	// Held says a live lease holds the job: a computer is running it. A
	// service that keeps no leases says true, because it cannot tell.
	Held bool
	// GitHubRead is when the factory last read GitHub well, zero for never.
	GitHubRead time.Time
	// AutoPresses says Auto would give the approval the job waits on, by the
	// rule the service presses by.
	AutoPresses bool
	// Busy says the job is in the queue behind other work: the computers that
	// take it are here, and every slot they have is in use. A queue that is
	// moving is not a stall, however long it is.
	Busy bool
}

// Watched says the watchdog looks at the job at all. A finished job, one
// nobody started, one its person stopped and a wait its person put away are
// never stalls.
func Watched(it Item) bool {
	w := underStall(it)
	return w.Who != WaitDone && w.Who != WaitBacklog && !w.PutAway
}

// StallWaitKey is the key of the wait a stall on the job is recorded against
// ([Stall.Wait]): the wait the job's fields give with the watchdog's record
// set aside.
func StallWaitKey(it Item) string { return underStall(it).key() }

// StallOf says what is wrong with the job's wait at now, or nil. It answers
// the kind, the words and the wait; the service stamps the times. A job with
// a stall already on record is the watchdog's already, and answers nil.
func StallOf(it Item, facts WatchFacts, now time.Time) *Stall {
	if it.Stall != nil || !Watched(it) {
		return nil
	}
	want := WaitOf(it)
	stall := func(kind, words string) *Stall {
		return &Stall{Kind: kind, Words: words, Wait: want.key()}
	}
	late := func(at time.Time) bool { return !at.IsZero() && !now.Before(at) }
	// AN APPROVAL OR A MERGE INSIDE ITS UNDO WINDOW IS THE SERVICE'S ACT IN
	// FLIGHT, whatever the job reads meanwhile: the settle pass carries it out
	// when the window closes. It is a stall only when that never happened.
	if due, what := actInFlight(it); !due.IsZero() {
		if late(due.Add(WatchWorkingFor)) {
			return stall(StallOverdue, overdueWords(what))
		}
		return nil
	}
	if WaitFault(it) != nil {
		// THE ONE FAULT OF A WAIT THAT IS STORED RIGHT is a person with nothing
		// to press. Every other fault is the store's record being wrong.
		if w := it.Wait; w != nil && w.same(want) && w.Who == WaitYou && w.Action != "" {
			return stall(StallNoOffer, "This job waits on you to "+firstNonEmpty(actionWords[w.Action], w.Action)+", and nothing it offers does that.")
		}
		return stall(StallWait, "This job lost track of what it waits on.")
	}
	w := *it.Wait
	switch w.Who {
	case WaitWorking:
		if !w.Deadline.IsZero() {
			// Work with a deadline of its own is late by that, lease or none.
			if late(w.Deadline.Add(WatchWorkingFor)) {
				return stall(StallOverdue, overdueWords(w.What))
			}
			return nil
		}
		if !facts.Held && !facts.Busy && late(w.Since.Add(WatchWorkingFor)) {
			return stall(StallUnheld, unheldWords(w.What))
		}
	case WaitGitHub:
		// THE DEADLINE ALONE IS NOT ENOUGH. A read that finds the pull request
		// as it was writes nothing, so a quiet pull request passes its
		// deadline while it is read every minute. It is late only when the
		// factory has also stopped reading GitHub.
		if late(w.Deadline) && late(facts.GitHubRead.Add(WaitGitHubFor)) {
			return stall(StallGitHub, "GitHub has not been read for this pull request in over "+plainMinutes(WaitGitHubFor)+".")
		}
	case WaitMainAgent:
		if late(w.Deadline) {
			return stall(StallMainAgent, "The manager did not answer "+firstNonEmpty(w.What, "the step")+"'s question in time.")
		}
	case WaitRunner:
		if late(w.Deadline) && !facts.Busy {
			return stall(StallRunner, "No computer picked this job up in time.")
		}
	case WaitYou:
		if facts.AutoPresses && late(w.Since.Add(WatchAutoFor)) {
			return stall(StallAuto, "Auto approves this by itself, and has not.")
		}
	}
	return nil
}

// StallStands says the stall on record is still true of the job. The store
// drops a stall when the job reads another wait; this is for a stall whose
// cause went away while the wait stayed: a runner that came back to its step,
// a computer that came back and is busy with other work, GitHub read again,
// an offer that appeared.
func StallStands(it Item, facts WatchFacts) bool {
	st := it.Stall
	if st == nil {
		return false
	}
	switch st.Kind {
	case StallUnheld:
		return !facts.Held && !facts.Busy
	case StallRunner:
		return !facts.Busy
	case StallGitHub:
		return !facts.GitHubRead.After(st.At)
	case StallAuto:
		return facts.AutoPresses
	case StallNoOffer, StallWait:
		// The job as it would be stored with the stall set aside.
		it.Stall = nil
		w := WaitOf(it)
		it.Wait, it.Offers = &w, OffersOf(it)
		return WaitFault(it) != nil
	}
	return true
}

// actInFlight is when an act the service itself carries out on the job falls
// due, and which act: an approval or a merge the person may still undo. It is
// zero for a job with none.
func actInFlight(it Item) (due time.Time, what string) {
	if a := it.Approval; a != nil && a.State == "approving" {
		return a.UndoUntil, "approving"
	}
	if m := it.Merging; m != nil && m.State == MergeAsked {
		return m.UndoUntil, "merging"
	}
	return time.Time{}, ""
}

// WatchAt is when the watchdog next has the job to look at by the clock
// alone: its stall's next look, else the moment [StallOf] could first report
// it on these facts. It is zero for a job no clock makes a stall, and now for
// one that reads as a stall already.
//
// IT IS [StallOf] READ AS A CLOCK, and the two must agree: a job StallOf
// reports is due by now, and a job it does not report is never due by now,
// or the timer that reads this would come round every two seconds for a job
// the pass can do nothing about. A wait that is past its deadline and
// excused by a fact (a queue that is moving, a step whose lease only the
// service can read) is looked at again every [WatchWorkingFor].
func WatchAt(it Item, facts WatchFacts, now time.Time) time.Time {
	if !Watched(it) {
		return time.Time{}
	}
	if st := it.Stall; st != nil {
		return st.NextAt
	}
	if due, _ := actInFlight(it); !due.IsZero() {
		return due.Add(WatchWorkingFor)
	}
	if WaitFault(it) != nil {
		return now
	}
	// later is at, or the watchdog's period on from now when at has passed.
	later := func(at time.Time) time.Time {
		if !at.After(now) {
			return now.Add(WatchWorkingFor)
		}
		return at
	}
	w := it.Wait
	switch w.Who {
	case WaitWorking:
		if !w.Deadline.IsZero() {
			return w.Deadline.Add(WatchWorkingFor)
		}
		if at := w.Since.Add(WatchWorkingFor); facts.Held || facts.Busy || at.After(now) {
			return later(at)
		}
		return now
	case WaitGitHub:
		// Late by its deadline and by the factory's last read, whichever is
		// later.
		if at := facts.GitHubRead.Add(WaitGitHubFor); !facts.GitHubRead.IsZero() && at.After(w.Deadline) {
			return at
		}
		return w.Deadline
	case WaitMainAgent:
		return w.Deadline
	case WaitRunner:
		if facts.Busy {
			return later(w.Deadline)
		}
		return w.Deadline
	case WaitYou:
		if facts.AutoPresses {
			return w.Since.Add(WatchAutoFor)
		}
	}
	return time.Time{}
}

// StallBudgetSpent says the job's budget is spent, so the manager gets no
// turn on its stall: a turn is charged to the job.
func StallBudgetSpent(it Item) bool {
	return it.Cap > 0 && JobLifetimeSpent(it) >= it.Cap
}

// TurnsLeft says the manager has a turn on the stall still to come: the
// first, and one after each back-off.
func (st Stall) TurnsLeft() bool { return st.Turns <= len(StallBackoff) }

// Turned is the stall after one manager turn that ended at now: the turn
// counted and the next look set by the back-off, none once the back-off is
// used up. A TURN THAT FAILED HANDS THE STALL TO THE PERSON at once; one that
// ran is given until the next look to have moved the job.
func (st Stall) Turned(now time.Time, failed bool) Stall {
	st.Turns++
	st.NextAt = time.Time{}
	if st.Turns <= len(StallBackoff) {
		st.NextAt = now.Add(StallBackoff[st.Turns-1])
	}
	if failed {
		return st.GivenUp(StallTurnFailed)
	}
	return st
}

// GivenUp is the stall handed to the person, with why said after its words.
// The job then reads "you: decide" ([WaitOf]). A stall already handed over
// keeps its words.
func (st Stall) GivenUp(why string) Stall {
	if st.GaveUp {
		return st
	}
	st.GaveUp = true
	if why = strings.TrimSpace(why); why != "" {
		st.Words = strings.TrimSpace(st.Words + " " + why)
	}
	return st
}

// unheldWords says work under way that nothing is running.
func unheldWords(what string) string {
	switch what {
	case "publishing":
		return "Publishing is marked as under way, but no computer is doing it."
	case "merging":
		return "The merge is marked as under way, but no computer is doing it."
	case "":
		return "This job is marked as running, but no computer is running it."
	}
	return "The " + what + " step is marked as running, but no computer is running it."
}

// overdueWords says an act of the service that is past its time.
func overdueWords(what string) string {
	switch what {
	case "merging":
		return "The merge was due and was never carried out."
	case "approving":
		return "The approval was due and was never carried out."
	}
	return "This job's next step was due and was never carried out."
}

// plainMinutes says a whole number of minutes in words a person reads.
func plainMinutes(d time.Duration) string {
	return itoa(int(d/time.Minute)) + " minutes"
}
