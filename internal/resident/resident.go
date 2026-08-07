// Package resident reconciles durable thread commands with the active task
// graph and reports graph outcomes back into their originating sessions.
package resident

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
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
// store remains the source of truth; this type keeps only a restart-safe event
// cursor and injected planning behavior in memory.
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
	sentinel        SentinelFunc
	composeBrief    BriefComposeFunc
	proposeCharters bool
	dailyBudgetUSD  float64
	practiceEnabled bool
	practiceBudget  float64
	practiceIdle    time.Duration
	services        *ServiceSupervisor

	mu                 sync.Mutex
	watcherInitialized bool
	lastEventSeq       int64
	progress           map[string]*subtreeProgress
	learningMoments    map[string]*pendingLearningMoment
	lastConsolidation  time.Time
	now                func() time.Time
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
		services: NewServiceSupervisor(graph)}
}

func (r *Reconciler) WithServiceRuntime(runtime ServiceRuntime) *Reconciler {
	if r.services == nil {
		r.services = NewServiceSupervisor(r.store)
	}
	r.services.WithRuntime(runtime)
	return r
}

// Serve polls until ctx is cancelled or the store can no longer be read or
// written. Strategy failures reject their command and do not stop the loop.
func (r *Reconciler) Serve(ctx context.Context) error {
	if err := r.Tick(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.Tick(ctx); err != nil {
				return err
			}
		}
	}
}

