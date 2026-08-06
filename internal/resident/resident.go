// Package resident reconciles durable thread commands with the active task
// graph and reports graph outcomes back into their originating sessions.
package resident

import (
	"context"
	"errors"
	"fmt"
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

// Compiled is one instruction after assume-and-declare: the goal to act on,
// the defaults that were filled (each a revisable receipt), and the
// compiler's judgement of shape — "lookup", "task", or "project" — which the
// planner may use to decide how much structure the work deserves.
type Compiled struct {
	Goal        string
	Assumptions []string
	Scale       string
}

// CompileFunc turns a verbatim thread instruction into a goal the planner can
// act on. graphContext is a compact rendering of the active graph.
type CompileFunc func(ctx context.Context, instruction string, graphContext string) (Compiled, error)

// PlanFunc turns one compiled goal into an atomic subtree admission.
type PlanFunc func(ctx context.Context, compiled Compiled) (store.Subtree, error)

// Reconciler is the replaceable background half of the resident thread. The
// store remains the source of truth; this type keeps only a restart-safe event
// cursor and injected planning behavior in memory.
type Reconciler struct {
	store   *store.Store
	compile CompileFunc
	plan    PlanFunc

	mu                 sync.Mutex
	watcherInitialized bool
	lastEventSeq       int64
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
	return &Reconciler{store: graph, compile: compile, plan: plan}
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

	if r.store == nil {
		return errors.New("resident tick: nil store")
	}
	if err := r.initializeWatcher(); err != nil {
		return fmt.Errorf("resident tick: initialize watcher: %w", err)
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

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.announceTransitions(ctx); err != nil {
		return fmt.Errorf("resident tick: watch graph: %w", err)
	}
	return nil
}

type commandOutcome struct {
	status  store.CommandStatus
	result  string
	receipt string
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
	_, err = r.store.PostMessage(store.Message{
		SessionID:  command.SessionID,
		Role:       store.RoleSystem,
		Body:       boundMessage(outcome.receipt),
		CommandSeq: command.Seq,
	})
	return err
}

func (r *Reconciler) applyCommand(ctx context.Context, command store.Command) (commandOutcome, error) {
	switch command.Kind {
	case store.CommandSplice:
		return r.splice(ctx, command)
	case store.CommandCancel:
		return r.cancel(ctx, command)
	case store.CommandAmend:
		const reason = "amend is not implemented yet; cancel and re-ask, or splice an addition"
		return commandOutcome{
			status:  store.CommandRejected,
			result:  reason,
			receipt: reason,
		}, nil
	default:
		reason := fmt.Sprintf("command kind %q is not supported", command.Kind)
		return commandOutcome{
			status:  store.CommandRejected,
			result:  reason,
			receipt: reason,
		}, nil
	}
}

func (r *Reconciler) splice(ctx context.Context, command store.Command) (commandOutcome, error) {
	snapshot, err := r.store.ActiveSnapshot()
	if err != nil {
		return commandOutcome{}, fmt.Errorf("read active graph: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return commandOutcome{}, err
	}

	compiled, err := r.compile(ctx, command.Instruction, renderGraphContext(snapshot))
	if err != nil {
		return commandOutcome{}, fmt.Errorf("compile request: %w", err)
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
		subtree, err = r.plan(ctx, compiled)
		if err != nil {
			return commandOutcome{}, fmt.Errorf("plan request: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return commandOutcome{}, err
	}

	provenance := store.Provenance{
		Origin:    store.OriginUser,
		SessionID: command.SessionID,
		Intent:    command.Instruction,
	}
	if err := r.store.Splice(store.RootID, subtree, provenance); err != nil {
		if r.plan != nil || !r.defaultSpliceExists(command) {
			return commandOutcome{}, err
		}
	}

	return commandOutcome{
		status:  store.CommandApplied,
		result:  fmt.Sprintf("spliced %d nodes", len(subtree.Nodes)),
		receipt: compileReceipt(compiled.Goal, compiled.Assumptions),
	}, nil
}

func (r *Reconciler) defaultSpliceExists(command store.Command) bool {
	node, ok, err := r.store.Node(fmt.Sprintf("task-%d", command.Seq))
	if err != nil || !ok {
		return false
	}
	return node.Parent == store.RootID &&
		node.Provenance.Origin == store.OriginUser &&
		node.Provenance.SessionID == command.SessionID &&
		node.Provenance.Intent == command.Instruction
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

	owner := fmt.Sprintf("resident-cancel-%d", command.Seq)
	cancelled := 0
	for {
		progress := false
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
			claim, won, err := r.store.Claim(id, owner)
			if err != nil {
				return commandOutcome{}, err
			}
			if !won {
				continue
			}
			if err := r.store.Fail(claim, "cancelled by resident request"); err != nil {
				if errors.Is(err, store.ErrClaimLost) {
					continue
				}
				return commandOutcome{}, err
			}
			cancelled++
			progress = true
		}
		if !progress {
			break
		}
	}

	inFlight := 0
	for _, id := range targets {
		node, found, err := r.store.Node(id)
		if err != nil {
			return commandOutcome{}, err
		}
		if found && !terminal(node.Status) {
			inFlight++
		}
	}
	result := fmt.Sprintf("cancelled %d %s, %d in flight left to land",
		cancelled, plural(cancelled, "node", "nodes"), inFlight)
	return commandOutcome{
		status:  store.CommandApplied,
		result:  result,
		receipt: "I " + result + ".",
	}, nil
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
		for _, event := range events {
			if err := ctx.Err(); err != nil {
				return err
			}
			if event.Kind == store.EventNodeCompleted || event.Kind == store.EventNodeFailed {
				if err := r.announceNode(event); err != nil {
					return err
				}
			}
			r.lastEventSeq = event.Seq
		}
		if len(events) < eventBatchSize {
			return nil
		}
	}
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
	return err
}

func renderGraphContext(snapshot store.Snapshot) string {
	var context strings.Builder
	for _, node := range snapshot.Nodes {
		fmt.Fprintf(&context, "%s | %s | %s\n", node.ID, firstLine(node.Brief), node.Status)
	}
	return strings.TrimSuffix(context.String(), "\n")
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
