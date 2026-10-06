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
// calls one method with a call, a result and nothing else; the collector —
// owned by the root session, which is the only agent with a store — stamps the
// root's session and owner and writes the same observed attempt the root writes
// for a call of its own. A worker gains no general memory write, no approval and
// no promotion power from this: the pointer is a one-way channel for a raw
// receipt, and the decision to keep it is the root's.
//
// ── TWO KINDS OF RAW RECEIPT, BOTH TAKEN FROM THE ACTUAL BOUNDARY ──
//
// 1. A FAILURE OR BLOCK reaches the journal exactly as the root's own does.
// 2. A SUCCESSFUL, TRUSTED FULL READ also reaches the root's dependency
//    observation, so a delegated worker can discover a real producer/consumer
//    link by normal work. Only [Agent.contextualReadReceipt]'s proof is
//    forwarded: an actual path whose whole source content the tool returned.
//    Shell output, partial reads, summaries and matching names prove nothing.
//
// ── WHAT IT IS TIED TO AND WHAT IT IS BOUNDED BY ──
//
// The record is tied to the FROZEN ORIGIN captured at worker creation
// ([delegatedOrigin]): the root session, the root user turn and goal, the task
// and the owner, taken before the worker ran. It is never the model's summary of
// its own work, and a late arrival is stamped with the turn it was launched
// under rather than the user turn that happens to be live when it lands — so a
// slow worker cannot attach its receipt to somebody's next question.
//
// A LATE ARRIVAL IS ATTRIBUTED TO ITS OWN JOB, NOT THE CURRENT TURN. A settled
// job's ending is stamped with its own source key rather than whatever turn is
// live when it lands, so a promoted command that dies an hour later cannot be
// read as a fact about the turn the person is in now.
//
// THE IDEMPOTENCY HINT IS A HINT, NOT THE LAW. The collector's bounded set only
// avoids re-sending a receipt it already appended; the canonical append-only
// journal is the dedup (a re-sent source key and hash returns the row already
// held). The hint is evictable, so it can never permanently refuse to learn:
// once evicted, a genuine retry reaches the journal again. Nothing is marked
// seen until the append actually succeeded, so a failed write stays retryable.
// The failure itself is exposed: it is journaled, the owner is told once per
// window through the existing memory-failure lane, and [outcomeCollector.errorState]
// answers the count and the last error to whoever owns the session.
//
// ── BOUNDED, AND HONEST ABOUT WHAT IS NOT ──
//
// Every per-origin counter lives in [delegatedOriginState] and the set of
// origins is bounded and least-recently-used, so a long-lived root does not
// accumulate bookkeeping forever. A redundant repeat — the same failure, by
// action, observation and pre-action snapshot, inside one frozen origin — is
// recorded once, while a new turn or a genuinely different failure is fresh.
// None of this bounds the RAW APPEND-ONLY AUDIT: the canonical journal still
// keeps every distinct event, and physical pruning is refused by its own law.
//
// ── A WORKER'S PROMOTED JOB IS THE SAME BRIDGE ──
//
// A worker that promotes a bash command has no store either. Its job's real
// ending is forwarded through the same collector
// ([outcomeCollector.settleJob]) with the worker's frozen origin and the run
// identity that separates two workers' `job 1`, and only the OWNER of the
// collector may seal it ([Agent.Close]) — a node finishing must not shut the
// bridge later nodes and turns still need.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/store"
)

const (
	// outcomeCollectorMax bounds the idempotency hint. It is generous for a
	// large delegated run and finite; unlike the old set it is EVICTABLE (see
	// [outcomeCollector.remember]), so reaching it never stops learning.
	outcomeCollectorMax = 4096
	// delegatedReadEmissionMax bounds the successful full-read receipts one
	// frozen origin forwards to dependency observation. The reads are the
	// high-volume automatic path; the bound keeps one enormous node from
	// churning the journal without limit.
	delegatedReadEmissionMax = 32
	// delegatedAttemptEmissionMax bounds the automatic attempt rows one frozen
	// origin may write. A DEMONSTRATED FAILURE IS NEVER DROPPED by this bound:
	// a fresh failure is the learning contract 1 preserves, and the canonical
	// journal's source dedup keeps a re-send idempotent without the hint.
	delegatedAttemptEmissionMax = 256
	// delegatedOriginBookkeepingMax bounds how many frozen origins the collector
	// remembers. A long-lived root spawns workers across many turns; without
	// this the per-origin counters would grow forever. Eviction is
	// least-recently-used and never loses a HALF-READ pair that is still waiting
	// for its other side (see [outcomeCollector.evictOriginsLocked]).
	delegatedOriginBookkeepingMax = 256
	// delegatedOriginHardMax is the size at which even a pending half-read pair
	// is evicted. It exists so bookkeeping is genuinely finite; a single read
	// that never found its partner proves nothing, so losing it is honest.
	delegatedOriginHardMax = 512
	// delegatedRepeatMax bounds one origin's redundant-repeat set. A repeat is
	// the SAME failure, by action, observation and pre-action snapshot, inside
	// one frozen origin; a new turn or a genuinely different failure is fresh
	// and is recorded.
	delegatedRepeatMax = 64
)

