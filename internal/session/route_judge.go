package session

// THE ROUTE JUDGE: one cheap question asked AFTER a turn that answered in words.
//
// The system prompt has a work-or-words law (prompts/system.md): a question, a
// discussion, a fact and a few tool calls are answered here; research across
// sources, changes across files and anything with several independent parts is
// handed over and answered afterwards. The law is a paragraph in a prompt, which
// means it holds exactly as often as the model remembers it, and the turn where
// it is forgotten is invisible: the person gets a good paragraph about work
// nobody started.
//
// So this file asks a SECOND model, once, after the fact. It sees what the
// person said and two lines of what came back, and it answers one thing: should
// that have been work? A yes STARTS the work and tells them it started.
//
// THERE IS ONE ROAD OUT OF HERE and it is a task ([Agent.launchRouteTask]).
// The judge used to name a shape, because there were two roads and the second
// was a planned graph; a chat turn cannot open one of those any more, so the
// word came off the wire with the branch that read it. Nothing was lost that the
// judge could still express: the breadth it used to reach for `adaptive` to say
// is said in `wide`, which arms the one worker to hand the parts out once it has
// opened the material (task_divide.go) — the same road, wider.
//
// FIVE LAWS HOLD IT TO SOMETHING NOBODY WILL WANT TURNED OFF.
//
//   - A YES STARTS THE WORK, AND THE PERSON IS TOLD IT STARTED. There is no card
//     and no keypress. An offer is a modal row on a turn nobody said was unusual,
//     and it asks somebody to make a decision about work they have not seen — the
//     task itself is the better version of that question, because it is on the
//     rail, it says what it is doing, and it can be stopped from there. Being
//     told after is the honest shape; every law below is what makes it safe.
//   - IT ONLY WATCHES A TOOL-LESS TURN. A turn that called tools was already
//     work of some size, and asking whether work should have been work is a
//     question with no useful answer.
//   - IT IS RATE-LIMITED, and auto-start makes the limit MORE load-bearing rather
//     than less: at most one task every [routeJudgeGap] turns, which is also what
//     makes two in a row impossible. Somebody who has just stopped one is having
//     a conversation, and the second task started over the top of it is the one
//     that makes the feature a nuisance.
//   - IT IS SILENT WHEN IT CANNOT WORK. No router model, nobody watching this
//     session, a reply that is not JSON, a judge that would not answer at all:
//     each of those is one turn that behaves exactly as it did before this file
//     existed. Nothing is started and nothing is said about a judgement nobody
//     made.
//   - IT IS CHEAP. RoleRouter sits on the low tier (internal/roles) because it
//     reads one turn and answers one bounded question, and a wrong no costs a
//     task that was never started rather than money.

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// routeJudgeGap is how many turns must pass between two of these starts.
	// Three is a person's patience rather than a budget: work that began without
	// being asked for is already an interruption, and the one that begins on the
	// turn after they stopped the last is what teaches somebody to reach for the
	// rail's stop key without reading. It also settles the "never twice in a row"
	// rule outright — two consecutive turns can never both start something.
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
	// routeWhyBytes is the one line the person is shown about why this started:
	// it rides the note on the transcript and stays under the task's row.
	routeWhyBytes = 120
)

// routeVerdict is the judge's whole vocabulary. An empty one — the shape of
// every failure — is a no.
//
// THERE IS NO SHAPE FIELD, and its absence is deliberate rather than an
// oversight. It carried one legal value once the planned-graph road closed, and
// a field on the wire that code no longer branches on is a field a model reasons
// about for nothing. A judge that still writes one is answering a question this
// brief does not ask, and encoding/json drops it where it belongs.
type routeVerdict struct {
	Work bool   `json:"work"`
	Goal string `json:"goal"`
	Why  string `json:"why"`
	// Wide is THE JUDGE'S OWN READING OF BREADTH, and it arms the task this door
	// starts ([taskSpec.wide], task.go).
	//
	// It is here because this door had no honest place to put the judgement it
	// was already making. The judge is asked for a self-contained goal and never
	// for a count, so the only signal reaching [Agent.armDivision] from here was
	// the text gate — which reads a number only where it stands beside one of
	// eighteen item-nouns, and therefore counts zero on almost every goal a judge
	// writes ("research the pricing tiers of every major cloud provider" is
	// zero). The one model-decided door for wide work admitted unarmed.
	//
	// A WRONG YES COSTS NOTHING, which is why it is free to take. Arming only
	// means the worker MAY discover it is wide; the evidence gate still refuses a
	// division the material does not support (task_divide.go).
	Wide bool `json:"wide"`
}

