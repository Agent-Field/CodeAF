package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
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
}
type memoryTurnEvidence struct {
	Session, Turn, User, Revision string
	Receipts                      []memoryToolReceipt
	At                            time.Time
}

// recordMemoryTool retains independent receipts, including failure and refusal.
// Assistant summaries never turn into additional corroborating observations.
func (a *Agent) recordMemoryTool(turn uint64, call ai.ToolCall, result toolResult) {
	if !a.remembers() {
		return
	}
	receipt := memoryToolReceipt{ID: call.ID, Tool: call.Function.Name, Text: contextualClip(result.text, contextualReceiptRunes), Status: toolStatus(result)}
	receipt = a.contextualReadReceipt(call, result, receipt)
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
	if c.Scope == store.MemoryScopeEnv && !userSupported {
		c.Scope = store.MemoryScopeProject
	}
	if !userSupported {
		c.Source = "assistant"
		c.Authority = "proposal"
		for _, r := range s.Receipts {
			if c.ReceiptID == r.ID && r.Status != "refused" {
				c.Source = "tool"
				c.Authority = "observation"
				break
			}
		}
	} else {
		// An extractor label cannot manufacture approval from a quoted "yes".
		// Binding requires an explicit rule in the user's actual utterance.
		if c.Authority == "approved_rule" && !explicitContextualRule(s.User) {
			c.Authority = "observation"
		}
		if c.Authority == "confirmed_decision" && !explicitContextualDecision(s.User) {
			c.Authority = "observation"
		}
		if c.Authority != "approved_rule" && c.Authority != "confirmed_decision" {
			c.Authority = "observation"
		}
		quote = contextualClip(s.User, contextualReceiptRunes)
	}
	if c.Scope == store.MemoryScopeUser && !explicitContextualGlobal(s.User) {
		c.Scope = store.MemoryScopeProject
	}
	// Rich conditions and rationale are kept in evidence, not squeezed into the
	// memory row's established 512-rune body. Settlement keeps this marker.
	c.Text = contextualClip(c.Text, store.MemoryTextRunes)
	c.Tags = append(c.Tags, contextualTag)
	return c
}

func (a *Agent) recordContextualMemory(m store.Memory, c reflex.ExtractResult, s memoryTurnEvidence) error {
	owner := a.ownerForScope(c.Scope)
	// An update result can omit owner; the authority still comes from the same
	// candidate partition used by settlement, never from the session's read set.
	e := store.ContextualEvidence{ID: store.NewMemoryID(), MemoryID: m.ID, Owner: owner, SessionID: s.Session, TurnID: s.Turn, Actor: c.Source, Authority: c.Authority, Observation: contextualClip(s.User, 4000), Verification: "asserted", ValidFrom: s.At, SourceKey: s.Session + ":" + s.Turn, SourceHash: contextualHash(s.User), Applicability: contextualItems(c.Conditions), Rationale: contextualClip(c.Rationale, 240), Rejected: contextualItems(c.Rejected), Reconsider: contextualClip(c.Reconsider, 240)}
	if owner != store.OwnerUser {
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
				break
			}
		}
		e.Revision = s.Revision
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
	revision := a.contextualRevision(ctx)
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
	a.memory.mu.Unlock()
	block := a.bindingContext(cue, revision)
	block += a.contextualImpactContext(cue)
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
		evidence, err := a.memory.store.ContextualEvidenceApplicable(owner, conditions, time.Now(), contextualContextLimit)
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
	return a.memory.store.SuppressContextualMemorySources(m.Owner, m.ID, "explicit forget")
}

func (a *Agent) contextualRevision(ctx context.Context) string {
	if strings.TrimSpace(a.config.Workspace) == "" {
		return ""
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, "git", "-C", a.config.Workspace, "rev-parse", "HEAD")
	raw, err := command.Output()
	if err != nil {
		return ""
	}
	revision := strings.TrimSpace(string(raw))
	dirty := exec.CommandContext(bounded, "git", "-C", a.config.Workspace, "status", "--porcelain", "--untracked-files=normal")
	status, err := dirty.Output()
	if err != nil || len(status) != 0 {
		// A clean commit identifies the entire tracked snapshot. A dirty or
		// untracked source has no such identity, so its results need a fresh read.
		return ""
	}
	return revision
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

func explicitContextualGlobal(text string) bool {
	lower := strings.ToLower(text)
	for _, cue := range []string{"across projects", "all projects", "every project", "any project", "everywhere", "personal preference"} {
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
