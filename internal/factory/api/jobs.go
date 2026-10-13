package api

import "github.com/Agent-Field/codeaf/internal/factory"

// ForViewer adds facts that depend on the authenticated caller, never on UI state.
func ForViewer(job *Job, it factory.Item, viewer string) {
	job.Dismissed = factory.IsDismissed(it, viewer)
	if job.State == StateNeedsYou && it.Person() != "" && factory.NormalizeHandle(viewer) != it.Person() {
		job.WaitingFor = "@" + it.Person()
	}
}

// NewJob keeps hand-off metadata in the binary; the app sends no store shape.
type NewJob struct {
	Repo      string             `json:"repo"`
	Title     string             `json:"title"`
	Text      string             `json:"text"`
	Source    string             `json:"source,omitempty"`
	FromRef   *factory.SourceRef `json:"from_ref,omitempty"`
	Assignee  *string            `json:"assignee,omitempty"`
	Start     string             `json:"start,omitempty"`
	Autostart bool               `json:"autostart,omitempty"`
}
