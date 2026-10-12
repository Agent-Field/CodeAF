package serve

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// readPipeline prefers the host's pipeline read (personal file included),
// then the checkout's file. An empty directory keeps the seam's recipe, which
// is how a floor with no checkout still answers source defaults.
func (h *Handler) readPipeline(w http.ResponseWriter, r *http.Request, full, dir string, recipe factory.Recipe, problems []factory.Problem) (api.Pipeline, []string, bool) {
	notes := problemNotes(problems)
	if h.config.Pipeline == nil && dir == "" {
		p := api.FromRecipe(recipe)
		// RecipeAt guarantees defaults without a checkout, but does not say
		// whether a known checkout has a file. Leave that unknown source absent.
		if p.Source == "" && dir == "" {
			p.Source = "defaults"
		}
		return p, notes, true
	}
	var file pipeline.File
	var err error
	if h.config.Pipeline != nil {
		file, err = h.config.Pipeline(r.Context(), full, dir)
	} else {
		file, err = pipeline.Load(dir)
	}
	// A file that names its own problems is still the payload. An error with
	// nothing to show is a failure.
	if err != nil && len(file.Problems) == 0 && len(problems) == 0 {
		h.failed(w, err)
		return api.Pipeline{}, nil, false
	}
	if len(file.Problems) > 0 {
		notes = make([]string, len(file.Problems))
		for i, problem := range file.Problems {
			notes[i] = problem.Error()
		}
	}
	return wirePipeline(file), notes, true
}

func problemNotes(problems []factory.Problem) []string {
	notes := make([]string, len(problems))
	for i, problem := range problems {
		notes[i] = problem.Error()
	}
	return notes
}

// wirePipeline is the file the pipeline route returns. Until stays the
// runner's word. UntilSentence is the file's own until line.
func wirePipeline(f pipeline.File) api.Pipeline {
	p := api.FromRecipe(f.Recipe())
	p.DefaultBudget = f.DefaultBudget
	p.Text = f.Text
	p.PersonalText = f.PersonalText
	p.Personal = f.Personal
	p.Origin = pipelineOrigin(f.From)
	p.Start = &api.StartBlock{
		Labels:   copyStrings(f.Start.Labels),
		MaxSize:  f.Start.MaxSize,
		MaxCost:  f.Start.MaxCost,
		Never:    copyStrings(f.Start.Never),
		AskFirst: copyStrings(f.Start.AskFirst),
	}
	p.Size = &api.SizeBlock{PRLines: f.Size.PRLines, Issue: f.Size.Issue}
	noteSentences(p.Steps, f)
	for kind, steps := range p.ByKind {
		noteSentences(steps, f)
		p.ByKind[kind] = steps
	}
	return p
}

func noteSentences(steps []api.StepDef, f pipeline.File) {
	for i := range steps {
		step, ok := f.Steps[steps[i].Name]
		if !ok || strings.TrimSpace(step.Until) == "" {
			continue
		}
		steps[i].UntilSentence = step.Until
	}
}

func pipelineOrigin(from string) string {
	switch from {
	case factory.FromMainBranch:
		return "main"
	case factory.FromWorkingTree:
		return "working tree"
	case factory.FromDefault:
		return "defaults"
	default:
		return ""
	}
}

func copyStrings(s []string) []string {
	if len(s) == 0 {
		return []string{}
	}
	return append([]string(nil), s...)
}
