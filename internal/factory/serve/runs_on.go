package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// modelDefaults is what this factory knows about models below a job's own
// picks: its current default model, and, when it runs its own jobs
// (Config.RunnerModels), its runner's. A spending file that cannot be read
// says no default here; the start that reads it refuses on its own.
func (h *Handler) modelDefaults() factory.ModelDefaults {
	var d factory.ModelDefaults
	if h.config.Store != nil {
		if spending, err := h.config.Store.Spending(); err == nil {
			d.Factory = spending.DefaultModel
		}
	}
	if h.config.RunnerModels != nil {
		d.Runner = h.config.RunnerModels()
	}
	return d
}

// modelsIn says on each read job the model its steps run on ([api.ModelsIn]),
// items[i] being jobs[i]'s item. The defaults are read once for the response.
func (h *Handler) modelsIn(jobs []api.Job, items []factory.Item) {
	if len(jobs) == 0 {
		return
	}
	d := h.modelDefaults()
	for i := range jobs {
		if i < len(items) {
			api.ModelsIn(&jobs[i], items[i], d)
		}
	}
}