// delegatedOriginState is the bounded bookkeeping for one frozen origin: its
// automatic emission counts, the trusted reads waiting to be paired, and the
// redundant repeats already recorded.
type delegatedOriginState struct {
	emissions   int
	reads       int
	receipts    []memoryToolReceipt
	repeats     map[string]struct{}
	repeatOrder []string
	// altOf, altTool and altAction are the most recent DEMONSTRATED failure this
	// frozen origin recorded and has not yet paired; altDone marks that its one
	// observed alternative is already held, so a node full of successes still
	// stores only the one that answers a real failure.
	altOf     string
	altTool   string
	altAction string
	altDone   bool
}

// delegatedOrigin is the immutable provenance of one worker, captured before it
// launches. Session and Owner are the root's; Turn and Goal are the root user
// turn's binding circumstances frozen at that instant; Task names the node and
// Run identifies the worker Agent itself.
type delegatedOrigin struct {
	Session string
	Owner   string
	// Project is the project identity this worker was ADMITTED under, frozen at
	// the same instant as Owner. It is recorded EXPLICITLY rather than read from
	// the root's live [Config.MemoryProjectKey] at write time, so a root anchor
	// that moves the conversation to another repository mid-run cannot relabel an
	// already-admitted worker's receipt or widen the project its conditions name
	// ([originProjectKey] recovers it from Owner when this is empty).
	Project string
	Turn    string
	Goal    string
	Task    string
	// Run identifies THIS worker Agent. Job ids restart per Agent, so two
	// workers of one node each starting job 1 must not share a source key; the
	// task label alone is not enough.
	Run string
}

type outcomeCollector struct {
	root *Agent

	mu     sync.Mutex
	closed bool
	// hint is the bounded, evictable idempotency set. hintOrder keeps insertion
	// order so the oldest entry can be evicted rather than refusing forever.
	hint      map[string]struct{}
	hintOrder []string
	// origins is the bounded, least-recently-used bookkeeping for frozen
	// origins; originOrder is its recency order.
	origins     map[string]*delegatedOriginState
	originOrder []string
	// failures and lastErr expose the write state an owner can actually see.
	failures int
	lastErr  error
	// inflight joins the writes already admitted when Close seals the collector.
	inflight sync.WaitGroup
}

func newOutcomeCollector(root *Agent) *outcomeCollector {
	return &outcomeCollector{
		root:    root,
		hint:    map[string]struct{}{},
		origins: map[string]*delegatedOriginState{},
	}
}

// close stops the collector accepting further observations, then waits for the
// writes already admitted. It is called from [Agent.Close] before the journal is
// sealed, so nothing new lands as it shuts and nothing admitted is cut off.
func (c *outcomeCollector) close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.inflight.Wait()
}

