package resident

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
)

const (
	// ServiceConsentGrace is the bounded hold after a leaf asks to keep an
	// otherwise unconsented process. Silence always lands on the stop default.
	ServiceConsentGrace = 30 * time.Second
	serviceConsentPoll  = 100 * time.Millisecond
)

// faultNotice is what the thread says when a leaf hit a panic. It names the
// consequence and the receipt, and nothing else: a stack trace belongs in the
// log, not in the user's reading.
const faultNotice = "this task hit an internal fault — recorded to the log; the rest of the board is unaffected"

// ExecResult is what one execution produced: the summary that flows to
// dependents, and what producing it cost.
type ExecResult struct {
	Summary          string
	PromptTokens     int
	CompletionTokens int
	// CachedTokens is the share of PromptTokens the provider billed at the
	// cached rate. The executor has always measured it and the journal now has
	// somewhere to put it; carrying it here is what joins the two, and without
	// it every cache discipline the harness practises stays unfalsifiable from
	// the outside.
	CachedTokens int
	Cost         float64
	// Turns is the same spend with its shape kept: one row per model call,
	// summing to the three totals above. It rides here for the same reason
	// CachedTokens does — the executor has always known it and the journal now
	// has somewhere to put it — and an executor that does not meter turns leaves
	// it empty, which journals nothing rather than journalling a zero.
	Turns []executor.TurnUsage
	// Promote asks the runner to settle this reflex partial and enqueue the
	// same verbatim instruction on the ordinary compiled path atomically.
	Promote         bool
	ServiceRequests []executor.ServiceRequest
	// Model is who actually served the work — the rung a panel picked or an
	// escalation moved to, which is not the model anyone asked for. It rides
	// the spend row because that is the row a receipt already reads.
	Model string
	// SpendBanked says this run banked its own calls as they were billed — one
	// usage row per response, written at the moment the provider answered
	// (cmd/aforge's leafBanker, provider.WithBilling) — so the three totals
	// above are the REMAINDER and not the whole. On an ordinary banked run they
	// are zero and nothing more is written; they are non-zero when part of the
	// leaf ran on a worker whose calls the adapter never saw, and that part is
	// journaled exactly as it always was.
	//
	// The per-turn shape below is NOT a duplicate and is written either way —
	// it is a different kind of record, one row per turn rather than one per
	// call, and nothing else in the journal carries it. Which is why the zero
	// check above asks this field as well: a fully banked run has nothing left
	// to sum and its turn ledger still has to reach the journal.
	SpendBanked bool

	// HOW THE WORK ENDED, WHICH IS A DIFFERENT FACT FROM WHAT IT PRODUCED.
	//
	// A leaf that ran out of its tokens mid-edit and a leaf that finished
	// reached this seam wearing the same three fields — a summary, some money,
	// some turns — so the scheduler saw err == nil and settled the node done.
	// A judge's "nothing is left" then stood as the whole account of a worker
	// that had been cut off with a red build: measured on 2026-09-01, a leaf
	// stopped at `⏳ ran out of tokens — 33 turns` and was ✓ two seconds later,
	// its summary its own last sentence, and its siblings briefed on truncated
	// work. Nothing that runs after that ✓ can recover the cut work.
	//
	// Stopped is asked BEFORE Stop, and it is not redundant with it. A
	// StopReason is a string; its zero value is the empty string; and a result
	// built by any caller that does not fill these in would otherwise read as a
	// leaf that stopped for no reason, which is indistinguishable from one that
	// finished. Absence is said out loud.
	Stopped bool
	// Stop is the executor's own word for the ending — "budget", "turn-cap",
	// "deadline", "done".
	Stop executor.StopReason
	// Meter is what ran out and how far the leaf got, read at land time. It is
	// carried for the record rather than for the decision: the decision is
	// RanOut, and this is what a person and an autopsy are owed about it.
	Meter executor.Meter
	// Continued says something else is already carrying this leaf's remaining
	// work — a spliced continuation, a repair journaled against the daily rail.
	// A leaf that ran out and HAS a successor is finished with; one that ran out
	// with nothing behind it has no account, and the scheduler must not write
	// one for it. See Runner.runOne.
	Continued bool
}

// RanOut reports that this leaf STOPPED BECAUSE IT RAN OUT, and that nothing is
// carrying the rest of its work.
//
// A LEAF THAT RAN OUT HAS NO ACCOUNT. Its work is evidence for the next
// attempt, never a result: only a leaf that itself reported done, inside its
// budget, settles a node. Exhaustion is a measured fact and doneness is a claim,
// and a measured fact is never overturned by an unmeasured claim.
func (r ExecResult) RanOut() bool {
	return r.Stopped && !r.Continued && r.Stop.OutOfRoom()
}

// ExecuteFunc runs one claimed node to completion. The runner owns the claim
// lifecycle around it; the function owns nothing but the work.
type ExecuteFunc func(ctx context.Context, node store.Node) (ExecResult, error)

// ExpandFunc is asked, after a node has been claimed and before a worker is
// given it, whether the node is really one worker's job.
//
// It is the one moment the question can be asked well: the claim proves every
// piece feeding this node has landed, so the division can be decided against
// what they produced rather than against what they were called. Answering "no"
// means the hook has already put the parts in the graph and handed the claim
// back, and the node must not be run — it is structure now, and its parts are
// ready. Answering "yes" — which is the overwhelmingly common answer and the
// null hypothesis — means nothing happened and the node runs exactly as it
// always did.
type ExpandFunc func(ctx context.Context, node store.Node) (spliced int, expanded bool)

// Runner drains ready nodes from the durable graph and executes them. It is
// the store-side counterpart of the one-shot scheduler: any process may run
// one, claims make ownership a compare-and-swap, and a crashed runner leaves
// nothing worse than claimed nodes another Release can recover.
type Runner struct {
	graph               *store.Store
	execute             ExecuteFunc
	owner               string
	slots               chan struct{}
	wg                  sync.WaitGroup
	dailyBudgetUSD      float64
	activeMu            sync.Mutex
	activePractice      map[string]context.CancelFunc
	serviceConsentGrace time.Duration
	governor            *executor.Governor
	// passFaults counts panics recovered *inside a dispatch pass* — the ones
	// that end the pass early without ending the loop. A leaf that faults in
	// its own goroutine is not one of these: it lands failed through the
	// ordinary path and journals its landing. Only these say the pass stopped
	// looking before it was done, which is what the dispatch loop's quiet gate
	// is not allowed to mistake for "nothing to do."
	passFaults atomic.Int64
	craft      *CraftRunner
	// staleAge bounds how long a claim may sit SILENT before the tick reaper
	// returns it to pending; defaults to staleClaimAge and is raised by
	// [Runner.RaiseStaleAge] as leaves with longer deadlines are dispatched.
	// The lock is because the raise happens on a leaf's goroutine and the read
	// happens on the dispatch loop's.
	staleMu  sync.Mutex
	staleAge time.Duration
	// holds is this process's grip on every node it currently has a worker
	// behind, keyed by node id. It is what makes "never two workers on one
	// node" a fact rather than a hope: the reaper consults it before taking a
	// claim back, and a claim this process is behind is never taken back at
	// all — it is CANCELLED, and its own worker releases it on the way out.
	// See [Runner.reapSilentClaims] and [leafHold].
	holdMu sync.Mutex
	holds  map[string]*leafHold
	// expand is the depth loop, moved out of the plan build and into the
	// schedule. Nil is the whole rollback: with no hook, a claimed node goes
	// straight to its worker exactly as it did before claim-time division
	// existed.
	expand    ExpandFunc
	drain     chan struct{}
	drainOnce sync.Once
	// wake is the event edge under the poll. Claiming used to be purely timed,
	// so every leaf that came free — a slot returned, a dependent unblocked by
	// the landing that just happened — waited out the rest of the tick before
	// anybody looked. Three jobs cost three of those gaps, and the gaps are
	// visible in the only number that matters: what the person waited. A
	// landing is the one moment the ready set provably changed, so it says so
	// instead of leaving the next pass to find out.
	wake chan struct{}
}

// NewRunner builds a runner executing at most workers nodes concurrently.
func NewRunner(graph *store.Store, execute ExecuteFunc, owner string, workers int) *Runner {
	if workers <= 0 {
		workers = 2
	}
	if strings.TrimSpace(owner) == "" {
		owner = "runner"
	}
	return &Runner{
		graph:               graph,
		execute:             execute,
		owner:               owner,
		slots:               make(chan struct{}, workers),
		activePractice:      make(map[string]context.CancelFunc),
		holds:               make(map[string]*leafHold),
		serviceConsentGrace: ServiceConsentGrace,
		governor:            executor.HostGovernor(),
		staleAge:            staleClaimAge,
		drain:               make(chan struct{}),
		wake:                make(chan struct{}, 1),
	}
}

