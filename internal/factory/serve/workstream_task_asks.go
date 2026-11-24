package serve

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── A PERSON ASKS SOMEONE (Workstreams final spec, "Working with other
// people"; v2 J7 "Priya asks") ──
//
// A person raises an Ask on a task to ONE named decider, through the same
// add_ask a task's other Asks use (factory.ApplyPatch): there is one Ask
// model. The decider sees it in their Inbox under Waiting on you, because the
// Inbox reads every open Ask whose decider is the viewer, and their answer is
// kept as a decision through the one answer door (answerWorkstreamTask).

// checkDecider answers d as someone in ws, with the name they go by there, or
// refuses: an Ask goes to a person who can open the task and answer it, never
// to someone outside the workstream, whose Inbox would never show it.
func (h *Handler) checkDecider(ws factory.OrgWorkstream, d factory.PersonRef) (factory.PersonRef, error) {
	handle := factory.NormalizeHandle(d.ID)
	name, in := h.wsPeopleNames(ws)[handle]
	if handle == "" || !in {
		who := firstNonEmpty(d.Name, d.ID, "That person")
		return d, factory.TaskInvalid(fmt.Sprintf("%s is not in this workstream, so they cannot answer its Asks; ask someone who is", who))
	}
	return factory.PersonRef{ID: handle, Name: firstNonEmpty(name, d.Name, handle)}, nil
}
