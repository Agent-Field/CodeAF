// This file ports the narrow interfaces imported by review-gate.ts from
// src/session/retry-advisor.ts:1-112 and
// src/session/issue-advisor.ts:1-141. Their structured dispatch remains on the
// shared internal/session/agentjson runtime.
package reviewgate

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
)

type RetryAdviceDecision struct {
	Action       string  `json:"action"`
	Reason       string  `json:"reason"`
	StrategyHint *string `json:"strategy_hint"`
}

var RetryAdvisorFallback = RetryAdviceDecision{
	Action: "escalate_to_advisor",
	Reason: "Retry Advisor produced no parseable JSON after retries. " +
		"Defaulting to escalate_to_advisor so the deeper Issue Advisor can decide.",
	StrategyHint: nil,
}

type retryAdviceSchema struct{}

func (retryAdviceSchema) SafeParse(raw json.RawMessage) agentjson.Validation[RetryAdviceDecision] {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return agentjson.Validation[RetryAdviceDecision]{
			Issues: []agentjson.Issue{{Message: "Expected object"}},
		}
	}
	issues := exactKeys(object, []string{"action", "reason", "strategy_hint"}, nil)
	var out RetryAdviceDecision
	if value, ok := object["action"]; ok && json.Unmarshal(value, &out.Action) != nil {
		issues = append(issues, agentjson.Issue{Path: []string{"action"}, Message: "Expected string"})
	}
	if out.Action != "retry_with_hint" && out.Action != "escalate_to_advisor" {
		issues = append(issues, agentjson.Issue{Path: []string{"action"}, Message: "Invalid enum value"})
	}
	if value, ok := object["reason"]; ok && json.Unmarshal(value, &out.Reason) != nil {
		issues = append(issues, agentjson.Issue{Path: []string{"reason"}, Message: "Expected string"})
	}
	if utf16Length(out.Reason) < 1 || utf16Length(out.Reason) > 2000 {
		issues = append(issues, agentjson.Issue{Path: []string{"reason"}, Message: "String length out of range"})
	}
	if value, ok := object["strategy_hint"]; ok && !bytes.Equal(value, []byte("null")) {
		var hint string
		if json.Unmarshal(value, &hint) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{"strategy_hint"}, Message: "Expected string or null"})
		} else {
			out.StrategyHint = &hint
		}
	}
	return agentjson.Validation[RetryAdviceDecision]{Data: out, Issues: issues}
}

type RetryAdvisorInput struct {
	TaskID           string
	Workspace        string
	ParentSessionID  string
	OriginalSpec     string
	ReviewerFeedback string
	Attempt          int
}

func BuildRetryAdvisorPrompt(input RetryAdvisorInput, outputPath string) string {
	return strings.Join([]string{
		"# Retry Advisor — pick: hint or escalate",
		"",
		"Task ID: " + input.TaskID,
		"Repair attempt: " + strconvInt(input.Attempt+1),
		"",
		"## Leaf spec",
		"",
		input.OriginalSpec,
		"",
		"## Reviewer verdict (the rejection)",
		"",
		input.ReviewerFeedback,
		"",
		"## Your output",
		"",
		"Write a single JSON RetryAdviceDecision object to " + outputPath + " per the contract in your role.",
		"Pick exactly one of: retry_with_hint | escalate_to_advisor.",
		"For retry_with_hint, strategy_hint MUST be a concrete one-sentence directive (no 'try harder').",
		"For escalate_to_advisor, strategy_hint MUST be null.",
	}, "\n")
}

func (s *Service) dispatchRetryAdvisor(
	ctx context.Context,
	input RetryAdvisorInput,
) (RetryAdviceDecision, bool) {
	outputPath := filepath.Join(
		input.Workspace, ".codeaf", "agents", "retry-advisor",
		input.TaskID+".json",
	)
	prompt := BuildRetryAdvisorPrompt(input, outputPath)
	maxRetries := 1
	timeoutMS := int64(3 * 60_000)
	label := "retry-advisor"
	fallback := RetryAdvisorFallback
	result, err := agentjson.DispatchJSON(ctx, agentjson.Input[RetryAdviceDecision]{
		Agent: "retry-advisor", ParentSessionID: input.ParentSessionID,
		Workspace: input.Workspace, TaskPrompt: prompt, OutputPath: outputPath,
		Schema: retryAdviceSchema{}, Fallback: &fallback,
		MaxRetries: &maxRetries, TimeoutMS: &timeoutMS, Label: &label,
	}, s.agent)
	if err != nil {
		return RetryAdvisorFallback, false
	}
	return result.Data, err == nil && !result.UsedFallback
}

type IssueAdvisorSubtask struct {
	Title          string `json:"title"`
	DependsOnAbove *bool  `json:"depends_on_above"`
}

type IssueAdvisorDebt struct {
	Gap      string `json:"gap"`
	Severity string `json:"severity"`
}

