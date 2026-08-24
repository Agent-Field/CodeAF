package session

// THE ROUTE JUDGE: one cheap question asked AFTER a turn that answered in words.
//
// The system prompt has a work-or-words law (prompts/system.md): a question, a
// discussion, a fact and a few tool calls are answered here; research across
// sources, changes across files and anything with several independent parts is
// launched — an adaptive run, a task, a harness — and answered afterwards. The
// law is a paragraph in a prompt, which means it holds exactly as often as the
// model remembers it, and the turn where it is forgotten is invisible: the
// person gets a good paragraph about work nobody started.
//
// So this file asks a SECOND model, once, after the fact. It sees what the
// person said and two lines of what came back, and it answers one thing: should
// that have been work? A yes raises a card. Nothing here launches anything.
//
// FIVE LAWS HOLD IT TO SOMETHING NOBODY WILL WANT TURNED OFF.
//
//   - IT NEVER AUTO-LAUNCHES. The card is the action. A judge that started runs
//     on its own reading of somebody's sentence would be spending their money on
//     its own suggestion, which is exactly what orchestrate.go's cue path refuses
//     to do with a regular expression and what this refuses to do with a model.
//   - IT ONLY WATCHES A TOOL-LESS TURN. A turn that called tools was already
//     work of some size, and asking whether work should have been work is a
//     question with no useful answer.
//   - IT IS RATE-LIMITED, and the limit is about a person's patience rather than
//     about money: at most one offer every [routeJudgeGap] turns, which is also
//     what makes two offers in a row impossible. Somebody who has just said no is
//     having a conversation, and the second card is the one that makes the
//     feature a nuisance.
//   - IT IS SILENT WHEN IT CANNOT WORK. No router model, no surface to answer a
//     card, a reply that is not JSON, a judge that would not answer at all: each
//     of those is one turn that behaves exactly as it did before this file
//     existed. Nothing is said about a judgement nobody made.
//   - IT IS CHEAP. RoleRouter sits on the low tier (internal/roles) because it
//     reads one turn and answers one bounded question, and a wrong no costs a
//     card that was never shown rather than money.

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// routeJudgeGap is how many turns must pass between two offers. Three is a
	// person's patience rather than a budget: a card is a modal row on a turn
	// nobody said was unusual, and the one after a no is the one that teaches
	// somebody to reach for esc without reading. It also settles the "never twice
	// in a row" rule outright — two consecutive turns can never both offer.
	routeJudgeGap = 3
	// routeJudgeWords is the floor under "a non-trivial message". "thanks", "what
	// does this key do", "run the tests" are turns whose answer is words by
	// construction, and paying a model call to be told so on every one of them is
	// the tax this feature must not become.
	routeJudgeWords = 6
	// The judge's own budget. It answers with one small object, and what the
	// tokens are actually for is the goal it writes when the answer is yes.
	routeJudgeTokens = 700
	routeJudgeTemp   = 0
	// routeShapeLines is how much of the assistant's answer the judge is shown.
	// TWO LINES IS THE SHAPE AND NOT THE ANSWER: what the judge is deciding is
	// whether the person's request needed work, and a judge handed the whole reply
	// would be judging the reply.
	routeShapeLines = 2
	routeShapeBytes = 600
	// routeAskBytes bounds what the person's own message contributes. A judgement
	// about the shape of a request is made in its first paragraph, and a pasted
	// stack trace is not evidence about it.
	routeAskBytes = 4000
	// routeGoalBytes bounds the goal the judge writes. It is a brief, not a page.
	routeGoalBytes = 4000
	// routeWhyBytes is the one line the card shows.
	routeWhyBytes = 120
)

// The two shapes a judge may name, spelled once. They are the two things this
// session can actually start on somebody's yes — a run (orchestrate.go) and a
// node (task.go) — and a third word is a judge answering a question nobody
// asked.
const (
	routeShapeAdaptive = "adaptive"
	routeShapeTask     = "task"
)

// routeVerdict is the judge's whole vocabulary. An empty one — the shape of
// every failure — is a no.
type routeVerdict struct {
	Work  bool   `json:"work"`
	Shape string `json:"shape"`
	Goal  string `json:"goal"`
	Why   string `json:"why"`
}

