package api

import "github.com/Agent-Field/codeaf/internal/factory"

// PresentJob is shared by every producer. The service narrows controls using
// its actual doors after caller permissions and runner presence are known.
func PresentJob(j *Job) {
	p := StatePresentation{Kind: j.State}
	switch {
	case j.DoneHow == "closed_locally":
		p.Kind, p.Title, p.Detail = "done", "Closed", "Closed locally; work and spending kept. Reopen to continue."
	case j.Recovery != nil && (j.Recovery.State == "waiting" || j.Recovery.State == "queued"):
		p.Kind, p.Title, p.Detail = "waiting_for_runner", "Waiting to retry", j.Recovery.Reason
	case j.Failure != nil:
		p.Kind, p.Title, p.Detail = "failed", "Step failed", j.Failure.Detail
		if j.Failure.Code == factory.CodeStopped {
			p.Title = "Step stopped"
		}
	case j.NeedsYou != nil && j.NeedsYou.Kind == factory.NeedsOutOfAttempts:
		p.Kind, p.Title = "out_of_rounds", "Stuck"
		if j.FollowUp != nil && j.FollowUp.BlockedBy == "verification" {
			p.Title = "Verification needs retry"
		}
		if j.NeedsYou.Question != nil {
			p.Detail = j.NeedsYou.Question.Text
		}
	case j.SpendingLimit != nil:
		p.Kind, p.Title = "spending", "Spending limit reached"
	case j.State == StateQueued || (j.NeedsYou != nil && j.NeedsYou.Kind == factory.NeedsRunnerOffline):
		p.Kind, p.Title = "waiting_for_runner", "Waiting to start"
	case j.State == StateRunning:
		p.Title = "Running"
		for _, step := range j.Steps {
			if step.State != StepRunning || step.Verification == nil || len(step.Verification.Checks) == 0 {
				continue
			}
			p.Kind, p.Title = "verifying", "Waiting for checks"
			p.Detail = step.Name + ": " + step.Verification.Checks[0].Purpose + " is running. Waiting does not use a model turn."
			break
		}
	case j.State == StateNeedsYou:
		p.Title, p.Detail = "Needs you", j.Reason
	case j.State == StateDone:
		p.Title = "Done"
	case j.State == StateBacklog:
		p.Title = "Backlog"
	}
	j.StatePresentation = p
	j.Controls = JobControls{ChangeAssignee: j.Can.ChangeAssignee, Timeline: len(j.Steps) > 0}
}
