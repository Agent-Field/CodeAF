package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/reflex"
	"github.com/Agent-Field/codeaf/internal/store"
)

const (
	contextualReceiptLimit = 8
	contextualReceiptRunes = 2000
	contextualContextLimit = 8
	contextualTag          = "contextual-evidence"
)

type memoryToolReceipt struct {
	ID, Tool, Text, Status string
	Path, Hash, Body       string
	// Snapshot is the source identity the read actually saw. Tool-derived
	// evidence carries THIS rather than the turn's start-of-turn snapshot: a
	// claim learned from a receipt is earned under the state that receipt read.
	Snapshot string
}
type memoryTurnEvidence struct {
	Session, Turn, User, Revision string
	// Project is the ADMITTED project a delegated origin's receipts belong to,
	// carried so the dependency seam can scope an emission to the admission-time
	// project rather than the root's live [Config.MemoryProjectKey]. Empty for a
	// conversation's own turn, whose receipts already carry their file's owner.
	Project string
	// Owner is the FROZEN owner a delegated origin was admitted under, carried so
	// the dependency seam can hold a forwarded read to the owner it was admitted
	// into even though the edge's two endpoint owners are derived from the paths.
	// Empty for a conversation's own turn.
	Owner string
	// Ceiling is the ADMISSION-TIME approval policy a delegated origin was
	// stamped with (its frozen read ceiling), carried so the dependency seam can
	// judge a forwarded producer re-read under BOTH that ceiling and the live
	// policy. Nil for a conversation's own turn, which has no admission of its
	// own and answers only to the live policy.
	Ceiling  *approval.Policy
	Receipts []memoryToolReceipt
	At       time.Time
}

// recordMemoryTool retains independent receipts, including failure and refusal.
// Assistant summaries never turn into additional corroborating observations.
func (a *Agent) recordMemoryTool(ctx context.Context, turn uint64, call ai.ToolCall, result toolResult) {
	if !a.memoryWritable() {
		return
	}
	receipt := memoryToolReceipt{ID: call.ID, Tool: call.Function.Name, Text: redact.Secrets(contextualClip(result.text, contextualReceiptRunes)), Status: toolStatus(result)}
	receipt = a.contextualReadReceipt(ctx, call, result, receipt)
	a.memory.mu.Lock()
	defer a.memory.mu.Unlock()
	if a.memory.receipts == nil {
		a.memory.receipts = make(map[uint64][]memoryToolReceipt)
	}
	rows := a.memory.receipts[turn]
	if len(rows) < contextualReceiptLimit {
		a.memory.receipts[turn] = append(rows, receipt)
	}
	for seq := range a.memory.receipts {
		if seq+1 < turn {
			delete(a.memory.receipts, seq)
		}
	}
}

func (a *Agent) memoryTurnSource(user string) memoryTurnEvidence {
	a.mu.Lock()
	turn := a.turnSeq
	a.mu.Unlock()
	source := memoryTurnEvidence{Session: a.memorySourceSession(), Turn: fmt.Sprint(turn) + ":" + contextualHash(user)[:16], User: user, At: time.Now()}
	a.memory.mu.Lock()
	source.Receipts = append([]memoryToolReceipt(nil), a.memory.receipts[turn]...)
	source.Revision = a.memory.revisions[turn]
	delete(a.memory.receipts, turn)
	a.memory.mu.Unlock()
	return source
}

func (s memoryTurnEvidence) extractContext(answer string) string {
	var b strings.Builder
	// Receipts precede the answer so the extractor's input ceiling cannot discard
	// the independent evidence while retaining the assistant's own claims.
	if len(s.Receipts) > 0 {
		b.WriteString("TOOL RECEIPTS (observations, not proof of a claimed cause):\n")
		for _, r := range s.Receipts {
			fmt.Fprintf(&b, "%s [%s/%s]: %s\n", r.ID, r.Tool, r.Status, contextualClip(r.Text, 240))
		}
	}
	b.WriteString("ASSISTANT STATEMENT (not corroboration):\n")
	b.WriteString(contextualClip(answer, contextualReceiptRunes))
	return b.String()
}

