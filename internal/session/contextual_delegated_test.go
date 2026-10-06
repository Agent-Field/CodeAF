package session

// ADVERSARIAL COVERAGE for delegated continuity and truthful forgetting. Every
// test drives the REAL seam: a task worker built by [Agent.newTaskAgent] on an
// admitted node, the [Agent.executeTool] -> [Agent.recordOutcome] boundary, the
// registry's own [jobRegistry.settleExit], and the user-level Forget path.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/store"
)

// spawnTaskWorker builds a worker the way the runner does — an admitted node
// and [Agent.newTaskAgent] — so its frozen origin is captured through the real
// spawn rather than set by hand.
func spawnTaskWorker(t *testing.T, root *Agent, dir string) *Agent {
	t.Helper()
	graph := stubbedGraph(root, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "read the contract", brief: "b", acceptance: "a", depth: 1})
	worker, err := root.newTaskAgent(context.Background(), dir, graph.node(id), "")
	if err != nil {
		t.Fatalf("newTaskAgent: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	if worker.remembers() {
		t.Fatal("a task worker gained a brain")
	}
	return worker
}

func frozenRootTurn(t *testing.T, root *Agent) (string, string) {
	t.Helper()
	root.memory.mu.Lock()
	defer root.memory.mu.Unlock()
	return root.memory.outcomeTurnID, root.memory.outcomeGoal
}

func delegatedReadCall(t *testing.T, path, id string) ai.ToolCall {
	t.Helper()
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	call := ai.ToolCall{ID: id}
	call.Function.Name = "read"
	call.Function.Arguments = string(args)
	return call
}

func delegatedBashCall(id, command string) ai.ToolCall {
	call := ai.ToolCall{ID: id}
	call.Function.Name = "bash"
	body, _ := json.Marshal(map[string]string{"command": command})
	call.Function.Arguments = string(body)
	return call
}

