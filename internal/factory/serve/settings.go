package serve

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
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
		h.write(w, 200, h.settingsView(settings))
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
		if body.RunnerTimeoutSeconds == nil && body.KeepOutputDays == nil && body.KeepOutputMB == nil && body.ReadOnArrival == nil && body.AgentRunner == nil {
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
		if body.AgentRunner != nil {
			choice := strings.TrimSpace(*body.AgentRunner)
			if !slices.Contains(h.agentRunners(), choice) {
				if choice == factory.AgentRunnerServer {
					return errors.New("this organization's server cannot run agents yet; they run on the laptop of whoever drives the workstream")
				}
				return errors.New(`agent_runner is "driver" or "server"`)
			}
			out.AgentRunner = choice
		}
		return nil
	})
	if err != nil {
		h.fail(w, 422, err.Error())
		return
	}
	h.recordOrg(r, record.SettingsChanged, "changed settings", orgObjectFactory, "")
	h.machineEvents()
	h.write(w, 200, h.settingsView(settings))
}

// agentRunners is where this server can run an organization workstream's
// agent. Every organization can run it on its driver's laptop. Running it on
// the server needs the server runner (WAVE2.md §4.3), which this build does
// not have yet, so the choice is ABSENT rather than offered and failing.
func (h *Handler) agentRunners() []string {
	return []string{factory.AgentRunnerDriver}
}

func (h *Handler) settingsView(s api.FactorySettings) api.FactorySettingsView {
	s.AgentRunner = factory.AgentRunnerOf(s.AgentRunner)
	return api.FactorySettingsView{FactorySettings: s, AgentRunners: h.agentRunners()}
}

// agentRunner is the operator's saved choice; a store that cannot be read
// answers driver, the one every organization can run.
func (h *Handler) agentRunner() string {
	if h.config.Store == nil {
		return factory.AgentRunnerDriver
	}
	s, err := h.config.Store.RunnerSettings()
	if err != nil {
		return factory.AgentRunnerDriver
	}
	return factory.AgentRunnerOf(s.AgentRunner)
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
