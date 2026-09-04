package session

import "strings"

// ── ONE READING OF WHAT A TASK IS DOING ─────────────────────────────────────
//
// [TaskState] is the scheduler's word for a node: five values chosen so that
// cascading, settling and checkpointing can be decided from one field. It is not
// the answer to "what is this task doing, why, and is anything mine to do about
// it", and every surface that tried to answer that from the state alone grew a
// private table of its own — the roster, the record page, the task place, home's
// rows and the mention menu each read state, ending, the stop flag and the merge
// word in a different order and disagreed about the same node.
//
// This file is that reading, once, as a pure function. It keeps four questions
// apart that the surfaces used to fold into one word:
//
//	presence     where the work is, in the person's terms
//	next         which of the actions the runtime actually offers is the
//	             person's move, if any
//	changes      what happened to the node's edits in source control
//	fault        whether anything went wrong, as opposed to not finishing
//
// It renames nothing on the wire, spells no person-facing word (surfaces own
// their vocabulary), and derives nothing it was not told: an absent fact stays
// absent rather than becoming a claim.

// TaskPresence is where one task is in the person's terms. The zero value is
// "nothing is known", which is what an unrecognised state reads as: a word this
// build does not know is not evidence that anything was admitted or run.
type TaskPresence string

const (
	// TaskPresenceUnknown is the emptiness law: no reading, so no row.
	TaskPresenceUnknown TaskPresence = ""
	// TaskPresenceQueued is admitted and not started. [TaskStatus.Reason] may
	// say what is holding it.
	TaskPresenceQueued TaskPresence = "queued"
	// TaskPresenceWorking is a worker spending its time on the work.
	TaskPresenceWorking TaskPresence = "working"
	// TaskPresenceWaiting is admitted work not being worked on right now because
	// something else has to happen first. [TaskStatus.On] says what, which is the
	// difference between "stuck" and "next in line".
	TaskPresenceWaiting TaskPresence = "waiting"
	// TaskPresenceFinishing is the end of a run: a repair round closing named
	// gaps, or the check reading what the worker left.
	TaskPresenceFinishing TaskPresence = "finishing"
	// TaskPresenceDone is work that ran to the end and was accepted. It says
	// nothing about where the edits went; that is [TaskStatus.Changes].
	TaskPresenceDone TaskPresence = "done"
	// TaskPresenceIncomplete is work that ended without finishing: a check that
	// named gaps, a dropped connection, a threshold, a brief whose world had
	// moved, or a fault. Only [TaskStatus.Fault] says something went wrong.
	TaskPresenceIncomplete TaskPresence = "incomplete"
	// TaskPresenceNeedsLook is work the machine has taken as far as it can — a
	// claim nobody could check, a design waiting to be approved. Nothing moves
	// until a person looks.
	TaskPresenceNeedsLook TaskPresence = "needs-look"
	// TaskPresenceStopped is a person ending the work, and nothing else. A
	// threshold, a loop guard and a rule the worker would not follow also end
	// runs, and none of them is this.
	TaskPresenceStopped TaskPresence = "stopped"
)

// TaskWaitOn is what a waiting task is waiting on, because "waiting" alone
// leaves the person's next move undecidable.
type TaskWaitOn string

const (
	TaskWaitNobody TaskWaitOn = ""
	// TaskWaitPerson is the person's attention.
	TaskWaitPerson TaskWaitOn = "you"
	// TaskWaitWork is unmet prerequisites, named in [TaskStatus.Reason] when the
	// caller could resolve them.
	TaskWaitWork TaskWaitOn = "work"
	// TaskWaitMachine is capacity or a provider: a slot, a busy machine, paced
	// calls. It clears without anybody doing anything.
	TaskWaitMachine TaskWaitOn = "machine"
)

// TaskNextAction is the person's move. Every value names an action the runtime
// ACTUALLY OFFERS for a node in that reading — offering one it would refuse is
// worse than offering none, so there is deliberately no "resume" or "steer
// this": [Agent.SteerTask] refuses anything that is not running, and no door
// restarts a settled node.
type TaskNextAction string

const (
	// TaskNextNone is work in flight, or a settled node with nothing left.
	TaskNextNone TaskNextAction = ""
	// TaskNextWait is a hold that clears itself — the answer that says a person
	// should NOT act on a row that looks stuck.
	TaskNextWait TaskNextAction = "wait"
	// TaskNextLook is settling work nobody could check ([Agent.ResolveUnverified],
	// reachable from the `tasks` tool and the card) or answering a card that is
	// waiting to be approved.
	TaskNextLook TaskNextAction = "look"
	// TaskNextInspect is reading what a settled run left — its report, its
	// journal, its files. It is what an incomplete node offers, because there is
	// nothing to resume: whether the work continues is a new decision.
	TaskNextInspect TaskNextAction = "inspect"
	// TaskNextCollect is edits sitting on a branch nobody brought home. It is a
	// source-control move and says nothing about whether the person got an
	// answer.
	TaskNextCollect TaskNextAction = "collect"
)