// closedNow answers whether the collector has been sealed, for callers that
// write through their own path (a settled job) rather than through observe.
func (c *outcomeCollector) closedNow() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// errorState answers how many delegated writes failed and what the last error
// was. The count is the owner-visible answer; the person is also told once per
// window through [Agent.journalMemoryFailure].
func (c *outcomeCollector) errorState() (int, error) {
	if c == nil {
		return 0, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failures, c.lastErr
}

// rootOrigin mints the frozen origin from the root session's live turn. It is
// called once, at worker creation, and never again: the copy lives on the
// worker so a long node is not re-stamped by the person's next question.
func (c *outcomeCollector) rootOrigin() delegatedOrigin {
	if c == nil || c.root == nil || c.root.memory == nil {
		return delegatedOrigin{}
	}
	root := c.root
	origin := delegatedOrigin{
		Session: root.memorySourceSession(),
		Owner:   root.ownerForScope(store.MemoryScopeProject),
	}
	// THE PROJECT IS DERIVED FROM THE OWNER JUST TAKEN, so the two can never
	// disagree: a project owner spells its key, and a non-project owner spells
	// none. Reading the live key here instead would be the very leak this field
	// exists to close.
	origin.Project = projectKeyFromOwner(origin.Owner)
	root.memory.mu.Lock()
	origin.Turn, origin.Goal = root.memory.outcomeTurnID, root.memory.outcomeGoal
	root.memory.mu.Unlock()
	return origin
}

func delegatedOriginKey(origin delegatedOrigin) string {
	return origin.Session + "\x00" + origin.Turn + "\x00" + origin.Task
}

// projectKeyFromOwner recovers a project key from a FROZEN owner. It is the safe
// derivation for an origin that carries only its owner: a project-kind owner
// spells the key it was minted from ([store.OwnerProject]), and the legacy
// quarantine owner spells NO project, so a quarantined row is never widened to a
// named one. It reads no live session field, so a later anchor cannot relabel it.
func projectKeyFromOwner(owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == store.OwnerLegacyProject {
		return ""
	}
	if !strings.HasPrefix(owner, "project:") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(owner, "project:"))
}

// originProjectKey is the project a frozen delegated origin was ADMITTED under.
// The explicit [delegatedOrigin.Project] wins; an origin carrying only its owner
// recovers the key from that owner. Either way it is the admission-time project,
// NEVER the root's live [Config.MemoryProjectKey], so a root anchor mid-run
// cannot relabel an already-admitted worker's receipt or widen its owner set.
func originProjectKey(origin delegatedOrigin) string {
	if key := strings.TrimSpace(origin.Project); key != "" {
		return key
	}
	return projectKeyFromOwner(origin.Owner)
}

// jobOriginProjectKey is [originProjectKey] for a promoted job's frozen
// provenance: its own field when set, otherwise the key its owner spells.
func jobOriginProjectKey(origin jobOrigin) string {
	if key := strings.TrimSpace(origin.Project); key != "" {
		return key
	}
	return projectKeyFromOwner(origin.Owner)
}

func delegatedSourceKey(origin delegatedOrigin, id string) string {
	return contextualClip("delegated:"+origin.Session+":"+origin.Turn+":"+origin.Task+":"+id, 1024)
}

// observe is the one call a worker makes. It carries the raw call and result
// from the worker's execution boundary; the collector decides, under the frozen
// origin and the root's owner, whether the observation is worth a journal row.
func (c *outcomeCollector) observe(worker *Agent, call ai.ToolCall, result toolResult, preSnapshot string) {
	if c == nil || worker == nil {
		return
	}
	root := c.root
	if root == nil || !root.remembers() || root.memory == nil {
		return
	}
	// A BRAIN THAT WAS ONLY LENT TO BIND IS NEVER WRITTEN THROUGH. The collector
	// is built beside the brain and hangs off the root session, so a
	// binding-only standing run has one even though [Agent.memoryWritable] is
	// false; a nested task worker carries this collector and a frozen origin
	// ([Agent.newTaskAgentOn]), so without this line the worker's failing tool
	// calls would be journaled as project attempts and the run's READ-ONLY
	// posture would be false. The refusal sits on the ONE boundary every
	// observation crosses, so a tool receipt, a forwarded read and a settled job
	// are all covered rather than each caller remembering.
	if root.config.bindingOnlyMemory {
		return
	}
	// FAIL CLOSED WITHOUT AN IMMUTABLE ORIGIN. Every real worker is stamped at
	// creation ([Agent.newTaskAgentOn]); falling back to the ROOT'S CURRENT turn
	// here would silently reattribute a late legacy arrival to whatever the
	// person is asking now. An observation with no frozen session, turn or owner
	// is refused instead.
	origin := worker.origin
	if strings.TrimSpace(origin.Session) == "" || strings.TrimSpace(origin.Turn) == "" || origin.Owner == "" || !store.ValidOwner(origin.Owner) {
		return
	}
	if strings.TrimSpace(origin.Task) == "" {
		origin.Task = "task"
	}
	if status := attemptOutcomeStatus(result); status != "" {
		c.observeAttempt(worker, origin, call, result, preSnapshot, status)
		return
	}
	c.observeAlternative(worker, origin, call, result, preSnapshot)
	c.observeRead(worker, origin, call, result)
}

