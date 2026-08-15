package session

// The approval gate: the moment between "the model asked for a tool" and "the
// tool ran".
//
// The policy itself is internal/approval — a pure function from a call to
// allow, prompt or deny, with the dangerous cases decided by MATCHING rather
// than by a model's judgement about its own request. This file is the other
// half: where that answer is asked for, and what happens to each of the three.
//
//   - ALLOW runs the call, exactly as before this file existed.
//   - DENY returns an ordinary error tool result naming the rule. The model
//     reads it, knows the call did not run, and can do something else. A refusal
//     it can act on is worth more than a refusal that ends the turn.
//   - PROMPT emits EventConsentRequest and BLOCKS the call — not the turn's
//     other tools, which run in their own goroutines — until the person answers
//     or the turn's context dies.
//
// ── WHY CONSENT IS NEVER JOURNALED ──
//
// The session file is the record of what was DONE: messages sent, tools run,
// results returned, compaction passes taken. A consent request is a question
// about work that has not happened yet, and a denied call never ran at all —
// journaling it would put an event in the transcript that no message
// corresponds to, and a resume would replay a question whose moment is gone.
// So consent is events only, and the record shows the two things that are true
// afterwards: either the tool result, or the refusal the model was handed.
//
// ── WHY THE MEMO IS NOT A SETTING ──
//
// "Don't ask me again" (ConsentToolSession) lives in memory for this agent's
// life and is written nowhere. A session-scoped answer that survived the
// session would be a policy change the person never made, in a file they did
// not open. The place to change the policy is the policy.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ConsentScope says how long one answer lasts.
type ConsentScope string

const (
	// ConsentOnce answers this call and nothing else.
	ConsentOnce ConsentScope = "once"
	// ConsentToolSession answers every later prompt for the SAME TOOL, for the
	// rest of this agent's life.
	//
	// It is deliberately coarse — "bash" means every bash command the policy
	// would have asked about, not the one that was asked about — because the
	// alternative is a per-argument memo the person cannot hold in their head:
	// they would be agreeing to a set they have not seen. A tool whose calls
	// deserve individual answers should say so in the policy, where a rule can
	// name the pattern; that is what the bash pattern list is for.
	ConsentToolSession ConsentScope = "tool-session"
)

// consentAnswer is one resolution travelling from the surface to the blocked
// call.
type consentAnswer struct {
	allow bool
	scope ConsentScope
}

// ResolveConsent answers one EventConsentRequest. An id nobody is waiting on —
// a question whose turn was interrupted, a double click — is ignored rather
// than reported: the answer is simply late, and the surface has already seen
// the turn end.
func (a *Agent) ResolveConsent(id uint64, allow bool) {
	a.ResolveConsentRemember(id, allow, ConsentOnce)
}

// ResolveConsentRemember answers one request and says how long the answer
// lasts. An unknown scope is read as ConsentOnce: the narrow reading is the
// safe one, and a typo must not silently widen an approval.
//
// The memo is written BEFORE the answer is delivered, and it is written even
// when the call that asked has already given up. The scope is a standing
// instruction about the tool ("stop asking me about read"), not a property of
// the call that happened to prompt it, so an interrupt racing the click must
// not quietly turn "always" into "once".
func (a *Agent) ResolveConsentRemember(id uint64, allow bool, scope ConsentScope) {
	if scope != ConsentToolSession {
		scope = ConsentOnce
	}
	a.mu.Lock()
	answers, waiting := a.consent[id]
	if waiting {
		delete(a.consent, id)
	}
	a.mu.Unlock()
	if !waiting {
		return
	}
	// The channel is buffered to one and read at most once, so this never
	// blocks and never needs the lock held across it.
	answers <- consentAnswer{allow: allow, scope: scope}
}

// decide asks the policy about one call. The bool is false when there is no
// policy at all, which is the configured-nothing case and means allow: an
// agent built without an ApprovalPolicy behaves exactly as it did before this
// file existed.
func (a *Agent) decide(call ai.ToolCall) (approval.Decision, bool) {
	policy := a.config.ApprovalPolicy
	if policy == nil {
		return approval.Decision{}, false
	}
	return policy.Check(call.Function.Name, json.RawMessage(call.Function.Arguments)), true
}

