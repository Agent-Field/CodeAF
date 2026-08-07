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
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// ServiceConsentGrace is the bounded hold after a leaf asks to keep an
	// otherwise unconsented process. Silence always lands on the stop default.
	ServiceConsentGrace = 30 * time.Second
	serviceConsentPoll  = 100 * time.Millisecond
)

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
	}
}

// WithServiceConsentGrace is primarily a deterministic test seam; production
// uses the named bounded default above.
func (r *Runner) WithServiceConsentGrace(grace time.Duration) *Runner {
	if grace > 0 {
		r.serviceConsentGrace = grace
	}
	return r
}

// WithDailyBudgetUSD installs the policy rail checked immediately before each
// claim. Zero is unlimited and preserves the old scheduling path.
func (r *Runner) WithDailyBudgetUSD(amount float64) *Runner {
	r.dailyBudgetUSD = amount
	return r
}

// Serve polls for ready work until ctx ends, then waits for in-flight nodes
// to land. Landing is bounded by each execution's own respect for ctx.
func (r *Runner) Serve(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			if _, err := r.Tick(ctx); err != nil {
				return err
			}
		}
	}
}

// Tick claims as many ready nodes as free slots allow and dispatches them.
// It returns how many nodes were dispatched; store errors stop the runner,
// execution errors do not — they land on the node as a recorded failure.
func (r *Runner) Tick(ctx context.Context) (int, error) {
	if err := r.preemptPracticeForUserWork(); err != nil {
		return 0, err
	}
	dispatched := 0
	for {
		select {
		case r.slots <- struct{}{}:
		default:
			return dispatched, nil
		}
		node, ok, err := r.claimNext()
		if err != nil {
			<-r.slots
			return dispatched, err
		}
		if !ok {
			<-r.slots
			return dispatched, nil
		}
		dispatched++
		runCtx := ctx
		var cancel context.CancelFunc
		if node.Group == store.PracticeGroup {
			runCtx, cancel = context.WithCancel(ctx)
			r.activeMu.Lock()
			r.activePractice[node.ID] = cancel
			r.activeMu.Unlock()
		}
		r.wg.Add(1)
		go func(node store.Node, runCtx context.Context, cancel context.CancelFunc) {
			defer r.wg.Done()
			defer func() { <-r.slots }()
			if cancel != nil {
				defer cancel()
				defer func() {
					r.activeMu.Lock()
					delete(r.activePractice, node.ID)
					r.activeMu.Unlock()
				}()
			}
			r.runOne(runCtx, node)
		}(node, runCtx, cancel)
	}
}

// Wait blocks until every dispatched node has landed. Tests use it to make
// Tick deterministic.
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) claimNext() (store.Node, bool, error) {
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
	open, err := openChildren(r.graph)
	if err != nil {
		return store.Node{}, false, err
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
		if open[node.ID] {
			continue
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

func (r *Runner) runOne(ctx context.Context, node store.Node) {
	claim := store.Claim{ID: node.ID, Owner: node.Owner, Token: node.ClaimToken}
	result, err := r.execute(ctx, node)
	control, controlErr := r.graph.Control(node.ID)
	if controlErr == nil && (control.CancelRequested || control.Held) {
		stopServiceRequests(result.ServiceRequests)
		// Spend precedes settlement even on a user-directed boundary. Release is
		// the CAS transition that invalidates this worker's authority; a cancel
		// then uses the ordinary pending cancellation event.
		_ = r.graph.RecordUsage(store.NodeUsage{
			NodeID: node.ID, PromptTokens: result.PromptTokens,
			CompletionTokens: result.CompletionTokens, Cost: result.Cost,
		})
		if releaseErr := r.graph.Release(claim); releaseErr != nil {
			return
		}
		if control.CancelRequested {
			_ = r.graph.CancelPending(node.ID, "cancelled by user")
		}
		return
	}
	if err != nil {
		stopServiceRequests(result.ServiceRequests)
		if node.Group == store.PracticeGroup && errors.Is(ctx.Err(), context.Canceled) {
			_ = r.graph.Release(claim)
			return
		}
		_ = r.graph.Fail(claim, err.Error())
		return
	}
	result.Summary = r.applyServiceRequests(ctx, node, result.Summary, result.ServiceRequests)
	// Spend is recorded before completion settles: a refused completion is
	// still money spent, and the journal should say so.
	_ = r.graph.RecordUsage(store.NodeUsage{
		NodeID:           node.ID,
		PromptTokens:     result.PromptTokens,
		CompletionTokens: result.CompletionTokens,
		Cost:             result.Cost,
	})
	summary := result.Summary
	if strings.TrimSpace(summary) == "" {
		summary = "finished with no summary"
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
