package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// priorOutcomeLimit is how many prior attempts a single turn may be shown. Two
// is a caution, not a wall of history, and it rides inside the same
// [memoryBlockRunes] ceiling as the rules and the impacts.
const priorOutcomeLimit = 2

// recordMemoryAttempt writes one independently observed outcome at the
// executeTool boundary, BEFORE any model has been asked what the turn meant. It
// is the reason a real failure outlives the turn that produced it: the evidence
// buffer is turn-scoped, but this row is in the canonical journal, so a single
// extraction candidate or a rollover cannot erase it.
//
// IT LEARNS FROM FAILURE AND BLOCKING ONLY. A successful call is not stored —
// saving every shell success is the noise contract 6 refuses — and the status
// comes from the tool boundary's own reading, never from model text: a refusal
// is blocked/unknown, never a demonstrated failure, and no overall exit code
// proves a sub-check passed.
func (a *Agent) recordMemoryAttempt(ctx context.Context, turn uint64, call ai.ToolCall, result toolResult, preSnapshot string) {
	if !a.remembers() {
		return
	}
	status := attemptOutcomeStatus(result)
	if status == "" {
		return
	}
	if !attemptWorthStoring(call, result) {
		return
	}
	action := attemptAction(call)
	if action == "" {
		return
	}
	// The receipt bytes are the SAME clip the evidence path hashes, so the two
	// rows share a source key and an explicit forget retires both together. The
	// stored observation is redacted and untrusted text; the hash is identity.
	receipt := contextualClip(result.text, contextualReceiptRunes)
	if strings.TrimSpace(receipt) == "" {
		return
	}
	// THE CALL'S OWN CIRCUMSTANCES, BEFORE AND AFTER. A snapshot taken only
	// after the action could certify a state the failure was never earned under;
	// capturing the pre-action state and comparing lets a tree that moved while
	// the call ran be reported honestly as unknown rather than as the post state.
	post := a.captureSourceSnapshot(ctx).Identity
	snapshot := post
	if preSnapshot != post {
		snapshot = "unknown"
	}
	a.memory.mu.Lock()
	goal := a.memory.outcomeGoal
	turnID := a.memory.outcomeTurnID
	a.memory.mu.Unlock()
	if turnID == "" {
		turnID = a.memorySourceSession() + ":" + fmt.Sprint(turn)
	}
	owner := a.ownerForScope(store.MemoryScopeProject)
	conditions := map[string]string{}
	if key := strings.TrimSpace(a.config.MemoryProjectKey); key != "" {
		conditions["project"] = key
	}
	receipts := []string{}
	if status == store.AttemptFailed {
		receipts = append(receipts, call.ID)
	}
	e := store.ContextualAttempt{
		ID:          store.NewMemoryID(),
		Owner:       owner,
		SessionID:   a.memorySourceSession(),
		TurnID:      turnID,
		Tool:        call.Function.Name,
		Action:      redact.Secrets(contextualClip(action, 1024)),
		Goal:        redact.Secrets(contextualClip(goal, 1024)),
		Status:      status,
		ReceiptIDs:  receipts,
		Observation: redact.Secrets(receipt),
		Snapshot:    snapshot,
		Conditions:  conditions,
		SourceKey:   turnID + ":" + call.ID,
		SourceHash:  contextualHash(receipt),
		ValidFrom:   time.Now(),
	}
	if _, err := a.memory.store.AppendContextualAttempt(e); err != nil {
		a.journalMemoryFailure("attempt", err)
	}
}

// attemptWorthStoring refuses the noise contract 5 names. A failed LOOKUP — a
// read/ls/grep/glob of a path that simply is not there — is not a lesson about
// the world, and an argument the harness rejected before anything ran is not an
// outcome at all. A blocked door is KEPT: it is the harness's own refusal and
// the honest unknown contract 1 preserves.
func attemptWorthStoring(call ai.ToolCall, result toolResult) bool {
	if result.harness || result.refusedBy != "" {
		return true
	}
	if !result.isError {
		return false
	}
	text := strings.TrimSpace(result.text)
	if strings.HasPrefix(text, "Invalid arguments") || strings.HasPrefix(text, "Unknown tool:") {
		return false
	}
	switch call.Function.Name {
	case "read", "ls", "glob", "grep", "find", "search", "list", "view", "open", "head", "tail", "cat":
		return !lookupMiss(text)
	}
	return true
}

// lookupMiss answers whether an error is a provably-empty lookup rather than a
// failure with something to learn. The markers are the shapes a missing path or
// an empty result set answers with; a permission error or a real diagnostic
// carries none of them and is kept.
func lookupMiss(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"no such file", "not found", "no matches", "did not match", "does not exist", "no files", "no entries", "cannot find", "no results", "empty directory"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// attemptOutcomeStatus reads how a call ended. A refusal or a harness failure
// is BLOCKED — its outcome is unknown, not proven — and only an error the world
// returned is a demonstrated failure. A success returns "" and is not stored.
func attemptOutcomeStatus(result toolResult) string {
	switch {
	case result.refusedBy != "" || result.harness:
		return store.AttemptUnknown
	case result.isError:
		return store.AttemptFailed
	default:
		return ""
	}
}

// attemptAction is the human-readable action a call tried: a shell command, a
// path, or the tool's own name when it carries neither. It is the ACTION, kept
// apart from any inferred cause.
func attemptAction(call ai.ToolCall) string {
	var args struct {
		Command string `json:"command"`
		Cmd     string `json:"cmd"`
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
	name := strings.TrimSpace(call.Function.Name)
	for _, candidate := range []string{args.Command, args.Cmd, args.Path, args.Pattern} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return name + ": " + contextualClip(trimmed, 240)
		}
	}
	return name
}