// nudge asks the dispatch loop to look again now. It is a one-slot signal, so a
// burst of landings costs one extra pass rather than one per landing — the
// difference between an event edge and the busy loop the load governor exists
// to prevent.
func (r *Runner) nudge() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Drain stops the dispatch loop without cancelling the work already in flight,
// and Serve returns nil once the running leaves have landed.
//
// A handover is why it exists. The process giving up the resident role must
// stop claiming new leaves the instant it lets go of the lease, or two runners
// race for the same queue; but it must not take its running leaves down with
// it, because the store owns their claims and a leaf killed mid-turn is work
// paid for and thrown away. Cancelling the context does both at once, which is
// exactly the thing that must not happen here.
func (r *Runner) Drain() {
	r.drainOnce.Do(func() { close(r.drain) })
}

// WithGovernor replaces the shared host gate. Production uses the process-wide
// one so every runner in this process reads the same machine.
func (r *Runner) WithGovernor(governor *executor.Governor) *Runner {
	r.governor = governor
	return r
}

// WithServiceConsentGrace is primarily a deterministic test seam; production
// uses the named bounded default above.
func (r *Runner) WithServiceConsentGrace(grace time.Duration) *Runner {
	if grace > 0 {
		r.serviceConsentGrace = grace
	}
	return r
}

// WithCraftRunner installs the craft sentinel. Nil (the default) leaves every
// leaf ordinary; craft provenance is what selects a node into it, so a runner
// with one installed behaves identically on work that is not a craft run.
func (r *Runner) WithCraftRunner(craft *CraftRunner) *Runner {
	r.craft = craft
	return r
}

// WithExpand installs the claim-time division. Nil (the default) is today's
// behaviour: every claimed node goes to a worker whole.
func (r *Runner) WithExpand(expand ExpandFunc) *Runner {
	r.expand = expand
	return r
}

// FreeSlots reports how many dispatch slots are unheld right now. The
// claim-time expander consults it before multiplying nodes: with every slot
// held, decomposition buys no parallelism and pays pure cost, which is the
// split-waste this field exists to refuse.
func (r *Runner) FreeSlots() int {
	return cap(r.slots) - len(r.slots)
}

// WithStaleAge sets how long a claim may sit SILENT before the tick reaper
// returns the node to pending. Callers that know their longest possible leaf
// deadline set it just above it; the default (staleClaimAge) covers jobs with
// no deadline to compare against.
func (r *Runner) WithStaleAge(age time.Duration) *Runner {
	r.staleAge = age
	return r
}

// ── one node, one worker ────────────────────────────────────────────────────
//
// A RELEASE THAT DOES NOT STOP THE WORKER DOES NOT FREE THE NODE, IT DOUBLES IT.
// Taking a claim back is a change to a row; the goroutine that held it is not
// party to the transaction and does not notice. On the ink run of 2026-08-29 the
// reaper released `task-2` at 07:30:13 and a fresh worker claimed it 17
// milliseconds later, while the first one went on making model calls and editing
// the same checkout for another eleven minutes — the store shows both streams
// interleaved under one node, turns 1-25 of the new attempt flushed between
// turns 45 and 67 of the old one. Two workers, one workspace, each undoing the
// other's edits.
//
// So the claim is not what gets taken. The WORKER gets cancelled — every leaf
// runs on a context this process can end, and the bare loop and its tool calls
// already honour it — and the node stays exactly as it is, Running and
// unclaimable, until that worker's own landing releases it. The release
// therefore happens after the goroutine has provably returned, which is the only
// moment at which the node is genuinely held by nobody.
//
// The backstop under the backstop is [claimReaperPad]. A worker that ignores its
// cancellation for that long is not unwinding, it is gone, and the claim is
// taken back without it — journaled as exactly that, because "released after its
// worker stopped" and "released over a worker that never answered" are different
// facts and only one of them is safe.

// leafHold is this process's grip on one dispatched node: how to stop its
// worker, what that worker is currently waiting on, whether the reaper has asked
// it to stop, and when it asked.
type leafHold struct {
	// token is the claim this worker holds. A hold is only ever matched against
	// the token it was created for, so a stale sweep can never cancel the
	// worker that came after the one it was looking at.
	token  uint64
	cancel context.CancelFunc

	// inFlight is how many calls this worker is waiting on right now, and
	// lastSeen is when one of them last began or ended, in Unix nanoseconds.
	//
	// THE MARK THE JOURNAL CANNOT CARRY. Every durable sign of life is written
	// when something FINISHES — a usage row when a call is billed, a transcript
	// flush when a batch of sixty-four entries fills — so a leaf spending its
	// window inside three long shell commands writes nothing at all and reads as
	// a corpse. The fact that was missing is that a call is IN FLIGHT, and it is
	// known here, for free, by the process that is waiting. See exec.Working.
	//
	// Atomics rather than the mutex below because a batch of tools running in
	// parallel opens several spans at once, on the worker's own goroutines,
	// while the sweep reads them on the dispatch loop's.
	inFlight atomic.Int64
	lastSeen atomic.Int64

	mu      sync.Mutex
	reason  string
	askedAt time.Time
}

// working is the [executor.LivenessMark] this hold hands its worker: a call is
// starting, and the returned function says it is over.
func (hold *leafHold) working() func() {
	hold.inFlight.Add(1)
	hold.mark()
	var once sync.Once
	return func() {
		once.Do(func() {
			hold.inFlight.Add(-1)
			hold.mark()
		})
	}
}

func (hold *leafHold) mark() { hold.lastSeen.Store(time.Now().UnixNano()) }

// busy reports that this worker is demonstrably at work: it is waiting on
// something right now, or it finished waiting on something inside the window.
//
// A worker that has never marked anything is not busy, and that is the honest
// answer rather than a generous one: a leaf that made no call and issued no tool
// has done nothing this process can vouch for, and the journal is then the only
// account of it — which is exactly the account the sweep already read.
func (hold *leafHold) busy(now time.Time, window time.Duration) bool {
	if hold.inFlight.Load() > 0 {
		return true
	}
	seen := hold.lastSeen.Load()
	return seen > 0 && now.Sub(time.Unix(0, seen)) < window
}

// reap asks this worker to stop and answers whether this is the first ask. The
// context is cancelled once; a repeat is the sweep coming round again while the
// worker unwinds, and it must not restart the clock the backstop measures.
func (hold *leafHold) reap(reason string) bool {
	hold.mu.Lock()
	first := hold.reason == ""
	if first {
		hold.reason, hold.askedAt = reason, time.Now()
	}
	hold.mu.Unlock()
	if first && hold.cancel != nil {
		hold.cancel()
	}
	return first
}

// stopping reports whether the reaper has asked this worker to stop, and why.
// A worker asks it of itself at its landing: an answer of true means its claim
// is no longer its to settle.
func (hold *leafHold) stopping() (string, bool) {
	if hold == nil {
		return "", false
	}
	hold.mu.Lock()
	defer hold.mu.Unlock()
	return hold.reason, hold.reason != ""
}

// unheeded reports that this worker was asked to stop and has not, for longer
// than a leaf told to land is given to land.
func (hold *leafHold) unheeded(now time.Time) bool {
	hold.mu.Lock()
	defer hold.mu.Unlock()
	return hold.reason != "" && now.Sub(hold.askedAt) >= claimReaperPad
}

// takeHold registers this process's grip on a node it has just claimed.
func (r *Runner) takeHold(nodeID string, token uint64, cancel context.CancelFunc) *leafHold {
	hold := &leafHold{token: token, cancel: cancel}
	r.holdMu.Lock()
	defer r.holdMu.Unlock()
	r.holds[nodeID] = hold
	return hold
}

// dropHold forgets a grip once its worker has returned. It compares identity
// rather than node id so a landing can never delete the hold of the worker that
// replaced it.
func (r *Runner) dropHold(nodeID string, hold *leafHold) {
	r.holdMu.Lock()
	defer r.holdMu.Unlock()
	if r.holds[nodeID] == hold {
		delete(r.holds, nodeID)
	}
}

// heldLeaf is the grip this process has on one claim, or nil when the worker
// behind that claim is not this process's — a claim owned by a resident that
// died, or one whose goroutine has already returned and is settling.
func (r *Runner) heldLeaf(nodeID string, token uint64) *leafHold {
	r.holdMu.Lock()
	defer r.holdMu.Unlock()
	hold := r.holds[nodeID]
	if hold == nil || hold.token != token {
		return nil
	}
	return hold
}

