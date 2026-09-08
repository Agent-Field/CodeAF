package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The no-progress guard, and why a leaf that is not making forward progress
// must be stopped.
//
// Measured: one bug replicate spent 1.3M tokens and 184 seconds with no forward
// progress — the model re-reading files it had already read, re-running the
// same checks, and circling. The token ceiling eventually caught it, but only
// after it had spent an order of magnitude more than the same task should ever
// cost, and the landing reserve meant the last several turns were paid for
// too. The turn cap at 400 is the only thing that would have reached it sooner,
// and 400 is the number that was raised from 40 precisely because it stopped
// honest complex work.
//
// What was missing is a signal that distinguishes "a leaf taking many turns
// because the work is hard" from "a leaf taking many turns because it is
// stuck". The difference is observable and it is general: a leaf that is
// making progress writes to the filesystem, reads files it has not read
// before, or calls tools with arguments it has not used before. A leaf that is
// not doing any of those three things is not working — it is repeating itself
// — and the measured pathology is exactly that shape.
//
// Three signals, and every one is a general criterion rather than a task-shape
// guess:
//
//   - Repeated identical tool calls: the same tool with the same arguments
//     producing the same result, several turns in a row. A model that reads
//     the same file and gets the same answer three times has no reason to
//     read it a fourth.
//   - A stagnant window: several consecutive turns with zero filesystem
//     mutations and zero new information. A big refactor might take many
//     turns, but it writes files between its thinking turns; a leaf that
//     goes five turns without writing anything and without learning anything
//     new is circling.
//   - A turn floor: past a generous multiple of what any honest leaf has ever
//     needed, the leaf is on rails no token bound can see. The floor is well
//     above the measured longest honest leaf (25 turns) and well below the
//     400-turn hard cap, and it fires into a conclude directive rather than a
//     guillotine.
//
// On any signal the guard first injects a conclude-now directive — one chance
// to land the work — and then, if the leaf is still spinning, terminates it
// with its partial result and a journaled reason. The two-stage exit is the
// same shape the budget landing and the straggler hand-back already take: the
// workspace is left consistent and the partial goes out whole, because the
// work is being returned rather than thrown away.
//
// The thresholds are deliberately generous. This is a tail-risk bound, not a
// budget: it exists to catch the one-in-a-hundred runaway, and it must never
// fire on the ninety-nine honest leaves that happen to be long. Every
// threshold is set clear of the measured distribution of well-sized leaves,
// and the guard errs on the side of firing late.

// StopNoProgress is the reason recorded when the guard terminates a leaf that
// was not making forward progress. It is separate from StopBudget and
// StopTurnCap because it is a different finding: the leaf had money and turns
// left, and was spending both without advancing. Grading it as a budget stop
// would teach the ruler nothing it does not already learn from the budget; the
// distinct reason is what lets the recalibration path see that the node was
// sized correctly and the model was the thing that stalled.
const StopNoProgress StopReason = "no-progress"

// noProgressRepeatCap is how many consecutive identical tool calls (same tool,
// same arguments, same result) the guard tolerates before concluding the leaf
// is repeating itself.
//
// Four is generous against the measured shape of honest work: a model that
// reads a file, processes it, and re-reads to confirm a detail makes the same
// call twice, and a model that runs the same check after a small edit makes
// it twice more. A fourth identical call with an identical result, after the
// workspace has not changed, is the model going round in a circle.
const noProgressRepeatCap = 4

// noProgressStagnantCap is how many consecutive turns with zero filesystem
// mutations and zero new information the guard tolerates.
//
// Six sits clear of the honest leaf that thinks between writes. A big refactor
// might spend two or three turns reasoning before each edit, and a leaf that
// reads several files before acting can go four or five turns between
// mutations. Six consecutive turns with neither a mutation nor a single new
// result is the leaf circling, and the cap fires into a conclude directive
// rather than a stop.
const noProgressStagnantCap = 6

