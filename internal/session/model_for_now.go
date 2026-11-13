package session

import (
	"errors"
	"strings"
)

// ConfiguredModel is the saved choice, even while this run needs another model.
func (a *Agent) ConfiguredModel() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.configuredModelLocked()
}

func (a *Agent) configuredModelLocked() string {
	if a.modelForNowConfigured != "" {
		return a.modelForNowConfigured
	}
	return a.model
}

// UseModelForNow changes future requests without replacing the saved choice or
// pair. Choosing the configured model again removes the temporary selection.
func (a *Agent) UseModelForNow(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("choose a model")
	}
	a.modelChanges.Lock()
	defer a.modelChanges.Unlock()
	a.mu.Lock()
	configured := a.configuredModelLocked()
	if a.modelForNowProvider == "" {
		a.modelForNowProvider = a.config.serviceFor(configured).Source.ID
	}
	a.modelForNowConfigured = configured
	if model == configured {
		a.modelForNowConfigured = ""
	}
	a.model = model
	if a.running {
		// Let the current call finish; the next request takes this model.
		a.spokenModel = model
	}
	if a.clientPool != nil {
		a.clientPool.setSeat(model)
	}
	if !a.running {
		a.rebindClientLocked(model)
	}
	if a.config.ContextWindowFor != nil {
		if window := a.config.ContextWindowFor(model); window > 0 {
			a.contextWindow.Store(int64(window))
		}
	}
	a.noteModelWindow(model)
	a.scrubBlindImagePartsLocked(model)
	a.followModelOnThePageLocked()
	a.mu.Unlock()
	a.publishModelIdentity()
	return nil
}

// Request routing uses a copy of the saved pair so a temporary main never
// enters metadata or a later explicit edit of the conversation's fallback.
func (a *Agent) requestModelPair() (ConversationModels, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conversationModels == nil {
		return ConversationModels{}, false
	}
	pair := *a.conversationModels
	if a.modelForNowConfigured != "" {
		pair.Main = a.model
		if pair.Fallback == pair.Main {
			pair.Fallback = ""
		}
	}
	return pair, true
}
