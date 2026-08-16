package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
)

const taskDescription = "Launch a new agent to handle complex, multistep tasks autonomously. Each invocation returns a task_id that can resume the same subagent session."
const planDBDescription = "PlanDB task graph coordination for durable work packages, dependencies, context, scheduling, and completion evidence."

const taskSchema = `{
  "type":"object",
  "properties":{
    "description":{"type":"string"},"prompt":{"type":"string"},"subagent_type":{"type":"string"},
    "task_id":{"type":"string"},"command":{"type":"string"},"category":{"type":"string"},
    "load_skills":{"type":"array","items":{"type":"string"}},
    "mcps":{"type":"array","items":{"type":"string"}},"run_in_background":{"type":"boolean"}
  },
  "required":["description","prompt","subagent_type"],"additionalProperties":false
}`

const planDBSchema = `{
  "type":"object",
  "properties":{
    "op":{"type":"string"},"dbPath":{"type":"string"},"project":{"type":"string"},"taskId":{"type":"string"},
    "agent":{"type":"string"},"title":{"type":"string"},"tasks":{"type":"array"},"description":{"type":"string"},
    "kind":{"type":"string"},"priority":{"type":"number"},"parent":{"type":"string"},"deps":{"type":"array"},
    "tags":{"type":"array","items":{"type":"string"}},"access":{"type":"string"},"parallel":{"type":"string"},
    "worktree":{"type":"string"},"fileScope":{"type":"string"},"taskRole":{"type":"string"},
    "contextInputs":{"type":"array","items":{"type":"string"}},"outputs":{"type":"array","items":{"type":"string"}},
    "suggestedAgent":{"type":"string"},"acceptance":{"type":"string"},"status":{"type":"string"},"tag":{"type":"string"},
    "result":{"type":"string"},"files":{"type":"array","items":{"type":"string"}},"content":{"type":"string"},
    "query":{"type":"string"},"after":{"type":"string"},"before":{"type":"string"},"into":{"type":"string"},
    "limit":{"type":"number"},"depth":{"type":"number"},"contextKind":{"type":"string"},
    "leafOnly":{"type":"boolean"},"runnableOnly":{"type":"boolean"}
  },
  "required":["op"],"additionalProperties":false
}`

func validateTask(raw json.RawMessage) error {
	var input TaskParams
	return decodeInput(raw, &input, "description", "prompt", "subagent_type")
}

func validatePlanDB(raw json.RawMessage) error {
	var input PlanDBParams
	return decodeInput(raw, &input, "op")
}

func (r *Registry) executePlanDB(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var params PlanDBParams
	if err := decodeInput(call.Input, &params, "op"); err != nil {
		return steploop.ToolResult{}, err
	}
	dbPath := params.DBPath
	if dbPath == "" {
		dbPath = filepath.Join(r.workDir, ".plandb.db")
	} else if !filepath.IsAbs(dbPath) {
		dbPath = filepath.Join(r.workDir, dbPath)
	}
	if err := r.ask(ctx, call, "plandb", []string{params.Op}, map[string]any{
		"op": params.Op, "dbPath": dbPath,
	}); err != nil {
		return steploop.ToolResult{}, err
	}
	parent := ""
	if info := r.planDBInfo(ctx); info != nil {
		parent = info.AssignedTaskID
		if parent == "" {
			parent = info.RootTaskID
		}
	}
	result, err := ExecutePlanDB(params, parent, r.planRun)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	metadata, _ := jscompat.Stringify(map[string]any{
		"op": params.Op, "dbPath": dbPath, "command": result.Command, "parsed": result.Parsed, "exitCode": result.ExitCode,
	})
	return steploop.ToolResult{Title: result.Title, Output: result.Output, Metadata: msgmodel.RawObject(metadata)}, nil
}