func (s memoryTurnEvidence) ground(c reflex.ExtractResult) reflex.ExtractResult {
	// Automatic scope defaults to the observed project. A user-level preference
	// requires a literal supporting quote, rather than the model's scope guess.
	quote := strings.TrimSpace(c.SourceQuote)
	userSupported := c.Source == "user" && quote != "" && strings.Contains(s.User, quote)
	if c.Scope == store.MemoryScopeUser && (!userSupported || (c.Type != store.MemoryPreference && c.Authority != "approved_rule")) {
		c.Scope = store.MemoryScopeProject
	}
	// MACHINE SCOPE NEEDS A MACHINE-WIDE SPAN. Kept only for a supported user
	// quote that actually says so; an unrelated quote in the same turn, or a
	// tool observation the model labelled `env`, stays project-local rather
	// than silently applying to every project on the machine.
	if c.Scope == store.MemoryScopeEnv && (!userSupported || !explicitContextualMachine(userQuoteForScope(c, s.User))) {
		c.Scope = store.MemoryScopeProject
	}
	if !userSupported {
		c.Source = "assistant"
		c.Authority = "proposal"
		for _, r := range s.Receipts {
			// A RECEIPT FROM A DERIVED SOURCE IS NOT AN INDEPENDENT OBSERVATION.
			// A task worker's report, a conversation-history lookup and this
			// session's own memory_evidence are all things a model already read
			// or summarized, so citing one back is self-corroboration: it can
			// re-learn a suppressed claim under a fresh reader receipt id. The
			// raw boundary collector is the only path a worker's real outcome
			// or full read takes. Genuine raw tool evidence is untouched.
			if c.ReceiptID == r.ID && r.Status != "refused" && !contextualDerivedReceipt(r.Tool) {
				c.Source = "tool"
				c.Authority = "observation"
				break
			}
		}
	} else {
		// AN EXTRACTOR LABEL CANNOT MANUFACTURE AUTHORITY FROM A WHOLE
		// UTTERANCE. The gate reads the SELF-CONTAINED supporting span, not
		// every word of the turn: an unrelated true quote sharing a turn with
		// another rule or a global preference must not widen a different fact,
		// and a quoted "yes" is not a rule whatever else was said. The
		// surrounding utterance still travels in the evidence, so conditions,
		// exceptions and rationale are not lost.
		// A TRUE SUBSTRING IS NOT AUTHORITY. The quote must also sit in a span
		// the person actually asserted: a rule inside a question ("Should we
		// never use pandas?") or a rejected third-party quotation ("The reviewer
		// said never use floats, but I reject that") is reported, not approved.
		// The check is textual and conservative over the CONTAINING clause, not
		// a semantic vote; a polite real directive ("Please make sure ...") and
		// ordinary literal constraints still bind automatically.
		clause := contextualSupportClause(s.User, quote)
		directive := contextualSpanDirective(clause)
		// A ONE-TURN TASK AUTHORIZATION IS NOT A LASTING RULE. The word cues the
		// gates read ("only", "must", "use the") are the same words an ordinary
		// task command uses, so an extractor that mislabels a command as a rule
		// or a decision could carry a one-off authorization into the persistent
		// binding. A clearly bounded task — cross-check, compare, verify, run
		// something read-only — is demoted to an observation unless the same
		// span states a durable rule or an explicit decision, which still binds.
		task := contextualOneTurnTaskAuthorization(clause)
		if c.Authority == "approved_rule" && (!explicitContextualRule(quote) || !directive || task) {
			c.Authority = "observation"
		}
		if c.Authority == "confirmed_decision" && (!explicitContextualDecision(quote) || !directive || task) {
			c.Authority = "observation"
		}
		if c.Authority != "approved_rule" && c.Authority != "confirmed_decision" {
			c.Authority = "observation"
		}
	}
	if c.Scope == store.MemoryScopeUser && !explicitContextualGlobal(userQuoteForScope(c, s.User)) {
		c.Scope = store.MemoryScopeProject
	}
	// SANITIZE ON THE WAY OUT, VALIDATE ON THE WAY IN. The literal-support
	// checks above read the person's ORIGINAL words, because that is what proves
	// a rule is real; everything this session persists or renders is passed
	// through the one secret redactor first. A credential in the same utterance
	// as a genuine constraint is removed from the claim body, its conditions,
	// rationale, rejected alternatives and reconsideration, and from the
	// observation, before any of it can reach the prompt or memory_evidence.
	c.Text = redact.Secrets(contextualClip(c.Text, store.MemoryTextRunes))
	c.Tags = append(c.Tags, contextualTag)
	return c
}

func (a *Agent) recordContextualMemory(m store.Memory, c reflex.ExtractResult, s memoryTurnEvidence) error {
	owner := a.ownerForScope(c.Scope)
	// An update result can omit owner; the authority still comes from the same
	// candidate partition used by settlement, never from the session's read set.
	// THE SOURCE KEY AND HASH STAY ON THE RAW TURN so suppression and dedup keep
	// their exact semantics; only the human-readable fields are sanitized.
	e := store.ContextualEvidence{ID: store.NewMemoryID(), MemoryID: m.ID, Owner: owner, SessionID: s.Session, TurnID: s.Turn, Actor: c.Source, Authority: c.Authority, Observation: redact.Secrets(contextualClip(s.User, 4000)), Verification: "asserted", ValidFrom: s.At, SourceKey: s.Session + ":" + s.Turn, SourceHash: contextualHash(s.User), Applicability: contextualSanitizedItems(c.Conditions), Rationale: redact.Secrets(contextualClip(c.Rationale, 240)), Rejected: contextualSanitizedItems(c.Rejected), Reconsider: redact.Secrets(contextualClip(c.Reconsider, 240))}
	// A PROJECT FACT CARRIES ITS PROJECT; A MACHINE FACT DOES NOT. The owner is
	// what scopes a machine-wide rule to the one authorized machine, so pinning
	// its evidence to the origin project would silently restrict it there. User
	// and machine evidence carry no project condition; project evidence does.
	if owner != store.OwnerUser && owner != store.OwnerMachine {
		e.Conditions = map[string]string{"project": a.config.MemoryProjectKey}
	}
	if c.Source == "tool" {
		for _, r := range s.Receipts {
			if r.ID == c.ReceiptID {
				e.Tool = r.Tool
				e.ReceiptIDs = []string{r.ID}
				e.Observation = r.Text
				e.Verification = "observed"
				e.SourceHash = contextualHash(r.Text)
				e.SourceKey = s.Session + ":" + s.Turn + ":" + r.ID
				// THE RECEIPT'S OWN SNAPSHOT, not the turn's start-of-turn one: the
				// claim was earned under the state the read actually saw.
				e.Revision = r.Snapshot
				break
			}
		}
		if e.Revision == "" {
			e.Revision = "source-snapshot-unavailable"
		}
	} else if c.Source != "user" {
		e.Actor = "assistant"
		e.Authority = "proposal"
		e.Verification = "unverified"
		e.Revision = s.Revision
		if e.Revision == "" {
			e.Revision = "source-snapshot-unavailable"
		}
	}
	if c.Source == "user" && c.Type == store.MemoryProjectState && (c.WorkStatus == "completed" || c.WorkStatus == "abandoned") {
		e.ValidUntil = s.At.Add(time.Nanosecond)
	}
	if e.Observation == "" {
		e.Observation = c.Text
	}
	_, err := a.memory.store.AppendContextualEvidence(e)
	return err
}

