package tui3

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func providerMenuIDs(choices []entryChoice) []string {
	var ids []string
	for _, c := range choices {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestDefaultProviderMenuUsesDefaultRefreshAndKeySetting(t *testing.T) {
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", config.ResolveSources(dir, "test-key", ""), pickerCatalog)
	a.openSettings()
	toConnections(t, a)
	calls := 0
	a.refreshModels = func(context.Context) ([]Model, time.Time, error) {
		calls++
		return pickerCatalog, time.Now(), nil
	}
	if got := providerMenuIDs(a.modelServiceMenuChoices(modelsource.DefaultID)); !reflect.DeepEqual(got, []string{"refresh", "key"}) {
		t.Fatalf("default menu = %v", got)
	}
	cmd := a.modelServiceMenuChoice(modelsource.DefaultID, "refresh")
	if cmd == nil || calls != 0 {
		t.Fatal("refresh must defer the catalog request off the UI loop")
	}
	if again := a.modelServiceMenuChoice(modelsource.DefaultID, "refresh"); again != nil {
		t.Fatal("duplicate refresh while pending")
	}
	msg := cmd().(modelsFetchedMsg)
	if calls != 1 || msg.err != nil {
		t.Fatalf("default catalog refresh calls=%d error=%v", calls, msg.err)
	}
	a.modelsFetched(msg)
	a.modelServiceMenuChoice(modelsource.DefaultID, "key")
	if a.sheet.edit == nil || a.sheet.edit.key != config.KeyAPIKey || !a.sheet.edit.secret {
		t.Fatal("default key must open existing secret API-key settings editor")
	}
	if a.modelDraft != nil {
		t.Fatal("default key must not create a model_sources draft")
	}
}

func TestProviderMenuOmitsUnsupportedOperations(t *testing.T) {
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", config.ResolveSources(dir, "test-key", ""), pickerCatalog)
	if got := providerMenuIDs(a.modelServiceMenuChoices(modelsource.DefaultID)); !reflect.DeepEqual(got, []string{"key"}) {
		t.Fatalf("without refresh capability: %v", got)
	}
	for _, action := range []string{"rename", "disconnect", "refresh"} {
		if cmd := a.modelServiceMenuChoice(modelsource.DefaultID, action); cmd != nil || a.modelDraft != nil {
			t.Fatalf("unsupported default action %s changed flow", action)
		}
	}
	for _, source := range modelsource.Vendored() {
		if source.ID == "deepseek" || source.ID == "codex" {
			a.modelCatalog = append(a.modelCatalog, source)
		}
	}
	for _, c := range a.modelServiceMenuChoices("deepseek") {
		if c.ID == "rename" {
			t.Fatal("vendored provider cannot offer unsupported rename")
		}
	}
	if got := providerMenuIDs(a.modelServiceMenuChoices("codex")); !reflect.DeepEqual(got, []string{"disconnect"}) {
		t.Fatalf("browser provider offered key or persisted-row refresh: %v", got)
	}
	menu := newModelChoiceEntry(modelConnectionID("deepseek"), "DeepSeek", "provider action", a.modelServiceMenuChoices("deepseek"))
	region := newModelChoiceEntry(modelConnectionID("deepseek"), "DeepSeek", "region", []entryChoice{{ID: "key", Name: "region named key"}})
	if !isServiceMenuChoices(menu) || isServiceMenuChoices(region) {
		t.Fatal("action menu must be distinct from region answers")
	}
}

func TestProviderMenuRefreshDoesNotReconnectOrSwitchModels(t *testing.T) {
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", config.ResolveSources(dir, "test-key", ""), pickerCatalog)
	for _, source := range modelsource.Vendored() {
		if source.ID == "deepseek" {
			a.modelCatalog = append(a.modelCatalog, source)
		}
	}
	calls := 0
	a.refreshAllModels = func(context.Context) { calls++ }
	before := a.model
	cmd := a.modelServiceMenuChoice("deepseek", "refresh")
	if cmd == nil || calls != 0 {
		t.Fatal("provider refresh must be an asynchronous listing request")
	}
	msg, ok := cmd().(modelsFetchedMsg)
	if !ok || !msg.all || calls != 1 {
		t.Fatalf("refresh did not use listing-only door: %#v calls %d", msg, calls)
	}
	a.modelsFetched(msg)
	if a.model != before || a.modelDraft != nil {
		t.Fatal("refresh reconnected or switched the conversation model")
	}
}
