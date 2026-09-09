package session

import "strings"

// One reading of what a task is doing, for every surface that draws one.
//
// [TaskState] is the scheduler's word for a node — five values chosen so that
// cascading, settling and checkpointing can be decided from one field. It is not
// the answer to "what is this task doing, why, and does it need me", and each
// surface that tried to answer that from the state alone grew a table of its own
// over state, ending, the stop flag and the merge word. They disagreed: a task a
// person stopped drew ⊘ on the roster and ✗ on the record page.
//
// This file is that reading, once, as a pure function over facts the caller
// already holds. It keeps apart what those tables folded together: where the
// work is, what it is waiting on, what happened to its edits in source control,
// and whether anything actually broke. It renames nothing on the wire and turns
// no absent fact into a claim.
//
// AND IT IS WHERE THE PERSON'S WORDS ARE SPELLED. Surfaces used to own their own
// vocabulary, which is how one program came to have four names for one state and
// a card that disagreed with the rail beside it. [TaskTier] is the question every
// row answers, [TaskStatus.Word] is the word it answers with, [TaskAsk] is the
// question and its two answers, and every one of them is written down exactly
// once, here (docs/design/task-states/DESIGN.md).

// TaskPresence is where one task is in the person's terms. The zero value means
// nothing is known — an unrecognised state is not evidence that anything was
// admitted or run.
type TaskPresence string

const (
	// TaskPresenceUnknown draws no row: there is no reading.
	TaskPresenceUnknown TaskPresence = ""
	// TaskPresenceQueued is admitted and not started. [TaskStatus.Reason] may say
	// what is holding it.
	TaskPresenceQueued TaskPresence = "queued"
	// TaskPresenceWorking is a worker spending its time on the work.
	TaskPresenceWorking TaskPresence = "working"
	// TaskPresenceWaiting is admitted work that is not being worked on right now
	// because something else has to happen first; [TaskStatus.On] says what. It is
	// the difference between "stuck" and "next in line", which a still row cannot
	// otherwise show.
	TaskPresenceWaiting TaskPresence = "waiting"
	// TaskPresenceFinishing is the end of a run: the check reading what the worker
	// left, or a round closing named gaps.
	TaskPresenceFinishing TaskPresence = "finishing"
	// TaskPresenceDone is work that ran to the end and was accepted. It says
	// nothing about where the edits went; that is [TaskStatus.Changes].
	TaskPresenceDone TaskPresence = "done"
	// TaskPresenceIncomplete is work that ended without finishing: a check that
	// named gaps, a dropped connection, a threshold, a brief whose world had
	// moved, or a fault. Only [TaskStatus.Fault] says something went wrong.
	TaskPresenceIncomplete TaskPresence = "incomplete"
	// TaskPresenceNeedsLook is work the machine has taken as far as it can — a
	// claim nobody could check, a design waiting to be approved.
	TaskPresenceNeedsLook TaskPresence = "needs-look"
	// TaskPresenceStopped is a person ending the work, and nothing else. A
	// threshold, a loop guard and a rule the worker would not follow also end
	// runs, and none of them is this.
	TaskPresenceStopped TaskPresence = "stopped"
)

// TaskWaitOn is what a waiting task is waiting on. "Waiting" alone leaves the
// person's next move undecidable.
type TaskWaitOn string

const (
	TaskWaitNobody TaskWaitOn = ""
	// TaskWaitPerson is the person's attention.
	TaskWaitPerson TaskWaitOn = "you"
	// TaskWaitWork is unmet prerequisites, named in [TaskStatus.Reason] when the
	// caller could resolve them.
	TaskWaitWork TaskWaitOn = "work"
	// TaskWaitMachine is capacity or a provider — a slot, a busy machine, paced
	// calls — and it clears without anybody doing anything.
	TaskWaitMachine TaskWaitOn = "machine"
)

// TaskChangeDisposition is what happened to the node's edits in source control,
// and nothing else. It is not a claim about whether the person received a
// result: a research or writing task can be answered in full with no branch at
// all, and a kept branch is not evidence either way.
type TaskChangeDisposition string

const (
	// TaskChangesNone is no edits to place: no branch was made, or the engine said
	// nothing about one.
	TaskChangesNone TaskChangeDisposition = ""
	// TaskChangesMerged is the branch home on the ground's own branch.
	TaskChangesMerged TaskChangeDisposition = "merged"
	// TaskChangesInPlace is work done in the ground itself, with nowhere to land.
	TaskChangesInPlace TaskChangeDisposition = "in-place"
	// TaskChangesKept is a branch left standing. The engine also writes "aborted"
	// for this, which is the same fact in a scarier word.
	TaskChangesKept TaskChangeDisposition = "kept"
	// TaskChangesConflicted is a branch that would not fasten.
	TaskChangesConflicted TaskChangeDisposition = "conflicted"
)