// approve is the gate. It returns the refusal to hand the model and false when
// the call must not run.
func (a *Agent) approve(ctx context.Context, hub *eventHub, call ai.ToolCall) (toolResult, bool) {
	decision, governed := a.decide(call)
	if !governed || decision.Action == approval.ActionAllow {
		return toolResult{}, true
	}
	if decision.Action == approval.ActionDeny {
		return refusal("denied by approval rule: " + decision.Rule), false
	}

	// From here the policy wants a person. A remembered answer for this tool
	// stands in for one; an explicit deny rule above does NOT consult the memo,
	// because a rule that refuses outright is not a question anybody was asked.
	if remembered, known := a.rememberedConsent(call.Function.Name); known {
		if remembered {
			return toolResult{}, true
		}
		return refusal("denied by approval rule: " + decision.Rule + " (remembered for this session)"), false
	}

	// Nobody is watching. Denying is the only honest answer: blocking would
	// hang a headless run forever on a question with no reader, and allowing
	// would make "prompt" mean "allow" wherever the surface is not a terminal.
	if !a.config.AskConsent || hub == nil {
		return refusal("needs approval but no resolver is attached: " + decision.Rule), false
	}

	allowed, err := a.ask(ctx, hub, call, decision)
	if err != nil {
		return refusal("the turn ended before this call was approved: " + decision.Rule), false
	}
	if !allowed {
		return refusal("denied by the person: " + decision.Rule), false
	}
	return toolResult{}, true
}

// ask emits one request and waits for the answer or for the turn to end.
//
// The wait is on the TURN's context, which is what makes Interrupt work on a
// pending question: the cancellation releases this select, the call refuses
// with a result the batch can record, and the turn ends the way any
// interrupted turn ends. Nothing here holds a.mu across the wait — the lock
// Interrupt needs must never be held by something waiting on a person.
func (a *Agent) ask(ctx context.Context, hub *eventHub, call ai.ToolCall, decision approval.Decision) (bool, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return false, errAgentClosed
	}
	a.consentSeq++
	id := a.consentSeq
	answers := make(chan consentAnswer, 1)
	if a.consent == nil {
		a.consent = make(map[uint64]chan consentAnswer, 1)
	}
	a.consent[id] = answers
	a.mu.Unlock()

	hub.send(Event{
		Kind: EventConsentRequest,
		ID:   id,
		Tool: call.Function.Name,
		// Hint is the same gloss every tool row carries, so a surface renders
		// the question against the row it already drew; Rule is the policy's
		// own words for why it is asking.
		Hint: gloss(call),
		Args: argsText(call),
		Rule: decision.Rule,
	})

	select {
	case answer := <-answers:
		if answer.scope == ConsentToolSession {
			a.rememberConsent(call.Function.Name, answer.allow)
		}
		return answer.allow, nil
	case <-ctx.Done():
		a.forgetConsent(id)
		return false, ctx.Err()
	}
}

func (a *Agent) rememberConsent(tool string, allow bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.consentMemo == nil {
		a.consentMemo = make(map[string]bool, 1)
	}
	a.consentMemo[tool] = allow
}

func (a *Agent) rememberedConsent(tool string) (bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	allow, known := a.consentMemo[tool]
	return allow, known
}

// forgetConsent drops a question nobody will answer. Without it an interrupted
// turn would leave its request in the map for the life of the session, and a
// late resolve would deliver an answer into a channel with no reader.
func (a *Agent) forgetConsent(id uint64) {
	a.mu.Lock()
	delete(a.consent, id)
	a.mu.Unlock()
}

// PendingConsent lists the requests still waiting for an answer, oldest id
// first. It exists for a surface redrawing itself mid-turn — a resized window,
// a reattached view — which needs to know a question is outstanding without
// having kept the event.
func (a *Agent) PendingConsent() []uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]uint64, 0, len(a.consent))
	for id := range a.consent {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}

// refusal is one refused call as the model sees it: an error result, with the
// reason in the words the person would read.
func refusal(reason string) toolResult {
	return toolResult{text: strings.TrimSpace(reason), isError: true}
}

var errAgentClosed = errors.New("session: agent is closed")
