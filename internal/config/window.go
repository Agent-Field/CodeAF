package config

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// CachedContextWindow is how many tokens a model accepts according to the
// model catalog this profile already keeps on disk, or zero when that catalog
// cannot say: an unknown model, a service whose list was never fetched, a
// profile that has never listed anything.
//
// IT NEVER REACHES THE NETWORK AND READS NO KEY. It exists for a program codeaf
// starts (senior-dev), which runs with every provider key taken out of its
// environment and may sit behind a network that reaches nothing but the run's
// own model API. That program still deserves the window codeaf itself would
// use, and codeaf wrote it down the last time it listed the models: the
// default service's listing in `model-catalog.json`, every other connected
// service's in its own compartment beside it ([CatalogOptionsFor]). The model
// is spelled the way a person writes it, and its service is found the way
// every other door finds one ([modelsource.Set.For]).
func CachedContextWindow(model string) int {
	model = strings.TrimSpace(model)
	if model == "" {
		return 0
	}
	profileDir := ProfileDir()
	values, _ := readProfileConfig(profileDir)
	baseURL := firstNonEmpty(env.Get("CODEAF_BASE_URL"), DefaultBaseURL)
	noKey := func(PersistedSource, modelsource.Source) string { return "" }
	sources := sourceHomes(resolveSources("", baseURL, persistedSourcesFrom(values), noKey), profileDir)
	service, bare := sources.For(model)
	id := strings.TrimSpace(service.Source.ID)
	switch {
	case strings.EqualFold(id, modelsource.DefaultID):
		// The default service's listing is the profile's one shared catalog,
		// written under no source name (cmd/codeaf's sharedCatalog).
		return catalog.Recall(catalog.Options{BaseURL: baseURL, Dir: profileDir}).ContextLength(bare)
	case strings.EqualFold(id, "codex"):
		// A Codex row remembered before its window was kept answers the
		// window the fallback list names ([CodexRememberedModels]).
		for _, row := range CodexRememberedModels(service, profileDir) {
			if strings.EqualFold(strings.TrimSpace(row.ID), bare) {
				return row.ContextLength
			}
		}
		return 0
	}
	// The same compartment the connected service's listing is written to.
	// Recall reads the file and nothing else, so the key the options carry
	// (none here) and their client are never used.
	return catalog.Recall(CatalogOptionsFor(service, profileDir)).ContextLength(bare)
}
