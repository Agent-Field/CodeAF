package session

// prepareModel runs on the serialized turn road, before auxiliary or main
// provider calls. The callback reads local readiness only.
func (a *Agent) prepareModel(hub *eventHub) (resolved, ready bool) {
	a.mu.Lock()
	prepare, model := a.config.PrepareModel, a.model
	if prepare == nil {
		a.mu.Unlock()
		return false, true
	}
	sources := a.config.Sources.OrDefault(a.config.APIKey, a.config.BaseURL)
	a.mu.Unlock()
	replacement, notice, err := prepare(model, sources)
	if err == nil && replacement != "" {
		err = a.ContinueWithModel(replacement, notice)
		notice = ""
	}
	if err != nil {
		hub.send(Event{Kind: EventError, Err: err})
		return false, false
	}
	a.mu.Lock()
	a.config.PrepareModel = nil
	if notice != "" {
		n := a.recordModelSwitchLocked(notice)
		a.mu.Unlock()
		a.publishModelSwitch(n)
	} else {
		a.mu.Unlock()
	}
	return true, true
}
