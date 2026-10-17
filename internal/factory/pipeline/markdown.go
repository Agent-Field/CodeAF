package pipeline

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// FromMarkdown reads a .codeaf/factory.md the way the floor reads it, and
// answers that recipe as a pipeline file.
//
// A STEP IS A PROMPT with the runtime words its stage already had: kind, when,
// effort and fanout travel when they are in the runner's vocabulary. A stage
// that is off is not in the pipeline. A word outside the YAML's vocabulary has
// nowhere to go, and each one is a problem so the loss is not quiet. Habits are rules:
// both are sentences the job is given, and a rule this file does not recognise
// is given to the model as text. Keys the markdown cannot say — labels, size,
// cost — stay the defaults.
func FromMarkdown(text string) File {
	recipe, parsed := factory.Parse(text)
	f := Defaults()
	f.Text = text
	f.Source = SourceMarkdown
	f.markdownAdapt = copyAdapt(recipe.Adapt)
	f.Pipeline = map[factory.Kind][]string{}
	f.Fixed = []string{}
	f.set.fixed = true

	// ONE NAME IS ONE STEP IN factory.yaml, while each markdown section wrote
	// its own (and factory.Parse fills a section the markdown leaves out with
	// the shipped stages). Where two sections disagree about what runs, when,
	// how hard or how wide, keeping either would quietly move it onto the
	// other, so the later section's step is written under its own name and
	// the rename is a problem. With no name free it holds for a person. A
	// different prompt, stop or rounds keeps the first, as it always has.
	defined := map[string]bool{}
	add := func(kind factory.Kind, stages []factory.Stage) {
		var names []string
		for _, stage := range stages {
			name, step, keep, probs, costs := convertStage(stage)
			f.Problems = append(append(f.Problems, probs...), costs...)
			if !keep {
				continue
			}
			if prev := f.Steps[name]; defined[name] && !factory.ApproveWord(name) && runsOtherwise(prev, step) {
				if alt := spareName(f, defined, name, kind); alt != "" {
					f.Problems = append(f.Problems, Problem{
						Where: name,
						Why:   "runs differently for " + string(kind) + ", so it is " + alt + " there",
					})
					name = alt
				} else if prev.hold == "" {
					prev.hold = "runs differently for " + string(kind) + " and no other name is free; the run holds here for a person until the sections agree"
					f.Problems = append(f.Problems, Problem{Where: name, Why: prev.hold})
					f.Steps[name] = prev
				}
			}
			if prev := f.Steps[name]; defined[name] {
				if step.wrotePrompt && strings.TrimSpace(prev.Prompt) != strings.TrimSpace(step.Prompt) {
					f.Problems = append(f.Problems, Problem{
						Where: name,
						Why:   "has another prompt for " + string(kind) + "; the first is kept",
					})
				}
				if prev.Until != step.Until || prev.Max != step.Max {
					f.Problems = append(f.Problems, Problem{
						Where: name,
						Why:   "has another until or max for " + string(kind) + "; the first is kept",
					})
				}
			} else {
				defined[name] = true
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
	// Sections are read in a fixed order so "the first is kept" is the same
	// first on every read.
	kinds := make([]string, 0, len(recipe.ByKind))
	for kind := range recipe.ByKind {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		add(factory.Kind(kind), recipe.ByKind[factory.Kind(kind)])
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
		if mode == factory.AdaptFixed {
			f.Problems = append(f.Problems, Problem{Where: string(kind), Why: "a fixed markdown section stays fixed while read; YAML fixes individual steps instead"})
		}
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

// convertStage is one stage as a step. probs change what runs or whether it
// runs; costs are a fanout or effort outside the YAML's words, read as the
// default, which only changes how much one run spends.
func convertStage(stage factory.Stage) (name string, step Step, keep bool, probs, costs []Problem) {
	name = stepToken(stage.Name)
	if !stage.On {
		return name, Step{}, false, []Problem{{Where: name, Why: "off in the markdown, so it is not in the pipeline"}}, nil
	}
	if !validStepName(name) {
		return name, Step{}, false, []Problem{{Where: name, Why: "not a step name"}}, nil
	}
	if factory.ApproveWord(name) {
		if strings.TrimSpace(stage.Ask) != "" {
			probs = append(probs, Problem{Where: name, Why: "approve is a person and has no prompt"})
		}
		if stage.When != "" {
			probs = append(probs, Problem{Where: name, Why: "approve is a person, with no condition"})
		}
		return name, Step{Until: "you"}, true, probs, nil
	}
	prompt := strings.TrimSpace(stage.Ask)
	var fields Step
	if kind, ok := kindWord(string(stage.Kind)); ok {
		fields.Kind = kind
	} else {
		// An unknown kind is never a chat: the step holds for a person.
		fields.hold = "a step has no kind called " + string(stage.Kind) + "; the run holds here for a person"
		probs = append(probs, Problem{Where: name, Why: fields.hold})
	}
	// A chat carries what the YAML cannot say as words in its prompt. A check's
	// prompt is a shell command and a post's is a verb: words added to either
	// would change what runs, so the step holds instead.
	if when, ok := stepWord(stage.When, factory.WhenWords); ok {
		fields.When = when
	} else if fields.Kind == "" {
		prompt = strings.TrimSpace(prompt + " (when " + stage.When + ")")
	} else if fields.hold == "" {
		fields.hold = "a " + fields.Kind + " cannot carry the condition " + stage.When + " in factory.yaml; the run holds here for a person"
		probs = append(probs, Problem{Where: name, Why: fields.hold})
	}
	if len(stage.Proof) > 0 {
		if fields.Kind == "" {
			prompt = strings.TrimSpace(prompt + " Show: " + strings.Join(stage.Proof, ", ") + ".")
		} else if fields.hold == "" {
			fields.hold = "a " + fields.Kind + " cannot carry show in factory.yaml; the run holds here for a person"
			probs = append(probs, Problem{Where: name, Why: fields.hold})
		}
	}
	if fanout, ok := stepWord(stage.Fanout, StepFanouts); ok {
		fields.Fanout = fanout
	} else {
		costs = append(costs, Problem{Where: name, Why: "fanout " + stage.Fanout + " is not in factory.yaml; the step is one conversation"})
	}
	if effort, ok := stepWord(stage.Effort, factory.EffortWords); ok {
		fields.Effort = effort
	} else {
		costs = append(costs, Problem{Where: name, Why: "effort " + stage.Effort + " is not in factory.yaml"})
	}
	step = Step{
		Prompt: prompt, Kind: fields.Kind, When: fields.When, Effort: fields.Effort,
		Fanout: fields.Fanout, Until: strings.TrimSpace(stage.Until), hold: fields.hold,
	}
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
	// Only a chat may be left without a prompt, and only where a shipped one
	// fills it. A gate needs none, and a check or post without one is kept so
	// it holds for a person instead of vanishing from the pipeline.
	if prompt == "" && step.Kind == "" && step.hold == "" {
		if _, ok := builtinTable[name]; !ok {
			probs = append(probs, Problem{Where: name, Why: "a step needs a prompt"})
			return name, Step{}, false, probs, costs
		}
	}
	return name, step, true, probs, costs
}

// runsOtherwise says whether two definitions of one step name would run
// differently: another kind, condition, effort or fanout, or one of them holds.
func runsOtherwise(a, b Step) bool {
	return a.Kind != b.Kind || a.When != b.When || a.Effort != b.Effort || a.Fanout != b.Fanout || a.hold != b.hold
}

// spareName is a step name for kind's own definition of name: name-kind, then
// name-kind-2 to -9, the first that is a step name, a stage word the runner can
// run, and not yet a step. "" when none is.
func spareName(f File, defined map[string]bool, name string, kind factory.Kind) string {
	base := name + "-" + string(kind)
	for k := 1; k <= 9; k++ {
		alt := base
		if k > 1 {
			alt = fmt.Sprintf("%s-%d", base, k)
		}
		if !validStepName(alt) || defined[alt] {
			continue
		}
		if _, taken := f.Steps[alt]; taken {
			continue
		}
		if _, err := factory.StageWord(strings.ReplaceAll(alt, "-", "")); err != nil {
			continue
		}
		return alt
	}
	return ""
}
