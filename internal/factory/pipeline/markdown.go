package pipeline

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// FromMarkdown reads a .codeaf/factory.md the way the floor reads it, and
// answers that recipe as a pipeline file.
//
// A STEP IS A PROMPT. A check or a post becomes the sentence it already was.
// A stage that is off is not in the pipeline. Fanout and thinking have nowhere
// to go, and each one is a problem so the loss is not quiet. Habits are rules:
// both are sentences the job is given, and a rule this file does not recognise
// is given to the model as text. Keys the markdown cannot say — labels, size,
// cost — stay the defaults.
func FromMarkdown(text string) File {
	recipe, parsed := factory.Parse(text)
	f := Defaults()
	f.Source = SourceMarkdown
	f.Pipeline = map[factory.Kind][]string{}
	f.Fixed = []string{}
	f.set.fixed = true

	add := func(kind factory.Kind, stages []factory.Stage) {
		var names []string
		for _, stage := range stages {
			name, step, keep, probs := convertStage(stage)
			f.Problems = append(f.Problems, probs...)
			if !keep {
				continue
			}
			if prev, ok := f.Steps[name]; ok && (prev.wrotePrompt || prev.wroteUntil || prev.wroteMax) {
				if step.wrotePrompt && strings.TrimSpace(prev.Prompt) != strings.TrimSpace(step.Prompt) {
					f.Problems = append(f.Problems, Problem{
						Where: name,
						Why:   "has another prompt for " + string(kind) + "; the first is kept",
					})
				}
			} else {
				f.Steps[name] = step
			}
			if stage.Fixed && !listed(f.Fixed, name) {
				f.Fixed = append(f.Fixed, name)
			}
			names = append(names, name)
		}
		if names == nil {
			names = []string{}
		}
		f.Pipeline[kind] = names
	}
	add(factory.KindIssue, recipe.Stages)
	for kind, stages := range recipe.ByKind {
		add(kind, stages)
	}
	f.Rules = dedupeFold(append(append([]string{}, recipe.Policy...), recipe.Habits...))
	for _, problem := range parsed {
		why := problem.Why
		if problem.Text != "" {
			why = problem.Why + " (" + problem.Text + ")"
		}
		f.Problems = append(f.Problems, Problem{Line: problem.Line, Where: factory.RecipeFile, Why: why})
	}
	for kind, mode := range recipe.Adapt {
		if mode == factory.AdaptAsk {
			f.Problems = append(f.Problems, Problem{
				Where: string(kind),
				Why:   "plan asking first is not a word in factory.yaml; mark the step fixed instead",
			})
		}
	}
	f.resolve()
	f.clean(true)
	return f
}

func convertStage(stage factory.Stage) (name string, step Step, keep bool, probs []Problem) {
	name = stepToken(stage.Name)
	if !stage.On {
		return name, Step{}, false, []Problem{{Where: name, Why: "off in the markdown, so it is not in the pipeline"}}
	}
	if !validStepName(name) {
		return name, Step{}, false, []Problem{{Where: name, Why: "not a step name"}}
	}
	if factory.ApproveWord(name) {
		if strings.TrimSpace(stage.Ask) != "" {
			probs = append(probs, Problem{Where: name, Why: "approve is a person and has no prompt"})
		}
		if stage.When != "" {
			probs = append(probs, Problem{Where: name, Why: "approve is a person, with no condition"})
		}
		return name, Step{Until: "you"}, true, probs
	}
	prompt := strings.TrimSpace(stage.Ask)
	if stage.When != "" {
		prompt = strings.TrimSpace(prompt + " (when " + stage.When + ")")
	}
	if len(stage.Proof) > 0 {
		prompt = strings.TrimSpace(prompt + " Show: " + strings.Join(stage.Proof, ", ") + ".")
	}
	switch stage.Kind {
	case "", factory.StageChat:
	case factory.StageCheck:
		probs = append(probs, Problem{Where: name, Why: "a check becomes a prompt; there is no step kind"})
	case factory.StagePost:
		probs = append(probs, Problem{Where: name, Why: "a post becomes a prompt; there is no step kind"})
	case factory.StageGate:
		probs = append(probs, Problem{Where: name, Why: "a person step is named approve"})
	default:
		probs = append(probs, Problem{Where: name, Why: "a step is a prompt"})
	}
	if stage.Fanout != "" {
		probs = append(probs, Problem{Where: name, Why: "fanout is not in factory.yaml; the step is one conversation"})
	}
	if stage.Effort != "" {
		probs = append(probs, Problem{Where: name, Why: "thinking is not in factory.yaml"})
	}
	step = Step{Prompt: prompt, Until: strings.TrimSpace(stage.Until)}
	if prompt != "" {
		step.wrotePrompt = true
	}
	if step.Until != "" {
		step.wroteUntil = true
	}
	if stage.Max > 0 {
		step.Max = stage.Max
		step.wroteMax = true
	}
	if prompt == "" {
		if _, ok := builtinTable[name]; !ok {
			probs = append(probs, Problem{Where: name, Why: "a step needs a prompt"})
			return name, Step{}, false, probs
		}
	}
	return name, step, true, probs
}