func attemptsFor(t *testing.T, brain *store.Store, owner, project string) []store.ContextualAttempt {
	t.Helper()
	rows, err := brain.ContextualAttemptsApplicable(owner, map[string]string{"project": project}, time.Now(), store.ContextualAttemptLimit)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func attemptsForProject(t *testing.T, brain *store.Store, root *Agent) []store.ContextualAttempt {
	t.Helper()
	return attemptsFor(t, brain, store.OwnerProject(root.config.MemoryProjectKey), root.config.MemoryProjectKey)
}

// ── 1: a worker's SUCCESSFUL trusted full read reaches dependency observation,
// carrying the frozen root origin, and proves an exact producer/consumer link. ──

func TestDelegatedWorkerForwardedReadsReachDependencyObservation(t *testing.T) {
	root, brain, producer, consumer := contextualReviewObservedFixture(t)
	root.prepareBindingContext(context.Background(), "wire the producer library")
	turn, goal := frozenRootTurn(t, root)
	if turn == "" || goal == "" {
		t.Fatalf("root turn circumstances were not frozen: turn=%q goal=%q", turn, goal)
	}
	worker := spawnTaskWorker(t, root, filepath.Dir(producer))
	if worker.origin.Session != root.memorySourceSession() || worker.origin.Turn != turn || worker.origin.Goal != goal || worker.origin.Task == "" {
		t.Fatalf("worker origin was not frozen at launch: %+v (root turn %q)", worker.origin, turn)
	}
	producerBody, err := os.ReadFile(producer)
	if err != nil {
		t.Fatal(err)
	}
	consumerBody, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, one := range []struct {
		path, id string
		body     string
	}{{producer, "p-read", string(producerBody)}, {consumer, "c-read", string(consumerBody)}} {
		call := delegatedReadCall(t, one.path, one.id)
		worker.recordOutcome(ctx, 0, call, toolResult{text: one.body}, worker.captureSourceSnapshot(ctx).Identity)
	}
	links, err := brain.DependenciesForProducer(contextualPathOwner(producer), 8)
	if err != nil || len(links) != 1 || links[0].ProducerPath != producer || links[0].ConsumerPath != consumer {
		t.Fatalf("delegated full reads did not form the exact observed edge: links=%+v err=%v", links, err)
	}
	// A SUCCESSFUL READ IS NOT A FAILURE ATTEMPT: nothing was written as an
	// observed outcome.
	if rows := attemptsForProject(t, brain, root); len(rows) != 0 {
		t.Fatalf("a successful read wrote attempt rows: %+v", rows)
	}
}

// A late worker receipt keeps the turn and goal it was launched under, and does
// not attach itself to the user turn that is live when it lands.
func TestDelegatedWorkerLateReceiptKeepsFrozenOrigin(t *testing.T) {
	root, brain, producer, _ := contextualReviewObservedFixture(t)
	root.prepareBindingContext(context.Background(), "fix the foobar parser")
	turn, goal := frozenRootTurn(t, root)
	worker := spawnTaskWorker(t, root, filepath.Dir(producer))
	// The person moves on to an unrelated question while the worker runs.
	root.prepareBindingContext(context.Background(), "what is the weather in oslo")
	ctx := context.Background()
	call := delegatedBashCall("late-1", "go test ./foobar")
	worker.recordOutcome(ctx, 0, call, toolResult{text: "undefined: foobar.Token", isError: true}, worker.captureSourceSnapshot(ctx).Identity)
	rows := attemptsForProject(t, brain, root)
	if len(rows) != 1 {
		t.Fatalf("late delegated failure rows=%+v", rows)
	}
	if rows[0].TurnID != turn || rows[0].Goal != goal {
		t.Fatalf("late receipt attached to the wrong turn: %+v (want turn %q goal %q)", rows[0], turn, goal)
	}
	if strings.Contains(rows[0].Goal, "weather") {
		t.Fatalf("late receipt claimed the next user turn: %q", rows[0].Goal)
	}
}

// ── 2: the idempotency hint is bounded and evictable, seen is marked only after
// a successful append, the canonical journal is the real dedup, the per-origin
// bound preserves fresh failures, and a closed collector drops late receipts. ──

func TestDelegatedCollectorHintIsBoundedEvictableAndBackedByCanonicalDedup(t *testing.T) {
	root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "p" })
	c := newOutcomeCollector(root)
	for i := 0; i < outcomeCollectorMax; i++ {
		c.remember(fmt.Sprintf("k%d", i))
	}
	c.remember("fresh")
	c.mu.Lock()
	_, freshSeen := c.hint["fresh"]
	_, oldestSeen := c.hint["k0"]
	size := len(c.hint)
	c.mu.Unlock()
	if !freshSeen || oldestSeen || size > outcomeCollectorMax {
		t.Fatalf("hint is not bounded/evictable: fresh=%v oldest=%v size=%d", freshSeen, oldestSeen, size)
	}
	// THE CANONICAL JOURNAL IS WHAT MAKES A RE-SEND IDEMPOTENT — even after the
	// hint forgot it. The same source key and hash returns the row already held.
	e := store.ContextualAttempt{ID: store.NewMemoryID(), Owner: store.OwnerProject("p"), SessionID: "s", TurnID: "t", Tool: "bash", Action: "bash: x", Status: store.AttemptFailed, Observation: "boom", ReceiptIDs: []string{"c"}, SourceKey: "src:1", SourceHash: "h1", Conditions: map[string]string{"project": "p"}, ValidFrom: time.Now()}
	first, err := brain.AppendContextualAttempt(e)
	if err != nil {
		t.Fatal(err)
	}
	e.ID = store.NewMemoryID()
	second, err := brain.AppendContextualAttempt(e)
	if err != nil || second.Seq != first.Seq {
		t.Fatalf("a re-sent source was not idempotent: first=%d second=%d err=%v", first.Seq, second.Seq, err)
	}
	if rows := attemptsFor(t, brain, store.OwnerProject("p"), "p"); len(rows) != 1 {
		t.Fatalf("canonical dedup made %d rows", len(rows))
	}
}

