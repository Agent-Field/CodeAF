package run

import (
	"github.com/Agent-Field/codeaf/internal/factory"
)

// A completed feedback pass may reset its retry counter locally. The held
// attempt, round, head and captured feedback still fence that completion.
// Never accept an arbitrary counter reset as a new attempt or a stale result.
func completedFollowReport(it factory.Item, report factory.RunReport) bool {
	f, n := it.FollowUp, report.FollowUp
	if f == nil || n == nil || f.Kind != "pr" || n.Kind != "pr" || f.HeadSHA == "" || n.HeadSHA != f.HeadSHA || f.State != "running" || (n.State != "watching" && n.State != "healthy") || n.ReviewKey != f.ReviewKey || n.EventKey != f.EventKey || report.Question != nil || (it.QKind != "" && it.QKind != "results_not_saved") || it.Manual || it.PullRequest == nil || it.PullRequest.HeadSHA != f.HeadSHA {
		return false
	}
	if n.Attempt != f.Attempt && !(n.Attempt == 0 && f.Attempt > 0) {
		return false
	}
	if it.Stream == nil {
		return false
	}
	for _, step := range report.Stages {
		if step.Name != factory.FollowPRStep || step.Result == nil || step.State != factory.PhaseWaiting {
			continue
		}
		r := *step.Result
		if r.Checkpoint || r.ExecutionBlocked != "" || r.JobBudgetStopped || verificationReason(r) != "" || r.Repair == nil || r.Repair.Decision != "no_change" || factory.ReadinessReportFault(f.HeadSHA, r) != "" {
			continue
		}
		for _, ph := range it.Stream.Phases {
			if ph.Name == step.Name && ph.Round == step.Round && ph.Started.Equal(step.Started) && ph.State == factory.PhaseRunning {
				return true
			}
		}
	}
	return false
}