// prepareBindingContext performs a bounded local read before the first request.
// Semantic recall remains optional; a router outage cannot erase approved rules.
func (a *Agent) prepareBindingContext(ctx context.Context, cue string) {
	if !a.remembers() {
		return
	}
	snapshot := a.captureSourceSnapshot(ctx)
	revision := snapshot.Identity
	a.mu.Lock()
	turn := a.turnSeq
	a.mu.Unlock()
	a.memory.mu.Lock()
	if a.memory.revisions == nil {
		a.memory.revisions = map[uint64]string{}
	}
	a.memory.revisions[turn] = revision
	for seq := range a.memory.revisions {
		if seq+1 < turn {
			delete(a.memory.revisions, seq)
		}
	}
	a.memory.outcomeGoal = cue
	a.memory.outcomeTurnID = a.memorySourceSession() + ":" + fmt.Sprint(turn) + ":" + contextualHash(cue)[:16]
	// A NEW TURN BEGINS WITH NO PENDING FAILURE: an alternative is only ever the
	// later success of the SAME turn and goal that recorded the failure, never a
	// success inherited across turns.
	a.memory.outcomeFailedKey = ""
	a.memory.outcomeFailedTool = ""
	a.memory.outcomeFailedAction = ""
	a.memory.outcomeAlternativeDone = false
	a.memory.mu.Unlock()
	rules := a.bindingContext(cue, revision)
	// The shared ceiling is a promise about the system prompt: the mandatory
	// approved rules are reserved FIRST and whole, and the optional impacts and
	// prior outcomes spend only what is left, by WHOLE records, so no record is
	// ever clipped mid-sentence and no wrapper is left open.
	block := composeBeforeRequestContext(rules, a.contextualImpactContext(cue), a.priorOutcomeContext(cue, revision), "")
	a.mu.Lock()
	a.memoryText = block
	a.landVolatileLocked()
	a.mu.Unlock()
}

// prepareWorkerBinding performs the DETERMINISTIC approved-binding read a task
// worker must have before its FIRST provider request, ALONGSIDE the node's
// asynchronous semantic routing ([nodeMemory]). The router is an aid and may
// arrive after the work has begun — that is by design ([Agent.takeMemory]) — but
// an approved rule or confirmed decision must be in front of the worker before
// it acts, and a router outage must not erase one. It is bounded and
// owner-filtered ([Agent.bindingMemories]) and READ-ONLY: it reads the lent
// store ([Config.bindingStore], which is never [Config.Memory]) and writes
// nothing, so no worker is granted a memory write, extraction, import, the
// remember/forget verbs, or any future-task authority. A conversation (which
// owns a brain) and a worker with nothing lent both return immediately, opening
// exactly as they did.
func (a *Agent) prepareWorkerBinding(ctx context.Context, cue string) {
	if a.memory != nil || a.config.bindingStore == nil {
		return
	}
	rules := renderBindingBlock(a.bindingMemories(a.config.bindingStore, cue, ""))
	// AND THE RELEVANT PRIOR OUTCOMES, READ-ONLY, FROM THE SAME LENT STORE AND
	// THE SAME FROZEN PROJECT KEY the rules were read under. A worker has no
	// memory writer and no brain (its [Agent.remembers] is false), so this read
	// borrows the store and the key it was handed and writes nothing: no
	// remember, forget, import or any other verb is granted, and the worker
	// still owns no memory and no future-task authority. The read is bounded and
	// owner-filtered on the existing journal ([Agent.priorOutcomeBlock]).
	outcomes := a.priorOutcomeBlock(a.config.bindingStore, a.config.MemoryProjectKey, cue, a.workerSourceSnapshot(ctx))
	// THE BINDING RULES ARE MANDATORY AND RESERVED FIRST; the relevant prior
	// outcomes are advisory and spend only what is left of the one shared
	// ceiling, by WHOLE records.
	block := composeBeforeRequestContext(rules, "", outcomes, "")
	if block == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.bindingText = block
	// THE BINDING IS MANDATORY AND RESERVED FIRST; whatever routed memory has
	// already arrived is bounded to what is LEFT of the one shared ceiling.
	a.reserveBindingFirstLocked()
	a.landVolatileLocked()
}

// workerSourceSnapshot is the worker's OWN bounded source identity, captured
// from the worker's own workspace before its first provider request. It is the
// tree the worker will actually act in — NEVER a snapshot borrowed from the
// conversation, whose workspace may have moved under a later anchor, and never
// a stale scope re-bound onto a project the worker is not standing in. The
// capture is trusted ONLY while the frozen [Config.MemoryProjectKey] still
// names this worker's own workspace, so a key inherited from another project
// leaves the label honestly unknown rather than certifying another tree. A
// capture that is unknown, truncated or raced is unknown, and a worker with no
// provable project is unknown too.
func (a *Agent) workerSourceSnapshot(ctx context.Context) string {
	key := strings.TrimSpace(a.config.MemoryProjectKey)
	if key == "" {
		return "unknown"
	}
	if own := anchoredProjectKey(strings.TrimSpace(a.config.Workspace)); own == "" || own != key {
		return "unknown"
	}
	snapshot := a.captureSourceSnapshot(ctx)
	if !snapshot.current() {
		return "unknown"
	}
	return snapshot.Identity
}