// TaskLiveness is what the caller knows about whether anything is behind a row
// that claims to be running or queued. Unknown is the default and is preserved:
// a node absent from one process is not a dead node, and work outlives the
// window that started it.
type TaskLiveness string

const (
	// TaskLivenessUnknown is no authoritative answer; the reading follows the
	// state as claimed.
	TaskLivenessUnknown TaskLiveness = ""
	// TaskLivenessHeld is a positive claim: something holds this node.
	TaskLivenessHeld TaskLiveness = "held"
	// TaskLivenessUnclaimed is an authoritative negative — no live claim exists
	// anywhere the caller can see, which is the judgement [SessionRow.Runs] makes
	// from the presence file and the lock. It is not "no terminal is attached".
	TaskLivenessUnclaimed TaskLiveness = "unclaimed"
)

// TaskFacts is everything the reading uses. A caller fills in what it has, and
// every absent field stays an absence.
type TaskFacts struct {
	// State and Ending are the engine's own words, unchanged.
	State  TaskState
	Ending TaskEnding
	// Life is which of a running node's lives it is in ([TaskPhaseWorking],
	// [TaskPhaseChecking], [TaskPhaseRepairing], [TaskPhaseSizing]). It is the
	// typed fact the finishing reading is taken from.
	Life string
	// Kind and Phase are what sort of node this is and, for kinds that name their
	// own moments, which moment (TaskNotice.Doing). Phases are a kind's private
	// vocabulary: exactly one is interpreted here ([HarnessPhaseAsking], which has
	// no typed equivalent) and the rest are carried as prose.
	Kind  TaskKind
	Phase string
	// Gap is what a nearly-finished worker is still closing (TaskNotice.Mending)
	// and Hold is why a node is not spending its time on the work
	// (TaskNotice.Waiting). Both are the engine's sentences about right now.
	Gap  string
	Hold string
	// Waits names unmet prerequisites, resolved to titles by the caller: ids are
	// not names.
	Waits []string
	// Paused says the work is held at a gate only a person can open
	// (TaskNotice.Paused): an adaptive run that has spent its tank. It is not a
	// [TaskFacts.Hold] — a hold clears itself and nobody need act, and this one
	// clears when somebody decides — which is why it is a fact of its own.
	//
	// It is read of a node the graph is still holding open and of no other, so a
	// flag left on a row that has since landed can never contradict its ending.
	Paused bool
	// Stopped says a person ended this node (TaskNotice.Stopped).
	Stopped bool
	// Liveness is what the caller knows about a live-looking row.
	Liveness TaskLiveness
	// Merge and Branch are the source-control facts (TaskNotice.Merge, .Branch).
	Merge  string
	Branch string
	// Report is the landing's own account of itself (TaskNotice.Report), and it is
	// read for exactly two things: which incomplete reason a fault or a check's
	// finding gets ([TaskReasonOf]), and the gaps a held landing names. Nothing
	// else here reads prose, and a node that has not landed has none.
	Report string
	// Consent says the work HAS NOT BEEN AGREED TO YET: a proposal in front of
	// somebody, before any of it runs. Countdown is how long the consent clock has
	// left, already spelled by whoever is ticking it ("9s"), and empty means there
	// is no clock — either it was never set or typing held it.
	//
	// THE CLOCK IS THE WHOLE DIFFERENCE BETWEEN THE TWO TIERS. A proposal that
	// will start by itself needs nothing from anybody and is MOVING; one that will
	// sit there until somebody answers is the person's call and says so. The
	// spelling of the remaining time stays with the surface that is redrawing it
	// every second, and the sentence it goes into stays here.
	Consent   bool
	Countdown string
	// Cap is what a run's fuel gate is holding at, in the person's own money
	// ("$5.00"), and "" when the amount is not known. It is read only beside
	// [TaskFacts.Paused]: the gate is the fact, and this is the figure the question
	// is about.
	Cap string
	// Held says the landing TURNED THE WORK BACK (TaskNotice.ResultHeld): the
	// check did not pass it, and what it produced is named rather than handed on
	// (task_result.go). It is not the same news as an ordinary incomplete — there
	// is an answer sitting there, and taking it anyway is one of the two things a
	// person may say about it.
	Held bool
	// Conflicts names the files that clash, for a landing whose branch would not
	// fasten (TaskNotice.Conflicts). An empty list under a conflicted merge is the
	// emptiness law and not a claim that nothing clashed: git does not always say
	// which files it was about, and the sentence simply stops after "conflicts
	// with your branch".
	Conflicts []string
	// Shifted says the names in [TaskFacts.Conflicts] are there because THE GROUND
	// MOVED and not because the merge was refused (TaskNotice.Shifted): the branch
	// would have fastened, and the person's own changed the same files while the
	// work ran. It asks the conflict's question — two versions of these files,
	// which survives — with the conflict's two answers, and only the reason
	// sentence differs ([taskShiftReason]).
	Shifted bool
	// Decider is WHO HOLDS THE DECISION right now (TaskNotice.Decider). The zero
	// value reads as the person, which is the only safe reading of a caller that
	// said nothing: work whose owner nobody recorded is work waiting on whoever is
	// looking at it.
	Decider TaskAskOwner
}

