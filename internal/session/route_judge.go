package session

// THE ROUTE JUDGE: one cheap question, asked at the TWO MOMENTS a turn can
// still be handed over.
//
// The system prompt has a work-or-words law (prompts/system.md): a question, a
// discussion, a fact and a few tool calls are answered here; research across
// sources, changes across files and anything with several independent parts is
// handed over and answered afterwards. The law is a paragraph in a prompt, which
// means it holds exactly as often as the model remembers it, and the turn where
// it is forgotten is invisible: the person gets a good paragraph about work
// nobody started.
//
// So this file asks a SECOND model, and it asks it in two places.
//
//   - BESIDE the turn ([Agent.routeAhead]), reading the REQUEST and nothing
//     else. It is launched at the front of the turn and RUNS WHILE THE MODEL
//     ANSWERS. A yes is TRIAGE and no longer a conversion: it tightens the
//     checkpoint's meter so the work itself is read at the next step boundary
//     instead of after the full handoff price ([Agent.routeTriage]). A verdict
//     that arrives after the turn has finished is dropped.
//   - AFTER a turn that answered in words alone ([Agent.routeJudge]), reading
//     the request and two lines of what came back. A yes starts the work the
//     answer only talked about.
//
// ── WHY THE PRE-TURN READ NO LONGER CONVERTS ──
//
// It used to end the turn it was racing outright, and the benchmark took that
// away from it. Over ten measured cells the raced screen converted BOTH of the
// small-work traps — messages whose fastest correct answer was a few tool calls
// in the conversation, taken out of it and handed to a worker in a worktree. The
// reason is structural rather than a tuning problem: what this read has in front
// of it is a REQUEST NOBODY HAS WORKED ON YET, and no amount of confirming turns
// that into evidence about the work. Reading the work is what checkpoint.go's
// sidecar does, at a mark, over the transcript the turn has actually built.
//
// So the race keeps the thing it is genuinely good at — noticing early that this
// one is worth looking at — and gives up the thing it was bad at. A both-yes now
// pulls the first mark down to the very next boundary, and a person sees NOTHING
// at all until the reading of the work says something (THE EMPTINESS LAW: a
// judgement that changed nothing they can observe is not announced). What its
// verdict wrote about breadth and about the done-condition is kept and rides
// whatever task eventually starts.
//
// THE PRE-TURN READ EXISTS BECAUSE THE POST-TURN ONE CANNOT SEE THE FAILURE THAT
// COSTS THE MOST. A chat message carrying four independent pieces of work was
// answered, twice in measurement, by ninety-odd rounds of inline tool calls: the
// model had propose_task on its belt and a prompt that taught it to hand work
// over mid-turn, and a model deep in tool momentum does not stop to re-consult a
// verb it rarely reaches for. That turn never reaches the post-turn judge at all
// — it called tools, so the second law below excuses it — and by the time it
// ends, the grinding the hand-off would have prevented has already happened. A
// decision the model will not make mid-grind has to be made at a HARNESS SEAM
// before the grinding starts, which is what the pre-turn read is.
//
// NEITHER READ REPLACES THE OTHER. The pre-turn one judges a sentence nobody has
// worked on yet and is wrong in the cautious direction by design; the post-turn
// one still runs on every wordy turn and catches what reading the request alone
// could not have known.
//
// THERE IS ONE ROAD OUT OF HERE and it is a task ([Agent.launchRouteTask]).
// The judge used to name a shape, because there were two roads and the second
// was a planned graph; a chat turn cannot open one of those any more, so the
// word came off the wire with the branch that read it. Nothing was lost that the
// judge could still express: the breadth it used to reach for `adaptive` to say
// is said in `wide`, which arms the one worker to hand the parts out once it has
// opened the material (task_divide.go) — the same road, wider.
//
// SIX LAWS HOLD IT TO SOMETHING NOBODY WILL WANT TURNED OFF.
//
//   - A YES STARTS THE WORK, AND THE PERSON IS TOLD IT STARTED. There is no card
//     and no keypress. An offer is a modal row on a turn nobody said was unusual,
//     and it asks somebody to make a decision about work they have not seen — the
//     task itself is the better version of that question, because it is on the
//     rail, it says what it is doing, and it can be stopped from there. Being
//     told after is the honest shape; every law below is what makes it safe.
//   - IT NEVER SECOND-GUESSES A TURN THAT USED TOOLS. A turn that called tools
//     was already work of some size, and asking afterwards whether work should
//     have been work is a question with no useful answer — so the post-turn read
//     watches a TOOL-LESS turn only. The pre-turn read is made before any tool
//     has run, which is the whole reason it is made there.
//     (The pre-turn read starts nothing directly at all any more — it hands the
//     decision to the reading of the work — but the post-turn read still does,
//     and everything below is written about that.)
//   - IT IS RATE-LIMITED, and auto-start makes the limit MORE load-bearing rather
//     than less: at most one task every [routeJudgeGap] turns, which is also what
//     makes two in a row impossible. Somebody who has just stopped one is having
//     a conversation, and the second task started over the top of it is the one
//     that makes the feature a nuisance. ONE LIMIT COVERS BOTH READS — one
//     counter, one gap, one memory of when work last began — because the person
//     is being interrupted by TASKS and does not care which of the two moments
//     noticed. Two limits would be two tasks in one conversation's breath.
//   - IT IS SILENT WHEN IT CANNOT WORK. No router model, nobody watching this
//     session, a reply that is not JSON, a judge that would not answer at all:
//     each of those is one turn that behaves exactly as it did before this file
//     existed. Nothing is started and nothing is said about a judgement nobody
//     made.
//   - IT IS CHEAP WHERE IT IS BUSY AND DEAR WHERE IT DECIDES. RoleRouter sits
//     on the low tier (internal/roles) because it SCREENS: it is asked after
//     every substantial wordy turn, which is volume, and volume belongs on the
//     cheap model. A wrong no there still costs only a task that was never
//     started. A wrong YES is the one that changed when the card went away — it
//     now spends a task's money — so a yes is never taken from the cheap model
//     alone. It is put ONCE MORE, in the same words and a fresh context, on the
//     tier that thinks ([Agent.confirmRouteWork]), and only both-yes starts
//     anything. The confirm is asked on nothing else, so what it costs over a
//     conversation is nearly nothing and what it stands in front of is a whole
//     task's spend.
//   - IT NEVER STANDS IN FRONT OF A TURN. The post-turn read happens after the
//     answer is on the screen, so it can take the time it takes. The pre-turn one
//     used to be a DEADLINE on the first request — three seconds, and a judge
//     that had not answered by then was a no — and that is the shape this file
//     was rewritten to remove: the bound was tighter than the floor latency of
//     the model it bounded, so every call was issued, every call missed it, and a
//     whole cascade read as a silent no ([routeRaceWindow] carries the
//     measurement). It now RACES the turn instead: the question goes out on a
//     goroutine, the turn proceeds into the model on the same beat, and the
//     answer is spent at a step boundary or not at all. A no costs a person
//     nothing because a no is never waited for, and a yes costs the seconds
//     already spent — which are not lost, because they go with it as the dowry.

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The confirm is registered here, beside the call it belongs to, exactly as the
// auditor and the shaper are (internal/roles states the open-registry law). The
// SCREEN is not: RoleRouter is one of the handful of roles the registry assigns
// itself, because its tier and the worker's are one balance rather than two
// opinions — cheap where the volume is, dear where the decision is.
func init() { roles.Register(roles.RoleRouterConfirm, roles.TierMastermind) }

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
	// tokens are actually for is the two things it WRITES when the answer is
	// yes: the goal, and the done-condition the work is finished against.
	//
	// IT GREW WITH THE SECOND FIELD. A judge that runs out of budget halfway
	// through its object produces JSON nothing can salvage, which this file reads
	// as a no and says nothing about — so a ceiling that fit one written field
	// and not two would have turned the feature off quietly on exactly the
	// requests worth starting. Nine hundred is both fields at their bounds
	// ([routeGoalBytes], taskShapeAcceptanceLimit) with the object around them,
	// and it is still a fraction of what the task it decides costs.
	routeJudgeTokens = 900
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
	// routeRaceWindow is how long the SCREEN gets, and it is generous because
	// NOBODY IS WAITING ON IT. It bounds the race's own life and not a person's
	// patience: the turn it is asked about is already running.
	//
	// IT WAS THREE SECONDS, AND THOSE THREE SECONDS ARE THE MEASURED DEFECT THIS
	// SHAPE EXISTS TO FIX. The bound stood BELOW THE FLOOR LATENCY OF THE MODEL IT
	// BOUNDED: at the prompt sizes this brief actually reaches, the flash tier's
	// median answer took 3.59s and not one of six calls came back inside three
	// seconds. Over ten benchmark cells the pre-turn read completed exactly zero
	// times — every call was issued, every one hit the deadline, and the whole
	// cascade read as a silent no. A deadline shorter than the floor of what it is
	// waiting for is not a bound, it is an off switch with a bill attached.
	//
	// Twenty is several times that floor, which is what a slow answer to a long
	// request needs and still leaves the window bounded. Nothing ordinarily waits
	// for it: the race is ended by the TURN ending ([routeRace.end]), and the
	// clock is only the backstop under a judge that never answers at all.
	routeRaceWindow = 20 * time.Second
	// routeRaceConfirmWindow is the mastermind's, and it breathes for the same
	// reason plus one of its own: it is asked only on the screen's yes, so what a
	// generous bound costs over a conversation is nearly nothing, and the tier
	// that thinks is slower than the tier that screens by about the multiple the
	// screen's own floor moved. It is still a hard bound and a confirm that misses
	// it is a no (the fourth law) — and because it stands after the screen rather
	// than beside it, the two together are the outside of one race's life.
	routeRaceConfirmWindow = 30 * time.Second
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
	// Acceptance is THE DONE-CONDITION THE WORK IS FINISHED AGAINST, and it is
	// asked for here because this door had nothing else that could write one.
	//
	// A task is finished by a checker judging it against its acceptance ALONE —
	// never against the brief, which is the executor's instruction and which the
	// checker is deliberately not shown (task_audit.go's auditQuestion). So a
	// door that admits a generic acceptance admits work nothing can judge, and
	// the sentence this file used to write ("the goal above is met") named a goal
	// that is not above anything the checker ever reads. Every OTHER door already
	// writes a real one: propose_task's schema demands it, `/task` has the shaper
	// write it (task_shape.go), a divided part carries its own (task_divide.go).
	// This is that same field, asked of the judge that is already reading the
	// turn and already writing the goal — one more line in a call that was being
	// made anyway, which is how [Agent.shapeBrief] gets a name for free.
	//
	// AN ABSENT ONE IS SURVIVABLE and falls back to [routeFallbackAcceptance],
	// exactly as an absent one from the shaper falls back to
	// [taskPersonAcceptance]: a judgement nobody asked for must never be the
	// reason work is refused.
	Acceptance string `json:"acceptance"`
	// Delivery is used only by the original-ask acceptance writer.
	Delivery deliveryContract `json:"delivery,omitempty"`
	// Repeatable checks travel with the same request that declared them. A
	// correction may keep the task useful while invalidating its old checks.
	Checks        []string `json:"checks,omitempty"`
	checksRequest string
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
//
// WHAT IT ALSO ASKS FOR IS THE DONE-CONDITION, and that is a field rather than a
// third question: the judge is not deciding anything by writing it, it is
// writing down what a finished answer looks like for the goal it just wrote. The
// paragraph telling it the condition is read ALONE, by somebody who cannot see
// the goal, is the load-bearing half — a condition that says "the goal is met"
// is a condition nobody can check (see [routeVerdict.Acceptance]).
const routeJudgeBrief = `You judge ONE turn of a coding assistant, after the fact. The assistant answered the person in WORDS ALONE — it called no tool. You decide one thing: should that turn have been WORK?

WORDS are a question, a discussion, advice, an opinion, a fact, an explanation, a plan somebody asked to read. SMALL WORK is words too, for this purpose: a few tool calls, one obvious edit, a file read and an answer. Handing small work off is slower than doing it, so it is not work.

WORK is research across several sources, changes across several files, a goal with several independent parts, or anything the person would otherwise watch a spinner for.

THE TEST IS THE CRITICAL PATH AND NOT THE SIZE. If the fastest correct answer runs through the assistant's own tools in a few calls, it is not work, however large the subject sounds. If it runs through minutes of them, or through parts somebody would otherwise serialize by hand, it is work.

A YES STARTS THE WORK IMMEDIATELY. One worker takes the goal you write and the person is told it started. Nobody is asked first, so answer yes only for work you would want begun on your behalf.

` + routeVerdictContract + `

When you are unsure, answer {"work": false}. A wrong yes starts work over the top of somebody who was having a conversation.`