// CloseOut stops a job's outstanding work so that what has landed can be
// judged, and answers how many nodes it stopped.
//
// A GATE ALWAYS PRECEDES THE WALL. A job whose growth has just been refused
// with the wall nearer than one measured round has had this run's last decision
// about it taken; what it must not do next is keep claiming leaves the clock
// will kill mid-flight, because a job still moving when the clock stops never
// settles, no verdict on it is ever cut, and the person is handed a partial with
// nothing judged. ofetch v4-flash s13 ended exactly there: two refusals inside
// the last ninety seconds, thirteen pending leaves, a pending job root, and not
// one delivery judgement in 5407 seconds.
//
// The job root itself is never touched. It is the node that carries the whole
// remainder and it is where the gate fires, so what this does is clear the way
// to it: every part of the job that has not started is retired, every part this
// process is behind is asked to stop and cancelled, and the root then has
// nothing left to wait for. A part claimed by a worker in another process is
// asked and left — the claim reaper is what finishes those, and taking a claim
// from a live worker is the one thing this may not do.
//
// keep is the node whose landing is asking. Stopping it would release the very
// claim it is in the middle of settling, and the row would go back on the queue
// to be claimed again — a close-out that reopens the job it closed.
func (r *Runner) CloseOut(jobRoot, keep, reason string) int {
	jobRoot = strings.TrimSpace(jobRoot)
	if r == nil || r.graph == nil || jobRoot == "" {
		return 0
	}
	parts, err := r.graph.SubtreeNodes(jobRoot)
	if err != nil {
		_ = guard.Note("resident/runner close-out "+jobRoot, err)
		return 0
	}
	stopped := 0
	for _, part := range parts {
		if part.ID == jobRoot || part.ID == keep {
			continue
		}
		switch part.Status {
		case store.Pending:
			// Retired outright rather than merely marked: a node the ready set
			// still counts as unsettled keeps the job root waiting forever, and
			// asking a pending row to cancel only takes it out of the queue.
			if err := r.graph.CancelPendingWithPartial(part.ID, reason, ""); err == nil {
				stopped++
			}
		case store.Claimed, store.Running:
			// The order is the record's own: the cancellation is requested
			// first, so that the worker's landing reads it and retires the row
			// instead of releasing it back into the queue, and only then is the
			// worker asked to stop.
			if err := r.graph.RequestNodeCancel(part.ID, reason); err != nil {
				continue
			}
			if hold := r.heldLeaf(part.ID, part.ClaimToken); hold != nil {
				hold.reap(reason)
			}
			stopped++
		}
	}
	if stopped > 0 {
		// The ready set provably changed — the root may have just become
		// claimable — and waiting out the rest of the tick to notice is wall
		// this job does not have.
		r.nudge()
	}
	return stopped
}

// reapSilentClaims is the backstop pass: every claim showing no sign of life is
// either stopped, waited for, or taken back from a process that is gone. It
// answers whether anything reopened the ready set.
func (r *Runner) reapSilentClaims() bool {
	silent, err := r.graph.SilentClaims(r.staleWindow())
	if err != nil || len(silent) == 0 {
		return false
	}
	window := r.staleWindow()
	now := time.Now()
	freed := false
	for _, claim := range silent {
		if hold := r.heldLeaf(claim.ID, claim.Token); hold != nil {
			// THE JOURNAL IS NOT THE ONLY WITNESS WHEN THE WORKER IS OURS. The
			// sweep above read what has been written down, and what gets written
			// down is what has FINISHED; a leaf waiting on one long command has
			// finished nothing and written nothing. This process is the one
			// doing the waiting, so it can simply say so.
			if _, stopping := hold.stopping(); !stopping && hold.busy(now, window) {
				continue
			}
			// Ours, and still running. Ask it to stop and leave the claim
			// exactly where it is: its own landing releases it, which is the
			// only release that cannot overtake a live worker. The one
			// exception is a worker that has ignored the ask for longer than a
			// leaf is given to land, which is a goroutine nobody is going to
			// hear from again.
			if hold.reap(claim.Reason); !hold.unheeded(now) {
				continue
			}
			claim.Reason = claim.Reason +
				" — and it did not stop when it was asked, so the claim was taken without it"
		}
		if err := r.graph.ReleaseWithReason(claim.Claim(), claim.Reason); err != nil {
			continue
		}
		if claim.CancelRequested {
			_ = r.graph.CancelPending(claim.ID, store.UserCancelReason)
		}
		freed = true
	}
	return freed
}

// RaiseStaleAge lifts the reaper's window so it stays above a leaf whose own
// deadline has just been decided, and never lowers it.
//
// It exists because the window is a CONSTANT and the deadline is not. A leaf's
// deadline scales with the budget it was granted (cmd/aforge's leafDeadline: a
// minute per fifty thousand tokens above the floor), so a well-fed leaf is
// entitled to run for longer than the reaper's default window — and the reaper
// would then take a node away from a worker that was still working, which is the
// one thing a backstop must never do. The surface that decides a deadline is the
// only party that knows it, so it says so here.
//
// MONOTONIC, AND ON PURPOSE. Several leaves of different sizes run at once and
// the sweep is one query over the whole store, so the window has to clear the
// widest deadline in flight rather than the newest. It is never lowered again:
// the cost of a window left wide is a dead claim noticed later, and the cost of
// one narrowed under a live leaf is the leaf.
//
// It is called from the leaf-building goroutine and read by the dispatch loop,
// which is why it is guarded.
func (r *Runner) RaiseStaleAge(deadline time.Duration) {
	if deadline <= 0 {
		return
	}
	window := deadline + claimReaperPad
	r.staleMu.Lock()
	defer r.staleMu.Unlock()
	if window > r.staleAge {
		r.staleAge = window
	}
}

// staleWindow is the reaper's window as it stands now.
func (r *Runner) staleWindow() time.Duration {
	r.staleMu.Lock()
	defer r.staleMu.Unlock()
	return r.staleAge
}

// WithDailyBudgetUSD installs the policy rail checked immediately before each
// claim. Zero is unlimited and preserves the old scheduling path.
func (r *Runner) WithDailyBudgetUSD(amount float64) *Runner {
	r.dailyBudgetUSD = amount
	return r
}

// runnerTickFailures is how many consecutive failed passes end the dispatch
// loop, and it is the same number the reconciler's own loop uses because it is
// the same lesson learned twice.
//
// A pass fails for two very different reasons: something transient — a store
// busy behind the reconciler's own heavy queries, one racing writer — or
// something structural, a store that can no longer be read at all. Returning on
// the first error treated them as the same thing, and the transient one is
// overwhelmingly the common one. The consequence is the worst failure this
// component has: the loop returned, the process lived on holding the resident
// lease, and no leaf was ever claimed again — silently, forever. It was caught
// in the field as a compiled task sitting pending with an empty started_at for
// fifteen minutes while the reconciler beside it ticked happily once a second.
// Nothing was wrong with the node, nothing was wrong with the queue, and there
// was nobody left to look at either.
//
// Counting consecutive failures separates the two without anyone having to
// enumerate a store's error strings: a store that is genuinely gone fails every
// pass, and a transient fault does not survive the next one.
const runnerTickFailures = 10

// runnerQuietCeiling is the longest the dispatch loop may skip on an unmoved
// journal. Every claimable leaf is journaled, so the watermark is a complete
// account of the ready set — but not of the clock, and a few of the claim's own
// gates are clock-driven: the daily rail rolls over at midnight, the practice
// lane waits out an idle threshold. This is the standing guarantee for those,
// so no clock-driven readiness can ever be more than one ceiling late.
const runnerQuietCeiling = 15 * time.Second