// observeAttempt writes one delegated failure or block. It is the worker's own
// raw boundary evidence written under the root's journal, carrying the FROZEN
// ORIGINAL PURPOSE so a later turn can match the work that actually failed.
func (c *outcomeCollector) observeAttempt(worker *Agent, origin delegatedOrigin, call ai.ToolCall, result toolResult, preSnapshot, status string) {
	if !attemptWorthStoring(call, result) {
		return
	}
	action := attemptAction(call)
	// REDACTED BEFORE IT IS HASHED, FOR THE SAME REASON AS THE SESSION WRITER
	// ([Agent.recordMemoryAttempt]): every path that compares or stores this
	// receipt uses the redacted bytes, so identity, the repeat key and the
	// stored observation agree, and the secret never enters the journal.
	receipt := redact.Secrets(contextualClip(result.text, contextualReceiptRunes))
	if action == "" || strings.TrimSpace(receipt) == "" {
		return
	}
	key := delegatedSourceKey(origin, call.ID)
	originKey := delegatedOriginKey(origin)
	// A REDUNDANT REPEAT IS THE SAME FACT, not a new one. Within ONE frozen
	// origin, an identical action, observation and pre-action snapshot is the
	// same failure arriving again (a fresh call id, the same wall); recording it
	// again is churn. A NEW ORIGIN (the next turn, another node) has its own
	// repeat set, so an independently observed fact is always recorded.
	repeat := contextualHash(action + "\x00" + receipt + "\x00" + preSnapshot)
	if !c.admitAttempt(originKey, status, key, repeat) {
		return
	}
	defer c.inflight.Done()
	// The worker's own workspace, so the snapshot is the tree the call ran in
	// rather than the root's. A tree that moved while the call ran is unknown.
	post := worker.captureSourceSnapshot(context.Background()).Identity
	snapshot := post
	if preSnapshot != post {
		snapshot = "unknown"
	}
	// THE CONDITIONS NAME THE ADMITTED PROJECT, NEVER THE ROOT'S LIVE ONE. A root
	// anchor between admission and this write must not scope an already-admitted
	// worker's receipt to a repository it was never in ([originProjectKey]).
	conditions := map[string]string{}
	if projectKey := originProjectKey(origin); projectKey != "" {
		conditions["project"] = projectKey
	}
	receipts := []string{}
	if status == store.AttemptFailed {
		receipts = append(receipts, call.ID)
	}
	e := store.ContextualAttempt{
		ID:          store.NewMemoryID(),
		Owner:       origin.Owner,
		SessionID:   origin.Session,
		TurnID:      origin.Turn,
		Tool:        call.Function.Name,
		Action:      redact.Secrets(contextualClip(action, 1024)),
		Goal:        redact.Secrets(contextualClip(origin.Goal, 1024)),
		Status:      status,
		ReceiptIDs:  receipts,
		Observation: receipt,
		Snapshot:    snapshot,
		Conditions:  conditions,
		SourceKey:   key,
		SourceHash:  contextualHash(receipt),
		ValidFrom:   time.Now(),
	}
	if _, err := c.root.memory.store.AppendContextualAttempt(e); err != nil {
		// SEEN IS NOT MARKED. A failed write leaves the receipt retryable, and
		// the failure is exposed rather than swallowed.
		c.fail(err)
		c.root.journalMemoryFailure("delegated-attempt", err)
		return
	}
	c.remember(key)
	c.rememberRepeat(originKey, repeat)
	// A DEMONSTRATED FAILURE OPENS THE ONE PAIRING SLOT for this frozen origin:
	// a later success of the same tool class and action may be carried as its
	// observed alternative. A block (unknown) does not, because nothing was
	// proven to have failed.
	if status == store.AttemptFailed {
		c.mu.Lock()
		if !c.closed {
			state := c.stateLocked(originKey)
			state.altOf = key
			state.altTool = e.Tool
			state.altAction = e.Action
			state.altDone = false
		}
		c.mu.Unlock()
	}
}

