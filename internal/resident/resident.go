// Package resident reconciles durable thread commands with the active task
// graph and reports graph outcomes back into their originating sessions.
package resident

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/thread"
	"github.com/Agent-Field/aforge-v2/internal/watchdog"
)

const (
	pollInterval     = 500 * time.Millisecond
	commandBatchSize = 50
	eventBatchSize   = 200
)

// ReflexGroup is the durable node marker for the no-compiler, no-planner rung.
// Group is already part of the splice event and rebuilt node view.
const ReflexGroup = "reflex"

const reflexPromotionLine = "this turned out to be a job — doing it properly"

// Compiled is one instruction after assume-and-declare: the goal to act on,
// the defaults that were filled (each a revisable receipt), and the
// compiler's judgement of shape — "lookup", "task", or "project" — which the
// planner may use to decide how much structure the work deserves.
type Compiled struct {
	Goal        string
	Assumptions []string
	Scale       string

	// Title is the job's rail-sized display name, produced by the compile
	// call itself. Empty falls back to the separate naming pass, which is
	// what every caller without a compiler still gets.
	Title string
	// Contract is the task-scale working method, produced by the same compile
	// call. Empty falls back to the separate contract pass.
	Contract string
	// Parts is the compile call's declaration of a bundle: independent
	// requests that share a stage instead of pretending to be a sequence.
	Parts []string
	// TrialOf is the retrieved unsettled fact this goal deliberately tests.
	// Zero means the compiled job is ordinary work.
	TrialOf int64

	// BuildsOn names earlier top-level jobs this one continues. Each becomes
	// a feeds_into edge onto the new subtree's entry nodes, so the prior
	// result arrives as an input digest — continuity through the graph, not
	// through a shared workspace.
	BuildsOn []string

	// Question, when set, means the compiler judged one gap too consequential
	// to guess. Nothing is spliced; the question is asked in the thread and
	// the user's reply arrives as an ordinary next message.
	Question string

	// QuestionOptions makes any compiler askback selectable without removing
	// free text. Charter is an inert standing draft until the reconciler records
	// a separate ratification command.
	QuestionOptions []store.QuestionOption
	Charter         *store.CharterSpec
	ServiceIntent   bool

	// WorkModel is the model the user named for this job in their own words.
	// It rides the splice as provenance, so the leaves that run it are pinned
	// to what was asked for rather than to whatever the slot holds later.
	WorkModel string

	// ModelNote is the one calm receipt line about that choice.
	ModelNote string

	// Subharness is the specialist worker this whole job was judged to be for.
	// It rides the splice as provenance, exactly as WorkModel does, so the
	// leaves that run it are held to what was chosen when the ask was read
	// rather than to whatever the process has registered an hour later.
	Subharness string
}

// SkillCandidate names the artifact directory a job proved useful. It remains
// a belief until recurrence and an executable self-test promote it.
type SkillCandidate struct {
	Artifact string
}

// Learned is one distilled memory: what it is about, what kind, one line.
// Replaces names one existing fact this line supersedes. Quarantines names
// suspect inputs that consolidation removes from retrieval without replacing;
// both transitions remain reversible journal events.
type Learned struct {
	Scope string
	Kind  store.FactKind
	Body  string
	// Unsettled is the structured pair required by FactUnsettled. Body is a
	// searchable projection and is regenerated from this payload on write.
	Unsettled *store.UnsettledPair
	Replaces  int64
	// Sources names the existing facts a consolidated line derives from,
	// strongest evidence first. It is empty outside consolidation.
	Sources []int64
	// Skill is set only when this memory names a reusable artifact produced by
	// the job. Its fact enters the notebook as a non-retrievable candidate.
	Skill *SkillCandidate
	// Craft is set only when the job's SHAPE looked reusable. It is not a
	// memory at all — it rides here because one distiller call judges both,
	// and it is split off before the notebook ever sees it.
	Craft *CraftCandidate
	// Quarantines names source facts rejected by a repeated bad-outcome pattern.
	// It is honored only by consolidation.
	Quarantines []int64
}

// DistillFunc extracts durable memories from one finished or failed job.
// outcome is the summary on success or the error on failure.
type DistillFunc func(ctx context.Context, goal, outcome string, failed bool) ([]Learned, error)

// ScopePair is one cheap taxonomy candidate offered to consolidation. Both
// names have the same gardenable prefix and are still canonical shelves.
type ScopePair struct {
	First  string
	Second string
}

// ScopeAliasJudgment is the consolidator's one taxonomy decision. Canonical
// is empty for keep-separate and one member of the candidate pair for merge.
type ScopeAliasJudgment struct {
	Merge     bool   `json:"merge"`
	Canonical string `json:"canonical"`
}

// Consolidation carries the ordinary line rewrite and, when a candidate was
// offered, the one merge-or-separate taxonomy judgment made in the same call.
type Consolidation struct {
	Facts      []Learned
	ScopeAlias *ScopeAliasJudgment
}

// ConsolidateFunc rewrites at most one scope's accumulated facts into fewer,
// better lines and judges at most one emergent scope pair. Each returned line
// maps itself to its originals through Sources.
type ConsolidateFunc func(ctx context.Context, scope string, facts []store.Fact, candidate *ScopePair) (Consolidation, error)

// CompileFunc turns a verbatim thread instruction into a goal the planner can
// act on. graphContext is a compact rendering of the active graph.
type CompileFunc func(ctx context.Context, instruction string, graphContext string) (Compiled, error)

// PlanFunc turns one compiled goal into an atomic subtree admission.
type PlanFunc func(ctx context.Context, compiled Compiled) (store.Subtree, error)

// PlanAnchor is the durable identity a planner can speak against before the
// subtree itself has landed. CommandSeq is set for a new chat job; replanning
// an existing job carries only its already-admitted node and session.
type PlanAnchor struct {
	NodeID     string
	SessionID  string
	CommandSeq int64
}

type planAnchorKey struct{}

func withPlanAnchor(ctx context.Context, anchor PlanAnchor) context.Context {
	return context.WithValue(ctx, planAnchorKey{}, anchor)
}

// PlanAnchorFromContext returns the journal anchor for planning progress. It is
// optional so PlanFunc remains usable outside the resident command loop.
func PlanAnchorFromContext(ctx context.Context) (PlanAnchor, bool) {
	if ctx == nil {
		return PlanAnchor{}, false
	}
	anchor, ok := ctx.Value(planAnchorKey{}).(PlanAnchor)
	return anchor, ok && strings.TrimSpace(anchor.NodeID) != ""
}

// Reconciler is the replaceable background half of the resident thread. The
// store remains the source of truth; this type keeps injected planning
// behavior and a working copy of two durable cursors in memory.
//
// Those cursors are restart-safe because they are journaled, not because they
// are cheap: the settle lane's place in the event stream and the consolidation
// clock are both written as lane watermarks (store.LaneSettlement,
// store.LaneConsolidation). They used to be plain fields primed on the first
// tick of every process, which meant a job that landed while no reconciler was
// running was never announced, never distilled and never folded, and every
// restart bought another belief-rewriting consolidation pass. Anything else
// held here is a per-tick working set, and losing it costs telemetry only.
type Reconciler struct {
	store           *store.Store
	compile         CompileFunc
	plan            PlanFunc
	narrate         NarrateFunc
	distill         DistillFunc
	consolidate     ConsolidateFunc
	title           TitleFunc
	reflect         ReflectFunc
	digestTerritory TerritoryDigestFunc
	overrunPlan     OverrunPlanFunc
	cancelRethink   CancelRethinkFunc
	redirect        RedirectFunc
	sentinel        SentinelFunc
	composeBrief    BriefComposeFunc
	standingWatch   StandingWatch
	craft           *CraftRunner
	craftMind       *CraftMind
	resolveModel    ModelResolveFunc
	proposeCharters bool
	// oneShotErrand is the headless surface's fact about itself: every command
	// this reconciler will apply is one errand, run once, with nobody who could
	// answer a question about it.
	oneShotErrand bool
	// forcedSubharness overrides every compiler judgement about who runs this
	// job. It exists for measurement: comparing two workers on the same corpus
	// means taking the choice away from the model that would otherwise vary it.
	forcedSubharness string
	// modelsInForce answers, at the moment a job is admitted, which model would
	// structure it and which would work it if the job named neither. It is a
	// question only the surface can answer — the slots live there — and it is
	// asked here because this is where the answer stops being configuration and
	// becomes provenance.
	modelsInForce  func() (plan, work string)
	dailyBudgetUSD float64
	// rooms is the addressing policy, resolved once at construction. Every
	// decision about *which conversation* a piece of news is spoken into reads
	// this one value; nothing below reads the environment again.
	rooms           roomPolicy
	practiceEnabled bool
	practiceBudget  float64
	practiceIdle    time.Duration
	services        *ServiceSupervisor
	heartbeat       func(time.Time)
	handover        HandoverFunc
	residentSince   time.Time

	mu                 sync.Mutex
	watcherInitialized bool
	lastEventSeq       int64
	// settlementMark is the cursor value already written to the journal. It
	// exists so a pass that consumed nothing but its own watermark event does
	// not write another one, which would otherwise make the lane a perpetual
	// writer and defeat the quiet-tick gate.
	settlementMark int64
	progress       map[string]*subtreeProgress
	// commandStrikes counts how many times each pending command has stalled.
	// In memory on purpose: it is a fact about this process's luck with a
	// provider, not about the request, and a restart re-reads the same pending
	// row with a clean slate — which is exactly what a fresh process should do.
	commandStrikes map[int64]int
	// commandWall overrides the package constant. Zero means the constant.
	commandWall       time.Duration
	learningMoments   map[string]*pendingLearningMoment
	lastConsolidation time.Time
	// charterOutcomeSeq is how far the charter ladder has finished reading. It
	// is a working cursor, not a durable one: zero re-reads every unfolded
	// firing, which is what a fresh process should do and what makes losing it
	// cost one pass rather than one outcome.
	charterOutcomeSeq       int64
	lastWatchPass           WatchPass
	standingWatchKeyPersist func() (bool, string, error)
	now                     func() time.Time

	// arrivalSession and arrivalSeq mark where the user's arrival began. The
	// attach edge is journaled after AttachSession has already surfaced
	// questions and said what lapsed, so the brief's window — which ends at that
	// edge — swallowed the arrival's own noise and reported it as news from
	// while the user was away. This is the true boundary, taken before anything
	// is surfaced, and the brief closes its window on it instead.
	arrivalSession string
	arrivalSeq     int64

	// The host repair runs outside mu on purpose, so it keeps its own lock.
	standingMu         sync.Mutex
	standingWatchCheck time.Time

	// craftMu serializes recognition. Independent asks are compiled and planned
	// side by side now, and the shelf they all ask "do we already know how to do
	// this?" is a git-backed index rather than a pure function. Recognition is a
	// local BM25 read and a file load, so holding one lock across it costs
	// nothing measurable and removes the only shared mutable thing on the
	// concurrent splice path.
	craftMu sync.Mutex

	// gate is the change-detection state that lets an idle tick return
	// without re-deriving a graph nothing has touched.
	gatePrimed     bool
	gateEventSeq   int64
	gateDeadline   time.Time
	gateClockLimit time.Time
}

// StandingWatch is the small consequence-facing seam the resident needs.
// watchdog.Manager implements it; tests inject an in-memory recorder.
type StandingWatch interface {
	Install(ctx context.Context) error
	// Uninstall is the reverse gear. A consent the product accepts and cannot
	// give back is not consent, and the timer repairs itself against a manual
	// `launchctl unload` every five minutes, so the only honest off-switch is
	// one the resident itself performs after journalling the decision.
	Uninstall(ctx context.Context) error
	Status() (watchdog.Status, error)
}

// New constructs a reconciler. A nil compiler preserves the instruction
// verbatim with no assumptions. A nil planner admits one task whose stable ID
// is derived from the command sequence.
func New(graph *store.Store, compile CompileFunc, plan PlanFunc) *Reconciler {
	if compile == nil {
		compile = func(_ context.Context, instruction, _ string) (Compiled, error) {
			return Compiled{Goal: instruction}, nil
		}
	}
	return &Reconciler{store: graph, compile: compile, plan: plan, now: time.Now,
		rooms: resolveRoomPolicy(), services: NewServiceSupervisor(graph)}
}

func (r *Reconciler) WithServiceRuntime(runtime ServiceRuntime) *Reconciler {
	if r.services == nil {
		r.services = NewServiceSupervisor(r.store)
	}
	r.services.WithRuntime(runtime)
	return r
}

// WithCraftRunner installs the craft sentinel's resume half. The runner
// advances a craft run as each of its nodes lands; this sweep re-derives the
// same moves from the store alone, which is what makes a run that died between
// a completion and its splice pick up exactly where it stopped.
func (r *Reconciler) WithCraftRunner(craft *CraftRunner) *Reconciler {
	r.craft = craft
	return r
}

// WithOneShotErrands pins this reconciler to the headless errand surface.
//
// The compiler is told the same fact and is the place it should be settled;
// this is the second rung, for the case where a charter draft arrives anyway —
// a compiler that is not the head's, a provider that emitted a charter key the
// prompt never asked for. A draft that reaches a surface with no one at the
// keyboard is auto-resolved exactly as the caller already chose by typing the
// verb: once, not standing. The resolution is journaled, and the work then
// runs, which is the whole point of the errand.
//
// It is also the switch for the surface's other law, which is about words
// rather than time: the submitted ask is the goal, kept byte for byte, and a
// question the compiler wanted to ask is answered here rather than returned.
// See keepTheAskVerbatim and assumeAndDeclare.
func (r *Reconciler) WithOneShotErrands() *Reconciler {
	r.oneShotErrand = true
	return r
}

// WithSubharness forces every job this reconciler admits onto one worker,
// whatever the compiler thought. It is a benchmarking instrument and is named
// as one: a run that is comparing workers cannot let the choice be the variable
// it is trying to measure. Empty restores the ordinary path, where the choice
// is the compiler's and is usually the generalist.
func (r *Reconciler) WithSubharness(subharness string) *Reconciler {
	r.forcedSubharness = strings.TrimSpace(subharness)
	return r
}

// WithModelsInForce teaches the resident the surface's two model slots, read at
// splice time: the one that structures and the one that works. Without it a job
// records nothing about who planned it, which is exactly what every embedding
// path with no slots to speak of should record.
func (r *Reconciler) WithModelsInForce(models func() (plan, work string)) *Reconciler {
	r.modelsInForce = models
	return r
}