// routeVerdictContract is the WIRE, and it is one const because both readings
// land in the same [routeVerdict] and start the same task. The two briefs ask
// their question of different evidence — a finished turn, an unanswered request
// — and that difference is theirs to spell; the fields, their bounds and what
// each is FOR are the same sentence twice, and a second spelling of them is the
// drift that ends with one door writing an acceptance nobody can check.
const routeVerdictContract = `Answer with ONE JSON object and nothing else — no prose, no code fence:

  {"work": false}

or

  {"work": true, "wide": true, "goal": "...", "acceptance": "...", "checks": ["..."], "why": "..."}

  wide   true when the work is BROAD — many files, many sources, several
         independent parts — so the one worker that starts on it is allowed to
         hand the parts out once it has opened the material. Leave it out for
         work that is one job however long it takes. This is the only place
         breadth is said.
  goal   self-contained. Whoever reads it cannot see this conversation, so fold in
         what the person's words were pointing at: the subject, the files, the
         checks, what a finished answer looks like.
  acceptance
         DONE WHEN — the observable condition that says this is finished, in a
         sentence or two. Somebody ELSE checks it, and they are shown THIS
         SENTENCE ON ITS OWN: not the goal, not this conversation, not the
         worker's account of itself. So name the thing that must exist and the
         check that shows it — "every package under internal/ has been read and
         the report names each pricing bug with its file and line" — and never
         write "the goal is met" or "the task is complete", which give the
         checker nothing to look at.
  checks Optional. ONE simple command per entry that safely re-establishes the
         result, such as a test, build or probe. The checker can run only these
         declared commands. Do not put verification only in acceptance prose.
         NEVER the requested action itself: a deploy, send or one-time job must
         not be repeated. Omit checks when none are known or safe to repeat.
  why    ONE line, in a person's own words, saying what this looks like. It is
         shown to them beside the work, so write it as you would say it:
         "research across every package", "a sweep over forty files".`

