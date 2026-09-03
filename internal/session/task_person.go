package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	taskJudgeTimeout = 3 * time.Second
	taskJudgeTokens  = 120
)

// taskJudgePrompt sizes one piece of work. WHAT IT DECIDES IS NARROWER THAN IT
// LOOKS, and the last paragraph is the whole of the change this wave made to
// it: the answer no longer settles how the work runs, only whether a planner is
// offered. A single worker that opens the material and finds six separate jobs
// in it can now split itself and stay to fold the parts back together
// (task_divide.go), so a no here is no longer a decision that the work will
// only ever be one pair of hands. That makes the judge free to be strict — the
// cost of a wrong no fell to nearly nothing — and being strict is what it was
// always asked to be ("when unsure, false").
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
// THE BRIEF IS SHAPED BEFORE IT IS ADMITTED (task_shape.go), and the shaped text
// is what the node, the room, the roster and the journal all carry — there is no
// second, secret version of the work anywhere.
//
// THE TITLE IS SHAPED WITH IT, and the reason is what the rail actually draws:
// THREE WORDS ([taskTitleOf], tui3). The first three words of a typed sentence
// are whatever that sentence happened to open with — "can you go", "please have
// a", "look into why" — so a rail of them names every task after the way somebody
// cleared their throat. The shaper has already read the work closely enough to
// write a worker's brief about it, so it is asked for the name in the same
// answer; where it did not run, [taskPersonTitle] cuts the old mechanical one and
// nothing is lost but a good name.
//
// WHAT IS STILL THEIRS, WORD FOR WORD, is the summary under the row and the
// request the worker is told outranks anything a model wrote. A shaper that could
// not run leaves the brief exactly as they typed it.
func (a *Agent) StartTask(ctx context.Context, brief string) (uint64, string, string, error) {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		return 0, "", "", errors.New("a task needs a brief")
	}
	shaped := a.shapeBrief(ctx, brief)
	title := taskName(shaped.Title, brief)
	work, acceptance := shaped.Brief, shaped.Acceptance
	// THE ONE HONEST LINE ABOUT A CUT SHAPER. Path (a) of issue #133: the
	// person's words are the brief either way — display-only — but where a
	// shaper was genuinely invoked and came back cut, the surface that draws
	// the started row carries one dim line saying so. Every silent
	// pass-through (no shaper configured, an empty request, a whole answer
	// that failed to parse) leaves this empty.
	note := ""
	if shaped.FellBack {
		note = TaskShapeFallbackNote
	}
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
	// THE REQUEST STAYS THE PERSON'S SENTENCE whatever the shaper wrote, and
	// [composeBrief] prints it above the work under the heading that says whose
	// words they are, with the rule that theirs win where the two read
	// differently (task_brief.go). Where nothing shaped it the two halves are
	// identical and that same function prints them once.
	// THE NAME IS THE SHAPER'S WHERE IT WROTE ONE, and where it did not the title
	// above is the mechanical cut of the person's own opening words — which is
	// what [taskSpec.named] says, and what sends the cheap namer after it once
	// the node is running (taskname.go).
	spec := taskSpec{
		title: title, named: strings.TrimSpace(shaped.Title) != "",
		summary: firstLine(brief), request: brief, origin: a.taskOriginRef(), brief: work,
		acceptance: acceptance, where: shaped.Where, model: a.resolveTaskModel("").model,
	}
	// A SHAPER THAT INVENTED `in place` DOES NOT SKIP THE TREE. The field is
	// honored only when the person's own sentence named a folder or said
	// those words (taskstands.go's [placementThePersonAskedFor]).
	a.dropGuessedWhere(&spec)
	// AND WHERE THE WORK STANDS (taskstands.go). A typed task gets the same
	// ladder a proposal gets, because a person who opened aforge in their home
	// directory and typed `/task fix the crash` is in exactly the position issue
	// #76 was written about — and the one thing this door cannot do is ask, so it
	// takes the rung below rather than stopping.
	stand := a.taskGroundOrStandingIn(spec)
	spec.ground, spec.mode = stand.dir, stand.mode
	graph.admit(id, spec)
	return id, title, note, nil
}

// THE PLANNER DOOR A PERSON'S COMMAND USED TO OPEN IS GONE FROM THIS FILE.
// `StartPlannerRun` stood here: it shaped a typed brief, named it, folded the
// done-condition into the goal and handed the lot to [Agent.RunOrchestrate]. It
// went with `/task adaptive`, which was its only caller, because ONE ROAD — a
// planner has to guess the parts from a request it can only read, while one
// worker that starts, opens the material and then hands out what it can actually
// see is the shape the measured runs favour (internal/splitgate, task_divide.go).
//
// NOTHING WAS LOST WITH IT. The shaping and naming above are the same two calls
// [Agent.StartTask] makes, on the road that survived; the planner ENGINE is
// untouched and still shipped ([Agent.RunOrchestrate], orchestrate.go), reached
// today by cmd/harness-design's own driver — no conversation reaches it at all
// since the anchored cue went the same way this command's word did (loop.go).
// What went here is one command's approach road, and a door with no caller is a
// door the next reader assumes somebody walks through.

// taskName settles what a person's task is called: the shaper's name where it
// wrote one, and the mechanical cut of their own opening words where it did not.
//
// It is a function of its own rather than two lines inside [Agent.StartTask]
// because the CHOICE is the thing worth naming and testing on its own: which of
// two names a task ends up wearing, and the fact that the shaper being offline
// costs a good name and nothing else. It had a second caller until the planner
// door above went, and it is written to be called again.
func taskName(shaped, brief string) string {
	if shaped = strings.TrimSpace(shaped); shaped != "" {
		return shaped
	}
	return taskPersonTitle(brief)
}

func taskPersonTitle(brief string) string {
	words := strings.Fields(firstLine(brief))
	if len(words) > 8 {
		words = words[:8]
	}
	return clip(strings.Join(words, " "), titleLimit)
}

// JudgeDecomposable asks one bounded auxiliary question. Every failure is a no:
// the caller can start a single task without teaching a person about this call.
func (a *Agent) JudgeDecomposable(ctx context.Context, brief string) (bool, []string, string) {
	return a.judgeDecomposable(ctx, brief)
}

func (a *Agent) judgeDecomposable(ctx context.Context, brief string) (bool, []string, string) {
	ctx, cancel := context.WithTimeout(ctx, taskJudgeTimeout)
	defer cancel()

	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	messages := []ai.Message{textMessage("system", taskJudgePrompt), textMessage("user", strings.TrimSpace(brief))}
	for attempt := 0; attempt < 2; attempt++ {
		response, judge, callErr := a.callRole(ctx, roles.RolePlanner, model, messages,
			ai.WithMaxTokens(taskJudgeTokens))
		if callErr != nil || response == nil {
			return false, nil, ""
		}
		a.addAuxiliaryUsage(response, judge, 1)
		if verdict, ok := parseTaskJudge(response.Text()); ok {
			// A YES IS BANKED AGAINST THE TEXT IT WAS ABOUT, AND IT IS WHAT THE
			// WHOLE CALL IS FOR NOW. A yes used to raise a card offering a
			// planner; today `/task <brief>` starts one worker either way and
			// this banked yes is what arms that worker to hand the work out
			// once it has opened the material and found the width is real
			// (task_divide.go's [Agent.armDivision]).
			if verdict.Parallel {
				a.rememberDivisible(brief)
			}
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
