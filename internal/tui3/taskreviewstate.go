package tui3

import "github.com/Agent-Field/aforge-v2/internal/session"

const taskReviewPendingWord = "awaiting review"

// taskReviewPending is an explicit handoff, not an inference from a parent's
// activity. An unanswered human decision stays visible even inside a family.
func (a *app) taskReviewPending(node *taskNode) bool {
	if node == nil || node.state != session.TaskUnverified || a.tasks[node.id] != node {
		return false
	}
	card := a.doneCardFor(node.id)
	return card != nil && card.reviewByModel
}