// routeJudge is this file's whole place in a turn, called once from
// [Agent.runTurn] when the model has answered without a tool call.
//
// It reports nothing. Every branch out of it is either a task that started and
// one line saying so, or silence, and the turn that called it ends the same way
// either way — which is what lets the hook in the loop be one line with no
// result to read.
func (a *Agent) routeJudge(ctx context.Context, hub *eventHub, user userMessage, usedTools bool, answer string) {
	// THE COUNTER IS READ HERE AND MOVED NOWHERE. It is stepped once per turn, at
	// the front, by [Agent.routeAhead] — which every turn passes through before
	// its first request — so what this reads is THIS turn's number and the two
	// reads share one limit rather than two. Stepping it a second time here would
	// halve the gap and make "every three turns" mean every one and a half.
	a.mu.Lock()
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
	// WHAT THE JUDGE IS SHOWN AS THE ASK, and this is the one gate the goal owner
	// changes (principal.go).
	//
	// ONLY WHAT A PERSON TYPED, when there is a person. A woken turn's note is
	// the session talking to itself, and work started against one would be the
	// session spending money on its own sentence (harness.go keeps the same law).
	//
	// ON A SESSION NOBODY IS SITTING AT, THAT LAW REMOVES THE ONLY ROAD LEFT. A
	// woken turn is the ONLY kind of turn an unattended run has after its first
	// one — a unit of work lands, the note wakes a turn, the model answers it in
	// words — so a rule that skips every woken turn is a rule that makes it
	// impossible for a failed landing to ever start a repair. It was measured
	// doing exactly that.
	//
	// So a [Steward] supplies the ask instead of the message: the goal it is
	// working towards, in the person's own words, frozen at the start of the
	// session. That is not the session's own sentence — it is the only sentence
	// a person ever wrote here — and it is what the judge should have been
	// reading all along on this road. EVERYTHING ELSE STILL STANDS: the word
	// floor, the gap, the cheap screen, the mastermind confirm, and both of them
	// having to say yes.
	asked := user.text()
	if user.empty() || user.wake || user.authored {
		steward := a.steward()
		if steward == nil {
			return
		}
		asked = steward.Ask()
	}
	// THE FLOOR IS THE ASK, and it is cheaper than a model call. A commit,
	// an undo, a one-line edit or a single read is answered here; paying a
	// judge to be told it is work is how F26's commit became a task.
	if trivialAsk(asked) {
		return
	}
	if !routeSubstantial(asked) {
		return
	}
	if offered > 0 && turn-offered < routeJudgeGap {
		return
	}
	// The judge is a ROLE, so an install with no tiers configured resolves it to
	// the conversation's own model rather than refusing — and an install with
	// nothing anywhere gets no judge at all, which is this feature absent rather
	// than broken.
	verdict, ok := a.askRouteJudge(ctx, model, asked, answer)
	if !ok || !verdict.Work {
		return
	}
	// THE NAME IS ASKED FOR THE MOMENT THE JUDGE SAYS WORK, beside the confirm
	// rather than after the node exists (taskname.go's [nameAhead]): the
	// told-after line is the first thing a person reads about this task, and it
	// carries the name wherever the name is in hand. A confirm that declines
	// lets the call go.
	ahead := a.nameAhead(asked)
	defer ahead.release()
	// THE CONFIRM, and it is asked HERE — after the yes and before anything is
	// admitted — because that is the only place it costs anything at all.
	confirmed, ok := a.confirmRouteWork(ctx, model, asked, answer)
	if !ok {
		return
	}
	// AND ITS READING OF BREADTH JOINS THE SCREEN'S. Both models answered the
	// same contract about the same request; [routeWidth] says why either yes is
	// enough and why the mastermind's no is not.
	verdict.Wide = routeWidth(verdict, confirmed)

	// THE GAP IS SPENT BY A START AND BY NOTHING ELSE, which is why this line
	// stands below the confirm rather than above it. The gap is a person's
	// patience: it exists because work appearing over the top of a conversation
	// is an interruption, and three turns of quiet afterwards is what stops the
	// second one from being a nuisance ([routeJudgeGap]). A confirmed no started
	// nothing and said nothing, so there is no interruption for the next three
	// turns to be protected from, and silencing the screen over work that never
	// existed would hand the mistake a second cost.
	//
	// WHAT THAT COSTS IS BOUNDED AND WORTH IT: a stretch of turns the screen
	// likes and the confirm refuses pays one mastermind call each, rather than
	// one every three. It is bounded by the screen saying yes at all, which is
	// the rare half of the rare case, and the alternative is a conversation
	// going deaf for three turns because a cheap model was wrong once.
	a.mu.Lock()
	a.routeOffered = turn
	a.mu.Unlock()
	// THE TITLE COMES OFF THE GOAL ON THIS ROAD, which it may because the goal was
	// written BY A JUDGE, to a contract, in one shot, out of the person's own
	// request — see [Agent.handOverRunningTurn] for why the other road into this
	// call may not do the same with a goal that is a continuation.
	// AND WITH NO DIVISION DRAWN, because nobody has drawn one: this door reads a
	// REQUEST nobody has worked on yet, and the shape of what is left of a turn is
	// a question only a mark can answer (checkpoint.go's [drawnDivision]).
	a.launchRouteTask(hub, verdict, verdict.Goal, drawnDivision{}, ahead)
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
	verdict, ok := a.putRouteQuestion(ctx, roles.RoleRouter, model, routeJudgeBrief, routeJudgeQuestion(asked, answered), asked)
	if !ok {
		return routeVerdict{}, false
	}
	if verdict.Goal == "" {
		// A yes with nothing to run is not a yes. Whoever would be handed this
		// cannot see the conversation, so an empty goal would start work nobody
		// could describe.
		return routeVerdict{}, false
	}
	return verdict, true
}