// TaskChangeDisposition is what happened to the node's EDITS in source control,
// and nothing else. It is not a claim about whether the person received a
// result: a research or writing task can be fully answered with no branch at
// all, and a kept branch is not evidence that an answer did or did not arrive.
type TaskChangeDisposition string

const (
	// TaskChangesNone is no edits to place: no branch was made, or the engine
	// said nothing about one.
	TaskChangesNone TaskChangeDisposition = ""
	// TaskChangesMerged is the branch home on the ground's own branch.
	TaskChangesMerged TaskChangeDisposition = "merged"
	// TaskChangesInPlace is work done in the ground itself, so there was nowhere
	// to land it.
	TaskChangesInPlace TaskChangeDisposition = "in-place"
	// TaskChangesKept is a branch left standing. The engine also writes
	// "aborted" for this, which is the same fact in a scarier word.
	TaskChangesKept TaskChangeDisposition = "kept"
	// TaskChangesConflicted is a branch that would not fasten.
	TaskChangesConflicted TaskChangeDisposition = "conflicted"
)

// TaskLiveness is what the caller KNOWS about whether anything is behind a row
// that claims to be running or queued. Unknown is the default and is preserved:
// a node absent from one process is not a dead node, and work outlives the
// window that started it.
type TaskLiveness string

const (
	// TaskLivenessUnknown is no authoritative answer. The reading follows the
	// state as claimed.
	TaskLivenessUnknown TaskLiveness = ""
	// TaskLivenessHeld is a positive claim: something holds this node.
	TaskLivenessHeld TaskLiveness = "held"
	// TaskLivenessUnclaimed is an authoritative NEGATIVE — no live claim exists
	// for this node anywhere the caller can see (internal/session's
	// [SessionRow.Runs] makes exactly that judgement from the presence file and
	// the lock). It is not "no terminal is attached".
	TaskLivenessUnclaimed TaskLiveness = "unclaimed"
)

// TaskFacts is everything the reading uses. A caller fills in what it has; every
// absent field is an absence rather than a claim.
type TaskFacts struct {
	// State and Ending are the engine's own words, unchanged.
	State  TaskState
	Ending TaskEnding
	// Life is which of a running node's lives it is in ([TaskPhaseWorking],
	// [TaskPhaseChecking], [TaskPhaseRepairing], [TaskPhaseSizing]) — a typed
	// runtime fact, and the one the finishing reading is taken from.
	Life string
	// Kind and Phase are what sort of node this is and, for kinds that name their
	// own moments, which moment (TaskNotice.Doing). Phases are a kind's private
	// vocabulary: exactly one is interpreted here ([HarnessPhaseAsking], the
	// design waiting to be approved, which has no typed equivalent), and every
	// other phase is carried as prose. Closing that gap means giving harness
	// phases a typed lifecycle, which belongs to the kind that owns them.
	Kind  TaskKind
	Phase string
	// Gap is what a nearly-finished worker is still closing (TaskNotice.Mending);
	// Hold is why a node is not spending its time on the work (TaskNotice.Waiting).
	// Both are the engine's own sentences and both describe RIGHT NOW.
	Gap  string
	Hold string
	// Waits names unmet prerequisites, resolved to titles by the caller. Ids are
	// not names.
	Waits []string
	// Stopped says a person ended this node (TaskNotice.Stopped).
	Stopped bool
	// Liveness is what the caller knows about a live-looking row.
	Liveness TaskLiveness
	// Merge and Branch are the source-control facts (TaskNotice.Merge, .Branch).
	Merge  string
	Branch string
}

// TaskStatus is the reading. Fields are separate answers and none is derivable
// from another.
type TaskStatus struct {
	Presence TaskPresence
	On       TaskWaitOn
	Next     TaskNextAction
	// Reason is the ENGINE'S prose about why — the hold, the gap, prerequisites
	// by name, a kind's phase word. This file never writes a sentence of its own
	// into it.
	Reason string
	// Fault says something went wrong, as opposed to the work merely not
	// finishing: a dropped connection and an exhausted threshold are not faults.
	Fault bool
	// Attention says the node will not move without a person.
	Attention bool
	// Changes and Branch are the source-control axis.
	Changes TaskChangeDisposition
	Branch  string
	// State, Ending and Liveness are carried through unchanged so that callers
	// needing the runtime's own facts read them instead of inferring them back
	// out of the presence.
	State    TaskState
	Ending   TaskEnding
	Liveness TaskLiveness
}

