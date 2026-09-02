package session

// Process rules the turn loop can hold a model to, and the rung where advice
// stops being advice.
//
// ── THE LAW ──
//
// A RULE THE LOOP CAN ENFORCE IS NOT A SUGGESTION. The harness owns the turn
// loop. When a rule about HOW work is done matters to the record, and the model
// demonstrably ignores the advisory form of it, the loop escalates to the form
// it can actually hold: it stops running the next submission until the rule is
// met.
//
// The measured case that wrote this file. One dogfood conversation on
// 2026-08-31 took the write-your-notes advisory thirty-two times — nineteen at
// the first rung and thirteen more at the second — and obeyed it approximately
// never. The second rung's own words were "stop calling tools until you have
// written that note", and nothing anywhere stopped anything, so the sentence was
// false every one of the thirteen times it was said. A rule stated thirty-two
// times and enforced no times is the harness talking to itself, and every
// repetition of it is spend.
//
// ── WHY THIS IS A REGISTRY AND NOT AN `IF` IN THE NUDGE ──
//
// Write-your-notes is the first tenant here and deliberately not the only shape
// the mechanism admits. The escalation is the same for any rule about process:
// state your assumptions before an irreversible act, name the file before you
// replace it, say what you checked before you claim it holds. Every one of those
// is the same four questions — is this rule's advisory being ignored, does the
// submission in front of the loop meet it, what does the model read instead, and
// what does the turn say if it never complies — and a mechanism that took them
// as an interface can grow a second rule without touching the loop again. So the
// loop asks [enforceableProcessRules] and knows nothing about notes.
//
// ── THE THREE STEPS, AND WHY THE FIRST ONE IS UNCHANGED ──
//
//  1. ADVICE. The silent ladder in looped.go, exactly as it was. A model that
//     writes its notes never reaches step two and cannot tell this file exists.
//  2. THE HELD SUBMISSION. Once the advisory has been said [silentEnforceRung]
//     times and the streak is still unbroken, the next submission that carries
//     only tool calls is answered with the rule's demand and NOT executed. One
//     rung, and one only: a note lands, the streak resets, and the loop is back
//     where it was.
//  3. THE HONEST LANDING. After [processRuleRefusals] held submissions the turn
//     ENDS, saying plainly what it would not do. Enforcement that cannot end is
//     an infinite argument with a model that cannot comply, which is a worse
//     failure than the one being fixed: it spends the person's money forever and
//     leaves nothing on the record either way.
//
// ── WHAT A HELD SUBMISSION COSTS, AND WHAT IT DOES NOT ──
//
// Nothing reaches the world. The demand goes back as the answer to every call in
// the batch, and the batch is never shown to the step boundary at all — the loop
// takes its next round without running `post-feedback` — so the silent ladder
// FREEZES where it stands rather than climbing on submissions the harness itself
// refused. That is what makes the rung ONE rung: the ladder cannot advance past
// the note that armed the hold.
//
// AND THE ANSWERS SAY WHO WROTE THEM, marked as the harness's own on the result
// and on the event (loop.go's [toolResult], [Event.HarnessMade]). Everything
// that judges a model by its steps — the runner's no-progress counter above all
// — reads those and skips them, which is the law withdrawn.go states after a
// worker was scolded three times for retrying a tool the harness had taken away.
//
// A READ THAT HAD ALREADY STARTED EARLY IS THROWN AWAY. The early-start law in
// loop.go admits read-only calls only, precisely so that an attempt whose result
// is discarded costs the work and nothing else; a held submission discards one
// exactly as a provider retry does.

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// silentEnforceRung is the rung of the silent ladder (looped.go) at which the
	// write-your-notes rule stops being advice: THE LAST ONE, which is why it is
	// [silentRungs] and not a number of its own.
	//
	// THE TWO ARE ONE FACT AND MUST STAY ONE. The ladder's last note is the note
	// that says the tools are held, and a rung after it would be a rung nothing
	// can reach — the hold freezes the silent streak where it stands, so a ladder
	// whose last rung was not the enforced one would end in dead prose promising
	// something untrue. Today that is two: the first rung is advice a model may
	// simply not have read yet, and the second is the harness watching its own
	// advice be read and ignored, which is the evidence enforcement needs.
	silentEnforceRung = silentRungs

	// processRuleRefusals is how many submissions one rule may hold before the
	// turn lands instead of arguing.
	//
	// THREE. Once could be a model that had not read the answer yet; three is a
	// model that read the same demand three times and sent tool calls anyway,
	// and no fourth statement of it is going to arrive as news. Every held
	// submission is a real model round trip the person pays for, so the budget
	// is the smallest number that can tell "did not see it" from "will not do
	// it".
	processRuleRefusals = 3
)

