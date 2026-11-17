package pipeline

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
)

// RecipeLoader is the same YAML-first read for the seam and the runner.
// Problems retain their location when crossing into the older recipe vocabulary.
func RecipeLoader(personal string) func(string) (factory.Recipe, []factory.Problem, error) {
	return func(dir string) (factory.Recipe, []factory.Problem, error) {
		if strings.TrimSpace(dir) == "" {
			return Defaults().Recipe(), nil, nil
		}
		f, err := LoadMerged(dir, personal)
		problems := make([]factory.Problem, len(f.Problems))
		for i, p := range f.Problems {
			problems[i] = factory.Problem{Line: p.Line, Text: p.Where, Why: p.Why}
		}
		return f.Recipe(), problems, err
	}
}
