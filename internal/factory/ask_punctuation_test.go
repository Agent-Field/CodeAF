package factory_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAddStageSentencePreservesAskPunctuation(t *testing.T) {
	for _, ask := range []string{
		"Review the diff. Fix what you find.",
		"Fix what you find?",
		"Fix what you find!",
		"Report these findings:",
		"Fix what you find;",
		"Fix what you find,",
		"...Fix what you find.",
	} {
		t.Run(ask, func(t *testing.T) {
			stages, err := factory.AddStageSentence(factory.DefaultRecipe().Stages, "after review, audit: "+ask+" \t")
			if err != nil {
				t.Fatal(err)
			}
			stage := stages[factory.StageIndex(stages, "audit")]
			if stage.Ask != ask {
				t.Errorf("stored ask = %q, want %q", stage.Ask, ask)
			}
		})
	}
}
