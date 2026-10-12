package api

import "github.com/Agent-Field/codeaf/internal/factory"

// ForViewer adds facts that depend on the authenticated caller, never on UI state.
func ForViewer(job *Job, it factory.Item, viewer string) {
	job.Dismissed = factory.IsDismissed(it, viewer)
	if job.State == StateNeedsYou && it.Person() != "" && factory.NormalizeHandle(viewer) != it.Person() {
		job.WaitingFor = "@" + it.Person()
	}
}