// Settled reports whether the RUN is over, from the lifecycle state and nothing
// else. A design waiting to be approved is running work whose next step is a
// person's ([TaskPresenceNeedsLook]) — its room stays open, its stop still
// works — and reading terminality off the presence would close it early.
func (s TaskStatus) Settled() bool { return s.State.settled() }

// ChangesUnlanded reports that the node's edits are on a branch that never came
// home. It is a claim about source control only: a task with no branch may be
// completely delivered, and one with a kept branch may have answered the person
// in full.
func (s TaskStatus) ChangesUnlanded() bool {
	switch s.Changes {
	case TaskChangesKept, TaskChangesConflicted:
		return strings.TrimSpace(s.Branch) != ""
	}
	return false
}

// ProjectTask reads one node's facts.
//
// The order of the tests is the design: a person's stop outranks the state,
// because a node they ended settles `failed` and nothing went wrong with it; an
// authoritative "nothing holds this" outranks a claim of running. After those
// the lifecycle leads, and within a state the most specific true fact wins.
func ProjectTask(facts TaskFacts) TaskStatus {
	status := TaskStatus{
		State:    facts.State,
		Ending:   facts.Ending,
		Liveness: facts.Liveness,
		Branch:   strings.TrimSpace(facts.Branch),
		Changes:  taskChangesOf(facts.Merge),
	}
	switch {
	case taskStoppedByPerson(facts) && facts.State != TaskRunning:
		// A stop still going through is not a stop yet: the context is cut and
		// the child is winding up, and the node is genuinely running until it
		// settles.
		status.Presence = TaskPresenceStopped
	case taskClaimsToBeLive(facts.State) && facts.Liveness == TaskLivenessUnclaimed:
		// The row claims to be running and the caller has an authoritative
		// negative. Nothing will move it, and nothing can be resumed.
		status.Presence, status.On, status.Next = TaskPresenceIncomplete, TaskWaitPerson, TaskNextInspect
	default:
		status = taskLifecycleStatus(status, facts)
	}
	return taskStatusDemand(status)
}

// taskClaimsToBeLive reports whether a state is one a row can claim while
// nothing is behind it.
func taskClaimsToBeLive(state TaskState) bool {
	return state == TaskRunning || state == TaskQueued
}

// taskLifecycleStatus is the reading of a node nobody stopped and nothing
// contradicts: the state leads.
func taskLifecycleStatus(status TaskStatus, facts TaskFacts) TaskStatus {
	switch facts.State {
	case TaskQueued:
		return taskQueuedStatus(status, facts)
	case TaskRunning:
		return taskRunningStatus(status, facts)
	case TaskUnverified:
		// Nobody could say whether the work holds, which is not a finding that it
		// does not. Only a person moves it ([Agent.ResolveUnverified]).
		status.Presence, status.On, status.Next = TaskPresenceNeedsLook, TaskWaitPerson, TaskNextLook
	case TaskFailed:
		status.Presence, status.On, status.Next = TaskPresenceIncomplete, TaskWaitPerson, TaskNextInspect
		status.Fault = taskEndingIsFault(facts.Ending)
	case TaskDone:
		status.Presence = TaskPresenceDone
	}
	return status
}

// taskQueuedStatus reads a node that has been admitted and has not started.
func taskQueuedStatus(status TaskStatus, facts TaskFacts) TaskStatus {
	status.Presence = TaskPresenceQueued
	if waits := taskWaitsWord(facts.Waits); waits != "" {
		// A dependency outranks a hold: it names work a person can look at,
		// reorder or stop, where a hold names a queue that clears itself.
		status.Presence, status.On, status.Reason = TaskPresenceWaiting, TaskWaitWork, waits
		return status
	}
	if hold := strings.TrimSpace(facts.Hold); hold != "" {
		// A hold does not move a queued node off `queued` — it has not started
		// either way. What it adds is why, and that nobody need act.
		status.On, status.Next, status.Reason = TaskWaitMachine, TaskNextWait, hold
	}
	return status
}

// taskEndingIsFault says whether something went WRONG, which is what is left
// once every ending that is nobody's finding is taken out: the wire, a
// threshold, a loop, another task's copy and a rule the worker would not follow
// (haltedVerb holds that list, so this file keeps no second copy); a brief whose
// world had moved never started; a check that named gaps found work still TO DO.
// What remains — a working copy that could not be made, an error nobody
// classified, and a row from an engine that named no ending — is a fault.
func taskEndingIsFault(ending TaskEnding) bool {
	switch ending {
	case TaskEndingRefused, TaskEndingStale:
		return false
	}
	return haltedVerb(ending) == ""
}

