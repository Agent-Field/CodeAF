package config

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ModelUsable checks resolved local provider readiness for an incoming model.
// providerID, when known, prevents a missing custom alias from falling through
// to the default router. No catalog or network request is needed.
func ModelUsable(sources modelsource.Set, model, providerID string) bool {
	model, _ = roles.SplitEffort(model)
	written := sources.Written()
	for _, source := range modelsource.Vendored() {
		// Cloud vendor names can also be OpenRouter author prefixes.
		// Local runners and the sign-in transport cannot use that fallback.
		if source.KeyOptional || source.ID == "codex" {
			written = append(written, source.Written)
		}
	}
	qualifier, _ := modelsource.Split(model, written)
	service, _ := sources.For(model)
	if providerID != "" && service.Source.ID != providerID && !(modelsource.IsCustomID(providerID) && modelsource.IsCustomID(service.Source.ID)) {
		return false
	}
	if qualifier != "" && !strings.EqualFold(qualifier, service.Source.Written) {
		return false
	}
	return service.HasCredentials()
}
