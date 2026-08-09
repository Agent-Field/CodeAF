// This file ports swe-pro/src/tool/plandb_guard.ts:8-417 at commit 3b25a1a.
// PlanDB I/O is routed through PlanRunFunc and active-package state through
// PlanDBActiveStore so guard decisions remain deterministic in tests.
package tool

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type PlanDBPackageBinding struct {
	TaskID  string `json:"taskID"`
	Created bool   `json:"created"`
	DBPath  string `json:"dbPath"`
}

type PlanDBActiveTask struct {
	TaskID string
	DBPath string
	Agent  string
}

type PlanDBActiveStore interface {
	Get(sessionID, directory string) *PlanDBActiveTask
	Set(sessionID, directory string, task PlanDBActiveTask)
	Clear(sessionID, directory, taskID string)
}

type MemoryPlanDBActiveStore struct {
	mu    sync.Mutex
	tasks map[string]PlanDBActiveTask
}

func (s *MemoryPlanDBActiveStore) key(sessionID, directory string) string {
	return sessionID + "\x00" + directory
}
func (s *MemoryPlanDBActiveStore) Get(sessionID, directory string) *PlanDBActiveTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[s.key(sessionID, directory)]
	if !ok {
		return nil
	}
	copy := task
	return &copy
}
func (s *MemoryPlanDBActiveStore) Set(sessionID, directory string, task PlanDBActiveTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks == nil {
		s.tasks = map[string]PlanDBActiveTask{}
	}
	s.tasks[s.key(sessionID, directory)] = task
}

// Clear removes the active binding only when taskID still owns it. An empty
// taskID is the explicit unconditional-clear form used by the PlanDB tool.
func (s *MemoryPlanDBActiveStore) Clear(sessionID, directory, taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(sessionID, directory)
	current, ok := s.tasks[key]
	if !ok || (taskID != "" && current.TaskID != taskID) {
		return
	}
	delete(s.tasks, key)
}

type PlanDBGuardContext struct {
	Messages  []string
	SessionID string
	MessageID string
	Agent     string
	Directory string
	Worktree  string
}

// PlanDBInfoFromMessages prefers the first assigned-task reminder and
// otherwise retains the first root-only reminder.
func PlanDBInfoFromMessages(messages []string) *TaskPlanDBInfo {
	var root *TaskPlanDBInfo
	for _, message := range messages {
		info := ParseTaskPlanDBInfo(message)
		if info == nil {
			continue
		}
		if info.AssignedTaskID != "" {
			return info
		}
		if root == nil {
			root = info
		}
	}
	return root
}