// ── the claim reaper's window, and why it is a backstop ─────────────────────
//
// IT IS NOT THE DETECTOR, AND ON 2026-08-28 IT WAS ACTING AS ONE. A provider
// call hung; nothing in the call path noticed, because the guard that watches a
// stream for silence was armed only when somebody was watching the stream
// (internal/provider's CompleteWithMessages, and the law stated there). So the
// first thing in the whole system to react was this reaper, twenty minutes
// later, whose only available reaction is the bluntest one there is: take the
// node away from the worker holding it and let somebody claim it again. It did
// that three times to one leaf and the run produced nothing.
//
// The detector belongs where the failure is: the guard cuts a silent call in
// ninety seconds, the ledger routes around the endpoint that went quiet, and the
// worker's own loop asks the CALL again with everything it had still in hand. By
// the time a claim is old enough to interest this reaper, every one of those has
// been tried and the claim is genuinely held by nobody — which is the only case
// a reaper can be right about.
//
// So the window is sized to be OUT OF THE WAY rather than to be timely. It is
// the leaf executor's own deadline plus a landing pad, and it is RAISED by
// [Runner.RaiseStaleAge] whenever a surface grants a leaf a longer one, because
// a reaper that fires below a worker's own deadline is not a backstop — it is
// the thing that fires first.
//
// AND ON 2026-08-29 THAT WAS STILL NOT ENOUGH, because a window is only as good
// as what it is measured against. The window below came out at twenty-two
// minutes — fifteen for the executor's own deadline, two for the watchdog above
// it, five for the pad — and the reaper measured it from the claim's start. So
// it fired on the ink run at 22m10s, 22m10s, 22m11s and 22m06s, four times, at a
// leaf that was calling the model throughout: one claim legitimately carries the
// executor's deadline AND the retry a spent deadline earns, which is twice this
// window, and the clock could not tell that from a corpse. The window is now
// measured from the node's last SIGN OF LIFE (store.ReleaseSilent), so it bounds
// SILENCE rather than work, and a leaf that keeps calling keeps its claim for as
// long as it keeps calling.
const (
	// claimReaperPad is how far above a worker's own deadline the reaper sits:
	// the room a leaf told to land needs to write its result and release its
	// claim. Five minutes, which is the landing reserve a leaf at its deadline
	// is already given, doubled — a claim released a minute late costs nothing,
	// and a claim reaped a minute early costs the whole leaf.
	claimReaperPad = 5 * time.Minute
)

// staleClaimAge is the window as a fresh runner starts with it. It bounds
// SILENCE, not work: see [Runner.Tick] and store.ReleaseSilent.
//
// The floor it is measured from is the generalist's own, ASKED FOR RATHER THAN
// REPEATED. It used to be a fifteen-minute constant beside this one, and the
// arithmetic that produces a leaf's deadline lived in three other files as
// well; a floor raised in one of them and not here would put this backstop
// below the deadline it exists to sit above. See exec.SubharnessInfo.Deadline,
// which is now the only place in the process that knows the shape.
var staleClaimAge = executor.SubharnessFor(executor.LinearSubharness).Deadline(0) + claimReaperPad

// runnerQuietGate is the dispatch loop's proof that a timed pass would find
// nothing. A negative seq means it is disarmed and the next pass runs.
type runnerQuietGate struct {
	seq   int64
	until time.Time
}

func newRunnerQuietGate() runnerQuietGate { return runnerQuietGate{seq: -1} }

// skip reports that this pass can be dropped whole. A watermark that cannot be
// read is not an argument for sleeping, so it wakes the loop instead.
func (gate runnerQuietGate) skip(watermark int64, watermarkErr error, now time.Time) bool {
	return watermarkErr == nil && gate.seq >= 0 && watermark == gate.seq && now.Before(gate.until)
}

// settle records what the pass that just ran leaves behind. Only a pass that
// dispatched nothing may arm the gate: one that dispatched has freed no slot
// yet and its own landing is the next thing that will move the journal.
func (gate *runnerQuietGate) settle(watermark int64, watermarkErr error, dispatched int, now time.Time) {
	if dispatched > 0 || watermarkErr != nil {
		gate.disarm()
		return
	}
	gate.seq, gate.until = watermark, now.Add(runnerQuietCeiling)
}

// disarm is what a nudge does: a splice or a landing is news the watermark
// this gate is holding cannot possibly account for.
func (gate *runnerQuietGate) disarm() { gate.seq = -1 }

// runnerFaultStep is the backoff a *repeated* fault earns, and runnerFaultCap
// is where doubling stops. The first fault earns nothing at all.
const (
	runnerFaultStep = time.Second
	runnerFaultCap  = runnerQuietCeiling
)

// runnerFaultBackoff is the anti-spin rail for a dispatch pass that panics.
//
// A recovered panic used to be indistinguishable from a pass that honestly
// found nothing: tickGuarded returned (0, nil), so the quiet gate armed on the
// watermark and the loop then slept a full ceiling — fifteen seconds of silence
// bought by a fault, on a board that may have had ready work the whole time.
// A fault is the opposite of proof that a timed pass would find nothing.
//
// So a fault never arms the quiet gate, and the first one costs no delay: the
// next tick, 500ms later, reads the same durable graph and tries again. Only a
// fault that repeats — the shape that could spin — is made to wait, and it
// waits longer each time up to the same ceiling the quiet gate uses.
type runnerFaultBackoff struct {
	consecutive int
	until       time.Time
}

func newRunnerFaultBackoff() runnerFaultBackoff { return runnerFaultBackoff{} }

// hold reports that this tick is inside the backoff a repeated fault bought.
func (backoff runnerFaultBackoff) hold(now time.Time) bool {
	return backoff.consecutive > 1 && now.Before(backoff.until)
}

// wait is the delay the nth consecutive fault earns: none for the first, then
// one second doubling per repeat, capped.
func runnerFaultWait(consecutive int) time.Duration {
	if consecutive < 2 {
		return 0
	}
	wait := runnerFaultStep
	for step := 2; step < consecutive && wait < runnerFaultCap; step++ {
		wait *= 2
	}
	if wait > runnerFaultCap {
		wait = runnerFaultCap
	}
	return wait
}

// fault records a panicking pass and returns how long the loop now waits.
func (backoff *runnerFaultBackoff) fault(now time.Time) time.Duration {
	backoff.consecutive++
	wait := runnerFaultWait(backoff.consecutive)
	backoff.until = now.Add(wait)
	return wait
}

// clear is what any pass that did not fault does to the rail.
func (backoff *runnerFaultBackoff) clear() {
	backoff.consecutive, backoff.until = 0, time.Time{}
}

// settleTimedPass records one timed pass against both rails, and it is where
// the difference between the two is kept: the quiet gate may only sleep on a
// pass that finished and found nothing, and a fault is neither.
func settleTimedPass(
	gate *runnerQuietGate, backoff *runnerFaultBackoff,
	watermark int64, watermarkErr error, dispatched int, faulted bool, now time.Time,
) {
	if faulted {
		gate.disarm()
		backoff.fault(now)
		return
	}
	backoff.clear()
	gate.settle(watermark, watermarkErr, dispatched, now)
}

// Serve polls for ready work until ctx ends, then waits for in-flight nodes
// to land. Landing is bounded by each execution's own respect for ctx.
//
// A pass is not free: it asks the store for deferred overruns, for the ready
// set, and for whether the user is idle, three times a second even on a machine
// with nothing to do. So a pass that dispatched nothing records the journal
// watermark it started from, and the ticker skips while that watermark stands
// still — the same proof the head already sleeps on. The watermark is read
// before the pass and only kept afterwards: anything journaled while the pass
// was reading sits above the recorded mark, so the next tick looks again rather
// than sleeping through it. Nothing about latency changes — nudge() still fires
// the instant a landing opens the ready set, and it never consults the gate.
func (r *Runner) Serve(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	failures := 0
	pass := func() (int, bool, error) {
		dispatched, faulted, err := r.tickGuarded(ctx)
		if err != nil {
			// Cancellation is the caller's decision, not a fault, and it is the
			// one error that must end the loop on its first appearance.
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return dispatched, faulted, err
			}
			failures++
			_ = guard.Note("resident/runner tick", err)
			if failures >= runnerTickFailures {
				return dispatched, faulted, fmt.Errorf("runner serve: %d consecutive failed passes: %w", failures, err)
			}
			return dispatched, faulted, nil
		}
		failures = 0
		return dispatched, faulted, nil
	}
	gate := newRunnerQuietGate()
	faults := newRunnerFaultBackoff()
	timed := func() error {
		if faults.hold(time.Now()) {
			return nil
		}
		watermark, watermarkErr := r.graph.LatestEventSeq()
		if gate.skip(watermark, watermarkErr, time.Now()) {
			return nil
		}
		dispatched, faulted, err := pass()
		if err != nil {
			return err
		}
		settleTimedPass(&gate, &faults, watermark, watermarkErr, dispatched, faulted, time.Now())
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return ctx.Err()
		case <-r.drain:
			r.wg.Wait()
			return nil
		case <-r.wake:
			gate.disarm()
			// A nudge is news and runs even inside a fault backoff, but what it
			// finds still counts: the rail exists to bound a repeating fault
			// wherever the pass was entered from.
			_, faulted, err := pass()
			if err != nil {
				r.wg.Wait()
				return err
			}
			if faulted {
				faults.fault(time.Now())
			} else {
				faults.clear()
			}
		case <-ticker.C:
			if err := timed(); err != nil {
				r.wg.Wait()
				return err
			}
		}
	}
}