// The three things that can happen to a process rule in one conversation, as
// the journal spells them. They are constants because whatever is counting
// nudges against the thirty-two-ignores baseline reads these words, and an event
// spelled two ways is two events to a reader.
const (
	// ruleAdvised is one advisory note going out — the count this file exists to
	// make measurable.
	ruleAdvised = "advised"
	// ruleHeld is one submission answered with the demand instead of run.
	ruleHeld = "held"
	// ruleStopped is the turn ending because the rule was never met.
	ruleStopped = "stopped"
)

// submission is one thing the model sent the loop: the batch of calls it wants
// run, and whether it wrote anything visible beside them.
//
// It carries the batch-level facts and no others on purpose. A rule that needed
// to read the transcript would be a second model's worth of judgement about a
// turn, which is the line hooks.go draws around the deterministic half of
// recovery and the line this file stays inside.
type submission struct {
	calls       []ai.ToolCall
	visibleText bool
}

// processRule is one rule about HOW a turn works — not about what the work is —
// that the loop can hold a model to rather than merely advise it of.
//
// The four methods are the four questions the escalation asks, and a rule that
// can answer them needs to know nothing about the loop.
type processRule interface {
	// slug names the rule in the journal. It is short, domain-neutral and
	// stable, because a count kept under two spellings is two counts.
	slug() string

	// advisedBy reports whether a nudge the detector has just produced is this
	// rule's own advisory step. It is how the advisory and the enforcement stay
	// one rule rather than two mechanisms that happen to agree.
	advisedBy(n nudge) bool

	// ignored reports whether this turn has reached the rung where the advisory
	// has demonstrably not worked.
	ignored(w *loopWatch) bool

	// met reports whether one submission satisfies the rule. A submission that
	// meets it runs untouched and puts the rule back to advisory.
	met(sub submission) bool

	// demand is what the model reads instead of its tool results when a
	// submission does not meet the rule. It must say what to do next, because a
	// refusal the model can act on is worth more than one that ends the turn.
	demand() string

	// stopped is the honest line the turn lands on when the rule is never met.
	// It says what happened in a person's words and promises nothing.
	stopped() string

	// ending is how a TASK WORKER stopped by this rule is written up: the one
	// word under its row that tells a person why it stopped (task_contract.go's
	// [TaskEnding]).
	//
	// THE RULE NAMES IT AND NOT THE RUNNER. A worker's row has to say which rule
	// it would not follow — "would not write its notes down" is a different
	// piece of news from "went in circles" and asks a different thing of the
	// person — so a second rule in this registry brings its own ending with it
	// rather than being folded into this one's words. A rule enforced in a
	// person's own conversation has no node and nobody reads this.
	ending() TaskEnding
}

// enforceableProcessRules is every rule the loop enforces, in the order it asks
// them. It is a list rather than anything cleverer because there is one of them:
// the value here is the SHAPE, so that the second one is a line in this slice
// and not a second escalation.
var enforceableProcessRules = []processRule{writeNotesRule{}}

// stoppedByProcessRule reports whether an ending is one a rule in that registry
// wrote, which is the whole of what anybody outside this file needs to know
// about a node that ended here.
//
// IT WALKS THE REGISTRY RATHER THAN NAMING AN ENDING, so that the second rule's
// ending is answered for by adding the rule and nothing else — the drift the
// one-source-of-truth law forbids is a second list of these endings kept
// somewhere that has to be remembered (taskgrade.go is the reader).
func stoppedByProcessRule(ending TaskEnding) bool {
	if ending == "" {
		return false
	}
	for _, rule := range enforceableProcessRules {
		if rule.ending() == ending {
			return true
		}
	}
	return false
}

// ── the first tenant ────────────────────────────────────────────────────────