// Tick drains the current command queue and announces newly settled nodes.
// Calls are serialized so tests and embedding processes may invoke Tick
// without racing another Serve loop on the same reconciler.
func (r *Reconciler) Tick(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.learningMoments = make(map[string]*pendingLearningMoment)

	if r.store == nil {
		return errors.New("resident tick: nil store")
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
		for _, command := range commands {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := r.reconcileCommand(ctx, command); err != nil {
				return fmt.Errorf("resident tick: command %d: %w", command.Seq, err)
			}
		}
		if len(commands) < commandBatchSize {
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
	if _, err := r.watchOnceLocked(ctx); err != nil {
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
	if err := r.speakProgress(ctx); err != nil {
		return fmt.Errorf("resident tick: narrate progress: %w", err)
	}
	retrospectiveAfter := r.latestEventSeq()
	r.consolidateNotebook(ctx)
	r.reflectOnJobs(ctx)
	r.promoteRecurringSkills(ctx)
	r.flushLearningMoments()
	r.postRetrospectiveDigest(retrospectiveAfter)
	r.syncSkillBins()
	if err := r.practiceOnceLocked(ctx); err != nil {
		return fmt.Errorf("resident tick: practice loop: %w", err)
	}
	return nil
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

func (r *Reconciler) reconcileCommand(ctx context.Context, command store.Command) error {
	outcome, err := r.applyCommand(ctx, command)
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
		_, err := r.askQuestionLocked(store.AgentQuestion{
			SessionID: command.SessionID, Text: boundMessage(text),
			OriginCharterID:  questionCharterOrigin(outcome.options),
			OriginCommandSeq: command.Seq, Urgency: store.QuestionBlocking,
			Options: outcome.options, Category: outcome.category,
			DefaultAnswer: outcome.defaultAnswer,
		})
		return err
	}
	if strings.TrimSpace(outcome.receipt) == "" {
		return nil
	}
	role := store.RoleSystem
	if outcome.asAgent {
		role = store.RoleAgent
	}
	body := outcome.receipt
	if len(outcome.options) > 0 {
		// Selectable receipts carry the structured payload the TUI's question
		// components read; the durable option rows remain the continuation
		// and validation source.
		body = store.QuestionMessageBody(outcome.receipt, outcome.options)
	}
	_, err = r.store.PostMessage(store.Message{
		SessionID:  command.SessionID,
		Role:       role,
		Body:       boundMessage(body),
		NodeID:     commandReceiptNode(command),
		CommandSeq: command.Seq,
		Options:    outcome.options,
	})
	return err
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
	case store.CommandPause:
		return r.pause(command)
	case store.CommandResume:
		return r.resume(command)
	case store.CommandReprioritize:
		return r.reprioritize(command)
	case store.CommandRestart:
		return r.restart(command)
	case store.CommandServiceStop, store.CommandServiceRestart, store.CommandServiceAutoRestart:
		return r.applyServiceCommand(command)
	case store.CommandCharterRatify, store.CommandCharterPause, store.CommandCharterRetire,
		store.CommandCharterCadence, store.CommandCharterOnce, store.CommandCharterFire,
		store.CommandCharterDecline, store.CommandCharterAlways, store.CommandCharterNever, store.CommandCharterProbation:
		return r.applyCharterCommand(ctx, command)
	case store.CommandAmend:
		return r.amend(command)
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

	compileContext := r.renderCompileContext(snapshot, command.Instruction)
	compileContext += attachedDocumentCompileContext(command.Attachments)
	promotion, promoted, err := r.promotionSource(command)
	if err != nil {
		return commandOutcome{}, err
	}
	if promoted {
		compileContext += "\n\nPromoted reflex (reuse this partial; compile the SAME verbatim ask as normal work):\n" +
			"node: " + promotion.ID + "\nverbatim ask: " + promotion.Provenance.Intent +
			"\npartial result:\n" + clipBlock(promotion.Summary, store.MaxDigestBytes)
	}
	compiled, err := r.compile(ctx, command.Instruction, compileContext)
	if err != nil {
		return commandOutcome{}, fmt.Errorf("compile request: %w", err)
	}
	compiled.Goal = anchorAttachedDocuments(compiled.Goal, command.Attachments)
	if promoted {
		compiled.BuildsOn = append([]string{promotion.ID}, compiled.BuildsOn...)
	}
	if compiled.Charter != nil {
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

	if question := strings.TrimSpace(compiled.Question); question != "" {
		defaultAnswer := defaultQuestionAnswer(compiled.QuestionOptions)
		ask, _, gateErr := r.store.ShouldAsk(store.QuestionCategoryCompileAssumption)
		if gateErr == nil && !ask && defaultAnswer != "" {
			assumedContext := compileContext + "\n\nEmpirical ask policy: assume and declare this answered default:\n" +
				question + "\nDefault answer: " + defaultAnswer
			if assumed, compileErr := r.compile(ctx, command.Instruction, assumedContext); compileErr == nil &&
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
	if r.plan == nil {
		subtree = store.Subtree{Nodes: []store.NodeSpec{{
			ID:    fmt.Sprintf("task-%d", command.Seq),
			Brief: compiled.Goal,
			Stage: 1,
		}}}
	} else {
		planCtx := withPlanAnchor(ctx, PlanAnchor{
			NodeID:    fmt.Sprintf("task-%d", command.Seq),
			SessionID: command.SessionID, CommandSeq: command.Seq,
		})
		subtree, err = r.plan(planCtx, compiled)
		if err != nil {
			return commandOutcome{}, fmt.Errorf("plan request: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return commandOutcome{}, err
	}
	subtree = r.wireContinuity(subtree, compiled.BuildsOn)
	r.titleSubtree(ctx, &subtree, compiled)

	provenance := store.Provenance{
		Origin:        store.OriginUser,
		SessionID:     command.SessionID,
		Intent:        command.Instruction,
		TrialOf:       compiled.TrialOf,
		ServiceIntent: compiled.ServiceIntent,
		Attachments:   append([]string(nil), command.Attachments...),
	}
	if err := r.store.Splice(store.RootID, subtree, provenance); err != nil {
		if r.plan != nil || !r.defaultSpliceExists(command, compiled) {
			return commandOutcome{}, err
		}
	}

	receipt := compileReceipt(compiled.Goal, compiled.Assumptions)
	if promoted {
		receipt = reflexPromotionLine
	}
	return commandOutcome{
		status:  store.CommandApplied,
		result:  fmt.Sprintf("spliced %d nodes", len(subtree.Nodes)),
		receipt: receipt,
	}, nil
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

// titleSubtree names the job's root node — the line the rail shows for the
// whole job. Planned leaves keep the planner's own short titles; the root is
// the one node whose title would otherwise be a generic "Synthesis" or the
// full compiled goal. Best effort by design: a titling failure costs a long
// label, never the job.
func (r *Reconciler) titleSubtree(ctx context.Context, subtree *store.Subtree, compiled Compiled) {
	if r.title == nil {
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
		short, err := r.title(ctx, compiled.Goal)
		short = strings.TrimSpace(short)
		if err != nil || short == "" {
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
			if err := r.store.CancelPending(id, "cancelled by user"); err != nil {
				return commandOutcome{}, err
			}
			cancelled++
		case store.Claimed, store.Running:
			if err := r.store.RequestNodeCancel(id, "cancelled by user"); err != nil {
				return commandOutcome{}, err
			}
			requested++
		}
	}
	result := fmt.Sprintf("cancelled %d; requested cooperative cancellation for %d", cancelled, requested)
	receipt := fmt.Sprintf("cancelled — %d %s cancelled", cancelled, plural(cancelled, "leaf", "leaves"))
	if requested > 0 {
		receipt = fmt.Sprintf("cancellation requested — %d running %s will release at the next boundary",
			requested, plural(requested, "leaf", "leaves"))
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
		store.CommandReprioritize, store.CommandRestart:
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

func (r *Reconciler) initializeWatcher() error {
	if r.watcherInitialized {
		return nil
	}
	events, err := r.store.Events(0, 0)
	if err != nil {
		return err
	}
	if len(events) != 0 {
		r.lastEventSeq = events[len(events)-1].Seq
	}
	r.watcherInitialized = true
	return nil
}

func (r *Reconciler) announceTransitions(ctx context.Context) error {
	for {
		events, err := r.store.Events(r.lastEventSeq, eventBatchSize)
		if err != nil {
			return err
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
			switch event.Kind {
			case store.EventNodeCompleted, store.EventNodeFailed:
				node, ok, err := r.store.Node(event.NodeID)
				if err != nil {
					return err
				}
				if event.Kind == store.EventNodeCompleted && ok {
					continuing, err := r.continuingNode(node)
					if err != nil {
						return err
					}
					if continuing {
						break
					}
				}
				if err := r.announceNode(event); err != nil {
					return err
				}
				r.recordForNarration(byID, event)
				practice := ok && node.Group == store.PracticeGroup &&
					node.Provenance.Origin == store.OriginSelf
				if ok && (node.Provenance.SessionID != "" || practice) {
					if event.Kind == store.EventNodeFailed {
						r.distillJob(ctx, node, true)
					} else if node.Parent == store.RootID && !r.reflexPromoted(node) {
						r.distillJob(ctx, node, false)
					}
					if node.Parent == store.RootID {
						r.foldJob(node)
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

func (r *Reconciler) continuingNode(node store.Node) (bool, error) {
	if strings.Contains(node.Summary, "[splitting the remaining work --") {
		return true, nil
	}
	return r.store.OverrunDeferred(node.ID)
}

func (r *Reconciler) announceNode(event store.Event) error {
	node, ok, err := r.store.Node(event.NodeID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("event %d names missing node %q", event.Seq, event.NodeID)
	}
	if node.Provenance.SessionID == "" {
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
		failure := firstLine(node.Error)
		if failure == "" {
			failure = "no reason was recorded"
		}
		body = fmt.Sprintf("I hit a problem with %q: %s", clipLabel(firstLine(node.Brief), 60), failure)
	default:
		return nil
	}

	_, err = r.store.PostMessage(store.Message{
		SessionID: node.Provenance.SessionID,
		Role:      store.RoleSystem,
		Body:      boundMessage(body),
		NodeID:    node.ID,
	})
	if err != nil {
		return err
	}
	return r.surfaceNaturalQuestionLocked(node.Provenance.SessionID)
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

func renderGraphContext(snapshot store.Snapshot) string {
	var context strings.Builder

	// Jobs first, newest first: "improve it" almost always reaches for the
	// most recent thing, and a job's summary carries the artifact paths the
	// next job starts from.
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
		if summary := strings.TrimSpace(node.Summary); summary != "" {
			fmt.Fprintf(&context, "  result: %s\n", clipBlock(summary, 600))
		}
	}

	context.WriteString("\nnodes:\n")
	for _, node := range snapshot.Nodes {
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

func compileReceipt(goal string, assumptions []string) string {
	var receipt strings.Builder
	fmt.Fprintf(&receipt, "Here's my reading: %s", strings.TrimSpace(goal))
	for _, assumption := range assumptions {
		if assumption = strings.TrimSpace(assumption); assumption != "" {
			fmt.Fprintf(&receipt, "\nAssumed: %s", assumption)
		}
	}
	receipt.WriteString("\nCorrect me anytime — redirects are cheap.")
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
		if !strings.HasPrefix(edge.To, prefix) || strings.HasPrefix(edge.From, prefix) || seen[edge.From] {
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
	if gate, ok, err := r.store.DeliveryGateFor(node.ID); err == nil && ok && !gate.Pass {
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
	facts, err := r.distill(ctx, node.Provenance.Intent, outcome, failed)
	if err != nil {
		if isTrial {
			r.recordInconclusiveTrial(node, trial)
		}
		return
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

// renderCompileContext is the compiler's whole view: the notebook first —
// durable facts the user should never have to repeat — then the graph.
func (r *Reconciler) renderCompileContext(snapshot store.Snapshot, instruction string) string {
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
	context.WriteString(renderGraphContext(snapshot))
	return context.String()
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