// confirmRouteWork puts the screen's yes ONCE MORE, on the mastermind tier.
//
// SAME BRIEF, SAME CONTRACT, FRESH CONTEXT. It is not shown the cheap judge's
// answer and is not asked to review it: a second reader handed the first
// reader's verdict is a reader agreeing with it, and what this is for is a
// second INDEPENDENT reading of the same turn. So it is the identical question,
// put to a model that can afford to think about it.
//
// A NO IS SILENCE AND SO IS A FAILURE, which is the file's fourth law applied
// where it now matters most. There is no note, no card, no retry and no repair
// turn — the person never asked either of these models anything, and a yes
// nobody could confirm is exactly the yes this call exists to hold back. It is
// the opposite posture from the division review (task_divide.go), which admits
// its parts when it cannot answer: that plan had already earned its way past
// two measured gates, and this one has earned nothing but a cheap model's
// opinion.
//
// IT IS NEVER ASKED ABOUT A NO, and that is the whole economy of the cascade:
// the trivial turns, the turns that called tools and the plain nos are all
// screened out before this line is reached, so the mastermind is billed once
// per yes and a yes is rare.
//
// THE GOAL IS NOT REQUIRED HERE. The work runs on the goal the screen wrote —
// this call decides one bit and nothing else, and refusing a confirm that
// answered `{"work": true}` would be refusing the answer the brief asks for.
//
// ITS WHOLE VERDICT COMES BACK, THOUGH, BECAUSE OF ONE FIELD. It answers the
// same contract the screen does, so it has already written its own reading of
// breadth — and this call used to parse that field and drop it, which is the
// better reader's answer thrown away at no saving whatever ([routeWidth] is what
// the two readings come to).
func (a *Agent) confirmRouteWork(ctx context.Context, model, asked, answered string) (routeVerdict, bool) {
	verdict, ok := a.putRouteQuestion(ctx, roles.RoleRouterConfirm, model, routeJudgeBrief, routeJudgeQuestion(asked, answered), asked)
	return verdict, ok && verdict.Work
}

// routeWidth is what TWO READINGS OF ONE REQUEST come to on breadth: armed if
// EITHER of them said the work was broad.
//
// THE CASCADE IS NOT A VOTE ON THIS FIELD, and it is worth saying why, because
// the confirm decides everything else here. Whether this is WORK is a question
// with a costly wrong answer in one direction — a yes takes somebody's message
// out of the conversation that was about to answer it — so it is asked twice and
// fails closed, and the mastermind's no is final. BREADTH IS NOT THAT SHAPE.
// Arming only means the worker MAY discover the work is wide once it has opened
// the material; the evidence gate and the reviewer both still stand in front of
// every actual division (task_divide.go). A wrong yes costs a verb on a belt
// nothing makes it use. A wrong no costs the whole road, silently, on exactly
// the work it was built for.
//
// So the readers compose the way an asymmetric cost says they should: the
// mastermind can ARM work the screen read as one job — which is the better
// reader's answer being worth having — and it cannot DISARM work the screen
// called broad, because that would spend the expensive reader's fallibility on
// the side where being wrong is expensive.
func routeWidth(screen, confirm routeVerdict) bool {
	return screen.Wide || confirm.Wide
}