// splitModelSlots is the durable answer to "who structured this job and who
// worked it", and it is deliberately silent in the ordinary case. The plan slot
// follows the work slot by default, so recording the same name twice would be a
// fact about nothing — and a surface reading it back would announce a split that
// never happened. The model the user pinned for the work outranks the slot,
// because that is the model this job's leaves will actually run on.
//
// The two names are returned together because they are one fact: a split. A
// build that recorded only the planner taught every surface to say "planned by
// <a model nobody recognizes>" and never who the work went to, which reads as an
// accusation rather than a receipt. Either both names are worth recording or
// neither is.
func (r *Reconciler) splitModelSlots(pinnedWork string) (plan, run string) {
	if r == nil || r.modelsInForce == nil {
		return "", ""
	}
	plan, work := r.modelsInForce()
	plan, work = strings.TrimSpace(plan), strings.TrimSpace(work)
	if pinned := strings.TrimSpace(pinnedWork); pinned != "" {
		work = pinned
	}
	if plan == "" || strings.EqualFold(plan, work) {
		return "", ""
	}
	return plan, work
}

// chosenSubharness is the one place the two sources of the choice meet.
func (r *Reconciler) chosenSubharness(compiled Compiled) string {
	if r != nil && r.forcedSubharness != "" {
		return r.forcedSubharness
	}
	return strings.TrimSpace(compiled.Subharness)
}

// forceWorkerOnSubtree writes the forced worker onto every node as well as onto
// the splice, and it exists because provenance alone is not forcing.
//
// A node inherits the splice's worker only when it named none of its own, so a
// provenance-only force was a force over exactly the nodes nobody had an
// opinion about. That was invisible while the only thing anyone forced was a
// specialist — the sizing pass's own specialist verdicts named the same worker
// — and it is the whole story for the arm that forces the generalist: those
// nodes name a worker, the flag did not reach them, and the run measured the
// specialist it was told not to use.
func (r *Reconciler) forceWorkerOnSubtree(subtree store.Subtree) store.Subtree {
	if r == nil || r.forcedSubharness == "" {
		return subtree
	}
	for index := range subtree.Nodes {
		subtree.Nodes[index].Subharness = r.forcedSubharness
	}
	return subtree
}

// WithStandingWatch enables the one-time unattended-presence offer after the
// first charter ratification. Nil preserves embedding paths with no host timer.
func (r *Reconciler) WithStandingWatch(standing StandingWatch) *Reconciler {
	r.standingWatch = standing
	return r
}

// WithStandingWatchKeyPersist supplies the credential step that runs before a
// watch install: timer-driven wakes see no shell environment, so the key must
// survive on disk for them. Kept as an injected hook so nothing in this
// package ever writes to the real home during tests; nil skips persistence.
func (r *Reconciler) WithStandingWatchKeyPersist(persist func() (bool, string, error)) *Reconciler {
	r.standingWatchKeyPersist = persist
	return r
}

// residentTickFailures is how many consecutive failed passes end the loop.
//
// A pass fails for two very different reasons. Something transient — a provider
// 429 inside a practice plan, a recycled PID the service supervisor cannot
// signal, one sentinel whose model is briefly unreachable — or something
// structural: a store that can no longer be read. Returning on the first error
// treated them as the same thing, and the transient one is overwhelmingly the
// common one. The resident then died in under a millisecond while its process
// lived on holding the lease, so every later `aforge wake` reported it alive and
// no standing watch, charter or practice ever fired again, silently, forever.
//
// Counting consecutive failures separates the two without anyone having to
// enumerate a provider's error strings: a store that is genuinely gone fails
// every pass, and a transient fault does not survive the next one.
const residentTickFailures = 10

// Serve polls until ctx is cancelled or the store can no longer be read or
// written. Strategy failures reject their command and do not stop the loop.
func (r *Reconciler) Serve(ctx context.Context) error {
	failures := 0
	pass := func() error {
		err := r.tickGuarded(ctx)
		if err == nil {
			failures = 0
			r.noteHeartbeat()
			return nil
		}
		// Cancellation is the caller's decision, not a fault, and it is the one
		// error that must end the loop on its first appearance.
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		failures++
		_ = guard.Note("resident/reconciler tick", err)
		if failures >= residentTickFailures {
			return fmt.Errorf("resident serve: %d consecutive failed passes: %w", failures, err)
		}
		return nil
	}
	if err := pass(); err != nil {
		return err
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := pass(); err != nil {
				return err
			}
		}
	}
}

// WithHeartbeat installs the liveness stamp the resident lease reads. Holding
// the role is a claim about doing the work, and the flock alone cannot tell a
// serving process from a wedged one — so the loop says so on every pass it
// completes, and a probe that finds the stamp stale may take the role back.
// Nil (the default) leaves the lease saying nothing, which a probe reads as
// unknown rather than as dead.
func (r *Reconciler) WithHeartbeat(beat func(time.Time)) *Reconciler {
	r.heartbeat = beat
	return r
}

func (r *Reconciler) noteHeartbeat() {
	if r.heartbeat != nil {
		r.heartbeat(r.now())
	}
}

// tickGuarded absorbs a panicking pass. The store is the truth and the lock is
// released by the unwind, so the next tick re-reads the same queue and does the
// work this one dropped; a fault in one command must not end the loop that
// applies every later one.
func (r *Reconciler) tickGuarded(ctx context.Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("resident/reconciler tick", recovered)
			err = nil
		}
	}()
	return r.Tick(ctx)
}