// ── the three tiers ─────────────────────────────────────────────────────────

// TaskTier is the ONE QUESTION every row answers before it says anything else:
// do I need to do anything? There are three answers, each with one glyph and one
// word, and a person who has learned three glyphs has learned the whole system
// (docs/design/task-states/DESIGN.md).
//
// IT IS COMPUTED HERE AND NOWHERE ELSE. Every surface that worked a tier out of
// the state, the ending, the stop flag and the merge word grew a table of its
// own, and the tables disagreed — which is the defect this type exists to end.
type TaskTier string

const (
	// TaskTierMoving is work in flight: nothing for you.
	TaskTierMoving TaskTier = "moving"
	// TaskTierOver is work that has ended, however it ended: nothing for you, and
	// a rerun may be offered.
	TaskTierOver TaskTier = "over"
	// TaskTierYourCall is the machine having done what it can. The card carries
	// the reason and the two answers.
	TaskTierYourCall TaskTier = "your-call"
)

// TaskAskKind is which of the closed set of questions a your-call row is asking.
// The set is closed on purpose: a card whose reason is not one of these six is a
// card nobody wrote the answers for.
type TaskAskKind string

const (
	// TaskAskStart is a proposal waiting on the person's word, with no clock to
	// start it.
	TaskAskStart TaskAskKind = "start"
	// TaskAskApprove is a design written and waiting to be approved.
	TaskAskApprove TaskAskKind = "approve"
	// TaskAskConflict is a branch that would not fasten onto the person's.
	TaskAskConflict TaskAskKind = "conflict"
	// TaskAskCheck is work nobody could check.
	TaskAskCheck TaskAskKind = "check"
	// TaskAskHeld is work the check did not pass, whose answer is being held.
	TaskAskHeld TaskAskKind = "held"
	// TaskAskCap is a run standing at its fuel gate.
	TaskAskCap TaskAskKind = "cap"
)

// TaskAskOwner is who holds a decision right now.
type TaskAskOwner string

const (
	// TaskAskOwnerPerson is the ordinary answer and the floor every other answer
	// falls back to.
	TaskAskOwnerPerson TaskAskOwner = "person"
	// TaskAskOwnerModel is `task.settle = auto`, or the person handing this one
	// card over ([Agent.HandUnverifiedToModel]). IT IS NEVER PERMANENT: the floor
	// hands it back at the end of the model's turn (agent.go).
	TaskAskOwnerModel TaskAskOwner = "model"
)

// TaskAsk is the question on a your-call row: the reason sentence and the two
// closed answers, spelled once here and read by every surface.
//
// YES AND NO ARE THE PERSON'S OWN VERBS and not the engine's three resolutions.
// A surface draws them as chips and maps them back — `a` to accept or to the
// conflict's merge round, `n` to refute — so that "drop it" and "not right" can
// be the right words on their own cards without either of them becoming a fourth
// thing the engine has to know about.
type TaskAsk struct {
	Kind TaskAskKind
	// Reason is the whole row sentence, complete: "conflicts with your branch:
	// a.go, b.go".
	Reason string
	Yes    string
	No     string
	Owner  TaskAskOwner
}

