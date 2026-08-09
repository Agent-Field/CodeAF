// This file ports swe-pro/src/tool/task.ts:22-193,425-623,739-802 at commit
// 3b25a1a. Session creation and prompting are deliberately represented by the
// narrow TaskSessionSpawner interface (the steploop integration seam).
package tool

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/observer"
)

const RootIssueExcerptLimit = 2000

var (
	taskWriteRE    = regexp.MustCompile(`\b(edit|write|modify|implement|fix|patch|change|create|delete|refactor|update)\b`)
	taskReadRE     = regexp.MustCompile(`\b(read-only|read only|research|explore|inspect|audit|review|search|find|analy[sz]e)\b`)
	taskSecurityRE = regexp.MustCompile(`\b(security|vulnerability|threat|secret|auth|permission)\b`)
	taskQARe       = regexp.MustCompile(`\b(qa|test|verify|verification|regression|e2e|smoke)\b`)
	taskReviewRE   = regexp.MustCompile(`\b(review|critique|audit)\b`)
	taskArchRE     = regexp.MustCompile(`\b(architecture|architect|design|rfc|plan)\b`)
	taskReleaseRE  = regexp.MustCompile(`\b(release|changelog|migration note)\b`)
	taskProbeRE    = regexp.MustCompile(`\b(probe|scope|map|locate|find files?)\b`)
	taskPolicyRE   = regexp.MustCompile(`(?i)^(access|parallel|worktree|file_scope|task_role|context_inputs|outputs|agent|acceptance|session_id|message_id|parent_session_id|subagent_type):`)
	projectIDRE    = regexp.MustCompile(`(?i)Project:\s+(p-[a-z0-9-]+)`)
	rootTaskIDRE   = regexp.MustCompile(`(?i)Root task:\s+(t-[a-z0-9-]+)`)
	assignedIDRE   = regexp.MustCompile(`(?i)Assigned PlanDB task:\s+(t-[a-z0-9-]+)`)
	databaseRE     = regexp.MustCompile(`Database:\s+(.+)`)
)

// TaskParams is the task tool's model-visible input.
type TaskParams struct {
	Description     string   `json:"description"`
	Prompt          string   `json:"prompt"`
	SubagentType    string   `json:"subagent_type"`
	TaskID          string   `json:"task_id,omitempty"`
	Command         string   `json:"command,omitempty"`
	Category        string   `json:"category,omitempty"`
	LoadSkills      []string `json:"load_skills,omitempty"`
	MCPs            []string `json:"mcps,omitempty"`
	RunInBackground bool     `json:"run_in_background,omitempty"`
}

// TaskPlanDBInfo is the coordinate set recovered from a typed context or a
// legacy reminder.
type TaskPlanDBInfo struct {
	ProjectID      string `json:"projectID"`
	RootTaskID     string `json:"rootTaskID"`
	AssignedTaskID string `json:"assignedTaskID,omitempty"`
	DBPath         string `json:"dbPath"`
}

// TaskContextItem is one durable PlanDB context row projected into a reminder.
type TaskContextItem struct {
	Kind    string `json:"kind,omitempty"`
	TaskID  string `json:"task_id,omitempty"`
	Content any    `json:"content,omitempty"`
}

// PickTaskTier gives category precedence over subagent type and otherwise
// defaults to high.
func PickTaskTier(category, subagent string) baked.Tier {
	if category != "" {
		for _, assignment := range baked.CategoryTierAssignments() {
			if assignment.Name == category {
				return assignment.Tier
			}
		}
	}
	return baked.TierFor(subagent, baked.TierHigh)
}

// CompactTaskText is task.ts compact(), including UTF-16 length/slice rules.
func CompactTaskText(text string, limit int) string {
	units := utf16.Encode([]rune(text))
	if len(units) <= limit {
		return text
	}
	end := limit - 3
	if end < 0 {
		end = max(0, len(units)+end)
	}
	if end > len(units) {
		end = len(units)
	}
	return string(utf16.Decode(units[:end])) + "..."
}

func taskSearchText(params TaskParams) string {
	return strings.ToLower(params.SubagentType + " " + params.Description + " " + params.Prompt)
}

// InferTaskAccess reproduces the write-vs-read keyword heuristic.
func InferTaskAccess(params TaskParams) string {
	text := taskSearchText(params)
	if taskWriteRE.MatchString(text) && !taskReadRE.MatchString(text) {
		return "write"
	}
	return "read"
}

