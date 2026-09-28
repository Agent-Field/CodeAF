package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/subharness"
)

// taskJudgeTimeout is the sizing judge's own deadline. It is the call's end and
// no longer anybody's pause: the judge reads the person's sentence beside a
// worker that has already started, and three seconds is the figure it carried
// when the surface held the command for it, left where it was.
const taskJudgeTimeout = 3 * time.Second

// taskJudgePrompt sizes one piece of work. WHAT IT DECIDES IS NARROWER THAN IT
// LOOKS: the answer never settles whether the work starts, or how. The work has
// already started as one worker by the time this is asked — the judge reads the
// person's sentence BESIDE that worker (task_divide_sketch.go's
// [Agent.proposalBeside]) — and its yes is a division proposed for the worker,
// put through the same gates and the same reviewer any division is. A no costs
// nothing, because one worker is what is already running, which is what frees
// the judge to be strict ("when unsure, false").
const taskJudgePrompt = `Decide whether this task is meaningfully parallelizable or nestable for speed or quality. Parallel means independent parts can proceed at the same time and a planner can combine them; a merely long sequence is not parallel.

Answer with exactly one JSON object and no markdown:
{"parallelizable":bool,"parts":["part in at most 6 words"],"why":"reason in at most 12 words"}

Use at most 6 parts. When unsure, set parallelizable to false.

You are only deciding whether to plan the work up front. Work that starts as one worker can still split itself later, once the worker has opened the material and can see how much of it there is, so a "false" here does not commit the work to one pair of hands. Say true only when the parts are already visible from the request itself.`

type taskJudgeVerdict struct {
	Parallel bool     `json:"parallelizable"`
	Parts    []string `json:"parts"`
	Why      string   `json:"why"`
}

// StartTask starts one person-authored task without routing it through the chat
// model or presenting the model's proposal card.
//
// ── NOTHING IS WAITED FOR IN FRONT OF IT ──
//
// WHAT WAS TRUE: the surface asked the sizing judge (three seconds) and then this
// door asked the shaper (twenty-five) before the node was admitted, in series,
// and on a thinking model both ran their windows out and returned nothing — so
// every `/task` read `shaping the brief…` for twenty-eight seconds and then
// started on the person's sentence anyway (issue #936).
//
// WHAT IS TRUE NOW: the node is admitted here, at once, on the person's own
// sentence and the canned done-condition, and both readings run BESIDE ITS FIRST
// WORKER through the one mechanism every answer beside the work comes through
// (task_beside.go). The shaper writes the brief and hands it to the worker when
// it lands (task_shape.go); the judge reads the sentence for width and its parts
// are weighed as a division the worker is handed (task_divide_sketch.go). Neither
// decides whether the work may start, and the worker's first request waits on
// neither.
//
// solo is the person saying the work is one worker's and asking for no reading
// of its width — `/task solo`, or a standing answer of `single` — and it is the
// only thing the judge needs to be told not to do.
//
// THE NAME is the one the namer gives every node nothing named
// ([TaskGraph.nameNode]), asked the moment the node exists; until it lands, the
// title is the mechanical cut of the person's opening words ([taskPersonTitle]).
//
// WHAT IS STILL THEIRS, WORD FOR WORD, is the summary under the row and the
// request the worker is told outranks anything a model wrote.
func (a *Agent) StartTask(ctx context.Context, brief string, solo bool) (uint64, string, string, error) {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		return 0, "", "", errors.New("a task needs a brief")
	}
	// WHEN THE BASH BELT IS ASKED FOR this door takes its second road: a run on
	// the run engine, answered AT ONCE with the id the store knows the work by,
	// so the conversation stays usable while the run goes (task_run_belt.go).
	// The run road falls back here for every store it cannot serve, so the road
	// below is the whole of this door whenever the belt is not asked for — which
	// is every build and every caller that never asked.
	if bashBeltAsked() {
		return a.startTaskRun(ctx, brief, solo, "")
	}
	return a.startTaskLegacy(ctx, brief, solo)
}

