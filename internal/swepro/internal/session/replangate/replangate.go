package replangate

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
)

const DefaultMaxReplans = 4

// DispatchReplannerInput mirrors replan-gate.ts:237-248.
type DispatchReplannerInput struct {
	Workspace        string
	DBPath           string
	ParentSessionID  string
	PromptOps        any
	UserGoal         string
	EscalatedTaskIDs []string
	SessionKey       *string
}

// ApplyReplanResult mirrors replan-gate.ts:343-350.
type ApplyReplanResult struct {
	Abort      bool   `json:"abort"`
	Summary    string `json:"summary"`
	OpsApplied int    `json:"opsApplied"`
}

// PlanDBRunner is the side-effect boundary for CLI bridge calls.
type PlanDBRunner interface {
	Run(args []string) (plandb.RunResult, error)
}

type PlanDBRunnerFunc func(args []string) (plandb.RunResult, error)

func (function PlanDBRunnerFunc) Run(args []string) (plandb.RunResult, error) {
	return function(args)
}

type nativePlanDBRunner struct{}

func (nativePlanDBRunner) Run(args []string) (plandb.RunResult, error) {
	return plandb.RunPlanDB(append([]string{"plandb"}, args...)), nil
}

// Dependencies are the agent dispatcher, PlanDB bridge, and pinned clock.
type Dependencies struct {
	AgentJSON agentjson.Dependencies
	PlanDB    PlanDBRunner
	NowMillis func() int64
	LookupEnv func(string) (string, bool)
}

type PlanDBSnapshot struct {
	TaskList string `json:"taskList"`
	Blockers string `json:"blockers"`
	Debt     string `json:"debt"`
	Frozen   string `json:"frozen"`
}

type ReplanHistoryEntry struct {
	Timestamp    jscompat.JSNumber `json:"timestamp"`
	Action       string            `json:"action"`
	Reason       string            `json:"reason"`
	OpsApplied   *int              `json:"opsApplied,omitempty"`
	DropsApplied *int              `json:"dropsApplied,omitempty"`
}

// CapturePlanDBSnapshot renders the bounded prompt snapshot.
func CapturePlanDBSnapshot(
	workspace, dbPath string,
	escalatedTaskIDs []string,
	runner PlanDBRunner,
) PlanDBSnapshot {
	_ = workspace
	_ = dbPath
	if runner == nil {
		runner = nativePlanDBRunner{}
	}
	taskList := "(no plandb data)"
	if result, err := runner.Run([]string{"status", "--detail"}); err == nil {
		taskList = sliceUTF16(string(result.Stdout), 8000)
	} else {
		taskList = "(plandb status failed: " + sliceUTF16(err.Error(), 120) + ")"
	}
	blockerLines := []string{}
	for _, taskID := range escalatedTaskIDs {
		result, err := runner.Run([]string{
			"contexts", "--task", taskID, "--kind", "blocker",
		})
		if err != nil {
			continue
		}
		text := string(result.Stdout)
		if jscompat.Trim(text) != "" {
			blockerLines = append(blockerLines,
				"Task "+taskID+":", sliceUTF16(text, 2000))
		}
	}
	blockers := "(no blocker entries found)"
	if len(blockerLines) > 0 {
		blockers = strings.Join(blockerLines, "\n")
	}
	debt := contextSnapshot(
		runner, []string{"contexts", "--kind", "debt"},
		"(no debt entries)", 4000,
	)
	frozen := contextSnapshot(
		runner, []string{"contexts", "--kind", "frozen"},
		"(no frozen leaves)", 4000,
	)
	return PlanDBSnapshot{
		TaskList: taskList, Blockers: blockers, Debt: debt, Frozen: frozen,
	}
}

func contextSnapshot(
	runner PlanDBRunner, args []string, fallback string, maximum int,
) string {
	result, err := runner.Run(args)
	if err != nil {
		return fallback
	}
	text := string(result.Stdout)
	if jscompat.Trim(text) == "" {
		return fallback
	}
	return sliceUTF16(text, maximum)
}

