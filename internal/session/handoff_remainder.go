package session

// THE LAW: A CONVERSATION MAY STOP WITHOUT DELEGATING WHEN THE ONLY THING LEFT
// IS THE ENDING OF AN OPERATION IT ALREADY OWNS — AND IT SAYS WHICH ONE.
//
// A turn that has finished every artifact the person asked for and is left only
// waiting on a command it started has nothing a worker could be given: a cold
// worker cannot wait on a job in somebody else's registry. The stopped-turn door
// already knows this ([Agent.turnIsWaitingOnItsOwnWork]); the running-turn
// handover door could not see it, and made a task out of the wait. The measured
// cell is written up in the review under docs/design/conversation-runtime.
//
// The runtime knows WHICH operations are outstanding and cannot know whether the
// rest of the request is done, so the dowry ask the handover already makes
// ([checkpointHandoffAsk]) offers this conversation's own live operations, and
// the model may answer one exact line naming the ones it is waiting for. This is
// not a new model call, not a reading of prose, and not a running-job exemption:
// a live operation on its own suppresses nothing, and a remainder with real work
// in it is handed over exactly as before.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ── what may be awaited ─────────────────────────────────────────────────────

// ownedOperation is one background operation this conversation started that is
// still running. The number and the label are the surface's own (jobrow.go), so
// the ask and the roster cannot come to call one thing two names.
type ownedOperation struct {
	id    int
	label string
}

// awaitableOperations is the snapshot the ask offers and the only thing an await
// will ever be granted over. It is the registry's own answer, taken the way
// [Agent.jobsWorkingNow] takes it, and it holds no agent lock.
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

// awaitableKind is the whole of the kind policy. A background command and a fork
// hand each have one ending that is owed to this session and queued to wake it
// (agent.go wires the registry's announce to [Agent.enqueueJobNote]). A watch is
// a command re-run on a timer with no such ending, and a task node already ends
// a turn by its own road ([Agent.turnHandedItsAskOff]); widening this set is a
// ruling rather than a refactor.
func awaitableKind(kind jobKind) bool {
	return kind == jobKindBash || kind == jobKindHand
}

// ── the ground a decision stands on ─────────────────────────────────────────

// awaitGround is the request an offer was made under, together with the fact
// that the model's view of it was complete.
//
// THE SECOND HALF IS THE ONE A BARE EPOCH CANNOT GIVE. [Agent.Steer] mints its
// number when the words are ENQUEUED and the words reach the model only at the
// next boundary, so an epoch on its own can be unchanged across a correction the
// model has not read yet — and an await granted there would answer the old
// request. So the queue is read WITH the epoch, under one hold of the lock
// [Agent.Steer] appends to, and a session that is closed, not running, or
// holding anything of the person's is no ground at all.
type awaitGround struct {
	epoch requestEpoch
	sound bool
}