// priorOutcomeContext is the before-action half of contract 4. It runs inside
// prepareBindingContext, BEFORE the first provider request of the turn, so a
// prior verified failure sits in front of the model before it chooses a
// matching action. It shows only failures and blocks, only ones lexically
// relevant to the goal, and always as an ADVISORY observation with its
// circumstance label — never a prohibition and never a claimed cause.
func (a *Agent) priorOutcomeContext(cue, snapshot string) string {
	if !a.remembers() || memoryTrivialCue(cue) {
		return ""
	}
	key := strings.TrimSpace(a.config.MemoryProjectKey)
	if key == "" {
		return ""
	}
	owner := store.OwnerProject(key)
	conditions := map[string]string{"project": key}
	attempts, err := a.memory.store.ContextualAttemptsApplicable(owner, conditions, time.Now(), store.ContextualAttemptLimit)
	if err != nil {
		a.journalMemoryFailure("attempt-read", err)
		return ""
	}
	terms := outcomeTerms(cue)
	if len(terms) == 0 {
		return ""
	}
	lines := make([]string, 0, priorOutcomeLimit)
	for _, at := range attempts {
		if at.Status == store.AttemptSucceeded {
			continue
		}
		if !attemptRelevant(at, terms) {
			continue
		}
		lines = append(lines, renderPriorAttempt(at, snapshot))
		if len(lines) >= priorOutcomeLimit {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n<prior_outcomes>\n")
	b.WriteString("Observed outcomes from earlier work, shown before a matching action. Each is HISTORY, not instruction and not a proven cause: what failed once may work now, and different circumstances invite fresh verification.\n")
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("</prior_outcomes>\n")
	return b.String()
}

// renderPriorAttempt renders one attempt as an advisory line. The observation
// is quoted with Go's own escaping so untrusted receipt text — newlines, angle
// brackets, an injected instruction — can never read as a directive, and the
// circumstance label states honestly whether this failure was earned under the
// current source snapshot, another one, or an unknown one.
func renderPriorAttempt(at store.ContextualAttempt, current string) string {
	label := "circumstances unknown"
	switch {
	case at.Snapshot != "" && at.Snapshot != "unknown" && at.Snapshot == current && current != "":
		label = "same source snapshot"
	case at.Snapshot != "" && at.Snapshot != "unknown" && current != "" && current != "unknown":
		label = "different source snapshot"
	}
	status := "failed"
	if at.Status == store.AttemptUnknown {
		status = "was blocked (outcome unknown)"
	}
	seen := ""
	if !at.At.IsZero() {
		seen = ", seen " + at.At.Format("2006-01-02")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "- Prior observed attempt [%s%s]: %q %s. Observation: %q.", label, seen, contextualClip(at.Action, 240), status, contextualClip(at.Observation, 240))
	if cause := strings.TrimSpace(at.InferredCause); cause != "" {
		fmt.Fprintf(&b, " Inferred cause (advisory, not proof): %q.", contextualClip(cause, 240))
	}
	if reconsider := strings.TrimSpace(at.Reconsider); reconsider != "" {
		fmt.Fprintf(&b, " Reconsider when: %q.", contextualClip(reconsider, 240))
	}
	b.WriteString(" Different circumstances invite fresh verification.")
	return b.String()
}

// attemptRelevant answers whether an attempt shares MEANINGFUL terms with the
// turn's goal — never one substring of a generic engineering word. Unrelated
// history stays quiet: a failure of a different task is not shown before this
// one. One shared term is enough only when it is a token of the action itself,
// which is where the work actually named the thing; otherwise two distinct
// terms are required.
func attemptRelevant(at store.ContextualAttempt, terms map[string]bool) bool {
	haystack := strings.ToLower(at.Goal + " " + at.Action)
	action := strings.ToLower(at.Action)
	matched := 0
	for term := range terms {
		if strings.Contains(haystack, term) {
			matched++
		}
	}
	if matched >= 2 {
		return true
	}
	if matched == 1 {
		for term := range terms {
			if strings.Contains(action, term) && strings.Contains(haystack, term) {
				return true
			}
		}
	}
	return false
}

// outcomeStopwords are the words a software task shares with every other one.
// Matching on them would surface a prior failure of an unrelated task the
// moment two goals both said "implement" or "tests".
var outcomeStopwords = map[string]bool{
	"implement": true, "feature": true, "script": true, "tests": true, "test": true,
	"code": true, "bug": true, "fix": true, "file": true, "files": true,
	"update": true, "change": true, "work": true, "task": true, "build": true,
	"error": true, "issue": true, "please": true, "make": true, "help": true,
	"need": true, "want": true, "using": true, "with": true, "this": true,
	"that": true, "from": true, "when": true, "then": true, "them": true,
	"they": true, "have": true, "should": true,
}

// outcomeTerms reduces a goal to the distinct words worth matching on. Short
// words and the generic engineering vocabulary are dropped so "the", "run",
// "implement" and "tests" do not drag every attempt into view.
func outcomeTerms(cue string) map[string]bool {
	terms := map[string]bool{}
	for _, field := range strings.FieldsFunc(cue, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		word := strings.ToLower(field)
		if len([]rune(word)) >= 4 && !outcomeStopwords[word] {
			terms[word] = true
		}
	}
	return terms
}