func TestDelegatedCollectorFailedAppendIsRetryableAndVisible(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	root, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "p" })
	worker, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	worker.outcomes = root.outcomes
	worker.origin = delegatedOrigin{Session: root.memorySourceSession(), Owner: store.OwnerProject("p"), Turn: "turn:1", Goal: "fix the foobar parser", Task: "task:1"}
	// A store that cannot take the write: the append fails, and that must be
	// VISIBLE and RETRYABLE rather than marked seen.
	if err := brain.Close(); err != nil {
		t.Fatal(err)
	}
	call := delegatedBashCall("w-fail", "go test ./foobar")
	worker.recordOutcome(context.Background(), 1, call, toolResult{text: "undefined: y", isError: true}, "unknown")
	failures, lastErr := root.outcomes.errorState()
	if failures != 1 || lastErr == nil {
		t.Fatalf("a failed delegated append was not exposed: failures=%d err=%v", failures, lastErr)
	}
	key := delegatedSourceKey(worker.origin, call.ID)
	root.outcomes.mu.Lock()
	_, seen := root.outcomes.hint[key]
	root.outcomes.mu.Unlock()
	if seen {
		t.Fatal("a failed append was marked seen and can never be retried")
	}
}

func TestDelegatedEmissionBoundPreservesFreshFailures(t *testing.T) {
	root, _ := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "p" })
	c := newOutcomeCollector(root)
	originKey := "s\x00turn\x00task:1"
	for i := 0; i < delegatedAttemptEmissionMax; i++ {
		if !c.admitAttempt(originKey, store.AttemptUnknown, fmt.Sprintf("u%d", i), fmt.Sprintf("r%d", i)) {
			t.Fatalf("blocked attempt %d was refused before the bound", i)
		}
		c.inflight.Done()
	}
	if c.admitAttempt(originKey, store.AttemptUnknown, "u-more", "r-more") {
		t.Fatal("the per-origin bound did not bound blocked attempts")
	}
	// A FRESH DEMONSTRATED FAILURE IS NEVER DROPPED BY THE BOUND.
	if !c.admitAttempt(originKey, store.AttemptFailed, "f-fresh", "r-fresh") {
		t.Fatal("the per-origin bound dropped a fresh failure")
	}
	c.inflight.Done()
	// A RECORDED FACT MARKS ITS REPEAT; a redundant repeat in the same origin is
	// then refused (it is the same fact), while a different fact still gets
	// through.
	c.rememberRepeat(originKey, "r-fresh")
	if c.admitAttempt(originKey, store.AttemptFailed, "f-repeat", "r-fresh") {
		t.Fatal("a redundant repeat in one origin was admitted")
	}
	if !c.admitAttempt(originKey, store.AttemptFailed, "f-other", "r-other") {
		t.Fatal("a genuinely different failure in the same origin was refused")
	}
	c.inflight.Done()
}

func TestClosedCollectorDropsLateWorkerReceipt(t *testing.T) {
	root, brain, producer, _ := contextualReviewObservedFixture(t)
	root.prepareBindingContext(context.Background(), "wire the producer")
	worker := spawnTaskWorker(t, root, filepath.Dir(producer))
	root.outcomes.close()
	call := delegatedBashCall("closed-1", "go test ./foobar")
	worker.recordOutcome(context.Background(), 1, call, toolResult{text: "undefined: y", isError: true}, "unknown")
	if rows := attemptsForProject(t, brain, root); len(rows) != 0 {
		t.Fatalf("a closed collector accepted a late receipt: %+v", rows)
	}
}

// ── 3: the promoted-job settle pipeline, with frozen provenance, cancellation,
// closed roots, fresh agents/ids and a forgotten old source. ──

func settleJobFixture(t *testing.T) (*Agent, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	initRepo(t, dir)
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.Workspace = dir; c.MemoryProjectKey = "p" })
	// The roster is not what this test is about; leaving it off keeps the
	// settle path under test free of the naming errand.
	a.jobs.announce = nil
	return a, brain, dir
}