// BuildReplannerPrompt ports replan-gate.ts:251-306.
func BuildReplannerPrompt(
	input DispatchReplannerInput,
	snapshot PlanDBSnapshot,
	history []ReplanHistoryEntry,
	outputPath string,
) string {
	historyBlock := "  (no prior replans this session)"
	if len(history) > 0 {
		start := 0
		if len(history) > 5 {
			start = len(history) - 5
		}
		lines := make([]string, 0, len(history)-start)
		for index, entry := range history[start:] {
			lines = append(lines,
				"  "+intString(index+1)+". action="+entry.Action+
					", reason="+sliceUTF16(entry.Reason, 200))
		}
		historyBlock = strings.Join(lines, "\n")
	}
	escalated := strings.Join(input.EscalatedTaskIDs, ", ")
	if escalated == "" {
		escalated = "(none)"
	}
	return strings.Join([]string{
		"# Replanner — choose next-step action for stalled build",
		"",
		"Workspace: " + input.Workspace,
		"Escalated task IDs: " + escalated,
		"",
		"## Original user goal",
		"",
		input.UserGoal,
		"",
		"## Current plandb state",
		"",
		"```",
		snapshot.TaskList,
		"```",
		"",
		"## Blockers on escalated tasks",
		"",
		snapshot.Blockers,
		"",
		"## Accumulated debt across the build",
		"",
		snapshot.Debt,
		"",
		"## Frozen leaves — DO NOT re-target",
		"",
		snapshot.Frozen,
		"",
		"## Prior replan history this session",
		"",
		historyBlock,
		"",
		"## Your output",
		"",
		"Write a single JSON ReplanDecision object to " + outputPath + " per the contract in your role.",
		"Pick exactly one of: continue | modify_dag | reduce_scope | abort.",
		"For fields not relevant to your action, emit `null` (do not omit the key).",
		"",
		"Hard constraints:",
		"- Do NOT repeat strategies that already appear in the prior replan history with action=modify_dag if they didn't unblock the build.",
		"- Do NOT include any task ID from the \"Frozen leaves\" section in your modify_dag ops (no cancel, no retry, no replan_subtree, no add-as-replacement). Frozen leaves are contract-complete and merged; rework would regress shipped code.",
	}, "\n")
}

// DispatchReplanner snapshots state and invokes the baked replanner through
// agentjson.
func DispatchReplanner(
	ctx context.Context,
	input DispatchReplannerInput,
	deps Dependencies,
) (agentjson.Result[ReplanDecision], error) {
	sessionKey := ""
	if input.SessionKey != nil {
		sessionKey = *input.SessionKey
	} else {
		now := time.Now().UnixMilli()
		if deps.NowMillis != nil {
			now = deps.NowMillis()
		}
		sessionKey = "replan-" + jscompat.FormatNumber(float64(now))
	}
	outputPath := filepath.Join(
		input.Workspace, ".codeaf", "agents", "replanner", sessionKey+".json",
	)
	runner := deps.PlanDB
	if runner == nil {
		runner = nativePlanDBRunner{}
	}
	snapshot := CapturePlanDBSnapshot(
		input.Workspace, input.DBPath, input.EscalatedTaskIDs, runner,
	)
	history, err := readHistory(input.Workspace)
	if err != nil {
		history = []json.RawMessage{}
	}
	typedHistory := make([]ReplanHistoryEntry, 0, len(history))
	for _, raw := range history {
		var entry ReplanHistoryEntry
		if json.Unmarshal(raw, &entry) != nil {
			return agentjson.Result[ReplanDecision]{}, errors.New("replan history entry is not readable")
		}
		typedHistory = append(typedHistory, entry)
	}
	maxRetries := 1
	timeout := int64(10 * 60_000)
	label := "replanner"
	fallback := ReplanFallback
	return agentjson.DispatchJSON(ctx, agentjson.Input[ReplanDecision]{
		Agent: "replanner", ParentSessionID: input.ParentSessionID,
		Workspace:  input.Workspace,
		TaskPrompt: BuildReplannerPrompt(input, snapshot, typedHistory, outputPath),
		OutputPath: outputPath, Schema: DecisionSchema{}, Fallback: &fallback,
		MaxRetries: &maxRetries, TimeoutMS: &timeout, Label: &label,
	}, deps.AgentJSON)
}

