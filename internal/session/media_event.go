package session

// MediaChange invalidates a source listing after one actual index append.
// Immutable source facts still come from ListMedia/FindMedia; no path grant or
// guessed user-message join travels on this notification.
type MediaChange struct {
	ID   string `json:"id"`
	Run  string `json:"run,omitempty"`
	Call string `json:"call,omitempty"`
}

func (a *Agent) emitMediaChange(id string, fact MediaFact) {
	change := MediaChange{ID: id}
	if len(fact.Run) <= 1024 {
		change.Run = fact.Run
	}
	if len(fact.Tool) <= 1024 {
		change.Call = fact.Tool
	}
	event := Event{Kind: EventMediaUpdate, Media: &change}
	a.mu.Lock()
	hub := a.hub
	watchers := append([]*eventStream(nil), a.taskWatchers...)
	a.mu.Unlock()
	if hub != nil {
		hub.send(event)
	}
	for _, watcher := range watchers {
		watcher.send(event)
	}
}
