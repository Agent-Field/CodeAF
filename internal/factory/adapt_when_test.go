package factory_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestAddedStepRefusesUnknownCondition(t *testing.T) {
	for _, kind := range []factory.StageKind{factory.StageChat, factory.StageCheck, factory.StageGate} {
		t.Run(string(kind), func(t *testing.T) {
			_, _, err := factory.Edit(defaultJob(), factory.RunEdit{By: factory.ByYou, Add: []factory.Added{{Stage: factory.Stage{Name: "inspect", Kind: kind, Ask: "inspect it", When: "banana"}}}}, factory.DefaultRecipe(), factory.EditBeforeRun)
			if err == nil || !strings.Contains(err.Error(), "when is one of always, thin, large, touches auth, has ui") {
				t.Fatalf("unknown condition must be refused clearly: %v", err)
			}
		})
	}
}
