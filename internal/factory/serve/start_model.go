package serve

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// startModelRefused refuses a person's start before any write when neither
// the job's model nor the profile's first fallback is on a service with a key:
// the job would only fail at its first step's first call. ONLY A SOLO FACTORY
// CLAIMS THIS: it runs on its person's own profile. A team server holds no
// one's keys (its ProfileDir is the server's own home), so it claims nothing;
// nor does a job on automatic models, whose role routing may use other keyed
// models, or a job with no model of its own and no default.
func (h *Handler) startModelRefused(w http.ResponseWriter, it factory.Item) bool {
	profile := strings.TrimSpace(h.config.ProfileDir)
	if h.mode() != api.FactorySolo || profile == "" {
		return false
	}
	if it.AutomaticModels != nil && *it.AutomaticModels {
		return false
	}
	model := strings.TrimSpace(it.Model)
	if model == "" && h.config.DefaultModel != nil {
		model = strings.TrimSpace(h.config.DefaultModel())
	}
	if model == "" {
		return false
	}
	sources := config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)
	if keyed(sources, model) {
		return false
	}
	if first, _, _ := strings.Cut(config.ModelFallbacksAt(profile), ","); strings.TrimSpace(first) != "" && keyed(sources, first) {
		return false
	}
	h.refuse(w, 409, fmt.Sprintf("This job can't start: its model, %s, is on %s, which has no key. Add a key for %s in Settings, or choose another model for this job.", model, serviceName(sources, model), serviceName(sources, model)), "conflict", "", nil, factory.JobState(it))
	return true
}

// keyed says model's service can authenticate (a key, a sign-in, or a service
// that needs none).
func keyed(sources modelsource.Set, model string) bool {
	base, _ := roles.SplitEffort(strings.TrimSpace(model))
	service, _ := sources.For(base)
	return service.HasCredentials()
}

// serviceName is the service model is served by, as a person finds it in
// Settings.
func serviceName(sources modelsource.Set, model string) string {
	base, _ := roles.SplitEffort(strings.TrimSpace(model))
	service, _ := sources.For(base)
	if name := strings.TrimSpace(service.Source.Name); name != "" {
		return name
	}
	if id := strings.TrimSpace(service.Source.ID); id != "" {
		return id
	}
	return "its service"
}