// putRouteQuestion is the one call EVERY reading in this file is made of: ask
// the role, bill the person, salvage the object. The brief and the question ride
// in as arguments because there are two moments and they read different
// evidence; everything after that — the budget, the temperature, the auxiliary
// billing, the salvage ladder and the bounds each written field is held to — is
// the same for all four calls, and a second spelling of it is how a confirm
// slowly stops confirming what its screen answered.
func (a *Agent) putRouteQuestion(ctx context.Context, role roles.Role, model, brief, question, asked string) (routeVerdict, bool) {
	response, judge, err := a.callRole(ctx, role, model,
		[]ai.Message{
			textMessage("system", brief),
			textMessage("user", question),
		},
		ai.WithMaxTokens(routeJudgeTokens))
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
	// THE SAME BOUND THE SHAPER'S DONE-CONDITION IS HELD TO, and it is that
	// constant rather than a second number of this file's own: a condition
	// somebody else can check is a sentence or two whichever call wrote it, and
	// two spellings of that bound would be two answers to one question
	// (task_shape.go's taskShapeAcceptanceLimit).
	verdict.Acceptance = clip(strings.TrimSpace(verdict.Acceptance), taskShapeAcceptanceLimit)
	verdict.checksRequest = asked
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

// ── the pre-turn ask ────────────────────────────────────────────────────────
//
// The same cascade, one moment earlier, on the one piece of evidence that exists
// before a turn has run: the request itself.

// routeAheadBrief is the pre-turn question, and it is NOT the post-turn one with
// a word changed. What the judge is reading is honestly different — an
// unanswered request rather than a finished turn — so the shape of the mistake
// it can make is different too: there is no answer in front of it to be
// impressed by, and a yes here takes the message OUT of the conversation rather
// than adding work beside an answer that was already given.
//
// THE CRITICAL-PATH LAW IS THE SAME LAW, word for word, because it is the thing
// that keeps both readings honest: small work is not work, and the fastest
// correct answer through a few tool calls is a conversation however grand the
// subject sounds.
//
// WHAT IS NEW IS THE THREE SHAPES A REQUEST WEARS. They are the pre-turn read's
// whole edge over the post-turn one: several independent deliverables named in
// one message, a sweep across many files or sources, and an answer somebody
// would otherwise sit and watch a spinner for. THEY ARE TAUGHT AS PRINCIPLES AND
// NEVER AS PATTERNS — there is no list of words, no count of numerals, nothing a
// person could defeat by phrasing a request differently — because a judge tuned
// to surface features is a keyword rule with a bill attached.
const routeAheadBrief = `You judge ONE message a person has just typed to a coding assistant, BEFORE it is answered. Nothing has been done yet: you are reading the REQUEST, not a reply. You decide one thing: is this WORK?

WORDS are a question, a discussion, advice, an opinion, a fact, an explanation, a plan somebody asked to read. SMALL WORK is words too, for this purpose: a few tool calls, one obvious edit, a file read and an answer. Handing small work off is slower than doing it, so it is not work.

WORK is research across several sources, changes across several files, a goal with several independent parts, or anything the person would otherwise watch a spinner for. Three shapes of request are almost always work: one message that asks for SEVERAL INDEPENDENT DELIVERABLES, a SWEEP across many files or many sources, and anything whose answer is MINUTES OF TOOL CALLS the person can only sit and watch.

THE TEST IS THE CRITICAL PATH AND NOT THE SIZE. If the fastest correct answer runs through the assistant's own tools in a few calls, it is not work, however large the subject sounds. If it runs through minutes of them, or through parts somebody would otherwise serialize by hand, it is work.

A YES STARTS THE WORK IMMEDIATELY AND THE CONVERSATION DOES NOT ANSWER THE MESSAGE. One worker is given this message and the goal you write, and the person is told it started rather than being replied to here. So answer yes only for a request you would rather have DONE than ANSWERED.

` + routeVerdictContract + `

When you are unsure, answer {"work": false}. A wrong yes takes somebody's question away from the conversation that was about to answer it.`

// routeAheadQuestion is the turn as the pre-turn judge reads it: the request,
// and NOTHING ELSE. There is no answer to show it and no shape of one to
// summarise — that is the whole difference between the two readings, and it is
// why this file spells the question twice instead of passing an empty string
// into [routeJudgeQuestion] under a heading that would then be a lie.
func routeAheadQuestion(asked string) string {
	var out strings.Builder
	out.WriteString("WHAT THE PERSON JUST ASKED FOR:\n")
	out.WriteString(clip(strings.TrimSpace(asked), routeAskBytes))
	out.WriteString("\n\nIs this work? Answer with one JSON object.")
	return out.String()
}

// routeRace is the pre-turn read AS IT ACTUALLY RUNS: a question in flight
// beside the turn it is about.
//
// IT IS A HANDLE AND NOT A GATE. The loop takes one at the front of the turn,
// never waits on it, and asks it at each step boundary whether it has settled
// yet ([routeRace.yes]) — so a race that is still thinking costs the turn one
// non-blocking channel read per boundary, which is the whole of what this
// mechanism may charge an ordinary message.
//
// THE VERDICT IS WRITTEN ONCE AND READ AFTER. `settled` closing is the only
// synchronisation there is: the goroutine writes both fields before it closes,
// every reader reads them after, and there is no lock because there is nothing
// left to contend for.
type routeRace struct {
	// stop ends the race. It is the turn's own hand on it: a verdict nobody can
	// spend any more must not go on paying for a mastermind call.
	stop    context.CancelFunc
	settled chan struct{}
	verdict routeVerdict
	work    bool
	// spent is the turn's own mark that this verdict has been taken, and it makes
	// ONE VERDICT PER TURN a property of the race rather than of whoever reads it.
	//
	// IT MATTERS BECAUSE A YES CHANGES NOTHING VISIBLE. What it does is tighten
	// the checkpoint's meter ([Agent.routeTriage]), which is a thing worth doing
	// exactly once: a settled race that answered again at every boundary would
	// keep re-pulling a rung that has already moved, and would go on holding a
	// verdict the turn has finished with.
	//
	// IT NEEDS NO LOCK because it is written and read on the TURN'S goroutine
	// alone. The race's own goroutine touches `verdict` and `work` and closes
	// `settled`; it never sees this field, so there is nothing here to contend
	// for.
	spent bool
}

// yes reports the both-yes verdict IF ONE HAS ALREADY LANDED, and never waits.
// A nil race — every gated turn — is a no, which is what lets the loop hold this
// in one line with no branch around it.
//
// A SETTLED RACE ANSWERS ONCE. Whatever the caller does with the verdict, the
// race has said its piece and has nothing further to offer this turn — including
// when the answer it gave was a no, which cannot become a yes later.
func (r *routeRace) yes() (routeVerdict, bool) {
	if r == nil || r.spent {
		return routeVerdict{}, false
	}
	select {
	case <-r.settled:
		r.spent = true
		return r.verdict, r.work
	default:
		return routeVerdict{}, false
	}
}

// end discards the race, whether or not it has answered.
//
// IT DOES NOT WAIT FOR THE GOROUTINE, deliberately. What it is called on is the
// end of a turn, and a turn that waited here for a provider to notice a
// cancelled context would have moved the hang this file removed from the front
// of the turn to the back of it. The goroutine is bounded by its own two windows
// either way, and what it writes after this point is read by nobody.
func (r *routeRace) end() {
	if r == nil || r.stop == nil {
		return
	}
	r.stop()
}

// routeAhead LAUNCHES the read at the front of a turn, called once from
// [Agent.runTurn] before the model has been sent anything.
//
// IT ANSWERS NOTHING AND STOPS NOTHING. It hands back a race the loop carries
// through the turn, and the turn goes into the model on the same beat: the
// question is asked BEFORE the first request because that is when the request is
// the only evidence there is, and the answer is spent AFTER it because a person
// who has pressed enter must not wait on a judgement they never asked for.
// Everything gated here returns nil, which is a turn running exactly as it did
// before this file existed.
//
// WHAT A YES DOES IS TIGHTEN THE CHECKPOINT'S METER ([Agent.routeTriage]), at
// the next step boundary. The turn carries on exactly as it was; what changes is
// that the reading of the WORK — checkpoint.go's sidecar — happens at that
// boundary instead of after ten rounds of tool calls. Everything that can end the
// turn belongs to that reading, and nothing is said to the person here, because
// nothing they can observe has happened. The person's message is already the
// spec's request ([Agent.taskRequest], written into the transcript and into
// a.personAsk by [Agent.startTurnLocked] before the loop begins), so a handover
// out of that reading is genuinely a hand-off rather than a copy, and what the
// conversation had already found out by then goes with it.
//
// WHAT IT COSTS: one low-tier call on a substantial message the gap allows,
// which is a few hundred tokens of request and one small object back. The
// mastermind is asked only on the screen's yes, which is rare, and only ever
// once. That is the same economy the post-turn read keeps (the fifth law) with
// the same counter in front of it, so a conversation cannot be charged twice for
// one turn's worth of judgement.
func (a *Agent) routeAhead(ctx context.Context, user userMessage) *routeRace {
	// THE TURN COUNTER MOVES HERE, AND HERE ONLY. It counts turns of
	// conversation, so it belongs at the FRONT of one — where every turn passes,
	// including the ones that end interrupted or in an error and never reach the
	// post-turn read at all. Both reads then see the same number for the same
	// turn, which is what makes [routeJudgeGap] one limit across the two rather
	// than two limits that happen to share a name.
	a.mu.Lock()
	a.routeTurns++
	turn, offered := a.routeTurns, a.routeOffered
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return nil
	}
	// THE GATES ARE THE POST-TURN READ'S GATES, unchanged and in the same order,
	// because they are gates on STARTING WORK and not on when the question was
	// asked: nobody watching, a node with no surface, a line the session wrote
	// itself, a message too short to be worth a model call, and a start too
	// recent to follow with another. Each of them is a race that is never run at
	// all, which is this feature costing those turns nothing whatever.
	if !a.config.AskConsent || a.config.InTask {
		return nil
	}
	if user.empty() || user.wake || user.authored {
		// ONLY WHAT A PERSON TYPED. A woken turn's note is the session talking to
		// itself, and work started against one would be the session spending money
		// on its own sentence (harness.go keeps the same law).
		return nil
	}
	if len(user.refs) > 0 {
		// AND ONLY WHAT A TASK COULD BE HANDED. A message with pictures attached
		// (image.go) carries evidence that lives in this conversation and nowhere
		// else: a spec is words, so a node started from one would open on the
		// caption alone and the pictures would be answered by nobody. This turn is
		// the only place that message can be looked at, so it is left to it.
		return nil
	}
	asked := user.text()
	if said := strings.TrimSpace(user.said); said != "" {
		// AND ONLY THEIR HALF OF IT. What the model reads is sometimes the
		// person's sentence with an instruction the SESSION wrote in front of it
		// — a draft they marked as standing is the one door that does this
		// (standing_mark.go) — and [userMessage.said] is the half they typed. A
		// judge shown the instruction would be judging the session's own words,
		// which is the same law the wake check above keeps.
		asked = said
	}
	// THE FLOOR IS THE ASK. A trivial verb is never worth a raced yes, and
	// a yes here only pulls the checkpoint's first mark forward — which is
	// still a look a one-command turn must not pay for.
	if trivialAsk(asked) {
		return nil
	}
	if !routeSubstantial(asked) {
		return nil
	}
	if offered > 0 && turn-offered < routeJudgeGap {
		return nil
	}
	// AND THE CASCADE RUNS ON A GOROUTINE. Both calls are made here, in order and
	// on one context, because the confirm is still the screen's second reader and
	// nothing about racing the turn changes what the two of them are for: the
	// screen is cheap and volume, the confirm is dear and decides, and a screen
	// that could start work on its own is the thing this cascade exists to
	// prevent. What changed is only that nobody is standing in front of them.
	//
	// THE GAP IS NOT SPENT HERE. A verdict is not a start — it may never be spent
	// at all, because the turn it is racing can finish first — and charging a
	// person three turns of quiet for work that never began would be the same
	// mistake a confirmed no would make. It is spent where the work actually
	// starts ([Agent.handOverRunningTurn]).
	raceCtx, stop := context.WithCancel(ctx)
	race := &routeRace{stop: stop, settled: make(chan struct{})}
	go func() {
		defer close(race.settled)
		verdict, ok := a.askRouteAhead(raceCtx, asked)
		if !ok || !verdict.Work {
			return
		}
		confirmed, ok := a.confirmRouteAhead(raceCtx, asked)
		if !ok {
			return
		}
		// THE VERDICT THAT LANDS IS THE SCREEN'S, WITH THE CONFIRM'S BREADTH
		// FOLDED IN. The goal and the done-condition stay the screen's because
		// the confirm is asked to decide one bit and is not required to write
		// either ([Agent.confirmRouteWork]); breadth is the one field both of
		// them answer, and [routeWidth] is what two answers to it come to.
		verdict.Wide = routeWidth(verdict, confirmed)
		race.verdict, race.work = verdict, true
	}()
	return race
}