func settleJob(t *testing.T, a *Agent, command string, code int, kill bool) *job {
	t.Helper()
	one, err := a.jobs.newJob(command, jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	if kill && !one.requestKill() {
		t.Fatalf("could not request a kill for job %d", one.id)
	}
	a.jobs.settleExit(one, code)
	return one
}

func TestPromotedJobFailureUsesFrozenOriginAndDistinguishesCancellation(t *testing.T) {
	a, brain, _ := settleJobFixture(t)
	a.prepareBindingContext(context.Background(), "run the foobar suite")
	turn, goal := frozenRootTurn(t, a)
	one := settleJob(t, a, "go test ./foobar", 3, false)
	rows := attemptsFor(t, brain, store.OwnerProject("p"), "p")
	if len(rows) != 1 {
		t.Fatalf("promoted job failure rows=%+v", rows)
	}
	want := "job:" + a.memorySourceSession() + ":" + fmt.Sprint(one.id)
	if rows[0].SourceKey != want || rows[0].TurnID != turn || rows[0].Goal != goal || rows[0].Status != store.AttemptFailed {
		t.Fatalf("settled job provenance wrong: %+v (want key %q turn %q)", rows[0], want, turn)
	}
	// A CANCELLED DEATH IS NOT AN EXECUTION FAILURE.
	settleJob(t, a, "go test ./other", 137, true)
	if rows = attemptsFor(t, brain, store.OwnerProject("p"), "p"); len(rows) != 1 {
		t.Fatalf("a cancelled job was journaled as a failure: %+v", rows)
	}
	// A clean exit has nothing to warn about either.
	settleJob(t, a, "go test ./clean", 0, false)
	if rows = attemptsFor(t, brain, store.OwnerProject("p"), "p"); len(rows) != 1 {
		t.Fatalf("a clean job exit wrote a warning: %+v", rows)
	}
}

func TestPromotedJobRespectsClosedCollectorAndFreshIdentity(t *testing.T) {
	a, brain, _ := settleJobFixture(t)
	a.prepareBindingContext(context.Background(), "run the foobar suite")
	// A CLOSED ROOT COLLECTOR REFUSES THE LATE SETTLEMENT.
	a.outcomes.close()
	settleJob(t, a, "go test ./foobar", 3, false)
	if rows := attemptsFor(t, brain, store.OwnerProject("p"), "p"); len(rows) != 0 {
		t.Fatalf("a closed root collector accepted a settled job: %+v", rows)
	}
	// A FRESH AGENT HAS ITS OWN SESSION IDENTITY, so its job 1 is a different
	// source even though job ids restart per Agent.
	b, brainB, _ := settleJobFixture(t)
	b.prepareBindingContext(context.Background(), "run the foobar suite")
	settleJob(t, b, "go test ./foobar", 3, false)
	rowsB := attemptsFor(t, brainB, store.OwnerProject("p"), "p")
	if len(rowsB) != 1 {
		t.Fatalf("fresh agent job failure rows=%+v", rowsB)
	}
	if !strings.HasPrefix(rowsB[0].SourceKey, "job:"+b.memorySourceSession()+":") {
		t.Fatalf("promoted job key lacks the launching session: %q", rowsB[0].SourceKey)
	}
}

func TestForgottenJobSourceDoesNotSuppressNewIndependentFailure(t *testing.T) {
	a, brain, _ := settleJobFixture(t)
	a.prepareBindingContext(context.Background(), "run the foobar suite")
	first := settleJob(t, a, "go test ./foobar", 3, false)
	rows := attemptsFor(t, brain, store.OwnerProject("p"), "p")
	if len(rows) != 1 {
		t.Fatalf("rows=%+v", rows)
	}
	oldKey, oldHash := rows[0].SourceKey, rows[0].SourceHash
	if err := brain.SuppressContextualSource(rows[0].Owner, oldKey, oldHash, "explicit forget"); err != nil {
		t.Fatal(err)
	}
	if rows = attemptsFor(t, brain, store.OwnerProject("p"), "p"); len(rows) != 0 {
		t.Fatalf("a suppressed source stayed readable: %+v", rows)
	}
	// The SAME source re-sent is refused; a NEW independent failure with a new
	// job id in the same session is learned.
	resend := store.ContextualAttempt{ID: store.NewMemoryID(), Owner: store.OwnerProject("p"), SessionID: a.memorySourceSession(), TurnID: "t", Tool: "bash", Action: "bash: go test ./foobar", Status: store.AttemptFailed, Observation: "boom", ReceiptIDs: []string{oldKey}, SourceKey: oldKey, SourceHash: oldHash, ValidFrom: time.Now()}
	if _, err := brain.AppendContextualAttempt(resend); err == nil {
		t.Fatal("a forgotten source was re-learned")
	}
	second := settleJob(t, a, "go test ./second", 3, false)
	wantKey := "job:" + a.memorySourceSession() + ":" + fmt.Sprint(second.id)
	rows = attemptsFor(t, brain, store.OwnerProject("p"), "p")
	if len(rows) != 1 || rows[0].SourceKey != wantKey {
		t.Fatalf("a new independent failure was suppressed by an old forget: rows=%+v want %q (first %d)", rows, wantKey, first.id)
	}
}

// ── 4: forget is truthful about partial completion and exact in its scope. ──

func TestForgetReportsPartialCompletionTruthfully(t *testing.T) {
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	seedAttempt(t, brain, owner, "fix the foobar parser", "bash: go test ./foobar", "undefined: foobar.Token", store.AttemptFailed, "turn:1:call1", "h1", "unknown")
	// An attempt whose source key is empty cannot be suppressed. The store
	// accepts the row, so the forget must report the failure rather than
	// claiming a complete success.
	seedAttempt(t, brain, owner, "fix the foobar parser again", "bash: go test ./foobar", "undefined: foobar.Token", store.AttemptFailed, "", "h2", "unknown")
	title, err := a.Forget("fix the foobar parser")
	if err == nil {
		t.Fatalf("forget claimed success while an attempt could not be retired (title %q)", title)
	}
	if title == "" {
		t.Fatal("a partial forget named nothing it did retire")
	}
	if !strings.Contains(err.Error(), "could not retire") {
		t.Fatalf("partial forget error is not truthful: %v", err)
	}
	// The retirable attempt went; the unsuppressible one remains visible.
	rows := attemptsFor(t, brain, owner, "p")
	if len(rows) != 1 || rows[0].SourceKey != "" {
		t.Fatalf("partial forget retired the wrong rows: %+v", rows)
	}
	// A NEW INDEPENDENTLY OBSERVED ATTEMPT IS STILL ALLOWED: no owner mute.
	fresh := store.ContextualAttempt{ID: store.NewMemoryID(), Owner: owner, SessionID: "s", TurnID: "turn:9", Tool: "bash", Action: "bash: go test ./foobar", Goal: "fix the foobar parser", Status: store.AttemptFailed, Observation: "undefined: foobar.Token", ReceiptIDs: []string{"call9"}, SourceKey: "turn:9:call9", SourceHash: "h9", Conditions: map[string]string{"project": "p"}, ValidFrom: time.Now()}
	if _, err := brain.AppendContextualAttempt(fresh); err != nil {
		t.Fatalf("a fresh attempt was refused after a partial forget: %v", err)
	}
}

func TestForgetAttemptsSurfacesReadErrors(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	a, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "p" })
	owner := store.OwnerProject("p")
	seedAttempt(t, brain, owner, "fix the foobar parser", "bash: go test ./foobar", "undefined: foobar.Token", store.AttemptFailed, "turn:1:call1", "h1", "unknown")
	if err := brain.Close(); err != nil {
		t.Fatal(err)
	}
	retired, _, err := a.forgetAttempts("fix the foobar parser")
	if err == nil {
		t.Fatalf("a failed attempt read was swallowed: retired=%d", retired)
	}
}

