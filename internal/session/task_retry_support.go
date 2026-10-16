package session

// retrySupported is a capability of the actual owning road, not a guess from
// a title, kind or task number. Admission still checks the current task state,
// workspace and budget when the person explicitly requests a fresh attempt.
func (g *TaskGraph) retrySupported(id uint64) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	node := g.nodes[id]
	return node != nil && node.spec.quick == nil && !g.deleted[id]
}