// TaskStatus is the reading. Each field answers a different question, and none
// is derivable from another.
type TaskStatus struct {
	Presence TaskPresence
	On       TaskWaitOn
	// Reason is the engine's own prose about why — the hold, the gap, the
	// prerequisites by name, a kind's phase word. This file never writes a
	// sentence of its own into it.
	Reason string
	// Fault says something went wrong, as distinct from work that did not finish:
	// a dropped connection and an exhausted threshold are not faults.
	Fault bool
	// Attention says the node will not move without a person.
	Attention bool
	// Changes and Branch are the source-control axis.
	Changes TaskChangeDisposition
	Branch  string
	// State, Ending and Liveness are carried through unchanged, so a caller that
	// needs the runtime's own facts reads them instead of inferring them back out
	// of the presence.
	State    TaskState
	Ending   TaskEnding
	Liveness TaskLiveness
	// Tier is the one question a person asks of a row before anything else, and
	// Word is the word the row wears for it. Ask is the question and its two
	// answers, and it is the zero value unless Tier is [TaskTierYourCall].
	//
	// A SURFACE READS THESE AND SPELLS NOTHING OF ITS OWN. The glyph comes from
	// the tier, the row from Word and Reason ([TaskStatus.RowWord]), the card from
	// Ask. Presence and On are still here and still say where the work is; what
	// they no longer decide is what a person is told.
	Tier TaskTier
	Word string
	Ask  TaskAsk
}

// RowWord is the whole of what one row says: the word, and the reason under it
// where there is one.
//
// A ROW NEVER READS A BARE `waiting` OR A BARE `your call`. The reason is the
// half a person can act on, so it travels with the word everywhere the word is
// drawn. THE REASON IS NEVER SAID TWICE: a word that already carries its own
// object ("waiting on Collect sources") is the whole sentence on its own.
func (s TaskStatus) RowWord() string {
	word, reason := strings.TrimSpace(s.Word), strings.TrimSpace(s.Reason)
	switch {
	case word == "":
		return reason
	case reason == "" || strings.HasSuffix(word, reason):
		return word
	}
	return word + " · " + reason
}

// Settled reports whether the run is over, from the lifecycle state alone. A
// design waiting to be approved needs a person and is still running work: its
// room stays open and its stop still works, so terminality may not be read off
// the presence.
func (s TaskStatus) Settled() bool { return s.State.settled() }

// ChangesUnlanded reports that the node's edits sit on a branch that never came
// home. It is a source-control claim only.
func (s TaskStatus) ChangesUnlanded() bool {
	switch s.Changes {
	case TaskChangesKept, TaskChangesConflicted:
		return strings.TrimSpace(s.Branch) != ""
	}
	return false
}

// ProjectTask reads one node's facts, and answers the question every surface asks
// first: which tier, which word, and — when it is the person's call — which
// question and its two answers.
//
// The order of the tests is the design: work nobody has agreed to yet outranks
// everything, because there is no lifecycle to read on a proposal; a person's
// stop outranks the state, because a node they ended settles `failed` and nothing
// went wrong with it; and an authoritative "nothing holds this" outranks a claim
// of running. After those the lifecycle leads, and within a state the most
// specific true fact wins. The words go on last, over the reading rather than
// inside it ([taskStatusWords]).
func ProjectTask(facts TaskFacts) TaskStatus {
	status := TaskStatus{
		State:    facts.State,
		Ending:   facts.Ending,
		Liveness: facts.Liveness,
		Branch:   strings.TrimSpace(facts.Branch),
		Changes:  taskChangesOf(facts.Merge),
	}
	switch {
	case facts.Consent && !facts.State.settled():
		// NOTHING HAS RUN YET, which outranks every state below: a proposal in
		// front of somebody has no lifecycle to read, and whichever state a caller
		// happens to be carrying for it describes work that has not started.
		status = taskConsentStatus(status, facts)
	case taskStoppedByPerson(facts) && facts.State != TaskRunning:
		// A stop still going through is not a stop yet: the context is cut, the
		// child is winding up, and the node is running until it settles.
		status.Presence = TaskPresenceStopped
	case taskClaimsToBeLive(facts.State) && facts.Liveness == TaskLivenessUnclaimed:
		// The row claims to be live and the caller has an authoritative negative.
		status.Presence, status.On = TaskPresenceIncomplete, TaskWaitPerson
	default:
		status = taskLifecycleStatus(status, facts)
	}
	return taskStatusWords(taskStatusDemand(status), facts)
}

// taskConsentStatus reads a piece of work nobody has agreed to yet. The clock is
// the whole of it: a proposal that will start by itself is moving, and one that
// will sit there is the person's call.
func taskConsentStatus(status TaskStatus, facts TaskFacts) TaskStatus {
	status.Presence, status.On = TaskPresenceQueued, TaskWaitPerson
	if strings.TrimSpace(facts.Countdown) == "" {
		status.Presence = TaskPresenceNeedsLook
	}
	return status
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
		status.Presence, status.On = TaskPresenceNeedsLook, TaskWaitPerson
	case TaskFailed:
		if facts.Held {
			// THE ANSWER IS STILL THERE AND SOMEBODY MAY STILL WANT IT. A landing
			// that turned the work back is not the same news as work that ran out of
			// road: the check said no and what it produced is sitting on the record,
			// so taking it anyway is a real answer and the row has to offer it.
			status.Presence, status.On = TaskPresenceNeedsLook, TaskWaitPerson
			return status
		}
		status.Presence, status.On = TaskPresenceIncomplete, TaskWaitPerson
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
		status.On, status.Reason = TaskWaitMachine, hold
	}
	return status
}