// A CONCURRENT/LATE RECEIPT LIFECYCLE: one worker sending the SAME call twice
// at once writes exactly one journal row — the hint where it holds, the
// canonical source dedup where it does not — and two workers reading the same
// exact pair form exactly one observed edge.
func TestDelegatedConcurrentDuplicateReceiptsWriteOnce(t *testing.T) {
	root, brain, producer, consumer := contextualReviewObservedFixture(t)
	root.prepareBindingContext(context.Background(), "wire the producer library")
	first := spawnTaskWorker(t, root, filepath.Dir(producer))
	second := spawnTaskWorker(t, root, filepath.Dir(producer))
	ctx := context.Background()
	failure := delegatedBashCall("dup-fail", "go test ./foobar")
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			first.recordOutcome(ctx, 0, failure, toolResult{text: "undefined: foobar.Token", isError: true}, "unknown")
		}()
	}
	wg.Wait()
	if rows := attemptsForProject(t, brain, root); len(rows) != 1 {
		t.Fatalf("concurrent duplicate receipts wrote %d rows: %+v", len(rows), rows)
	}
	if failures, lastErr := root.outcomes.errorState(); failures != 0 {
		t.Fatalf("a duplicate receipt was reported as a failed write: failures=%d err=%v", failures, lastErr)
	}
	// Two concurrent reads of the same exact pair still form exactly one edge.
	producerBody, err := os.ReadFile(producer)
	if err != nil {
		t.Fatal(err)
	}
	consumerBody, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatal(err)
	}
	for _, worker := range []*Agent{first, second} {
		wg.Add(1)
		go func(w *Agent) {
			defer wg.Done()
			w.recordOutcome(ctx, 0, delegatedReadCall(t, producer, "p-race"), toolResult{text: string(producerBody)}, "unknown")
			w.recordOutcome(ctx, 0, delegatedReadCall(t, consumer, "c-race"), toolResult{text: string(consumerBody)}, "unknown")
		}(worker)
	}
	wg.Wait()
	links, err := brain.DependenciesForProducer(contextualPathOwner(producer), 8)
	if err != nil || len(links) != 1 {
		t.Fatalf("concurrent reads formed %d edges: %+v err=%v", len(links), links, err)
	}
}