// reserveBindingFirstLocked bounds a task worker's TWO moving blocks under ONE
// shared [memoryBlockRunes] ceiling rather than each claiming it. The approved
// binding block is MANDATORY and is reserved whole ([prepareWorkerBinding]); the
// asynchronous semantic block is the OPTIONAL remainder and is trimmed to what
// is left by whole records, so neither block is ever cut mid-sentence. The
// caller holds a.mu.
func (a *Agent) reserveBindingFirstLocked() {
	binding := strings.TrimSpace(a.bindingText)
	if binding == "" {
		return
	}
	limit := memoryBlockRunes - utf8.RuneCountInString(binding)
	if limit < 0 {
		limit = 0
	}
	a.memoryText = trimRenderedMemoryBlock(strings.TrimSpace(a.memoryText), limit)
}

// composeBeforeRequestContext assembles the blocks a request opens with under
// the ONE shared [memoryBlockRunes] ceiling. The approved binding RULES are
// mandatory and are reserved FIRST, whole; the contextual impacts, the relevant
// prior outcomes and the asynchronous routed recall are optional and each spends
// only what is left, by WHOLE records, so no record is ever clipped mid-sentence
// and no wrapper is ever left open. An optional block that cannot show at least
// one whole record is omitted WHOLE rather than cut into a fragment: an honest
// absence is preferable to a malformed record.
func composeBeforeRequestContext(rules, impacts, outcomes, recall string) string {
	var b strings.Builder
	b.WriteString(rules)
	remaining := memoryBlockRunes - utf8.RuneCountInString(rules)
	if remaining < 0 {
		remaining = 0
	}
	for _, part := range []struct{ block, open, close string }{
		{impacts, "<contextual_impacts>", "</contextual_impacts>"},
		{outcomes, "<prior_outcomes>", "</prior_outcomes>"},
	} {
		if part.block == "" {
			continue
		}
		kept := trimRenderedWholeRecords(part.block, part.open, part.close, remaining)
		b.WriteString(kept)
		remaining -= utf8.RuneCountInString(kept)
		if remaining < 0 {
			remaining = 0
		}
	}
	if recall != "" {
		b.WriteString(trimRenderedMemoryBlock(strings.TrimSpace(recall), remaining))
	}
	return b.String()
}

// trimRenderedWholeRecords drops trailing whole RECORDS from a rendered optional
// block so it fits limit runes, keeping the block's wrapper and its one preamble
// line intact. The records are the lines after the first, and a prior failure
// rides with its observed alternative on the SAME line, so the pair is never
// separated. A block whose shape is unknown, whose preamble does not fit, or
// that would be left with no record at all is omitted WHOLE: neither an
// instruction fragment nor a mutated record is ever emitted.
func trimRenderedWholeRecords(block, openTag, closeTag string, limit int) string {
	if block == "" || limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(block) <= limit {
		return block
	}
	start := strings.Index(block, openTag)
	if start < 0 || !strings.HasSuffix(block, closeTag+"\n") {
		return ""
	}
	lead := block[:start]
	inner := strings.Trim(block[start+len(openTag):len(block)-len(closeTag)-1], "\n")
	lines := strings.Split(inner, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return ""
	}
	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		candidate := lead + openTag + "\n" + strings.Join(append(append([]string(nil), kept...), line), "\n") + "\n" + closeTag + "\n"
		if utf8.RuneCountInString(candidate) > limit {
			if i == 0 {
				// Even the preamble alone does not fit: omit the block whole.
				return ""
			}
			break
		}
		kept = append(kept, line)
	}
	if len(kept) <= 1 {
		// The preamble with no record says nothing; omit it whole.
		return ""
	}
	return lead + openTag + "\n" + strings.Join(kept, "\n") + "\n" + closeTag + "\n"
}