type IssueAdvisorDecision struct {
	Action        string                `json:"action"`
	Reason        string                `json:"reason"`
	RelaxCriteria []string              `json:"relax_criteria"`
	StrategyHint  *string               `json:"strategy_hint"`
	Subtasks      []IssueAdvisorSubtask `json:"subtasks"`
	Debt          []IssueAdvisorDebt    `json:"debt"`
	Blocker       *string               `json:"blocker"`
}

var IssueAdvisorFallback = IssueAdvisorDecision{
	Action: "escalate_to_replan",
	Reason: "Issue Advisor produced no parseable JSON after retries. " +
		"Defaulting to escalate_to_replan so the build-level replanner can decide the next move.",
	RelaxCriteria: nil,
	StrategyHint:  nil,
	Subtasks:      nil,
	Debt:          nil,
	Blocker: stringPointer(
		"advisor failed to emit a valid decision; treat as unresolvable at the leaf level and consider broader replanning",
	),
}

type issueAdvisorSchema struct{}

func (issueAdvisorSchema) SafeParse(raw json.RawMessage) agentjson.Validation[IssueAdvisorDecision] {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return agentjson.Validation[IssueAdvisorDecision]{
			Issues: []agentjson.Issue{{Message: "Expected object"}},
		}
	}
	keys := []string{
		"action", "reason", "relax_criteria", "strategy_hint",
		"subtasks", "debt", "blocker",
	}
	issues := exactKeys(object, keys, nil)
	var out IssueAdvisorDecision
	readString := func(key string, target *string) {
		if value, ok := object[key]; ok && json.Unmarshal(value, target) != nil {
			issues = append(issues, agentjson.Issue{Path: []string{key}, Message: "Expected string"})
		}
	}
	readString("action", &out.Action)
	readString("reason", &out.Reason)
	switch out.Action {
	case "retry_modified", "retry_approach", "split", "accept_with_debt", "escalate_to_replan":
	default:
		issues = append(issues, agentjson.Issue{Path: []string{"action"}, Message: "Invalid enum value"})
	}
	if utf16Length(out.Reason) < 1 || utf16Length(out.Reason) > 2000 {
		issues = append(issues, agentjson.Issue{Path: []string{"reason"}, Message: "String length out of range"})
	}
	parseNullableString(object, "strategy_hint", &out.StrategyHint, &issues)
	parseNullableString(object, "blocker", &out.Blocker, &issues)
	parseNullableStrings(object, "relax_criteria", &out.RelaxCriteria, &issues)
	parseNullableSubtasks(object, &out.Subtasks, &issues)
	parseNullableDebt(object, &out.Debt, &issues)
	return agentjson.Validation[IssueAdvisorDecision]{Data: out, Issues: issues}
}

func parseNullableString(
	object map[string]json.RawMessage,
	key string,
	target **string,
	issues *[]agentjson.Issue,
) {
	value, ok := object[key]
	if !ok || bytes.Equal(value, []byte("null")) {
		return
	}
	var text string
	if json.Unmarshal(value, &text) != nil {
		*issues = append(*issues, agentjson.Issue{Path: []string{key}, Message: "Expected string or null"})
		return
	}
	*target = &text
}

func parseNullableStrings(
	object map[string]json.RawMessage,
	key string,
	target *[]string,
	issues *[]agentjson.Issue,
) {
	value, ok := object[key]
	if !ok || bytes.Equal(value, []byte("null")) {
		return
	}
	var values []json.RawMessage
	if json.Unmarshal(value, &values) != nil {
		*issues = append(*issues, agentjson.Issue{Path: []string{key}, Message: "Expected array or null"})
		return
	}
	*target = make([]string, 0, len(values))
	for index, item := range values {
		var text string
		if json.Unmarshal(item, &text) != nil {
			*issues = append(*issues, agentjson.Issue{
				Path:    []string{key, jscompat.FormatNumber(float64(index))},
				Message: "Expected string",
			})
			continue
		}
		*target = append(*target, text)
	}
}

func parseNullableSubtasks(
	object map[string]json.RawMessage,
	target *[]IssueAdvisorSubtask,
	issues *[]agentjson.Issue,
) {
	value, ok := object["subtasks"]
	if !ok || bytes.Equal(value, []byte("null")) {
		return
	}
	var values []json.RawMessage
	if json.Unmarshal(value, &values) != nil {
		*issues = append(*issues, agentjson.Issue{Path: []string{"subtasks"}, Message: "Expected array or null"})
		return
	}
	*target = make([]IssueAdvisorSubtask, 0, len(values))
	for index, item := range values {
		base := []string{"subtasks", jscompat.FormatNumber(float64(index))}
		var row map[string]json.RawMessage
		if json.Unmarshal(item, &row) != nil || row == nil {
			*issues = append(*issues, agentjson.Issue{Path: base, Message: "Expected object"})
			continue
		}
		var subtask IssueAdvisorSubtask
		if value, ok := row["title"]; !ok || json.Unmarshal(value, &subtask.Title) != nil || utf16Length(subtask.Title) < 1 {
			*issues = append(*issues, agentjson.Issue{Path: appendPath(base, "title"), Message: "Expected nonempty string"})
		}
		if value, ok := row["depends_on_above"]; ok && !bytes.Equal(value, []byte("null")) {
			var dependency bool
			if json.Unmarshal(value, &dependency) != nil {
				*issues = append(*issues, agentjson.Issue{Path: appendPath(base, "depends_on_above"), Message: "Expected boolean or null"})
			} else {
				subtask.DependsOnAbove = &dependency
			}
		} else if !ok {
			*issues = append(*issues, agentjson.Issue{Path: appendPath(base, "depends_on_above"), Message: "Required"})
		}
		*target = append(*target, subtask)
	}
}