// A worker with no immutable origin is REFUSED rather than silently
// reattributed to the turn that happens to be live when its receipt lands.
func TestDelegatedWorkerWithoutFrozenOriginIsRefused(t *testing.T) {
	root, brain, producer, _ := contextualReviewObservedFixture(t)
	// Spawned before the root has any turn binding: the worst case for a late
	// arrival.
	worker := spawnTaskWorker(t, root, filepath.Dir(producer))
	if worker.origin.Turn != "" {
		t.Fatalf("a worker was spawned with an invented turn: %+v", worker.origin)
	}
	root.prepareBindingContext(context.Background(), "a question asked later")
	call := delegatedBashCall("unfrozen-1", "go test ./foobar")
	worker.recordOutcome(context.Background(), 0, call, toolResult{text: "undefined: foobar.Token", isError: true}, "unknown")
	if rows := attemptsForProject(t, brain, root); len(rows) != 0 {
		t.Fatalf("a receipt with no frozen origin was reattributed to the live turn: %+v", rows)
	}
}

// Redundant repeats inside ONE frozen origin are recorded once; a different
// fact, and the same fact under a NEW origin, are recorded.
func TestDelegatedRedundantRepeatRecordedOncePerOrigin(t *testing.T) {
	root, brain, producer, _ := contextualReviewObservedFixture(t)
	root.prepareBindingContext(context.Background(), "fix the foobar parser")
	worker := spawnTaskWorker(t, root, filepath.Dir(producer))
	ctx := context.Background()
	pre := worker.captureSourceSnapshot(ctx).Identity
	for _, id := range []string{"r1", "r2", "r3"} {
		worker.recordOutcome(ctx, 0, delegatedBashCall(id, "go test ./foobar"), toolResult{text: "undefined: foobar.Token", isError: true}, pre)
	}
	if rows := attemptsForProject(t, brain, root); len(rows) != 1 {
		t.Fatalf("identical repeats in one origin wrote %d rows: %+v", len(rows), rows)
	}
	// A DIFFERENT failure in the same origin is a new fact.
	worker.recordOutcome(ctx, 0, delegatedBashCall("r4", "go test ./foobar"), toolResult{text: "undefined: other.Symbol", isError: true}, pre)
	if rows := attemptsForProject(t, brain, root); len(rows) != 2 {
		t.Fatalf("a different failure in one origin was dropped: %+v", rows)
	}
	// THE SAME FAILURE UNDER A NEW TURN IS INDEPENDENTLY OBSERVED and recorded.
	root.prepareBindingContext(context.Background(), "fix the foobar parser again")
	next := spawnTaskWorker(t, root, filepath.Dir(producer))
	next.recordOutcome(ctx, 0, delegatedBashCall("next-1", "go test ./foobar"), toolResult{text: "undefined: foobar.Token", isError: true}, next.captureSourceSnapshot(ctx).Identity)
	if rows := attemptsForProject(t, brain, root); len(rows) != 3 {
		t.Fatalf("a new-turn observation was suppressed as a repeat: %+v", rows)
	}
}