// taskRunningStatus reads a node the graph is holding open, which is the one
// state with more than one honest answer in it.
func taskRunningStatus(status TaskStatus, facts TaskFacts) TaskStatus {
	switch {
	case facts.Paused:
		// A run standing at its fuel gate is not working: nothing new is launched
		// and nothing will be until somebody tops it up, finishes it or stops it.
		// It leads the switch because it outranks everything under it — a gap or a
		// pacing word left over from the moment the tank emptied describes work that
		// has since stopped moving, and reading either one first would put a
		// spinner's worth of "still going" over a question nobody has answered.
		status.Presence, status.On = TaskPresenceNeedsLook, TaskWaitPerson
	case facts.Kind == TaskKindHarness && facts.Phase == HarnessPhaseAsking:
		// The one piece of running work that is not running: the page is written
		// and the only remaining step is somebody approving it. Counted as working
		// it made surfaces say "1 running" about a card that had been waiting on
		// the person reading that line.
		status.Presence, status.On = TaskPresenceNeedsLook, TaskWaitPerson
	case facts.Life == TaskPhaseChecking || facts.Life == TaskPhaseRepairing:
		// The typed end of a run, so a round with nothing to say still reads as
		// finishing.
		status.Presence, status.Reason = TaskPresenceFinishing, strings.TrimSpace(facts.Gap)
	case strings.TrimSpace(facts.Gap) != "":
		status.Presence, status.Reason = TaskPresenceFinishing, strings.TrimSpace(facts.Gap)
	case strings.TrimSpace(facts.Hold) != "":
		// A paced node is not spending its time on the work, and saying it is
		// working would assert a present that is not happening.
		status.Presence, status.On = TaskPresenceWaiting, TaskWaitMachine
		status.Reason = strings.TrimSpace(facts.Hold)
	default:
		status.Presence, status.Reason = TaskPresenceWorking, strings.TrimSpace(facts.Phase)
	}
	return status
}

// taskEndingIsFault says whether something went wrong, which is what is left
// once every ending that is nobody's finding is taken out: the wire, the
// provider, a threshold, a loop, another task's copy and a rule the worker would
// not follow; a brief whose world had moved never started; a check that named
// gaps found work still to do; a person's stop is not a finding at all. What
// remains — a working copy that could not be made, an error nobody classified,
// and a row from an engine that named no ending — is a fault.
//
// EVERY ONE OF THESE HAS A SENTENCE AND THE SENTENCES ARE NOT HERE. What a
// person reads for each ending is [TaskReasonOf]'s table, spelled once; this
// answers the one thing that table does not, which is whether the row may be
// coloured as something having broken.
func taskEndingIsFault(ending TaskEnding) bool {
	switch ending {
	case TaskEndingStopped, TaskEndingWire, TaskEndingUpstream, TaskEndingCircling,
		TaskEndingBlocked, TaskEndingSteps, TaskEndingNotes, TaskEndingRefused, TaskEndingStale:
		return false
	}
	return true
}

