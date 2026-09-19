package session

import (
	"context"
	"time"
)

// PlanAgent is the complete optional plan capability a conversation surface
// may use. It lives in session so the local engine and remote client implement
// one method set rather than allowing each transport or surface to redefine it.
type PlanAgent interface {
	PlanTasks() []PlanTaskRow
	PlanTaskPage(id string) (PlanTaskPage, bool)
	PlanNote(id, text string) error
	PlanPause(id string) error
	PlanResume(id string) error
	PlanCancel(id string) error
	PlanAmend(id, text string) error
	PlanPriority(id string, priority int) error
	PlanRunSummary(rootID string) (RunPlanSummary, bool)
	RefreshRunSummary(ctx context.Context, rootID string, lastLook time.Time) (RunPlanSummary, bool)
}