// observeAlternative carries the one success that follows a demonstrated
// failure of the same frozen origin. It mirrors the root writer
// ([Agent.recordMemoryAlternative]) exactly: a true tool-boundary success only,
// the same tool class, a shared meaningful token, and a lookup or metadata call
// refused. The row is bounded by the SAME per-origin emission bound as any other
// automatic attempt, so an enormous node cannot churn the journal; the failure
// itself remains the undroppable evidence.
func (c *outcomeCollector) observeAlternative(worker *Agent, origin delegatedOrigin, call ai.ToolCall, result toolResult, preSnapshot string) {
	if worker == nil || result.harness || result.refusedBy != "" || result.isError {
		return
	}
	action := attemptAction(call)
	receipt := redact.Secrets(contextualClip(result.text, contextualReceiptRunes))
	if action == "" || strings.TrimSpace(receipt) == "" || receiptShowsFailure(receipt) {
		return
	}
	originKey := delegatedOriginKey(origin)
	// RESERVE THE ONE-ALTERNATIVE SLOT AND THE EMISSION UNDER ONE LOCK, BEFORE
	// the append. The pairing must still be for the SAME failure and still open,
	// so the second of two eligible siblings of one concurrent tool batch sees
	// the slot already taken and returns; marking the slot only after the write
	// let every sibling append. The emission counter is reserved here too, so
	// the per-origin bound cannot be lost to the race either.
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	state := c.stateLocked(originKey)
	// THE WORKER'S OWN WORKSPACE BELONGS IN THE ELIGIBILITY CALL, and it is now
	// passed. [alternativeEligible]'s optional trailing workspace is the effective
	// directory a goal-named file operand is normalized against; the root caller
	// passes Config.Workspace and this delegated caller must pass the worker's own
	// Config.Workspace, or an unrelated command that merely runs from the same
	// BASENAME elsewhere can be read as the remedy for a failure (the A4 false
	// positive). A worker stands where its ground put it, so the worker's config is
	// the correct directory — never the root's, whose workspace may have moved
	// under a later anchor.
	if state.altDone || state.altOf == "" || state.emissions >= delegatedAttemptEmissionMax || !alternativeEligible(call, state.altTool, state.altAction, origin.Goal, worker.config.Workspace) {
		c.mu.Unlock()
		return
	}
	key := state.altOf
	state.altDone = true
	state.emissions++
	c.inflight.Add(1)
	c.mu.Unlock()
	defer c.inflight.Done()
	post := worker.captureSourceSnapshot(context.Background()).Identity
	snapshot := post
	if preSnapshot != post {
		snapshot = "unknown"
	}
	// THE SAME ADMITTED PROJECT AS ITS FAILURE, for the same reason: the pair is
	// one fact under one owner and must not straddle a later root anchor.
	conditions := map[string]string{}
	if projectKey := originProjectKey(origin); projectKey != "" {
		conditions["project"] = projectKey
	}
	e := store.ContextualAttempt{
		ID:            store.NewMemoryID(),
		Owner:         origin.Owner,
		SessionID:     origin.Session,
		TurnID:        origin.Turn,
		Tool:          call.Function.Name,
		Action:        redact.Secrets(contextualClip(action, 1024)),
		Goal:          redact.Secrets(contextualClip(origin.Goal, 1024)),
		Status:        store.AttemptSucceeded,
		ReceiptIDs:    []string{call.ID},
		Observation:   receipt,
		Snapshot:      snapshot,
		Conditions:    conditions,
		AlternativeOf: key,
		SourceKey:     delegatedSourceKey(origin, call.ID),
		SourceHash:    contextualHash(receipt),
		ValidFrom:     time.Now(),
	}
	if _, err := c.root.memory.store.AppendContextualAttempt(e); err != nil {
		// A FAILED WRITE REOPENS THE PAIRING FOR THE SAME FAILURE and gives the
		// reserved emission back, so a retry is neither blocked nor counted; a
		// newer failure owns its own slot and is never cleared by this. The
		// failure is exposed through the collector's own lane, never swallowed.
		c.mu.Lock()
		if !c.closed {
			if latest := c.stateLocked(originKey); latest.altOf == key {
				latest.altDone = false
				if latest.emissions > 0 {
					latest.emissions--
				}
			}
		}
		c.mu.Unlock()
		c.fail(err)
		c.root.journalMemoryFailure("delegated-alternative", err)
		return
	}
}