// tickGuarded keeps the drain loop alive across a panicking pass. A fault in
// one tick is recorded and dropped; the next tick reads the same durable graph
// and dispatches whatever is still ready.
//
// It reports the fault rather than swallowing it whole. "Dispatched nothing"
// and "could not finish looking" are different facts, and the caller's quiet
// gate is entitled to sleep only on the first. The report covers the faults
// dispatchOne already recovers on its own as well as one that reaches here:
// both end the pass with nothing dispatched and neither is evidence about the
// ready set.
func (r *Runner) tickGuarded(ctx context.Context) (dispatched int, faulted bool, err error) {
	before := r.passFaults.Load()
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("resident/runner tick", recovered)
			dispatched, faulted, err = 0, true, nil
		}
	}()
	dispatched, err = r.Tick(ctx)
	return dispatched, r.passFaults.Load() != before, err
}

// Tick claims as many ready nodes as free slots allow and dispatches them.
// It returns how many nodes were dispatched; store errors stop the runner,
// execution errors do not — they land on the node as a recorded failure.
func (r *Runner) Tick(ctx context.Context) (int, error) {
	if err := r.preemptPracticeForUserWork(); err != nil {
		return 0, err
	}
	// The silent-claim reaper. A worker that dies mid-run — a process killed, a
	// goroutine wedged past every deadline it was given — leaves its node
	// running forever, and every downstream node gated on it waits behind a
	// claim nobody holds. The runner heartbeats the whole time and finds nothing
	// to do: that is the live-lock a long-horizon run dies of.
	//
	// A CLAIM IS HELD BY EVIDENCE OF LIFE, NOT BY A CLOCK. What the window is
	// measured against is the node's last durable sign that somebody is working
	// it — a billed model call, a recorded turn — and never how long the claim
	// has existed, which is a fact about the wall and not about the worker. See
	// store.ReleaseSilent for the four restarts the old reading cost.
	if r.reapSilentClaims() {
		// A released node reopens the ready set, so the pass that freed it
		// should look again immediately rather than at the tick.
		defer r.nudge()
	}
	dispatched := 0
	// Both readings a claim attempt needs, derived at most once for the whole
	// pass and lazily, because the overwhelmingly common pass claims nothing at
	// all and must pay for neither.
	var pass passReads
	for {
		select {
		case r.slots <- struct{}{}:
		default:
			return dispatched, nil
		}
		spawned, err := r.dispatchOne(ctx, &pass)
		if err != nil {
			return dispatched, err
		}
		if !spawned {
			return dispatched, nil
		}
		dispatched++
	}
}

// passReads holds what one dispatch pass derives once and every claim attempt
// in it re-reads. Both entries are whole-journal or whole-graph questions whose
// answers a claim cannot move, and both used to be asked again for every slot
// the pass filled — so a pass that handed out eight leaves asked them eight
// times and got the same answer eight times.
//
// Nil means "not asked yet", which is how the reading stays lazy: a pass that
// claims nothing performs neither.
type passReads struct {
	// open is the set of nodes that still have unfinished children.
	open map[string]bool
	// railRaised is whether a durable repair is waiting to be admitted, which
	// stops the pass claiming anything at all.
	railRaised *bool
}

// dispatchOne owns the slot the caller just took: every path that does not
// hand it to a worker gives it back, including the fault path. A panic between
// taking a slot and spawning would otherwise starve the runner one worker at a
// time, which is exactly the kind of slow death a crash at least announces.
func (r *Runner) dispatchOne(ctx context.Context, pass *passReads) (spawned bool, err error) {
	held := true
	release := func() {
		if held {
			held = false
			<-r.slots
		}
	}
	// The claim is taken here and handed to the worker at the end. A fault in
	// between owns both: the slot goes back, and the node goes back to pending
	// where the next tick can claim it cleanly.
	var claimed store.Claim
	defer func() {
		if recovered := recover(); recovered != nil {
			release()
			if claimed.ID != "" {
				_ = r.graph.Release(claimed)
			}
			_ = guard.Note("resident/runner dispatch", recovered)
			r.passFaults.Add(1)
			spawned, err = false, nil
		}
	}()

	// The backstop, and nothing else. How many leaves may exist at once is a
	// resource question — handles, goroutines — not a scheduling one, because
	// every leaf is a goroutine parked on a socket waiting for a model.
	// Refusing here is therefore allowed to end the pass: at the backstop no
	// leaf could be admitted, so there is nothing to skip to.
	if !r.governor.Admit(len(r.slots) - 1) {
		release()
		return false, nil
	}
	node, ok, claimErr := r.claimNext(pass)
	if claimErr != nil {
		release()
		return false, claimErr
	}
	if !ok {
		release()
		return false, nil
	}
	claimed = store.Claim{ID: node.ID, Owner: node.Owner, Token: node.ClaimToken}

	// The last question asked of a node before it becomes work: is this really
	// one worker's job? It is asked here and not at plan time because the claim
	// is the proof that everything feeding this node has landed — the division,
	// if there is one, is decided against results rather than against titles.
	//
	// A division ends the pass. Its parts are in the graph and ready, the node
	// is structure and the claim is back, and the nudge below brings the next
	// pass round immediately rather than at the tick — so what the person waits
	// is a scheduling gap and not a poll interval.
	if r.expand != nil {
		if _, expanded := r.expand(ctx, node); expanded {
			claimed = store.Claim{} // the division handed the claim back itself
			release()
			r.nudge()
			return false, nil
		}
	}
	// Everything above this line can still hand the claim back; nothing above it
	// has taken a grip. Below it the grip exists and the goroutine owns it.
	// EVERY LEAF RUNS ON A CONTEXT THIS PROCESS CAN END. It used to be only
	// practice leaves, because user-stopped practice was the only reason anybody
	// had to end one — and so the claim reaper, which is the other reason, had
	// no way to end a worker at all and took the claim out from under it
	// instead. See the leafHold block above for what that cost.
	runCtx, cancel := context.WithCancel(ctx)
	hold := r.takeHold(node.ID, node.ClaimToken, cancel)
	// And the worker's own account of what it is waiting on, which is the half
	// of "is anybody behind this claim" that the journal structurally cannot
	// answer. See leafHold.busy.
	runCtx = executor.WithLiveness(runCtx, hold.working)
	if node.Group == store.PracticeGroup {
		// Deferred because a fault under this lock would otherwise leave it
		// held forever — trading a crash for a deadlock is not a rescue.
		func() {
			r.activeMu.Lock()
			defer r.activeMu.Unlock()
			r.activePractice[node.ID] = cancel
		}()
	}
	r.wg.Add(1)
	held = false            // the worker's own defer returns the slot now
	claimed = store.Claim{} // and the worker's own landing settles the claim
	go func(node store.Node, runCtx context.Context, cancel context.CancelFunc, hold *leafHold) {
		// runOne settles the node on its own fault; this is the outer belt, for
		// a fault in the settling itself. Registered first so it absorbs last.
		defer guard.Recover("resident/runner worker " + node.ID)
		defer r.wg.Done()
		// Registered before the slot goes back so it fires after it: a pass
		// woken while this worker still held its slot would find the queue
		// exactly as full as it was.
		defer r.nudge()
		defer func() { <-r.slots }()
		defer cancel()
		// Dropped after the landing and before the slot goes back, so the pass
		// woken by the returned slot can never find a grip on a worker that is
		// no longer there.
		defer r.dropHold(node.ID, hold)
		if node.Group == store.PracticeGroup {
			defer func() {
				r.activeMu.Lock()
				defer r.activeMu.Unlock()
				delete(r.activePractice, node.ID)
			}()
		}
		r.runOne(runCtx, node, hold)
	}(node, runCtx, cancel, hold)
	return true, nil
}

