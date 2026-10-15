package tui3

import (
	"strings"

	modelcatalog "github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// The desktop app's backend (cmd/codeaf's app.go) draws the same facts this
// surface draws, and it reads them through these doors rather than restating
// the rules, so a tool row and a model menu say the same thing in both places.

// ToolTitle is the one line a tool row says for a call: the tool's name and
// what the call is about, exactly as [app.toolLineLayout] lays them out before
// any width is spent (`read src/auth/session.ts`, `bash pnpm test`).
func ToolTitle(tool, args, hint string) string {
	name, _ := toolWords(tool, hint)
	target := toolTarget(tool, args, hint)
	if target == "" {
		return name
	}
	if name == "" {
		return target
	}
	return name + " " + target
}

// PickerModels is the chat model picker's selectable rows for a profile's
// services: the default service's rows from defaults (the process catalog, as
// the terminal's Options.Models), every other connected service's from
// forService, and the small model caches on disk where the terminal reads them.
// Rows that are a group's notice rather than a model are left out. It never
// reaches the network.
func PickerModels(profileDir string, sources modelsource.Set, defaults func() []Model, forService func(modelsource.Connected) []Model) []Model {
	a := &app{profileDir: profileDir, sources: sources, models: defaults, modelsForService: forService}
	a.modelLists = newLearned(readModelCacheName)
	// The names the terminal learns when it opens, read here at once.
	a.modelLists.learn(modelCacheNameFor("", modelcatalog.DefaultBaseURL))
	for _, service := range sources.All() {
		a.modelLists.learn(modelCacheNameFor(service.Source.ID, service.Address))
	}
	var rows []Model
	for _, model := range a.modelList() {
		if model.Unavailable || model.Notice != "" || strings.TrimSpace(model.ID) == "" {
			continue
		}
		rows = append(rows, model)
	}
	return rows
}
