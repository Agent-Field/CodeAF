package session

// DELEGATED EVIDENCE.
//
// A task node builds its workers without a memory brain ([Agent.remembers] is
// false on a node), so a worker's own failing bash call used to be recorded
// nowhere: the failure lived only in the node's transcript and died with it.
// This file is the one bridge from a worker's TRUE tool boundary — the place
// its call actually ran — to the root conversation's journal.
//
// ── IT IS OBSERVATION-ONLY ──
//
// A worker HOLDS NO STORE AND NO MEMORY. It is handed a collector pointer and
// calls one method with a call, a result and nothing else; the collector — owned
// by the root session, which is the only agent with a store — stamps the root's
// session and owner and writes the same observed attempt the root writes for a
// call of its own. A worker gains no general memory write, no approval and no
// promotion power from this: the pointer is a one-way channel for a raw receipt,
// and the decision to keep it is the root's.
//
// ── WHAT IT IS TIED TO AND WHAT IT IS BOUNDED BY ──
//
// The record is tied to the ROOT SESSION and its owner, and to the TASK the
// worker belongs to ([Agent.outcomeOrigin]), never to the model's summary of its
// own work — a summary is the thing this exists to stop trusting. It is bounded
// three ways: the collector keeps a small idempotency set so a re-sent receipt
// writes once, it refuses to grow the set past [outcomeCollectorMax], and it
// DROPS every observation once the root closes, so a worker still running when
// the conversation ends cannot write into a journal that is shutting.
//
// A LATE ARRIVAL IS ATTRIBUTED TO ITS OWN JOB, NOT THE CURRENT TURN. A settled
// job's ending is stamped with its own source key rather than whatever turn is
// live when it lands, so a promoted command that dies an hour later cannot be
// read as a fact about the turn the person is in now.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/store"
)

// outcomeCollectorMax bounds the idempotency set. It is generous for a large
// delegated run and finite so a runaway worker cannot grow it without limit.
const outcomeCollectorMax = 4096

type outcomeCollector struct {
	root *Agent

	mu     sync.Mutex
	closed bool
	seen   map[string]bool
}

func newOutcomeCollector(root *Agent) *outcomeCollector {
	return &outcomeCollector{root: root, seen: map[string]bool{}}
}

// close stops the collector accepting further observations. Called from
// [Agent.Close] before the journal is sealed, so nothing lands as it shuts.
func (c *outcomeCollector) close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
}

// observe is the one call a worker makes. It carries the raw call and result
// from the worker's execution boundary; the collector decides, under the root's
// owner and session, whether the observation is worth a journal row.
func (c *outcomeCollector) observe(worker *Agent, call ai.ToolCall, result toolResult, preSnapshot string) {
	if c == nil {
		return
	}
	root := c.root
	if root == nil || !root.remembers() || root.memory == nil {
		return
	}
	status := attemptOutcomeStatus(result)
	if status == "" || !attemptWorthStoring(call, result) {
		return
	}
	action := attemptAction(call)
	receipt := contextualClip(result.text, contextualReceiptRunes)
	if action == "" || strings.TrimSpace(receipt) == "" {
		return
	}
	// The worker's own workspace, so the snapshot is the tree the call ran in
	// rather than the root's. A tree that moved while the call ran is unknown.
	post := worker.captureSourceSnapshot(context.Background()).Identity
	snapshot := post
	if preSnapshot != post {
		snapshot = "unknown"
	}
	origin := worker.outcomeOrigin
	if origin == "" {
		origin = "task"
	}
	key := "delegated:" + origin + ":" + call.ID

	c.mu.Lock()
	if c.closed || c.seen[key] || len(c.seen) >= outcomeCollectorMax {
		c.mu.Unlock()
		return
	}
	c.seen[key] = true
	c.mu.Unlock()

	owner := root.ownerForScope(store.MemoryScopeProject)
	conditions := map[string]string{}
	if key := strings.TrimSpace(root.config.MemoryProjectKey); key != "" {
		conditions["project"] = key
	}
	receipts := []string{}
	if status == store.AttemptFailed {
		receipts = append(receipts, call.ID)
	}
	e := store.ContextualAttempt{
		ID:          store.NewMemoryID(),
		Owner:       owner,
		SessionID:   root.memorySourceSession(),
		TurnID:      origin,
		Tool:        call.Function.Name,
		Action:      redact.Secrets(contextualClip(action, 1024)),
		Observation: redact.Secrets(receipt),
		Status:      status,
		ReceiptIDs:  receipts,
		Snapshot:    snapshot,
		Conditions:  conditions,
		SourceKey:   key,
		SourceHash:  contextualHash(receipt),
		ValidFrom:   time.Now(),
	}
	if _, err := root.memory.store.AppendContextualAttempt(e); err != nil {
		root.journalMemoryFailure("delegated-attempt", err)
	}
}

// recordOutcome is the dispatch at [Agent.executeTool]'s boundary. A session
// with a brain records the attempt as it always did; a worker with no brain but
// a collector forwards the raw observation to the root. Nothing else differs.
func (a *Agent) recordOutcome(ctx context.Context, turn uint64, call ai.ToolCall, result toolResult, preSnapshot string) {
	if a.remembers() {
		a.recordMemoryAttempt(ctx, turn, call, result, preSnapshot)
		return
	}
	if a.outcomes != nil {
		a.outcomes.observe(a, call, result, preSnapshot)
	}
}

// recordSettledJob records a bash job's REAL ending — the one the model never
// saw, because the call answered "still running as job N". It fires only for a
// failure (a job that exited 0 has nothing to warn a later turn about), and it
// is stamped with the job's own source key so a late death cannot be misread as
// a fact about the turn that happens to be live when it settles.
func (a *Agent) recordSettledJob(id int, command, tail string, code int) {
	if !a.remembers() || a.memory == nil || code == 0 {
		return
	}
	action := "bash: " + command
	receipt := contextualClip(tail, contextualReceiptRunes)
	if strings.TrimSpace(receipt) == "" {
		receipt = fmt.Sprintf("job %d exited %d", id, code)
	}
	owner := a.ownerForScope(store.MemoryScopeProject)
	conditions := map[string]string{}
	if key := strings.TrimSpace(a.config.MemoryProjectKey); key != "" {
		conditions["project"] = key
	}
	sourceKey := fmt.Sprintf("job:%d", id)
	e := store.ContextualAttempt{
		ID:          store.NewMemoryID(),
		Owner:       owner,
		SessionID:   a.memorySourceSession(),
		TurnID:      sourceKey,
		Tool:        "bash",
		Action:      redact.Secrets(contextualClip(action, 1024)),
		Observation: redact.Secrets(receipt),
		Status:      store.AttemptFailed,
		ReceiptIDs:  []string{sourceKey},
		Snapshot:    "unknown",
		Conditions:  conditions,
		SourceKey:   sourceKey,
		SourceHash:  contextualHash(receipt),
		ValidFrom:   time.Now(),
	}
	if _, err := a.memory.store.AppendContextualAttempt(e); err != nil {
		a.journalMemoryFailure("settled-job", err)
	}
}