// Wait blocks until every dispatched node has landed. Tests use it to make
// Tick deterministic.
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) claimNext(pass *passReads) (store.Node, bool, error) {
	// A raised rail must let the reconciler admit every durable repair before a
	// former consumer can race ahead using only the partial result.
	//
	// The question is asked of the whole journal — every deferral that no resume
	// event answers, which is a correlated json_extract of one events scan
	// against another — and it was asked again for every slot the pass filled.
	// Nothing a claim does defers an overrun, for the same reason nothing a
	// claim does gives a node children, so one reading serves the pass.
	if pass.railRaised == nil {
		deferred, err := r.graph.PendingOverruns(1)
		if err != nil {
			return store.Node{}, false, fmt.Errorf("list deferred overruns: %w", err)
		}
		raised := len(deferred) > 0
		pass.railRaised = &raised
	}
	if *pass.railRaised {
		return store.Node{}, false, nil
	}
	ready, err := r.graph.Ready(0)
	if err != nil {
		return store.Node{}, false, fmt.Errorf("list ready nodes: %w", err)
	}
	if len(ready) == 0 {
		return store.Node{}, false, nil
	}
	idle, err := r.graph.UserIdle(time.Now(), 0)
	if err != nil {
		return store.Node{}, false, fmt.Errorf("check user work: %w", err)
	}
	userInFlight := !idle
	sort.SliceStable(ready, func(i, j int) bool {
		return runnerPriority(ready[i]) < runnerPriority(ready[j])
	})
	if pass.open == nil {
		derived, err := openChildren(r.graph)
		if err != nil {
			return store.Node{}, false, err
		}
		pass.open = derived
	}
	for _, node := range ready {
		// Yield to user work only for BACKGROUND self work (practice, or
		// sessionless self splices). A self-origin node carrying a session is
		// the user's own job continuing — the resident spliced its synthesis
		// stages — and deferring it deadlocked the graph: the user job could
		// never finish because its own children were classified as background.
		background := node.Group == store.PracticeGroup ||
			(node.Provenance.Origin != store.OriginUser && node.Provenance.SessionID == "")
		if userInFlight && background && node.Provenance.Origin != store.OriginUser {
			continue
		}
		// A goal node lands after its children: it may be ready by its edges
		// while its subtree is still working, and the store would refuse its
		// completion anyway. Skip it until the children are terminal.
		if pass.open[node.ID] {
			continue
		}
		// Admission reserves the per-firing budget; nothing until now spent it.
		// A firing admitted at $0.50 could run six leaves and journal $3, and
		// the only thing that ever noticed was the next day's admission
		// arithmetic. The rail belongs where every other one already is — in
		// front of the claim — because that is the last moment at which not
		// starting a leaf is free.
		if node.Group == store.PracticeGroup {
			overspent, err := r.practiceFiringOverspent(node)
			if err != nil {
				return store.Node{}, false, err
			}
			if overspent {
				// Stopping the firing means landing it, not wedging it: a
				// pending leaf nobody will ever claim would keep its root open
				// forever. Cancelling one node per pass is deliberate — each is
				// journaled with its own reason, and the root settles as soon as
				// the last child is terminal. A refused cancel is a race with
				// another writer, not a reason to take the whole runner down.
				if err := r.graph.CancelPending(node.ID, practiceBudgetStop); err != nil {
					_ = guard.Note("resident/runner practice rail "+node.ID, err)
				}
				continue
			}
		}
		if r.dailyBudgetUSD > 0 {
			var rail store.DailyRail
			if node.Group == store.PracticeGroup || node.Provenance.SessionID == "" {
				rail, err = r.graph.DailyRailToday(r.dailyBudgetUSD)
			} else {
				rail, _, err = r.graph.PauseDailyRail(r.dailyBudgetUSD, node.Provenance.SessionID)
			}
			if err != nil {
				return store.Node{}, false, err
			}
			if rail.Reached {
				return store.Node{}, false, nil
			}
		}
		// The same rail scoped to one task, and off until somebody sets a
		// ceiling: with none journaled anywhere this is a single probe of an
		// empty table and the claim proceeds exactly as it did before task
		// ceilings existed. Reaching one skips this node rather than the whole
		// pass — the day is shared, a subtree is not, so everything outside the
		// stopped task is still claimable.
		taskRail, _, err := r.graph.PauseTaskRail(node.ID, node.Provenance.SessionID, 0)
		if err != nil {
			return store.Node{}, false, err
		}
		if taskRail.Reached {
			continue
		}
		claim, ok, err := r.graph.Claim(node.ID, r.owner)
		if err != nil {
			return store.Node{}, false, err
		}
		if !ok {
			continue // raced with another runner; both outcomes are fine
		}
		if err := r.graph.Start(claim); err != nil {
			return store.Node{}, false, err
		}
		node.Owner = claim.Owner
		node.ClaimToken = claim.Token
		return node, true, nil
	}
	return store.Node{}, false, nil
}

// executeGuarded turns a panicking execution into an ordinary failed outcome.
// The leaf is the blast radius: it lands failed with the fault as its error,
// the claim settles through the same path any other failure takes, and the
// runner keeps draining.
func (r *Runner) executeGuarded(ctx context.Context, node store.Node) (result ExecResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result, err = ExecResult{}, guard.Note("resident/runner leaf "+node.ID, recovered)
		}
	}()
	return r.execute(ctx, node)
}

// finishCancellation lands the node the user stopped, carrying whatever the
// worker had already written.
//
// The error used to be discarded, and that discard is the whole reason this is
// a named function. Release has already run by the time it is called, so a
// cancellation that does not land leaves the node Pending with cancel_requested
// still set — a row Ready refuses to offer and Claim refuses to take, whose
// parent then waits on it forever. Nothing anywhere noticed, because nothing
// anywhere was told.
//
// One retry, because the realistic failure is a momentary collision with
// another writer and the second attempt costs nothing. A cancellation that
// still will not land is journaled as a fault so it is visible, and the startup
// sweep finishes the parked row — see finishParkedCancellations.
func (r *Runner) finishCancellation(node store.Node, partial string) {
	err := r.graph.CancelPendingWithPartial(node.ID, store.UserCancelReason, partial)
	if err == nil {
		return
	}
	if retry := r.graph.CancelPendingWithPartial(node.ID, store.UserCancelReason, partial); retry != nil {
		_ = guard.Note("resident/runner cancel "+node.ID, retry)
	}
}

