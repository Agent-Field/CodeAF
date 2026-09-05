package session

// THE LAW: A CONVERSATION MAY STOP WITHOUT DELEGATING WHEN THE ONLY THING LEFT
// IS THE ENDING OF AN OPERATION IT IS ALREADY OWED — AND IT SAYS WHICH ONE.
//
// ── THE MEASURED FAILURE (calibration-02, cell 018-revision-midwork-aforge) ──
//
// A person asked for two things in one sentence: run ./slow-build.sh ("it takes
// about a minute and I want it done"), and write a report. The turn started the
// build in the background as job 1, wrote the report, took the person's revision
// mid-turn, wrote report.csv and removed report.md — every artifact they asked
// for — and the write seam then fired on the second file. The mark's reader drew
// `(waiting)`, which is not one of [checkpointDoneShapes], so the two-minds
// decline could not fire, and task 2 was admitted carrying a brief whose whole
// content was "wait for ./slow-build.sh to finish … then verify report.csv".
//
// Forty-two seconds later job 1's own ending woke the conversation and it
// answered correctly in one round. The task, meanwhile, spawned a repair child
// and an audit child, and the cell ran to its 180-second cap.
//
// ── WHAT WAS ACTUALLY MISSING ──
//
// Not a threshold. At the moment of the handover the conversation held ONE
// outstanding obligation, it OWNED that obligation, and the obligation's ending
// was already queued to wake it ([Agent.enqueueJobNote]). There was nothing a
// worker could have been given: a cold worker cannot wait on a job in somebody
// else's registry, and the verification it would do is a re-reading of files
// this turn had just written.
//
// [Agent.turnIsWaitingOnItsOwnWork] already states exactly this reasoning at the
// STOPPED-turn door (checkpoint.go). The RUNNING-turn handover door could not
// see it. This file is that fact, made available to the one road that lacked it,
// as a typed answer rather than as a second opinion about English.
//
// ── WHY THE MODEL IS ASKED, AND WHAT IT IS ASKED FOR ──
//
// The runtime knows WHICH operations are outstanding; it does not know whether
// the person's request is finished apart from them. Only the model that spent
// the turn knows that. So the dowry ask it already answers
// ([checkpointHandoffAsk]) is extended, AND ONLY WHERE THERE IS SOMETHING REAL
// TO OFFER, with the operations this conversation is genuinely awaiting, by
// number and by name. The model may then answer one more typed line —
// [awaitOnlyToken] with those numbers — which means: everything else the person
// asked for is done here, and what is left is these endings.
//
// IT IS NOT A NEW MODEL CALL. It is one more answer to a call this road already
// makes and already pays for.
//
// IT IS NOT A KEYWORD READING. The numbers are checked against the snapshot the
// ask offered, the operations are checked to be still running, and the request
// is checked not to have moved under it. A claim the runtime cannot verify is
// not refused politely — it is simply not an await, and the work is handed over
// exactly as it would have been.
//
// ── AND THE THREE THINGS IT IS NOT ──
//
//   - IT IS NOT "NOTHING LEFT TO DO". That token says the request is discharged;
//     this one says a named obligation is still owed and is somebody's — this
//     conversation's — to receive. They are journaled apart ([carryAwaited]
//     against [carryNothingLeft], [checkpointCeilingAwaiting] against
//     [checkpointCeilingNothing]) because a bench that spelled them alike could
//     not tell a finished turn from a waiting one.
//   - IT IS NOT A RUNNING-JOB EXEMPTION. A live job on its own suppresses
//     nothing: a turn with a build in flight and a rename still to do hands the
//     rename over exactly as it did before this file existed. What suppresses
//     the handover is the MODEL SAYING the remainder is only those endings.
//   - IT IS NOT A TASK, A WATCH BUS OR A UNIVERSAL OPERATION GRAPH. It is
//     background commands and fork hands in THIS agent's own registry, which are
//     the operations whose ending is wired to wake THIS agent by construction
//     (agent.go's `newJobRegistry(…, agent.enqueueJobNote, …)`). Task nodes have
//     their own road ([Agent.turnHandedItsAskOff]) and their own custody
//     reduction (checkpoint_custody.go); watches are left out because a watch is
//     a command re-run on a timer, and "wait for it to end" is not a thing a
//     person can be owed. Extending the set is a ruling, not a refactor.

import (
	"fmt"
	"strconv"
	"strings"
)

// ── what may be awaited ─────────────────────────────────────────────────────

