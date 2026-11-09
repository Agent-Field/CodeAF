package session

import "github.com/Agent-Field/codeaf/internal/executor"

// sayJailLimit names the host's reduced limits once, in the turn that first
// encounters them. Background jobs use it too, with no turn cancellation tied
// to their command lifetime. A fenced conversation names what the fence could
// not apply on this system instead (internal/executor's FenceLimitLine).
func (a *Agent) sayJailLimit() {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	line := executor.DegradedLine
	if a.config.fence() != nil {
		line = executor.FenceLimitLine()
	}
	if hub != nil && line != "" {
		a.jailNotice.Do(func() { hub.send(Event{Kind: EventNotice, Text: line}) })
	}
}