func (r *Runner) runOne(ctx context.Context, node store.Node, hold *leafHold) {
	claim := store.Claim{ID: node.ID, Owner: node.Owner, Token: node.ClaimToken}
	// Settling beats stranding. A fault in the landing steps below — service
	// promotion, the craft sentinel, the completion itself — must not leave a
	// claimed node no later tick will ever pick up.
	defer func() {
		if recovered := recover(); recovered != nil {
			fault := guard.Note("resident/runner landing "+node.ID, recovered)
			_ = r.graph.Fail(claim, fault.Error())
			r.noteFault(node)
		}
	}()
	result, err := r.executeGuarded(ctx, node)
	control, controlErr := r.graph.Control(node.ID)
	if controlErr == nil && (control.CancelRequested || control.Held) {
		stopServiceRequests(result.ServiceRequests)
		// Spend precedes settlement even on a user-directed boundary. Release is
		// the CAS transition that invalidates this worker's authority; a cancel
		// then uses the ordinary pending cancellation event.
		r.recordSpend(node, result)
		if releaseErr := r.graph.Release(claim); releaseErr != nil {
			return
		}
		if control.CancelRequested {
			r.finishCancellation(node, result.Summary)
		}
		return
	}
	// A WORKER THAT WAS ASKED TO STOP DOES NOT SETTLE ITS NODE, IT RELEASES IT —
	// and this is the release the reaper deliberately did not make, held back
	// until the goroutine it was about actually returned. Whatever the worker
	// reached is on its record and in the workspace, and the next claim resumes
	// from both; what it must not do is report a verdict, because it was stopped
	// mid-thought and its verdict would be one. The stop is journaled first so
	// the record's order is the truth's: leaf_stopped for this token, then the
	// release that frees it.
	if reason, stopping := hold.stopping(); stopping {
		stopServiceRequests(result.ServiceRequests)
		r.recordSpend(node, result)
		if stopErr := r.graph.RecordLeafStopped(node.ID, store.LeafStopped{
			Token: claim.Token, Reason: reason,
		}); stopErr != nil {
			_ = guard.Note("resident/runner stop "+node.ID, stopErr)
		}
		_ = r.graph.ReleaseWithReason(claim, reason)
		return
	}
	if err != nil {
		stopServiceRequests(result.ServiceRequests)
		// A failed leaf spent exactly as much as a successful one, and often
		// more: escalation runs the work twice before it gives up. The daily
		// rail is summed from this table and nowhere else, so an unrecorded
		// failure is money the rail cannot see and the user is never told
		// about — which is how a day of failures reads as a day of $0.00.
		// Recorded before the settlement for the same reason the success path
		// records before Complete: the spend is true whatever the store then
		// decides about the node.
		r.recordSpend(node, result)
		// The runner's own context ending is not this leaf's verdict on itself.
		// It means the process that was carrying it is going away — a window
		// closed, a role handed over, a one-shot's wall reached — and the leaf
		// died mid-POST because of that and nothing else. Journaling it as a
		// failure is a lie the rest of the machine then believes: the parent
		// replans around a child that never actually failed, the receipt bills
		// a fault nobody incurred, and the retrospect reads
		// `Post ".../chat/completions": context canceled` as a flaky provider
		// and files a lesson recommending retries — for a network that was
		// answering perfectly.
		//
		// Release is the same settlement ReleaseOrphans gives a leaf that was
		// still claimed when a process was killed outright: the row goes back
		// to pending, the workspace it had written stays where it is, and the
		// next resident picks it up. A leaf's OWN deadline runs on a context
		// derived inside the executor, so a genuine timeout leaves ctx.Err()
		// nil here and is still a failure, exactly as before.
		if ctx.Err() != nil {
			_ = r.graph.Release(claim)
			return
		}
		// AN ENDING THAT IS EXHAUSTION IS NOT A FAILURE, AND A FAILED NODE IS
		// NEVER OFFERED AGAIN. Store.Ready serves pending rows only, so failing
		// a node here is the end of it: the ink run of 2026-08-29 journaled
		// "the node goes back on the queue", settled the node failed in the same
		// second, and left with exit 1 and no gate verdict over seventy-three
		// minutes of unspent wall and a twenty-six kilobyte patch on disk.
		//
		// The rule itself is [executor.Requeued] — the one this runner and the
		// one-shot scheduler both ask, so the two surfaces cannot answer
		// differently about what a spent clock means. What is local here is only
		// what the record IS: this side reads the transcript bank, and the next
		// claim resumes from the same bank (cmd/aforge reads it at claim time
		// through resident.Bank when node.Attempt is above zero).
		if allowed, recorded, requeue := executor.Requeued(err, func() int {
			_, turns := BankedRun(r.graph, node.ID)
			return turns
		}); requeue {
			// The turn count rides on the release itself and not only inside its
			// sentence, because the headless stream has to tell a requeue that
			// is PROGRESS from a claim taken off a worker that never answered,
			// and reading that out of prose would be a second, private answer to
			// a question the record already holds. See store.ReleaseWithRecord.
			_ = r.graph.ReleaseWithRecord(claim, exhaustedClaimReason(allowed, recorded), recorded)
			return
		}
		_ = r.graph.Fail(claim, err.Error())
		if guard.IsFault(err) {
			r.noteFault(node)
		}
		return
	}
	result.Summary = r.applyServiceRequests(ctx, node, result.Summary, result.ServiceRequests)
	// The summary is settled on before anything reads it. The sentinel reads
	// this result now and the sweep reads the completed node's summary later,
	// and those two have to be the same words: a substitution made after the
	// sentinel had already looked is how a fan-out reports "no items" live and
	// then unrolls a phantom one on the next tick.
	summary := result.Summary
	if strings.TrimSpace(summary) == "" {
		summary = "finished with no summary"
	}
	// The craft sentinel reads this result before the node closes. Splicing
	// while the landed leaf is still open keeps its job root open too, so no
	// consumer can start against a plan that is one splice out of date. A
	// failure here costs the splice, never the result: the resident's sweep
	// re-derives the same move from the store on its next tick. What this leaf
	// spent rides along because it is not journaled yet — the money gate is
	// deciding whether to open work on the strength of the very landing that
	// paid for it.
	if r.craft != nil {
		_, _ = r.craft.Settle(node, summary, result.Cost)
	}
	// Spend is recorded before completion settles: a refused completion is
	// still money spent, and the journal should say so.
	r.recordSpend(node, result)
	// AND A LEAF THAT RAN OUT IS NEVER COMPLETED HERE. It reached this arm with
	// err == nil because running out of room is not an error — the executor
	// grants a landing reserve and the leaf lands — but landing is not
	// finishing, and the summary it carries is its own last sentence. So the
	// ending goes back through the rule the error arm already applies: the node
	// returns to pending with its banked turns on the release, and the next
	// claim carries on from them. See ExecResult.RanOut, and the sibling law in
	// revision.coverageRefused — a measured fact is not overturned by a claim.
	//
	// IT IS BOUNDED BY THE SAME THREE EVERY OTHER GROWTH IS. The attempt column
	// is incremented at the claim, so a node that has run out on three claims
	// with nothing able to continue it has said all it is going to say, and it
	// is failed with the ending named rather than handed round again for ever.
	if result.RanOut() {
		_, recorded := BankedRun(r.graph, node.ID)
		if node.Attempt < MaxOverrunRounds && recorded > 0 {
			_ = r.graph.ReleaseWithRecord(claim, outOfRoomClaimReason(result, recorded), recorded)
			return
		}
		_ = r.graph.Fail(claim, outOfRoomFailure(result))
		return
	}
	var settleErr error
	if result.Promote && node.Group == ReflexGroup {
		_, settleErr = r.graph.CompleteAndRequestFollowup(claim, summary, store.Command{
			SessionID: node.Provenance.SessionID,
			Kind:      store.CommandSplice, Target: node.ID,
			Instruction: node.Provenance.Intent,
			Attachments: append([]string(nil), node.Provenance.Attachments...),
		})
	} else {
		settleErr = r.graph.Complete(claim, summary)
	}
	if settleErr != nil {
		// A refused completion (a child opened underneath us, a lost claim)
		// must not strand the node mid-flight; release returns it to pending
		// where a later tick can pick it up cleanly.
		_ = r.graph.Release(claim)
	}
}

// practiceBudgetStop is the reason journaled onto every leaf a firing does not
// get to run. It names the bound rather than the accident, because the node's
// own record is the only place anyone will later look to ask why the practice
// job is short.
const practiceBudgetStop = "practice firing reached its per-firing budget"

// practiceFiringOverspent asks whether this firing has already journaled more
// than the charter reserved for it. It reads the whole firing rather than the
// leaf, because the budget is the firing's: six cheap leaves overrun a bound
// no single one of them comes near.
//
// An unbounded or uncharterable node is never refused. A missing charter, a
// zero rail and an unresolvable root all mean the same thing here — no bound is
// in force — and inventing one from a default would stop work nobody agreed to
// stop.
func (r *Runner) practiceFiringOverspent(node store.Node) (bool, error) {
	charterID := strings.TrimSpace(node.Provenance.CharterID)
	if charterID == "" {
		return false, nil
	}
	charter, found, err := r.graph.Charter(charterID)
	if err != nil || !found {
		return false, err
	}
	budget := charter.Rails().PerFiringBudgetUSD
	if budget <= 0 {
		return false, nil
	}
	root, err := r.firingRoot(node)
	if err != nil || root == "" {
		return false, err
	}
	impact, err := r.graph.Impact(root, time.Now())
	if err != nil {
		return false, err
	}
	return impact.Cost >= budget, nil
}

// firingRoot walks a practice leaf back to the job the firing admitted. The
// walk stops at the spine because that is what "one firing" means in the store:
// fireCharter splices one subtree whose root hangs directly off it.
func (r *Runner) firingRoot(node store.Node) (string, error) {
	current := node
	for depth := 0; depth < maxFiringDepth; depth++ {
		if current.Parent == "" || current.Parent == store.RootID {
			return current.ID, nil
		}
		parent, found, err := r.graph.Node(current.Parent)
		if err != nil || !found {
			return "", err
		}
		current = parent
	}
	return "", nil
}

// maxFiringDepth bounds the walk above. A cycle cannot occur through parent
// links the store enforces, so this is a belt against a corrupted view rather
// than an expected depth.
const maxFiringDepth = 32

// recordSpend journals what one execution cost, on every way out of runOne.
// It is one function rather than three call sites because the three endings —
// settled, refused, failed — differ in what happens to the node and not at all
// in what was paid, and the one that was missing is the one that pays most.
// A zero row is skipped: nothing was spent, and an empty row would only make
// the journal longer.
func (r *Runner) recordSpend(node store.Node, result ExecResult) {
	spent := result.PromptTokens > 0 || result.CompletionTokens > 0 || result.Cost > 0
	if !spent && !result.SpendBanked {
		return
	}
	// The three totals are what is LEFT to write: a run that banked its own
	// calls put them on disk one row per response, as each was billed, and
	// summing them again here would charge the node twice for one leaf. What
	// arrives non-zero on a banked run is spend the adapter could not see —
	// a worker that drives another process — and it is journaled as it always
	// was. The rows already on disk are the better record either way, because
	// they survive an ending this function never sees.
	if spent {
		_ = r.graph.RecordUsage(store.NodeUsage{
			NodeID:           node.ID,
			PromptTokens:     result.PromptTokens,
			CompletionTokens: result.CompletionTokens,
			CachedTokens:     result.CachedTokens,
			Cost:             result.Cost,
			Model:            result.Model,
		})
	}
	// The same spend with its shape kept, in its own table, beside the summed
	// row every existing reader counts. It is written after the total and it is
	// allowed to fail on its own: shape is evidence, and losing the evidence
	// must never cost the money.
	_ = r.graph.RecordTurnUsage(node.ID, result.Model, turnLedger(result.Turns))
}

