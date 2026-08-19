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
func (a *Agent) StartTask(_ context.Context, brief string) (uint64, string, error) {
	brief = strings.TrimSpace(brief)
	if brief == "" {
		return 0, "", errors.New("a task needs a brief")
	}
	title := taskPersonTitle(brief)
	graph := a.graph()
	id := graph.reserve()
	// THE PERSON'S WORDS ARE BOTH HALVES HERE, and that is not a duplication: on
	// this path nobody paraphrased anything, so the request IS the work and
	// [composeBrief] prints it once, under the heading that says whose words they
	// are (task_brief.go).
	graph.admit(id, taskSpec{
		title: title, summary: firstLine(brief), request: brief, brief: brief,
		acceptance: "Complete the brief and report the result and checks run.",
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
	title := taskPersonTitle(brief)
	// THE PERSON TYPED THIS, so it is what the run's planner and every one of its
	// nodes will be shown as the request (task_brief.go). Without this line the
	// run would carry whatever was last said in the CHAT, which on this path is
	// some other conversation entirely — the brief came in through a command.
	a.rememberAsk(brief)
	goal := brief
	if plannerHint = strings.TrimSpace(plannerHint); plannerHint != "" {
		goal += "\n\nPossible parallel parts: " + plannerHint
	}
	id, err := a.RunOrchestrate(ctx, goal, "", 0)
	return id, title, err
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
		a.addAuxiliaryUsage(response)
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