// startTaskLegacy is a person's task on this session's own tree: the node is
// admitted here, at once, and its readings run beside its first worker. It is
// the door this one has always been, kept whole for every build and every
// caller that did not ask for the bash belt's run road.
func (a *Agent) startTaskLegacy(ctx context.Context, brief string, solo bool) (uint64, string, string, error) {
	graph := a.graph()
	id := graph.reserve()
	// THE MODEL IS SETTLED HERE, AT ADMISSION, and frozen with the rest of the
	// spec — which is what [taskSpec.model] has always said of itself and what
	// this one path did not do. A person's task named no model, so the field was
	// left empty and the id was picked much later, when the worker was actually
	// spun up (task_run.go's [Agent.newTaskAgent] falling to a.model). Two things
	// were wrong with that. A `/model` switch between starting the task and the
	// worker reaching the front of the queue moved the work onto a model nobody
	// chose it for; and an empty field is an empty [TaskNotice.Model], so the
	// node's own room had nothing to say about what was running it.
	//
	// FROZEN AGAINST DRIFT IS NOT FROZEN AGAINST THE PERSON. What the freeze
	// stops is the IMPLICIT move — the conversation's dial reaching across into
	// work that was handed over before it turned. A person standing in this
	// node's room and picking a model for THIS node moves it, from its next turn
	// on, and moves nothing else ([Agent.RetargetTask], task_room.go). That door
	// is the only writer of this field after this line, and a node that has
	// settled is refused at it.
	//
	// The word is empty because a person's task names no model, and
	// [Agent.resolveTaskModel] answers that with the configured task model or the
	// conversation's own — the same ladder a proposal's blank `model` argument
	// takes, so both doors freeze the same id at the same moment.
	//
	// WHAT THIS DOES NOT CLAIM is anything below the first node. A task may spawn
	// work of its own and that work resolves its own model when it is admitted;
	// this is the id THIS node runs on, which is the only one anybody can be told
	// up front.
	//
	// THE BRIEF IS THEIR SENTENCE, STANDING IN, and [taskSpec.unshaped] says so:
	// [composeBrief] prints the request once where the two halves are identical,
	// and the shaper's brief replaces the stand-in when it lands (task_shape.go).
	spec := taskSpec{
		title: taskPersonTitle(brief), summary: firstLine(brief), request: brief,
		origin: a.taskOriginRef(), brief: brief, acceptance: taskPersonAcceptance,
		model:    a.resolveTaskModel("").model,
		unshaped: true, unsized: !solo,
		// AND WHAT WAS SAID AROUND IT, from the one compiler every door uses
		// (admission.go). A typed task is the door where the person has most
		// often already settled something in the conversation above it — the
		// folder, the format, the thing not to touch — and their command names
		// none of it.
		admission: a.admissionContext(),
	}
	// AND WHERE THE WORK STANDS (taskstands.go). A typed task gets the same
	// ladder a proposal gets, because a person who opened codeaf in their home
	// directory and typed `/task fix the crash` is in exactly the position issue
	// #76 was written about — and the one thing this door cannot do is ask, so it
	// takes the rung below rather than stopping. The ladder reads the paths in
	// the person's own sentence, which is the brief it is handed here.
	stand := a.taskGroundOrStandingIn(spec)
	// A FOLDER A PROGRAM'S RUN HOLDS IS REFUSED AT THE DOOR (programhold.go):
	// this door has nobody to ask, so the rung below may still land on it.
	if refusal := standHeldRefusal(stand, a.config.Workspace); refusal != "" {
		return 0, "", "", errors.New(refusal)
	}
	spec.ground, spec.mode = stand.dir, stand.mode
	graph.admit(id, spec)
	return id, spec.title, stand.redirect, nil
}

// THE PLANNER DOOR A PERSON'S COMMAND USED TO OPEN IS GONE FROM THIS FILE.
// `StartPlannerRun` stood here: it shaped a typed brief, named it, folded the
// done-condition into the goal and handed the lot to [Agent.RunOrchestrate]. It
// went with `/task adaptive`, which was its only caller, because ONE ROAD — a
// planner has to guess the parts from a request it can only read, while one
// worker that starts, opens the material and then hands out what it can actually
// see is the shape the measured runs favour (internal/splitgate, task_divide.go).
//
// NOTHING WAS LOST WITH IT. The shaping and naming it made are the two calls a
// person's task still gets on the road that survived — the shaper beside its
// first worker, the namer at admission — and the planner ENGINE is
// untouched and still shipped ([Agent.RunOrchestrate], orchestrate.go), reached
// today by cmd/harness-design's own driver — no conversation reaches it at all
// since the anchored cue went the same way this command's word did (loop.go).
// What went here is one command's approach road, and a door with no caller is a
// door the next reader assumes somebody walks through.

// taskPersonTitle is what a person's task is called until the namer answers:
// the first eight words of their own opening line.
func taskPersonTitle(brief string) string {
	words := strings.Fields(firstLine(brief))
	if len(words) > 8 {
		words = words[:8]
	}
	return clip(strings.Join(words, " "), titleLimit)
}

// judgeDecomposable asks one bounded auxiliary question. Every failure is a no:
// a no leaves the one worker that is already running exactly as it was.
func (a *Agent) judgeDecomposable(ctx context.Context, brief string) (bool, []string, string) {
	ctx, cancel := context.WithTimeout(ctx, taskJudgeTimeout)
	defer cancel()

	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	messages := []ai.Message{textMessage("system", taskJudgePrompt), textMessage("user", strings.TrimSpace(brief))}
	for attempt := 0; attempt < 2; attempt++ {
		response, judge, callErr := a.callRole(ctx, roles.RolePlanner, model, messages)
		if callErr != nil || response == nil {
			return false, nil, ""
		}
		a.addAuxiliaryUsageAs(response, judge, 1, string(roles.RolePlanner))
		if verdict, ok := parseTaskJudge(response.Text()); ok {
			return verdict.Parallel, verdict.Parts, verdict.Why
		}
		messages = append(messages, textMessage("assistant", response.Text()),
			textMessage("user", "Repair the answer. Return only the exact JSON object required by the schema."))
	}
	return false, nil, ""
}

func parseTaskJudge(text string) (taskJudgeVerdict, bool) {
	raw, err := subharness.Salvage(text)
	if err != nil {
		return taskJudgeVerdict{}, false
	}
	var verdict taskJudgeVerdict
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return taskJudgeVerdict{}, false
	}
	if !verdict.Parallel {
		return taskJudgeVerdict{}, true
	}
	if len(verdict.Parts) > 6 {
		verdict.Parts = verdict.Parts[:6]
	}
	for i := range verdict.Parts {
		words := strings.Fields(verdict.Parts[i])
		if len(words) > 6 {
			words = words[:6]
		}
		verdict.Parts[i] = strings.Join(words, " ")
	}
	why := strings.Fields(verdict.Why)
	if len(why) > 12 {
		why = why[:12]
	}
	verdict.Why = strings.Join(why, " ")
	return verdict, true
}
