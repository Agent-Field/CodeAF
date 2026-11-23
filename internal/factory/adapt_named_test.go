package factory_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A MODEL'S STEP HAS A NAME OF ITS OWN. On factory-demo#168 the manager was
// asked mid-run for a step after Write that runs go vet. A step called
// Immediately appeared between Review and Proof: a sentence added with no
// `name:` is named by its first word and, with no place at its front, put
// just before proof. A manager or plan add whose only name is that guess is
// refused, saying so, with nothing changed. Under the same sentence a step
// that names itself, a guess that is a real step, and a person's own typing
// all still add.
func TestAModelsStepNamedOnlyByItsFirstWordIsRefused(t *testing.T) {
	sentence := "Immediately after write, run go vet ./... and fail on any finding"
	for _, by := range []string{factory.ByManager, factory.ByPlan} {
		it := adaptItem()
		it.Stream = nil
		it.Stages = append(it.Stages[:5:5], it.Stages[6:]...)
		before := stageNames(it)
		got, lines, err := factory.Edit(it, factory.RunEdit{By: by, Add: []factory.Added{{Stage: factory.Stage{Ask: sentence}}}}, factory.DefaultRecipe(), factory.EditBeforeRun)
		if err == nil || len(lines) != 0 {
			t.Fatalf("%s: a step named by its first word was added: %q %v", by, lines, stageNames(got))
		}
		if !strings.Contains(err.Error(), `"immediately", its first word, is not a step`) || !strings.Contains(err.Error(), `vet: run go vet`) {
			t.Fatalf("%s: the refusal does not say what to do: %v", by, err)
		}
		if strings.Join(stageNames(got), " ") != strings.Join(before, " ") {
			t.Fatalf("%s: a refused edit changed the steps: %v", by, stageNames(got))
		}
	}

	// The same work, named, is added where it says.
	it := adaptItem()
	it.Stream = nil
	it.Stages = append(it.Stages[:5:5], it.Stages[6:]...)
	got, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByManager, Add: []factory.Added{{Stage: factory.Stage{Ask: "after write, vet: run go vet ./... and fail on any finding"}}}}, factory.DefaultRecipe(), factory.EditBeforeRun)
	if err != nil || len(lines) != 1 || factory.StageIndex(got.Stages, "vet") != factory.StageIndex(got.Stages, "write")+1 {
		t.Fatalf("a named step = %q %v %v", lines, stageNames(got), err)
	}

	// A first word that is a step codeaf ships is a real name.
	it = adaptItem()
	it.Stream = nil
	it.Stages = append(it.Stages[:5:5], it.Stages[6:]...)
	if i := factory.StageIndex(it.Stages, "security"); i >= 0 {
		it.Stages = append(it.Stages[:i:i], it.Stages[i+1:]...)
	}
	if _, ok := factory.ShippedStage("security"); ok {
		if _, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByManager, Add: []factory.Added{{Stage: factory.Stage{Ask: "after review, security review of the diff"}}}}, factory.DefaultRecipe(), factory.EditBeforeRun); err != nil || len(lines) != 1 {
			t.Fatalf("a shipped step's name was refused: %q %v", lines, err)
		}
	}

	// A person's typed sentence keeps its own reading.
	it = adaptItem()
	it.Stream = nil
	it.Stages = append(it.Stages[:5:5], it.Stages[6:]...)
	if got, lines, err := factory.Edit(it, factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Ask: "after review, make it neater"}}}}, factory.DefaultRecipe(), factory.EditBeforeRun); err != nil || len(lines) != 1 || factory.StageIndex(got.Stages, "make") < 0 {
		t.Fatalf("the person's sentence = %q %v %v", lines, stageNames(got), err)
	}
}