// noProgressTurnFloor is the turn count past which the guard stops needing
// the stagnant signal to fire — but only against a leaf whose recent window
// is mostly unproductive. The signals above catch fast spins (identical
// repeats, six flat turns); what they cannot catch is the leaf that fidgets
// just enough to dodge them: one write or one fresh read every few turns,
// forever. The floor names that pattern: past this many turns with fewer
// than noProgressFloorMinProductive productive turns in the recent window,
// the leaf is on rails no token bound can see, and the conclude directive is
// the gentle way to land it. A leaf that is genuinely working — mutations or
// new information in its recent window — is never floor-stopped: the leaf's
// own turn and token budgets own that ceiling, exactly as the engine's step
// budgets do.
const noProgressTurnFloor = 60

// noProgressFloorWindow and noProgressFloorMinProductive shape the recent
// window the floor reads: of the last twenty turns, fewer than five with a
// mutation or new information is fidgeting. Five-of-twenty is exactly the
// slowest honest cadence the signals already tolerate (one productive turn
// every five keeps the stagnant counter from ever reaching six), so any leaf
// the floor catches was already circling by the signals' own standard.
const (
	noProgressFloorWindow        = 20
	noProgressFloorMinProductive = 5
)

// noProgressConcludeDirective is the one message the guard injects to give a
// spinning leaf a single chance to land its work. It is stated as a fact
// rather than a judgment — the guard has observed no progress, and the leaf
// must conclude — and it mirrors the budget and deadline landing directives
// in shape so the model reads it as the same kind of instruction.
const noProgressConcludeDirective = "This task is not making forward progress — the same tool calls " +
	"are repeating without changing anything or learning anything new. Conclude " +
	"now: state your deliverable in the body of your reply, reporting what you " +
	"finished and what you did not. Do not make any more tool calls."

// mutationTools is the set of tool names that always write to the filesystem.
// A turn containing any of these is a turn that mutated something, regardless
// of what the result said. The set is general — these are the harness's write
// tools, not a guess about what the work is — and it errs on the side of
// marking a turn as mutating, which is the safe direction for the guard: a
// false mutation never fires the stagnant signal, so the guard errs toward
// leaving the leaf alone.
var mutationTools = map[string]bool{
	"write":       true,
	"edit":        true,
	"apply_patch": true,
}

// progressGuard tracks whether a leaf is making forward progress, across the
// turns of one Run. It is not safe for concurrent use; one leaf is one loop.
type progressGuard struct {
	// Signal 1: the last tool call's signature and result, and how many times
	// in a row it has repeated identically.
	lastCallKey   string
	lastResultKey string
	repeatCount   int

	// Signal 2: how many consecutive turns have had no filesystem mutation and
	// no new information.
	stagnantTurns int

	// Signal 3: total turns observed.
	turns int

	// productiveWindow is the ring of the last noProgressFloorWindow turns'
	// productivity (a turn with a mutation or new information), and
	// productiveCount how many of those held true. The floor reads it to tell
	// a fidgeting leaf from a working one.
	productiveWindow [noProgressFloorWindow]bool
	productiveCount  int

	// seenResults is the set of all result content hashes the leaf has ever
	// been shown. A result whose hash is already here is "not new information";
	// a result whose hash is not is "new information" and resets the stagnant
	// counter.
	seenResults map[string]bool

	// concluded records whether the conclude directive has already been
	// injected. The guard gives one chance — a bounded grace period — and
	// then terminates.
	concluded bool
	// concludeRemaining is how many turns of grace are left after the
	// directive was injected. It is set to landingTurns so the guard's
	// landing matches the budget's: enough to write a file, run one check,
	// and deliver, but not enough to keep working.
	concludeRemaining int

	// mu guards seenResults, which is read and written from the tool-execution
	// goroutines in the loop. The rest of the guard is touched only from the
	// main loop goroutine, at turn boundaries.
	mu sync.Mutex
}

