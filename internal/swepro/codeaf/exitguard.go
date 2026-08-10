package codeaf

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/ledgers"
)

type runFailureLedgerAdapter struct {
	recent func(string, ...float64) []*ledgers.LeafFailure
	was    func(string, string) bool
	mark   func(string, string)
}

func (adapter runFailureLedgerAdapter) RecentFailures(rootTaskID string, limit int) []steploop.LeafFailure {
	recent := adapter.recent
	if recent == nil {
		recent = ledgers.GetRecentFailures
	}
	rows := recent(rootTaskID, float64(limit))
	out := make([]steploop.LeafFailure, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		bugs := make([]steploop.FailureBug, 0, len(row.Bugs))
		for _, bug := range row.Bugs {
			var line *float64
			if bug.Line != nil {
				value := float64(*bug.Line)
				line = &value
			}
			bugs = append(bugs, steploop.FailureBug{
				Severity: bug.Severity, File: bug.File, Line: line, Detail: bug.Detail,
			})
		}
		var attempt *float64
		if row.Attempt != nil {
			value := float64(*row.Attempt)
			attempt = &value
		}
		out = append(out, steploop.LeafFailure{
			TaskID: row.TaskID, Title: row.Title, Reason: row.Reason,
			Bugs: bugs, RepairHints: append([]string(nil), row.RepairHints...),
			Confidence: row.Confidence, Attempt: attempt, Timestamp: float64(row.Timestamp),
		})
	}
	return out
}

func (adapter runFailureLedgerAdapter) WasFailureNudged(rootTaskID, taskID string) bool {
	if adapter.was != nil {
		return adapter.was(rootTaskID, taskID)
	}
	return ledgers.WasFailureNudged(rootTaskID, taskID)
}

func (adapter runFailureLedgerAdapter) MarkFailureNudged(rootTaskID, taskID string) {
	if adapter.mark != nil {
		adapter.mark(rootTaskID, taskID)
		return
	}
	ledgers.MarkFailureNudged(rootTaskID, taskID)
}