// routeJudgeBrief is what the judge is told, and it is the work-or-words law in
// miniature: the same three sentences the model itself is given, asked as a
// question about a turn that has already happened.
//
// THE CRITICAL-PATH TEST IS THE WHOLE OF IT. Not the size of the request, not
// how important it sounds: whether the fastest correct answer runs through a few
// tool calls or through minutes of them. A judge tuned by size says yes to every
// interesting question somebody asks.
const routeJudgeBrief = `You judge ONE turn of a coding assistant, after the fact. The assistant answered the person in WORDS ALONE — it called no tool. You decide one thing: should that turn have been WORK?

WORDS are a question, a discussion, advice, an opinion, a fact, an explanation, a plan somebody asked to read. SMALL WORK is words too, for this purpose: a few tool calls, one obvious edit, a file read and an answer. Handing small work off is slower than doing it, so it is not work.

WORK is research across several sources, changes across several files, a goal with several independent parts, or anything the person would otherwise watch a spinner for.

THE TEST IS THE CRITICAL PATH AND NOT THE SIZE. If the fastest correct answer runs through the assistant's own tools in a few calls, it is not work, however large the subject sounds. If it runs through minutes of them, or through parts somebody would otherwise serialize by hand, it is work.

Answer with ONE JSON object and nothing else — no prose, no code fence:

  {"work": false}

or

  {"work": true, "shape": "adaptive", "goal": "...", "why": "..."}

  shape  "task" is the default and covers WIDE work too — a sweep across many
         files, research across many sources, a goal with several independent
         parts. One worker starts on it and hands the parts out itself once it
         has opened the material. Answer "adaptive" only when the work needs its
         graph planned before anything starts, or the person asked for a plan
         they can watch and steer. Width alone is not that.
  goal   self-contained. Whoever reads it cannot see this conversation, so fold in
         what the person's words were pointing at: the subject, the files, the
         checks, what a finished answer looks like.
  why    ONE line, in a person's own words, saying what this looks like. It is
         shown to them on a card, so write it as you would say it: "research
         across every package", "a sweep over forty files".

When you are unsure, answer {"work": false}. A wrong yes interrupts somebody who was having a conversation.`

// routeJudge is this file's whole place in a turn, called once from
// [Agent.runTurn] when the model has answered without a tool call.
//
// It reports nothing. Every branch out of it is either a card the person may
// answer or silence, and the turn that called it ends the same way either way —
// which is what lets the hook in the loop be one line with no result to read.
func (a *Agent) routeJudge(ctx context.Context, hub *eventHub, user userMessage, usedTools bool, answer string) {
	// THE TURN COUNTER MOVES ON EVERY TURN, gates or no gates: the limit below is
	// "one offer every three turns of conversation", and a counter that only
	// advanced on the turns this file examined would make it "every three turns
	// this file happened to like".
	a.mu.Lock()
	a.routeTurns++
	turn, offered := a.routeTurns, a.routeOffered
	model, source, closed := a.model, a.config.RolesSource, a.closed
	a.mu.Unlock()
	if closed || usedTools {
		return
	}
	// THE GATES ARE THE OFFER'S GATES, in the order they are cheapest to fail. A
	// card nobody can answer is a model call spent on a question that will never
	// be asked, and a node has no surface at all.
	if !a.config.AskConsent || a.config.InTask {
		return
	}
	if user.empty() || user.wake || user.authored {
		// ONLY WHAT A PERSON TYPED. A woken turn's note is the session talking to
		// itself, and an offer raised against one would be the session offering to
		// spend money on its own sentence (harness.go keeps the same law).
		return
	}
	if !routeSubstantial(user.text()) {
		return
	}
	if offered > 0 && turn-offered < routeJudgeGap {
		return
	}
	// The judge is a ROLE, so an install with no tiers configured resolves it to
	// the conversation's own model rather than refusing — and an install with
	// nothing anywhere gets no judge at all, which is this feature absent rather
	// than broken.
	judge, err := roles.Resolve(roles.Source(source), roles.RoleRouter, model)
	if err != nil || strings.TrimSpace(judge) == "" {
		return
	}

	verdict, ok := a.askRouteJudge(ctx, judge, user.text(), answer)
	if !ok || !verdict.Work {
		return
	}
	if !a.canRunShape(verdict.Shape) {
		// The judge named work this build cannot start — an adaptive run with no
		// runner wired. There is nothing to offer, so nothing is said: a card whose
		// yes could only fail is worse than no card.
		return
	}

	a.mu.Lock()
	a.routeOffered = turn
	a.mu.Unlock()
	if !a.askRouteOffer(ctx, hub, verdict) {
		return
	}
	a.launchRoute(ctx, hub, verdict)
}