// taskStatusDemand adds what the reading asks of a person, which is the one
// question both axes can answer. It does not overwrite the presence: where the
// work is and where its edits went stay separate answers.
func taskStatusDemand(status TaskStatus) TaskStatus {
	// Keeping a branch is a valid delivery workflow, not a request to merge.
	// A conflict or an unresolved review is the actionable condition.
	if (status.Changes == TaskChangesConflicted && status.ChangesUnlanded()) || status.Presence == TaskPresenceNeedsLook {
		status.Attention = true
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
// rather than copies. A word this build does not know reads as nothing to place.
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

// StatusFacts is one live notice as the reading takes it, so that the surface
// drawing a card, the note the model reads and the roster all ask the same
// function the same question about the same node.
//
// IT CLAIMS NOTHING THE NOTICE DOES NOT CARRY. A notice has no consent clock, no
// fuel figure, no prerequisite TITLES and no word for which of a running node's
// three lives it is in — those reach the reading from whoever is holding them
// (the surface's own [EventTaskPhase] fold, the proposal card's countdown) — and
// every one of them stays an absence here rather than a guess.
func (n TaskNotice) StatusFacts() TaskFacts {
	return TaskFacts{
		State:     n.State,
		Ending:    n.Ending,
		Kind:      n.Kind,
		Phase:     n.Doing,
		Gap:       n.Mending,
		Hold:      n.Waiting,
		Paused:    n.Paused,
		Stopped:   n.Stopped,
		Merge:     n.Merge,
		Branch:    n.Branch,
		Report:    n.Report,
		Held:      n.ResultHeld,
		Conflicts: n.Conflicts,
		Shifted:   n.Shifted,
		Decider:   n.Decider,
	}
}

// StatusFacts is one record row as the reading takes it. `held` is the caller's
// authoritative liveness ([SessionRow.Runs]); false becomes UNCLAIMED only
// because that method's ladder — a live conversation's claim list, then the lock
// — is a negative answer rather than the absence of one.
//
// A record row knows less than a live one. The index carries no merge word,
// branch, hold, gap or prerequisite, so a row read from it can say what state it
// is in and why it ended, and never where its edits are or what is holding it.
func (e TaskIndexEntry) StatusFacts(held bool) TaskFacts {
	facts := TaskFacts{
		State:  TaskState(e.Status),
		Ending: e.Ending,
		Kind:   e.Kind,
		Life:   strings.TrimSpace(e.Phase),
		// The row's own first sentence is the nearest thing a record keeps to the
		// report, and it is what the fault and the gaps are read out of
		// ([TaskReasonOf]). A row that never carried one simply says the word.
		Report: strings.TrimSpace(e.Outcome),
	}
	if e.Live() {
		facts.Liveness = TaskLivenessUnclaimed
		if held {
			facts.Liveness = TaskLivenessHeld
		}
	}
	return facts
}

// ── the words, spelled once ─────────────────────────────────────────────────

// The tier words, and they are the ONE spelling. Card head, rail, roster, home,
// the landing note the model reads and the `tasks` tool's reply all draw from
// here, because a state with two spellings is two states to whoever is reading.
//
// WHAT IS NOT HERE IS DELETED AS PERSON-FACING TEXT: `awaiting review`,
// `unverified`, `needs your look`, `failed`, `delivery needs attention`. Each of
// them was a different surface's private word for one of the words below.
const (
	taskWordQueued     = "queued"
	taskWordWaiting    = "waiting"
	taskWordWaitingOn  = "waiting on "
	taskWordAutoStarts = "auto-starts in "
	taskWordWorking    = "working"
	taskWordFinishing  = "finishing"
	taskWordDone       = "done"
	taskWordStopped    = "stopped"
	taskWordIncomplete = "incomplete"
	taskWordYourCall   = "your call"
)

// The reason sentences for an incomplete landing, one per ending, in the
// person's own words. `failed` is gone: the state keeps its name inside the
// engine and what somebody reads is `incomplete` plus one of these.
const (
	taskReasonWire     = "lost the connection"
	taskReasonUpstream = "the model provider refused it"
	taskReasonCircling = "went in circles"
	taskReasonBlocked  = "was blocked by another task"
	taskReasonSteps    = "ran out of steps"
	taskReasonNotes    = "would not write its notes down"
	taskReasonStale    = "its brief went stale"
	taskReasonRefused  = "would not take a step it was asked to"
	// taskReasonGaps and taskReasonFault are the two the ending alone cannot
	// answer: what the check found, and what broke. Both read the landing's own
	// report, which is the only place either sentence exists.
	taskReasonGaps  = "the check found gaps: "
	taskReasonFault = "a fault"
)

// The six questions a your-call row can be asking, and the two answers each one
// closes with. THEY ARE SPELLED ONCE HERE: a chip, a card's reason line and the
// note the model reads are three drawings of one sentence.
const (
	taskAskStartReason    = "starts on your word"
	taskAskApproveReason  = "design ready to approve"
	taskAskConflictReason = "conflicts with your branch"
	// taskAskShiftReason is the SECOND ROAD TO THE SAME QUESTION: the branch would
	// have fastened, and the person's own changed the same files while the work
	// ran (taskground.go). It is one question with two true sentences, and this
	// one has to say what happened — work that held its check and read `nobody
	// could check it` was the card lying about both halves.
	taskAskShiftReason = "your branch changed the same files while it worked"
	taskAskCheckReason = "nobody could check it"
	taskAskHeldReason  = "the check did not pass it"
	taskAskCapReason   = "paused at the "
	taskAskCapTail     = " cap"
	taskAskCapPlain    = "paused at the cap"

	taskAskStartYes    = "start"
	taskAskStartNo     = "don't"
	taskAskApproveYes  = "approve"
	taskAskApproveNo   = "decline"
	taskAskConflictYes = "resolve it"
	taskAskConflictNo  = "drop it"
	taskAskCheckYes    = "accept"
	taskAskCheckNo     = "not right"
	taskAskHeldYes     = "accept anyway"
	taskAskCapYes      = "raise the cap"
	taskAskCapNo       = "stop it"
)

// TaskReasonOf is the incomplete reason sentence for one ending, in the person's
// own words, and it is the ONE place that table is written down.
//
// The report is read for the two endings whose reason is not knowable from the
// word alone: a check that named gaps, whose finding is the first line of its own
// report, and a fault, whose first line is the only account of what broke. A
// stop is not here at all — `stopped` is its own word, not a kind of incomplete.
func TaskReasonOf(ending TaskEnding, report string) string {
	switch ending {
	case TaskEndingStopped:
		return ""
	case TaskEndingWire:
		return taskReasonWire
	case TaskEndingUpstream:
		return taskReasonUpstream
	case TaskEndingCircling:
		return taskReasonCircling
	case TaskEndingBlocked:
		return taskReasonBlocked
	case TaskEndingSteps:
		return taskReasonSteps
	case TaskEndingNotes:
		return taskReasonNotes
	case TaskEndingStale:
		return taskReasonStale
	case TaskEndingRefused:
		// THE CHECK'S OWN FINDING OUTRANKS THE WORD FOR IT. "Refused" is the
		// engine's name for both a check that named gaps and a worker that would
		// not take a step, and where the gaps were written down they are the answer
		// a person is actually owed.
		if gaps := taskGapsOf(report); gaps != "" {
			return taskReasonGaps + gaps
		}
		return taskReasonRefused
	}
	// An error, or a node that named no ending at all. The gaps are read first
	// because a landing carrying them was looked at, whatever else went wrong
	// afterwards, and a fault with nothing to say stays the bare word rather than
	// trailing off after a colon.
	if gaps := taskGapsOf(report); gaps != "" {
		return taskReasonGaps + gaps
	}
	if line := taskFirstLine(report); line != "" {
		return taskReasonFault + ": " + line
	}
	return taskReasonFault
}

// taskGapsOf is what the check said was missing, out of the landing's own report
// (task_audit.go's [gapsOutcome] wrote it). It is the FIRST round's first line:
// the rounds under it are the story of the work converging, which belongs on the
// card and not on a row.
func taskGapsOf(report string) string {
	report = strings.TrimSpace(report)
	if !strings.HasPrefix(report, incompleteLead) {
		return ""
	}
	return taskFirstLine(strings.TrimPrefix(report, incompleteLead))
}

// taskFirstLine is the report's opening line, trimmed, and "" for a report that
// is nothing but blank space.
func taskFirstLine(report string) string {
	for _, line := range strings.Split(report, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// taskStatusWords fills the tier, the word and the question — the three fields
// every surface draws from — from the reading above it. NOTHING OUTSIDE THIS
// PACKAGE COMPUTES A TIER: the glyph is the tier, and a surface working one out
// of the state for itself is the disagreement this file exists to end.
func taskStatusWords(status TaskStatus, facts TaskFacts) TaskStatus {
	switch status.Presence {
	case TaskPresenceQueued:
		status.Tier, status.Word = TaskTierMoving, taskWordQueued
		if facts.Consent {
			// A clock nobody stopped is the whole reason this needs nothing from
			// anybody: it starts by itself, and the row says when.
			status.Word = taskWordAutoStarts + strings.TrimSpace(facts.Countdown)
		}
	case TaskPresenceWaiting:
		status.Tier, status.Word = TaskTierMoving, taskWordWaiting
		if status.On == TaskWaitWork && status.Reason != "" {
			status.Word = taskWordWaitingOn + status.Reason
		}
	case TaskPresenceWorking:
		status.Tier, status.Word = TaskTierMoving, taskWordWorking
	case TaskPresenceFinishing:
		status.Tier, status.Word = TaskTierMoving, taskWordFinishing
	case TaskPresenceDone:
		status.Tier, status.Word = TaskTierOver, taskWordDone
	case TaskPresenceStopped:
		status.Tier, status.Word = TaskTierOver, taskWordStopped
	case TaskPresenceIncomplete:
		status.Tier, status.Word = TaskTierOver, taskWordIncomplete
		if status.Reason == "" && facts.State == TaskFailed {
			// A LANDED NODE HAS A REASON AND A ROW NOBODY IS RUNNING DOES NOT. The
			// other way into this presence is a live-looking row with an
			// authoritative negative behind it, and nothing was found out about that
			// work at all, so it says the word and stops.
			status.Reason = TaskReasonOf(facts.Ending, facts.Report)
		}
	case TaskPresenceNeedsLook:
		status.Tier, status.Word = TaskTierYourCall, taskWordYourCall
		status.Ask = taskAskOf(facts)
		status.Reason = status.Ask.Reason
	}
	return status
}

// taskAskOf is the closed set of your-call questions, in the order the most
// specific true fact wins — which is the same order the presence above it is
// read in, so a row and its card can never be asking two different questions.
func taskAskOf(facts TaskFacts) TaskAsk {
	ask := TaskAsk{Owner: taskDeciderOf(facts)}
	switch {
	case facts.Consent:
		ask.Kind, ask.Reason = TaskAskStart, taskAskStartReason
		ask.Yes, ask.No = taskAskStartYes, taskAskStartNo
	case facts.Paused:
		ask.Kind, ask.Reason = TaskAskCap, taskCapReason(facts.Cap)
		ask.Yes, ask.No = taskAskCapYes, taskAskCapNo
	case facts.Kind == TaskKindHarness && facts.Phase == HarnessPhaseAsking:
		ask.Kind, ask.Reason = TaskAskApprove, taskAskApproveReason
		ask.Yes, ask.No = taskAskApproveYes, taskAskApproveNo
	case facts.Shifted || taskChangesOf(facts.Merge) == TaskChangesConflicted:
		// TWO ROADS, ONE QUESTION. A branch that would not fasten and a ground that
		// moved under one that would are the same shape — two versions of the same
		// files, one on the task's branch and one on the person's — so they close
		// with the same two answers and differ only in the sentence that says what
		// happened ([taskShiftReason], task_run.go's [Agent.landShifted]).
		ask.Kind, ask.Reason = TaskAskConflict, taskShiftReason(facts)
		ask.Yes, ask.No = taskAskConflictYes, taskAskConflictNo
		// AND A CONFLICT IS NEVER THE MODEL'S. It cannot merge by decree, and no
		// settle policy hands it one: whatever a caller says about who is deciding,
		// two versions of somebody's own file are theirs (docs/design/task-states).
		ask.Owner = TaskAskOwnerPerson
	case facts.Held:
		ask.Kind, ask.Reason = TaskAskHeld, taskHeldReason(facts.Report)
		ask.Yes, ask.No = taskAskHeldYes, taskAskCheckNo
	default:
		ask.Kind, ask.Reason = TaskAskCheck, taskAskCheckReason
		ask.Yes, ask.No = taskAskCheckYes, taskAskCheckNo
	}
	return ask
}

// taskDeciderOf reads who is holding the question, and an unrecorded owner is
// the person: work whose owner nobody wrote down is work waiting on whoever is
// looking at it, which is the only safe way to be wrong about this.
func taskDeciderOf(facts TaskFacts) TaskAskOwner {
	if facts.Decider == TaskAskOwnerModel {
		return TaskAskOwnerModel
	}
	return TaskAskOwnerPerson
}

// taskShiftReason picks WHICH OF THE TWO SENTENCES this landing gets, off the
// fact and never off the prose: a ground that moved says so, and everything else
// on this road is a branch that would not fasten.
func taskShiftReason(facts TaskFacts) string {
	if facts.Shifted {
		return taskNamedFiles(taskAskShiftReason, facts.Conflicts)
	}
	return taskConflictReason(facts.Conflicts)
}

// taskConflictReason names the files that clash, and stops after the branch when
// git would not say which they were — a sentence ending in a bare colon has told
// nobody anything (task_run.go's [conflictSentence] states the same law).
func taskConflictReason(files []string) string {
	return taskNamedFiles(taskAskConflictReason, files)
}

// taskNamedFiles is that law, written once for both sentences: the files after a
// colon where there are any, and the sentence alone where there are not.
func taskNamedFiles(said string, files []string) string {
	named := make([]string, 0, len(files))
	for _, one := range files {
		if one = strings.TrimSpace(one); one != "" {
			named = append(named, one)
		}
	}
	if len(named) == 0 {
		return said
	}
	return said + ": " + strings.Join(named, ", ")
}

// taskHeldReason is the check's own finding, where the report carries one.
func taskHeldReason(report string) string {
	if gaps := taskGapsOf(report); gaps != "" {
		return taskAskHeldReason + ": " + gaps
	}
	return taskAskHeldReason
}

// taskCapReason names the amount the gate is holding at, and falls back to the
// plain sentence rather than drawing a hole where a figure would go.
func taskCapReason(amount string) string {
	if amount = strings.TrimSpace(amount); amount != "" {
		return taskAskCapReason + amount + taskAskCapTail
	}
	return taskAskCapPlain
}
