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

	"github.com/Agent-Field/agentfield/sdk/go/ai"
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
	Receipts                      []memoryToolReceipt
	At                            time.Time
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
		directive := contextualSpanDirective(contextualSupportClause(s.User, quote))
		if c.Authority == "approved_rule" && (!explicitContextualRule(quote) || !directive) {
			c.Authority = "observation"
		}
		if c.Authority == "confirmed_decision" && (!explicitContextualDecision(quote) || !directive) {
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
	a.memory.mu.Unlock()
	block := a.bindingContext(cue, revision)
	block += a.contextualImpactContext(cue)
	block += a.priorOutcomeContext(cue, revision)
	// The shared ceiling is a promise about the system prompt: the rules, the
	// impacts and any prior outcome all ride inside it together.
	block = contextualClip(block, memoryBlockRunes)
	a.mu.Lock()
	a.memoryText = block
	a.landVolatileLocked()
	a.mu.Unlock()
}

func (a *Agent) withBindingContext(block, cue string) string {
	a.mu.Lock()
	turn := a.turnSeq
	a.mu.Unlock()
	a.memory.mu.Lock()
	revision := a.memory.revisions[turn]
	a.memory.mu.Unlock()
	bound := a.bindingContext(cue, revision)
	bound += a.contextualImpactContext(cue)
	bound += a.priorOutcomeContext(cue, revision)
	if bound == "" {
		return block
	}
	// One shared ceiling includes deterministic and optional routed context.
	return contextualClip(bound+block, memoryBlockRunes)
}

func (a *Agent) bindingContext(cue, revision string) string {
	var memories []store.Memory
	seen := map[string]bool{}
	conditions := map[string]string{"project": a.config.MemoryProjectKey, "revision": revision}
	for _, owner := range a.memoryOwners() {
		// THE BINDING PROJECTION SPENDS ITS WINDOW ON AUTHORITY, NOT ON NOISE.
		// A burst of newer incidental observations must not push a rare
		// approved rule out of the read; the same latest/suppression/expiry
		// guards still decide whether each one is live.
		evidence, err := a.memory.store.ContextualEvidenceApproved(owner, conditions, time.Now(), contextualContextLimit)
		if err != nil {
			continue
		}
		for _, e := range evidence {
			if e.Authority != "approved_rule" && e.Authority != "confirmed_decision" {
				continue
			}
			latest, err := a.memory.store.ContextualEvidenceForMemory(owner, e.MemoryID)
			if err != nil || latest.Seq != e.Seq || (latest.Authority != "approved_rule" && latest.Authority != "confirmed_decision") {
				continue
			}
			rows, err := a.memory.store.GetMemories([]string{owner}, []string{e.MemoryID})
			rows = a.contextualEligibleMemories(rows, revision)
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
	// A direct lexical match supplies useful fallback without asking a provider.
	// A nonmatching project and a trivial continuation stay quiet.
	if !memoryTrivialCue(cue) && len(memories) < contextualContextLimit {
		candidates, err := a.memory.store.SearchMemories(a.memoryOwners(), cue, contextualContextLimit-len(memories))
		if err == nil {
			for _, m := range a.contextualEligibleMemories(candidates, revision) {
				if !seen[m.ID] {
					memories = append(memories, m)
					seen[m.ID] = true
				}
			}
		}
	}
	block, _ := renderMemoryBlock(memories, time.Now())
	if block != "" {
		block = strings.Replace(block, "<memory>", "<memory>\nApply only under each claim's stated conditions and exceptions. Source words take precedence over interpretations. Proposals and old observations are not approved rules or current test proof. An opportunity does not authorize new work.", 1)
	}
	return block
}

func (a *Agent) contextualEligibleMemories(memories []store.Memory, revision string) []store.Memory {
	result := make([]store.Memory, 0, len(memories))
	conditions := map[string]string{"project": a.config.MemoryProjectKey, "revision": revision}
	for _, m := range memories {
		e, err := a.memory.store.ContextualEvidenceForMemory(m.Owner, m.ID)
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
		usable, err := a.memory.store.ContextualEvidenceEligible(e, conditions, time.Now())
		if err == nil && usable {
			m.Text += "\nMemory id: " + m.ID + "\nEvidence: " + e.Authority + "/" + e.Verification
			if e.Actor == "user" {
				m.Text += "\nSource words: " + contextualClip(e.Observation, contextualReceiptRunes)
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