// routeTriage is where the race LANDS, called at every step boundary of the turn
// it is racing ([Agent.runTurn]).
//
// IT IS NOT A DOOR ANY MORE, AND THAT IS THE POINT OF THE WAVE. It used to end
// the turn here and hand it to the graph; the benchmark measured that conversion
// taking both of its small-work traps out of the conversation, because a read of
// an UNANSWERED REQUEST cannot tell a job that is four jobs from a job that
// sounds like four. So what a both-yes buys now is a LOOK, sooner: the
// checkpoint's first mark moves down to this boundary, and the reading that can
// actually end a turn is the one over the transcript the turn has built
// (checkpoint.go's [Agent.readMark]).
//
// IT SAYS NOTHING AND STARTS NOTHING, which is why it answers nothing either. The
// person is not told that a judgement was made about their message, because at
// this instant nothing has happened to their turn that they could observe — THE
// EMPTINESS LAW, applied to an event rather than to a number.
//
// A LATE VERDICT IS STILL DROPPED, and that is still a law. The turn that
// finished already had its own reading (the post-turn judge, this file's second
// moment), and tightening a meter that belongs to a turn which has ended would be
// carrying one turn's triage into the next one. So the race is asked only at a
// boundary of a turn that is still running, and the turn's end throws whatever is
// left of it away.
//
// AND THE GATES THAT USED TO STAND HERE STAND ONE LINE LATER. An interrupt and a
// closed session both used to be re-read here before anything was admitted;
// nothing is admitted here now, and [Agent.checkpoints] re-reads both of them in
// front of every call and every handover this can lead to.
func (a *Agent) routeTriage(race *routeRace, meter *checkpointMeter) {
	verdict, ok := race.yes()
	if !ok {
		return
	}
	// ONE VERDICT PER TURN. The race answered, so it is over whatever this
	// boundary does with the answer — there is no second chance to spend it and
	// nothing left for it to pay for.
	race.end()
	meter.tighten(verdict)
}