// turnLedger carries the executor's per-turn rows across the seam into the
// journal's own shape. It is a translation and nothing else — a field added at
// one end and forgotten here is a column that silently reads zero, which is
// exactly how cached tokens went unrecorded for a wave.
func turnLedger(turns []executor.TurnUsage) []store.TurnUsage {
	if len(turns) == 0 {
		return nil
	}
	rows := make([]store.TurnUsage, 0, len(turns))
	for _, turn := range turns {
		rows = append(rows, store.TurnUsage{
			Turn:             turn.Turn,
			PromptTokens:     turn.PromptTokens,
			CompletionTokens: turn.CompletionTokens,
			CachedTokens:     turn.CachedTokens,
			SentTokens:       turn.Sent,
			Cost:             turn.Cost,
		})
	}
	return rows
}

// noteFault journals the one quiet line a fault earns in the thread. It is
// best-effort: a fault is already being recorded to the log, and failing to
// say so must not raise a second one.
//
// It stays a THREAD line and did not move to the record with the status
// producers around it (13.18, the noise sweep of 2026-08-11). A fault is a
// failure, and failure is one of the three classes: work the person is waiting
// for has stopped, nothing else in the product is going to say so, and a
// failure filed where only a reader who goes looking can find it is the silence
// this whole law exists to distinguish itself from.
func (r *Runner) noteFault(node store.Node) {
	if r.graph == nil {
		return
	}
	_, _ = thread.Post(r.graph, store.Message{
		SessionID: node.Provenance.SessionID,
		Role:      store.RoleSystem,
		NodeID:    node.ID,
		Body:      faultNotice,
	})
}

func stopServiceRequests(requests []executor.ServiceRequest) {
	for index := range requests {
		requests[index].Stop()
	}
}

func (r *Runner) applyServiceRequests(ctx context.Context, node store.Node, summary string, requests []executor.ServiceRequest) string {
	for index := range requests {
		request := &requests[index]
		keep, autoRestart := node.Provenance.ServiceIntent, false
		if !keep {
			keep, autoRestart = r.awaitServiceConsent(ctx, node, request)
		}
		if !keep {
			request.Stop()
			summary = appendServiceReceipt(summary, request.Name+" stopped at task end")
			continue
		}
		service := store.Service{
			ID:   fmt.Sprintf("service-%d-%d", node.CreatedSeq, request.JobID),
			Name: request.Name, Command: request.Command, Dir: request.Dir,
			Health: request.Health, LogPath: request.LogPath, PID: request.PID,
			StartedAt: request.StartedAt, Status: store.ServiceRunning,
			AutoRestart: autoRestart,
			Provenance:  store.ServiceProvenance{OriginJobID: request.JobID, LeafNodeID: node.ID},
		}
		if _, err := r.graph.PromoteService(service); err != nil {
			request.Stop()
			summary = appendServiceReceipt(summary, fmt.Sprintf("%s stopped at task end — %v", request.Name, err))
			continue
		}
		request.Adopt()
		receipt := fmt.Sprintf("%s keeps running", request.Name)
		if suffix := request.Health.Suffix(); suffix != "" {
			receipt += " · " + suffix
		}
		receipt += fmt.Sprintf(" — say 'stop the %s' to end it", request.Name)
		summary = appendServiceReceipt(summary, receipt)
	}
	return summary
}

func appendServiceReceipt(summary, receipt string) string {
	if strings.TrimSpace(summary) == "" {
		return receipt
	}
	return strings.TrimSpace(summary) + "\n\n" + receipt
}

func (r *Runner) awaitServiceConsent(ctx context.Context, node store.Node, request *executor.ServiceRequest) (bool, bool) {
	if strings.TrimSpace(node.Provenance.SessionID) == "" {
		return false, false
	}
	allowFree := true
	options := []store.QuestionOption{
		{Label: "keep it running", Value: "service:keep:" + request.Name, Hint: "say ‘keep with auto-restart’ to opt in"},
		{Label: "stop at task end", Value: "service:stop:" + request.Name},
	}
	// Categorized like every other durable ask so the meta loop can measure how
	// often the stop default is accepted — but ShouldAsk never gates it away:
	// keeping a process alive past its task is consent-bearing.
	prompt := store.QuestionMessageBody("Keep "+request.Name+" running after this task?", options,
		store.QuestionConfig{Kind: store.QuestionConfirm, Category: store.QuestionCategoryServiceConsent,
			Default: "2", AllowFree: &allowFree})
	question, err := r.graph.AskQuestion(store.AgentQuestion{
		SessionID: node.Provenance.SessionID, Text: prompt, OriginNodeID: node.ID,
		Urgency: store.QuestionBlocking, Category: store.QuestionCategoryServiceConsent,
		DefaultAnswer: "2", Options: options,
		ExpiresAt: time.Now().Add(r.serviceConsentGrace),
	})
	if err != nil {
		return false, false
	}
	if _, err := r.graph.SurfaceQuestion(question.Seq); err != nil {
		return false, false
	}
	deadline := time.Now().Add(r.serviceConsentGrace)
	for time.Now().Before(deadline) {
		current, found, readErr := r.graph.AgentQuestionBySeq(question.Seq)
		if readErr == nil && found && current.Status == store.QuestionAnswered {
			answer := strings.ToLower(strings.TrimSpace(current.Resolution))
			keep := strings.Contains(answer, "keep") && !strings.Contains(answer, "stop")
			auto := keep && strings.Contains(answer, "auto")
			return keep, auto
		}
		select {
		case <-ctx.Done():
			_ = r.graph.ResolveQuestion(question.Seq, store.QuestionExpired, "service promotion cancelled; default stop")
			return false, false
		case <-time.After(serviceConsentPoll):
		}
	}
	_ = r.graph.ResolveQuestion(question.Seq, store.QuestionExpired, "service promotion grace elapsed; default stop")
	return false, false
}

func (r *Runner) preemptPracticeForUserWork() error {
	idle, err := r.graph.UserIdle(time.Now(), 0)
	if err != nil {
		return fmt.Errorf("check practice preemption: %w", err)
	}
	if idle {
		return nil
	}
	r.activeMu.Lock()
	defer r.activeMu.Unlock()
	for _, cancel := range r.activePractice {
		cancel()
	}
	return nil
}

func runnerPriority(node store.Node) int {
	switch {
	case node.Provenance.Origin == store.OriginUser:
		return 0
	case node.Provenance.Origin == store.OriginTrigger:
		return 1
	case node.Group == store.PracticeGroup:
		return 3
	default:
		return 2
	}
}

// openChildren maps each node id that has at least one non-terminal child.
func openChildren(graph *store.Store) (map[string]bool, error) {
	nodes, err := graph.ActiveNodes()
	if err != nil {
		return nil, fmt.Errorf("list active nodes: %w", err)
	}
	open := make(map[string]bool)
	for _, node := range nodes {
		if node.Parent == "" {
			continue
		}
		switch node.Status {
		case store.Done, store.Failed, store.Cancelled:
		default:
			open[node.Parent] = true
		}
	}
	return open, nil
}

// exhaustedClaimReason is what the journal says when a claim goes back because
// the worker holding it ran out of room.
//
// It names both facts a reader needs and neither of them is machinery: how long
// the worker was given, and how much of its work survived — because "the node
// goes back on the queue" is only worth reading if the next claim is going to
// pick up where this one stopped, and the turn count is the evidence that it
// can. The headless stream prints it beside the node (see cmd/aforge's
// narrateOne on store.EventNodeReleased).
// outOfRoomClaimReason is what a person reads when a leaf that ran out goes back
// on the queue: what ran out, how far it got, and that its work is kept.
func outOfRoomClaimReason(result ExecResult, recorded int) string {
	reason := "it was still working when it ran out"
	if words := result.Meter.Words(); words != "" {
		reason += " (" + words + ")"
	}
	return reason + " — " + pluralTurns(recorded) + " of its work is recorded, and the next attempt carries on from there"
}

// outOfRoomFailure is the same fact when there is no next attempt left: the leaf
// ran out on every claim it was given and nothing could continue it, so the node
// says so rather than settling on a summary written by a worker that was cut off
// mid-sentence.
func outOfRoomFailure(result ExecResult) string {
	words := result.Meter.Words()
	if words == "" {
		words = string(result.Stop)
	}
	return "it ran out of room on every attempt and was never able to finish (" + words + ")"
}

func exhaustedClaimReason(allowed time.Duration, recorded int) string {
	return fmt.Sprintf("the worker did not come back within %s and was stopped — %s of its work is recorded, and the next one carries on from there",
		allowed.Round(time.Second), pluralTurns(recorded))
}

// pluralTurns spells a turn count in a person's words.
func pluralTurns(turns int) string {
	if turns == 1 {
		return "1 turn"
	}
	return fmt.Sprintf("%d turns", turns)
}