// routeSubstantial reports whether a message is worth a model call. It counts
// WORDS rather than bytes because a pasted path is one long word and "have a
// look at the reconciler and tell me why it drops the second event" is eleven
// short ones.
func routeSubstantial(text string) bool {
	return len(strings.Fields(strings.TrimSpace(text))) >= routeJudgeWords
}

// canRunShape reports whether this session could actually start the shape the
// judge named. A task is available in any conversation; a run needs the runner
// (orchestrate.go's own gate), and an unknown word is not a shape.
func (a *Agent) canRunShape(shape string) bool {
	switch strings.ToLower(strings.TrimSpace(shape)) {
	case routeShapeAdaptive:
		return a.canOrchestrate()
	case routeShapeTask:
		return !a.config.InTask
	}
	return false
}

// askRouteJudge is the one call, salvaged. THERE IS NO REPAIR TURN, which is
// where this parts company with the planner and the harness designer
// (orchestrate.go, harness_build.go): both of those are spending a run's or a
// design's whole budget and a second call to rescue it is cheap by comparison.
// This one is a suggestion nobody asked for, and the honest answer to a judge
// that could not write eighty bytes of JSON is to say nothing at all.
func (a *Agent) askRouteJudge(ctx context.Context, judge, asked, answered string) (routeVerdict, bool) {
	response, err := a.client.CompleteWithMessages(
		// WithoutStream for the reason the title, the guardian and the compaction
		// summary use it: this is the session thinking about the conversation, and
		// left on the turn's stream it would type JSON into the room.
		provider.WithoutStream(ctx),
		[]ai.Message{
			textMessage("system", routeJudgeBrief),
			textMessage("user", routeJudgeQuestion(asked, answered)),
		},
		ai.WithModel(judge),
		ai.WithMaxTokens(routeJudgeTokens),
		ai.WithTemperature(routeJudgeTemp))
	if err != nil || response == nil {
		return routeVerdict{}, false
	}
	// The person pays for it, out of the pocket every auxiliary call comes from.
	a.addAuxiliaryUsage(response, judge, 1)

	// The salvage ladder is internal/subharness's, shared rather than reimplemented
	// so that a fenced or smart-quoted reply is read here exactly as it is read
	// everywhere else in this binary.
	raw, err := subharness.Salvage(response.Text())
	if err != nil {
		return routeVerdict{}, false
	}
	var verdict routeVerdict
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return routeVerdict{}, false
	}
	verdict.Shape = strings.ToLower(strings.TrimSpace(verdict.Shape))
	verdict.Goal = clip(strings.TrimSpace(verdict.Goal), routeGoalBytes)
	verdict.Why = clip(firstLine(verdict.Why), routeWhyBytes)
	if verdict.Goal == "" {
		// A yes with nothing to run is not a yes. Whoever would be handed this
		// cannot see the conversation, so an empty goal is a card offering to start
		// work nobody could describe.
		return routeVerdict{}, false
	}
	return verdict, true
}

// routeJudgeQuestion is the turn as the judge reads it: what was asked, and the
// SHAPE of what came back.
func routeJudgeQuestion(asked, answered string) string {
	var out strings.Builder
	out.WriteString("WHAT THE PERSON SAID:\n")
	out.WriteString(clip(strings.TrimSpace(asked), routeAskBytes))
	shape := clip(firstLines(strings.TrimSpace(answered), routeShapeLines), routeShapeBytes)
	if shape == "" {
		shape = "(nothing)"
	}
	out.WriteString("\n\nHOW THE ASSISTANT ANSWERED (the first two lines):\n")
	out.WriteString(shape)
	out.WriteString("\n\nShould that turn have been work? Answer with one JSON object.")
	return out.String()
}

// ── the card ────────────────────────────────────────────────────────────────

