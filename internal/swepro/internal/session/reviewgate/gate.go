// This file ports the bounded review/repair orchestration and advisor action
// application from src/session/review-gate.ts:1174-1377 and 1483-1937.
package reviewgate

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/baked"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/scheduler"
	"github.com/Agent-Field/swe-pro-go/internal/util"
)

func (s *Service) GateLeaf(
	ctx context.Context,
	input scheduler.GateInput,
) (scheduler.GateResult, error) {
	if input.Task == nil {
		return scheduler.GateResult{}, errors.New("review-gate: nil impl task")
	}
	task := input.Task
	kind := string(task.Kind)
	if kind == "review" || kind == "repair" {
		s.runPlanDB(
			"context",
			"review skipped: kind="+kind+" (gate doesn't gate its own machinery)",
			"--kind", "review", "--task", task.ID,
		)
		return scheduler.GateResult{
			Status:            scheduler.GateSkipped,
			FinalWorktreePath: input.WorktreePath,
			FinalBranch:       input.Branch,
			Reason:            "gate-machinery kind: " + kind,
		}, nil
	}

	if mode, ok := baked.ModeForAgent(input.SubagentType); ok {
		s.runPlanDB(
			"context",
			"review skipped: "+mode.Name+"-mode agent "+input.SubagentType+
				" (read-only, no source mutation)",
			"--kind", "review", "--task", task.ID,
		)
		return scheduler.GateResult{
			Status:            scheduler.GateSkipped,
			FinalWorktreePath: input.WorktreePath,
			FinalBranch:       input.Branch,
			Reason:            mode.Name + "-mode agent: " + input.SubagentType,
		}, nil
	}

	skip := ShouldSkipReview(
		ctx, input.WorktreePath, input.BaseSHA, s.config, s.run,
	)
	if skip.Skip {
		s.runPlanDB(
			"context", "review skipped: "+skip.Reason,
			"--kind", "review", "--task", task.ID,
		)
		return scheduler.GateResult{
			Status:            scheduler.GateSkipped,
			FinalWorktreePath: input.WorktreePath,
			FinalBranch:       input.Branch,
			Reason:            skip.Reason,
		}, nil
	}

	s.autoCommitImpl(ctx, input.WorktreePath, task.ID, taskTitle(task))
	currentBranch := input.Branch
	currentWorktree := input.WorktreePath
	var lastVerdict *ReviewVerdict
	lastVerdictAuthored := false
	repairsRun := 0
	retryAdvisorFired := false
	previousFingerprint := ""

	for attempt := 0; attempt <= s.config.RepairCap; attempt++ {
		fingerprint := util.FingerprintDiff(ctx, currentWorktree, input.BaseSHA)
		if attempt > 0 && previousFingerprint != "" &&
			fingerprint.SHA256 == previousFingerprint {
			feedback := "(no prior reviewer verdict — first round had no semantic change)"
			if lastVerdict != nil {
				feedback = issueFeedback(*lastVerdict, true)
			}
			decision, issueAdvisorAuthored := s.dispatchIssueAdvisor(ctx, IssueAdvisorInput{
				TaskID: task.ID, Workspace: input.Workspace,
				ParentSessionID:  input.ParentSessionID,
				OriginalSpec:     taskOriginalSpec(task),
				ReviewerFeedback: feedback,
				EscalationReason: "stuck-loop: byte-identical diff across rounds (attempt " +
					strconvInt(attempt) + "/" + strconvInt(s.config.RepairCap) + ")",
				ImplBranch: currentBranch, RepairsRun: repairsRun,
			})
			return s.applyAdvisorDecision(
				decision, task.ID, currentBranch, currentWorktree,
				lastVerdict, repairsRun, !issueAdvisorAuthored,
			), nil
		}
		previousFingerprint = fingerprint.SHA256

		reviewerVerdict, reviewerVerdictAuthored := s.dispatchReviewAttempt(
			ctx, input, currentWorktree, currentBranch, attempt,
		)
		lastVerdictAuthored = reviewerVerdictAuthored
		verdict := reviewerVerdict
		if IsHighRisk(task) {
			verdict = s.runFlaggedSynthesis(
				ctx, input, currentWorktree, currentBranch, reviewerVerdict,
			)
		}
		lastVerdict = &verdict

		if verdict.Verdict == "pass" && hasTag(task.Tags, "tests:required") {
			if !s.worktreeAddsTestFile(ctx, currentWorktree, input.BaseSHA) {
				verdict = enforceRequiredTests(verdict, input.BaseSHA)
				lastVerdict = &verdict
			}
		}

		if verdict.Verdict == "pass" {
			done := verdict.Done
			return scheduler.GateResult{
				Status:            scheduler.GatePass,
				Verdict:           toSchedulerVerdict(verdict),
				FinalWorktreePath: currentWorktree,
				FinalBranch:       currentBranch,
				RepairAttempts:    float64(repairsRun),
				Done:              &done,
			}, nil
		}

		if attempt >= s.config.RepairCap {
			decision, issueAdvisorAuthored := s.dispatchIssueAdvisor(ctx, IssueAdvisorInput{
				TaskID: task.ID, Workspace: input.Workspace,
				ParentSessionID:  input.ParentSessionID,
				OriginalSpec:     taskOriginalSpec(task),
				ReviewerFeedback: issueFeedback(verdict, true),
				EscalationReason: "cap-exhausted: " +
					strconvInt(s.config.RepairCap) +
					" repair rounds did not converge",
				ImplBranch: currentBranch, RepairsRun: repairsRun,
			})
			return s.applyAdvisorDecision(
				decision, task.ID, currentBranch, currentWorktree,
				&verdict, repairsRun,
				!issueAdvisorAuthored || !lastVerdictAuthored,
			), nil
		}

		if !retryAdvisorFired {
			retryAdvisorFired = true
			advice, _ := s.dispatchRetryAdvisor(ctx, RetryAdvisorInput{
				TaskID: task.ID, Workspace: input.Workspace,
				ParentSessionID:  input.ParentSessionID,
				OriginalSpec:     taskOriginalSpec(task),
				ReviewerFeedback: retryFeedback(verdict),
				Attempt:          attempt,
			})
			if advice.Action == "escalate_to_advisor" {
				decision, issueAdvisorAuthored := s.dispatchIssueAdvisor(ctx, IssueAdvisorInput{
					TaskID: task.ID, Workspace: input.Workspace,
					ParentSessionID:  input.ParentSessionID,
					OriginalSpec:     taskOriginalSpec(task),
					ReviewerFeedback: issueFeedback(verdict, false),
					EscalationReason: "retry-advisor recommended escalation: " +
						utf16Slice(advice.Reason, 0, 200),
					ImplBranch: currentBranch, RepairsRun: repairsRun,
				})
				return s.applyAdvisorDecision(
					decision, task.ID, currentBranch, currentWorktree,
					&verdict, repairsRun, !issueAdvisorAuthored,
				), nil
			}
			hint := "(no hint provided)"
			if advice.StrategyHint != nil {
				hint = *advice.StrategyHint
			}
			s.runPlanDB(
				"task", "amend", task.ID,
				"--prepend", "STRATEGY HINT (retry-advisor): "+hint,
			)
		}

		repair := s.dispatchRepair(
			ctx, input, verdict, currentBranch, attempt,
		)
		if !repair.OK || repair.WorktreePath == "" || repair.Branch == "" {
			reason := repair.Error
			if reason == "" {
				reason = "unknown"
			}
			return scheduler.GateResult{
				Status:            scheduler.GateFail,
				Verdict:           toSchedulerVerdict(verdict),
				FinalWorktreePath: currentWorktree,
				FinalBranch:       currentBranch,
				Reason:            "repair dispatch failed: " + reason,
				RepairAttempts:    float64(repairsRun),
				Unadjudicated:     true,
			}, nil
		}
		if currentWorktree != input.WorktreePath {
			s.cleanupWorktree(ctx, input.Workspace, currentWorktree, currentBranch)
		}
		currentBranch = repair.Branch
		currentWorktree = repair.WorktreePath
		repairsRun++
	}

	result := scheduler.GateResult{
		Status:            scheduler.GateFail,
		FinalWorktreePath: currentWorktree,
		FinalBranch:       currentBranch,
		Reason:            "gate loop exited unexpectedly",
		RepairAttempts:    float64(repairsRun),
		Unadjudicated:     true,
	}
	if lastVerdict != nil {
		result.Verdict = toSchedulerVerdict(*lastVerdict)
	}
	return result, nil
}

