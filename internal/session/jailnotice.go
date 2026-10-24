package session

import "github.com/Agent-Field/codeaf/internal/executor"

// sayJailLimit names the host's reduced limits once, in the turn that first
// encounters them. Background jobs use it too, with no turn cancellation tied
// to their command lifetime.
func (a *Agent) sayJailLimit() {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub != nil {
		a.jailNotice.Do(func() { hub.send(Event{Kind: EventNotice, Text: executor.DegradedLine}) })
	}
}