func newProgressGuard() *progressGuard {
	return &progressGuard{seenResults: map[string]bool{}}
}

// readTurn is the half of observe that touches seenResults, and it is its own
// function for one reason: the lock it takes is released by a defer directly
// under it. Written inline, the critical section spanned a loop whose body an
// absorbed panic could leave the mutex held forever — a deadlock in place of a
// recovered turn. What it reads out is the turn's three facts; everything after
// it is the main loop's own state, which this mutex has never guarded (see mu).
func (g *progressGuard) readTurn(calls []ai.ToolCall, results []Result) (mutated, newInfo, anyError bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for index, call := range calls {
		if mutationTools[call.Function.Name] {
			mutated = true
		}
		if results[index].IsError {
			// An error is always new information: the model learned that
			// something does not work, which is forward progress even if
			// nothing was written. Counting errors as "not new" would make
			// the stagnant signal fire on a model that is failing its way
			// through different approaches, which is the opposite of
			// spinning.
			anyError = true
			newInfo = true
			continue
		}
		key := resultKey(results[index])
		if !g.seenResults[key] {
			g.seenResults[key] = true
			newInfo = true
		}
	}
	return mutated, newInfo, anyError
}

// callSignature is the key for signal 1: the tool name plus its arguments,
// canonicalised. Two calls with the same signature asked the same thing.
func callSignature(call ai.ToolCall) string {
	return call.Function.Name + "\x00" + call.Function.Arguments
}

