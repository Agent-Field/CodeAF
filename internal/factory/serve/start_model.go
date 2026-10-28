package serve

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// startModelRefused refuses a person's start before any write when no model
// the job's first step could answer on is on a service with a key: the job
// would only fail at its first call. Which models count follows the session's
// own policy. On automatic models, which is also a job that never chose (nil:
// a step's conversation then allows automatic models), a refused call may move
// to the profile's fallbacks, up to the provider's limit
// ([provider.MaxFallbackModels]), so the start is refused only when the model
// and every one of those lack a key. With automatic models off, the session
// refuses any substitution, so only the model itself counts.
//
// ONLY A SOLO FACTORY CLAIMS THIS: it runs on its person's own profile. A team
// server holds no one's keys (its ProfileDir is the server's own home), so it
// claims nothing; nor does a profile on one model for every task, whose own
// check refuses a model it cannot reach, or a job with no model of its own and
// no default.
func (h *Handler) startModelRefused(w http.ResponseWriter, it factory.Item) bool {
	profile := strings.TrimSpace(h.config.ProfileDir)
	if h.mode() != api.FactorySolo || profile == "" {
		return false
	}
	if _, one := config.TaskOneModelAt(profile); one {
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
	candidates := []string{model}
	if it.AutomaticModels == nil || *it.AutomaticModels {
		candidates = append(candidates, fallbacksFor(model, config.ParseModelFallbacks(config.ModelFallbacksAt(profile)))...)
	}
	for _, m := range candidates {
		if keyed(sources, m) {
			return false
		}
	}
	h.refuse(w, 409, fmt.Sprintf("This job can't start: its model, %s, is on %s, which has no key. Add a key for %s in Settings, or choose another model for this job.", model, serviceName(sources, model), serviceName(sources, model)), "conflict", "", nil, factory.JobState(it))
	return true
}

// fallbacksFor is the fallbacks a refused call on model may move to, as the
// provider builds them: the list in order, without model, up to its limit.
func fallbacksFor(model string, list []string) []string {
	var out []string
	for _, m := range list {
		if strings.EqualFold(strings.TrimSpace(m), model) {
			continue
		}
		if out = append(out, m); len(out) == provider.MaxFallbackModels {
			break
		}
	}
	return out
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
