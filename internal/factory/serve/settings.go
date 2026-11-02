package serve

import (
	"errors"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func (h *Handler) runnerPolicy(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	if r.Method == "GET" {
		settings, err := h.config.Store.RunnerSettings()
		if h.failed(w, err) {
			return
		}
		h.write(w, 200, settings)
		return
	}
	if !h.requireOperator(w, r) {
		return
	}
	var body api.PatchFactorySettings
	if !h.decode(w, r, &body, false) {
		return
	}
	settings, err := h.config.Store.ChangeRunnerSettings(func(out *api.FactorySettings) error {
		if body.RunnerTimeoutSeconds == nil && body.KeepOutputDays == nil && body.KeepOutputMB == nil && body.ReadOnArrival == nil {
			return errors.New("needs at least one runner setting")
		}
		if body.RunnerTimeoutSeconds != nil {
			out.RunnerTimeoutSeconds = *body.RunnerTimeoutSeconds
		}
		if body.KeepOutputDays != nil {
			out.KeepOutputDays = *body.KeepOutputDays
		}
		if body.KeepOutputMB != nil {
			out.KeepOutputMB = *body.KeepOutputMB
		}
		if body.ReadOnArrival != nil {
			out.ReadOnArrival = *body.ReadOnArrival
		}
		return nil
	})
	if err != nil {
		h.fail(w, 422, err.Error())
		return
	}
	h.machineEvents()
	h.write(w, 200, settings)
}

// Every presence door reads the same saved silence deadline as lease renewal.
func (h *Handler) runnerOfflineAfter() time.Duration {
	if h.config.Store != nil {
		if value, err := h.config.Store.RunnerSettings(); err == nil {
			return time.Duration(value.RunnerTimeoutSeconds) * time.Second
		}
	}
	return lease.OfflineAfter
}