// writeNotesRule is the rule the dogfood run broke thirty-two times: the
// reasoning behind a step has to reach the record, and the record is the visible
// reply.
//
// IT IS ABOUT THE RECORD AND NOT ABOUT THE WORK. A task's room, its checker, its
// parent and the person all read what was written down; reasoning that stayed
// between two steps of one model's head is lost to every one of them, and lost
// to the model too the moment the step boundary drops it. That is why the rule
// is worth enforcing at all, and also why its advisory rungs are the ONLY notes
// in looped.go that may not end a turn: quiet is not stuck, and a worker
// committing and pushing is doing the most valuable part of its job in silence.
type writeNotesRule struct{}

func (writeNotesRule) slug() string { return "write-your-notes" }

// advisedBy claims the silent ladder. Every other rule in looped.go is a claim
// that the turn is not MOVING; this one is the claim about the record, and it is
// the only one whose remedy is a sentence rather than a different call.
func (writeNotesRule) advisedBy(n nudge) bool { return n.silent }

func (writeNotesRule) ignored(w *loopWatch) bool {
	return w.silentLadderRung() >= silentEnforceRung
}

// met is the whole predicate: visible assistant text beside the calls. It is
// deliberately the SAME fact the silent ladder counts — one reading of one
// thing, so the advisory and the enforcement can never disagree about whether a
// note was written.
func (writeNotesRule) met(sub submission) bool { return sub.visibleText }

// demand spells its two thresholds out of the constants the loop applies, so the
// numbers a model reasons from and the numbers the loop enforces cannot drift.
func (writeNotesRule) demand() string {
	return fmt.Sprintf("[held] Nothing was run this step. You were asked at %d tool-using replies "+
		"with no visible text, and again at %d, to write down what you are doing, and you called tools "+
		"instead. Your reasoning between steps is not saved — if it isn't in your visible reply, it's gone. "+
		"Write that note now, as an ordinary visible message: what you have learned so far, what you are "+
		"checking next, and why. Your next tool call runs as soon as that note is there; until then none of "+
		"them run.",
		silentThreshold(0), silentThreshold(silentEnforceRung-1))
}

// stopped is what the person reads on a turn that would not comply. It says the
// two things that are true — the turn stopped, and the reasoning behind whatever
// it did is not on the record — and nothing about the mechanism that stopped it.
func (writeNotesRule) stopped() string {
	return "stopped here · would not write its notes down, so what this turn worked out is not on the record"
}

// ending writes the same thing on a worker's row, in the rail's own half-line
// vocabulary. It is DELIBERATELY THE SENTENCE ABOVE CUT DOWN rather than a
// second wording of it: a person who reads the row and then opens the room must
// meet one claim in two lengths, not two claims.
func (writeNotesRule) ending() TaskEnding { return TaskEndingNotes }

// ── the turn's enforcement state ────────────────────────────────────────────

// ruleHold is one decision about one submission: which rule holds it, which
// refusal this is, and whether the budget for arguing is spent.
type ruleHold struct {
	rule  processRule
	count int
	stop  bool
}

// ruleWatch is one turn's memory of which rules are holding and how long they
// have been. It carries a mutex for [loopWatch]'s reason rather than because
// two goroutines drive it today.
type ruleWatch struct {
	mu sync.Mutex
	// refused counts, per rule, the submissions held since the last one that met
	// it. A submission that meets the rule clears its entry, which is the whole
	// of "back to normal once a note is written".
	refused map[string]int
}

func newRuleWatch() *ruleWatch { return &ruleWatch{refused: make(map[string]int, 1)} }

// hold reads one submission against every enforceable rule and reports the first
// one that will not let it through.
//
// A SUBMISSION THAT MEETS A RULE CLEARS IT FIRST, before any other rule is
// tested, so a model that complies is never held by the rule it has just
// satisfied. The rules are otherwise independent: the first to hold wins, and
// the rest are not asked, because a submission somebody has already refused is
// not a submission the next rule has an opinion about.
func (r *ruleWatch) hold(watch *loopWatch, sub submission) (ruleHold, bool) {
	if r == nil {
		return ruleHold{}, false
	}
	for _, rule := range enforceableProcessRules {
		slug := rule.slug()
		if rule.met(sub) {
			r.mu.Lock()
			delete(r.refused, slug)
			r.mu.Unlock()
			continue
		}
		if !rule.ignored(watch) {
			continue
		}
		r.mu.Lock()
		r.refused[slug]++
		count := r.refused[slug]
		r.mu.Unlock()
		return ruleHold{rule: rule, count: count, stop: count > processRuleRefusals}, true
	}
	return ruleHold{}, false
}