// ApplyReplanDecision mutates PlanDB and appends durable history.
func ApplyReplanDecision(
	decision ReplanDecision,
	workspace, dbPath string,
	deps Dependencies,
) ApplyReplanResult {
	_ = dbPath
	runner := deps.PlanDB
	if runner == nil {
		runner = nativePlanDBRunner{}
	}
	opsApplied := 0
	switch decision.Action {
	case "modify_dag":
		for _, operation := range decision.Ops {
			switch value := operation.(type) {
			case AddOp:
				args := []string{"add", value.Title, "--kind", value.Kind}
				for _, dependency := range value.Deps {
					args = append(args, "--dep", dependency+":feeds_into")
				}
				if _, err := runner.Run(args); err == nil {
					opsApplied++
				}
			case CancelOp:
				if _, err := runner.Run([]string{"task", "cancel", value.ID}); err == nil {
					opsApplied++
				}
			case AmendOp:
				if _, err := runner.Run([]string{
					"task", "amend", value.ID, "--prepend", value.Prepend,
				}); err == nil {
					opsApplied++
				}
			case SplitOp:
				specs := make([]plandb.SplitSpec, 0, len(value.Parts))
				for _, part := range value.Parts {
					description := part.Description
					if len(part.FileScope) > 0 {
						description = "file_scope: " +
							strings.Join(part.FileScope, ", ") + "\n" + description
					}
					specs = append(specs, plandb.SplitSpec{
						Title: part.Title, Description: description,
					})
				}
				if _, err := plandb.GetPlanDB().SplitTaskWithSpecs(value.ID, specs); err == nil {
					opsApplied++
				}
			}
		}
	case "reduce_scope":
		for _, id := range decision.DropIDs {
			if _, err := runner.Run([]string{"task", "cancel", id}); err == nil {
				opsApplied++
			}
		}
	}

	now := time.Now().UnixMilli()
	if deps.NowMillis != nil {
		now = deps.NowMillis()
	}
	entry := ReplanHistoryEntry{
		Timestamp: jscompat.JSNumber(now), Action: decision.Action, Reason: decision.Reason,
	}
	if decision.Action == "modify_dag" {
		value := opsApplied
		entry.OpsApplied = &value
	}
	if decision.Action == "reduce_scope" {
		value := opsApplied
		entry.DropsApplied = &value
	}
	appendHistory(workspace, entry)

	summary := ""
	if decision.Action == "abort" {
		summary = decision.Reason
		if decision.AbortSummary != nil {
			summary = *decision.AbortSummary
		}
	} else {
		summary = "replanner:" + decision.Action + " — " +
			sliceUTF16(decision.Reason, 200)
		if opsApplied > 0 {
			summary += " (" + intString(opsApplied) + " ops applied)"
		}
	}
	return ApplyReplanResult{
		Abort: decision.Action == "abort", Summary: summary, OpsApplied: opsApplied,
	}
}

// ShouldReplan enforces MAX_REPLANS against the durable history length.
func ShouldReplan(workspace string, lookup func(string) (string, bool)) bool {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	rawCap, exists := lookup("MAX_REPLANS")
	if !exists {
		rawCap = jscompat.FormatNumber(float64(DefaultMaxReplans))
	}
	capacity := jscompat.ToNumber(rawCap)
	if !isFinite(capacity) || capacity < 1 {
		return false
	}
	history, _ := readHistory(workspace)
	return float64(len(history)) < capacity
}

func readHistory(workspace string) ([]json.RawMessage, error) {
	raw, err := os.ReadFile(filepath.Join(workspace, ".codeaf", "replan-history.json"))
	if err != nil {
		return []json.RawMessage{}, err
	}
	var history []json.RawMessage
	if json.Unmarshal(raw, &history) != nil {
		return []json.RawMessage{}, errors.New("invalid history")
	}
	return history, nil
}

func appendHistory(workspace string, entry ReplanHistoryEntry) {
	filePath := filepath.Join(workspace, ".codeaf", "replan-history.json")
	history, _ := readHistory(workspace)
	encodedEntry, err := jscompat.Stringify(entry)
	if err != nil {
		return
	}
	history = append(history, encodedEntry)
	encoded, err := jscompat.StringifyIndent(history)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(filePath), 0o777) != nil {
		return
	}
	_ = os.WriteFile(filePath, encoded, 0o666)
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func sliceUTF16(value string, maximum int) string {
	units := utf16.Encode([]rune(value))
	if len(units) > maximum {
		units = units[:maximum]
	}
	return string(utf16.Decode(units))
}

func intString(value int) string { return jscompat.FormatNumber(float64(value)) }
