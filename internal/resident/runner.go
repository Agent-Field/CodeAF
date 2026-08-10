package resident

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	executor "github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/store"
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
	Cost             float64
	// Promote asks the runner to settle this reflex partial and enqueue the
	// same verbatim instruction on the ordinary compiled path atomically.
	Promote         bool
	ServiceRequests []executor.ServiceRequest
	// Model is who actually served the work — the rung a panel picked or an
	// escalation moved to, which is not the model anyone asked for. It rides
	// the spend row because that is the row a receipt already reads.
	Model string
}

// ExecuteFunc runs one claimed node to completion. The runner owns the claim
// lifecycle around it; the function owns nothing but the work.
type ExecuteFunc func(ctx context.Context, node store.Node) (ExecResult, error)

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
	craft               *CraftRunner
	drain               chan struct{}
	drainOnce           sync.Once
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
		serviceConsentGrace: ServiceConsentGrace,
		governor:            executor.HostGovernor(),
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
	pass := func() (int, error) {
		dispatched, err := r.tickGuarded(ctx)
		if err != nil {
			// Cancellation is the caller's decision, not a fault, and it is the
			// one error that must end the loop on its first appearance.
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return dispatched, err
			}
			failures++
			_ = guard.Note("resident/runner tick", err)
			if failures >= runnerTickFailures {
				return dispatched, fmt.Errorf("runner serve: %d consecutive failed passes: %w", failures, err)
			}
			return dispatched, nil
		}
		failures = 0
		return dispatched, nil
	}
	gate := newRunnerQuietGate()
	timed := func() error {
		watermark, watermarkErr := r.graph.LatestEventSeq()
		if gate.skip(watermark, watermarkErr, time.Now()) {
			return nil
		}
		dispatched, err := pass()
		if err != nil {
			return err
		}
		gate.settle(watermark, watermarkErr, dispatched, time.Now())
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
			if _, err := pass(); err != nil {
				r.wg.Wait()
				return err
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
func (r *Runner) tickGuarded(ctx context.Context) (dispatched int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("resident/runner tick", recovered)
			dispatched, err = 0, nil
		}
	}()
	return r.Tick(ctx)
}

// Tick claims as many ready nodes as free slots allow and dispatches them.
// It returns how many nodes were dispatched; store errors stop the runner,
// execution errors do not — they land on the node as a recorded failure.
func (r *Runner) Tick(ctx context.Context) (int, error) {
	if err := r.preemptPracticeForUserWork(); err != nil {
		return 0, err
	}
	dispatched := 0
	// The open-children map is a decode of every live node in the graph, and
	// claimNext asked for one on every claim attempt — so a pass that filled
	// four slots read the whole graph four times. Nothing a claim does gives a
	// node children, so one reading serves the whole pass. It is derived lazily
	// because the overwhelmingly common pass claims nothing at all.
	var open map[string]bool
	for {
		select {
		case r.slots <- struct{}{}:
		default:
			return dispatched, nil
		}
		spawned, err := r.dispatchOne(ctx, &open)
		if err != nil {
			return dispatched, err
		}
		if !spawned {
			return dispatched, nil
		}
		dispatched++
	}
}

// dispatchOne owns the slot the caller just took: every path that does not
// hand it to a worker gives it back, including the fault path. A panic between
// taking a slot and spawning would otherwise starve the runner one worker at a
// time, which is exactly the kind of slow death a crash at least announces.
func (r *Runner) dispatchOne(ctx context.Context, open *map[string]bool) (spawned bool, err error) {
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
			spawned, err = false, nil
		}
	}()

	// This is the one gate on new leaves — every claim in the process comes
	// through here. A refusal delays nothing that is already running and
	// touches no user-origin surgery or answer: those never claim. The next
	// tick asks again, so a saturated machine simply admits more slowly.
	if !r.governor.Admit(len(r.slots) - 1) {
		release()
		return false, nil
	}
	node, ok, claimErr := r.claimNext(open)
	if claimErr != nil {
		release()
		return false, claimErr
	}
	if !ok {
		release()
		return false, nil
	}
	claimed = store.Claim{ID: node.ID, Owner: node.Owner, Token: node.ClaimToken}
	runCtx := ctx
	var cancel context.CancelFunc
	if node.Group == store.PracticeGroup {
		runCtx, cancel = context.WithCancel(ctx)
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
	go func(node store.Node, runCtx context.Context, cancel context.CancelFunc) {
		// runOne settles the node on its own fault; this is the outer belt, for
		// a fault in the settling itself. Registered first so it absorbs last.
		defer guard.Recover("resident/runner worker " + node.ID)
		defer r.wg.Done()
		// Registered before the slot goes back so it fires after it: a pass
		// woken while this worker still held its slot would find the queue
		// exactly as full as it was.
		defer r.nudge()
		defer func() { <-r.slots }()
		if cancel != nil {
			defer cancel()
			defer func() {
				r.activeMu.Lock()
				defer r.activeMu.Unlock()
				delete(r.activePractice, node.ID)
			}()
		}
		r.runOne(runCtx, node)
	}(node, runCtx, cancel)
	return true, nil
}

// Wait blocks until every dispatched node has landed. Tests use it to make
// Tick deterministic.
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) claimNext(open *map[string]bool) (store.Node, bool, error) {
	// A raised rail must let the reconciler admit every durable repair before a
	// former consumer can race ahead using only the partial result.
	deferred, err := r.graph.PendingOverruns(1)
	if err != nil {
		return store.Node{}, false, fmt.Errorf("list deferred overruns: %w", err)
	}
	if len(deferred) > 0 {
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
	if *open == nil {
		derived, err := openChildren(r.graph)
		if err != nil {
			return store.Node{}, false, err
		}
		*open = derived
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
		if (*open)[node.ID] {
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

func (r *Runner) runOne(ctx context.Context, node store.Node) {
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
		if node.Group == store.PracticeGroup && errors.Is(ctx.Err(), context.Canceled) {
			_ = r.graph.Release(claim)
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
	if result.PromptTokens == 0 && result.CompletionTokens == 0 && result.Cost == 0 {
		return
	}
	_ = r.graph.RecordUsage(store.NodeUsage{
		NodeID:           node.ID,
		PromptTokens:     result.PromptTokens,
		CompletionTokens: result.CompletionTokens,
		Cost:             result.Cost,
		Model:            result.Model,
	})
}

// noteFault journals the one quiet line a fault earns in the thread. It is
// best-effort: a fault is already being recorded to the log, and failing to
// say so must not raise a second one.
func (r *Runner) noteFault(node store.Node) {
	if r.graph == nil {
		return
	}
	_, _ = r.graph.PostMessage(store.Message{
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
