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
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/connect"
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
	// choice is set only by [Agent.ResolveRecovery] (recovery.go), where the
	// question has three answers and a bool has two. It is empty for every
	// ordinary consent answer, and the escalation reads that emptiness as "a
	// binary surface answered" rather than as a third option nobody picked.
	choice RecoveryChoice
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
	a.deliverConsent(id, consentAnswer{allow: allow, scope: scope})
}

// deliverConsent hands one answer to whoever is waiting for it, and drops it if
// nobody is. It is the one place an answer reaches a blocked call, so the
// recovery lane (recovery.go) and the consent lane cannot drift on what "the
// question was already abandoned" means.
func (a *Agent) deliverConsent(id uint64, answer consentAnswer) {
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
	answers <- answer
}

// decide asks the policy about one call. The bool is false when there is no
// policy at all, which is the configured-nothing case and means allow: an
// agent built without an ApprovalPolicy behaves exactly as it did before this
// file existed.
//
// ── AND THEN THE PERSON'S OWN WORD ABOUT THE ACCOUNT ──
//
// A call against one of their accounts has a second thing said about it: the
// capability's state, set in the settings sheet or by an "always" answer here
// (connectcaps.go). It is applied AFTER the policy and it moves the answer by at
// most one rung, in one direction each:
//
//   - YES is the person's named allow. It is worth exactly what a
//     `gmail_send:allow` rule is worth — and it is worth that for the same
//     reason, that somebody wrote it about that tool — so it lifts a prompt to
//     an allow, INCLUDING the floor internal/approval holds under a blanket
//     allow for calls that act in their name. That floor exists because a
//     blanket allow cannot vouch for a message it has not seen; this is not a
//     blanket allow.
//   - ASK is a floor of its own, and it applies to any capability rather than
//     only to the ones that act: a person who sets "read your mail" to ask first
//     is asking to be asked, and a policy that allowed everything would
//     otherwise silently ignore the only control they were given.
//
// NEITHER TOUCHES A DENY. A rule that refuses outright is a refusal, and a word
// on a settings row is not a licence to overrule it. Off is not here at all —
// it is answered before this, in [Agent.approve], because a capability that is
// off must not produce a question about a call that is never going to run.
func (a *Agent) decide(call ai.ToolCall) (approval.Decision, bool) {
	policy := a.config.ApprovalPolicy
	if policy == nil {
		return approval.Decision{}, false
	}
	args := json.RawMessage(call.Function.Arguments)
	decision := policy.Check(call.Function.Name, args)
	return a.capabilitySays(call.Function.Name, args, decision), true
}

// capabilitySays applies the person's word about the account to the policy's
// answer. A call that belongs to no account, or a build with no accounts layer,
// comes back exactly as it went in.
func (a *Agent) capabilitySays(tool string, args json.RawMessage, decision approval.Decision) approval.Decision {
	service := a.serviceOf(tool)
	if a.connect == nil || service == "" {
		return decision
	}
	capability := a.capabilityOf(service, tool, args)
	if capability == "" {
		return decision
	}
	phrase := a.capabilityPhrase(service, capability)
	if phrase == "" {
		phrase = capability
	}
	switch a.connect.CapabilityState(service, capability) {
	case connect.StateYes:
		if decision.Action == approval.ActionPrompt {
			return approval.Decision{
				Action: approval.ActionAllow,
				Rule:   "you said yes to " + strconv.Quote(phrase),
			}
		}
	case connect.StateAsk:
		if decision.Action == approval.ActionAllow {
			return approval.Decision{
				Action: approval.ActionPrompt,
				Rule:   strconv.Quote(phrase) + " is set to ask first",
			}
		}
	}
	return decision
}

