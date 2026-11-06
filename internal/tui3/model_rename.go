package tui3

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A label change carries saved and temporary selections independently. A
// working turn keeps both until settlement unless a later choice supersedes it.
type modelRename struct {
	owner      Agent
	configured string
	running    string
	models     *session.ConversationModels
	save       string
	live       string
	pair       *session.ConversationModels
}

func (a *app) reprefixTemporaryModel(oldWritten, newWritten string) bool {
	configured, live := a.configuredModel(), a.model
	var saved string
	var changedPair *session.ConversationModels
	var pair *session.ConversationModels
	if door, ok := a.agent.(interface{ ModelIdentity() session.ModelIdentity }); ok {
		if saved := door.ModelIdentity().Models; saved != nil {
			copy := *saved
			pair = &copy
		}
	}
	if deferred := a.deferredModelRename; deferred != nil && !a.modelRenameCurrent(deferred) {
		a.deferredModelRename = nil
	}
	owner, original, running, models := a.agent, configured, a.agent.Model(), pair
	if deferred := a.deferredModelRename; deferred != nil {
		owner, original, running, models = deferred.owner, deferred.configured, deferred.running, deferred.models
		saved, changedPair = deferred.save, deferred.pair
		if deferred.save != "" {
			configured = deferred.save
		}
		live = deferred.live
		if deferred.pair != nil {
			pair = deferred.pair
		}
	}
	if configured == live && pair == nil && a.deferredModelRename == nil {
		return false
	}
	next := config.ReprefixModelID(configured, oldWritten, newWritten)
	rename := &modelRename{owner: owner, configured: original, running: running, models: models, save: saved, pair: changedPair, live: config.ReprefixModelID(live, oldWritten, newWritten)}
	if next != configured {
		rename.save = next
	}
	if pair != nil {
		updated := session.ConversationModels{
			Main:     config.ReprefixModelID(pair.Main, oldWritten, newWritten),
			Fallback: config.ReprefixModelID(pair.Fallback, oldWritten, newWritten),
		}
		if updated != *pair {
			rename.pair = &updated
		}
	}
	if rename.save == "" && rename.pair == nil && rename.live == live {
		return true
	}
	if a.state == stateWorking {
		a.deferredModelRename = rename
	} else {
		a.applyModelRename(rename)
	}
	return true
}

func (a *app) applyModelRename(rename *modelRename) {
	if !a.modelRenameCurrent(rename) {
		return
	}
	configured := a.configuredModel()
	if rename.save != "" {
		configured = rename.save
	}
	a.queueModelChange(rename.live, 0, rename.live != configured, true, rename)
}

// An explicit choice made after the label change owns the saved selection,
// including a pair whose main model stayed the same but whose fallback changed.
func (a *app) modelRenameCurrent(rename *modelRename) bool {
	if rename.owner != a.agent || rename.configured != a.configuredModel() || rename.running != a.agent.Model() {
		return false
	}
	var pair *session.ConversationModels
	if door, ok := a.agent.(interface{ ModelIdentity() session.ModelIdentity }); ok {
		pair = door.ModelIdentity().Models
	}
	if pair == nil || rename.models == nil {
		return pair == nil && rename.models == nil
	}
	return *pair == *rename.models
}
