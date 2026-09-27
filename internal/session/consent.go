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

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/connect"
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
	// ConsentRule answers this call and says a RULE HAS BEEN WRITTEN that covers
	// it — the surface banked the shape the person picked into their own settings
	// before sending this (internal/config's approvalmemory.go).
	//
	// It exists because the memo above is keyed by TOOL NAME ALONE, and for bash
	// that is wider than anything the card ever promised: a card that said
	// "always, this command" and left behind a memo meaning "every bash command"
	// was a card that lied by one word, in the direction that matters. So a
	// surface that wrote a real rule says so with this scope, and the gate writes
	// NO memo — the rule is what answers the next call, and it answers only the
	// calls it matches.
	//
	// The honest consequence, stated here because it is surprising: this scope
	// makes the next call go back through the policy. A shape narrower than the
	// person expected means being asked again, which is the card's promise kept
	// rather than broken.
	ConsentRule ConsentScope = "rule"
)

// ConsentWaiting is the only silence the gate has: an unanswered question
// stays a question. A surface clock that recorded "denied" after ~10s and
// cancelled the call was F41; the engine never does that, and every
// EventConsentRequest says so in Wait.
const ConsentWaiting = "waiting"

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
	switch scope {
	case ConsentToolSession, ConsentRule:
	default:
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
	policy := a.approvalGate()
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

	// From here the policy wants a person, and a remembered answer for this tool
	// stands in for one. Two questions it never stands in for:
	//
	//   - AN OUTRIGHT DENY, which returned above and never reaches this line. A
	//     rule that refuses is not a question anybody was asked, so there is no
	//     answer to remember about it.
	//   - THE FLOOR. The critical shapes and the calls that act in the person's
	//     name answer PROMPT and not deny (internal/approval), so without this
	//     they were exactly the questions a memo could swallow: one approved
	//     `git status` with "stop asking me about bash" behind it, and `rm -rf /`
	//     ran silently for the rest of the session. The memo is somebody saying
	//     they are done being asked about ordinary work. It is not somebody
	//     saying they have read a message that has not been written yet.
	if !approval.AlwaysAsks(call.Function.Name, json.RawMessage(call.Function.Arguments)) {
		if remembered, known := a.rememberedConsent(call.Function.Name); known {
			if remembered {
				return toolResult{}, true
			}
			return refusal("denied by approval rule: " + decision.Rule + " (remembered for this session)"), false
		}
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
		// A WAIT THAT ENDED IS NOT A NO. ctx.Done — a timeout, an interrupt,
		// the agent closing — means nobody answered. Saying "denied by the
		// person" here is how a late Esc and a ten-second clock used to
		// write a refusal nobody gave (F41, R2).
		return refusal(notApprovedWording(err)), false
	}
	if !allowed {
		return refusal("denied by the person: " + decision.Rule), false
	}
	return toolResult{}, true
}

// notApprovedWording is what the model is told when the wait ended without
// anybody answering. The two sentences are the two truths: the question ran
// out of time, or the turn (or the agent) ended first. Neither is a person's
// no, and the words must not say it was.
func notApprovedWording(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "not approved: the question timed out"
	}
	return "not approved: ended before an answer"
}