// InferTaskRole reproduces the ordered role heuristic.
func InferTaskRole(params TaskParams, access string) string {
	text := taskSearchText(params)
	switch {
	case taskSecurityRE.MatchString(text):
		return "security"
	case taskQARe.MatchString(text):
		return "qa"
	case taskReviewRE.MatchString(text):
		return "review"
	case taskArchRE.MatchString(text):
		return "architecture"
	case taskReleaseRE.MatchString(text):
		return "release"
	case access == "write":
		return "implementation"
	case taskProbeRE.MatchString(text):
		return "probe"
	default:
		return "research"
	}
}

// InferTaskOutputs reproduces the output-contract heuristic.
func InferTaskOutputs(params TaskParams, access, role string) []string {
	text := taskSearchText(params)
	switch {
	case role == "security":
		return []string{"risk_report", "handoff"}
	case role == "qa" || taskQARe.MatchString(text):
		return []string{"test_report", "handoff"}
	case role == "review":
		return []string{"review_report", "handoff"}
	case role == "architecture":
		return []string{"decision", "handoff"}
	case access == "write":
		return []string{"patch", "handoff"}
	default:
		return []string{"findings", "handoff"}
	}
}

// ParseTaskPlanDBInfo extracts the first coordinates from reminder text.
func ParseTaskPlanDBInfo(text string) *TaskPlanDBInfo {
	project := regexpFirst(projectIDRE, text)
	root := regexpFirst(rootTaskIDRE, text)
	if project == "" || root == "" {
		return nil
	}
	return &TaskPlanDBInfo{
		ProjectID:      project,
		RootTaskID:     root,
		AssignedTaskID: regexpFirst(assignedIDRE, text),
		DBPath:         strings.TrimSpace(regexpFirst(databaseRE, text)),
	}
}