// observeRead forwards a successful, trusted full read to the root's dependency
// observation. It accepts ONLY [Agent.contextualReadReceipt]'s proof of an
// actual path and its whole content; nothing here parses a summary or trusts a
// tool name. The read is accumulated against its frozen origin so two reads of
// one node can form an observed link, and the bound keeps one origin finite.
func (c *outcomeCollector) observeRead(worker *Agent, origin delegatedOrigin, call ai.ToolCall, result toolResult) {
	// FORWARD ONLY UNDER AN AUTHENTIC, FROZEN SESSION AND OWNER. The dependency
	// seam pairs a delegated read with the root's own live trusted reads by the
	// NUMERIC turn parsed out of Turn, so an origin carrying a BORROWED session
	// (or an owner that is not a valid frozen one) with a colliding numeric turn
	// could pull another session's or another owner's reads into this worker's
	// edge. Refuse rather than widen: a read this collector cannot prove belongs
	// to its own root is not forwarded at all.
	if strings.TrimSpace(origin.Session) != c.root.memorySourceSession() || !store.ValidOwner(origin.Owner) {
		return
	}
	receipt := memoryToolReceipt{ID: call.ID, Tool: call.Function.Name, Text: redact.Secrets(contextualClip(result.text, contextualReceiptRunes)), Status: toolStatus(result)}
	receipt = worker.contextualReadReceipt(context.Background(), call, result, receipt)
	if receipt.Path == "" || receipt.Hash == "" {
		return
	}
	originKey := delegatedOriginKey(origin)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	state := c.stateLocked(originKey)
	if state.reads >= delegatedReadEmissionMax {
		c.mu.Unlock()
		return
	}
	state.reads++
	rows := append(append([]memoryToolReceipt(nil), state.receipts...), receipt)
	if len(rows) > contextualReceiptLimit {
		rows = rows[len(rows)-contextualReceiptLimit:]
	}
	state.receipts = rows
	c.inflight.Add(1)
	c.mu.Unlock()
	defer c.inflight.Done()
	// The turn identity is the FROZEN root turn, never the live one: a late read
	// cannot bind itself to the user turn that happens to be current.
	c.root.observeContextualDependencies(memoryTurnEvidence{Session: origin.Session, Turn: origin.Turn, Project: originProjectKey(origin), Owner: origin.Owner, Receipts: rows})
}

// admitAttempt answers whether this attempt may proceed, taking its in-flight
// slot under the same lock. The hint is checked, not written: the write happens
// after the append succeeds ([outcomeCollector.remember]).
func (c *outcomeCollector) admitAttempt(originKey, status, key, repeat string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	if _, seen := c.hint[key]; seen {
		return false
	}
	state := c.stateLocked(originKey)
	if _, repeated := state.repeats[repeat]; repeated {
		return false
	}
	// A DEMONSTRATED FAILURE IS NEVER DROPPED by the per-origin bound; the
	// bound exists for the automatic churn of blocks and duplicate retries.
	if status != store.AttemptFailed && state.emissions >= delegatedAttemptEmissionMax {
		return false
	}
	state.emissions++
	c.inflight.Add(1)
	return true
}

// stateLocked returns the bookkeeping for one origin, marking it most recently
// used and evicting completed old origins when the map is over its cap. The
// caller holds c.mu.
func (c *outcomeCollector) stateLocked(originKey string) *delegatedOriginState {
	state := c.origins[originKey]
	if state == nil {
		state = &delegatedOriginState{repeats: map[string]struct{}{}}
		c.origins[originKey] = state
		c.originOrder = append(c.originOrder, originKey)
	} else {
		c.bumpOriginLocked(originKey)
	}
	c.evictOriginsLocked()
	return state
}

func (c *outcomeCollector) bumpOriginLocked(originKey string) {
	for i, key := range c.originOrder {
		if key == originKey {
			c.originOrder = append(c.originOrder[:i], c.originOrder[i+1:]...)
			break
		}
	}
	c.originOrder = append(c.originOrder, originKey)
}

// evictOriginsLocked drops least-recently-used origins once the map is over its
// cap. A half-read pair (one forwarded read still waiting for its partner) is
// OBLIVIOUS to eviction while the map is merely at its soft cap, so an in-flight
// partial pair is not lost to ordinary churn; it can only go once the hard cap
// is reached, where the oldest entries — pending or not — are dropped. The
// canonical journal is untouched either way, so a replayed read is deduped.
func (c *outcomeCollector) evictOriginsLocked() {
	for len(c.originOrder) > delegatedOriginBookkeepingMax {
		if len(c.originOrder) > delegatedOriginHardMax {
			key := c.originOrder[0]
			c.originOrder = c.originOrder[1:]
			delete(c.origins, key)
			continue
		}
		dropped := false
		for i, key := range c.originOrder {
			state := c.origins[key]
			if state != nil && len(state.receipts) == 1 {
				continue
			}
			c.originOrder = append(c.originOrder[:i], c.originOrder[i+1:]...)
			delete(c.origins, key)
			dropped = true
			break
		}
		if !dropped {
			return
		}
	}
}

