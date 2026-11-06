package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/roles"
)

const noAvailableModelWord = "no available model · connect a provider or refresh /model"

// availableConversationModel keeps a preferred model only when it is listed.
// Otherwise the first available chat row in provider order supplies the default.
// Notice and connection rows never qualify, and a thinking level is kept only
// when the model it belongs to is still available.
func availableConversationModel(preferred string, rows []Model) (Model, bool) {
	bare, _ := roles.SplitEffort(strings.TrimSpace(preferred))
	var first Model
	for _, row := range rows {
		if row.Unavailable || row.AddProvider || strings.TrimSpace(row.ID) == "" || !chatModel(row) {
			continue
		}
		if first.ID == "" {
			first = row
		}
		if strings.EqualFold(row.ID, bare) {
			row.ID = strings.TrimSpace(preferred)
			return row, true
		}
	}
	return first, first.ID != ""
}

func (a *app) isAvailableModel(id string) bool {
	model, ok := availableConversationModel(id, a.modelList())
	return ok && strings.EqualFold(model.ID, strings.TrimSpace(id))
}

// ensureAvailableModel reconciles a local conversation with its picker catalog.
// A launch preference is not evidence of availability. An automatic choice uses
// the off-loop run-only door without saving a preference the person did not
// choose. With no rows, only the display is cleared: the engine has no unset-model
// door, and the empty-catalog checks keep that old preference from being used.
func (a *app) ensureAvailableModel() bool {
	if rename := a.deferredModelRename; rename != nil {
		// Relabel only at the owning conversation's turn/replay boundary.
		if a.state != stateWorking && !a.hostReplayLoading {
			a.deferredModelRename = nil
			a.applyModelRename(rename)
		}
	}
	if a.agent == nil {
		return true
	}
	pending := pendingModelSelection(a.agent)
	if pending != nil {
		// Returning before the ask completes still shows the chosen model.
		a.model = pending.model
		if pending.window > 0 {
			a.ctxWindow = pending.window
		}
	}
	if !a.requireListedModel {
		return true
	}
	// An answering request keeps its model until it settles. The turn-end path
	// reconciles the next request after the catalog notification has landed.
	if a.state == stateWorking {
		return a.model != ""
	}
	// A later explicit pick or relabel owns its place on the line. Automatic
	// selections may follow a newer catalog while an earlier ask is unfinished.
	if pending != nil && !pending.temporary {
		return a.model != ""
	}
	preferred := a.configuredModel()
	model, ok := availableConversationModel(preferred, a.modelList())
	if !ok {
		a.model, a.ctxWindow = "", 0
		a.hudStale = true
		return false
	}
	if model.ID != a.model || (pending == nil && model.ID != a.agent.Model()) {
		a.useModelForNow(model.ID, model.ContextLength)
		a.hudStale = true
	}
	return a.model == model.ID
}