// PlanDBFileScopeMatches tests both absolute and worktree-relative spellings.
func PlanDBFileScopeMatches(description string, files, relativeFiles []string) bool {
	scope := planPolicyLine(description, "file_scope")
	if scope == "" || regexp.MustCompile(`(?i)unknown`).MatchString(scope) {
		return false
	}
	scope = strings.ReplaceAll(scope, `\`, "/")
	for _, file := range append(append([]string{}, files...), relativeFiles...) {
		if strings.Contains(scope, strings.ReplaceAll(file, `\`, "/")) {
			return true
		}
	}
	return false
}

// ExpandPlanDBFileScope appends new files in insertion order and replaces the
// first policy line in place.
func ExpandPlanDBFileScope(description string, relativeFiles []string) string {
	scope := newOrderedStrings()
	for _, item := range strings.Split(planPolicyLine(description, "file_scope"), ",") {
		item = jscompat.Trim(item)
		if item != "" && !regexp.MustCompile(`(?i)unknown`).MatchString(item) {
			scope.Add(item)
		}
	}
	for _, file := range relativeFiles {
		scope.Add(file)
	}
	next := "file_scope: " + strings.Join(scope.Values(), ",")
	if description == "" {
		return next
	}
	re := regexp.MustCompile(`(?im)^file_scope:\s*.+$`)
	if re.MatchString(description) {
		return re.ReplaceAllString(description, next)
	}
	return next + "\n" + description
}

func PlanDBFileScope(description string) []string {
	out := []string{}
	for _, item := range strings.Split(planPolicyLine(description, "file_scope"), ",") {
		if item = jscompat.Trim(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func utf16Slice(value string, end int) string {
	units := utf16.Encode([]rune(value))
	if end < 0 {
		end = max(0, len(units)+end)
	}
	end = min(end, len(units))
	return string(utf16.Decode(units[:end]))
}

// PlanDBMutationTitle builds a bounded direct-implementation package title.
func PlanDBMutationTitle(relativeFiles []string) string {
	joined := strings.Join(relativeFiles, ", ")
	if utf16Length(joined) > 100 {
		return "Direct implementation: " + utf16Slice(joined, 77) + "..."
	}
	return "Direct implementation: " + joined
}

func collapseJSWhitespace(value string) string {
	var b strings.Builder
	spaced := false
	for _, r := range value {
		isSpace := unicode.IsSpace(r) || r == '\ufeff'
		if isSpace {
			if b.Len() > 0 {
				spaced = true
			}
			continue
		}
		if spaced {
			b.WriteByte(' ')
			spaced = false
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// PlanDBVerificationTitle builds the bounded normalized verification title.
func PlanDBVerificationTitle(command string) string {
	compact := collapseJSWhitespace(command)
	if utf16Length(compact) > 100 {
		return "Direct verification: " + utf16Slice(compact, 77) + "..."
	}
	return "Direct verification: " + compact
}

var (
	verificationTestRE = regexp.MustCompile(`\b(pytest|go test|bun test|npm test|pnpm test|yarn test|cargo test|swift test|gradle test|mvn test)\b`)
	verificationLintRE = regexp.MustCompile(`\b(typecheck|tsc|lint|ruff|mypy|eslint|prettier|biome|cargo clippy|go vet)\b`)
	verificationMakeRE = regexp.MustCompile(`\b(make|just|task)\s+[^;&|]*(test|check|lint|verify|typecheck)\b`)
)

// IsVerificationShellCommand is the guard's exact command classifier.
func IsVerificationShellCommand(command string) bool {
	text := strings.ToLower(command)
	return verificationTestRE.MatchString(text) ||
		verificationLintRE.MatchString(text) ||
		verificationMakeRE.MatchString(text)
}

func PlanDBShellTitle(command string) string {
	if IsVerificationShellCommand(command) {
		return PlanDBVerificationTitle(command)
	}
	return "Direct shell: " + utf16Slice(command, 87)
}

const (
	harnessDirectShellDescription        = "Harness-created direct shell package."
	harnessDirectVerificationDescription = "Harness-created direct verification package."
	harnessDirectMutationDescription     = "Harness-created direct mutation package (backfill: agent wrote outside the scheduler dispatch flow)."
	harnessSubagentTaskDescription       = "Harness-created subagent task."
)

// IsHarnessDirectPackageDescription recognizes only the exact first-line
// markers emitted by the guard. Planner-authored prose later in a description
// must not be mistaken for harness bookkeeping.
func IsHarnessDirectPackageDescription(description string) bool {
	first, _, _ := strings.Cut(description, "\n")
	switch first {
	case harnessDirectShellDescription,
		harnessDirectVerificationDescription,
		harnessDirectMutationDescription:
		return true
	default:
		return false
	}
}

// IsHarnessSubagentPackageDescription recognizes the bookkeeping package the
// task tool creates around a subagent dispatch (BuildTaskPackageDescription).
// Like the direct packages, only the exact first line counts.
func IsHarnessSubagentPackageDescription(description string) bool {
	first, _, _ := strings.Cut(description, "\n")
	return first == harnessSubagentTaskDescription
}

// PlanDBShellDescription assembles a direct shell/QA package.
func PlanDBShellDescription(ctx PlanDBGuardContext, command, role string) string {
	first := harnessDirectShellDescription
	outputs := "outputs: command_report"
	if role == "qa" {
		first = harnessDirectVerificationDescription
		outputs = "outputs: test_report"
	}
	return strings.Join([]string{
		first,
		"",
		"session_id: " + ctx.SessionID,
		"message_id: " + ctx.MessageID,
		"task_role: " + role,
		"access: read",
		"parallel: serial",
		"worktree: none",
		"file_scope: unknown until command completes",
		"agent: " + ctx.Agent,
		"context_inputs: parent,deps",
		outputs,
		"acceptance: command completes and result is summarized",
		"",
		"command: " + command,
	}, "\n")
}

func planDBObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func planDBString(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func planDBList(data []byte) []any {
	value := parsePlanJSONAny(data)
	list, _ := value.([]any)
	return list
}

func planDBOpenStatus(status string) bool {
	switch status {
	case "pending", "ready", "claimed", "running":
		return true
	default:
		return false
	}
}

func guardActive(store PlanDBActiveStore, sessionID, directory string) *PlanDBActiveTask {
	if store == nil {
		return nil
	}
	return store.Get(sessionID, directory)
}

func guardSetActive(store PlanDBActiveStore, ctx PlanDBGuardContext, info *TaskPlanDBInfo, taskID string) {
	if store != nil {
		store.Set(ctx.SessionID, ctx.Directory, PlanDBActiveTask{
			TaskID: taskID, DBPath: info.DBPath, Agent: ctx.Agent,
		})
	}
}

func guardClaimStart(run PlanRunFunc, taskID, agent, status string) {
	if status == "pending" || status == "ready" {
		run([]string{"plandb", "task", "claim", taskID, "--agent", agent, "--json"})
	}
	if status != "running" {
		run([]string{"plandb", "task", "start", taskID, "--json"})
	}
}

func guardRootExcerpt(info *TaskPlanDBInfo, run PlanRunFunc) string {
	result := run([]string{"plandb", "show", info.RootTaskID, "--json"})
	root := planDBObject(parsePlanJSONAny(result.Stdout))
	body := StripTaskPolicyLines(planDBString(root, "description"))
	if body == "" {
		return ""
	}
	return CompactTaskText(body, RootIssueExcerptLimit)
}

// EnsurePlanDBMutationPackage binds a direct mutation to an assigned/reusable
// package or creates the backfill package.
func EnsurePlanDBMutationPackage(ctx PlanDBGuardContext, filePaths []string, run PlanRunFunc, active PlanDBActiveStore) *PlanDBPackageBinding {
	info := PlanDBInfoFromMessages(ctx.Messages)
	if info == nil {
		return nil
	}
	files := make([]string, 0, len(filePaths))
	relFiles := make([]string, 0, len(filePaths))
	for _, file := range filePaths {
		if !filepath.IsAbs(file) {
			file = filepath.Join(ctx.Directory, file)
		}
		file = filepath.Clean(file)
		files = append(files, file)
		rel, _ := filepath.Rel(ctx.Worktree, file)
		relFiles = append(relFiles, strings.ReplaceAll(rel, `\`, "/"))
	}
	assigned := info.AssignedTaskID
	if current := guardActive(active, ctx.SessionID, ctx.Directory); current != nil &&
		current.DBPath == info.DBPath && current.TaskID != info.RootTaskID {
		assigned = current.TaskID
	}
	if assigned != "" && assigned != info.RootTaskID {
		taskResult := run([]string{"plandb", "show", assigned, "--json"})
		task := planDBObject(parsePlanJSONAny(taskResult.Stdout))
		if planDBString(task, "id") != "" &&
			!PlanDBFileScopeMatches(planDBString(task, "description"), files, relFiles) {
			description := ExpandPlanDBFileScope(planDBString(task, "description"), relFiles)
			args := []string{"plandb", "task", "update", planDBString(task, "id"), "--description", description}
			if strings.HasPrefix(planDBString(task, "title"), "Direct implementation:") {
				args = append(args, "--title", PlanDBMutationTitle(PlanDBFileScope(description)))
			}
			run(append(args, "--json"))
		}
		return &PlanDBPackageBinding{TaskID: assigned, Created: false, DBPath: info.DBPath}
	}

	list := run([]string{"plandb", "list", "--json", "--project", info.ProjectID})
	tasks := planDBList(list.Stdout)
	for _, value := range tasks {
		task := planDBObject(value)
		if planDBString(task, "parent_task_id") != info.RootTaskID ||
			!planDBOpenStatus(planDBString(task, "status")) {
			continue
		}
		access := planPolicyLine(planDBString(task, "description"), "access")
		if access != "write" && access != "integration" {
			continue
		}
		if !PlanDBFileScopeMatches(planDBString(task, "description"), files, relFiles) {
			continue
		}
		id := planDBString(task, "id")
		if id != "" {
			guardClaimStart(run, id, ctx.Agent, planDBString(task, "status"))
			guardSetActive(active, ctx, info, id)
			return &PlanDBPackageBinding{TaskID: id, Created: false, DBPath: info.DBPath}
		}
	}

	activeWrites := make([]map[string]any, 0)
	for _, value := range tasks {
		task := planDBObject(value)
		if planDBString(task, "parent_task_id") != info.RootTaskID ||
			!planDBOpenStatus(planDBString(task, "status")) {
			continue
		}
		if agent := planDBString(task, "agent_id"); agent != "" && agent != ctx.Agent {
			continue
		}
		description := planDBString(task, "description")
		access := planPolicyLine(description, "access")
		if access != "write" && access != "integration" {
			continue
		}
		role := planPolicyLine(description, "task_role")
		if role == "implementation" || role == "integration" || planDBString(task, "kind") == "code" {
			activeWrites = append(activeWrites, task)
		}
	}
	if len(activeWrites) == 1 && planDBString(activeWrites[0], "id") != "" {
		task := activeWrites[0]
		description := ExpandPlanDBFileScope(planDBString(task, "description"), relFiles)
		args := []string{"plandb", "task", "update", planDBString(task, "id"), "--description", description}
		if strings.HasPrefix(planDBString(task, "title"), "Direct implementation:") {
			args = append(args, "--title", PlanDBMutationTitle(PlanDBFileScope(description)))
		}
		run(append(args, "--json"))
		guardClaimStart(run, planDBString(task, "id"), ctx.Agent, planDBString(task, "status"))
		guardSetActive(active, ctx, info, planDBString(task, "id"))
		return &PlanDBPackageBinding{TaskID: planDBString(task, "id"), Created: false, DBPath: info.DBPath}
	}

	for _, value := range tasks {
		task := planDBObject(value)
		description := planDBString(task, "description")
		if planDBString(task, "parent_task_id") != info.RootTaskID ||
			(planDBString(task, "status") != "claimed" && planDBString(task, "status") != "running") ||
			(planDBString(task, "agent_id") != "" && planDBString(task, "agent_id") != ctx.Agent) ||
			!strings.HasPrefix(planDBString(task, "title"), "Direct implementation:") ||
			planPolicyLine(description, "task_role") != "implementation" {
			continue
		}
		description = ExpandPlanDBFileScope(description, relFiles)
		id := planDBString(task, "id")
		run([]string{
			"plandb", "task", "update", id, "--description", description,
			"--title", PlanDBMutationTitle(PlanDBFileScope(description)), "--json",
		})
		guardSetActive(active, ctx, info, id)
		return &PlanDBPackageBinding{TaskID: id, Created: false, DBPath: info.DBPath}
	}

	descriptionLines := []string{
		harnessDirectMutationDescription,
		"",
		"session_id: " + ctx.SessionID,
		"message_id: " + ctx.MessageID,
		"task_role: implementation",
		"file_scope: " + strings.Join(relFiles, ","),
		"agent: " + ctx.Agent,
		"outputs: patch,handoff",
		"acceptance: mutation is applied, verified when practical, and summarized",
	}
	if excerpt := guardRootExcerpt(info, run); excerpt != "" {
		descriptionLines = append(descriptionLines, "", "Root issue context:", excerpt)
	}
	created := run([]string{
		"plandb", "add", PlanDBMutationTitle(relFiles), "--json",
		"--project", info.ProjectID, "--parent", info.RootTaskID, "--kind", "code",
		"--description", strings.Join(descriptionLines, "\n"),
		"--tag", "access:write", "--tag", "parallel:serial",
		"--tag", "role:implementation", "--tag", "output:patch",
	})
	task := planDBObject(parsePlanJSONAny(created.Stdout))
	id := planDBString(task, "id")
	if id == "" {
		return nil
	}
	run([]string{"plandb", "task", "claim", id, "--agent", ctx.Agent, "--json"})
	run([]string{"plandb", "task", "start", id, "--json"})
	guardSetActive(active, ctx, info, id)
	return &PlanDBPackageBinding{TaskID: id, Created: true, DBPath: info.DBPath}
}

// EnsurePlanDBShellPackage binds verification commands to the current/reusable
// QA package or creates one.
func EnsurePlanDBShellPackage(ctx PlanDBGuardContext, command string, run PlanRunFunc, active PlanDBActiveStore) *PlanDBPackageBinding {
	if !IsVerificationShellCommand(command) {
		return nil
	}
	info := PlanDBInfoFromMessages(ctx.Messages)
	if info == nil {
		return nil
	}
	if current := guardActive(active, ctx.SessionID, ctx.Directory); current != nil &&
		current.DBPath == info.DBPath && current.TaskID != info.RootTaskID {
		return &PlanDBPackageBinding{TaskID: current.TaskID, Created: false, DBPath: info.DBPath}
	}
	if info.AssignedTaskID != "" && info.AssignedTaskID != info.RootTaskID {
		return &PlanDBPackageBinding{TaskID: info.AssignedTaskID, Created: false, DBPath: info.DBPath}
	}
	list := run([]string{"plandb", "list", "--json", "--project", info.ProjectID})
	for _, value := range planDBList(list.Stdout) {
		task := planDBObject(value)
		if planDBString(task, "parent_task_id") != info.RootTaskID ||
			!planDBOpenStatus(planDBString(task, "status")) ||
			(planDBString(task, "agent_id") != "" && planDBString(task, "agent_id") != ctx.Agent) {
			continue
		}
		role := planPolicyLine(planDBString(task, "description"), "task_role")
		if role != "qa" && role != "review" && role != "integration" && planDBString(task, "kind") != "test" {
			continue
		}
		id := planDBString(task, "id")
		if id != "" {
			guardClaimStart(run, id, ctx.Agent, planDBString(task, "status"))
			guardSetActive(active, ctx, info, id)
			return &PlanDBPackageBinding{TaskID: id, Created: false, DBPath: info.DBPath}
		}
	}
	created := run([]string{
		"plandb", "add", PlanDBShellTitle(command), "--json",
		"--project", info.ProjectID, "--parent", info.RootTaskID, "--kind", "test",
		"--description", PlanDBShellDescription(ctx, command, "qa"),
		"--tag", "access:read", "--tag", "parallel:serial",
		"--tag", "role:qa", "--tag", "output:test_report",
	})
	task := planDBObject(parsePlanJSONAny(created.Stdout))
	id := planDBString(task, "id")
	if id == "" {
		return nil
	}
	run([]string{"plandb", "task", "claim", id, "--agent", ctx.Agent, "--json"})
	run([]string{"plandb", "task", "start", id, "--json"})
	guardSetActive(active, ctx, info, id)
	return &PlanDBPackageBinding{TaskID: id, Created: true, DBPath: info.DBPath}
}
