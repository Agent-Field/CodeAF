package config

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// ProviderKeyEnvNames is every environment variable a provider credential can
// live in: the two [APIKeyAt] reads directly, one per vendored service
// (modelsource.Vendored's KeyEnv), and one per custom variable a person named
// for a connected service in this profile (PersistedSource.KeyEnv,
// sourceKeyFromRow's third rung) — a service added later, or a person's
// own MY_ZAI_KEY, is covered without anyone remembering this list. It is read
// fresh rather than cached for the same reason.
func ProviderKeyEnvNames() []string {
	return ProviderKeyEnvNamesAt(ProfileDir())
}

// ProviderKeyEnvNamesAt is the same list for an explicitly resolved profile.
// The host comparison resolves relative references in the host's workspace,
// because its daemon changes directory before opening any conversations.
func ProviderKeyEnvNamesAt(profileDir string) []string {
	names := []string{APIKeyEnv, "OPENAI_API_KEY"}
	for _, source := range modelsource.Vendored() {
		if source.KeyEnv != "" {
			names = append(names, source.KeyEnv)
		}
	}
	for _, row := range PersistedSources(profileDir) {
		if row.KeyEnv != "" {
			names = append(names, row.KeyEnv)
			// A custom owned variable has the same compatibility read as
			// every other owned variable, so its former spelling is a key too.
			if strings.HasPrefix(row.KeyEnv, "CODEAF_") {
				names = append(names, env.Legacy(row.KeyEnv))
			}
		}
	}
	return names
}