// approve is the gate. It returns the refusal to hand the model and false when
// the call must not run.
func (a *Agent) approve(ctx context.Context, hub *eventHub, call ai.ToolCall) (toolResult, bool) {
	// WHAT THE PERSON HAS TURNED OFF NEVER RUNS, and it is answered here rather
	// than by the policy: it is not a judgement about this call, it is a hand
	// this build does not have. It comes FIRST, ahead of the policy, the memo
	// and the guardian, so that nothing downstream can allow it and nobody is
	// asked a question whose only honest answer is already known. See
	// [Agent.capabilityRefusal] for why an armed tool can be off at all.
	if off := a.capabilityRefusal(call.Function.Name, json.RawMessage(call.Function.Arguments)); off != "" {
		return refusal(off), false
	}
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

	// THE GUARDIAN (guardian.go), if the person turned it on: a small model is
	// asked whether this specific call is plainly safe before anybody is
	// bothered. It can only turn this prompt into an allow — every other answer,
	// every error and every interrupt falls through to the lines below unchanged.
	//
	// It sits ABOVE the no-watcher check on purpose. A headless run with the
	// guardian explicitly on gets the guardian's answer instead of the automatic
	// refusal, which is the whole point of having said so in advance; with it off
	// — the default — this line does nothing and a headless run refuses exactly as
	// it always did.
	if a.guardianAllows(ctx, hub, call, decision) {
		return toolResult{}, true
	}

	// INSIDE A TASK NODE the same law applies and the words are the node's own
	// (task_run.go). A node's policy allows everything, so the only decisions
	// that reach this line are approval's critical floor — the handful of shapes
	// that destroy a disk or drop the machine — and the honest thing to tell a
	// worker with no colleague in the room is that this one needed a person and
	// there is not one.
	if a.config.InTask {
		return refusal("refused in a task: " + decision.Rule + " — nobody to ask"), false
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

// ask emits one request for a CALL and waits for the answer or for the turn to
// end. A "don't ask me again" answer is remembered, because the question was
// about a tool.
func (a *Agent) ask(ctx context.Context, hub *eventHub, call ai.ToolCall, decision approval.Decision) (bool, error) {
	answer, err := a.askAnswer(ctx, hub, call, decision, true)
	return answer.allow, err
}

// askAnswer emits one request and waits for the whole answer or for the turn to
// end.
//
// The wait is on the TURN's context, which is what makes Interrupt work on a
// pending question: the cancellation releases this select, the call refuses
// with a result the batch can record, and the turn ends the way any
// interrupted turn ends. Nothing here holds a.mu across the wait — the lock
// Interrupt needs must never be held by something waiting on a person.
//
// memo says whether a ConsentToolSession answer may be remembered for the tool.
// It is true for the gate, whose question IS about a tool, and false for the
// stuck question (recovery.go), which borrows this lane to ask about a TURN —
// and where "and stop asking me" would otherwise write a standing approval for
// a tool nobody was asked to approve.
func (a *Agent) askAnswer(ctx context.Context, hub *eventHub, call ai.ToolCall, decision approval.Decision, memo bool) (consentAnswer, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return consentAnswer{}, errAgentClosed
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
		Hint: a.gloss(call),
		Args: argsText(call),
		Rule: decision.Rule,
		// And whether the memo is even available, so a surface can leave the
		// "always" key off a question it would be dropped on (see Event.Memo).
		Memo: memo,
	})

	select {
	case answer := <-answers:
		if memo && answer.scope == ConsentToolSession {
			// A STANDING YES ABOUT AN ACCOUNT IS A SETTING, NOT A MEMO. It is
			// written where the settings sheet writes it, and it is written
			// INSTEAD of the session memo rather than beside it: two records of
			// one answer would drift the moment somebody set the row back to ask
			// in the panel and went on not being asked here for the rest of the
			// session. Everything else keeps the memo it always had.
			if a.rememberCapability(call.Function.Name, json.RawMessage(call.Function.Arguments), answer.allow) {
				return answer, nil
			}
			a.rememberConsent(call.Function.Name, answer.allow)
		}
		return answer, nil
	case <-ctx.Done():
		a.forgetConsent(id)
		return consentAnswer{}, ctx.Err()
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