// The per-origin bookkeeping is bounded and least-recently-used, and a pending
// half-read pair survives ordinary churn.
func TestDelegatedOriginBookkeepingIsBoundedAndKeepsPendingPair(t *testing.T) {
	root, _ := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "p" })
	c := newOutcomeCollector(root)
	for i := 0; i < delegatedOriginBookkeepingMax+64; i++ {
		c.mu.Lock()
		c.stateLocked(fmt.Sprintf("o%d", i))
		c.mu.Unlock()
	}
	c.mu.Lock()
	size := len(c.origins)
	c.mu.Unlock()
	if size > delegatedOriginBookkeepingMax {
		t.Fatalf("origin bookkeeping grew to %d past its cap %d", size, delegatedOriginBookkeepingMax)
	}
	// A half-read pair is not ordinary churn's to drop.
	c = newOutcomeCollector(root)
	c.mu.Lock()
	pending := c.stateLocked("pending")
	pending.receipts = []memoryToolReceipt{{ID: "half"}}
	c.mu.Unlock()
	for i := 0; i < delegatedOriginBookkeepingMax+64; i++ {
		c.mu.Lock()
		c.stateLocked(fmt.Sprintf("n%d", i))
		c.mu.Unlock()
	}
	c.mu.Lock()
	_, kept := c.origins["pending"]
	pendingSize := len(c.origins)
	c.mu.Unlock()
	if !kept {
		t.Fatalf("a pending half-read pair was evicted at %d origins", pendingSize)
	}
	// Past the HARD cap even a pending pair goes, so bookkeeping is finite.
	for i := 0; i < delegatedOriginHardMax+64; i++ {
		c.mu.Lock()
		c.stateLocked(fmt.Sprintf("h%d", i))
		c.mu.Unlock()
	}
	c.mu.Lock()
	hardSize := len(c.origins)
	c.mu.Unlock()
	if hardSize > delegatedOriginHardMax {
		t.Fatalf("origin bookkeeping grew to %d past its hard cap %d", hardSize, delegatedOriginHardMax)
	}
}

// A settled job with no frozen launch turn is refused, not stamped with the
// live turn.
func TestPromotedJobWithoutFrozenTurnIsRefused(t *testing.T) {
	a, brain, _ := settleJobFixture(t)
	a.prepareBindingContext(context.Background(), "the question that is live at settle")
	a.jobs.origin = func() jobOrigin { return jobOrigin{Owner: store.OwnerProject("p")} }
	settleJob(t, a, "go test ./foobar", 3, false)
	if rows := attemptsFor(t, brain, store.OwnerProject("p"), "p"); len(rows) != 0 {
		t.Fatalf("a job with no frozen turn was reattributed to the live turn: %+v", rows)
	}
}