// askRouteOffer raises ONE card and waits for the answer or for the turn to end.
//
// IT IS THE HARNESS OFFER'S LANE, not a second one. The question is the same
// question at the same moment of a turn — "this looks like X, shall I?" — it is
// answered by the same two keys through [Agent.ResolveHarness], and a surface
// that draws one draws this. What rides the event is the SHAPE as the card's
// name and the judge's own line as its description, so the row a person reads
// says what would start and why it was raised.
//
// The wait is on the TURN's context, which is what makes Interrupt work on a
// pending card, and nothing here holds a.mu across it.
func (a *Agent) askRouteOffer(ctx context.Context, hub *eventHub, verdict routeVerdict) bool {
	if hub == nil {
		return false
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return false
	}
	a.harnessSeq++
	id := a.harnessSeq
	answers := make(chan harnessAnswer, 1)
	if a.harnessAsks == nil {
		a.harnessAsks = make(map[uint64]harnessAsk, 1)
	}
	a.harnessAsks[id] = harnessAsk{answers: answers}
	a.mu.Unlock()

	hub.send(Event{
		Kind: EventHarnessOffer,
		ID:   id,
		Text: routeShapeWord(verdict.Shape),
		Hint: verdict.Why,
	})

	select {
	case answer := <-answers:
		return answer.run
	case <-ctx.Done():
		a.forgetHarness(id)
		return false
	}
}

// routeShapeWord is what the card calls the thing it is offering. It is the
// person's word for the shape rather than the wire's — "adaptive run" is what
// the conversation, the manual and the room all call one.
func routeShapeWord(shape string) string {
	if shape == routeShapeAdaptive {
		return "adaptive run"
	}
	return "task"
}

// ── what a yes starts ───────────────────────────────────────────────────────

// launchRoute starts what the card promised, through the SAME doors the model's
// own hands go through: run_adaptive's runner for a run, and the task graph's
// admission for a node (tools_harness.go, task.go). Nothing new executes work
// here — a second way to start work is a second way for it to start differently.
func (a *Agent) launchRoute(ctx context.Context, hub *eventHub, verdict routeVerdict) {
	if verdict.Shape == routeShapeAdaptive {
		a.launchRouteRun(ctx, hub, verdict)
		return
	}
	a.launchRouteTask(hub, verdict)
}

// launchRouteRun starts one adaptive run on the session's default tank.
//
// THE DEFAULT IS THE ONLY HONEST FIGURE HERE. Nobody named a budget — the person
// pressed enter on a card, and the judge is not somebody's accountant — so the
// run opens on [orchestrateDefaultCap] and asks for more at the gate with its
// frontier on screen, which orchestrate.go argues is the better question.
func (a *Agent) launchRouteRun(ctx context.Context, hub *eventHub, verdict routeVerdict) {
	id, err := a.startOrchestrate(ctx, verdict.Goal, "", orchestrateDefaultCap)
	if err != nil {
		hub.send(Event{Kind: EventNotice, Text: "the run could not be started: " + err.Error()})
		return
	}
	if id == "" {
		// The runner declined without saying why. Nothing started, and nothing is
		// claimed to have.
		return
	}
	a.announceOrchestrate(id, verdict.Goal, "", orchestrateDefaultCap)
}

// launchRouteTask admits one node from the judge's goal.
//
// IT DOES NOT ASK AGAIN. propose_task's countdown is the consent for work the
// MODEL groomed on its own; this work was offered on a card the person just said
// yes to, and a second question about the same decision is the harness doubting
// an answer it already has. So the spec goes straight to [TaskGraph.admit],
// which is the same admission a countdown that ran out reaches.
//
// THE ACCEPTANCE IS THE HARNESS'S OWN SENTENCE and it is deliberately weak. A
// judge writes a goal and a line; it is never asked for a done-condition,
// because the person it is judging for did not state one — and an acceptance
// this file invented in specifics would be a target nobody set (task_contract.go
// on why a frozen acceptance matters).
func (a *Agent) launchRouteTask(hub *eventHub, verdict routeVerdict) {
	graph := a.graph()
	id := graph.reserve()
	spec := taskSpec{
		title:   clip(firstLine(verdict.Goal), hintLimit),
		summary: verdict.Why,
		// THE PERSON'S OWN MESSAGE RIDES ALONG, as it does on every other door
		// into the graph (task_brief.go). It matters most here: this goal was
		// written by a judge that read their turn and summarised it, so the node
		// would otherwise open on a summary of a summary.
		request:    a.taskRequest(),
		brief:      verdict.Goal,
		acceptance: "the goal above is met, and the report says what was done and how it was checked",
		model:      a.resolveTaskModel("").model,
	}
	if spec.summary == "" {
		spec.summary = spec.title
	}
	state := graph.admit(id, spec)
	word := "started"
	if state == TaskQueued {
		word = "queued"
	}
	if hub != nil {
		hub.send(Event{Kind: EventNotice, Text: "task " + strconv.FormatUint(id, 10) + " " + word + ": " + spec.title})
	}
}