func hasTag(tags []string, target string) bool {
	for _, tag := range tags {
		if tag == target {
			return true
		}
	}
	return false
}

func taskOriginalSpec(task *plandb.Task) string {
	if task == nil {
		return ""
	}
	if task.Description != nil {
		return *task.Description
	}
	return task.Title
}

func enforceRequiredTests(verdict ReviewVerdict, baseSHA string) ReviewVerdict {
	verdict.Verdict = "fail"
	verdict.Confidence = "high"
	verdict.Done = false
	verdict.Bugs = append(verdict.Bugs, ReviewBug{
		File: "", Severity: "blocker",
		Detail: "tests:required tag set on this task but the final diff contains zero new test files. Add at least one *_test.* file (Go: *_test.go; Python: test_*.py / *_test.py; JS/TS: *.test.{ts,js,tsx,jsx}; etc.) exercising the implemented behavior.",
	})
	verdict.RepairHints = append(
		verdict.RepairHints,
		"Add a new test file covering this task's acceptance criteria.",
	)
	verdict.Evidence += " | tests:required check FAILED: no new test files in diff vs " +
		utf16Slice(baseSHA, 0, 8)
	return verdict
}

func issueFeedback(verdict ReviewVerdict, includeEvidence bool) string {
	type row struct {
		Verdict      string      `json:"verdict"`
		Bugs         []ReviewBug `json:"bugs"`
		Evidence     *string     `json:"evidence,omitempty"`
		SpecCoverage *string     `json:"spec_coverage,omitempty"`
	}
	count := min(5, len(verdict.Bugs))
	value := row{Verdict: verdict.Verdict, Bugs: verdict.Bugs[:count]}
	if includeEvidence {
		value.Evidence = &verdict.Evidence
	} else {
		value.SpecCoverage = &verdict.SpecCoverage
	}
	raw, _ := jscompat.Stringify(value)
	return string(raw)
}