// Tick drains the current command queue and announces newly settled nodes.
// Calls are serialized so tests and embedding processes may invoke Tick
// without racing another Serve loop on the same reconciler.
func (r *Reconciler) Tick(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// The host timer repair shells out to launchctl or systemctl. It is
	// deliberately reconciled before the lock so a wedged daemon cannot stop
	// the resident from ticking at all.
	r.reconcileStandingWatch(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()
	// Learning moments are deliberately NOT reset here. A moment composed while
	// no surface was attached used to be wiped at the top of the next tick,
	// which is every moment the wake path ever produces; they are now held
	// until somebody is there to read them and dropped only on delivery.
	r.lastWatchPass = WatchPass{}

	if r.store == nil {
		return errors.New("resident tick: nil store")
	}
	quiet, err := r.quietTickLocked()
	if err != nil {
		return fmt.Errorf("resident tick: change gate: %w", err)
	}
	if quiet {
		return nil
	}
	if err := r.initializeWatcher(); err != nil {
		return fmt.Errorf("resident tick: initialize watcher: %w", err)
	}
	if err := r.expireQuestionsLocked(); err != nil {
		return fmt.Errorf("resident tick: expire questions: %w", err)
	}
	if err := r.surfaceBlockingQuestionsLocked(""); err != nil {
		return fmt.Errorf("resident tick: surface blocking questions: %w", err)
	}

	for {
		commands, err := r.store.PendingCommands(commandBatchSize)
		if err != nil {
			return fmt.Errorf("resident tick: pending commands: %w", err)
		}
		deferred, err := r.reconcileBatch(ctx, commands)
		if err != nil {
			return fmt.Errorf("resident tick: %w", err)
		}
		// A deferred command is still at the head of the pending queue, so
		// re-reading here would hand it straight back and burn its second strike
		// against the same wedged provider in the same breath. The next tick is
		// the retry; this one is done draining.
		if deferred || len(commands) < commandBatchSize {
			break
		}
	}
	if r.services != nil {
		if err := r.services.Tick(ctx); err != nil {
			return fmt.Errorf("resident tick: services: %w", err)
		}
	}

	if r.overrunPlan != nil {
		if _, err := ResumeDeferredOverruns(ctx, r.store, r.dailyBudgetUSD, r.overrunPlan); err != nil {
			return fmt.Errorf("resident tick: resume deferred overruns: %w", err)
		}
	}
	if r.craft != nil {
		if _, err := r.craft.Sweep(ctx); err != nil {
			return fmt.Errorf("resident tick: advance craft runs: %w", err)
		}
	}
	watchPass, err := r.watchOnceLocked(ctx)
	r.lastWatchPass = watchPass
	if err != nil {
		return fmt.Errorf("resident tick: standing watches: %w", err)
	}
	if err := r.reconcileCharterOutcomes(); err != nil {
		return fmt.Errorf("resident tick: charter outcomes: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.announceTransitions(ctx); err != nil {
		return fmt.Errorf("resident tick: watch graph: %w", err)
	}
	// One reading of the active view serves both passes below. It is a decode of
	// every live node in the graph, and the two of them asked for it separately
	// on every tick; nothing between them admits a node, and the only thing
	// either changes is which settled jobs are folded — which is terminal work
	// the narrator drops from its state either way.
	active, err := r.store.ActiveNodes()
	if err != nil {
		return fmt.Errorf("resident tick: active nodes: %w", err)
	}
	if err := r.foldSettledJobs(active); err != nil {
		return fmt.Errorf("resident tick: fold settled jobs: %w", err)
	}
	if err := r.speakProgress(ctx, active); err != nil {
		return fmt.Errorf("resident tick: narrate progress: %w", err)
	}
	retrospectiveAfter := r.latestEventSeq()
	r.consolidateNotebook(ctx)
	r.reflectOnJobs(ctx)
	r.promoteRecurringSkills(ctx)
	r.settleTasteLocked()
	r.flushLearningMoments()
	r.postRetrospectiveDigest(retrospectiveAfter)
	r.syncSkillBins()
	if err := r.practiceOnceLocked(ctx); err != nil {
		return fmt.Errorf("resident tick: practice loop: %w", err)
	}
	if err := r.primeQuietGateLocked(); err != nil {
		return fmt.Errorf("resident tick: change gate: %w", err)
	}
	return nil
}

// quietTickCeiling is the longest the change gate may hold a tick back. Every
// deadline the gate knows about is named in gateDeadline; this is the standing
// guarantee for the ones it cannot name — a day boundary crossing, an idle
// threshold elapsing — so no clock-driven path can ever run more than one
// ceiling late, however quiet the store gets.
const quietTickCeiling = 30 * time.Second

// quietTickLocked reports whether this tick can be skipped whole: nothing has
// been journaled since the last full pass, nothing is waiting on the clock, and
// the ceiling has not elapsed. Every derivation a tick performs would then read
// exactly the state it read last time and write exactly nothing.
func (r *Reconciler) quietTickLocked() (bool, error) {
	if !r.gatePrimed {
		return false, nil
	}
	// Unspoken progress is a debounce held in memory, not in the journal, so it
	// comes due without anything being written.
	if len(r.progress) > 0 {
		r.gatePrimed = false
		return false, nil
	}
	seq, err := r.store.LatestEventSeq()
	if err != nil {
		return false, err
	}
	now := r.now()
	quiet := seq == r.gateEventSeq && now.Before(r.gateClockLimit) &&
		(r.gateDeadline.IsZero() || now.Before(r.gateDeadline))
	if !quiet {
		// A tick that does real work must re-derive the gate from the state it
		// leaves behind, never from the state it found.
		r.gatePrimed = false
	}
	return quiet, nil
}

// primeQuietGateLocked records what a completed tick leaves behind. The
// deadlines are derived only once the journal has stood still across two
// passes, so an active store pays a single watermark read per tick.
func (r *Reconciler) primeQuietGateLocked() error {
	seq, err := r.store.LatestEventSeq()
	if err != nil {
		return err
	}
	if seq != r.gateEventSeq {
		r.gateEventSeq = seq
		r.gatePrimed = false
		return nil
	}
	deadline, err := r.nextClockDeadlineLocked()
	if err != nil {
		return err
	}
	r.gateDeadline = deadline
	r.gateClockLimit = r.now().Add(quietTickCeiling)
	r.gatePrimed = true
	return nil
}

// nextClockDeadlineLocked is the earliest moment at which the passage of time
// alone gives a tick something to do. A zero time means nothing is waiting.
func (r *Reconciler) nextClockDeadlineLocked() (time.Time, error) {
	now := r.now()
	deadline := time.Time{}
	// A deadline already in the past is a deadline of now: the thing it names is
	// overdue, not perpetual. Clamping here is what keeps one lapsed question —
	// or one supervision interval that elapsed while a long tick ran — from
	// pinning the gate open with a moment that can never be reached again.
	earlier := func(at time.Time) {
		if at.IsZero() {
			return
		}
		if at.Before(now) {
			at = now
		}
		if deadline.IsZero() || at.Before(deadline) {
			deadline = at
		}
	}

	charterDue, _, err := r.store.CharterClockDeadline(now)
	if err != nil {
		return time.Time{}, err
	}
	earlier(charterDue)

	// A supervised process can die without writing anything, so its health
	// check is a clock deadline the journal never announces. It is the
	// supervisor's own interval, not "now": naming now meant that adopting a
	// single service disarmed the gate for as long as that service lived, and
	// the resident then ran its full pass twice a second forever.
	services, err := r.store.ActiveServices()
	if err != nil {
		return time.Time{}, err
	}
	earlier(r.services.NextHealthCheck(services, now))

	// The same window expireQuestionsLocked reads, so the gate cannot miss an
	// expiry the tick itself would have applied.
	questions, err := r.store.UnresolvedQuestions(200)
	if err != nil {
		return time.Time{}, err
	}
	for _, question := range questions {
		earlier(question.ExpiresAt)
	}

	if !r.lastConsolidation.IsZero() {
		earlier(r.lastConsolidation.Add(consolidationInterval))
	} else {
		earlier(now)
	}

	// A job whose grace window has not closed yet is work the passage of time
	// alone gives the next tick. Without this the gate would sleep through the
	// deadline and the job would stay unfolded until something else wrote to
	// the journal — which on a quiet machine can be hours.
	nodes, err := r.store.ActiveNodes()
	if err != nil {
		return time.Time{}, err
	}
	for i := range nodes {
		if foldableSettledJob(&nodes[i]) {
			earlier(nodes[i].FinishedAt.Add(settledFoldGrace))
		}
	}
	return deadline, nil
}

// LastWatchPass returns the standing-watch decisions made by the latest Tick.
// It is an ephemeral operation report for bounded callers such as `aforge
// wake`; all resulting state transitions remain journaled in the store.
func (r *Reconciler) LastWatchPass() WatchPass {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastWatchPass
}

type commandOutcome struct {
	status  store.CommandStatus
	result  string
	receipt string
	// asAgent posts the receipt in the agent's own voice instead of as a
	// collapsed system receipt — a question must be heard, not filed.
	asAgent bool
	// options travels with any agent question as structured payload.
	options       []store.QuestionOption
	category      store.QuestionCategory
	defaultAnswer string
}

// concurrentCommands bounds how many independent asks are planned at once. It
// is small on purpose: this is not a throughput dial, it is the difference
// between one sentence naming three things and three sentences said in a queue.
const concurrentCommands = 4

// independentCommand reports that applying this command shares nothing with any
// other pending command, so it may be applied beside them.
//
// The membrane is deliberately narrow — a fresh, untargeted, non-reflex splice
// and nothing else. That is exactly the fan-out case and exactly the expensive
// one: a splice is three model round-trips (compile, plan, title) before its
// work exists as a single node. Everything else — a redirect, a cancel, a
// charter, a reflex, any splice that names a target — reads or edits something
// another command in the same breath may be writing, and stays in seq order
// where it has always been.
func independentCommand(command store.Command) bool {
	return !command.Reflex && command.Kind == store.CommandSplice &&
		strings.TrimSpace(command.Target) == ""
}

// reconcileBatch applies one drained queue. Runs of independent commands go
// side by side; anything else is a barrier that waits for the run before it and
// is applied alone, so the ordering guarantees the rest of the resident relies
// on are untouched.
//
// This is the half of "the jobs ran one after another" that no amount of worker
// pool would have fixed. The runner was never the thing holding the second job
// back — the second job did not exist yet, because the first one's planner was
// still talking to a model on the only goroutine allowed to make work.
// It reports whether any command in the batch was left pending. A stalled
// command is not a reason to stop applying the ones behind it — that was the
// entire failure this guards against — so the batch runs to the end either way
// and only the drain's re-read is held back.
func (r *Reconciler) reconcileBatch(ctx context.Context, commands []store.Command) (bool, error) {
	deferred := false
	for start := 0; start < len(commands); {
		if !independentCommand(commands[start]) {
			if err := ctx.Err(); err != nil {
				return deferred, err
			}
			left, err := r.reconcileCommand(ctx, commands[start])
			deferred = deferred || left
			if err != nil {
				return deferred, fmt.Errorf("command %d: %w", commands[start].Seq, err)
			}
			start++
			continue
		}
		end := start
		for end < len(commands) && independentCommand(commands[end]) {
			end++
		}
		left, err := r.reconcileGroup(ctx, commands[start:end])
		deferred = deferred || left
		if err != nil {
			return deferred, err
		}
		start = end
	}
	return deferred, nil
}

// reconcileGroup applies a run of independent commands concurrently and then
// settles them in seq order.
//
// The split is the whole safety argument. Application is the model work and the
// splice, and those genuinely do not care about each other. Settlement is the
// resolution, the receipt and any askback — the parts a reader sees, and the
// parts that touch the reconciler's own locked helpers — and those stay
// single-file, in the order the person said them.
func (r *Reconciler) reconcileGroup(ctx context.Context, group []store.Command) (bool, error) {
	if len(group) < 2 {
		if len(group) == 1 {
			left, err := r.reconcileCommand(ctx, group[0])
			if err != nil {
				return left, fmt.Errorf("command %d: %w", group[0].Seq, err)
			}
			return left, nil
		}
		return false, nil
	}
	type applied struct {
		outcome commandOutcome
		err     error
	}
	results := make([]applied, len(group))
	slots := make(chan struct{}, concurrentCommands)
	var wait sync.WaitGroup
	started := 0
	r.thinking(func() {
		for index := range group {
			if err := ctx.Err(); err != nil {
				break
			}
			slots <- struct{}{}
			started++
			wait.Add(1)
			go func(index int) {
				// A fault applying one ask becomes that ask's recorded rejection,
				// exactly as it would have on the serial path, rather than a panic
				// crossing back into a tick that is holding the reconciler's lock.
				defer func() {
					if recovered := recover(); recovered != nil {
						results[index].err = guard.Note("resident/reconciler command", recovered)
					}
				}()
				defer wait.Done()
				defer func() { <-slots }()
				results[index].outcome, results[index].err = r.applyBounded(ctx, group[index])
			}(index)
		}
		wait.Wait()
	})
	// Only what was actually applied is settled. A command the context cut off
	// before it started is still pending, and the next tick owns it — and so is
	// one whose own wall expired, which settleOrStrike leaves for the same tick
	// to collect. Settling every started command in seq order is unchanged: a
	// stall is a settlement that decided not to resolve, not a gap in the order.
	deferred := false
	for index, command := range group[:started] {
		left, err := r.settleOrStrike(ctx, command, results[index].outcome, results[index].err)
		deferred = deferred || left
		if err != nil {
			return deferred, fmt.Errorf("command %d: %w", command.Seq, err)
		}
	}
	if started < len(group) {
		deferred = true
	}
	return deferred, ctx.Err()
}

// reconcileCommand applies one command and settles it, reporting whether the
// command was left pending for a later tick.
func (r *Reconciler) reconcileCommand(ctx context.Context, command store.Command) (bool, error) {
	var outcome commandOutcome
	var err error
	r.thinking(func() { outcome, err = r.applyBounded(ctx, command) })
	return r.settleOrStrike(ctx, command, outcome, err)
}

// thinking runs one stretch of model work with the reconciler's lock released,
// and takes it back before returning.
//
// The lock is a guard over a handful of in-memory cursors — the settle lane's
// place in the event stream, the narrator's unspoken milestones, the change
// gate — and it was never protecting the model call. Holding it across one
// meant every other holder waited out somebody else's planner, and the holder
// that matters is the person: AttachSession and the arrival brief take this
// same lock on the chat-startup path, so opening a chat could sit behind three
// model round-trips of a job that had nothing to do with it.
//
// The rule for what may go inside is the one the concurrent splice path already
// established: application touches the store and nothing this lock guards,
// which is why four of them can run side by side. Settlement — the resolution,
// the receipt, the askback — stays locked and in seq order, because that is the
// half a reader sees and the half that reaches the guarded state.
//
// Every caller holds the lock when it calls, and a fault inside do unwinds
// through the deferred re-acquire, so the tick's own release stays balanced.
func (r *Reconciler) thinking(do func()) {
	r.mu.Unlock()
	defer r.mu.Lock()
	do()
}

// settleCommand records what applying one command decided: the durable
// resolution, and the single visible line the person is owed for it.
func (r *Reconciler) settleCommand(command store.Command, outcome commandOutcome, err error) error {
	if err != nil {
		reason := fmt.Sprintf("%s failed: %v", command.Kind, err)
		outcome = commandOutcome{
			status:  store.CommandRejected,
			result:  reason,
			receipt: "I couldn't apply that request: " + reason,
		}
	}

	if err := r.store.ResolveCommand(command.Seq, outcome.status, outcome.result); err != nil {
		return err
	}
	if outcome.asAgent {
		text := store.QuestionMessageBody(outcome.receipt, outcome.options, store.QuestionConfig{
			Category: outcome.category, Default: outcome.defaultAnswer,
		})
		ask := store.AgentQuestion{
			SessionID: command.SessionID, Text: boundMessage(text),
			OriginCharterID:  questionCharterOrigin(outcome.options),
			OriginCommandSeq: command.Seq, Urgency: store.QuestionBlocking,
			Options: outcome.options, Category: outcome.category,
			DefaultAnswer: outcome.defaultAnswer,
		}
		if outcome.category == store.QuestionCategoryCompileAssumption {
			// A compile askback has no node and no charter behind it, so no
			// origin can ever retire it and it hung forever — outliving the
			// request it was asked about, and outliving the session that could
			// answer it. Its own relevance window is the only thing that can
			// end it, and ending loudly is the point: a request that lapsed is
			// news, a request that vanished is a betrayal.
			ask.ExpiresAt = r.now().Add(compileAskWindow)
		}
		_, err := r.askQuestionLocked(ask)
		return err
	}
	if strings.TrimSpace(outcome.receipt) == "" {
		return nil
	}
	role := receiptVoice(command, outcome.status)
	body := outcome.receipt
	if len(outcome.options) > 0 {
		// Selectable receipts carry the structured payload the TUI's question
		// components read; the durable option rows remain the continuation
		// and validation source.
		body = store.QuestionMessageBody(outcome.receipt, outcome.options)
	}
	message := store.Message{
		SessionID:  command.SessionID,
		Role:       role,
		Body:       boundMessage(body),
		NodeID:     receiptAnchor(command, outcome.status),
		CommandSeq: command.Seq,
		Options:    outcome.options,
	}
	// An anchored receipt goes to the record, and 13.18 is only receiptAnchor's
	// own sentence finally being true. "Applied surgery is progress and belongs
	// on the job's own card" was written believing a node-anchored row lived on
	// that card; it lived on the card AND in the conversation, because every
	// thread read takes the whole session and a node under a row does not
	// remove it. So the anchored half now carries no session: the head said the
	// one thread sentence this command gets, in its own voice, before the
	// command was even journaled (HeadSpeaksFor), and the job's record keeps
	// the receipt for whoever opens the room. A refusal is unanchored by
	// receiptAnchor and still speaks — it is the one case where the head having
	// spoken first is the wrong answer, not a duplicate one.
	if message.NodeID != "" {
		_, err = thread.Record(r.store, message)
		return err
	}
	_, err = thread.Post(r.store, message)
	return err
}

// receiptAnchor decides where a command's receipt is read. Applied surgery is
// progress and belongs on the job's own card; a refusal is news and belongs in
// the thread, where the person who asked is actually looking. The worst case is
// the one that made this necessary: a command rejected because its target no
// longer exists was filed under that missing target, so the refusal rendered
// nowhere at all.
func receiptAnchor(command store.Command, status store.CommandStatus) string {
	if status == store.CommandRejected {
		return ""
	}
	return commandReceiptNode(command)
}

// receiptVoice decides whether a receipt is heard or filed, and it is the other
// half of receiptAnchor's question. An anchored receipt has a card to live on. An
// unanchored one has only the thread, and the thread files a system post as
// collapsed machine furniture — which is exactly what happened to the answer
// "1": the standing-watch receipt was written, journaled, and rendered as a grey
// line nobody reads, so the person who had just answered a question watched
// silence and typed the answer again. Where the conversation already carries an
// answer the receipt stays filed, because the wave-4 rule cuts both ways: one
// user action, exactly one visible response.
//
// A refusal is the one case where the head having spoken is not an answer, it
// is the wrong answer. The head says "cancelling that job" in the same breath
// that it journals the command; when the command is then rejected — the target
// finished a second earlier, the job no longer exists — the head's sentence is
// already on screen and untrue, and filing the correction as grey furniture is
// how the user is left believing something happened. So a rejection speaks,
// whoever else spoke first. It is still one visible line, and it is the only
// one that is true.
func receiptVoice(command store.Command, status store.CommandStatus) store.Role {
	if status == store.CommandRejected {
		return store.RoleAgent
	}
	if receiptAnchor(command, status) != "" || HeadSpeaksFor(command.Kind) {
		return store.RoleSystem
	}
	return store.RoleAgent
}

// HeadSpeaksFor is the audit, written down: every kind here is journaled by a
// route that answers the user in its own voice in the same breath — surgery and
// revision, charters, services, splices — and handover, whose outcome the
// residency narrates while it waits for it. A kind absent from this list is one
// nobody has volunteered to answer for, so its receipt becomes the answer.
// Silence is the failure this list exists to prevent; a kind that grows a spoken
// reply and is not added here says the same thing twice, which is the cheaper
// mistake and the one a reader can see.
//
// It is exported because the head reads the same list from the other end. This
// list is exactly the set of receipts the head has already spoken over, so it is
// exactly the set whose SETTLEMENT the head is on the hook for — the wake that
// says what the workforce actually made of the change (internal/head/wake.go).
func HeadSpeaksFor(kind store.CommandKind) bool {
	switch kind {
	case store.CommandSplice, store.CommandAmend, store.CommandCancel, store.CommandRedirect,
		store.CommandExpedite, store.CommandPause, store.CommandResume,
		store.CommandReprioritize, store.CommandRestart, store.CommandHandover,
		store.CommandServiceStop, store.CommandServiceRestart, store.CommandServiceAutoRestart,
		store.CommandCharterRatify, store.CommandCharterPause, store.CommandCharterRetire,
		store.CommandCharterCadence, store.CommandCharterWording, store.CommandCharterOnce,
		store.CommandCharterFire,
		store.CommandCharterDecline, store.CommandCharterAlways, store.CommandCharterNever,
		store.CommandCharterProbation,
		// The verbs aimed at what has been learned join for the same reason the
		// charter and service verbs did: the head's own craft tool answers in the
		// same breath that it journals one, so a spoken receipt beside it would
		// say the same thing twice. Fired from a page instead, the page is where
		// the person is looking and the filed receipt is the record of it — and a
		// refusal still speaks, whoever else spoke first.
		store.CommandCraftRun, store.CommandCraftRevert, store.CommandCraftRetire,
		store.CommandSkillRetire:
		return true
	default:
		return false
	}
}

func (r *Reconciler) applyCommand(ctx context.Context, command store.Command) (commandOutcome, error) {
	if command.Reflex {
		return r.reflex(command)
	}
	switch command.Kind {
	case store.CommandSplice:
		return r.splice(ctx, command)
	case store.CommandCancel:
		return r.cancel(ctx, command)
	case store.CommandRedirect:
		return r.redirectJob(ctx, command)
	case store.CommandExpedite:
		return r.expediteJob(ctx, command)
	case store.CommandPause:
		return r.pause(command)
	case store.CommandResume:
		return r.resume(command)
	case store.CommandReprioritize:
		return r.reprioritize(command)
	case store.CommandRestart:
		return r.restart(command)
	case store.CommandSetModel:
		return r.setModel(command)
	case store.CommandServiceStop, store.CommandServiceRestart, store.CommandServiceAutoRestart:
		return r.applyServiceCommand(command)
	case store.CommandCraftRun, store.CommandCraftRevert, store.CommandCraftRetire:
		return r.applyCraftCommand(ctx, command)
	case store.CommandSkillRetire:
		return r.applySkillRetire(command)
	case store.CommandCharterRatify, store.CommandCharterPause, store.CommandCharterRetire,
		store.CommandCharterCadence, store.CommandCharterWording, store.CommandCharterOnce,
		store.CommandCharterFire,
		store.CommandCharterDecline, store.CommandCharterAlways, store.CommandCharterNever, store.CommandCharterProbation:
		return r.applyCharterCommand(ctx, command)
	case store.CommandStandingWatchEnable, store.CommandStandingWatchDecline:
		return r.applyStandingWatchCommand(ctx, command)
	case store.CommandAmend:
		return r.amend(command)
	case store.CommandHandover:
		return r.applyHandoverCommand(command)
	default:
		reason := fmt.Sprintf("command kind %q is not supported", command.Kind)
		return commandOutcome{
			status:  store.CommandRejected,
			result:  reason,
			receipt: reason,
		}, nil
	}
}

func (r *Reconciler) reflex(command store.Command) (commandOutcome, error) {
	if command.Kind != store.CommandSplice || strings.TrimSpace(command.Target) != "" {
		return commandOutcome{}, errors.New("reflex must be an untargeted splice")
	}
	id := fmt.Sprintf("reflex-%d", command.Seq)
	subtree := store.Subtree{Nodes: []store.NodeSpec{{
		ID: id, Brief: command.Instruction, Title: clipLabel(firstLine(command.Instruction), 48),
		Stage: 1, Group: ReflexGroup,
	}}}
	provenance := store.Provenance{
		Origin: store.OriginUser, SessionID: command.SessionID, Intent: command.Instruction,
		Attachments: append([]string(nil), command.Attachments...),
	}
	if err := r.store.Splice(store.RootID, subtree, provenance); err != nil {
		node, ok, readErr := r.store.Node(id)
		if readErr != nil || !ok || node.Parent != store.RootID || node.Group != ReflexGroup ||
			node.Provenance.Origin != store.OriginUser ||
			node.Provenance.SessionID != command.SessionID ||
			node.Provenance.Intent != command.Instruction {
			return commandOutcome{}, err
		}
	}
	return commandOutcome{
		status: store.CommandApplied,
		result: "spliced 1 reflex node",
		// The head already acknowledged the action. Skipping a second receipt is
		// part of keeping this rung to one breath.
		receipt: "",
	}, nil
}

func (r *Reconciler) splice(ctx context.Context, command store.Command) (commandOutcome, error) {
	snapshot, err := r.store.ActiveSnapshot()
	if err != nil {
		return commandOutcome{}, fmt.Errorf("read active graph: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return commandOutcome{}, err
	}

	// The ASK and not the brief: everything this assembles is retrieval — the
	// notebook digest, the recall, the cues — and retrieval over an inherited
	// transcript retrieves whatever the room happened to be about rather than
	// what was asked for. Same reason craft recognition reads the ask
	// (craftmind.go), and the same failure when it did not.
	compileContext := r.renderCompileContextFor(snapshot, command.Instruction, command.SessionID)
	compileContext += attachedDocumentCompileContext(command.Attachments)
	if r.oneShotErrand {
		compileContext += oneShotErrandContext
	}
	promotion, promoted, err := r.promotionSource(command)
	if err != nil {
		return commandOutcome{}, err
	}
	if promoted {
		compileContext += "\n\nPromoted reflex (reuse this partial; compile the SAME verbatim ask as normal work):\n" +
			"node: " + promotion.ID + "\nverbatim ask: " + promotion.Provenance.Intent +
			"\npartial result:\n" + clipBlock(promotion.Summary, store.MaxDigestBytes)
	}
	// The first model round-trip of every splice, and until now the longest
	// silence on the card: nothing between the head saying "on it" and the
	// planner's first row said the request was even being read.
	r.noteCommandStage(command, stageReading, "")
	// THE COMPILER IS THE ONE READER ENTITLED TO BOTH HALVES. A forked ask
	// carries the conversation it came out of precisely so planning is
	// well-informed, so this is where it is spent — fenced and labelled as
	// context, which is what [store.Command.Brief] composes. Every other reading
	// of this command below is `command.Instruction`, the ask alone.
	compiled, err := r.compile(ctx, command.Brief(), compileContext)
	if err != nil {
		return commandOutcome{}, fmt.Errorf("compile request: %w", err)
	}
	// On the headless surface the reading is not allowed to become the ask.
	// This runs before the reading is posted so the row a person reads back
	// says the sentence that was actually submitted.
	compiled = r.keepTheAskVerbatim(command, compiled)
	// What the compiler made of the ask, in the compiler's own words. This is
	// the reading, and it is the one piece of the phase a person can check.
	r.noteCommandStage(command, stageReading, firstLine(compiled.Goal))
	compiled.Goal = anchorAttachedDocuments(compiled.Goal, command.Attachments)
	if promoted {
		compiled.BuildsOn = append([]string{promotion.ID}, compiled.BuildsOn...)
	} else if unfinished := r.unfinishedSource(command); unfinished != "" {
		// The head read this ask as arriving beside work still in flight. New
		// work it is, then, but not work that runs alongside: continuity is the
		// difference between a second job that inherits a result and two jobs
		// changing one thing at once, and only the second is unrepairable.
		compiled.BuildsOn = prependBuildsOn(unfinished, compiled.BuildsOn)
	}
	onceNote := ""
	if compiled.Charter != nil {
		if !r.oneShotErrand {
			id := fmt.Sprintf("charter-%d", command.Seq)
			charter, err := r.store.DraftCharter(id, command.SessionID, command.Seq, *compiled.Charter)
			if err != nil {
				return commandOutcome{}, fmt.Errorf("draft charter: %w", err)
			}
			question, options := charterRatificationQuestion(charter, compiled.Charter.Rails.MaxPerDayJustification)
			return commandOutcome{
				status: store.CommandRejected, result: "drafted charter pending ratification",
				receipt: question, asAgent: true, options: options,
				category:      store.QuestionCategoryCharterRatification,
				defaultAnswer: "1",
			}, nil
		}
		compiled, onceNote, err = r.resolveStandingAsOnce(command, compiled)
		if err != nil {
			return commandOutcome{}, err
		}
	}

	// A headless errand answers its own questions, because the alternative is
	// not "ask carefully" but "do nothing". This is the same move the empirical
	// ask policy below makes, taken unconditionally and without the second
	// compile: the goal on this surface is fixed to the submitted ask, so a
	// recompile could only re-open what the surface has already settled.
	if question := strings.TrimSpace(compiled.Question); question != "" && r.oneShotErrand {
		compiled = r.assumeAndDeclare(command, compiled, question)
	}

	if question := strings.TrimSpace(compiled.Question); question != "" {
		defaultAnswer := defaultQuestionAnswer(compiled.QuestionOptions)
		ask, _, gateErr := r.store.ShouldAsk(store.QuestionCategoryCompileAssumption)
		if gateErr == nil && !ask && defaultAnswer != "" {
			assumedContext := compileContext + "\n\nEmpirical ask policy: assume and declare this answered default:\n" +
				question + "\nDefault answer: " + defaultAnswer
			if assumed, compileErr := r.compile(ctx, command.Brief(), assumedContext); compileErr == nil &&
				strings.TrimSpace(assumed.Question) == "" {
				if err := r.store.RecordAssumedWithDefault(store.QuestionCategoryCompileAssumption,
					defaultAnswer, command.SessionID, question); err == nil {
					compiled = assumed
					compiled.Assumptions = append([]string{
						fmt.Sprintf("%s — defaulted to %s", question, defaultAnswer),
					}, compiled.Assumptions...)
				}
			}
		}
	}

	if question := strings.TrimSpace(compiled.Question); question != "" {
		// One gap was too consequential to guess. Ask in the agent's voice
		// and stop; the reply arrives as an ordinary next message and the
		// head routes it with this exchange in context.
		return commandOutcome{
			status:        store.CommandRejected,
			result:        "asked the user: " + clipLabel(question, 200),
			receipt:       question,
			asAgent:       true,
			options:       compiled.QuestionOptions,
			category:      store.QuestionCategoryCompileAssumption,
			defaultAnswer: defaultQuestionAnswer(compiled.QuestionOptions),
		}, nil
	}
	if strings.TrimSpace(compiled.Goal) == "" {
		return commandOutcome{}, errors.New("compile request: compiler returned an empty goal")
	}
	if err := ctx.Err(); err != nil {
		return commandOutcome{}, err
	}

	var subtree store.Subtree
	// The compile is answered and the plan has not started. Saying so closes the
	// reading and opens the pass that produces every row after this one — and it
	// is the only row the two silent shapes of splice, a recognized craft and a
	// planner-less single leaf, will ever produce before the work exists.
	r.noteCommandStage(command, stagePlanning, "")
	// Learned know-how is asked for before anything is planned: a request the
	// shelf answers decisively compiles to that workflow's subtree, and every
	// other request plans exactly as it always did.
	use, usingCraft := r.craftCompile(ctx, command)
	if usingCraft {
		subtree = use.subtree
	} else if r.plan == nil {
		subtree = store.Subtree{Nodes: []store.NodeSpec{{
			ID:    commandPlanAnchor(command),
			Brief: compiled.Goal,
			Stage: 1,
		}}}
	} else {
		planCtx := withPlanAnchor(ctx, PlanAnchor{
			NodeID:    commandPlanAnchor(command),
			SessionID: command.SessionID, CommandSeq: command.Seq,
		})
		// The planner sees the decisions, not just the goal. An assumption that
		// only ever reached a receipt was a promise nobody was assigned: "review
		// the diff for security regressions before pushing" has to become a step
		// or a leaf's law, and which of the two it becomes is the planner's call.
		planned := compiled
		planned.Goal = anchorWorkingDecisions(compiled.Goal, compiled.Assumptions)
		subtree, err = r.plan(planCtx, planned)
		if err != nil {
			return commandOutcome{}, fmt.Errorf("plan request: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return commandOutcome{}, err
	}
	subtree = anchorSubtreeWorkingDecisions(subtree, compiled.Assumptions)
	subtree = r.wireContinuity(subtree, compiled.BuildsOn)
	subtree = r.forceWorkerOnSubtree(subtree)
	r.titleSubtree(ctx, &subtree, compiled)

	planModel, runModel := r.splitModelSlots(compiled.WorkModel)
	provenance := store.Provenance{
		Origin:        store.OriginUser,
		SessionID:     command.SessionID,
		Intent:        command.Instruction,
		TrialOf:       compiled.TrialOf,
		ServiceIntent: compiled.ServiceIntent,
		WorkModel:     strings.TrimSpace(compiled.WorkModel),
		PlanModel:     planModel,
		RunModel:      runModel,
		Attachments:   append([]string(nil), command.Attachments...),
		Craft:         use.reference,
		Subharness:    r.chosenSubharness(compiled),
	}
	if err := r.store.Splice(store.RootID, subtree, provenance); err != nil {
		if r.plan != nil || !r.defaultSpliceExists(command, compiled) {
			return commandOutcome{}, err
		}
	}
	// The work exists. This is the last row the command posts and the one that
	// ends the wait — the receipt that follows it is filed rather than spoken
	// (HeadSpeaksFor), so without this the card's final transition from being
	// made to being real was signalled by nothing at all.
	r.noteCommandStage(command, stageStarting, spliceStageLatest(subtree))

	receipt := compileReceipt(compiled.Goal, compiled.Assumptions, compiled.ModelNote)
	switch {
	case usingCraft:
		receipt = use.receipt
	case strings.TrimSpace(use.receipt) != "":
		// A learned way of working was found and set aside because the person
		// asked for this one from scratch. Saying so is the whole difference
		// between being heard and being ignored.
		receipt = use.receipt + "\n" + receipt
	}
	if promoted {
		receipt = reflexPromotionLine
	}
	result := fmt.Sprintf("spliced %d nodes", len(subtree.Nodes))
	if onceNote != "" {
		receipt = onceNote + "\n" + receipt
		result = "standing draft resolved as once; " + result
	}
	return commandOutcome{
		status:  store.CommandApplied,
		result:  result,
		receipt: receipt,
	}, nil
}

// oneShotErrandContext is the surface fact in the compile context, for any
// compiler that reads context rather than being told directly.
const oneShotErrandContext = "\n\nSurface: this ask arrived as a single headless errand — one run, nobody " +
	"at a keyboard. It is work to be done once, now; it is never a standing rule, schedule or watch, and " +
	"no question asked about it can be answered.\n"

// oneShotErrandOnceEvidence is what the journal records about a standing draft
// that never had a ratification card to stand on.
const oneShotErrandOnceEvidence = "one-shot errand surface: `aforge do` is the choice of once, not standing"

// keepTheAskVerbatim is the headless surface's half of the compile contract.
//
// Chat's whole value at this seam is that it re-asks the question better: it
// rewrites a half-formed sentence into a goal, declares the assumptions it is
// working under, and asks back when one of them is too consequential to guess.
// An errand submitted from a script has none of that to gain and one specific
// thing to lose. The caller wrote the task; the task is the specification; and
// a compiler that improves it means a harness measured something nobody wrote,
// with no way to see that it happened — the reworded goal is the only goal the
// journal ever holds.
//
// So on this surface the goal is the instruction, trimmed and otherwise
// untouched, and the compiler's speculative assumptions are dropped rather than
// anchored into the work as decisions the leaves are held to. Everything the
// planner genuinely needs still comes from the compile — scale, parts, the
// chosen worker, the jobs this one builds on, the title, the model words. Only
// the wording of the ask is refused, because only the wording was the person's.
//
// An empty instruction leaves the compile alone: there is nothing verbatim to
// keep, and the empty-goal check downstream is the honest place for it to fail.
func (r *Reconciler) keepTheAskVerbatim(command store.Command, compiled Compiled) Compiled {
	if !r.oneShotErrand {
		return compiled
	}
	verbatim := strings.TrimSpace(command.Instruction)
	if verbatim == "" {
		return compiled
	}
	compiled.Goal = verbatim
	compiled.Assumptions = nil
	return compiled
}

// assumeAndDeclare turns a compiler askback into a decision on the record.
//
// Headless, a question is not a smaller failure than a wrong answer — it is a
// run that compiled, asked into an empty room, and returned an interactive card
// where its caller expected work. The only useful thing left is to pick the
// answer the compiler itself ranked first and say so out loud, which is exactly
// what the empirical ask policy does for a resident that has learned this
// category is not worth asking about.
//
// Declared means two things and both matter. The journal records the skipped
// ask, so a later correction can find it; and the answer joins Assumptions,
// which is what carries it into the goal the planner reads and onto the node
// the delivery gate judges. An assumption nothing is held to is a guess.
func (r *Reconciler) assumeAndDeclare(command store.Command, compiled Compiled, question string) Compiled {
	answer := defaultQuestionAnswer(compiled.QuestionOptions)
	// Best effort, exactly as the empirical path treats it: a journal write that
	// fails costs the correction trail, and doing the work anyway is still the
	// better of the two outcomes available to a run with nobody at the keyboard.
	_ = r.store.RecordAssumedWithDefault(store.QuestionCategoryCompileAssumption,
		answer, command.SessionID, question)
	compiled.Question, compiled.QuestionOptions = "", nil
	compiled.Assumptions = append([]string{
		fmt.Sprintf("%s — nobody was at the keyboard on this surface; assumed %s", question, answer),
	}, compiled.Assumptions...)
	return compiled
}

// resolveStandingAsOnce answers the ratification question the way the caller
// already answered it by typing the verb, and hands back ordinary work.
//
// It is deliberately not a shortcut around the machinery: the draft is
// journaled and retired so the record says what was proposed and what became
// of it, and the returned brief then goes through compile's own splice path —
// planner, working method, gate — because "do it once" means do it properly
// once, not splice a bare node and hope.
//
// The goal is the submitted ask itself — this only ever runs on the one-shot
// surface, whose law is that the caller's sentence is the specification
// (keepTheAskVerbatim). The charter's invariant, which the temporal compiler is
// required to keep verbatim anyway, is the fallback for the command that
// somehow arrived without words, and the action template backstops that.
func (r *Reconciler) resolveStandingAsOnce(command store.Command, compiled Compiled) (Compiled, string, error) {
	spec := *compiled.Charter
	goal := strings.TrimSpace(command.Instruction)
	if goal == "" {
		goal = strings.TrimSpace(spec.Invariant)
	}
	if goal == "" {
		goal = strings.TrimSpace(spec.Action)
	}
	// A spec complete enough to be a charter is recorded as one and retired in
	// the same breath. One too thin to draft is not an error here — it was
	// never going to be a standing rule on this surface anyway — and the work
	// still runs, which is the only thing the caller asked for.
	id := fmt.Sprintf("charter-%d", command.Seq)
	if charter, err := r.store.DraftCharter(id, command.SessionID, command.Seq, spec); err == nil {
		if template := strings.TrimSpace(charter.Invariant); template != "" && goal == "" {
			goal = template
		}
		if err := r.store.SetCharterStatusWithReason(charter.ID, store.CharterRetired, store.Ratification{
			Origin: store.OriginUser, SessionID: command.SessionID, Evidence: oneShotErrandOnceEvidence,
		}, oneShotErrandOnceEvidence); err != nil {
			return compiled, "", fmt.Errorf("retire the standing draft this surface cannot ratify: %w", err)
		}
	}
	compiled.Charter = nil
	compiled.Question = ""
	compiled.QuestionOptions = nil
	compiled.Goal = goal
	if strings.TrimSpace(compiled.Scale) == "" {
		compiled.Scale = "task"
	}
	return compiled, "Read as a standing rule; doing it once instead — a one-shot errand has no cadence to ratify.", nil
}

func defaultQuestionAnswer(options []store.QuestionOption) string {
	if len(options) == 0 {
		return "yes"
	}
	if value := strings.TrimSpace(options[0].Value); value != "" {
		return value
	}
	return strings.TrimSpace(options[0].Label)
}

func attachedDocumentNames(attachments []string) []string {
	seen := make(map[string]bool)
	var names []string
	for _, path := range attachments {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".pdf", ".docx", ".pptx":
		default:
			continue
		}
		name := filepath.Base(path)
		if name == "." || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func attachedDocumentCompileContext(attachments []string) string {
	names := attachedDocumentNames(attachments)
	if len(names) == 0 {
		return ""
	}
	return "\n\nAttached documents (workspace inputs; workers receive exact staged paths in their briefs):\n- " +
		strings.Join(names, "\n- ") +
		"\nWorkers read these with read_document; they are not ordinary chat-model content parts."
}

// WorkingDecisionsHeader names the block wherever it is rendered. Assumptions
// were only ever a receipt: the compiler declared "run the test suite before
// opening the PR" and nothing downstream was ever told. A decision the work is
// not held to is not a decision, so the same list travels with the goal, into
// the leaves, and on to the gate that judges what came back.
const WorkingDecisionsHeader = "Working decisions, already made — honor them:"

// anchorWorkingDecisions appends the declared decisions to a goal, in the idiom
// attached documents already use: deterministic sentences a provider cannot
// drop, added after the model has had its say.
func anchorWorkingDecisions(goal string, assumptions []string) string {
	decisions := workingDecisions(assumptions)
	if decisions == "" || strings.Contains(goal, WorkingDecisionsHeader) {
		return goal
	}
	return strings.TrimSpace(goal) + "\n\n" + decisions
}

// anchorSubtreeWorkingDecisions puts the decisions on the deliverable owner —
// the one node with no parent inside the subtree. That node writes the answer
// the user reads and is the node the delivery gate judges, so the standard it
// is held to has to be durable on it rather than left in a prompt.
func anchorSubtreeWorkingDecisions(subtree store.Subtree, assumptions []string) store.Subtree {
	if workingDecisions(assumptions) == "" {
		return subtree
	}
	for index, spec := range subtree.Nodes {
		if strings.TrimSpace(spec.Parent) == "" {
			subtree.Nodes[index].Brief = anchorWorkingDecisions(spec.Brief, assumptions)
			break
		}
	}
	return subtree
}

func workingDecisions(assumptions []string) string {
	kept := make([]string, 0, len(assumptions))
	for _, assumption := range assumptions {
		if assumption = strings.TrimSpace(assumption); assumption != "" {
			kept = append(kept, assumption)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return WorkingDecisionsHeader + "\n- " + strings.Join(kept, "\n- ")
}

// anchorAttachedDocuments makes the compiler's brief reliable even when a
// provider overlooks the attachment context. Planning therefore cannot erase
// the inputs before the leaf receives its exact staged workspace paths.
func anchorAttachedDocuments(goal string, attachments []string) string {
	names := attachedDocumentNames(attachments)
	if len(names) == 0 {
		return goal
	}
	return strings.TrimSpace(goal) + "\n\nAttached documents:\n- " + strings.Join(names, "\n- ") +
		"\nUse the workspace copies through read_document when their contents are needed."
}

// unfinishedSource reads the other meaning Target carries on a splice: not a
// settled reflex to reuse, but a job still open that this ask arrived beside.
// An id that names nothing, or names work already over, costs the continuity
// rather than the splice — the same forgiveness wireContinuity gives.
func (r *Reconciler) unfinishedSource(command store.Command) string {
	target := strings.TrimSpace(command.Target)
	if target == "" {
		return ""
	}
	// The spine is never "work still in flight" that this ask arrived beside,
	// however it got named. It is Running by construction, so continuity from
	// it is a dependency that never settles.
	if target == store.RootID {
		return ""
	}
	node, ok, err := r.store.Node(target)
	if err != nil || !ok || node.Group == ReflexGroup {
		return ""
	}
	// A correction is the one splice whose target is supposed to be over. The
	// user is not saying "this arrived beside live work", they are saying the
	// delivered thing is wrong — so the settled job is the strongest
	// continuity source there is, and refusing it left the revision with no
	// builds_on edge and therefore no sight of what it was revising.
	if terminal(node.Status) {
		if !IsCorrection(command.Instruction) {
			return ""
		}
		return node.ID
	}
	return node.ID
}

// CorrectionMarker is the head's mark on a splice that revises the work it
// targets. It is written here as well as where the head mints it because it is
// a wire form between two halves of the system — the same reason the head
// writes the craft-consent option codes out twice — and because the resident
// cannot import the head: the head already imports the resident for cue
// extraction, so the dependency runs one way only.
const CorrectionMarker = "Correcting delivered work:"

// IsCorrection reports that this splice is a revision of the job it targets
// rather than new work that merely follows one.
func IsCorrection(instruction string) bool {
	return strings.Contains(instruction, CorrectionMarker)
}

func prependBuildsOn(first string, rest []string) []string {
	buildsOn := []string{first}
	for _, id := range rest {
		if strings.TrimSpace(id) != first {
			buildsOn = append(buildsOn, id)
		}
	}
	return buildsOn
}

func (r *Reconciler) promotionSource(command store.Command) (store.Node, bool, error) {
	if strings.TrimSpace(command.Target) == "" {
		return store.Node{}, false, nil
	}
	node, ok, err := r.store.Node(command.Target)
	if err != nil {
		return store.Node{}, false, err
	}
	if !ok || node.Group != ReflexGroup {
		return store.Node{}, false, nil
	}
	if node.Status != store.Done ||
		node.Provenance.Intent != command.Instruction ||
		node.Provenance.SessionID != command.SessionID {
		return store.Node{}, false, fmt.Errorf("splice target %q is not the matching settled reflex", command.Target)
	}
	return node, true, nil
}

// TitleFunc compresses one goal into a few display words. It is a chat-surface
// nicety: headless runs never construct a reconciler, so they never pay for it.
type TitleFunc func(ctx context.Context, goal string) (string, error)

// WithTitler sets the display-title compressor. Nil stays valid: nodes fall
// back to the first line of their brief everywhere titles are shown.
func (r *Reconciler) WithTitler(title TitleFunc) *Reconciler {
	r.title = title
	return r
}

// WithOverrunPlanner installs restart-safe resumption for repairs deferred at
// the daily rail. Zero budget keeps the planner unlimited.
func (r *Reconciler) WithOverrunPlanner(dailyBudgetUSD float64, plan OverrunPlanFunc) *Reconciler {
	r.dailyBudgetUSD = dailyBudgetUSD
	r.overrunPlan = plan
	return r
}

// CancelRethinkFunc shows one user cancellation to the plan sentinel. It is
// injected for the same reason the overrun planner is: the live plan document
// belongs to the process that planned it, and the reconciler owns only the
// moment at which somebody should look at it again.
type CancelRethinkFunc func(ctx context.Context, node store.Node, reason string)

// WithCancelRethink gives cancellation the upward channel failure has had all
// along. Without it a cancel is exactly what it was before — the work stops and
// the plan around it carries on as though nothing had been withdrawn.
func (r *Reconciler) WithCancelRethink(rethink CancelRethinkFunc) *Reconciler {
	r.cancelRethink = rethink
	return r
}

// rethinkAfterCancel routes one cancellation to the plan sentinel, at most once
// per command.
//
// Three filters, and each is the whole reason a fourth replan authority is not
// what this is. Only the user's own cancellations pass — a revision's own
// removals cancel pending nodes too, and convening the sentinel over its own
// edit is a loop with a budget attached. Only work that never started passes —
// a running leaf is the leaf wrapper's business, which holds the partial and
// revises before it hands the claim back, and letting both fire would spend
// twice to say one thing. And only the top of a cancelled subtree passes: a
// cascade cancels N nodes and the sentinel is owed one event naming the root,
// not N events racing each other over the same remainder. The parent's own
// status is the whole test, which keeps this derived from the graph rather than
// from a set of command ids this process happens to remember.
func (r *Reconciler) rethinkAfterCancel(ctx context.Context, node store.Node) error {
	if r.cancelRethink == nil || strings.TrimSpace(node.Error) != store.UserCancelReason {
		return nil
	}
	if !node.StartedAt.IsZero() {
		return nil
	}
	if node.Parent != "" {
		parent, ok, err := r.store.Node(node.Parent)
		if err != nil {
			return err
		}
		if ok && parent.Status == store.Cancelled {
			return nil
		}
	}
	r.cancelRethink(ctx, node, node.Error)
	return nil
}

// titleSubtree names the job's root node — the line the rail shows for the
// whole job. Planned leaves keep the planner's own short titles; the root is
// the one node whose title would otherwise be a generic "Synthesis" or the
// full compiled goal. Best effort by design: a titling failure costs a long
// label, never the job.
func (r *Reconciler) titleSubtree(ctx context.Context, subtree *store.Subtree, compiled Compiled) {
	// The compile call has already read the whole ask, so the name comes back
	// with the goal it names and costs nothing. The separate pass survives as
	// the fallback for a compiler that did not answer and for every caller
	// that has no compiler at all.
	named := strings.TrimSpace(compiled.Title)
	if named == "" && r.title == nil {
		return
	}
	for index := range subtree.Nodes {
		node := &subtree.Nodes[index]
		if node.Parent != "" {
			continue
		}
		if node.Title != "" && !strings.EqualFold(node.Title, "synthesis") {
			return
		}
		short := named
		if short == "" {
			answered, err := r.title(ctx, compiled.Goal)
			if err != nil {
				return
			}
			short = strings.TrimSpace(answered)
		}
		if short == "" {
			return
		}
		node.Title = clipLabel(short, 48)
		return
	}
}

func (r *Reconciler) defaultSpliceExists(command store.Command, compiled Compiled) bool {
	node, ok, err := r.store.Node(fmt.Sprintf("task-%d", command.Seq))
	if err != nil || !ok {
		return false
	}
	return node.Parent == store.RootID &&
		node.Provenance.Origin == store.OriginUser &&
		node.Provenance.SessionID == command.SessionID &&
		node.Provenance.Intent == command.Instruction &&
		node.Provenance.TrialOf == compiled.TrialOf
}

func (r *Reconciler) cancel(ctx context.Context, command store.Command) (commandOutcome, error) {
	nodes, err := r.store.Nodes()
	if err != nil {
		return commandOutcome{}, err
	}
	targets, ok := descendants(nodes, command.Target)
	if !ok {
		return commandOutcome{}, fmt.Errorf("target %q no longer exists", command.Target)
	}

	impact, err := r.store.Impact(command.Target, time.Now())
	if err != nil {
		return commandOutcome{}, err
	}
	cancelled, requested := 0, 0
	for _, id := range targets {
		if err := ctx.Err(); err != nil {
			return commandOutcome{}, err
		}
		node, found, err := r.store.Node(id)
		if err != nil {
			return commandOutcome{}, err
		}
		if !found || terminal(node.Status) {
			continue
		}
		switch node.Status {
		case store.Pending:
			if err := r.store.CancelPending(id, store.UserCancelReason); err != nil {
				return commandOutcome{}, err
			}
			cancelled++
		case store.Claimed, store.Running:
			if err := r.store.RequestNodeCancel(id, store.UserCancelReason); err != nil {
				return commandOutcome{}, err
			}
			requested++
		}
	}
	result := fmt.Sprintf("cancelled %d; requested cooperative cancellation for %d", cancelled, requested)
	receipt := fmt.Sprintf("cancelled — %d %s cancelled", cancelled, plural(cancelled, "step", "steps"))
	if requested > 0 {
		receipt = fmt.Sprintf("cancellation requested — %d running %s will release at the next boundary",
			requested, plural(requested, "step", "steps"))
		if cancelled > 0 {
			receipt += fmt.Sprintf(", %d pending cancelled", cancelled)
		}
	}
	if impact.Cost > 0 {
		receipt += fmt.Sprintf(", $%.2f spent stays spent", impact.Cost)
	}
	return commandOutcome{
		status:  store.CommandApplied,
		result:  result,
		receipt: receipt,
	}, nil
}

func commandReceiptNode(command store.Command) string {
	if isNodeSurgeryCommand(command.Kind) {
		return command.Target
	}
	return ""
}

func isNodeSurgeryCommand(kind store.CommandKind) bool {
	switch kind {
	case store.CommandCancel, store.CommandPause, store.CommandResume, store.CommandAmend,
		store.CommandReprioritize, store.CommandRestart, store.CommandRedirect, store.CommandExpedite:
		return true
	default:
		return false
	}
}

func descendants(nodes []store.Node, target string) ([]string, bool) {
	children := make(map[string][]string)
	found := false
	for _, node := range nodes {
		if node.ID == target {
			found = true
		}
		children[node.Parent] = append(children[node.Parent], node.ID)
	}
	if !found {
		return nil, false
	}

	result := []string{target}
	for index := 0; index < len(result); index++ {
		result = append(result, children[result[index]]...)
	}
	return result, true
}

// initializeWatcher resumes the settle lane where the last reconciler left it.
// The watermark is the whole point: announce, distill and fold are one-shot
// reactions to settlement events and nothing re-derives them, so priming to the
// journal's head — which is what this did before — silently discarded every job
// that landed between one process ending and the next one starting. Only a
// store that has never had a resident starts at the head, and it writes its
// starting position immediately so the very next process inherits it.
func (r *Reconciler) initializeWatcher() error {
	if r.watcherInitialized {
		return nil
	}
	watermark, found, err := r.store.ResidentWatermarkFor(store.LaneSettlement)
	if err != nil {
		return err
	}
	if found {
		r.lastEventSeq = watermark.Cursor
		r.settlementMark = watermark.Cursor
		r.watcherInitialized = true
		return nil
	}
	seq, err := r.store.LatestEventSeq()
	if err != nil {
		return err
	}
	if _, err := r.store.MarkResidentWatermark(store.LaneSettlement, seq); err != nil {
		return err
	}
	r.lastEventSeq = seq
	r.settlementMark = seq
	r.watcherInitialized = true
	return nil
}

// checkpointSettlementLocked records how far the settle pass got. It writes
// only when the pass consumed something other than its own watermark events:
// each write is itself an event, so an unconditional checkpoint would advance
// the journal on every tick forever and no tick could ever be quiet again.
func (r *Reconciler) checkpointSettlementLocked(consumedWork bool) error {
	if !consumedWork || r.lastEventSeq <= r.settlementMark {
		return nil
	}
	if _, err := r.store.MarkResidentWatermark(store.LaneSettlement, r.lastEventSeq); err != nil {
		return err
	}
	r.settlementMark = r.lastEventSeq
	return nil
}

func (r *Reconciler) announceTransitions(ctx context.Context) error {
	consumedWork := false
	defer func() { _ = r.checkpointSettlementLocked(consumedWork) }()
	for {
		events, err := r.store.Events(r.lastEventSeq, eventBatchSize)
		if err != nil {
			return err
		}
		// Nothing to announce is the overwhelmingly common case, and the active
		// view below is a full-table decode. Reading it before finding out there
		// were no events meant paying for the whole graph to walk an empty list.
		if len(events) == 0 {
			return nil
		}
		var byID map[string]store.Node
		if r.narrate != nil {
			nodes, err := r.store.ActiveNodes()
			if err != nil {
				return err
			}
			byID = make(map[string]store.Node, len(nodes))
			for _, node := range nodes {
				byID[node.ID] = node
			}
		}
		for _, event := range events {
			if err := ctx.Err(); err != nil {
				return err
			}
			if event.Kind != store.EventResidentWatermarked {
				consumedWork = true
			}
			switch event.Kind {
			case store.EventNodeCompleted, store.EventNodeFailed:
				node, ok, err := r.store.Node(event.NodeID)
				if err != nil {
					return err
				}
				continuing := false
				if event.Kind == store.EventNodeCompleted && ok {
					continuing, err = r.continuingNode(node)
					if err != nil {
						return err
					}
				}
				// An exhausted node is not an ending, so nothing is announced or
				// narrated for it: the split receipt has already spoken and the
				// work continues elsewhere. It is still a landing, though. Taking
				// the whole lane away meant the most expensive jobs in the system
				// — the ones that blew a budget — taught the notebook nothing and
				// left their subtree open on the board forever, so the partial is
				// distilled and the subtree folded exactly as any other landing's.
				if !continuing {
					// The fallback runs BEFORE the announcement, and the order is
					// the whole point: the row a person reads has to say what
					// happens next, and "next" is not known until the fresh plan
					// has either landed or been refused. Announcing first and
					// replanning after would produce the two rows this wave
					// exists to delete — a failure that reads as the end, then
					// unexplained work appearing beside it.
					replanned := false
					if ok && event.Kind == store.EventNodeFailed {
						replanned = r.craftFallback(ctx, node)
					}
					if err := r.announceNode(event, craftFallbackNext(replanned)); err != nil {
						return err
					}
					r.recordForNarration(byID, event)
					if ok {
						r.recordCraftOutcome(node, event.Kind == store.EventNodeCompleted)
						// A redirection that raced this landing was never read.
						// The landing is the only moment that can know it, and
						// saying nothing is how a correction evaporates.
						if err := r.reportMissedDirection(node); err != nil {
							return err
						}
					}
				}
				practice := ok && node.Group == store.PracticeGroup &&
					node.Provenance.Origin == store.OriginSelf
				if ok && (r.effectiveSessionID(node) != "" || practice) {
					if event.Kind == store.EventNodeFailed {
						r.distillJob(ctx, node, true)
					} else if node.Parent == store.RootID && !r.reflexPromoted(node) {
						r.distillJob(ctx, node, false)
					}
					// Folding no longer rides this tick; foldSettledJobs takes it
					// once the grace window has passed. See settledFoldGrace.
				}
			case store.EventNodeCancelled:
				// A cancelled craft run counts against its version the way a
				// failure does: the user stopped it, which is the strongest
				// thing anyone can say about know-how that was supposed to fit.
				if node, ok, err := r.store.Node(event.NodeID); err == nil && ok {
					r.recordCraftOutcome(node, false)
					if err := r.rethinkAfterCancel(ctx, node); err != nil {
						return err
					}
				}
			case store.EventNodeStarted:
				r.recordForNarration(byID, event)
			}
			r.lastEventSeq = event.Seq
		}
		if len(events) < eventBatchSize {
			return nil
		}
	}
}

// overrunSplitPrefix is the invariant head of the one shared split receipt,
// cut out of that receipt with a probe count rather than copied by hand. A
// hand-copied substring is a second statement of user-facing prose, and the day
// the sentence is reworded the detection quietly stops matching anything.
var overrunSplitPrefix = splitReceiptPrefix()

func splitReceiptPrefix() string {
	const probe = 987654321
	sentence := OverrunContinuationMessage(probe)
	cut := strings.Index(sentence, fmt.Sprint(probe))
	if cut <= 0 {
		return sentence
	}
	return "[" + strings.TrimRight(sentence[:cut], " ")
}

func (r *Reconciler) continuingNode(node store.Node) (bool, error) {
	if SplitContinued(node.Summary) {
		return true, nil
	}
	return r.store.OverrunDeferred(node.ID)
}

// effectiveSessionID names the conversation a node's news belongs to. Work
// spliced by a plan revision or an internal repair carries no session of its
// own, and reading only the node's own provenance meant every one of those
// failures was swallowed: announceNode returned before it could interrupt
// anyone. The job root is the conversation; a child inherits it.
func (r *Reconciler) effectiveSessionID(node store.Node) string {
	if session := strings.TrimSpace(node.Provenance.SessionID); session != "" {
		return session
	}
	for hops := 0; hops < maxSessionWalk && node.Parent != "" && node.Parent != store.RootID; hops++ {
		parent, ok, err := r.store.Node(node.Parent)
		if err != nil || !ok {
			return ""
		}
		if session := strings.TrimSpace(parent.Provenance.SessionID); session != "" {
			return session
		}
		node = parent
	}
	return ""
}

// maxSessionWalk bounds the ancestor walk. Graph depth is small by
// construction; the bound is here so a cycle written by a future splice bug
// costs a miss rather than the reconciler.
const maxSessionWalk = 32

// roomPolicy is the resident's answer to one question — when the conversation a
// piece of news was born in is not the conversation somebody is sitting in,
// which one hears it? — and there are two right answers at once.
//
// The surface shipping today has one window and a session id that changes every
// launch, so its news is re-addressed to whoever is attached; that re-homing is
// the only reason an overnight job's answer is ever read (see
// deliverySessionID). Rooms invert it: a room is a place, work belongs to the
// place that commissioned it, and news that wanders between rooms is news filed
// under somebody else's task. The old surface keeps working while the new one
// grows beside it, so the choice is made once, at construction, and read
// wherever an address is decided — never re-derived from the environment.
type roomPolicy int

const (
	// roomsLegacy re-homes to the attached session. It is the zero value on
	// purpose: a Reconciler built any other way than New — tests, embeddings —
	// gets the behavior every shipped surface already has.
	roomsLegacy roomPolicy = iota
	// roomsOwnerPinned addresses the thread that commissioned the work, read
	// from the node's own provenance. Selected by the v2 chat surface.
	roomsOwnerPinned
)

// chatV2Env selects the v2 chat surface. The room policy rides it rather than
// carrying a flag of its own, because a half-pinned surface — v2 rooms with
// legacy re-homing, or the reverse — is not a configuration anyone wants.
const chatV2Env = "AFORGE_CHAT_V2"

func resolveRoomPolicy() roomPolicy {
	if strings.TrimSpace(os.Getenv(chatV2Env)) == "1" {
		return roomsOwnerPinned
	}
	return roomsLegacy
}

// roomPolicy reads the resolved policy. Nil-safe because the addressing paths
// must degrade to the legacy route rather than fault: an announcement that
// panics loses the deliverable it was carrying.
func (r *Reconciler) roomPolicy() roomPolicy {
	if r == nil {
		return roomsLegacy
	}
	return r.rooms
}

// deliverySessionID names the room a deliverable is actually spoken into.
//
// The originating session is the right address only while somebody is still in
// it. A charter's firings carry the session the user was in when they said
// "yes, stand this up", and every launch mints a new session id, so for the
// rest of that watch's life its findings were posted into a room that was
// sealed on day one — the answer existed, was journaled, and was unreadable.
// An overnight job has the same shape: it lands at 03:00 addressed to
// yesterday.
//
// The brief already solved this: it reads the attach watermark and speaks into
// whoever is home. So does the rescue that rehomes an orphaned blocking
// question. This is the same reasoning applied to the deliverable itself —
// deliver into the attached session when the originating one has nobody in it,
// and leave everything exactly where it was when it has.
func (r *Reconciler) deliverySessionID(origin string) string {
	origin = strings.TrimSpace(origin)
	// Under rooms the reasoning above stops holding: the room that commissioned
	// the work is a place the user can walk back into, so its answer waits there
	// instead of following them. A node that names no room of its own is the one
	// case with nothing to pin to, and it keeps the legacy address — the choice
	// is between the wrong room and no room, and no room is silence.
	if r.roomPolicy() == roomsOwnerPinned && origin != "" {
		return origin
	}
	seen, found, err := r.store.LastSeen()
	if err != nil || !found || seen.State != store.SeenAttached {
		return origin
	}
	live := strings.TrimSpace(seen.SessionID)
	if live == "" || live == origin {
		return origin
	}
	return live
}

// announceRoom names the room a node's news is spoken into, and it is the one
// place the two policies disagree about a node with no conversation behind it.
// Legacy says nothing for it: it has no originating room, and re-homing is
// defined as a move *from* one. Owner-pinned cannot afford that silence — it
// pins on provenance, so a splice that carried none would be exactly the node
// whose deliverable disappeared. It falls back to the legacy address instead,
// which is the attached room if anyone is home and nowhere if nobody is.
func (r *Reconciler) announceRoom(node store.Node) string {
	origin := r.effectiveSessionID(node)
	if origin == "" && r.roomPolicy() != roomsOwnerPinned {
		return ""
	}
	return r.deliverySessionID(origin)
}

// announceNode posts the one row a landing owes the room. next is what the
// caller has already arranged to happen after a failure — a fallback that is
// running, or nothing — and it is a parameter rather than something read back
// out of the graph because only the caller can know it in time.
func (r *Reconciler) announceNode(event store.Event, next string) error {
	node, ok, err := r.store.Node(event.NodeID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("event %d names missing node %q", event.Seq, event.NodeID)
	}
	sessionID := r.announceRoom(node)
	if sessionID == "" {
		return nil
	}
	if event.Kind == store.EventNodeCompleted && r.reflexPromoted(node) {
		return nil
	}

	// The rail is the status; the thread is the conversation. Intermediate
	// completions are progress, which the graph lens already shows live, so
	// posting them read as machine reporting. The thread speaks only when
	// something needs the user: the deliverable's answer, or a failure.
	var body string
	switch event.Kind {
	case store.EventNodeCompleted:
		if node.Parent != store.RootID {
			return nil
		}
		body = node.Summary
		if strings.TrimSpace(body) == "" {
			body = "That's done — it finished without leaving a summary."
		}
	case store.EventNodeFailed:
		// COMPOSED FROM PARTS, NEVER FORWARDED. This row used to be
		// `I hit a problem with "<brief>": <the whole first line of the error>`,
		// which is a template with transport poured into it: the brief is the
		// machine's own phrasing of a step, and the error is whatever the
		// executor last wrapped — node ids, attempt counts and a JSON body, all
		// of it delivered to a person as an explanation. See failure.go for the
		// four things a reader is actually owed and where the raw text goes.
		body = FailedNode(node, next).Room()
	default:
		return nil
	}

	// The asymmetry between the two arms is deliberate and it is not the noise
	// law slipping: an intermediate COMPLETION is progress and returns above
	// without saying anything, while an intermediate FAILURE speaks. A leg of
	// somebody's task dying is that task's news whether or not the leg was the
	// task — nothing else in the product says it in time, and the room it is
	// spoken into is the one that owns the work rather than whichever room is
	// open (see the rooms tests either side of this).
	_, err = thread.Post(r.store, store.Message{
		SessionID: sessionID,
		Role:      store.RoleSystem,
		Body:      boundMessage(body),
		NodeID:    node.ID,
	})
	if err != nil {
		return err
	}
	return r.surfaceNaturalQuestionLocked(sessionID)
}

func (r *Reconciler) reflexPromoted(node store.Node) bool {
	if node.Group != ReflexGroup {
		return false
	}
	promoted, err := r.store.HasCommandTarget(node.ID, store.CommandSplice)
	return err == nil && promoted
}

// compileContextBytes bounds what the compiler sees of the graph. Continuity
// needs the recent jobs' asks and results; it does not need the whole forest.
const compileContextBytes = 6 << 10

// compileGraphRank is the head's snapshot ordering, applied here for the same
// reason it was applied there: the store returns creation order, so a
// long-lived graph handed the compiler months of settled nodes before the job
// running right now. Live work first, then queued, then the freshest history,
// with packed folds last in line for the budget.
func compileGraphRank(node store.Node) int {
	switch {
	case node.Status == store.Running || node.Status == store.Claimed:
		return 0
	case node.Status == store.Pending:
		return 1
	case node.FoldRoot:
		return 3
	default:
		return 2
	}
}

// nodeOutcome is what a node has to say for itself. A grown territory rewrites
// only its fold digest, so preferring Summary over a live FoldDigest is how the
// compiler kept quoting the three-job version of a map that now covers eleven.
func nodeOutcome(node store.Node) string {
	if node.FoldRoot {
		if digest := strings.TrimSpace(node.FoldDigest); digest != "" {
			return digest
		}
	}
	if summary := strings.TrimSpace(node.Summary); summary != "" {
		return summary
	}
	return strings.TrimSpace(node.FoldDigest)
}

func renderGraphContext(snapshot store.Snapshot) string {
	var context strings.Builder

	nodes := append([]store.Node(nil), snapshot.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		ranked, other := compileGraphRank(nodes[i]), compileGraphRank(nodes[j])
		if ranked != other {
			return ranked < other
		}
		if ranked >= 2 {
			return nodes[i].FinishedAt.After(nodes[j].FinishedAt)
		}
		return nodes[i].CreatedSeq > nodes[j].CreatedSeq
	})

	// Jobs first, newest first: "improve it" almost always reaches for the
	// most recent thing, and a job's summary carries the artifact paths the
	// next job starts from.
	//
	// Both halves spend one budget. They used to hold a limit each, so a busy
	// graph could hand the compiler twice what the constant claims — and the
	// compile context is where the notebook, the recall and the thread slice
	// are already competing for room.
	context.WriteString("jobs (newest first):\n")
	for i := len(snapshot.Nodes) - 1; i >= 0; i-- {
		node := snapshot.Nodes[i]
		if node.Parent != store.RootID || context.Len() > compileContextBytes {
			continue
		}
		fmt.Fprintf(&context, "job %s [%s]\n", node.ID, node.Status)
		if intent := strings.TrimSpace(node.Provenance.Intent); intent != "" {
			fmt.Fprintf(&context, "  asked: %s\n", clipLabel(firstLine(intent), 180))
		}
		if outcome := nodeOutcome(node); outcome != "" {
			fmt.Fprintf(&context, "  result: %s\n", clipKeepingFiles(outcome, 600))
		}
	}

	context.WriteString("\nnodes:\n")
	for _, node := range nodes {
		if context.Len() > compileContextBytes {
			break
		}
		fmt.Fprintf(&context, "%s | %s | %s\n", node.ID, firstLine(node.Brief), node.Status)
	}
	return strings.TrimSuffix(context.String(), "\n")
}

// clipBlock bounds a multi-line block, keeping its newlines: a result's file
// paths live on their own lines and survive clipping.
func clipBlock(block string, limit int) string {
	if len(block) <= limit {
		return block
	}
	cut := limit
	for cut > 0 && !utf8.ValidString(block[:cut]) {
		cut--
	}
	return strings.TrimSpace(block[:cut]) + "…"
}

const (
	// clipFileCap and clipFileBytes bound the tail a clipped result keeps. Six
	// paths is the same count the head's deep slice settled on for the same
	// reason: past that many, a compiler that cannot find the work from the
	// first six will not find it from the twelfth.
	clipFileCap   = 6
	clipFileBytes = 400
)

// clipKeepingFiles bounds a result the way the compiler needs it bounded.
//
// A deliverable is prose with its file paths appended at the end, so clipping
// the tail is precisely clipping away the only part of the result the next job
// can act on: `builds_on` exists so day two can start from day one's artifacts,
// and the paths were the first thing cut. The prose is what is expendable here
// — the compiler is reading for continuity, not for the report — so the budget
// is spent on the prose first and the paths are re-attached afterwards, exactly
// as the head's files line re-attaches what its own truncation buried.
//
// A path already surviving inside the clipped prose is not repeated; that is
// the same filter-then-cap discipline, and for the same reason.
func clipKeepingFiles(block string, limit int) string {
	if len(block) <= limit {
		return block
	}
	paths := filePointers(block)
	if len(paths) == 0 {
		return clipBlock(block, limit)
	}
	// The prose keeps at least half the budget however many paths there are: a
	// list of files with no account of what they contain is as useless to the
	// compiler as an account with no files.
	tailBudget := clipFileBytes
	if half := limit / 2; tailBudget > half {
		tailBudget = half
	}
	var tail strings.Builder
	kept := 0
	for _, path := range paths {
		if kept == clipFileCap || tail.Len()+len(path)+1 > tailBudget {
			break
		}
		tail.WriteString("\n")
		tail.WriteString(path)
		kept++
	}
	// clipBlock spends its limit on content and then adds its ellipsis, so the
	// marker is budgeted here rather than discovered afterwards.
	prose := clipBlock(block, limit-tail.Len()-len("…"))
	var files strings.Builder
	for _, path := range paths[:kept] {
		if strings.Contains(prose, path) {
			continue
		}
		files.WriteString("\n")
		files.WriteString(path)
	}
	return prose + files.String()
}

func compileReceipt(goal string, assumptions []string, modelNote string) string {
	var receipt strings.Builder
	fmt.Fprintf(&receipt, "Here's my reading: %s", strings.TrimSpace(goal))
	for _, assumption := range assumptions {
		if assumption = strings.TrimSpace(assumption); assumption != "" {
			fmt.Fprintf(&receipt, "\nAssumed: %s", assumption)
		}
	}
	if modelNote = strings.TrimSpace(modelNote); modelNote != "" {
		fmt.Fprintf(&receipt, "\n%s", modelNote)
	}
	receipt.WriteString("\nCorrect me anytime — changing course costs nothing.")
	return receipt.String()
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if newline := strings.IndexByte(value, '\n'); newline >= 0 {
		value = value[:newline]
	}
	return strings.TrimSpace(strings.TrimSuffix(value, "\r"))
}

func terminal(status store.Status) bool {
	return status == store.Done || status == store.Failed || status == store.Cancelled
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func boundMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= store.MaxMessageBytes {
		return value
	}
	cut := store.MaxMessageBytes - 3
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + "..."
}

// clipLabel bounds a node label for thread announcements, where the body
// that follows carries the substance and a full goal statement is noise.
func clipLabel(label string, limit int) string {
	if limit <= 3 || len(label) <= limit {
		return label
	}
	return strings.TrimSpace(label[:limit-1]) + "…"
}

// wireContinuity attaches declared prior jobs as inputs to the new subtree's
// entry nodes — the ones that would otherwise start from nothing. Unknown ids
// are dropped rather than failing the splice: a mistaken reference should
// cost the continuity, not the work.
func (r *Reconciler) wireContinuity(subtree store.Subtree, buildsOn []string) store.Subtree {
	if len(buildsOn) == 0 {
		return subtree
	}
	sources := make([]string, 0, len(buildsOn))
	for _, id := range buildsOn {
		// The permanent spine is a node, so it passes an existence check — and
		// it is Running forever, so a job wired to build on it can never become
		// ready. A compiler answering builds_on:["root"] is all it took to
		// deadlock a job from birth, silently, for the whole of its ceiling.
		// Continuity is between pieces of work; the trunk is not one, and it is
		// dropped with the same forgiveness an id naming nothing already gets.
		if strings.TrimSpace(id) == store.RootID {
			continue
		}
		if _, ok, err := r.store.Node(id); err == nil && ok {
			sources = append(sources, id)
		}
	}
	if len(sources) == 0 {
		return subtree
	}
	return attachNeeds(subtree, sources)
}

// WithDistiller installs the notebook's writer and returns the reconciler
// for chaining. A nil distiller (the default) records no facts.
func (r *Reconciler) WithDistiller(distill DistillFunc) *Reconciler {
	r.distill = distill
	return r
}

// distillLimit bounds how much one job may add to the notebook.
const distillLimit = 5

// distillJob extracts durable facts from one settled node, best effort: a
// failed distillation costs the notebook entry, never the loop.
// continuitySources renders the earlier jobs this one was wired to build on
// — the feeds_into edges that cross into its subtree from outside. Empty for
// a job that stands alone.
func (r *Reconciler) continuitySources(node store.Node) string {
	edges, err := r.store.ActiveEdges()
	if err != nil {
		return ""
	}
	prefix := node.ID
	if cut := strings.LastIndex(node.ID, "-n"); cut > 0 {
		prefix = node.ID[:cut]
	}
	seen := make(map[string]bool)
	var out strings.Builder
	for _, edge := range edges {
		if !inJobNamespace(edge.To, prefix) || inJobNamespace(edge.From, prefix) || seen[edge.From] {
			continue
		}
		seen[edge.From] = true
		source, ok, err := r.store.Node(edge.From)
		if err != nil || !ok {
			continue
		}
		delivered := firstLine(source.Summary)
		if delivered == "" {
			delivered = firstLine(source.FoldDigest)
		}
		out.WriteString("- asked: " + clipLabel(firstLine(source.Provenance.Intent), 200) +
			" → delivered: " + clipLabel(delivered, 200) + "\n")
	}
	return out.String()
}

// inJobNamespace is the dash-delimited namespace test the store's own prefix
// reads use. A bare HasPrefix made task-14 the owner of task-142's edges, so a
// distilled fact could claim "asked: <task-9's intent> → delivered:" about a
// job it had never touched — and that claim went into the notebook as durable
// evidence of the user's standard.
func inJobNamespace(id, prefix string) bool {
	return id == prefix || strings.HasPrefix(id, prefix+"-")
}

// redirectBlock renders what the user said while this job was already running.
// It is the strongest correction signal the system ever sees — the standard
// stated against work in progress — and until it arrived here it survived
// downstream only as a boolean. The shape deliberately matches the gate
// evidence block below it: same brackets, same instruction to distill the
// transferable standard rather than the episode.
func (r *Reconciler) redirectBlock(node store.Node) string {
	commands, err := r.store.TargetedCommands(node.ID, store.CommandRedirect, redirectDistillScan)
	if err != nil || len(commands) == 0 {
		return ""
	}
	var block strings.Builder
	used := 0
	for _, command := range commands {
		words := strings.TrimSpace(command.Instruction)
		if words == "" {
			continue
		}
		line := "- " + clipBlock(words, redirectDistillLineBytes) + "\n"
		if used+len(line) > redirectDistillBytes {
			break
		}
		used += len(line)
		block.WriteString(line)
	}
	if block.Len() == 0 {
		return ""
	}
	return "[The user redirected this job while it was running, in their own words:\n" +
		block.String() +
		"The run adapted and delivered anyway, so the gap between what it was doing and what they " +
		"asked for mid-flight is the user's standard stated out loud. Record the standard, not the episode.]"
}

// correctionBlock renders what the user said when they rejected a delivery.
//
// The distiller's prompt asks for exactly this — the user's correction — and
// has never had a wire to it. A redirect is the standard stated against work in
// progress; a correction is the standard stated against a finished deliverable
// the user has actually read, which is stronger still, because they are not
// guessing at what is coming, they are looking at it.
//
// The words come off the job's own intent rather than out of a command scan:
// the head writes the user's verbatim sentence in front of its deterministic
// block, and the splice carries that whole instruction as the intent, so the
// critique is already durable on this node. TargetedCommands would find the
// same text one join further away.
func correctionBlock(node store.Node) string {
	intent := node.Provenance.Intent
	if !IsCorrection(intent) {
		return ""
	}
	words := strings.TrimSpace(intent[:strings.Index(intent, CorrectionMarker)])
	if words == "" {
		return ""
	}
	return "[The user read the earlier delivery and said it was wrong, in their own words:\n- " +
		clipBlock(words, redirectDistillLineBytes) + "\n" +
		"This job is the corrected version. The difference between what was delivered and what they " +
		"asked for after reading it is the user's standard, stated against something they could see. " +
		"Record the standard, not the episode.]"
}

const (
	// redirectDistillBytes bounds the mid-run correction block.
	redirectDistillBytes = 600
	// redirectDistillLineBytes bounds one redirect, so a pasted specification
	// cannot be the whole block.
	redirectDistillLineBytes = 300
	// redirectDistillScan bounds the read behind it.
	redirectDistillScan = 8
)

// gateVerdictSurvived is the distiller being told that a failing gate verdict
// has to be right before the notebook learns from it.
//
// A gap the gate names is a claim about the delivery, not a fact about it, and
// the claim is sometimes simply wrong. Measured on the validation battery: one
// job delivered twelve complete profiles in one message and the gate failed it
// for "a series of separate messages… pointing to individual profiles written
// to files" — not one clause of which described the text it was judging. That
// verdict was distilled into a standing lesson within the minute and injected
// into every leaf of the next, unrelated job. A judge that is wrong once writes
// the notebook every later run reads, which is how one bad reading becomes a
// permanent instruction.
//
// So the verdict has to be borne out by something other than itself before it
// is allowed to teach. There are exactly two things that bear it out, and both
// are repairs that were actually accepted:
//
//  1. the one polish pass closed the named gap — the gap was real enough that
//     fixing it changed the verdict;
//  2. a later round in the same lineage passed — the repair the gap bought was
//     itself accepted.
//
// A verdict with neither is a verdict whose job nonetheless delivered and whose
// repair, if one ran at all, was refused in turn. The deliverable stands, the
// gap is unproven, and the distiller is shown the outcome without it. Nothing
// is lost: if the gap is real it will fail a gate again, and the round that
// closes it is the round that gets to say what it taught.
func (r *Reconciler) gateVerdictSurvived(node store.Node, gate store.DeliveryGate) bool {
	if gate.PolishClosed {
		return true
	}
	if r.store == nil {
		return false
	}
	// The lineage is the id namespace, so the base is asked for rather than
	// parsed here — the "-x" law has one owner.
	base, _ := OverrunLineage(node.ID)
	lineage, err := r.store.DeliveryGateLineage(base)
	if err != nil || len(lineage) < 2 {
		return false
	}
	return lineage[len(lineage)-1].Pass
}

func (r *Reconciler) distillJob(ctx context.Context, node store.Node, failed bool) {
	if r.distill == nil {
		return
	}
	trial, isTrial := r.trialFact(node)
	outcome := node.Summary
	revealedGap := failed
	if failed {
		outcome = node.Error
	}
	// A job that continues or reworks an earlier delivery carries the richest
	// preference signal there is: the gap between what was delivered then and
	// what was asked now is the user's actual standard, stated in actions.
	if prior := r.continuitySources(node); prior != "" {
		revealedGap = true
		outcome += "\n\n[This job continued or revised earlier delivered work:\n" + prior +
			"When the new instruction reworks an earlier delivery, the difference between them is evidence of the user's real standard — record the standard, not the episode.]"
	}
	if correction := correctionBlock(node); correction != "" {
		revealedGap = true
		outcome += "\n\n" + correction
	}
	if redirect := r.redirectBlock(node); redirect != "" {
		revealedGap = true
		outcome += "\n\n" + redirect
	}
	if gate, ok, err := r.store.DeliveryGateFor(node.ID); err == nil && ok && !gate.Pass && r.gateVerdictSurvived(node, gate) {
		revealedGap = true
		ending := "The one polish pass did not close it."
		if gate.PolishClosed {
			ending = "The one polish pass closed it."
		}
		outcome += "\n\n[Delivery gate evidence: the job delivered the outcome above, and the gate caught this missing element: " +
			gate.Gap + " " + ending + " Distill the transferable lesson in what was delivered versus what the gate required.]"
	}
	if isTrial {
		outcome += "\n\n" + r.renderTrialForDistiller(trial)
	}
	if note := r.craftDistillerNote(node); note != "" {
		outcome += "\n\n" + note
	}
	facts, err := r.distill(ctx, node.Provenance.Intent, outcome, failed)
	if err != nil {
		if isTrial {
			r.recordInconclusiveTrial(node, trial)
		}
		return
	}
	facts, drafts := splitCraftDrafts(facts)
	for _, draft := range drafts {
		r.forgeCraft(ctx, node, draft)
	}
	if len(facts) > distillLimit {
		facts = facts[:distillLimit]
	}
	trialConsumed := false
	for _, fact := range facts {
		if fact.Kind == store.FactQuestion &&
			(node.Provenance.Origin != store.OriginUser || !revealedGap) {
			continue
		}
		if isTrial && fact.Replaces == trial.Seq {
			if !trialConsumed {
				recorded, settled, err := r.recordTrialVerdict(node, trial, fact)
				if err == nil {
					trialConsumed = true
					if settled {
						r.queueLearningMoment(node.ID, settledTrialMoment(recorded,
							len(trial.Unsettled.Trials)+1))
					}
				}
			}
			continue
		}
		// Child failures still teach ordinary lessons, but only the top-level
		// trial landing is allowed to consume the pair it was assembled to test.
		if node.Parent != store.RootID && node.Provenance.TrialOf > 0 && fact.Replaces == node.Provenance.TrialOf {
			continue
		}
		if strings.TrimSpace(fact.Body) == "" {
			continue
		}
		if fact.Skill != nil {
			if strings.TrimSpace(fact.Skill.Artifact) != "" {
				if recorded, err := r.store.RecordSkillCandidateFrom(store.FactWriterDistiller, node.ID, fact.Scope,
					clipFactBody(fact.Body), fact.Skill.Artifact); err == nil {
					r.queueLearningMoment(node.ID, learnedFactMoment(recorded))
				}
			}
			continue
		}
		// A skill kind without an artifact can describe an existing skill during
		// consolidation, but distillation may never mint it active by assertion.
		if fact.Kind == store.FactSkill {
			continue
		}
		recorded, err := r.recordLearnedFact(node.ID, fact)
		if err == nil {
			r.queueLearningMoment(node.ID, learnedFactMoment(recorded))
			if fact.Replaces > 0 {
				// The distiller judged this memory to update a specific older
				// belief: the old one retires in favour of the new, journaled.
				_ = r.store.SupersedeFact(fact.Replaces, recorded.Seq)
			}
		}
	}
	if isTrial && !trialConsumed {
		r.recordInconclusiveTrial(node, trial)
	}
}

func (r *Reconciler) trialFact(node store.Node) (store.Fact, bool) {
	if node.Parent != store.RootID || node.Provenance.TrialOf <= 0 {
		return store.Fact{}, false
	}
	fact, ok, err := r.store.Fact(node.Provenance.TrialOf)
	if err != nil || !ok || fact.Status != store.FactActive || fact.Kind != store.FactUnsettled || fact.Unsettled == nil {
		return store.Fact{}, false
	}
	return fact, true
}

func (r *Reconciler) renderTrialForDistiller(fact store.Fact) string {
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "[TRIAL VERDICT REQUIRED: this job tested unsettled fact #%d.\n", fact.Seq)
	for index, approach := range fact.Unsettled.Approaches {
		fmt.Fprintf(&rendered, "Approach %d: %s\nApplicable scope: %s\nEvidence:\n", index+1,
			approach.Approach, approach.Scope)
		for _, evidenceSeq := range approach.Evidence {
			evidence, ok, err := r.store.Fact(evidenceSeq)
			if err != nil || !ok {
				fmt.Fprintf(&rendered, "- #%d (unavailable)\n", evidenceSeq)
				continue
			}
			fmt.Fprintf(&rendered, "- #%d [%s · %s] %s\n", evidence.Seq, evidence.Scope, evidence.Kind, evidence.Body)
		}
	}
	if len(fact.Unsettled.Trials) > 0 {
		rendered.WriteString("Earlier inconclusive trials:\n")
		for _, trial := range fact.Unsettled.Trials {
			fmt.Fprintf(&rendered, "- %s: %s\n", trial.NodeID, trial.Outcome)
		}
	}
	fmt.Fprintf(&rendered, "If this job settled the comparison, emit the winning standing lesson, fact, or actionable playbook method with replaces:%d. If it did not settle the comparison, emit kind unsettled with replaces:%d; the store will carry the exact pair forward and note this run. Do not leave the verdict implicit.]",
		fact.Seq, fact.Seq)
	return rendered.String()
}

func (r *Reconciler) recordTrialVerdict(node store.Node, trial store.Fact, verdict Learned) (store.Fact, bool, error) {
	if verdict.Kind == store.FactUnsettled {
		pair := trial.Unsettled.WithInconclusiveTrial(node.ID)
		fact, err := r.store.ReplaceUnsettledFactFrom(store.FactWriterTrial, trial.Seq, node.ID, trial.Scope, pair)
		return fact, false, err
	}
	if strings.TrimSpace(verdict.Body) == "" {
		return store.Fact{}, false, fmt.Errorf("record trial verdict: empty winner")
	}
	fact, err := r.store.ReplaceFactFrom(store.FactWriterTrial, trial.Seq, node.ID, verdict.Scope, verdict.Kind, clipFactBody(verdict.Body))
	return fact, err == nil, err
}

func (r *Reconciler) recordInconclusiveTrial(node store.Node, trial store.Fact) {
	pair := trial.Unsettled.WithInconclusiveTrial(node.ID)
	_, _ = r.store.ReplaceUnsettledFactFrom(store.FactWriterTrial, trial.Seq, node.ID, trial.Scope, pair)
}

func (r *Reconciler) recordLearnedFact(nodeID string, learned Learned) (store.Fact, error) {
	if learned.Kind == store.FactQuestion {
		return r.store.RecordQuestion(nodeID, learned.Scope, clipFactBody(learned.Body))
	}
	if learned.Kind == store.FactUnsettled {
		if learned.Unsettled == nil {
			return store.Fact{}, fmt.Errorf("record learned fact: unsettled fact has no pair")
		}
		return r.store.RecordUnsettledFactFrom(store.FactWriterDistiller, nodeID, learned.Scope, *learned.Unsettled)
	}
	return r.store.RecordFactFrom(store.FactWriterDistiller, nodeID, learned.Scope, learned.Kind, clipFactBody(learned.Body))
}

// renderCompileContext is the compiler's whole view for a caller with no
// conversation behind it — a charter firing speaks its own template.
func (r *Reconciler) renderCompileContext(snapshot store.Snapshot, instruction string) string {
	return r.renderCompileContextFor(snapshot, instruction, "")
}

// renderCompileContextFor is the compiler's whole view: the notebook first —
// durable facts the user should never have to repeat — then the measured
// policy, then the graph, and last the conversation the instruction came out
// of.
//
// The order is the cache's order. Everything above the thread is stable across
// a session, so it is written once and re-read from the prefix; the thread
// moves every turn and therefore goes last, where a change costs only itself.
func (r *Reconciler) renderCompileContextFor(snapshot store.Snapshot, instruction, sessionID string) string {
	var context strings.Builder
	if notebook := NotebookDigest(r.store, "", "", instruction, 12); notebook != "" {
		context.WriteString(notebook)
		context.WriteString("\n\n")
	}
	if hits, err := r.store.Recall(instruction, ExtractCues(instruction), 5); err == nil {
		if recalled := store.FormatRecall(hits, compileContextBytes); recalled != "" {
			context.WriteString(recalled)
			context.WriteString("\n\n")
		}
	}
	if guidance := r.store.CompilerAssumptionGuidance(); guidance != "" {
		context.WriteString("Measured assumption policy: ")
		context.WriteString(guidance)
		context.WriteString("\n\n")
	}
	if traits := r.store.MeasuredTraitBlock(compileTraitBytes); traits != "" {
		context.WriteString(traits)
		context.WriteString("\n\n")
	}
	context.WriteString(renderGraphContext(snapshot))
	if thread := r.recentThreadBlock(sessionID); thread != "" {
		context.WriteString("\n\n")
		context.WriteString(thread)
	}
	return context.String()
}

const (
	// compileThreadBytes bounds the conversation slice. It is small on purpose:
	// the instruction is the ask, and this is only enough of the turns around it
	// to say what the ask's words point at.
	compileThreadBytes = 1536
	// compileThreadMessages bounds the read behind that cap.
	compileThreadMessages = 12
	// compileThreadLineBytes bounds any single turn, so one pasted wall of text
	// cannot be the whole slice.
	compileThreadLineBytes = 400
	// compileThreadLabelBytes bounds the job name a thread line is attributed
	// to. It is a short title, and a title long enough to need this is a brief.
	compileThreadLabelBytes = 80
	// compileTraitBytes bounds the measured-traits block.
	compileTraitBytes = 320
)

// recentThreadBlock gives the compiler the conversation its instruction came
// out of. The instruction stays verbatim — that law is not negotiable — but a
// verbatim instruction is frequently not self-contained: "check my github
// account and find it" is a complete sentence whose object lives entirely in
// the turn before it. Without those turns the compiler could only ask what "it"
// was, and the answer to that question replaced the real ask with a lookup.
//
// It is a slice for resolving references, not a second instruction, and it says
// so where the model reads it.
func (r *Reconciler) recentThreadBlock(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if r.store == nil || sessionID == "" {
		return ""
	}
	messages, err := r.store.Messages(sessionID, 0, 0)
	if err != nil || len(messages) == 0 {
		return ""
	}
	if len(messages) > compileThreadMessages {
		messages = messages[len(messages)-compileThreadMessages:]
	}
	// Newest first into the budget, so the turn nearest the instruction is the
	// one that always survives; the block itself reads oldest first.
	lines := make([]string, 0, len(messages))
	used := 0
	for i := len(messages) - 1; i >= 0; i-- {
		body := strings.TrimSpace(messages[i].Body)
		if body == "" {
			continue
		}
		// A line spoken by a job says which job spoke it, by the short name the
		// user reads on screen. Four jobs narrating into one thread arrived here
		// in one undifferentiated voice, and the slice exists precisely to say
		// what the instruction's words point at.
		speaker := string(messages[i].Role)
		if label := r.jobLabelFor(messages[i].NodeID); label != "" {
			speaker += " [" + label + "]"
		}
		line := speaker + ": " +
			strings.ReplaceAll(clipBlock(body, compileThreadLineBytes), "\n", "\n  ")
		if used+len(line)+1 > compileThreadBytes {
			break
		}
		used += len(line) + 1
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	return "recent conversation in this session (oldest first) — use it only to resolve what the " +
		"instruction's words refer to; the instruction itself is the ask:\n" + strings.Join(lines, "\n")
}

// jobLabelFor names the job one thread line was spoken by, in the words the
// user already reads: the job's short title, never an id.
func (r *Reconciler) jobLabelFor(nodeID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if r.store == nil || nodeID == "" || nodeID == store.RootID {
		return ""
	}
	node, found, err := r.store.Node(nodeID)
	if err != nil || !found {
		return ""
	}
	root, found, err := r.store.Node(jobRootID(r.store, node))
	if err != nil || !found {
		root = node
	}
	if title := strings.TrimSpace(root.Title); title != "" {
		return clipLabel(firstLine(title), compileThreadLabelBytes)
	}
	if brief := firstLine(root.Brief); brief != "" {
		return clipLabel(brief, compileThreadLabelBytes)
	}
	return ""
}

// foldJob compacts a landed job in the active view: the subtree collapses to
// its root carrying a bounded digest and pointers to the files it left
// behind. This is the graph's context economy — the head, the compiler, and
// the rail all read the active view, and without folding every finished job
// would weigh on every future turn. The journal keeps every original node;
// folding loses nothing, it files it. A subtree with anything still open is
// left alone.
func (r *Reconciler) foldJob(node store.Node) {
	digest := node.Summary
	if strings.TrimSpace(digest) == "" {
		digest = node.Error
	}
	_ = r.store.Fold(node.ID, digest, filePointers(digest))
}

// settledFoldGrace is how long a landed job stays open before folding files it.
//
// Folding is context economy and it is worth having, but it is also what makes
// a job unaddressable: every surgery verb — correct it, try again, redirect,
// expedite, open the result — selects on `folded = 0`. Folding in the same tick
// that announced the deliverable meant the moment a user read an answer was the
// moment they could no longer act on it, so "actually, make it shorter" landed
// on nothing every time.
//
// The window is chosen against the human it exists for, not against the
// machine: it has to outlast reading a deliverable and typing a reaction, and
// it has to be short enough that the compile context never carries a working
// day of open jobs. Fifteen minutes is comfortably longer than the first, well
// inside the second, and it costs a settled subtree fifteen minutes of rows in
// the active view — which is nothing against the days of tenure folding exists
// to compact. Nothing is lost by waiting: the journal already has everything,
// and the fold is derived from state rather than from an event, so a restart
// mid-window folds the job on the next tick past the deadline rather than
// forgetting it.
const settledFoldGrace = 15 * time.Minute

// foldSettledJobs files every job whose grace window has closed. It is handed
// the active view rather than a per-tick memory precisely so that a process
// that died between the landing and the fold still folds the job: the condition
// is a property of the graph, not of this reconciler's lifetime.
func (r *Reconciler) foldSettledJobs(nodes []store.Node) error {
	cutoff := r.now().Add(-settledFoldGrace)
	// Ranged by index: a node is around half a kilobyte, the active view is
	// every live one of them, and the overwhelming majority of this loop's
	// iterations read three fields and move on.
	for i := range nodes {
		node := &nodes[i]
		if !foldableSettledJob(node) || node.FinishedAt.After(cutoff) {
			continue
		}
		if r.effectiveSessionID(*node) == "" &&
			!(node.Group == store.PracticeGroup && node.Provenance.Origin == store.OriginSelf) {
			continue
		}
		r.foldJob(*node)
	}
	return nil
}

// foldableSettledJob is the fold's admission test, stated once. A job root that
// has already folded is represented by its own fold root, which stays in the
// active view forever and must never be folded again.
func foldableSettledJob(node *store.Node) bool {
	if node.Parent != store.RootID || node.Folded || node.FoldRoot {
		return false
	}
	if store.IsOrganizationalGroup(node.Group) {
		return false
	}
	switch node.Status {
	case store.Done, store.Failed, store.Cancelled:
		return !node.FinishedAt.IsZero()
	default:
		return false
	}
}

// filePointers pulls the absolute paths a summary names, so a fold keeps
// durable references to the artifacts even after the working view compacts.
func filePointers(summary string) []string {
	var pointers []string
	for _, line := range strings.Split(summary, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/") && !strings.ContainsAny(line, " \t") {
			pointers = append(pointers, line)
		}
	}
	return pointers
}
