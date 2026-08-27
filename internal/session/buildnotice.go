package session

// noticeNewerBuild puts a replacement discovered at the turn boundary into
// the same quiet transcript lane as the other notices a person can act on.
func (a *Agent) noticeNewerBuild() {
	if a == nil || a.config.InTask || a.config.Errand || a.config.newerBuild == nil {
		return
	}
	text := a.config.newerBuild()
	if text == "" {
		return
	}

	a.mu.Lock()
	hub := a.hub
	running := a.running
	a.mu.Unlock()
	if running && hub != nil {
		hub.send(Event{Kind: EventNotice, Text: text})
	}
}