// routeJudgeBrief is what the judge is told, and it is the work-or-words law in
// miniature: the same three sentences the model itself is given, asked as a
// question about a turn that has already happened.
//
// THE CRITICAL-PATH TEST IS THE WHOLE OF IT. Not the size of the request, not
// how important it sounds: whether the fastest correct answer runs through a few
// tool calls or through minutes of them. A judge tuned by size says yes to every
// interesting question somebody asks.
//
// IT ASKS TWO QUESTIONS AND NOT THREE. Is this work, and is it wide. There is
// nothing here about graphs, planners or shapes, because there is nothing left
// in this file that could open one — and a brief that taught a choice the code
// no longer makes would be teaching drift.
const routeJudgeBrief = `You judge ONE turn of a coding assistant, after the fact. The assistant answered the person in WORDS ALONE — it called no tool. You decide one thing: should that turn have been WORK?

WORDS are a question, a discussion, advice, an opinion, a fact, an explanation, a plan somebody asked to read. SMALL WORK is words too, for this purpose: a few tool calls, one obvious edit, a file read and an answer. Handing small work off is slower than doing it, so it is not work.

WORK is research across several sources, changes across several files, a goal with several independent parts, or anything the person would otherwise watch a spinner for.

THE TEST IS THE CRITICAL PATH AND NOT THE SIZE. If the fastest correct answer runs through the assistant's own tools in a few calls, it is not work, however large the subject sounds. If it runs through minutes of them, or through parts somebody would otherwise serialize by hand, it is work.

A YES STARTS THE WORK IMMEDIATELY. One worker takes the goal you write and the person is told it started. Nobody is asked first, so answer yes only for work you would want begun on your behalf.

Answer with ONE JSON object and nothing else — no prose, no code fence:

  {"work": false}

or

  {"work": true, "wide": true, "goal": "...", "why": "..."}

  wide   true when the work is BROAD — many files, many sources, several
         independent parts — so the one worker that starts on it is allowed to
         hand the parts out once it has opened the material. Leave it out for
         work that is one job however long it takes. This is the only place
         breadth is said.
  goal   self-contained. Whoever reads it cannot see this conversation, so fold in
         what the person's words were pointing at: the subject, the files, the
         checks, what a finished answer looks like.
  why    ONE line, in a person's own words, saying what this looks like. It is
         shown to them beside the work, so write it as you would say it:
         "research across every package", "a sweep over forty files".

When you are unsure, answer {"work": false}. A wrong yes starts work over the top of somebody who was having a conversation.`

