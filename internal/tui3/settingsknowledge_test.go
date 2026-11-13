package tui3

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestKnowledgeModelChoicePersistsWithoutChangingTheConversation(t *testing.T) {
	a, _ := sheetApp(t)
	a.models = func() []Model { return mixedModels }
	a.openSettings()
	toProviders(t, a)
	cursorTo(t, a, config.KeyKnowledgeModel)
	before := a.model
	drive(t, a, key("enter"))
	if a.sheet.sel == nil {
		t.Fatal("knowledge model did not open the standard model picker")
	}
	for _, r := range "son 4.5" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"))
	if got := config.KnowledgeModelAt(a.profileDir); got != "anthropic/claude-sonnet-4.5" {
		t.Fatalf("knowledge model choice was not saved: %q", got)
	}
	if a.model != before {
		t.Fatal("knowledge model choice changed the conversation model")
	}
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: a.profileDir}).Row(config.KeyKnowledgeModel)
	if !ok || row.Value() != "anthropic/claude-sonnet-4.5" {
		t.Fatal("reopened Settings lost knowledge model choice")
	}
	if err := row.Apply(""); err != nil {
		t.Fatal(err)
	}
	if got := config.KnowledgeModelAt(a.profileDir); got != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("clearing the choice did not restore exact default: %q", got)
	}
}

func TestKnowledgeModelPickerOffersOnlyTheOpenRouterCatalog(t *testing.T) {
	for _, connectedCloud := range []bool{true, false} {
		t.Run(fmt.Sprint(connectedCloud), func(t *testing.T) {
			a, _ := sheetApp(t)
			a.models = func() []Model { return []Model{{ID: "deepseek/deepseek-v4.1-flash"}} }
			local := testModelSource(t, "ollama")
			cloudKey := ""
			if connectedCloud {
				cloudKey = "fixture-key"
			}
			a.sources = modelsource.NewSet(testDefaultService(cloudKey), modelsource.Connected{Source: local, Address: local.Address})
			a.modelsForService = func(modelsource.Connected) []Model { return []Model{{ID: "qwen3:0.6b"}} }
			a.openSettings()
			toProviders(t, a)
			cursorTo(t, a, config.KeyKnowledgeModel)
			drive(t, a, key("enter"))
			got := pickedIDs(a.sheet.sel)
			want := []string{}
			if connectedCloud {
				want = []string{"deepseek/deepseek-v4.1-flash"}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("knowledge picker offered non-OpenRouter choices: %v, want %v", got, want)
			}
		})
	}
}
