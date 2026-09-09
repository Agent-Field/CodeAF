package session

// Explicit delegation is a model decision. Only dependencies that cannot run
// refuse it here; wording such as "commit" or "read" cannot measure task scope.
func (a *Agent) refuseProposedTask(spec taskSpec) string {
	if missing, failed := a.graph().doomedDependencies(spec.dependsOn); len(missing)+len(failed) > 0 {
		return dependencyRefusal(missing, failed)
	}
	return ""
}
