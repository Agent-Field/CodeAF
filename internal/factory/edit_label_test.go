package factory_test

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
	"testing"
)

func TestJobLabelEdits(t *testing.T) {
	for _, label := range []string{" Security review ", ""} {
		it := defaultJob()
		it.Stages = []factory.Stage{{Name: "write", Label: "Before", Kind: factory.StageChat, Ask: "write it", On: true}}
		raw, _ := json.Marshal(map[string]any{"By": factory.ByYou, "Label": map[string]string{"write": label}})
		var edit factory.RunEdit
		if err := json.Unmarshal(raw, &edit); err != nil {
			t.Fatal(err)
		}
		got, lines, err := factory.Edit(it, edit, factory.DefaultRecipe(), factory.EditBeforeRun)
		if err != nil || len(lines) == 0 || got.Stages[0].Label != strings.TrimSpace(label) || got.Stages[0].Name != "write" {
			t.Fatalf("label edit lost: %+v %v %v", got.Stages, lines, err)
		}
	}
}