// processRuleGuard is this file's citizen of the control plane (hooks.go): it
// hangs the turn's enforcement state off the episode at `episode-init` and does
// nothing else there.
//
// IT IS NOT A PRE-ACTION HOOK, although vetoing a call is exactly what
// pre-action is for. The decision here is about a SUBMISSION and not about a
// call — whether the model wrote anything beside its batch is a fact about the
// assistant message, and a hook that sees one call at a time cannot read it —
// and the same decision may end the turn, which hooks.go's law reserves for the
// loop that owns the usage and the meter. So the loop asks, at the one boundary
// where both facts are in hand.
type processRuleGuard struct{}

func (processRuleGuard) Name() string { return "process-rules" }

// EpisodeInit opens the turn's enforcement state, and CLOSES THE LAST TURN'S
// VERDICT ON IT: the witness below is about the turn that is starting, so a new
// one begins with nothing written on it. That is what keeps a worker which was
// held once, wrote its note and then finished from settling under an ending
// earned three turns ago.
func (processRuleGuard) EpisodeInit(ep *episode) {
	ep.rules = newRuleWatch()
	if ep.agent != nil {
		ep.agent.ruleStop.open()
	}
}

// holdSubmission reports whether the loop must answer this submission with a
// rule's demand instead of running it. An episode built before this citizen
// existed, and every caller that assembles one by hand in a test, holds nothing.
func (ep *episode) holdSubmission(sub submission) (ruleHold, bool) {
	if ep == nil || ep.rules == nil {
		return ruleHold{}, false
	}
	return ep.rules.hold(ep.watch, sub)
}

// ── the fact a landing reads ────────────────────────────────────────────────

// ruleStopWitness is WHY THE LAST TURN ENDED, when a process rule ended it: the
// ending that rule names, and "" for every turn that ended any other way.
//
// IT EXISTS BECAUSE A FACT MUST NOT BE RECOVERED FROM A SENTENCE. The task
// runner has to know whether its worker was stopped here, and the only evidence
// it had was the landing line sitting in the transcript as the worker's last
// words — so it either matched that prose or said nothing. Prose a person is
// meant to read is prose somebody will one day improve, and a reader that greps
// it is a reader that breaks silently on the day they do. The loop knows the
// answer at the moment it stops the turn; this is where it writes it down.
//
// IT IS ABOUT ONE TURN AND NOT ONE CONVERSATION, which is why it is cleared at
// every `episode-init` and not only set. A worker held once that then wrote its
// note and worked on is a worker that complied, and a latch nobody cleared would
// hang the ending on its landing hours later.
//
// It sits outside the Agent's mutex holding its own, for [ruleLedger]'s reason:
// it is written from the step boundary while a turn holds mu for its own state.
type ruleStopWitness struct {
	mu     sync.Mutex
	ending TaskEnding
}

// open forgets the last turn's answer.
func (w *ruleStopWitness) open() {
	w.mu.Lock()
	w.ending = ""
	w.mu.Unlock()
}

// stopped writes down which rule ended this turn.
func (w *ruleStopWitness) stopped(rule processRule) {
	w.mu.Lock()
	w.ending = rule.ending()
	w.mu.Unlock()
}

// reading is that answer, and "" on every turn no rule ended.
func (w *ruleStopWitness) reading() TaskEnding {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ending
}

// stoppedOnProcessRule is how a node's landing asks its worker why it stopped
// (task_run.go's [endingOfClaim]). An agent that finished, was cut off by the
// wire, or ran out of steps answers "".
func (a *Agent) stoppedOnProcessRule() TaskEnding {
	if a == nil {
		return ""
	}
	return a.ruleStop.reading()
}

// ── what the loop does with a hold ──────────────────────────────────────────

