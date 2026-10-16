package session

// emitRenderUpdate fans one render's record out to whoever is listening, on the
// same two lanes as [Agent.emitJobUpdate]: the turn's hub when a turn is in
// flight and every standing watcher. It is called by the render ledger after
// each change; the ledger — not a surface — is the durable account, so an event
// nobody heard costs nothing: render.list shows the record.
func (a *Agent) emitRenderUpdate(record RenderRecord) {
	event := Event{Kind: EventRenderUpdate, Render: &record}
	a.mu.Lock()
	hub := a.hub
	watchers := make([]*eventStream, len(a.taskWatchers))
	copy(watchers, a.taskWatchers)
	a.mu.Unlock()
	if hub != nil {
		hub.send(event)
	}
	for _, watcher := range watchers {
		watcher.send(event)
	}
}