// A promoted job started by a REAL task worker reaches the root journal through
// the collector, with frozen provenance, cancellation distinction, and a source
// key that separates two workers' `job 1`.
func TestWorkerPromotedJobFailureReachesRootJournal(t *testing.T) {
	root, brain, producer, _ := contextualReviewObservedFixture(t)
	root.prepareBindingContext(context.Background(), "run the foobar suite")
	turn, goal := frozenRootTurn(t, root)
	worker := spawnTaskWorker(t, root, filepath.Dir(producer))
	worker.jobs.announce = nil
	settleJob(t, worker, "go test ./foobar", 3, false)
	rows := attemptsForProject(t, brain, root)
	if len(rows) != 1 {
		t.Fatalf("a worker's promoted job failure did not reach the root journal: %+v", rows)
	}
	if rows[0].SessionID != root.memorySourceSession() || rows[0].TurnID != turn || rows[0].Goal != goal {
		t.Fatalf("worker job provenance wrong: %+v (want session %q turn %q)", rows[0], root.memorySourceSession(), turn)
	}
	if !strings.HasPrefix(rows[0].SourceKey, "job:"+worker.origin.Run+":") {
		t.Fatalf("worker job source key lacks the worker run identity: %q (run %q)", rows[0].SourceKey, worker.origin.Run)
	}
	// A CANCELLED WORKER JOB IS NOT AN EXECUTION FAILURE.
	settleJob(t, worker, "go test ./other", 137, true)
	if rows = attemptsForProject(t, brain, root); len(rows) != 1 {
		t.Fatalf("a cancelled worker job was journaled as a failure: %+v", rows)
	}
}

// Two workers of the SAME node each start `job 1`; their failures are two
// independent journal rows, not one deduped fact.
func TestSeparateWorkersJobIdentityIsDistinctWithinOneNode(t *testing.T) {
	root, brain, producer, consumer := contextualReviewObservedFixture(t)
	root.prepareBindingContext(context.Background(), "run the foobar suite")
	// Two workers in DIFFERENT directories: the job-id counter is per jobs
	// folder, so each really starts `job 1` — the exact collision the run
	// identity in the source key exists to separate.
	first := spawnTaskWorker(t, root, filepath.Dir(producer))
	second := spawnTaskWorker(t, root, filepath.Dir(consumer))
	for _, worker := range []*Agent{first, second} {
		worker.jobs.announce = nil
	}
	firstJob := settleJob(t, first, "go test ./foobar", 3, false)
	secondJob := settleJob(t, second, "go test ./foobar", 3, false)
	if firstJob.id != secondJob.id {
		t.Fatalf("fixture no longer shares a job id across workers: %d vs %d", firstJob.id, secondJob.id)
	}
	rows := attemptsForProject(t, brain, root)
	if len(rows) != 2 {
		t.Fatalf("two workers' job 1 collapsed into %d rows: %+v", len(rows), rows)
	}
	if rows[0].SourceKey == rows[1].SourceKey {
		t.Fatalf("separate workers shared one source key: %q", rows[0].SourceKey)
	}
	// The root collector being closed refuses a late worker job settlement.
	root.outcomes.close()
	settleJob(t, first, "go test ./late", 3, false)
	if rows = attemptsForProject(t, brain, root); len(rows) != 2 {
		t.Fatalf("a closed root collector accepted a worker's late job: %+v", rows)
	}
}

// A worker finishing its node must not seal the root session's collector: later
// nodes and turns still need the bridge.
func TestWorkerCloseDoesNotSealRootCollector(t *testing.T) {
	root, brain, producer, _ := contextualReviewObservedFixture(t)
	root.prepareBindingContext(context.Background(), "run the foobar suite")
	first := spawnTaskWorker(t, root, filepath.Dir(producer))
	if err := first.Close(); err != nil {
		t.Fatalf("worker close: %v", err)
	}
	if root.outcomes.closedNow() {
		t.Fatal("a worker's Close sealed the root session's collector")
	}
	later := spawnTaskWorker(t, root, filepath.Dir(producer))
	later.recordOutcome(context.Background(), 0, delegatedBashCall("after-close", "go test ./foobar"), toolResult{text: "undefined: foobar.Token", isError: true}, "unknown")
	if rows := attemptsForProject(t, brain, root); len(rows) != 1 {
		t.Fatalf("a receipt after a worker closed was dropped: %+v", rows)
	}
}