// routeJudge is this file's whole place in a turn, called once from
// [Agent.runTurn] when the model has answered without a tool call.
//
// It reports nothing. Every branch out of it is either a task that started and
// one line saying so, or silence, and the turn that called it ends the same way
// either way — which is what lets the hook in the loop be one line with no
// result to read.
func (a *Agent) routeJudge(ctx context.Context, hub *eventHub, user userMessage, usedTools bool, answer string) {
	// THE TURN COUNTER MOVES ON EVERY TURN, gates or no gates: the limit below is
	// "one start every three turns of conversation", and a counter that only
	// advanced on the turns this file examined would make it "every three turns
	// this file happened to like".
	a.mu.Lock()
	a.routeTurns++
	turn, offered := a.routeTurns, a.routeOffered
	model, closed := a.model, a.closed
	a.mu.Unlock()
	if closed || usedTools {
		return
	}
	// THE GATES ARE THE START'S GATES, in the order they are cheapest to fail.
	// Work started in a session nobody is watching is a model call spent on a
	// surprise nobody will see, and a node has no surface at all.
	if !a.config.AskConsent || a.config.InTask {
		return
	}
	if user.empty() || user.wake || user.authored {
		// ONLY WHAT A PERSON TYPED. A woken turn's note is the session talking to
		// itself, and work started against one would be the session spending money
		// on its own sentence (harness.go keeps the same law).
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
	verdict, ok := a.askRouteJudge(ctx, model, user.text(), answer)
	if !ok || !verdict.Work {
		return
	}

	a.mu.Lock()
	a.routeOffered = turn
	a.mu.Unlock()
	a.launchRouteTask(hub, verdict)
}

// routeSubstantial reports whether a message is worth a model call. It counts
// WORDS rather than bytes because a pasted path is one long word and "have a
// look at the reconciler and tell me why it drops the second event" is eleven
// short ones.
func routeSubstantial(text string) bool {
	return len(strings.Fields(strings.TrimSpace(text))) >= routeJudgeWords
}

// askRouteJudge is the one call, salvaged. THERE IS NO REPAIR TURN, which is
// where this parts company with the planner and the harness designer
// (orchestrate.go, harness_build.go): both of those are spending a run's or a
// design's whole budget and a second call to rescue it is cheap by comparison.
// This one is a judgement nobody asked for, and the honest answer to a judge
// that could not write eighty bytes of JSON is to say nothing at all.
// The judge is a ROLE, so an install with no tiers configured resolves it to the
// conversation's own model rather than refusing — and an install with nothing
// anywhere gets no judge at all, which is this feature absent rather than broken.
func (a *Agent) askRouteJudge(ctx context.Context, model, asked, answered string) (routeVerdict, bool) {
	response, judge, err := a.callRole(ctx, roles.RoleRouter, model,
		[]ai.Message{
			textMessage("system", routeJudgeBrief),
			textMessage("user", routeJudgeQuestion(asked, answered)),
		},
		ai.WithMaxTokens(routeJudgeTokens),
		ai.WithTemperature(routeJudgeTemp))
	if err != nil || response == nil {
		return routeVerdict{}, false
	}
	// The person pays for it, out of the pocket every auxiliary call comes from,
	// against the model that answered.
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
	verdict.Goal = clip(strings.TrimSpace(verdict.Goal), routeGoalBytes)
	verdict.Why = clip(firstLine(verdict.Why), routeWhyBytes)
	if verdict.Goal == "" {
		// A yes with nothing to run is not a yes. Whoever would be handed this
		// cannot see the conversation, so an empty goal would start work nobody
		// could describe.
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

// ── what a yes starts ───────────────────────────────────────────────────────

// launchRouteTask admits one node from the judge's goal. IT IS THE ONLY LANDING
// out of this file, and it goes through the SAME door the model's own hands go
// through — the task graph's admission (task.go). Nothing new executes work
// here: a second way to start work is a second way for it to start differently.
//
// IT DOES NOT ASK. propose_task's countdown is the consent for work the MODEL
// groomed on its own, and this work was not groomed by anybody — it is the
// judge's reading of a sentence a person already typed. The question a card
// would put ("shall I?") is a question about work nobody has seen yet; the task
// answers it better by existing, on the rail, saying what it is doing and
// stoppable there. So the spec goes straight to [TaskGraph.admit], which is the
// same admission a countdown that ran out reaches.
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
		// THE JUDGE'S OWN WIDE VERDICT ARMS THE TASK IT STARTS. It is the same
		// judgement the sizing judge is asked at the typed door and the same one
		// propose_task carries in `wide` — see [routeVerdict.Wide] for why this
		// door had nothing else to arm with.
		wide: verdict.Wide,
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
		// THE TOLD-AFTER LINE, and it opens by saying why work began that nobody
		// asked for. It is [EventNotice] — the dim one-liner a surface already
		// draws for machinery it did not choose to run — rather than a kind of its
		// own, because what a person needs here is the same thing that note always
		// gives them: what happened, once, without stopping anything. The task
		// itself says the rest, on the rail.
		hub.send(Event{Kind: EventNotice, Text: "this looked like work, so task " +
			strconv.FormatUint(id, 10) + " " + word + ": " + spec.title})
	}
}