func parseNullableDebt(
	object map[string]json.RawMessage,
	target *[]IssueAdvisorDebt,
	issues *[]agentjson.Issue,
) {
	value, ok := object["debt"]
	if !ok || bytes.Equal(value, []byte("null")) {
		return
	}
	var values []json.RawMessage
	if json.Unmarshal(value, &values) != nil {
		*issues = append(*issues, agentjson.Issue{Path: []string{"debt"}, Message: "Expected array or null"})
		return
	}
	*target = make([]IssueAdvisorDebt, 0, len(values))
	for index, item := range values {
		base := []string{"debt", jscompat.FormatNumber(float64(index))}
		var row map[string]json.RawMessage
		if json.Unmarshal(item, &row) != nil || row == nil {
			*issues = append(*issues, agentjson.Issue{Path: base, Message: "Expected object"})
			continue
		}
		var debt IssueAdvisorDebt
		if value, ok := row["gap"]; !ok || json.Unmarshal(value, &debt.Gap) != nil || utf16Length(debt.Gap) < 1 {
			*issues = append(*issues, agentjson.Issue{Path: appendPath(base, "gap"), Message: "Expected nonempty string"})
		}
		if value, ok := row["severity"]; !ok || json.Unmarshal(value, &debt.Severity) != nil {
			*issues = append(*issues, agentjson.Issue{Path: appendPath(base, "severity"), Message: "Expected string"})
		}
		if debt.Severity != "low" && debt.Severity != "medium" && debt.Severity != "high" {
			*issues = append(*issues, agentjson.Issue{Path: appendPath(base, "severity"), Message: "Invalid enum value"})
		}
		*target = append(*target, debt)
	}
}

type IssueAdvisorInput struct {
	TaskID           string
	Workspace        string
	ParentSessionID  string
	OriginalSpec     string
	ReviewerFeedback string
	EscalationReason string
	ImplBranch       string
	RepairsRun       int
}

func BuildIssueAdvisorPrompt(input IssueAdvisorInput, outputPath string) string {
	return strings.Join([]string{
		"# Failed leaf — choose an escalation action",
		"",
		"Task ID: " + input.TaskID,
		"Branch: " + input.ImplBranch,
		"Workspace: " + input.Workspace,
		"Repair attempts already run: " + strconvInt(input.RepairsRun),
		"Escalation reason: " + input.EscalationReason,
		"",
		"## Original spec for this leaf",
		"",
		input.OriginalSpec,
		"",
		"## Most recent reviewer verdict",
		"",
		input.ReviewerFeedback,
		"",
		"## Your output",
		"",
		"Write a single JSON object to " + outputPath + " per the IssueAdvisorDecision contract in your role.",
		"Pick exactly one of: retry_modified | retry_approach | split | accept_with_debt | escalate_to_replan.",
		"For fields not relevant to your action, emit `null` (do not omit the key).",
		"",
		"Use `read` / `grep` on the worktree if you need to see the actual code the fixer produced before deciding.",
	}, "\n")
}

func (s *Service) dispatchIssueAdvisor(
	ctx context.Context,
	input IssueAdvisorInput,
) (IssueAdvisorDecision, bool) {
	outputPath := filepath.Join(
		input.Workspace, ".codeaf", "agents", "issue-advisor",
		input.TaskID+".json",
	)
	prompt := BuildIssueAdvisorPrompt(input, outputPath)
	maxRetries := 1
	label := "issue-advisor"
	fallback := IssueAdvisorFallback
	result, err := agentjson.DispatchJSON(ctx, agentjson.Input[IssueAdvisorDecision]{
		Agent: "issue-advisor", ParentSessionID: input.ParentSessionID,
		Workspace: input.Workspace, TaskPrompt: prompt, OutputPath: outputPath,
		Schema: issueAdvisorSchema{}, Fallback: &fallback,
		MaxRetries: &maxRetries, Label: &label,
	}, s.agent)
	if err != nil {
		return IssueAdvisorFallback, false
	}
	return result.Data, err == nil && !result.UsedFallback
}

func stringPointer(value string) *string { return &value }