// trimRenderedMemoryBlock drops trailing RECORDS from a rendered <memory> block
// until it fits limit runes, keeping whole lines and the block's own wrapper. A
// block whose shape is unknown is omitted WHOLE rather than cut: an honest
// absence is the contract, a mutated record is not.
func trimRenderedMemoryBlock(block string, limit int) string {
	if block == "" {
		return ""
	}
	if utf8.RuneCountInString(block) <= limit {
		return block
	}
	if limit <= 0 {
		return ""
	}
	const (
		openTag  = "<memory>"
		closeTag = "</memory>"
	)
	start := strings.Index(block, openTag)
	if start < 0 || !strings.HasSuffix(block, closeTag) {
		return ""
	}
	inner := block[start+len(openTag) : len(block)-len(closeTag)]
	lines := strings.Split(strings.Trim(inner, "\n"), "\n")
	var kept []string
	for _, line := range lines {
		candidate := openTag + "\n" + strings.Join(append(append([]string(nil), kept...), line), "\n") + "\n" + closeTag
		if utf8.RuneCountInString(candidate) > limit {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return ""
	}
	return openTag + "\n" + strings.Join(kept, "\n") + "\n" + closeTag
}

// rebindAfterAnchor recomputes a conversation's binding block for the project it
// has just been anchored to and lands it as a FRESH APPEND-ONLY note, so the next
// action-capable request carries the ANCHORED repository's approved rules and
// confirmed decisions rather than the scratch project's stale block. It is a
// bounded local read (it asks no provider) and it rewrites nothing already said:
// [Agent.landVolatileLocked] appends a new note, and the last such note is the
// one that holds. The caller holds a.mu.
//
// AND THE OWNER CHANGE INVALIDATES EVERY OWNER-RELATIVE STATE THAT WAS EARNED
// UNDER THE OLD ONE. A failure demonstrated under the scratch owner BEFORE this
// anchor must never pair a success earned under the anchored repository as its
// observed alternative: that would fabricate a causal conclusion across two
// owners and attach a receipt to a project that never earned it. So the pending
// failure and its unspent alternative slot are cleared HERE, in the one place the
// owner changes mid-turn, under the held a.mu and the brain's own lock. The
// cached impact reading is cleared for the same reason: it was computed against
// the scratch owner's dependency edges, and a cached block re-read within the same
// turn would carry that scratch consequence into the repository the conversation
// has just moved to.
//
// THE PAIRING RESET IS ONE RULE, NOT TWO. This path already holds a.memory.mu,
// so it calls the locked form of the reset in contextual_outcome.go
// ([Agent.invalidateOutcomePairingLocked]) rather than clearing the fields itself:
// the anchor and an explicit owner transition can then never drift apart. What is
// reset is exactly the state keyed to the old owner, and nothing else the turn has
// already said is rewritten.
func (a *Agent) rebindAfterAnchor() {
	if !a.remembers() {
		return
	}
	a.memory.mu.Lock()
	cue := a.memory.outcomeGoal
	turn := a.turnSeq
	revision := a.memory.revisions[turn]
	// THE PRE-ANCHOR PAIRING DOES NOT SURVIVE THE ANCHOR. Dropping the key also
	// drops the turn's pending causal claim; a post-anchor success must start a
	// fresh pairing under the repository's owner or none at all.
	a.invalidateOutcomePairingLocked()
	// AND THE SCRATCH IMPACT READING GOES WITH IT: the cached block, its cue and
	// its held notices were all owner-relative and must be recomputed against the
	// repository on the next request rather than replayed from the scratch owner.
	a.memory.impactTurn = 0
	a.memory.impactPrepared = false
	a.memory.impactBlock = ""
	a.memory.impactCue = ""
	a.memory.impactNotices = nil
	a.memory.impactOrder = nil
	a.memory.mu.Unlock()
	block := a.bindingContext(cue, revision)
	a.memoryText = block
	a.landVolatileLocked()
}

func (a *Agent) withBindingContext(block, cue string) string {
	a.mu.Lock()
	turn := a.turnSeq
	a.mu.Unlock()
	a.memory.mu.Lock()
	revision := a.memory.revisions[turn]
	a.memory.mu.Unlock()
	rules := a.bindingContext(cue, revision)
	impacts := a.contextualImpactContext(cue)
	outcomes := a.priorOutcomeContext(cue, revision)
	if rules == "" && impacts == "" && outcomes == "" {
		return block
	}
	// One shared ceiling includes deterministic and optional routed context: the
	// approved rules are reserved whole FIRST, and the impacts, the prior
	// outcomes and the asynchronous routed recall each spend only what is left,
	// by whole records.
	return composeBeforeRequestContext(rules, impacts, outcomes, block)
}

func (a *Agent) bindingContext(cue, revision string) string {
	return renderBindingBlock(a.bindingMemories(a.memory.store, cue, revision))
}

// bindingMemories is THE bounded, owner-filtered read of the authority rows a
// conversation or a task worker must see before acting: approved rules and
// confirmed decisions first ([store.Store.ContextualEvidenceApproved]), then a
// provider-free lexical fallback. It is parameterized on the store so one
// projection serves both the conversation's own brain and a worker's read-only
// lent one ([Agent.prepareWorkerBinding]); it writes nothing and asks no
// provider, so a router outage cannot erase an approved rule.
func (a *Agent) bindingMemories(st *store.Store, cue, revision string) []store.Memory {
	var memories []store.Memory
	seen := map[string]bool{}
	conditions := map[string]string{"project": a.config.MemoryProjectKey, "revision": revision}
	for _, owner := range a.memoryOwners() {
		// THE BINDING PROJECTION SPENDS ITS WINDOW ON AUTHORITY, NOT ON NOISE.
		// A burst of newer incidental observations must not push a rare
		// approved rule out of the read; the same latest/suppression/expiry
		// guards still decide whether each one is live.
		evidence, err := st.ContextualEvidenceApproved(owner, conditions, time.Now(), contextualContextLimit)
		if err != nil {
			continue
		}
		for _, e := range evidence {
			if e.Authority != "approved_rule" && e.Authority != "confirmed_decision" {
				continue
			}
			latest, err := st.ContextualEvidenceForMemory(owner, e.MemoryID)
			if err != nil || latest.Seq != e.Seq || (latest.Authority != "approved_rule" && latest.Authority != "confirmed_decision") {
				continue
			}
			rows, err := st.GetMemories([]string{owner}, []string{e.MemoryID})
			rows = a.contextualEligibleWith(st, rows, revision)
			if err == nil && len(rows) > 0 && !seen[e.MemoryID] {
				memories = append(memories, rows[0])
				seen[e.MemoryID] = true
			}
			if len(memories) >= contextualContextLimit {
				break
			}
		}
		if len(memories) >= contextualContextLimit {
			break
		}
	}
	// THE LEXICAL FALLBACK IS NOT AN AUTHORITY FILTER. A direct word match
	// supplies useful history without asking a provider, but SearchMemories
	// ranks by relevance and knows nothing about a record's authority, so a
	// plain observation that merely shares words with the goal can reach this
	// window. Each such record is therefore LABELLED ON ITSELF: a candidate
	// whose own latest journal row is not an approved rule or a confirmed
	// decision rides marked "history only", so the block's opening sentence is
	// never the only place its authority is described and a prior user TASK
	// request can no longer be read as a current rule or decision. A nonmatching
	// project and a trivial continuation stay quiet.
	if !memoryTrivialCue(cue) && len(memories) < contextualContextLimit {
		candidates, err := st.SearchMemories(a.memoryOwners(), cue, contextualContextLimit-len(memories))
		if err == nil {
			for _, m := range a.contextualEligibleWith(st, candidates, revision) {
				if seen[m.ID] {
					continue
				}
				if !a.contextualRecordAuthoritative(st, m, revision) {
					m.Text = bindingAdvisoryLabel + "\n" + m.Text
				}
				memories = append(memories, m)
				seen[m.ID] = true
				if len(memories) >= contextualContextLimit {
					break
				}
			}
		}
	}
	return memories
}

// bindingAdvisoryLabel marks a record the binding projection could NOT establish
// as an approved rule or a confirmed decision. It rides on the record itself so
// a reader can tell quoted historical provenance apart from binding authority
// even when both share one <memory> block.
const bindingAdvisoryLabel = "History only \u2014 not an approved rule or a confirmed decision: an earlier turn's own words, kept as provenance and not authority for new work."

// contextualRecordAuthoritative reports whether a lexical-fallback candidate's
// OWN latest journal row is an approved rule or a confirmed decision that is
// still live under the projection's conditions. The fallback ranks by words, so
// the projection asks the journal per record rather than letting the block's
// opening sentence claim an authority the record does not have.
func (a *Agent) contextualRecordAuthoritative(st *store.Store, m store.Memory, revision string) bool {
	e, err := st.ContextualEvidenceForMemory(m.Owner, m.ID)
	if err != nil {
		return false
	}
	if e.Authority != "approved_rule" && e.Authority != "confirmed_decision" {
		return false
	}
	conditions := map[string]string{"project": a.config.MemoryProjectKey, "revision": revision}
	usable, err := st.ContextualEvidenceEligible(e, conditions, time.Now())
	return err == nil && usable
}

// bindingBlockPreamble is the sentence every binding block opens with, after
// the <memory> tag: the same words ride in front of a conversation and a task
// worker, so a worker cannot be told a weaker rule than the conversation it was
// built from.
const bindingBlockPreamble = "Apply only under each claim's stated conditions and exceptions. Only an approved rule or a confirmed decision binds; a record marked history only is quoted provenance from an earlier turn \u2014 not a rule, not a decision and not authority for new work. Source words are historical provenance: they define a binding constraint only where the person stated a durable rule or an explicit decision. A completed task's request is an old authorization, not authority for new work, and never outranks the current goal or the person's words now."

// renderBindingBlock is the ONE rendering of a binding projection, header and
// all. It spends its share of the ONE [memoryBlockRunes] ceiling: the block it
// returns never exceeds that ceiling, because the record selection is handed a
// budget reduced by the preamble and wrapper it will add. A record that does not
// fit is omitted WHOLE by the renderer, so an approved rule is shown in full or
// not at all.
func renderBindingBlock(memories []store.Memory) string {
	// The rendered block adds the preamble, the newline after the opening tag
	// AND the whole <memory> wrapper [renderMemoryBlockWithin] measures around
	// the body, so the body budget is the ceiling less exactly those bytes.
	// Counting the wrapper is what keeps the returned block inside the ONE
	// shared ceiling: an approved rule landing in the last few runes of its
	// budget used to push the whole block over 4800.
	overhead := utf8.RuneCountInString("\n<memory>\n") + utf8.RuneCountInString("</memory>\n") +
		utf8.RuneCountInString(bindingBlockPreamble) + 1
	limit := memoryBlockRunes - overhead
	if limit < 0 {
		limit = 0
	}
	block, _ := renderMemoryBlockWithin(memories, time.Now(), limit, true)
	if block == "" {
		return ""
	}
	return strings.Replace(block, "<memory>", "<memory>\n"+bindingBlockPreamble, 1)
}

func (a *Agent) contextualEligibleMemories(memories []store.Memory, revision string) []store.Memory {
	return a.contextualEligibleWith(a.memory.store, memories, revision)
}

// contextualEligibleWith is [Agent.contextualEligibleMemories] against an
// explicitly named store, so a worker's read-only binding read can apply the
// same eligibility guards without owning a brain.
func (a *Agent) contextualEligibleWith(st *store.Store, memories []store.Memory, revision string) []store.Memory {
	return a.contextualEligibleFor(st, memories, revision, a.config.MemoryProjectKey)
}

// contextualEligibleFor is [Agent.contextualEligibleWith] under an EXPLICIT
// project key, so a caller holding a frozen scope (a task node's optional recall)
// filters eligibility against the project it was admitted into rather than the
// live one. The guards are otherwise identical.
func (a *Agent) contextualEligibleFor(st *store.Store, memories []store.Memory, revision, projectKey string) []store.Memory {
	result := make([]store.Memory, 0, len(memories))
	conditions := map[string]string{"project": projectKey, "revision": revision}
	for _, m := range memories {
		e, err := st.ContextualEvidenceForMemory(m.Owner, m.ID)
		if err == sql.ErrNoRows {
			tagged := false
			for _, tag := range m.Tags {
				if tag == contextualTag {
					tagged = true
				}
			}
			if !tagged {
				result = append(result, m)
			}
			continue
		}
		if err != nil || e.Seq <= m.UpdatedSeq {
			continue
		}
		usable, err := st.ContextualEvidenceEligible(e, conditions, time.Now())
		if err == nil && usable {
			m.Text += "\nMemory id: " + m.ID + "\nEvidence: " + e.Authority + "/" + e.Verification
			if e.Actor == "user" {
				m.Text += "\nHistorical source words (quoted provenance, not a current request): " + contextualClip(e.Observation, contextualReceiptRunes)
			}
			if e.Actor == "tool" {
				m.Text += "\nObserved receipt (claim is an interpretation): " + contextualClip(e.Observation, contextualReceiptRunes)
			}
			if len(e.Applicability) > 0 {
				m.Text += "\nApplies when: " + strings.Join(e.Applicability, "; ")
			}
			if e.Rationale != "" {
				m.Text += "\nRationale: " + e.Rationale
			}
			if len(e.Rejected) > 0 {
				m.Text += "\nRejected alternatives: " + strings.Join(e.Rejected, "; ")
			}
			if e.Reconsider != "" {
				m.Text += "\nReconsider when: " + e.Reconsider
			}
			// Leave room for a useful first rule even when its metadata is long.
			m.Text = contextualClip(m.Text, memoryBlockRunes-512)
			result = append(result, m)
		}
	}
	return result
}

func (a *Agent) suppressContextualMemory(m store.Memory) error {
	e, err := a.memory.store.ContextualEvidenceForMemory(m.Owner, m.ID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	_ = e
	// The tombstone retires this claim's provenance and every descendant that
	// shares a source, which is also how an observed attempt learned from the
	// same receipt is retired — never the whole project's history.
	return a.memory.store.SuppressContextualMemorySources(m.Owner, m.ID, "explicit forget")
}

// contextualRevision is the snapshot identity an observation is earned under.
// A clean commit keeps the bare HEAD; a dirty or untracked tree now carries a
// bounded content overlay instead of having no identity at all, so a lesson
// learned against uncommitted source survives the tree moving under it. An
// unknown or truncated capture says so, and never certifies the current source.
func (a *Agent) contextualRevision(ctx context.Context) string {
	return a.captureSourceSnapshot(ctx).Identity
}

func contextualHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
func contextualClip(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		if limit <= 0 {
			return ""
		}
		return string(r[:limit-1]) + "…"
	}
	return s
}

// These conservative gates grant binding priority only to explicit wording.
// The original utterance remains alongside the interpretation for exceptions.
func explicitContextualRule(text string) bool {
	lower := " " + strings.ToLower(text) + " "
	for _, cue := range []string{" must ", " never ", " do not ", " don't ", " only ", " always ", " require ", " no network "} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}
func explicitContextualDecision(text string) bool {
	lower := strings.ToLower(text)
	for _, cue := range []string{"we decided", "i decided", "we chose", "i chose", "let's use", "use the", "instead of", "because"} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// contextualOneTurnTaskAuthorization reports a span that directs the agent to
// carry out ONE bounded piece of work now — cross-check, compare, verify, run or
// read something, usually with a read-only or one-off frame — rather than
// stating a lasting rule or a decision the person has made. It answers the
// source gap F3 names: the authority gates read ordinary wording cues, a one-turn
// task uses those same words, and an extractor that mislabels the task would
// otherwise promote it to a persistent binding. The check is deliberately
// narrow and textual, and it never demotes a span that ALSO states a lasting
// quantifier ("always", "never", "must") or a durable decision ("we decided",
// "instead of"), so a genuine durable rule or an explicit decision from the
// same turn still binds; an explicit one-off scope ("for this task", "for now")
// still bounds that span and keeps the demotion.
func contextualOneTurnTaskAuthorization(clause string) bool {
	lower := strings.ToLower(clause)
	// A CLEARLY BOUNDED ONE-TURN FRAME demotes on its own. A cross-check, an
	// independent verification, a read-only run, a "keep this" hold or an
	// explicit one-off scope ("for now", "for this task") names the work of
	// this turn, so it does not become a lasting rule however else it is
	// worded; a lasting quantifier in the same span does not rescue it.
	for _, cue := range []string{
		"cross-check", "cross check", "independently ", "read-only", "read only",
		"one-off", "one off", "for now", "for this task", "for this turn",
		"for this check", "keep this",
	} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	// AN ORDINARY TASK VERB ALONE DOES NOT DEMOTE A LASTING RULE. "run the",
	// "check the", "verify the" and "compare the" are the same words a durable
	// constraint uses ("Never run the ledger utility without approval",
	// "Always check the ledger before every release"). A span that also states
	// a lasting quantifier ("always", "never", "must") or a durable decision
	// keeps its authority; only a bare task command is a one-turn
	// authorization, and an explicit one-off scope still bounds it.
	task := false
	for _, cue := range []string{"run the", "re-run", "double-check", "verify the", "check the", "compare the"} {
		if strings.Contains(lower, cue) {
			task = true
			break
		}
	}
	if !task {
		return false
	}
	for _, durable := range []string{"we decided", "i decided", "we chose", "i chose", "we agreed", "i agreed", "let's", "instead of"} {
		if strings.Contains(lower, durable) {
			return false
		}
	}
	// A LASTING QUANTIFIER keeps the span authoritative; word boundaries stop
	// "whenever" or "mustard" standing in for "never"/"must".
	padded := " " + lower + " "
	for _, lasting := range []string{" always ", " never ", " must "} {
		if strings.Contains(padded, lasting) {
			return false
		}
	}
	return true
}

// contextualSanitizedItems is contextualItems with the secret redactor applied
// to every retained condition or rejected alternative.
func contextualSanitizedItems(items []string) []string {
	out := contextualItems(items)
	for i := range out {
		out[i] = redact.Secrets(out[i])
	}
	return out
}

func contextualItems(items []string) []string {
	if len(items) > 8 {
		items = items[:8]
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, contextualClip(item, 240))
	}
	return result
}

// contextualSupportClause answers the sentence the supporting quote actually
// sits in, terminator included. A question mark after the quote is part of the
// question, so it has to be inside the span the directive check reads.
func contextualSupportClause(user, quote string) string {
	idx := strings.Index(user, quote)
	if idx < 0 {
		return quote
	}
	start := idx
	for start > 0 {
		switch rune(user[start-1]) {
		case '.', '!', '?', '\n', ';':
			goto done
		}
		start--
	}
done:
	end := idx + len(quote)
	for end < len(user) {
		end++
		switch rune(user[end-1]) {
		case '.', '!', '?', '\n', ';':
			return user[start:end]
		}
	}
	return user[start:end]
}

// contextualSpanDirective answers whether a clause is the person asserting a
// rule rather than asking about one or reporting one they rejected. It is
// deliberately textual and conservative: ambiguity demotes to a proposal.
func contextualSpanDirective(clause string) bool {
	if strings.Contains(clause, "?") {
		return false
	}
	lower := strings.ToLower(clause)
	for _, cue := range []string{"should we", "should i", "should the", "do we", "do you", "did we", "did you", "can we", "could we", "would we", "is it", "are we", "what if", "why should", "any reason"} {
		if strings.Contains(lower, cue) {
			return false
		}
	}
	for _, cue := range []string{"reject", "refuse", "disagree", "not going to", "no longer", "don't want", "do not want", "according to", "reviewer", "someone said", "they said", "he said", "she said", "claimed", "reported that"} {
		if strings.Contains(lower, cue) {
			return false
		}
	}
	return true
}

// userQuoteForScope answers the literal span a scope promotion must be argued
// from. When the caller still holds the verified supporting quote it is that
// span alone; otherwise it falls back to the whole turn, which is the only
// string a non-user candidate can be checked against.
func userQuoteForScope(c reflex.ExtractResult, user string) string {
	if q := strings.TrimSpace(c.SourceQuote); q != "" && strings.Contains(user, q) {
		return q
	}
	return user
}

// contextualDerivedReceipt names the tools whose output is a SUMMARY, a HISTORY
// lookup or a memory read rather than an independent observation of the world.
// Citing one back cannot corroborate a claim: the model already read or wrote
// it, and a fresh reader receipt id would let suppressed history re-enter as
// observed proof.
func contextualDerivedReceipt(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "search_conversations", "recall", "read_task", "tasks", "memory_evidence":
		return true
	}
	return false
}

