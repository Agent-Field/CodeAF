package tui3

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A label change carries saved and temporary selections independently. A
// working turn keeps both until settlement, alongside any explicit next pick.
type modelRename struct {
	save string
	live string
	pair *session.ConversationModels
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
	if deferred := a.deferredModelRename; deferred != nil {
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
	rename := &modelRename{save: saved, pair: changedPair, live: config.ReprefixModelID(live, oldWritten, newWritten)}
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
	configured := a.configuredModel()
	if rename.save != "" {
		configured = rename.save
	}
	a.queueModelChange(rename.live, 0, rename.live != configured, true, rename)
}