// ownedOperation is ONE background operation this conversation started, still
// running, whose ending comes back here by itself.
//
// The two fields are what the ask has to show and what the journal has to name:
// the number the surface already gives it (`job 1`, jobs list's own spelling)
// and the line a person would recognise it by (jobrow.go's [jobRowTitle], so the
// ask and the roster cannot come to call one thing two names).
type ownedOperation struct {
	id    int
	label string
}

// awaitableOperations is the snapshot the dowry ask offers, and it is the whole
// of what this file will ever grant an await over.
//
// IT IS THE REGISTRY'S OWN ANSWER, taken the way [Agent.jobsWorkingNow] takes
// it, so the numbers a model is shown are the numbers a person is looking at.
// The two filters are the whole policy: RUNNING, because an operation that has
// ended is news to read rather than work to wait for; and a KIND WHOSE ENDING IS
// OWED — a background command or a fork hand — because those are the ones whose
// exit is queued as a note that starts a turn here on its own.
//
// A session that has started nothing answers nil, and nothing is offered at all.
func (a *Agent) awaitableOperations() []ownedOperation {
	if a == nil || a.jobs == nil {
		return nil
	}
	var out []ownedOperation
	for _, one := range a.jobs.all() {
		info := one.info()
		if info.state != jobRunning || !awaitableKind(info.kind) {
			continue
		}
		out = append(out, ownedOperation{id: info.id, label: jobRowTitle(info)})
	}
	return out
}

// awaitableKind is the one place the KIND policy is written down.
//
// A WATCH IS DELIBERATELY NOT ONE. It is a command re-run on a timer with no
// ending of its own to be owed, and a turn that stopped "until the watch fires"
// is the five measured minutes [checkpointCarryOnCap] was written from — a
// different failure with a different answer. A task node is not one either: its
// landing already ends a turn through [Agent.turnHandedItsAskOff], and a second
// road to the same fact is the thing this whole area has too many of.
func awaitableKind(kind jobKind) bool {
	return kind == jobKindBash || kind == jobKindHand
}

// ── what the model is offered, and what it may answer ───────────────────────

// awaitOnlyToken is the line the ask teaches, and it is a token for
// [checkpointNothingLeft]'s reason: a thing the model CHOSE to say, matched
// whole, rather than a phrase the harness thought it heard. The numbers follow
// it on the same line.
const awaitOnlyToken = "AWAITING"

// awaitOfferBlock is what is added to [checkpointHandoffAsk] WHEN AND ONLY WHEN
// this conversation is awaiting something.
//
// IT ENUMERATES THIS SESSION'S OWN LIVE OPERATIONS AND NOTHING ELSE. There is no
// vocabulary of kinds, no table of states and no explanation of the machinery: a
// model that is shown "job 1 (./slow-build.sh)" and told what answering with its
// number means has been told everything the harness will act on. An ask that
// described the operation model in general would be paying for a paragraph on
// every handover a session ever makes.
//
// AND IT NAMES THE DISTINCTION IT IS ASKING ABOUT, because that distinction is
// the whole reading: the person's request being finished APART FROM these
// endings, versus there being work left that somebody else could do now.
func awaitOfferBlock(operations []ownedOperation) string {
	if len(operations) == 0 {
		return ""
	}
	names := make([]string, 0, len(operations))
	numbers := make([]string, 0, len(operations))
	for _, operation := range operations {
		names = append(names, fmt.Sprintf("job %d (%s)", operation.id, operation.label))
		numbers = append(numbers, strconv.Itoa(operation.id))
	}
	return " Started here and still running: " + strings.Join(names, ", ") +
		" — each one's ending comes back to this conversation on its own. " +
		"If everything else the person asked for is already done and all that is left is those endings, " +
		"answer with the single line " + awaitOnlyToken + " " + strings.Join(numbers, " ") +
		" (that word and the numbers you are still waiting on, nothing else at all). " +
		"If there is anything somebody could be working on now, write their instruction instead."
}

// readAwaitClaim reads an answer for the typed await line and answers the
// numbers it claims.
//
// THE WHOLE ANSWER OR NOTHING. A reply that is the token, the numbers and then a
// paragraph is a reply that says two things, and a harness that took the first
// and dropped the second would be dropping real work on an ambiguity. So the
// trimmed answer must be that one line and nothing more; anything else reads as
// an ordinary brief and is handed over as one.
//
// AND A CLAIM WITH NO NUMBERS IS NOT A CLAIM. `AWAITING` alone names no
// operation, so there is nothing to verify and nothing to grant.
func readAwaitClaim(answer string) ([]int, bool) {
	line := strings.TrimSpace(answer)
	rest, cut := strings.CutPrefix(line, awaitOnlyToken)
	if !cut {
		return nil, false
	}
	fields := strings.FieldsFunc(rest, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '#'
	})
	if len(fields) == 0 {
		return nil, false
	}
	claimed := make([]int, 0, len(fields))
	seen := map[int]bool{}
	for _, field := range fields {
		id, err := strconv.Atoi(field)
		if err != nil {
			// ANY WORD THAT IS NOT A NUMBER ENDS IT. `AWAITING the build` is prose
			// about a wait, not a reference to an operation, and guessing which one
			// it meant is the reading this file exists to avoid.
			return nil, false
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		claimed = append(claimed, id)
	}
	return claimed, true
}

