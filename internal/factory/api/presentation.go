package api

import "github.com/Agent-Field/codeaf/internal/factory"

// PresentJob is shared by every producer. The service narrows controls using
// its actual doors after caller permissions and runner presence are known.
func PresentJob(j *Job) {
	p := StatePresentation{Kind: j.State}
	switch {
	case j.Failure != nil:
		p.Kind, p.Title, p.Detail = "failed", "Step failed", j.Failure.Detail
		if j.Failure.Code == "stopped" {
			p.Title = "Step stopped"
		}
	case j.NeedsYou != nil && j.NeedsYou.Kind == factory.NeedsOutOfAttempts:
		p.Kind, p.Title = "out_of_rounds", "Stuck"
		if j.NeedsYou.Question != nil {
			p.Detail = j.NeedsYou.Question.Text
		}
	case j.SpendingLimit != nil:
		p.Kind, p.Title = "spending", "Spending limit reached"
	case j.State == StateQueued || (j.NeedsYou != nil && j.NeedsYou.Kind == factory.NeedsRunnerOffline):
		p.Kind, p.Title = "waiting_for_runner", "Waiting to start"
	case j.State == StateRunning:
		p.Title = "Running"
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