func retryFeedback(verdict ReviewVerdict) string {
	type row struct {
		Verdict      string      `json:"verdict"`
		Bugs         []ReviewBug `json:"bugs"`
		SpecCoverage string      `json:"spec_coverage"`
		RepairHints  []string    `json:"repair_hints"`
	}
	count := min(5, len(verdict.Bugs))
	raw, _ := jscompat.Stringify(row{
		Verdict: verdict.Verdict, Bugs: verdict.Bugs[:count],
		SpecCoverage: verdict.SpecCoverage, RepairHints: verdict.RepairHints,
	})
	return string(raw)
}

func (s *Service) applyAdvisorDecision(
	decision IssueAdvisorDecision,
	taskID string,
	currentBranch string,
	currentWorktree string,
	verdict *ReviewVerdict,
	repairsRun int,
	unadjudicated bool,
) scheduler.GateResult {
	s.runPlanDB(
		"context", "advisor:"+decision.Action+" — "+decision.Reason,
		"--kind", "review", "--task", taskID,
	)
	result := scheduler.GateResult{
		Status:            scheduler.GateFail,
		FinalWorktreePath: currentWorktree,
		FinalBranch:       currentBranch,
		RepairAttempts:    float64(repairsRun),
		AdvisorAction:     decision.Action,
		Unadjudicated:     unadjudicated,
	}
	if verdict != nil {
		result.Verdict = toSchedulerVerdict(*verdict)
	}

	switch decision.Action {
	case "retry_modified":
		criteria := decision.RelaxCriteria
		if len(criteria) > 0 {
			s.runPlanDB(
				"task", "amend", taskID,
				"--prepend", "RELAXED CRITERIA (advisor): "+
					strings.Join(criteria, "; "),
			)
			for _, criterion := range criteria {
				s.runPlanDB(
					"context", "dropped acceptance criterion: "+criterion,
					"--kind", "debt", "--task", taskID,
				)
			}
		}
		result.Reason = "advisor:retry_modified — " +
			strconvInt(len(criteria)) + " criteria relaxed"

	case "retry_approach":
		hint := "(no hint provided)"
		if decision.StrategyHint != nil {
			hint = *decision.StrategyHint
		}
		s.runPlanDB(
			"task", "amend", taskID,
			"--prepend", "ALTERNATE STRATEGY (advisor): "+hint,
		)
		result.Reason = "advisor:retry_approach — " +
			utf16Slice(hint, 0, 120)

	case "split":
		var spec strings.Builder
		for index, subtask := range decision.Subtasks {
			if index > 0 {
				if subtask.DependsOnAbove != nil && *subtask.DependsOnAbove {
					spec.WriteString(" > ")
				} else {
					spec.WriteString(", ")
				}
			}
			spec.WriteString(subtask.Title)
		}
		if spec.Len() > 0 {
			s.runPlanDB("split", taskID, "--into", spec.String())
		}
		result.Status = scheduler.GateEscalated
		result.Reason = "advisor:split — " +
			strconvInt(len(decision.Subtasks)) + " sub-tasks"

	case "accept_with_debt":
		for _, item := range decision.Debt {
			s.runPlanDB(
				"context", "["+item.Severity+"] "+item.Gap,
				"--kind", "debt", "--task", taskID,
			)
		}
		partial, _ := jscompat.Stringify(struct {
			AcceptedWithDebt int `json:"accepted_with_debt"`
		}{AcceptedWithDebt: len(decision.Debt)})
		s.runPlanDB(
			"task", "partial", taskID, "--result", string(partial),
		)
		result.Status = scheduler.GateDonePartial
		result.Reason = "advisor:accept_with_debt — " +
			strconvInt(len(decision.Debt)) + " debt items recorded"

	case "escalate_to_replan":
		blocker := "(no blocker text)"
		if decision.Blocker != nil {
			blocker = *decision.Blocker
		}
		s.runPlanDB(
			"context", "escalation blocker: "+blocker,
			"--kind", "blocker", "--task", taskID,
		)
		result.Status = scheduler.GateEscalated
		result.Reason = "advisor:escalate_to_replan — " +
			utf16Slice(blocker, 0, 120)
		result.AdvisorBlocker = blocker
	}
	return result
}

func toSchedulerVerdict(verdict ReviewVerdict) *scheduler.ReviewVerdict {
	bugs := make([]scheduler.ReviewBug, 0, len(verdict.Bugs))
	for _, bug := range verdict.Bugs {
		var line any
		if bug.Line != nil {
			line = *bug.Line
		}
		bugs = append(bugs, scheduler.ReviewBug{
			Severity: bug.Severity, File: bug.File, Line: line, Detail: bug.Detail,
		})
	}
	return &scheduler.ReviewVerdict{
		Verdict:      scheduler.GateVerdict(verdict.Verdict),
		Confidence:   scheduler.GateConfidence(verdict.Confidence),
		SpecCoverage: verdict.SpecCoverage, Bugs: bugs,
		RepairHints: append([]string(nil), verdict.RepairHints...),
		Evidence:    verdict.Evidence, Raw: verdict,
	}
}