// rememberRepeat marks a redundant-repeat key as already recorded for this
// origin. Like the idempotency hint it is bounded and evictable, so it can
// never permanently refuse a genuinely new observation.
func (c *outcomeCollector) rememberRepeat(originKey, repeat string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	state := c.stateLocked(originKey)
	if _, seen := state.repeats[repeat]; seen {
		return
	}
	state.repeats[repeat] = struct{}{}
	state.repeatOrder = append(state.repeatOrder, repeat)
	for len(state.repeatOrder) > delegatedRepeatMax {
		oldest := state.repeatOrder[0]
		state.repeatOrder = state.repeatOrder[1:]
		delete(state.repeats, oldest)
	}
}

// remember records a successful append in the bounded, EVICTABLE hint. When the
// hint is full the oldest entry goes, so the set is a hint about recent writes
// rather than a permanent refusal to learn; the journal's own source dedup is
// what actually makes a re-send idempotent.
func (c *outcomeCollector) remember(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if _, seen := c.hint[key]; seen {
		return
	}
	c.hint[key] = struct{}{}
	c.hintOrder = append(c.hintOrder, key)
	for len(c.hintOrder) > outcomeCollectorMax {
		oldest := c.hintOrder[0]
		c.hintOrder = c.hintOrder[1:]
		delete(c.hint, oldest)
	}
}

func (c *outcomeCollector) fail(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	c.failures++
	c.lastErr = err
	c.mu.Unlock()
}

// recordOutcome is the dispatch at [Agent.executeTool]'s boundary. A session
// with a brain records the attempt as it always did; a worker with no brain but
// a collector forwards the raw observation to the root. Nothing else differs.
func (a *Agent) recordOutcome(ctx context.Context, turn uint64, call ai.ToolCall, result toolResult, preSnapshot string) {
	if a.memoryWritable() {
		a.recordMemoryAttempt(ctx, turn, call, result, preSnapshot)
		return
	}
	if a.outcomes != nil {
		a.outcomes.observe(a, call, result, preSnapshot)
	}
}

// jobOrigin is a promoted command's frozen provenance, taken at launch so a
// late death is stamped with the turn that started it rather than the turn that
// is live when it lands.
type jobOrigin struct {
	Session string
	Turn    string
	Goal    string
	Owner   string
	// Project is the project identity the launching agent was ADMITTED under,
	// frozen at launch for the same reason Owner is: a root anchor afterwards
	// must not relabel a promoted job's settled receipt or widen its conditions
	// ([jobOriginProjectKey] recovers it from Owner when this is empty).
	Project string
	// Worker is the launching worker Agent's identity, frozen at its creation.
	// It is empty for a job launched by the root session, whose stable session
	// identity is read at settle instead.
	Worker string
}

// frozenJobOrigin captures the root session's current turn circumstances for a
// job about to be launched. It deliberately reads only the memory brain's own
// lock, never the agent's: it runs on the launch path, and a session id read
// there would take a lock the launching tool may already hold. The session
// identity a settled job is stamped with is read at settle instead; it is
// stable for the session's whole life, so "the original session" is preserved.
func (a *Agent) frozenJobOrigin() jobOrigin {
	if a.memory == nil {
		// A TASK WORKER HAS NO BRAIN BUT IT DOES HAVE A FROZEN ORIGIN. A
		// promoted command it started is still the person's work, and its real
		// death is exactly the fact a later turn must be warned about
		// (contextual_delegated.go's [Agent.recordSettledJob]).
		o := a.origin
		if strings.TrimSpace(o.Session) == "" || strings.TrimSpace(o.Turn) == "" {
			return jobOrigin{}
		}
		return jobOrigin{Turn: o.Turn, Goal: o.Goal, Owner: o.Owner, Project: o.Project, Worker: o.Run}
	}
	owner := a.ownerForScope(store.MemoryScopeProject)
	origin := jobOrigin{Owner: owner, Project: projectKeyFromOwner(owner)}
	a.memory.mu.Lock()
	origin.Turn, origin.Goal = a.memory.outcomeTurnID, a.memory.outcomeGoal
	a.memory.mu.Unlock()
	return origin
}