func regexpFirst(re *regexp.Regexp, text string) string {
	match := re.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

// StripTaskPolicyLines removes scheduler policy lines while preserving the
// remaining line layout and compacting 3+ blank lines to two.
func StripTaskPolicyLines(description string) string {
	if description == "" {
		return ""
	}
	lines := strings.Split(description, "\n")
	out := lines[:0]
	for _, line := range lines {
		if !taskPolicyRE.MatchString(strings.TrimSpace(line)) {
			out = append(out, line)
		}
	}
	value := strings.Join(out, "\n")
	blankRuns := regexp.MustCompile(`\n{3,}`)
	return strings.TrimSpace(blankRuns.ReplaceAllString(value, "\n\n"))
}

// TaskContextLines projects the last eight relevant contexts.
func TaskContextLines(items []TaskContextItem, taskIDs []string) []string {
	wanted := map[string]bool{}
	for _, id := range taskIDs {
		if id != "" {
			wanted[id] = true
		}
	}
	filtered := make([]TaskContextItem, 0, len(items))
	for _, item := range items {
		if item.TaskID == "" || wanted[item.TaskID] {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) > 8 {
		filtered = filtered[len(filtered)-8:]
	}
	out := make([]string, 0, len(filtered))
	for _, item := range filtered {
		kind := item.Kind
		if kind == "" {
			kind = "context"
		}
		id := ""
		if item.TaskID != "" {
			id = " " + item.TaskID
		}
		out = append(out, fmt.Sprintf("- [%s%s] %s", kind, id, CompactTaskText(jsString(item.Content), 700)))
	}
	return out
}

func jsString(value any) string {
	if value == nil {
		return ""
	}
	switch value := value.(type) {
	case string:
		return value
	case bool:
		if value {
			return "true"
		}
		return "false"
	default:
		if number, ok := value.(float64); ok {
			return jscompat.FormatNumber(number)
		}
		return fmt.Sprint(value)
	}
}

// IssueFilePointerLines is the shared compact issue-file contract.
func IssueFilePointerLines(issueFile string) []string {
	return []string{
		"Full spec: " + issueFile,
		"Read this file FIRST — it is your source of truth for interface contracts, sibling-module dependencies, verbatim error strings, and testable acceptance criteria; do not infer requirements not present in it.",
		"Before declaring done, self-check every `## Acceptance criteria` bullet with per-bullet evidence — follow the Acceptance_Verification rule in your system prompt.",
	}
}

// BuildTaskPackageDescription assembles the PlanDB child package description.
func BuildTaskPackageDescription(params TaskParams, parentSessionID, sessionID, agentID, rootIssueExcerpt string) string {
	access := InferTaskAccess(params)
	role := InferTaskRole(params, access)
	outputs := InferTaskOutputs(params, access, role)
	lines := []string{
		harnessSubagentTaskDescription,
		"",
		"parent_session_id: " + parentSessionID,
		"session_id: " + sessionID,
		"subagent_type: " + params.SubagentType,
		"task_role: " + role,
		"access: " + access,
		map[bool]string{true: "parallel: safe", false: "parallel: serial"}[access == "read"],
		"worktree: none",
		"file_scope: unknown until probe",
		"agent: " + agentID,
		"context_inputs: parent,deps,file_scope",
		"outputs: " + strings.Join(outputs, ","),
		"acceptance: subagent returns compact findings or completed work and verification notes",
		"",
	}
	if rootIssueExcerpt != "" {
		lines = append(lines, "Root issue context:", rootIssueExcerpt, "")
	}
	lines = append(lines, "Delegated prompt:", params.Prompt)
	return strings.Join(lines, "\n")
}

// BuildTaskChildPrompt appends the issue-file pointer only for a known file.
func BuildTaskChildPrompt(prompt, issueFile string) string {
	if issueFile == "" {
		return prompt
	}
	return strings.Join(append([]string{prompt, ""}, IssueFilePointerLines(issueFile)...), "\n")
}

// TaskReminderInput contains the variable reminder coordinates.
type TaskReminderInput struct {
	ProjectID        string   `json:"projectID"`
	RootTaskID       string   `json:"rootTaskID"`
	ParentTaskID     string   `json:"parentTaskID,omitempty"`
	AssignedTaskID   string   `json:"assignedTaskID"`
	InheritedContext []string `json:"inheritedContext"`
}

// BuildTaskPlanReminder builds the synthetic system-reminder text.
func BuildTaskPlanReminder(input TaskReminderInput) string {
	parent := input.ParentTaskID
	if parent == "" {
		parent = input.RootTaskID
	}
	lines := []string{
		"<system-reminder>",
		"PlanDB has assigned this subagent a task created by the harness.",
		"Project: " + input.ProjectID,
		"Root task: " + input.RootTaskID,
		"Parent PlanDB task: " + parent,
		"Assigned PlanDB task: " + input.AssignedTaskID,
		"PlanDB usage within your assigned task:",
		fmt.Sprintf("- plandb add: create child packages under %s for substantial sub-work you discover. Children must declare access/parallel/file_scope/task_role.", input.AssignedTaskID),
		"- plandb context: record durable discoveries (kind: discovery|decision|constraint|blocker). Auto-links to your assigned task.",
		fmt.Sprintf("- plandb amend %s --prepend: append notes/findings to your own task description.", input.AssignedTaskID),
		"- plandb amend <future-task-id> --prepend: when you learn something a downstream task needs, amend that task with the note.",
		"- plandb split <task-id> --into 'A, B, C': split a task you own that turned out larger than scoped.",
		"- plandb insert --after <id> --before <id> --title '...': insert a missed step between two existing tasks.",
		"- task tool: spawn nested subagents; the harness creates a child PlanDB package under your assigned task.",
		"Do not use bash to run PlanDB commands; use the plandb tool.",
		"Do not micro-fragment: one PlanDB task per coherent unit of work, not one per tool call.",
	}
	if len(input.InheritedContext) > 0 {
		lines = append(lines, "Relevant PlanDB context:")
		lines = append(lines, input.InheritedContext...)
	} else {
		lines = append(lines, "Relevant PlanDB context: none recorded yet.")
	}
	lines = append(lines,
		"Return a compact final result; the harness will also record it on the assigned PlanDB task.",
		"</system-reminder>",
	)
	return strings.Join(lines, "\n")
}

// BuildTaskResultOutput is the model-visible task tool result.
func BuildTaskResultOutput(sessionID, assignedPlanDBTaskID, resultText string) string {
	lines := []string{"task_id: " + sessionID + " (for resuming to continue this task if needed)"}
	if assignedPlanDBTaskID != "" {
		lines = append(lines, "plandb_task_id: "+assignedPlanDBTaskID)
	}
	lines = append(lines, "", "<task_result>", resultText, "</task_result>")
	return strings.Join(lines, "\n")
}

// TaskSpawnRequest is the sub-session payload owned by the steploop adapter.
type TaskSpawnRequest struct {
	SessionID          string
	ParentSessionID    string
	SubagentType       string
	Description        string
	PackageDescription string
	Prompt             string
	Tier               baked.Tier
	PlanReminder       string
	AssignedPlanDBTask string
}

// TaskSpawnResult is the narrow result needed by task output assembly.
type TaskSpawnResult struct {
	SessionID  string
	ResultText string
}

// TaskSessionSpawner owns actual child-session creation and prompting.
type TaskSessionSpawner interface {
	SpawnTask(context.Context, TaskSpawnRequest) (TaskSpawnResult, error)
}

// TaskDispatchInput supplies the already-resolved session/PlanDB envelope.
type TaskDispatchInput struct {
	Params             TaskParams
	ParentSessionID    string
	ChildSessionID     string
	AssignedPlanDBTask string
	ParentPlanDBTask   string
	RootTaskID         string
	ProjectID          string
	IssueFile          string
	RootIssueExcerpt   string
	AgentID            string
	InheritedContext   []string
	Workspace          string
}

// TaskDispatcher provides call-ID dedup and delegates the actual sub-session
// spawn through TaskSessionSpawner.
type TaskDispatcher struct {
	Spawner  TaskSessionSpawner
	Observer observer.Tracker
	Now      func() time.Time
	mu       sync.Mutex
	claimed  map[string]bool
}

// Dispatch performs the assembly and spawn portion of task.ts.
func (d *TaskDispatcher) Dispatch(ctx context.Context, callID string, input TaskDispatchInput) (string, error) {
	if callID != "" {
		d.mu.Lock()
		if d.claimed == nil {
			d.claimed = map[string]bool{}
		}
		if d.claimed[callID] {
			d.mu.Unlock()
			return "duplicate task tool_use elided (callID=" + callID + ")", nil
		}
		d.claimed[callID] = true
		d.mu.Unlock()
	}
	if d.Spawner == nil {
		return "", fmt.Errorf("TaskTool requires a session spawner")
	}
	agentID := input.AgentID
	if agentID == "" {
		agentID = input.Params.SubagentType + ":" + input.ChildSessionID
	}
	reminder := ""
	if input.AssignedPlanDBTask != "" {
		reminder = BuildTaskPlanReminder(TaskReminderInput{
			ProjectID:        input.ProjectID,
			RootTaskID:       input.RootTaskID,
			ParentTaskID:     input.ParentPlanDBTask,
			AssignedTaskID:   input.AssignedPlanDBTask,
			InheritedContext: input.InheritedContext,
		})
	}
	tracked := false
	if d.Observer != nil && observableTaskAgent(input.Params.SubagentType) {
		now := time.Now()
		if d.Now != nil {
			now = d.Now()
		}
		// TS task.ts:625-650 tracks only the four long-running write roles
		// immediately before their prompt; :666-669 untracks after dispatch.
		d.Observer.Track(observer.ObservedSession{
			SessionID: input.ChildSessionID, AgentRole: input.Params.SubagentType,
			TaskSummary: sliceTaskUTF16(input.Params.Description, 200),
			StartedAt:   float64(now.UnixMilli()), Workspace: input.Workspace,
			ParentSessionID: input.ParentSessionID,
		})
		tracked = true
	}
	result, err := d.Spawner.SpawnTask(ctx, TaskSpawnRequest{
		SessionID:       input.ChildSessionID,
		ParentSessionID: input.ParentSessionID,
		SubagentType:    input.Params.SubagentType,
		Description:     input.Params.Description,
		PackageDescription: BuildTaskPackageDescription(
			input.Params,
			input.ParentSessionID,
			input.ChildSessionID,
			agentID,
			input.RootIssueExcerpt,
		),
		Prompt:             BuildTaskChildPrompt(input.Params.Prompt, input.IssueFile),
		Tier:               PickTaskTier(input.Params.Category, input.Params.SubagentType),
		PlanReminder:       reminder,
		AssignedPlanDBTask: input.AssignedPlanDBTask,
	})
	if tracked {
		d.Observer.Untrack(input.ChildSessionID)
	}
	if err != nil {
		return "", err
	}
	return BuildTaskResultOutput(result.SessionID, input.AssignedPlanDBTask, result.ResultText), nil
}

func observableTaskAgent(agent string) bool {
	switch agent {
	case "coder", "fixer", "deep-worker", "subtask-executor":
		return true
	default:
		return false
	}
}

func sliceTaskUTF16(value string, maximum int) string {
	units := utf16.Encode([]rune(value))
	if len(units) > maximum {
		units = units[:maximum]
	}
	return string(utf16.Decode(units))
}