func explicitContextualGlobal(text string) bool {
	lower := strings.ToLower(text)
	// A PROJECT-QUALIFIED "everywhere" IS PROJECT SCOPE. "everywhere in this
	// project" is the ordinary way to say the fact spans one repository, and
	// the earlier substring match promoted it to the person at large.
	for _, qualifier := range []string{"everywhere in this project", "everywhere in the project", "everywhere in this repo", "everywhere in this repository", "everywhere in this codebase"} {
		if strings.Contains(lower, qualifier) {
			return false
		}
	}
	for _, cue := range []string{"across projects", "all projects", "every project", "any project", "everywhere", "personal preference"} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// explicitContextualMachine grants machine scope only to a self-contained span
// that says the rule applies to the machine, not to one project.
func explicitContextualMachine(text string) bool {
	lower := strings.ToLower(text)
	for _, cue := range []string{"on this machine", "this machine", "on my machine", "my machine", "machine-wide", "machine wide"} {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// Evidence is fetched selectively under the same owner and validity guards as
// recall. Forgotten sources are never reintroduced through this second read.
func (a *Agent) contextualEvidenceTools() []bare.Tool {
	if !a.remembers() {
		return nil
	}
	return []bare.Tool{{Name: "memory_evidence", Description: "Read a saved memory's sources, conditions and rationale. Old tool results are historical; proposals do not confer user approval.", Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`), Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
		var request struct {
			ID string `json:"id"`
		}
		if err := decodeToolArguments(args, &request); err != nil {
			return "Invalid arguments: " + err.Error(), true, nil
		}
		rows, err := a.memory.store.GetMemories(a.memoryOwners(), []string{request.ID})
		if err != nil {
			return "Could not read that supporting source", true, nil
		}
		if len(rows) == 0 {
			return "That saved claim is not available here", false, nil
		}
		e, err := a.memory.store.ContextualEvidenceForMemory(rows[0].Owner, request.ID)
		if err != nil {
			return "That claim has no supporting evidence record; treat it as an old assertion", false, nil
		}
		revision := a.contextualRevision(ctx)
		usable, err := a.memory.store.ContextualEvidenceEligible(e, map[string]string{"project": a.config.MemoryProjectKey, "revision": revision}, time.Now())
		if err != nil || !usable || e.Seq <= rows[0].UpdatedSeq {
			return "That supporting source is no longer applicable; inspect the current work", false, nil
		}
		encoded, err := json.Marshal(e)
		if err != nil {
			return "Could not read that supporting source", true, nil
		}
		return contextualClip(string(encoded), memoryBlockRunes), false, nil
	}}}
}
