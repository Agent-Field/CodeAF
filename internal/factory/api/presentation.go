package api

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const ConcurrencySettingsHint = "Change Jobs at once in Settings → Factory → Your computers."

// WaitingForComputer is a queued job whose work is on a computer that is
// offline or asleep: no runner or Jobs at once setting can start it elsewhere.
const WaitingForComputer = "waiting_for_computer"

// PresentJob is shared by every producer. The service narrows controls using
// its actual doors after caller permissions and runner presence are known.
func PresentJob(j *Job) {
	p := StatePresentation{Kind: j.State}
	switch {
	case j.DoneHow == "closed_locally" && j.Rejection != nil:
		p.Kind, p.Title, p.Detail = "done", "Rejected", endSentence(factory.RejectedWords(j.Rejection.Words))+" Reopen or Run as new to try again."
	case j.DoneHow == "closed_locally":
		p.Kind, p.Title, p.Detail = "done", "Closed", "Closed locally; work and spending kept. Reopen to continue."
	case j.Recovery != nil && (j.Recovery.State == "waiting" || j.Recovery.State == "queued"):
		p.Kind, p.Title, p.Detail = "waiting_for_runner", "Waiting to retry", j.Recovery.Reason
	case j.Failure != nil:
		// THE ONE LINE A PERSON READS FIRST IS THE FAILURE'S SUMMARY when it
		// has one (the step's own outcome, or the manager's line), and its
		// whole detail otherwise; the detail stays on the failure, whole.
		p.Kind, p.Title, p.Detail = "failed", "Step failed", firstNonBlank(j.Failure.Summary, j.Failure.Detail)
		if j.Failure.Code == factory.CodeStopped {
			p.Title = "Step stopped"
		}
	case j.State != StateDone && j.Desk != nil && j.Desk.PersonStopped != nil && *j.Desk.PersonStopped:
		p.Kind, p.Title, p.Detail = "stopped", "Stopped by you", factory.PersonStopReason
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
	case j.State == StateRunning && j.Desk != nil && j.Desk.Run != nil && j.Desk.Run.Paused:
		p.Kind, p.Title, p.Detail = "paused", "Paused", "Work and step conversation kept. Resume continues this step."
	case j.State == StateQueued && j.Held:
		p.Kind, p.Title, p.Detail = "held", "Held", "Held in the queue. It starts only when you resume it."
	case j.State == StateQueued && j.StatePresentation.Kind == WaitingForComputer:
		// Nothing starts this job until its computer is back, Start now included.
		p = j.StatePresentation
	case j.State == StateQueued && strings.TrimSpace(j.RunOn) != "":
		// An AgentField job (run_on) takes no runner and no Jobs at once
		// place: Start dispatched it to its node, and it is starting there
		// until the run reports, a few seconds at most.
		node, _, _ := strings.Cut(strings.TrimSpace(j.RunOn), ".")
		p.Title, p.Detail = "Starting", "Starting on AgentField · "+node+"."
	case j.State == StateQueued && j.OverLimit:
		p.Kind, p.Title, p.Detail = "waiting_for_runner", "Starting now", "Starting now, above Jobs at once this one time."
	case j.State == StateQueued || (j.NeedsYou != nil && j.NeedsYou.Kind == factory.NeedsRunnerOffline):
		p.Kind, p.Title = "waiting_for_runner", "Waiting to start"
		if j.State == StateQueued {
			p.Detail = "Queued. Waiting for an available runner. " + ConcurrencySettingsHint
			// Presence decorates only this read, never the saved job or its version.
			if j.StatePresentation.Kind == "waiting_for_capacity" {
				p = j.StatePresentation
			}
		}
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
	case j.State == StateNeedsYou && j.NeedsYou != nil && j.NeedsYou.Kind == factory.NeedsApproveStep && j.NeedsYou.Step != "":
		// A MANUAL HOLD SAYS WHICH STEP IS NEXT, in a person's words, never
		// the machine reason it is filed under.
		p.Title, p.Detail = "Needs you", j.NeedsYou.Step+" is next · waiting for you to go on"
	case j.State == StateNeedsYou:
		p.Title, p.Detail = "Needs you", j.Reason
	case j.State == StateDone:
		p.Title = "Done"
	case j.State == StateBacklog && strings.TrimSpace(j.Repo) == "" && strings.TrimSpace(j.RunOn) == "":
		// Outside work no rule or person has given a repository yet: the
		// launch refuses it ([factory.NoRepoRefusal]), so its read says why.
		// A job an AgentField reasoner runs (run_on) needs none, so it waits
		// as ordinary backlog.
		p.Title, p.Detail = factory.NoRepoTitle, factory.NoRepoDetail
	case j.State == StateBacklog:
		p.Title = "Backlog"
	}
	j.StatePresentation = p
	publishing := j.Publishing != nil && (j.Publishing.State == "queued" || j.Publishing.State == "pushing")
	j.Controls = JobControls{ChangeAssignee: j.Can.ChangeAssignee, Timeline: len(j.Steps) > 0,
		GiveBack: j.Manual && j.State != StateDone && !publishing}
}

// endSentence is s ending its sentence once: a full stop is added unless the
// words already end in one, or in ! ? or …, so a person's own "Too broad."
// never reads "Too broad.." (live A+B run 2, E3).
func endSentence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") || strings.HasSuffix(s, "…") {
		return s
	}
	return s + "."
}