// awaitGroundNow reads that ground. It takes only the agent's own lock and calls
// nothing while holding it, so it can never sit in front of the job registry.
func (a *Agent) awaitGroundNow() awaitGround {
	if a == nil {
		return awaitGround{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || !a.running {
		return awaitGround{}
	}
	// Submit can enqueue an ordinary user message without a steer receipt.
	// Any unread message can change what the owner owes, so the next boundary
	// must incorporate the whole queue before an await can be granted.
	if len(a.steering) != 0 {
		return awaitGround{}
	}
	return awaitGround{epoch: requestEpoch{turn: a.turnSeq, steer: a.steerSeq.Load()}, sound: true}
}

// holds reports that a later reading of the ground is the same request, still
// sound. A dead epoch never holds.
func (g awaitGround) holds(later awaitGround) bool {
	return g.sound && later.sound && g.epoch.live() && g.epoch == later.epoch
}

// ── what the model is offered, and what it may answer ───────────────────────

// awaitOnlyToken opens the one line this contract accepts. It is a token the ask
// teaches rather than a phrase the harness sniffs for, exactly as
// [checkpointNothingLeft] is.
const awaitOnlyToken = "AWAITING"

// awaitLine is the whole grammar: the token, then one or more ids, single spaces
// throughout, no leading zeros, nothing before or after. A reply that says
// anything else — including this line with a paragraph after it — is a brief and
// is handed over as one.
var awaitLine = regexp.MustCompile(`^` + awaitOnlyToken + `( [1-9][0-9]{0,8})+$`)

// awaitOfferBlock is added to the dowry ask only where this conversation has
// something of its own running, so a session the failure was not about pays
// nothing for it. It names this session's live operations and no machinery.
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
		" — that word and the numbers you are waiting on, separated by single spaces, and nothing else at all. " +
		"If there is anything somebody could be working on now, write their instruction instead."
}

// readAwaitClaim answers the ids an exact await line names. Anything that is not
// that line answers nothing, because a reply the harness had to interpret is a
// reply it should be handing over instead.
func readAwaitClaim(answer string) ([]int, bool) {
	line := strings.TrimSpace(answer)
	if !awaitLine.MatchString(line) {
		return nil, false
	}
	fields := strings.Fields(strings.TrimPrefix(line, awaitOnlyToken))
	claimed := make([]int, 0, len(fields))
	seen := map[int]bool{}
	for _, field := range fields {
		id, err := strconv.Atoi(field)
		if err != nil || id <= 0 || seen[id] {
			// A REPEATED ID IS A MALFORMED CLAIM AND NOT A SET TO BE TIDIED. The
			// ask teaches one spelling, and a harness that quietly normalised a
			// second one would be teaching a grammar it never wrote down.
			return nil, false
		}
		seen[id] = true
		claimed = append(claimed, id)
	}
	return claimed, true
}

// ── what the runtime will grant ─────────────────────────────────────────────

// awaitDecision is what one claim came to, and it is the type the handover road
// consumes. refused is written whether or not a claim was made, so a file can
// tell a handover that wrote a brief from one that claimed an ended job.
type awaitDecision struct {
	granted bool
	ids     []int
	ground  awaitGround
	refused string
}

// The reasons a claim was not granted, in the register the carry ladder uses.
const (
	awaitNotClaimed   = "the model wrote a brief rather than an await"
	awaitNoOffer      = "nothing of this conversation's own was running to await"
	awaitUnknownID    = "it named something that was not offered"
	awaitEnded        = "what it named is no longer running"
	awaitRequestMoved = "the person's direction moved under it"
)

// confirmAwait grants a claim only where four runtime facts hold: something was
// offered; every id was on that offer; every id is still running when read
// again; and the request the offer was made under is still the request being
// answered, with nothing of the person's unread.
//
// AN OPERATION THAT ENDED WHILE THE MODEL WAS DRAFTING IS REFUSED, because its
// news is already on its way and the wait is over. Every refusal returns the
// same thing — not granted, with a reason — and the handover proceeds as it
// would have if none of this existed, which is the direction this errs in.
//
// The registry is walked first and the ground is read last, so no agent lock is
// ever held across a registry call.
func (a *Agent) confirmAwait(claimed []int, offered []ownedOperation, at awaitGround) awaitDecision {
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
	if !a.allStillRunning(claimed) {
		return awaitDecision{refused: awaitEnded}
	}
	if !at.holds(a.awaitGroundNow()) {
		return awaitDecision{refused: awaitRequestMoved}
	}
	return awaitDecision{granted: true, ids: claimed, ground: at}
}

// stillGranted is the SAME two facts read again at the moment the decision is
// acted on, because a grant taken a model call ago is evidence about a request
// that may since have moved. It is what the handover road branches on.
func (a *Agent) stillGranted(decision awaitDecision) bool {
	if !decision.granted {
		return false
	}
	return a.allStillRunning(decision.ids) && decision.ground.holds(a.awaitGroundNow())
}

// allStillRunning reports that every named operation is running now.
func (a *Agent) allStillRunning(ids []int) bool {
	running := make(map[int]bool)
	for _, operation := range a.awaitableOperations() {
		running[operation.id] = true
	}
	for _, id := range ids {
		if !running[id] {
			return false
		}
	}
	return true
}

// awaitedRow names a granted await in the journal, so an autopsy can see which
// ending the turn stopped for.
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