// taskStatusDemand adds what the reading asks of a PERSON, which is the one
// question both axes can answer.
func taskStatusDemand(status TaskStatus) TaskStatus {
	if status.ChangesUnlanded() {
		// Edits nobody brought home need a person whatever the state was, and the
		// demand does not overwrite the presence: where the work is and where its
		// edits went are the two axes this file keeps apart.
		status.Attention = true
		if status.Next == TaskNextNone {
			status.Next = TaskNextCollect
		}
	}
	if status.Presence == TaskPresenceNeedsLook {
		status.Attention = true
	}
	return status
}

// taskRunningStatus reads a node the graph is holding open — the one state with
// more than one honest answer in it.
func taskRunningStatus(status TaskStatus, facts TaskFacts) TaskStatus {
	switch {
	case facts.Kind == TaskKindHarness && facts.Phase == HarnessPhaseAsking:
		// The one piece of running work that is not running: the page is written
		// and the only remaining step is somebody approving it. Counted as working
		// it made surfaces say "1 running" about a card that had been waiting on
		// the person reading that line.
		status.Presence, status.On, status.Next = TaskPresenceNeedsLook, TaskWaitPerson, TaskNextLook
	case facts.Life == TaskPhaseChecking || facts.Life == TaskPhaseRepairing:
		// The typed end of a run: the gate reading what the worker left, or a
		// round closing named gaps. Taken from the lifecycle fact rather than from
		// the presence of a sentence, so a round with nothing to say still reads
		// as finishing.
		status.Presence, status.Reason = TaskPresenceFinishing, strings.TrimSpace(facts.Gap)
	case strings.TrimSpace(facts.Gap) != "":
		status.Presence, status.Reason = TaskPresenceFinishing, strings.TrimSpace(facts.Gap)
	case strings.TrimSpace(facts.Hold) != "":
		// A paced node is not spending its time on the work, and saying it is
		// working would assert a present that is not happening.
		status.Presence, status.On, status.Next = TaskPresenceWaiting, TaskWaitMachine, TaskNextWait
		status.Reason = strings.TrimSpace(facts.Hold)
	default:
		status.Presence, status.Reason = TaskPresenceWorking, strings.TrimSpace(facts.Phase)
	}
	return status
}

// taskStoppedByPerson answers the stopped reading from either record of one act:
// the flag a live notice carries (TaskNotice.Stopped) and the ending the engine
// writes when a person cancels. A reader with only the flag called every stopped
// row in the project's history a failure — the index file carries the ending and
// has never carried the flag.
func taskStoppedByPerson(facts TaskFacts) bool {
	return facts.Stopped || facts.Ending == TaskEndingStopped
}

// taskWaitsWord joins prerequisite names, dropping the ones the caller could not
// resolve rather than drawing gaps.
func taskWaitsWord(waits []string) string {
	named := make([]string, 0, len(waits))
	for _, one := range waits {
		if one = strings.TrimSpace(one); one != "" {
			named = append(named, one)
		}
	}
	return strings.Join(named, " · ")
}

// taskChangesOf reads the engine's merge word, using task_run.go's own constants
// rather than copies. A word this build does not know reads as nothing to place,
// which is the claim that says least.
func taskChangesOf(merge string) TaskChangeDisposition {
	switch strings.TrimSpace(merge) {
	case mergeMerged:
		return TaskChangesMerged
	case mergeInPlace:
		return TaskChangesInPlace
	case mergeKept, mergeAborted:
		return TaskChangesKept
	case mergeConflicted:
		return TaskChangesConflicted
	}
	return TaskChangesNone
}

// StatusFacts is one record row as the reading takes it. `held` is the caller's
// authoritative liveness ([SessionRow.Runs]); false is passed as UNCLAIMED only
// because that method's own ladder — a live conversation's claim list, then the
// lock — is a negative answer and not an absence of one.
//
// A record row knows less than a live one and the gaps are real: the index
// carries no merge word, branch name, hold, gap or prerequisite, so a row read
// from it can say what state it is in and why it ended, and never where its
// edits are or what is holding it. Those are absences, not claims; closing them
// means writing more into a persisted file.
func (e TaskIndexEntry) StatusFacts(held bool) TaskFacts {
	facts := TaskFacts{
		State:  TaskState(e.Status),
		Ending: e.Ending,
		Kind:   e.Kind,
		Life:   strings.TrimSpace(e.Phase),
	}
	if e.Live() {
		facts.Liveness = TaskLivenessUnclaimed
		if held {
			facts.Liveness = TaskLivenessHeld
		}
	}
	return facts
}