func (r *Registry) executeTask(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var params TaskParams
	if err := decodeInput(call.Input, &params, "description", "prompt", "subagent_type"); err != nil {
		return steploop.ToolResult{}, err
	}
	if err := r.ask(ctx, call, "task", []string{params.SubagentType}, map[string]any{
		"description": params.Description, "subagent_type": params.SubagentType,
	}); err != nil {
		return steploop.ToolResult{}, err
	}
	childID := params.TaskID
	if childID == "" && r.taskSessionID != nil {
		childID = r.taskSessionID()
	}
	input := TaskDispatchInput{
		Params: params, ParentSessionID: call.SessionID, ChildSessionID: childID,
		Workspace: project.Directory(ctx, r.workDir),
	}
	info := r.planDBInfo(ctx)
	createdPackage := false
	endGuardTask := func() {}
	if params.TaskID == "" && info != nil {
		input.ProjectID, input.RootTaskID = info.ProjectID, info.RootTaskID
		input.ParentPlanDBTask = info.AssignedTaskID
		if input.ParentPlanDBTask == "" {
			input.ParentPlanDBTask = info.RootTaskID
		}
		input.RootIssueExcerpt = guardRootExcerpt(info, r.planRun)
		input.AgentID = params.SubagentType + ":" + childID
		input.AssignedPlanDBTask, createdPackage, endGuardTask = r.createTaskPackage(params, input, info)
		if InferTaskAccess(params) != "read" {
			input.IssueFile = r.taskIssueFile(input.ParentPlanDBTask)
		}
		input.InheritedContext = r.taskContextLines(info.RootTaskID, input.ParentPlanDBTask)
	}
	output, err := r.taskDispatcher.Dispatch(ctx, call.ID, input)
	if err != nil {
		// A failed dispatch must not leave the bookkeeping package running:
		// the root drain would stall on it (run-T class). Packages this call
		// created close with an honest failure result; a reused planner task
		// is released for the scheduler, never auto-closed.
		if input.AssignedPlanDBTask != "" {
			if createdPackage {
				r.planRun([]string{"plandb", "done", input.AssignedPlanDBTask, "--agent", input.AgentID,
					"--result", "Subagent dispatch failed: " + err.Error(), "--json"})
			} else {
				r.planRun([]string{"plandb", "task", "release", input.AssignedPlanDBTask, "--json"})
			}
		}
		endGuardTask()
		return steploop.ToolResult{}, err
	}
	if input.AssignedPlanDBTask != "" {
		r.planRun([]string{"plandb", "done", input.AssignedPlanDBTask, "--agent", input.AgentID, "--result", output, "--json"})
		if input.ParentPlanDBTask != "" && input.ParentPlanDBTask != input.AssignedPlanDBTask {
			r.planRun([]string{"plandb", "context", "Child package " + input.AssignedPlanDBTask + " (" + params.Description + ") handoff for parent " + input.ParentPlanDBTask + ".\n\n" + output, "--kind", "handoff", "--task", input.ParentPlanDBTask, "--json"})
		}
	}
	endGuardTask()
	metadata, _ := jscompat.Stringify(map[string]any{
		"sessionId": childID, "planDBTaskId": input.AssignedPlanDBTask,
	})
	return steploop.ToolResult{Title: params.Description, Output: output, Metadata: msgmodel.RawObject(metadata)}, nil
}

func (r *Registry) planDBInfo(ctx context.Context) *TaskPlanDBInfo {
	if instance, ok := project.FromContext(ctx); ok && instance.PlanDB != nil {
		return &TaskPlanDBInfo{
			ProjectID: instance.PlanDB.ProjectID, RootTaskID: instance.PlanDB.RootTaskID,
			AssignedTaskID: instance.PlanDB.TaskID, DBPath: instance.PlanDB.DBPath,
		}
	}
	guard := r.planDBContext(ctx, steploop.ToolCall{})
	return PlanDBInfoFromMessages(guard.Messages)
}

func (r *Registry) createTaskPackage(params TaskParams, input TaskDispatchInput, info *TaskPlanDBInfo) (string, bool, func()) {
	list := r.planRun([]string{"plandb", "list", "--parent", input.ParentPlanDBTask, "--json"})
	for _, item := range planDBList(list.Stdout) {
		task := planDBObject(item)
		if strings.TrimSpace(planDBString(task, "title")) == strings.TrimSpace(params.Description) &&
			planDBOpenStatus(planDBString(task, "status")) {
			id := planDBString(task, "id")
			endGuard := r.BeginGuardTask(id)
			guardClaimStart(r.planRun, id, input.AgentID, planDBString(task, "status"))
			return id, false, endGuard
		}
	}
	access := InferTaskAccess(params)
	kind := "code"
	if access == "read" {
		kind = "research"
	}
	args := []string{
		"plandb", "add", params.Description, "--json", "--project", info.ProjectID,
		"--parent", input.ParentPlanDBTask, "--kind", kind,
		"--description", BuildTaskPackageDescription(params, input.ParentSessionID, input.ChildSessionID, input.AgentID, input.RootIssueExcerpt),
		"--tag", "session:" + input.ChildSessionID, "--tag", "subagent:" + params.SubagentType,
		"--tag", "access:" + access, "--tag", "role:" + InferTaskRole(params, access),
	}
	for _, output := range InferTaskOutputs(params, access, InferTaskRole(params, access)) {
		args = append(args, "--tag", "output:"+output)
	}
	created := r.planRun(args)
	id := planDBString(planDBObject(parsePlanJSONAny(created.Stdout)), "id")
	endGuard := r.BeginGuardTask(id)
	if id != "" {
		guardClaimStart(r.planRun, id, input.AgentID, "ready")
	}
	return id, id != "", endGuard
}

func (r *Registry) taskIssueFile(taskID string) string {
	if taskID == "" {
		return ""
	}
	shown := r.planRun([]string{"plandb", "show", taskID, "--json"})
	task := planDBObject(parsePlanJSONAny(shown.Stdout))
	for _, line := range strings.Split(planDBString(task, "description"), "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(key), "issue_file") {
			path := strings.TrimSpace(value)
			if path != "" {
				if !filepath.IsAbs(path) {
					path = filepath.Join(r.workDir, path)
				}
				if _, err := os.Stat(path); err == nil {
					return path
				}
			}
		}
	}
	return ""
}

func (r *Registry) taskContextLines(rootTaskID, parentTaskID string) []string {
	result := r.planRun([]string{"plandb", "contexts", "--json"})
	items := []TaskContextItem{}
	if json.Unmarshal(result.Stdout, &items) != nil {
		return []string{}
	}
	return TaskContextLines(items, []string{rootTaskID, parentTaskID})
}