// resultKey is a content hash of what the tool returned, used both for the
// repeat signal (signal 1) and for the new-information signal (signal 2).
func resultKey(result Result) string {
	body := result.Content
	if result.IsError {
		body = "ERROR:" + body
	}
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// observe records one turn's tool activity and returns the guard's verdict.
//
// It is called once per turn, after all the turn's tool calls have completed
// and their results are known. calls and results are the turn's calls and
// their results, in order; mutationsBefore and mutationsAfter are the
// workspace's monotonic per-leaf revisions — a change means something was
// written to disk, including a rewrite of an already-known path.
//
// The verdict is one of: progressContinue (the leaf is advancing),
// progressConclude (inject the conclude directive — the first trigger), or
// progressTerminate (the leaf was already told to conclude and is still
// spinning — stop it now).
type progressVerdict int

const (
	progressContinue progressVerdict = iota
	progressConclude
	progressTerminate
)

func (g *progressGuard) observe(
	calls []ai.ToolCall, results []Result,
	mutationsBefore, mutationsAfter int,
) progressVerdict {
	g.turns++

	// A turn with no tool calls is a thinking turn — it carries no signal
	// either way. Counting it as stagnant would punish a model that reasons
	// between actions; counting it as progress would let a model that emits
	// empty turns run forever. The guard leaves the stagnant counter where it
	// is: a thinking turn neither advances nor stalls the clock.
	//
	// If the conclude directive was already injected, a turn with no tool
	// calls is the model complying — it is delivering its answer — so the
	// guard does nothing. The loop's no-tool-calls path handles the finish.
	if len(calls) == 0 {
		return progressContinue
	}

	mutated, newInfo, anyError := g.readTurn(calls, results)

	// A revision change means a file landed on disk — a mutation by any tool,
	// including sh, which produces files the mutation-tools set does not name.
	if mutationsAfter > mutationsBefore {
		mutated = true
	}

	// Signal 1: repeated identical tool calls. The check is on the LAST call
	// of the turn, because a turn may carry several calls and the repeat that
	// matters is the one the model keeps making across turns. If the last
	// call's signature and result match the previous turn's last call, the
	// streak continues; otherwise it resets.
	//
	// Error results are excluded: a model that retries a failing call is
	// troubleshooting, not spinning. The same call with the same error is
	// the model trying again, which is different from the same call with
	// the same successful answer, which is the model going in a circle.
	lastCall := calls[len(calls)-1]
	lastSig := callSignature(lastCall)
	lastRes := resultKey(results[len(results)-1])
	if !anyError && lastSig == g.lastCallKey && lastRes == g.lastResultKey {
		g.repeatCount++
	} else {
		g.repeatCount = 1
	}
	g.lastCallKey = lastSig
	g.lastResultKey = lastRes

	// Signal 2: stagnant turns. A turn that mutated or learned something
	// resets the counter; a turn that did neither advances it. Errors count
	// as new information (see above), so a failing leaf never registers as
	// stagnant.
	if mutated || newInfo {
		g.stagnantTurns = 0
	} else {
		g.stagnantTurns++
	}
	// The floor's window records the same judgment: this turn was productive
	// or it was not.
	{
		slot := int(g.turns-1) % noProgressFloorWindow
		if g.productiveWindow[slot] {
			g.productiveCount--
		}
		g.productiveWindow[slot] = mutated || newInfo
		if g.productiveWindow[slot] {
			g.productiveCount++
		}
	}
	// If the conclude directive was already injected, the guard counts down
	// a bounded grace period — the same landingTurns the budget reserve
	// grants — so the model can write a file, run one check, and deliver.
	// A turn that makes progress (mutation or new information) resets the
	// stagnant and repeat counters but still consumes a grace turn; a turn
	// that re-triggers any signal terminates immediately, because the model
	// is spinning despite being told to conclude.
	if g.concluded {
		if g.repeatCount >= noProgressRepeatCap || g.stagnantTurns >= noProgressStagnantCap {
			return progressTerminate
		}
		g.concludeRemaining--
		if g.concludeRemaining <= 0 {
			return progressTerminate
		}
		return progressContinue
	}

	// Signal 1: the same call repeating identically past the cap.
	if g.repeatCount >= noProgressRepeatCap {
		return progressConclude
	}
	// Signal 2: a stagnant window past the cap.
	if g.stagnantTurns >= noProgressStagnantCap {
		return progressConclude
	}
	// Signal 3: a turn floor, read against the recent window. Past the floor
	// a leaf whose window is mostly unproductive is fidgeting — productive
	// just often enough to dodge the stagnant cap, never enough to finish —
	// and the conclude directive lands it. A leaf whose window shows real
	// work is left to its own budgets.
	if g.turns >= noProgressTurnFloor && g.productiveCount < noProgressFloorMinProductive {
		return progressConclude
	}

	return progressContinue
}

// markConcluded sets the conclude state: the directive has been injected,
// and the model has landingTurns to finish. It is called once, at the first
// trigger, and observe() reads the resulting state on every turn after.
func (g *progressGuard) markConcluded() {
	g.concluded = true
	g.concludeRemaining = landingTurns
}

// noProgressReason assembles the journaled reason for a guard termination, in
// the guard's own voice, naming the signal that fired. It is written to the
// trace and to the outcome so whatever routed the leaf can see why it was
// stopped.
func (g *progressGuard) noProgressReason() string {
	var reason strings.Builder
	reason.WriteString("no-progress guard: ")
	switch {
	case g.repeatCount >= noProgressRepeatCap:
		reason.WriteString("the same tool call repeated ")
		// repeatCount counts the current streak including the first
		// occurrence, so the number of REPEATS is one less.
		if n := g.repeatCount - 1; n > 0 {
			// write the count without importing strconv at the top — it
			// is already imported by other files in this package.
			reason.WriteString(itoa(n))
		}
		reason.WriteString(" times with identical results")
	case g.stagnantTurns >= noProgressStagnantCap:
		reason.WriteString(itoa(g.stagnantTurns))
		reason.WriteString(" consecutive turns with no filesystem mutations and no new information")
	default:
		reason.WriteString("ran past ")
		reason.WriteString(itoa(noProgressTurnFloor))
		reason.WriteString(" turns with no forward progress")
	}
	return reason.String()
}

// itoa is a local integer-to-string to avoid adding strconv to the import
// list of a file whose only other use of it would be this one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