// withholdSubmission answers every call in the batch with the rule's demand and
// runs none of them.
//
// THE ANSWERS ARE RECORDED EXACTLY AS TOOL RESULTS, because the transcript's
// shape is not negotiable: an assistant message naming three tool calls needs
// three tool messages after it or the next request is one no provider will
// accept. They are marked as the harness's own so that nothing which judges the
// model by its steps counts a step the harness refused (withdrawn.go).
func (a *Agent) withholdSubmission(hub *eventHub, calls []ai.ToolCall, hold ruleHold) {
	demand := hold.rule.demand()
	for _, call := range calls {
		a.record(ai.Message{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    []ai.ContentPart{{Type: "text", Text: demand}},
		})
		hub.send(Event{
			Kind:        EventToolFailed,
			Tool:        call.Function.Name,
			CallID:      call.ID,
			Hint:        clip(firstLine(demand), hintLimit),
			Args:        argsText(call),
			Output:      capOutput(demand),
			HarnessMade: true,
		})
	}
	a.journalProcessRule(hold.rule, ruleHeld)
}

// stopForProcessRule ends the turn on the honest line, and is the one place the
// escalation may cost the person their turn.
//
// It is deliberately the same tail as [Agent.handOverLoopingTurn]'s last road:
// the calls are answered so the transcript closes, the sentence is said once as
// a notice and once in the transcript, the turn is sealed on its own usage, and
// the name is asked for while there is still a stream to land it on. NO
// CHECKPOINT HAND-OFF IS ATTEMPTED. Handing this turn's remains to a worker
// would hand them to another model under the same rule with the same tools and
// the same silence, and the honest thing to say is that the work stopped.
func (a *Agent) stopForProcessRule(ctx context.Context, hub *eventHub, calls []ai.ToolCall, hold ruleHold, turn *Usage, started time.Time, model string) bool {
	landing := hold.rule.stopped()
	// SAID AS A FACT BEFORE IT IS SAID AS A SENTENCE. Whatever is reading this
	// turn from outside — a task node settling on its own row above all — gets
	// the answer from the rule itself rather than from the prose below it.
	a.ruleStop.stopped(hold.rule)
	for _, call := range calls {
		a.record(ai.Message{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    []ai.ContentPart{{Type: "text", Text: landing}},
		})
	}
	a.journalProcessRule(hold.rule, ruleStopped)
	hub.send(Event{Kind: EventNotice, Text: landing})
	a.record(textMessage("assistant", landing))
	hub.send(Event{Kind: EventTurnDone, Usage: a.sealTurn(*turn, started, model)})
	a.maybeTitle(ctx, hub)
	return true
}

// ── the count, which is the point of measuring any of this ──────────────────

// ruleLedger counts, PER CONVERSATION, how many times each process rule has had
// to say anything.
//
// IT IS PER CONVERSATION AND NOT PER TURN, which is the one thing that makes it
// worth writing down. The loop's own watch resets at every turn boundary, so a
// turn-shaped count can never say what the dogfood run said: thirty-two
// advisories in one conversation, nineteen at the first rung and thirteen at the
// second. That is the number the enforcement is measured against, and it can
// only be read from the journal.
//
// It sits outside the Agent's mutex holding its own, for the reason the lane
// witness does: it is written from the step boundary while a turn holds mu for
// its own state.
type ruleLedger struct {
	mu     sync.Mutex
	counts map[string]int
}

// mark returns how many advisories this rule has spent in this conversation,
// counting this one when the caller says it is one.
func (l *ruleLedger) mark(slug string, advisory bool) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.counts == nil {
		l.counts = make(map[string]int, 1)
	}
	if advisory {
		l.counts[slug]++
	}
	return l.counts[slug]
}

// journalProcessRule writes one moment of one rule down, with the conversation's
// running advisory count beside it (sessionfile.go's [journalRule]).
//
// EVERY MOMENT CARRIES THE SAME COUNT, so a reader asking "how often did this
// rule have to speak, and did holding the tools change it" reads one number off
// any line rather than summing two kinds. A held or stopped line does not
// advance it: the advisory is what was counted thirty-two times, and the
// enforcement is what is being measured against that.
func (a *Agent) journalProcessRule(rule processRule, event string) {
	slug := rule.slug()
	a.journalFile().appendRule(journalRule{
		Rule:  slug,
		Event: event,
		Count: a.processRules.mark(slug, event == ruleAdvised),
	})
}

// countProcessRuleAdvice books one advisory note against the rule that owns it.
// A nudge no rule claims — every [stuck] note in looped.go — books nothing, and
// this costs one walk of a one-element slice at the step boundary.
func (a *Agent) countProcessRuleAdvice(n nudge) {
	for _, rule := range enforceableProcessRules {
		if rule.advisedBy(n) {
			a.journalProcessRule(rule, ruleAdvised)
			return
		}
	}
}