type recoveryOperation struct {
	Action string `json:"action"`
	TaskID string `json:"taskID"`
	Into   string `json:"into,omitempty"`
	Note   string `json:"note,omitempty"`
	File   string `json:"file,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type recoveryDecision struct {
	Operations []recoveryOperation `json:"operations"`
}

type recoveryDecisionSchema struct{}

func (recoveryDecisionSchema) SafeParse(raw json.RawMessage) agentjson.Validation[recoveryDecision] {
	var decision recoveryDecision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return agentjson.Validation[recoveryDecision]{Issues: []agentjson.Issue{{Message: err.Error()}}}
	}
	issues := []agentjson.Issue{}
	if len(decision.Operations) == 0 {
		issues = append(issues, agentjson.Issue{Message: "operations must contain at least one recovery operation"})
	}
	for index, operation := range decision.Operations {
		prefix := fmt.Sprintf("operations[%d]", index)
		if operation.TaskID == "" {
			issues = append(issues, agentjson.Issue{Message: prefix + ".taskID is required"})
		}
		switch operation.Action {
		case "split":
			if operation.Into == "" {
				issues = append(issues, agentjson.Issue{Message: prefix + ".into is required for split"})
			}
		case "pivot":
			if operation.File == "" {
				issues = append(issues, agentjson.Issue{Message: prefix + ".file is required for pivot"})
			}
		case "amend":
			if operation.Note == "" {
				issues = append(issues, agentjson.Issue{Message: prefix + ".note is required for amend"})
			}
		case "cancel":
			if operation.Reason == "" {
				issues = append(issues, agentjson.Issue{Message: prefix + ".reason is required for cancel"})
			}
		default:
			issues = append(issues, agentjson.Issue{Message: prefix + ".action must be split, pivot, amend, or cancel"})
		}
	}
	return agentjson.Validation[recoveryDecision]{Data: decision, Issues: issues}
}

// exitGuardBeforeAudit wires unresolved-failures.ts into codeaf's quiet-to-audit boundary.
func (runner *pipeline) exitGuardBeforeAudit(
	ctx context.Context, projectID, rootID string,
	redispatch func(context.Context, string, string, string) error,
) error {
	ledger := runFailureLedgerAdapter{}
	guard := steploop.PlanDBExitGuard{DB: plandb.GetPlanDB(), Ledger: ledger}
	open, err := guard.FindOpenCapExhaustFailures(ctx, steploop.ExitGuardQuery{
		Workspace: runner.workspace, DBPath: runner.dbPath, RootTaskID: rootID,
	})
	if err != nil || len(open) == 0 {
		return nil
	}
	taskIDs := make([]string, 0, len(open))
	for _, failure := range open {
		taskIDs = append(taskIDs, failure.TaskID)
		guard.MarkFailureNudged(rootID, failure.TaskID)
	}
	runner.note("[codeaf] exit guard fired: failures=" + strings.Join(taskIDs, ",") + " tasks=" + strings.Join(taskIDs, ",") + "\n")
	runner.events.stage("exit-guard", "fired", map[string]any{
		"failure_ids": taskIDs, "task_ids": taskIDs,
	})

	decision, recoveryErr := runner.runRecoveryTurn(ctx, open)
	applied, failed := runner.applyRecoveryDecision(open, decision)
	status := "completed"
	if recoveryErr != nil {
		status = "failed"
	}
	runner.note(fmt.Sprintf("[codeaf] exit guard recovery turn: %s applied=%d failed=%d\n", status, applied, failed))
	recoveryData := map[string]any{
		"applied": applied, "failed": failed,
	}
	if recoveryErr != nil {
		recoveryData["error"] = recoveryErr.Error()
	}
	runner.events.stage("exit-guard", "recovery-"+status, recoveryData)
	if redispatch == nil {
		redispatch = runner.dispatchUntilQuiet
	}
	err = redispatch(ctx, projectID, rootID, "")
	redispatchStatus := "completed"
	if err != nil {
		redispatchStatus = "failed"
	}
	runner.note("[codeaf] exit guard re-dispatch: " + redispatchStatus + "\n")
	runner.events.stage("exit-guard", "redispatch-"+redispatchStatus, nil)
	return err
}

func (runner *pipeline) runRecoveryTurn(
	ctx context.Context, open []steploop.OpenFailure,
) (recoveryDecision, error) {
	outputPath := filepath.Join(runner.workspace, ".codeaf", "agents", "exit-guard", "recovery.json")
	prompt := steploop.RenderRecoveryReminder(open) + "\n\n" + strings.Join([]string{
		"This codeaf pipeline accepts the recovery plan through a strict JSON artifact.",
		"Write {\"operations\":[...]} to " + outputPath + ".",
		"Each operation has action (split|pivot|amend|cancel), taskID, and the action field:",
		"split: into; pivot: file; amend: note; cancel: reason.",
		"Use exactly one operation for each unresolved leaf and no unrelated operations.",
	}, "\n")
	maxRetries := 0
	label := "exit-guard recovery"
	result, err := agentjson.DispatchJSON(ctx, agentjson.Input[recoveryDecision]{
		Agent: runner.entryAgent, ParentSessionID: runner.sessionID,
		Workspace: runner.workspace, TaskPrompt: prompt, OutputPath: outputPath,
		Schema: recoveryDecisionSchema{}, MaxRetries: &maxRetries, Label: &label,
	}, runner.agentJSON())
	return result.Data, err
}

func (runner *pipeline) applyRecoveryDecision(
	open []steploop.OpenFailure, decision recoveryDecision,
) (int, int) {
	applied, failed := 0, 0
	allowed := recoveryScope(open, plandb.GetPlanDB().SnapshotState())
	for _, operation := range decision.Operations {
		if !allowed[operation.TaskID] {
			failed++
			runner.note("[codeaf] exit guard out-of-scope recovery op: " + operation.Action + " " + operation.TaskID + "\n")
			continue
		}
		args := []string{"plandb", "task", operation.Action, operation.TaskID}
		switch operation.Action {
		case "split":
			args = []string{"plandb", "split", operation.TaskID, "--into", operation.Into}
		case "pivot":
			args = append(args, "--file", operation.File)
		case "amend":
			args = append(args, "--prepend", operation.Note)
		case "cancel":
			args = append(args, "--reason", operation.Reason)
		}
		result := plandb.RunPlanDB(args)
		if result.Code == 0 {
			applied++
			if operation.Action == "split" {
				var created []*plandb.Task
				if json.Unmarshal(result.Stdout, &created) == nil {
					for _, task := range created {
						if task != nil {
							allowed[string(task.ID)] = true
						}
					}
				}
			}
		} else {
			failed++
			runner.note("[codeaf] exit guard operation failed: " + operation.Action + " " + operation.TaskID + " — " + jscompat.Trim(string(result.Stderr)) + "\n")
		}
	}
	return applied, failed
}

func recoveryScope(open []steploop.OpenFailure, snapshot plandb.Snapshot) map[string]bool {
	allowed := map[string]bool{}
	stack := make([]string, 0, len(open))
	for _, failure := range open {
		if !allowed[failure.TaskID] {
			allowed[failure.TaskID] = true
			stack = append(stack, failure.TaskID)
		}
	}
	downstream := map[string][]string{}
	for _, dependency := range snapshot.Dependencies {
		from, to := string(dependency.FromTask), string(dependency.ToTask)
		downstream[from] = append(downstream[from], to)
	}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, taskID := range downstream[current] {
			if allowed[taskID] {
				continue
			}
			allowed[taskID] = true
			stack = append(stack, taskID)
		}
	}
	return allowed
}

var _ steploop.FailureLedger = runFailureLedgerAdapter{}