// recordSettledJob records a promoted bash job's REAL ending — the one the model
// never saw, because the call answered "still running as job N". A session with
// a brain and a task worker BOTH route through the root-owned collector: a
// worker has no store of its own, and the collector is the one bridge it has
// into the root's journal (it gains no general write power from it). The
// collector refuses a job with no frozen launch turn rather than stamping the
// live one, stamps the source with the launching RUN identity plus the job id
// (job ids restart per Agent), and admits the write through the same in-flight
// gate as every other observation so it can never land after Close sealed the
// journal.
func (a *Agent) recordSettledJob(one *job, code int) {
	if one == nil || code == 0 || a.outcomes == nil {
		return
	}
	a.outcomes.settleJob(one, code)
}

// settleJob is the collector's own job write. It is observation-only on the
// caller: the worker supplies the job's raw sink tail and exit code and gets no
// store handle in return.
func (c *outcomeCollector) settleJob(one *job, code int) {
	if c == nil || one == nil || code == 0 {
		return
	}
	root := c.root
	if root == nil || !root.remembers() || root.memory == nil {
		return
	}
	// THE SAME BOUNDARY FOR A PROMOTED JOB. A binding-only run's own bash job
	// settles with a real frozen turn (unlike its tool calls, whose origin is
	// empty), so without this refusal its death would be written into the
	// borrowed brain exactly as a worker's would.
	if root.config.bindingOnlyMemory {
		return
	}
	origin := one.origin
	// FAIL CLOSED WITHOUT THE FROZEN TURN. A job with no launch-turn provenance
	// is not stamped with whatever turn is live at settle — that would let a
	// late death be read as a fact about the person's next question.
	turn := strings.TrimSpace(origin.Turn)
	if turn == "" {
		return
	}
	// ADMISSION AND CLOSE ARE ONE DECISION, exactly as they are for a tool
	// receipt: the in-flight slot is taken under the same lock the seal takes,
	// so a settlement racing Close either wins and is waited for, or is refused.
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.inflight.Add(1)
	c.mu.Unlock()
	defer c.inflight.Done()

	run := strings.TrimSpace(origin.Worker)
	if run == "" {
		run = root.memorySourceSession()
	}
	session := strings.TrimSpace(origin.Session)
	if session == "" {
		session = root.memorySourceSession()
	}
	owner := origin.Owner
	if owner == "" || !store.ValidOwner(owner) {
		owner = root.ownerForScope(store.MemoryScopeProject)
	}
	sourceKey := "job:" + run + ":" + strconv.Itoa(one.id)
	action := "bash: " + one.command
	// THE SAME REDACT-THEN-HASH ORDER AS EVERY OTHER ATTEMPT WRITER, so the
	// stored tail and its identity are the same bytes and no secret reaches the
	// project journal through a promoted job's death.
	receipt := redact.Secrets(contextualClip(one.sink.tail(jobExitTailLines), contextualReceiptRunes))
	if strings.TrimSpace(receipt) == "" {
		receipt = fmt.Sprintf("job %d exited %d", one.id, code)
	}
	// THE JOB'S PROJECT IS THE LAUNCHING AGENT'S ADMITTED PROJECT, frozen with
	// the launch turn; a root anchor before the job dies must not relabel it.
	conditions := map[string]string{}
	if projectKey := jobOriginProjectKey(origin); projectKey != "" {
		conditions["project"] = projectKey
	}
	e := store.ContextualAttempt{
		ID:          store.NewMemoryID(),
		Owner:       owner,
		SessionID:   session,
		TurnID:      turn,
		Tool:        "bash",
		Action:      redact.Secrets(contextualClip(action, 1024)),
		Goal:        redact.Secrets(contextualClip(origin.Goal, 1024)),
		Status:      store.AttemptFailed,
		ReceiptIDs:  []string{sourceKey},
		Observation: receipt,
		Snapshot:    "unknown",
		Conditions:  conditions,
		SourceKey:   sourceKey,
		SourceHash:  contextualHash(receipt),
		ValidFrom:   time.Now(),
	}
	if _, err := root.memory.store.AppendContextualAttempt(e); err != nil {
		c.fail(err)
		root.journalMemoryFailure("settled-job", err)
	}
}