// ── what the runtime will grant ─────────────────────────────────────────────

// awaitDecision is what one await claim came to, and it is the type
// [Agent.handOverRunningTurn] consumes exactly once.
//
// granted is the only field the road branches on. refused is for the journal and
// is written whether or not a claim was made, because "the model did not claim
// an await" and "it claimed one over a job that had already exited" are
// different facts about a handover and a file that spelled them alike could not
// tell them apart afterwards.
type awaitDecision struct {
	granted bool
	ids     []int
	refused string
}

// The reasons a claim was not granted. They are the harness's own words, in the
// register the carry ladder's reasons are written in.
const (
	awaitNotClaimed   = "the model wrote a brief rather than an await"
	awaitNoOffer      = "nothing of this conversation's own was running to await"
	awaitUnknownID    = "it named something that was not offered"
	awaitEnded        = "what it named is no longer running"
	awaitRequestMoved = "the person said something after the offer"
)

// confirmAwait is the VALIDATION, and every clause of it is a runtime fact.
//
// FOUR THINGS ARE CHECKED AND ALL FOUR MUST HOLD:
//
//  1. SOMETHING WAS OFFERED. A claim over an empty snapshot is a claim about
//     nothing; a session with no live operation of its own can never reach the
//     decline through this door.
//  2. EVERY NUMBER WAS ON THE OFFER. An id the ask did not show is an id the
//     model invented or remembered from earlier in the conversation, and neither
//     is an operation this turn is owed.
//  3. EVERY OPERATION IS STILL RUNNING, READ AGAIN NOW. This is the settled-job
//     race and it is resolved CONSERVATIVELY: an operation that ended between the
//     offer and this line has already queued its news, so the honest answer is
//     that the wait is over rather than that the turn may stop for it. Refusing
//     here costs a handover that might not have been needed; granting here risks
//     a turn that stops for an ending nobody is bringing.
//  4. THE REQUEST HAS NOT MOVED. [requestEpoch] is the turn and the sentences
//     the person has spliced into it (turnhandoff.go). A steer that landed while
//     the model was drafting is new direction, and an await granted over it would
//     be the harness answering the old request. A dead epoch — no turn running —
//     fails this for the same reason.
//
// AND WHAT IT NEVER DOES IS SUPPRESS WORK ON A DOUBT. Every failure above
// returns the same thing: not granted, with the reason written down, and the
// handover proceeds exactly as it would have if this file did not exist.
func (a *Agent) confirmAwait(claimed []int, offered []ownedOperation, at requestEpoch) awaitDecision {
	if len(claimed) == 0 {
		return awaitDecision{refused: awaitNotClaimed}
	}
	if len(offered) == 0 {
		return awaitDecision{refused: awaitNoOffer}
	}
	wasOffered := make(map[int]bool, len(offered))
	for _, operation := range offered {
		wasOffered[operation.id] = true
	}
	for _, id := range claimed {
		if !wasOffered[id] {
			return awaitDecision{refused: awaitUnknownID}
		}
	}
	running := make(map[int]bool)
	for _, operation := range a.awaitableOperations() {
		running[operation.id] = true
	}
	for _, id := range claimed {
		if !running[id] {
			return awaitDecision{refused: awaitEnded}
		}
	}
	// THE EPOCH IS READ LAST, AND IT IS READ NOW rather than remembered: what
	// matters is whether the person has spoken since the offer went out, and the
	// answer to that is only knowable at the moment the decision is taken.
	if now := requestEpochAt(a); !at.live() || now != at {
		return awaitDecision{refused: awaitRequestMoved}
	}
	return awaitDecision{granted: true, ids: claimed}
}

// awaitedRow is how a granted await names itself in the journal: the word and
// the numbers, so an autopsy can see WHICH ending the turn stopped for.
func (d awaitDecision) awaitedRow() string {
	if !d.granted {
		return d.refused
	}
	numbers := make([]string, 0, len(d.ids))
	for _, id := range d.ids {
		numbers = append(numbers, strconv.Itoa(id))
	}
	return "awaiting job " + strings.Join(numbers, ", ")
}