// askRouteAhead is the screen, BOUNDED and ASKED OF THE CREW ALONE. It is
// [Agent.askRouteJudge]'s call with the pre-turn brief, a window over it, and NO
// SESSION-MODEL FLOOR under it — the empty `sessionDefault`.
//
// THE LADDER'S FLOOR IS THE CONVERSATION'S OWN MODEL (auxiliary.go), which is
// the right last resort for an errand nobody is waiting on and the wrong one
// here — for a reason that survived the move off the critical path. That model
// is the biggest and slowest thing in the build; asked to screen every
// substantial message it would spend a person's money at full price to answer a
// question they never asked, and it would answer so late that the turn it is
// racing would usually be over. So an install whose crew has no cheap rung for
// this role — no pin, no low tier — gets NO PRE-TURN READ AT ALL, which is the
// codebase's own law about a capability that cannot work being absent rather
// than broken. The post-turn read still stands there, on the floor it can
// afford, and catches the same turn a moment later.
//
// A judge that misses the window is a judge that did not answer, which this file
// has always read as a no and said nothing about — so the timeout needs no
// branch of its own. There is no retry, for the reason there is no repair turn:
// a judgement nobody requested does not get to spend a second call, and the turn
// it was asked about has probably ended by then anyway.
func (a *Agent) askRouteAhead(ctx context.Context, asked string) (routeVerdict, bool) {
	ctx, done := context.WithTimeout(ctx, routeRaceWindow)
	defer done()
	verdict, ok := a.putRouteQuestion(ctx, roles.RoleRouter, "", routeAheadBrief, routeAheadQuestion(asked), asked)
	if !ok || verdict.Goal == "" {
		// A yes with nothing to run is not a yes: whoever is handed this cannot see
		// the conversation, so an empty goal would start work nobody could describe.
		return routeVerdict{}, false
	}
	return verdict, true
}

