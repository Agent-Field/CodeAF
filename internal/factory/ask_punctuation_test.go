package factory_test

import (
	"testing"
	"time"

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

func TestLocalNewPreservesAskPunctuation(t *testing.T) {
	_, st := openLocal(t)
	seam := factory.LocalSeam(st, time.Now())
	const ask = "Review the diff. Fix what you find."
	id, err := seam.New("acme/api", ask)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := seam.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snap.Items {
		if item.ID != id {
			continue
		}
		if item.Ask == nil || item.Ask.Text != ask {
			t.Errorf("stored job ask = %+v, want %q", item.Ask, ask)
		}
		if item.Title != "Review the diff. Fix what you find" {
			t.Errorf("short title = %q", item.Title)
		}
		return
	}
	t.Fatal("new job missing from Load")
}
