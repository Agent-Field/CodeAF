package session

import "errors"

// ModelIdentity separates execution evidence from the next request's choice.
// Revision orders updates within one live session; a restored snapshot keeps
// Last, but never revives an in-flight request from a previous process.
type ModelIdentity struct {
	Epoch        string `json:"epoch"`
	Selected     string `json:"selected,omitempty"`
	Requested    string `json:"requested,omitempty"`
	Active       string `json:"active,omitempty"`
	Last         string `json:"last,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Mode         string `json:"mode"`
	Chosen       string `json:"chosen,omitempty"`
	LockedReason string `json:"locked_reason,omitempty"`
	RequestMode  string `json:"request_mode,omitempty"`
	Pending      bool   `json:"pending"`
	Revision     uint64 `json:"revision"`
}

func (a *Agent) modelIdentityLocked() ModelIdentity {
	s := a.modelIdentity
	s.Selected = a.model
	s.Chosen = ""
	if choice := a.automaticModelsChosen(); choice != nil {
		s.Chosen = "fixed"
		if *choice {
			s.Chosen = "automatic"
		}
	}
	s.LockedReason = a.automaticModelsLockReason()
	s.Mode = "automatic"
	if !a.automaticModelsAllowed() {
		s.Mode = "fixed"
	}
	s.Pending = s.Active != "" && ((a.spokenModel != "" && a.spokenModel != s.Requested) || (s.RequestMode != "" && s.RequestMode != s.Mode))
	return s
}

func (a *Agent) ModelIdentity() ModelIdentity {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.modelIdentityLocked()
}

func (a *Agent) publishModelIdentity() {
	a.mu.Lock()
	a.modelIdentity.Revision++
	state, hub := a.modelIdentityLocked(), a.hub
	a.mu.Unlock()
	a.stampModel()
	hub.send(Event{Kind: EventModelIdentity, ModelIdentity: &state})
}

// The main request of a chat/role/task owns the chip. Background title, memory
// and checking errands must not replace the foreground model with their own.
func (a *Agent) foregroundModelCall(purpose callPurpose) bool {
	return purpose == purposeTurn || (a.config.taskID != 0 && purpose == purposeTask) ||
		(a.config.crewRole != "" && string(purpose) == string(a.config.crewRole))
}

func (a *Agent) startModelRequest(model, provider string) {
	a.mu.Lock()
	a.modelIdentity.Active, a.modelIdentity.Provider, a.modelIdentity.Requested = model, provider, model
	a.modelIdentity.RequestMode = "fixed"
	if a.automaticModelsAllowed() {
		a.modelIdentity.RequestMode = "automatic"
	}
	a.mu.Unlock()
	a.publishModelIdentity()
}

func (a *Agent) servedModel(model string) {
	a.mu.Lock()
	changed := a.modelIdentity.Active != "" && a.modelIdentity.Active != model
	if changed {
		a.modelIdentity.Active = model
	}
	a.mu.Unlock()
	if changed {
		a.publishModelIdentity()
	}
}

func (a *Agent) endModelRequest(model string) {
	a.mu.Lock()
	if model == a.modelIdentity.Requested && a.modelIdentity.Active != "" {
		model = a.modelIdentity.Active
	}
	a.modelIdentity.Last, a.modelIdentity.Active = model, ""
	a.mu.Unlock()
	a.publishModelIdentity()
}

func (a *Agent) LastModel() string { return a.ModelIdentity().Last }

// An explicit fixed choice disables cross-model rescue without disabling
// retries on the same model. Approval automation is unrelated to this policy.
func (a *Agent) automaticModelsAllowed() bool {
	return !a.config.OneModel && a.automaticModels.Load() != 1
}

// A launch constraint is not a saved preference. Nil keeps inherited policy
// inherited when a later launch no longer has the one-model restriction.
func (a *Agent) automaticModelsChosen() *bool {
	switch a.automaticModels.Load() {
	case 1:
		choice := false
		return &choice
	case 2:
		choice := true
		return &choice
	default:
		return nil
	}
}
func (a *Agent) automaticModelsLockReason() string {
	if a.config.OneModel {
		return "This session was launched with one-model; automatic model selection is unavailable."
	}
	return ""
}
func (a *Agent) SetAutomaticModels(enabled bool) error {
	if reason := a.automaticModelsLockReason(); enabled && reason != "" {
		return errors.New(reason)
	}
	mode := int32(1)
	if enabled {
		mode = 2
	}
	a.automaticModels.Store(mode)
	a.publishModelIdentity()
	return nil
}