// confirmRouteAhead puts the pre-turn yes once more on the tier that thinks,
// under a window of its own.
//
// SAME BRIEF, SAME CONTRACT, FRESH CONTEXT, and the same fail-closed posture
// [Agent.confirmRouteWork] keeps: it is not shown the screen's verdict, it is
// asked once, and anything that is not a yes — prose, a fault, a window that ran
// out — is a no that starts nothing and says nothing.
//
// IT IS THE CREW'S CALL TOO, with no session-model floor for the reason the
// screen has none: this decision is the mastermind's or it is not made, and a
// question that quietly fell through to the model the person is talking to would
// be the conversation confirming its own hand-off.
//
// IT IS STILL SEQUENTIAL, AND IT IS NOW OFF THE CRITICAL PATH TOO. It runs after
// the screen because a second reader is only worth having on a yes; it costs the
// turn nothing because the turn is already in the model by the time either of
// them is asked.
//
// AND ITS OWN READING OF BREADTH RIDES BACK WITH IT, for [confirmRouteWork]'s
// reason: this is the one door where a converted message becomes a task nobody
// groomed, so the field that arms it is the field with the most riding on it and
// the reader most likely to get it right was answering it into a bin.
func (a *Agent) confirmRouteAhead(ctx context.Context, asked string) (routeVerdict, bool) {
	ctx, done := context.WithTimeout(ctx, routeRaceConfirmWindow)
	defer done()
	verdict, ok := a.putRouteQuestion(ctx, roles.RoleRouterConfirm, "", routeAheadBrief, routeAheadQuestion(asked), asked)
	return verdict, ok && verdict.Work
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
// THE ACCEPTANCE IS THE JUDGE'S OWN DONE-CONDITION, written in the same call
// that wrote the goal ([routeVerdict.Acceptance]), and [routeAcceptance] stands
// in for it when the judge did not write one.
//
// IT HANDS BACK THE LINE IT SAID, AND THE NODE'S NUMBER. The post-turn caller has
// no use for the line — the person already has an answer on the screen and this
// note goes under it — but the pre-turn one ends the turn on this sentence and
// records it as the turn's answer ([Agent.routeAhead]), and a second copy of the
// wording assembled there would be the one that drifts. The number is what lets
// the checkpoint's own journal line name the task its ceiling started
// (checkpoint.go's [journalCeiling]); reading it back out of the line would be a
// number parsed out of a sentence written for a person.
//
// THE TITLE IS THE CALLER'S TO CHOOSE, and that is the one thing that is not the
// same on the two roads in. A goal a judge wrote is a sentence about the work and
// its front makes a serviceable name; a goal that is a MODEL'S CONTINUATION on a
// transcript full of tool calls is not, and has been measured opening with a
// provider's tool-call sentinel and with the closing remark of a finished answer
// (checkpoint.go). So the source is named at each call site rather than assumed
// here, and whatever arrives is put through the hand that cleans every other name
// on this surface ([routeTaskTitle]).
//
// AND IT CARRIES THE DIVISION A MARK ALREADY DREW, where a mark drew one. It is a
// PARAMETER and not a field on the verdict because the verdict is the judge's own
// vocabulary and no judge writes this: it is the second reader's drawing of what
// is left, taken at a checkpoint mark, and the spec is where it has to land so
// that the worker this admits can be started on it (task_divide_sketch.go). An
// empty one is every other door, and changes nothing.
//
// AND IT CARRIES THE NAME THE ROAD ASKED FOR AHEAD (taskname.go's [nameAhead]),
// which may be nil. One that has landed is the title from the first line a
// person reads, written as a name a model wrote so nothing renames it; one
// still in flight rides the spec, and the graph waits for it rather than asking
// again.
func (a *Agent) launchRouteTask(hub *eventHub, verdict routeVerdict, title string, drawn drawnDivision, ahead *nameAhead) (string, uint64) {
	// THE LAST LINE OF THE FLOOR (spawnfloor.go). Both roads into this function
	// already return above on a one-command ask; a reserved id for work that
	// must not start would be the floor leaking a node number into a conversation
	// that is answering inline.
	if trivialAsk(a.taskRequest()) {
		return "", 0
	}
	graph := a.graph()
	id := graph.reserve()
	spec := taskSpec{
		drawn:   drawn,
		title:   routeTaskTitle(title),
		summary: verdict.Why,
		// THE PERSON'S OWN MESSAGE RIDES ALONG, as it does on every other door
		// into the graph (task_brief.go). It matters most here: this goal was
		// written by a judge that read their turn and summarised it, so the node
		// would otherwise open on a summary of a summary.
		request: a.taskRequest(),
		// AND WHERE THOSE WORDS LIVE, the same pointer every other door sets
		// (task_brief.go). The checkpoint road reaches here through
		// [Agent.handOverRunningTurn] and does not build a spec of its own, so
		// this one assignment covers both roads.
		origin: a.taskOriginRef(),
		// AND THE WORKING CONTEXT, from the one compiler every door uses
		// (admission.go). This door needs it more than any other: nobody asked
		// for this work, so the only account of why it exists is a judge's
		// summary — and on the checkpoint road the turn being handed over has
		// already made the calls whose handles are in here.
		admission: a.admissionContext(),
		// THE BRIEF IS THE STATE AND THE ACCEPTANCE IS THE ASK, and they are two
		// different documents that were quietly collapsing into one.
		//
		// A goal that arrives here off the checkpoint road is a HANDOFF BRIEF: what
		// is left, what is known, what was ruled out (checkpoint.go). That is the
		// right thing to open a worker on and the wrong thing to finish it against —
		// and with no done-condition from a judge, the stand-in used to point the
		// checker at the task's TITLE, which on that road is a sentence cut off the
		// front of the brief. Measured, the acceptance of a ten-hour ask became "the
		// work named at the top is done", where the work named at the top was a
		// compile to-do the turn happened to be holding. The person's question had
		// stopped being the question anybody was answering.
		//
		// So the person's own words stand behind the judge's own done-condition and
		// in front of the generic stand-in ([routeAcceptance]).
		brief:      verdict.Goal,
		acceptance: routeAcceptance(verdict, a.taskRequest()),
		checks:     routeChecks(verdict, a.taskRequest()),
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
	if name, landed := ahead.ready(); landed && name != "" {
		spec.title, spec.named = name, true
	} else {
		spec.ahead = ahead
	}
	ahead.claim()
	// AND WHERE THE WORK STANDS (taskstands.go), on the same ladder every other
	// door climbs. This door cannot ask anything either — the person is told
	// afterwards that work began — so an evidence that will not settle falls back
	// to the workspace, which is what this card has always used.
	stand := a.taskGroundOrStandingIn(spec)
	spec.ground, spec.mode = stand.dir, stand.mode
	state := graph.admit(id, spec)
	word := "started"
	if state == TaskQueued {
		word = "queued"
	}
	// THE TOLD-AFTER LINE, and it opens by saying why work began that nobody
	// asked for. It is [EventNotice] — the dim one-liner a surface already draws
	// for machinery it did not choose to run — rather than a kind of its own,
	// because what a person needs here is the same thing that note always gives
	// them: what happened, once, without stopping anything. The task itself says
	// the rest, on the rail.
	said := "this looked like work, so task " +
		strconv.FormatUint(id, 10) + " " + word + ": " + spec.title
	hub.send(Event{Kind: EventNotice, Text: said})
	return said, id
}

// routeTaskTitle is the name an auto-started task wears until the namer improves
// it (taskname.go), and it is the SAME HAND that cleans both namers' answers
// ([cleanTitle], title.go) rather than a second one of this file's own.
//
// IT IS CLEANED BECAUSE THIS ONE IS READ ALOUD. Every other title on this road
// arrives from a call that asked for a name; this one is cut off the front of a
// paragraph nobody wrote to be a name, and it goes straight into the told-after
// line a person reads. A rail row rendering `**refactor beta.py — 15+ steps**`
// was measured, so the markdown a model emphasises with comes off here with the
// quotes, the announcements and the trailing full stop that hand already takes.
//
// AN ANSWER THE CLEANER REFUSES ENTIRELY still gets a title, which is where this
// parts company with [cleanTaskName]: that one is choosing whether to REPLACE a
// name and may answer "leave it alone", and this one is the only name the task
// has. So the raw first line stands when nothing survives the cleaning, and the
// namer has the row a second later either way.
func routeTaskTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	if title := cleanTitle(raw); title != "" {
		return title
	}
	return clip(firstLine(raw), hintLimit)
}

// routeFallbackAcceptance is what an auto-started task is finished against when
// the judge wrote no done-condition of its own.
//
// IT NAMES WHAT THE CHECKER CAN ACTUALLY SEE. The checker is handed the task's
// TITLE and this sentence and nothing else — no goal, no brief, no conversation
// (task_audit.go's auditQuestion) — so the sentence that stood here before,
// "the goal above is met", pointed at a paragraph that is above nothing the
// checker reads, and a check against it was a check against a blank. This one
// points at the title, which is on the page, and it asks for the two things any
// piece of work can be held to: that the thing named was actually done, and that
// the account of it says how that was checked.
//
// IT IS STILL WEAK, and deliberately so. A generic sentence this file invented
// in specifics would be a target nobody set (task_contract.go on why the
// acceptance is frozen). The strong version is the judge's own, which is why it
// is asked for.
const routeFallbackAcceptance = "the work named at the top is actually done, and the report says what was done and how it was checked"

// routeAskAcceptance is what stands in front of the person's own words when they
// become the done-condition, and it is one line because the words under it are
// the target and this is only the frame.
//
// IT SAYS "EVERYTHING" ON PURPOSE. The checker reads this and the title and
// nothing else (task_audit.go's auditQuestion), so it has no way of knowing that
// the paragraph it is holding is the WHOLE ask rather than one piece of it — and a
// half-finished piece of work reads as finished against a done-condition that
// quotes only the piece. It also asks for the account, which is the one thing
// every done-condition on this road asks for.
const routeAskAcceptance = "everything asked for below is actually done — all of it, not the part that was easiest to " +
	"reach — and the report says what was done and how that was checked:\n\n"

// routeAcceptance is the done-condition an auto-started task carries, and the
// ladder is THE PERSON'S QUESTION FIRST WHERE NOBODY WROTE A BETTER ONE.
//
//  1. THE JUDGE'S OWN, when a judge wrote one. It read the request and composed a
//     done-condition for it, which is a sentence about this work rather than a
//     frame around it.
//  2. THE PERSON'S OWN WORDS, framed. This rung is new and it is the one the
//     measurement demanded: work that starts out of a turn nobody groomed has no
//     judge's sentence on the checkpoint road, and what it used to fall to was a
//     generic line pointing at a title. Their words are the one thing on this road
//     nobody wrote, they are already carried on the spec's request, and a task
//     finished against them is a task finished against what was asked.
//  3. AND THE GENERIC STAND-IN LAST, for the case with neither: a graph built by
//     hand, a restored node, a door that carried no request at all. It is still
//     weak and still deliberately so — a sentence invented here in specifics would
//     be a target nobody set.
func routeAcceptance(verdict routeVerdict, request string) string {
	if acceptance := strings.TrimSpace(verdict.Acceptance); acceptance != "" {
		return acceptance
	}
	if asked := clip(strings.TrimSpace(request), briefAskLimit); asked != "" {
		return routeAskAcceptance + asked
	}
	return routeFallbackAcceptance
}

// A routed check is still a declared check, with the same validation as an
// explicit task proposal. A stale request or malformed list grants no commands;
// neither can stop useful work from being handed over without those checks.
func routeChecks(verdict routeVerdict, request string) []string {
	if request == "" || verdict.checksRequest != request {
		return nil
	}
	checks, _ := declaredCheckList(verdict.Checks)
	return checks
}
