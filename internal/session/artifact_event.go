package session

// ArtifactChange invalidates a deliverables listing after one actual append of
// an authored row to the artifacts index. It is generic on purpose: it names the
// deliverable the row recorded, its kind, and the run and call that made it,
// and nothing else. No path, no file bytes, no card text: a surface that wants
// the descriptor asks for it by id, which is how it learns whether the file is
// still there.
//
// It follows [MediaChange]'s pattern and is NOT a media source id: a markdown
// document is not media, and the two events are separate facts of one append.
type ArtifactChange struct {
	ID   string `json:"id"`
	Kind string `json:"kind,omitempty"`
	Run  string `json:"run,omitempty"`
	Call string `json:"call,omitempty"`
}

// emitArtifactChange announces the row as it was WRITTEN. A row that carries no
// deliverable fact (an attachment, evidence, an oversize fact that was dropped)
// has no deliverable id to name, so it announces nothing: an event is never
// invented for an append that did not make a deliverable.
func (a *Agent) emitArtifactChange(written Artifact) {
	if written.Deliver == nil || !ValidDeliverableID(written.Deliver.ID) {
		return
	}
	change := ArtifactChange{ID: written.Deliver.ID, Kind: written.Kind}
	if len(change.Kind) > 64 {
		change.Kind = ""
	}
	if len(written.Deliver.Run) <= 1024 {
		change.Run = written.Deliver.Run
	}
	if written.Media != nil && len(written.Media.Tool) <= 1024 {
		change.Call = written.Media.Tool
	}
	if change.Call == "" && len(written.Deliver.Call) <= 1024 {
		change.Call = written.Deliver.Call
	}
	event := Event{Kind: EventArtifactUpdate, Artifact: &change}
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