// ask emits one request for a CALL and waits for the answer or for the turn to
// end. A "don't ask me again" answer is remembered, because the question was
// about a tool.
func (a *Agent) ask(ctx context.Context, hub *eventHub, call ai.ToolCall, decision approval.Decision) (bool, error) {
	answer, err := a.askAnswer(ctx, hub, call, decision)
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
// THE QUESTION IS ALWAYS ABOUT A TOOL, and that is what makes it one another
// window may answer from its one line. The stuck-turn question that used to
// borrow this lane to ask about a TURN — with no line, no widening answer and
// three answers where this has two — had no caller left and is deleted
// (recovery.go), so every question this lane raises goes through the one door
// whole and the gate's own words are the only ones it speaks in. It is banked
// AFTER the id is minted, because the id is what an answer from somewhere else
// names.
func (a *Agent) askAnswer(ctx context.Context, hub *eventHub, call ai.ToolCall, decision approval.Decision) (consentAnswer, error) {
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

	// THE REQUEST NAMES THE ROW, and it is the lane's own announcement: the
	// question itself goes out after it on the questions lane
	// ([Agent.raiseQuestion] keeps that order), because a question attaches to
	// a row a surface has already drawn — the order this lane already keeps
	// against its batch's EventToolBegin rows, for the same reason.
	announce := func() {
		hub.send(Event{
			Kind: EventConsentRequest,
			ID:   id,
			Tool: call.Function.Name,
			// AND THE ID OF THE CALL IT IS ABOUT, for the reason the announcement
			// carries one (loop.go): a batch can raise three bash questions at once,
			// and a surface with no id can pair a question to a row only by tool name
			// — oldest-of-that-tool, which is a guess. Here the guess is worse than a
			// wrong row. The card reads the COMMAND off the row it paired to, so a
			// question that landed on the wrong one lets a person read command A,
			// press always, and bank a standing rule for command B.
			CallID: call.ID,
			// Hint is the same gloss every tool row carries, so a surface renders
			// the question against the row it already drew; Rule is the policy's
			// own words for why it is asking.
			Hint: a.gloss(call),
			Args: argsText(call),
			Rule: consentRule(call, decision),
			// And whether the memo is even available, so a surface can leave the
			// "always" key off a question it would be dropped on (see Event.Memo).
			Memo: true,
			// Silence is not a no. The card draws this so the wait mode is not a
			// hidden deny timer (F41).
			Wait: ConsentWaiting,
		})
	}
	// AND THE WHOLE QUESTION IS BANKED, not only the one line (question.go). The
	// gate is the only thing in this program that knows the tool, the rule the
	// policy matched and the gloss of the call, and until it did it threw all
	// three away the moment the event went out — so home, a second window and
	// the phone had the sentence and nothing under it. The short form goes up
	// beside it unchanged, for the builds that only ever read that.
	defer a.presenceAskingWhole(a.consentAsk(id, call, decision), announce)()

	select {
	case answer := <-answers:
		// A [ConsentRule] answer writes nothing here on purpose: the surface
		// already wrote the rule the person picked, and a memo beside it would be
		// the coarse tool-wide yes this scope exists to stop making. See its
		// doc for the consequence.
		if answer.scope == ConsentToolSession {
			// A STANDING YES ABOUT AN ACCOUNT IS A SETTING, NOT A MEMO. It is
			// written where the settings sheet writes it, and it is written
			// INSTEAD of the session memo rather than beside it: two records of
			// one answer would drift the moment somebody set the row back to ask
			// in the panel and went on not being asked here for the rest of the
			// session. Everything else keeps the memo it always had.
			// WHAT IT BOUGHT IS WRITTEN DOWN WHERE IT IS BOUGHT, so changing
			// your mind on the receipt can take back exactly this and nothing
			// else ([Agent.undoGrant]).
			if made, wrote := a.rememberCapability(call.Function.Name, json.RawMessage(call.Function.Arguments), answer.allow); wrote {
				a.rememberGrant(call.Function.Name, made)
				return answer, nil
			}
			a.rememberConsent(call.Function.Name, answer.allow)
			a.rememberGrant(call.Function.Name, grantMade{})
		}
		return answer, nil
	case <-ctx.Done():
		a.forgetConsent(id)
		return consentAnswer{}, ctx.Err()
	}
}

func (a *Agent) rememberConsent(tool string, allow bool) {
	if a.approvalParent != nil {
		a.approvalParent.rememberConsent(tool, allow)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.consentMemo == nil {
		a.consentMemo = make(map[string]bool, 1)
	}
	a.consentMemo[tool] = allow
}

// grantMade is the standing half of a yes, remembered so it can be TAKEN BACK.
//
// A widening yes is written into more than one place, and which one depends on
// what the tool is: an ordinary tool gets the session memo above, and a
// connected service's tool gets a capability left on in the person's own account
// settings ([Agent.rememberCapability]) INSTEAD of the memo. A revision that
// only deleted the memo therefore said "the permission is taken back" on the
// receipt while the account went on being allowed — so what was granted is
// written down here at the moment it is granted, and [Agent.undoGrant] is the
// one door that takes back all of it.
type grantMade struct {
	service    string
	capability string
}

// rememberGrant banks what a standing yes bought, keyed by the tool it was about
// — the same key the memo uses, because a second yes for one tool replaces the
// first and undoing the newest is what changing your mind on a receipt means.
func (a *Agent) rememberGrant(tool string, made grantMade) {
	if a.approvalParent != nil {
		a.approvalParent.rememberGrant(tool, made)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.grants == nil {
		a.grants = make(map[string]grantMade, 1)
	}
	a.grants[tool] = made
}

// undoGrant takes back everything a standing yes bought for one tool: the memo
// that stops this session asking again, and the capability left on in the
// person's account settings where the yes was written there instead.
//
// THE THIRD STORE IS THE SURFACE'S AND CANNOT BE REACHED FROM HERE. A yes given
// as a RULE is written to the person's settings by the window that asked
// (tui3's `app.rememberAlways`), where `/permissions` shows it; the surface
// removes its own with `app.forgetAlways` on the same key press that sends the
// revision. That is the whole of what this engine cannot do, and it is named
// here so the next reader does not conclude there are two doors.
func (a *Agent) undoGrant(tool string) {
	if a.approvalParent != nil {
		a.approvalParent.undoGrant(tool)
		return
	}
	a.mu.Lock()
	delete(a.consentMemo, tool)
	made, granted := a.grants[tool]
	delete(a.grants, tool)
	connected := a.connect
	a.mu.Unlock()
	if !granted || connected == nil || made.service == "" || made.capability == "" {
		return
	}
	// BACK TO ASKING, NOT TO NO. The person is taking back a standing yes, and
	// what they had before they gave it was a question — turning it into a
	// refusal would deny the next call something they never said no to.
	_ = connected.SetCapabilityState(made.service, made.capability, connect.StateAsk)
}

func (a *Agent) rememberedConsent(tool string) (bool, bool) {
	if a.approvalParent != nil {
		return a.approvalParent.rememberedConsent(tool)
	}
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

// withoutWidening drops the `always` answer from a consent question. It is
// used only for a call the policy graded irreversible, where a banked answer
// would be refused by the floor on the very next call.
func withoutWidening(options []AnswerOption) []AnswerOption {
	kept := options[:0]
	for _, option := range options {
		if !option.Widening {
			kept = append(kept, option)
		}
	}
	return kept
}

// consentAsk is the approval gate's question as [Question] — the same moment
// the EventConsentRequest above describes, in the object every lane now speaks.
//
// IT INVENTS NOTHING. The head is the line this lane already wrote for the
// presence file, the reason is the POLICY'S OWN phrasing of why it is asking
// (internal/approval's Rule), the evidence is the arguments the row already
// carries, and the answers are answers.go's for this kind.
//
// THE STAKES ARE INTERNAL/APPROVAL'S OWN JUDGEMENT, PASSED THROUGH — NEVER
// READ OFF THE COMMAND HERE. [approval.AlwaysAsks] is true for exactly the
// shapes that hold under a blanket allow: bash's critical table and the calls
// that act in the person's name. Those are the calls whose outcome cannot be
// taken back, so they are graded `irreversible` and everything else stays
// `costly`. A second list or a string match in this package would be two
// judgements about the same call that one day disagree, which is the
// repository's generic-and-meta law: internal/approval says which shapes are
// grave, and this lane carries the grade.
func (a *Agent) consentAsk(id uint64, call ai.ToolCall, decision approval.Decision) Question {
	irreversible := approval.AlwaysAsks(call.Function.Name, json.RawMessage(call.Function.Arguments))
	stakes := StakesCostly
	options := AnswerOptions(QuestionConsent)
	scope := []AnswerScope{ScopeOnce, ScopeAlways}
	if irreversible {
		stakes = StakesIrreversible
		// AND THE WIDENING YES IS NOT OFFERED ON ONE. `always` banks a memo or
		// a rule that answers the next call without asking — and these are
		// exactly the shapes the gate asks about EVERY time, memo or no memo
		// (approve's floor above), so the key would draw a standing permission
		// the very next call refuses to honour. One predicate decides both the
		// grade and the offer, because they are one fact.
		options = withoutWidening(options)
		scope = []AnswerScope{ScopeOnce}
	}
	return Question{
		ID:      id,
		Kind:    QuestionConsent,
		Ask:     AskPermission,
		Form:    FormLine,
		Asker:   Asker{Kind: AskerEngine},
		Head:    ConsentHead(call.Function.Name, call.Function.Arguments),
		Reason:  consentReason(call, decision),
		Subject: SubjectRef{Kind: SubjectCall, CallID: call.ID, Name: call.Function.Name},
		// AND WHICH STEP ASKED, so that three approvals raised by one tool batch
		// are drawn and answered as the one thing they are (question.go's
		// [Question.Batch]).
		Batch:   a.stepToken(),
		Options: options,
		Stakes:  stakes,
		// THE TURN IS STOPPED ON IT AND NOTHING ELSE IS. The call is blocked
		// inside its batch; the batch's other calls run in their own goroutines,
		// and no task waits on this at all.
		Blocking: Blocking{Turn: true},
		Scope:    scope,
		// AND IT ATTACHES NOTHING. The call's arguments are on the row this question
		// points at, and consent.go's own law is that IT SHOWS THE ROW THAT IS
		// ALREADY THERE — two renderings of one call is how a person ends up
		// approving something other than what they read. Copying them onto the
		// question would also put a whole file's body into a presence file every
		// window on the machine re-reads every few seconds.
	}
}

// consentHeadLead opens the one line this lane's question is read by in every
// window — home, a second terminal, the phone — and the tool's name closes it.
const consentHeadLead = "needs your ok to run "

// ConsentHead is the permission question's one line for a call: "needs your ok
// to run bash", and for a manager's `team_start` the sentence the person is
// actually being asked, "◆ manager wants to start @lexer". args is the call's
// arguments as JSON, the raw ones or [Event.Args]; a start whose handle cannot
// be read falls back to the ordinary line.
//
// IT IS EXPORTED SO THERE IS ONE BUILDER. The card a surface draws and the
// question home and a second window answer from are the same question, and two
// builders that drifted would make them two.
func ConsentHead(tool, args string) string {
	tool = strings.TrimSpace(tool)
	if tool == teamStartToolName {
		if handle, _ := teamStartArgs(args); handle != "" {
			return "◆ manager wants to start @" + handle
		}
	}
	return consentHeadLead + tool
}

// consentRule is [Event.Rule] for a call: the policy's own words, and for a
// start with none, what the person is agreeing to pay for ([teamStartCost]).
func consentRule(call ai.ToolCall, decision approval.Decision) string {
	if rule := strings.TrimSpace(decision.Rule); rule != "" {
		return rule
	}
	if call.Function.Name == teamStartToolName {
		return teamStartCost
	}
	return ""
}

// consentReason is why the gate is asking, in the policy's own words where it
// gave any and in this lane's own sentence where it did not. The wording is
// internal/approval's on the same terms [Event.Rule] takes it: every surface
// should say the same sentence about the same rule instead of deriving one.
func consentReason(call ai.ToolCall, decision approval.Decision) string {
	if rule := consentRule(call, decision); rule != "" {
		return rule
	}
	return ConsentFallbackReason
}
