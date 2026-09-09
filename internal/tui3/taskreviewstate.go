package tui3

import "github.com/Agent-Field/aforge-v2/internal/session"

// taskReviewPendingWord is the ROSTER'S remaining word for a landing aforge is
// deciding, and it is on its way out: `awaiting review` is deleted as
// person-facing text by docs/design/task-states/DESIGN.md, which says such a row
// reads `your call · nobody could check it · aforge is deciding` instead. The
// landing card stopped drawing it with this change; the rail, the roster, the
// record page and the room still do, and the rail lane owns their deletion.
const taskReviewPendingWord = "awaiting review"

// taskReviewPending reports that ONE NODE'S DECISION IS THE MODEL'S RIGHT NOW.
//
// It reads the landing card's own reading rather than a flag this surface used
// to set for itself: the engine records who is holding a question
// ([session.TaskAskOwner], written by `task.settle = auto`, by the person
// handing one card over and by the floor that hands it back at the end of a
// turn), and a second answer kept here would go stale the moment any of those
// three moved it. An unanswered decision stays visible even inside a family.
func (a *app) taskReviewPending(node *taskNode) bool {
	if node == nil || node.state != session.TaskUnverified || a.tasks[node.id] != node {
		return false
	}
	card := a.doneCardFor(node.id)
	return card != nil && card.status.Ask.Owner == session.TaskAskOwnerModel
}
