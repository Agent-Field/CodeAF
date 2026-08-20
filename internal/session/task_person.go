package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	taskJudgeTimeout = 3 * time.Second
	taskJudgeTokens  = 120
	taskJudgeTemp    = 0
)

const taskJudgePrompt = `Decide whether this task is meaningfully parallelizable or nestable for speed or quality. Parallel means independent parts can proceed at the same time and a planner can combine them; a merely long sequence is not parallel.

Answer with exactly one JSON object and no markdown:
{"parallelizable":bool,"parts":["part in at most 6 words"],"why":"reason in at most 12 words"}

Use at most 6 parts. When unsure, set parallelizable to false.`

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
func (a *Agent) StartTask(ctx context.Context, brief string) (uint64, string, error) {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		return 0, "", errors.New("a task needs a brief")
	}
	shaped := a.shapeBrief(ctx, brief)
	title := taskName(shaped.Title, brief)
	work, acceptance := shaped.Brief, shaped.Acceptance
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
	graph.admit(id, taskSpec{
		title: title, summary: firstLine(brief), request: brief, brief: work,
		acceptance: acceptance, model: a.resolveTaskModel("").model,
	})
	return id, title, nil
}

// StartPlannerRun starts an adaptive run from a person's brief. The hint is a
// sketch from the sizing call, not a model override, and reaches the planner as
// supporting context beneath the person's unchanged brief.
func (a *Agent) StartPlannerRun(ctx context.Context, brief, plannerHint string) (string, string, error) {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		return "", "", errors.New("an adaptive task needs a brief")
	}
	// THE PERSON TYPED THIS, so it is what the run's planner and every one of its
	// nodes will be shown as the request (task_brief.go). Without this line the
	// run would carry whatever was last said in the CHAT, which on this path is
	// some other conversation entirely — the brief came in through a command. It
	// is recorded BEFORE the shaping call and from the raw sentence, because the
	// request is the one thing on this path no model is allowed to have written.
	a.rememberAsk(brief)
	// The adaptive shape is shaped too — "this is true for all tasks". A run's
	// nodes are workers with the same silence around them as a single task's, and
	// a planner cutting up one unshaped sentence cuts up the same ambiguity into
	// several pieces.
	shaped := a.shapeBrief(ctx, brief)
	// The adaptive run is named out of the same answer as the single task's, for
	// the same reason: a run's row on the rail is three words too.
	title := taskName(shaped.Title, brief)
	goal, acceptance := shaped.Brief, shaped.Acceptance
	// A RUN HAS NO ACCEPTANCE FIELD — it is a goal, a planner and a fleet
	// (orchestrate.go) — so a shaped done-condition would be thrown away unless
	// it rides in the goal. It goes under [briefDoneHeading], the same word every
	// node brief already spells it with, and only when shaping actually happened:
	// where it did not, the goal is the person's sentence and nothing else, which
	// is what this path did before.
	if acceptance != taskPersonAcceptance && strings.TrimSpace(acceptance) != "" {
		goal += "\n\n" + briefDoneHeading + "\n" + acceptance
	}
	if plannerHint = strings.TrimSpace(plannerHint); plannerHint != "" {
		goal += "\n\nPossible parallel parts: " + plannerHint
	}
	id, err := a.RunOrchestrate(ctx, goal, "", 0)
	return id, title, err
}

// taskName settles what a person's task is called: the shaper's name where it
// wrote one, and the mechanical cut of their own opening words where it did not.
//
// It is one function rather than the same two-line choice at both doors, because
// the fallback is the thing that has to be identical — a single task and an
// adaptive run started from the identical sentence must not end up on the rail
// under two different names when the shaper is offline.
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

// TaskPlannerModel is the resolved mastermind call as the chooser spells it.
func (a *Agent) TaskPlannerModel() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	call, err := roles.ResolveCall(roles.Source(a.config.RolesSource), roles.RolePlanner, a.model)
	if err != nil {
		return ""
	}
	return call.String()
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
	call, err := roles.ResolveCall(roles.Source(a.config.RolesSource), roles.RolePlanner, a.model)
	a.mu.Unlock()
	if err != nil || strings.TrimSpace(call.Model) == "" {
		return false, nil, ""
	}
	if effort, ok := provider.ParseEffort(call.Effort); ok && effort != provider.EffortNone {
		ctx = provider.WithReasoningEffort(ctx, effort)
	}
	messages := []ai.Message{textMessage("system", taskJudgePrompt), textMessage("user", strings.TrimSpace(brief))}
	for attempt := 0; attempt < 2; attempt++ {
		response, callErr := a.client.CompleteWithMessages(provider.WithoutStream(ctx), messages,
			ai.WithModel(call.Model), ai.WithTemperature(taskJudgeTemp), ai.WithMaxTokens(taskJudgeTokens))
		if callErr != nil || response == nil {
			return false, nil, ""
		}
		a.addAuxiliaryUsage(response, call.Model, 1)
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
