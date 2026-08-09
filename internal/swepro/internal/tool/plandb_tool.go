// This file ports swe-pro/src/tool/plandb.ts:9-932,1091-1212 at commit
// 3b25a1a. The CLI-shaped execution shell delegates to internal/plandb's
// already byte-verified in-memory bridge.
package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
)

// PlanDepRef accepts either the id-keyed bare-string dependency or the legacy
// {taskId,kind} shape.
type PlanDepRef struct {
	TaskID string `json:"taskId"`
	Kind   string `json:"kind,omitempty"`
	Bare   bool   `json:"-"`
}

func (d *PlanDepRef) UnmarshalJSON(data []byte) error {
	var bare string
	if err := json.Unmarshal(data, &bare); err == nil {
		d.TaskID, d.Bare = bare, true
		return nil
	}
	type dep PlanDepRef
	var object dep
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	*d = PlanDepRef(object)
	return nil
}

func (d PlanDepRef) MarshalJSON() ([]byte, error) {
	if d.Bare {
		return jscompat.Stringify(d.TaskID)
	}
	type dep struct {
		TaskID string `json:"taskId"`
		Kind   string `json:"kind,omitempty"`
	}
	return jscompat.Stringify(dep{TaskID: d.TaskID, Kind: d.Kind})
}

// PlanTaskInput mirrors plandb.ts TaskInput.
type PlanTaskInput struct {
	Title          string       `json:"title"`
	ID             string       `json:"id,omitempty"`
	TaskKey        string       `json:"taskKey,omitempty"`
	Description    string       `json:"description,omitempty"`
	Kind           string       `json:"kind,omitempty"`
	Priority       *float64     `json:"priority,omitempty"`
	Parent         string       `json:"parent,omitempty"`
	Deps           []PlanDepRef `json:"deps,omitempty"`
	Tags           []string     `json:"tags,omitempty"`
	Access         string       `json:"access,omitempty"`
	Parallel       string       `json:"parallel,omitempty"`
	Worktree       string       `json:"worktree,omitempty"`
	FileScope      string       `json:"fileScope,omitempty"`
	TaskRole       string       `json:"taskRole,omitempty"`
	ContextInputs  []string     `json:"contextInputs,omitempty"`
	Outputs        []string     `json:"outputs,omitempty"`
	SuggestedAgent string       `json:"suggestedAgent,omitempty"`
	Acceptance     string       `json:"acceptance,omitempty"`
	rawJSON        json.RawMessage
}

func (t *PlanTaskInput) UnmarshalJSON(data []byte) error {
	type taskAlias PlanTaskInput
	var value taskAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*t = PlanTaskInput(value)
	t.rawJSON = append([]byte(nil), data...)
	return nil
}

func (t PlanTaskInput) MarshalJSON() ([]byte, error) {
	if len(t.rawJSON) > 0 {
		return append([]byte(nil), t.rawJSON...), nil
	}
	type taskAlias PlanTaskInput
	return jscompat.Stringify(taskAlias(t))
}

// PlanDBParams mirrors the PlanDB tool input.
type PlanDBParams struct {
	Op             string          `json:"op"`
	DBPath         string          `json:"dbPath,omitempty"`
	Project        string          `json:"project,omitempty"`
	TaskID         string          `json:"taskId,omitempty"`
	Agent          string          `json:"agent,omitempty"`
	Title          string          `json:"title,omitempty"`
	Tasks          []PlanTaskInput `json:"tasks,omitempty"`
	Description    string          `json:"description,omitempty"`
	Kind           string          `json:"kind,omitempty"`
	Priority       *float64        `json:"priority,omitempty"`
	Parent         string          `json:"parent,omitempty"`
	Deps           []PlanDepRef    `json:"deps,omitempty"`
	Tags           []string        `json:"tags,omitempty"`
	Access         string          `json:"access,omitempty"`
	Parallel       string          `json:"parallel,omitempty"`
	Worktree       string          `json:"worktree,omitempty"`
	FileScope      string          `json:"fileScope,omitempty"`
	TaskRole       string          `json:"taskRole,omitempty"`
	ContextInputs  []string        `json:"contextInputs,omitempty"`
	Outputs        []string        `json:"outputs,omitempty"`
	SuggestedAgent string          `json:"suggestedAgent,omitempty"`
	Acceptance     string          `json:"acceptance,omitempty"`
	Status         string          `json:"status,omitempty"`
	Tag            string          `json:"tag,omitempty"`
	Result         *string         `json:"result,omitempty"`
	Files          []string        `json:"files,omitempty"`
	Content        string          `json:"content,omitempty"`
	Query          string          `json:"query,omitempty"`
	After          string          `json:"after,omitempty"`
	Before         string          `json:"before,omitempty"`
	Into           string          `json:"into,omitempty"`
	Limit          *float64        `json:"limit,omitempty"`
	Depth          *float64        `json:"depth,omitempty"`
	ContextKind    string          `json:"contextKind,omitempty"`
	LeafOnly       *bool           `json:"leafOnly,omitempty"`
	RunnableOnly   *bool           `json:"runnableOnly,omitempty"`
}

type planPolicy struct {
	Access        string   `json:"access,omitempty"`
	Parallel      string   `json:"parallel,omitempty"`
	Worktree      string   `json:"worktree,omitempty"`
	FileScope     string   `json:"fileScope,omitempty"`
	TaskRole      string   `json:"taskRole,omitempty"`
	ContextInputs []string `json:"contextInputs,omitempty"`
	Outputs       []string `json:"outputs,omitempty"`
	Agent         string   `json:"agent,omitempty"`
	Acceptance    string   `json:"acceptance,omitempty"`
}

func planPolicyLine(description, name string) string {
	re := regexp.MustCompile(`(?im)^` + regexp.QuoteMeta(name) + `:\s*(.+)$`)
	match := re.FindStringSubmatch(description)
	if len(match) < 2 {
		return ""
	}
	return jscompat.Trim(match[1])
}

func parsePlanPolicy(description string) planPolicy {
	split := func(name string) []string {
		value := planPolicyLine(description, name)
		if value == "" {
			return nil
		}
		items := strings.Split(value, ",")
		out := make([]string, 0, len(items))
		for _, item := range items {
			if item = jscompat.Trim(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	return planPolicy{
		Access:        planPolicyLine(description, "access"),
		Parallel:      planPolicyLine(description, "parallel"),
		Worktree:      planPolicyLine(description, "worktree"),
		FileScope:     planPolicyLine(description, "file_scope"),
		TaskRole:      planPolicyLine(description, "task_role"),
		ContextInputs: split("context_inputs"),
		Outputs:       split("outputs"),
		Agent:         planPolicyLine(description, "agent"),
		Acceptance:    planPolicyLine(description, "acceptance"),
	}
}

func hasPlanPolicyLine(description, key string) bool {
	return regexp.MustCompile(`(?im)^` + regexp.QuoteMeta(key) + `:`).MatchString(description)
}

func planParamsFromTask(task PlanTaskInput, project string) PlanDBParams {
	return PlanDBParams{
		Project: project, Title: task.Title, Description: task.Description,
		Kind: task.Kind, Priority: task.Priority, Parent: task.Parent, Deps: task.Deps,
		Tags: task.Tags, Access: task.Access, Parallel: task.Parallel,
		Worktree: task.Worktree, FileScope: task.FileScope, TaskRole: task.TaskRole,
		ContextInputs: task.ContextInputs, Outputs: task.Outputs,
		SuggestedAgent: task.SuggestedAgent, Acceptance: task.Acceptance,
	}
}

func defaultPlanContextInputs(params PlanDBParams) []string {
	out := newOrderedStrings()
	for _, input := range params.ContextInputs {
		out.Add(input)
	}
	out.Add("parent")
	if len(params.Deps) > 0 {
		out.Add("deps")
	}
	if params.FileScope != "" || hasPlanPolicyLine(params.Description, "file_scope") {
		out.Add("file_scope")
	}
	return out.Values()
}

// WithPlanPolicy injects missing scheduler-policy lines at the front.
func WithPlanPolicy(params PlanDBParams) string {
	description := params.Description
	var lines []string
	add := func(key, value string) {
		if value != "" && !hasPlanPolicyLine(description, key) {
			lines = append(lines, key+": "+value)
		}
	}
	add("access", params.Access)
	add("parallel", params.Parallel)
	add("worktree", params.Worktree)
	add("file_scope", params.FileScope)
	add("task_role", params.TaskRole)
	if !hasPlanPolicyLine(description, "context_inputs") {
		lines = append(lines, "context_inputs: "+strings.Join(defaultPlanContextInputs(params), ","))
	}
	if len(params.Outputs) > 0 {
		add("outputs", strings.Join(params.Outputs, ","))
	}
	add("agent", params.SuggestedAgent)
	add("acceptance", params.Acceptance)
	if len(lines) > 0 {
		parts := []string{strings.Join(lines, "\n")}
		if description != "" {
			parts = append(parts, description)
		}
		description = strings.Join(parts, "\n")
	}
	return description
}

// BuildPlanDBAddCommand ports buildAddCommand.
func BuildPlanDBAddCommand(params PlanDBParams) []string {
	args := []string{"plandb", "add", params.Title, "--json"}
	if params.Project != "" {
		args = append(args, "--project", params.Project)
	}
	if params.Kind != "" {
		args = append(args, "--kind", params.Kind)
	}
	if params.Priority != nil {
		args = append(args, "--priority", jscompat.FormatNumber(*params.Priority))
	}
	if params.Parent != "" {
		args = append(args, "--parent", params.Parent)
	}
	if description := WithPlanPolicy(params); description != "" {
		args = append(args, "--description", description)
	}
	for _, dep := range params.Deps {
		kind := dep.Kind
		if kind == "" {
			kind = "feeds_into"
		}
		args = append(args, "--dep", dep.TaskID+":"+kind)
	}
	for _, tag := range params.Tags {
		args = append(args, "--tag", tag)
	}
	if params.Access != "" {
		args = append(args, "--tag", "access:"+params.Access)
	}
	if params.Parallel != "" {
		args = append(args, "--tag", "parallel:"+params.Parallel)
	}
	if params.Worktree == "required" {
		args = append(args, "--tag", "needs:worktree")
	}
	if params.TaskRole != "" {
		args = append(args, "--tag", "role:"+params.TaskRole)
	}
	for _, output := range params.Outputs {
		args = append(args, "--tag", "output:"+output)
	}
	return args
}

// BuildPlanDBCommand builds the CLI-bridge argv for a non-add_many operation.
func BuildPlanDBCommand(params PlanDBParams) ([]string, error) {
	args := []string{"plandb"}
	jsonFlag := func() { args = append(args, "--json") }
	switch params.Op {
	case "init":
		name := params.Project
		if name == "" {
			name = params.Title
		}
		if name == "" {
			name = "codeaf"
		}
		args = append(args, "init", name)
	case "add":
		if params.Title == "" {
			return nil, fmt.Errorf("plandb add requires title")
		}
		return BuildPlanDBAddCommand(params), nil
	case "add_many":
		return nil, fmt.Errorf("plandb add_many is handled directly")
	case "list", "list_ready":
		args = append(args, "list")
		jsonFlag()
		if params.Project != "" {
			args = append(args, "--project", params.Project)
		}
		if params.Op == "list_ready" {
			args = append(args, "--status", "ready")
		} else if params.Status != "" {
			args = append(args, "--status", params.Status)
		}
		if params.Kind != "" {
			args = append(args, "--kind", params.Kind)
		}
		if params.Tag != "" {
			args = append(args, "--tag", params.Tag)
		}
	case "show":
		if params.TaskID == "" {
			return nil, fmt.Errorf("plandb show requires taskId")
		}
		args = append(args, "show", params.TaskID)
		jsonFlag()
	case "claim":
		if params.TaskID == "" {
			return nil, fmt.Errorf("plandb claim requires taskId")
		}
		agent := params.Agent
		if agent == "" {
			agent = "codeaf"
		}
		args = append(args, "task", "claim", params.TaskID, "--agent", agent)
		jsonFlag()
	case "done":
		if params.TaskID == "" {
			return nil, fmt.Errorf("plandb done requires taskId")
		}
		args = append(args, "done", params.TaskID)
		jsonFlag()
		if params.Agent != "" {
			args = append(args, "--agent", params.Agent)
		}
		if params.Result != nil {
			args = append(args, "--result", *params.Result)
		}
		if len(params.Files) > 0 {
			args = append(args, "--files", strings.Join(params.Files, ","))
		}
	case "context":
		if params.Content == "" {
			return nil, fmt.Errorf("plandb context requires content")
		}
		args = append(args, "context", params.Content)
		jsonFlag()
		if params.ContextKind != "" {
			args = append(args, "--kind", params.ContextKind)
		}
		if params.TaskID != "" {
			args = append(args, "--task", params.TaskID)
		}
		if params.Project != "" {
			args = append(args, "--project", params.Project)
		}
	case "contexts":
		args = append(args, "contexts")
		jsonFlag()
		if params.Project != "" {
			args = append(args, "--project", params.Project)
		}
		if params.ContextKind != "" {
			args = append(args, "--kind", params.ContextKind)
		}
		if params.Limit != nil {
			args = append(args, "--limit", jscompat.FormatNumber(*params.Limit))
		}
	case "search":
		if params.Query == "" {
			return nil, fmt.Errorf("plandb search requires query")
		}
		args = append(args, "search", params.Query)
		jsonFlag()
		if params.Project != "" {
			args = append(args, "--project", params.Project)
		}
		if params.Limit != nil {
			args = append(args, "--limit", jscompat.FormatNumber(*params.Limit))
		}
	case "amend":
		if params.TaskID == "" {
			return nil, fmt.Errorf("plandb amend requires taskId")
		}
		if params.Content == "" {
			return nil, fmt.Errorf("plandb amend requires content")
		}
		args = append(args, "task", "amend", params.TaskID, "--prepend", params.Content)
		jsonFlag()
	case "insert":
		if params.After == "" {
			return nil, fmt.Errorf("plandb insert requires after")
		}
		if params.Title == "" {
			return nil, fmt.Errorf("plandb insert requires title")
		}
		args = append(args, "task", "insert", "--after", params.After, "--title", params.Title)
		jsonFlag()
		if params.Before != "" {
			args = append(args, "--before", params.Before)
		}
		if description := WithPlanPolicy(params); description != "" {
			args = append(args, "--description", description)
		}
		if params.Project != "" {
			args = append(args, "--project", params.Project)
		}
	case "update":
		if params.TaskID == "" {
			return nil, fmt.Errorf("plandb update requires taskId")
		}
		args = append(args, "task", "update", params.TaskID)
		jsonFlag()
		if params.Title != "" {
			args = append(args, "--title", params.Title)
		}
		if params.Description != "" {
			args = append(args, "--description", WithPlanPolicy(params))
		}
		if params.Kind != "" {
			args = append(args, "--kind", params.Kind)
		}
		if params.Priority != nil {
			args = append(args, "--priority", jscompat.FormatNumber(*params.Priority))
		}
	case "split":
		if params.Into == "" {
			return nil, fmt.Errorf("plandb split requires into")
		}
		args = append(args, "split")
		jsonFlag()
		if params.TaskID != "" {
			args = append(args, params.TaskID)
		}
		args = append(args, "--into", params.Into)
	case "status":
		args = append(args, "status", "--full")
		jsonFlag()
		if params.Project != "" {
			args = append(args, "--project", params.Project)
		}
	case "overview":
		args = append(args, "task", "overview")
		jsonFlag()
		if params.Project != "" {
			args = append(args, "--project", params.Project)
		}
	case "project_dag":
		args = append(args, "project", "dag")
		jsonFlag()
		if params.Project != "" {
			args = append(args, params.Project)
		}
	case "critical_path":
		args = append(args, "critical-path")
	case "bottlenecks":
		args = append(args, "bottlenecks")
	case "what_unlocks":
		if params.TaskID == "" {
			return nil, fmt.Errorf("plandb what_unlocks requires taskId")
		}
		args = append(args, "what-unlocks", params.TaskID)
		jsonFlag()
	case "ahead":
		args = append(args, "ahead")
		jsonFlag()
		if params.Project != "" {
			args = append(args, "--project", params.Project)
		}
		if params.Depth != nil {
			args = append(args, "--depth", jscompat.FormatNumber(*params.Depth))
		}
	}
	return args, nil
}

// PlanLimits bounds id-keyed documents.
type PlanLimits struct {
	MaxTasks            int `json:"maxTasks"`
	MaxTitleChars       int `json:"maxTitleChars"`
	MaxDescriptionChars int `json:"maxDescriptionChars"`
	MaxDepsPerTask      int `json:"maxDepsPerTask"`
	ApplyChunkSize      int `json:"applyChunkSize"`
}

var DefaultPlanLimits = PlanLimits{
	MaxTasks: 2000, MaxTitleChars: 500, MaxDescriptionChars: 20_000,
	MaxDepsPerTask: 200, ApplyChunkSize: 50,
}

type NormalizedPlanDep struct {
	Ref  string `json:"ref"`
	Kind string `json:"kind,omitempty"`
}

type NormalizedPlanTask struct {
	LocalID   string              `json:"localId"`
	Index     int                 `json:"index"`
	Raw       PlanTaskInput       `json:"raw"`
	ParentRef string              `json:"parentRef,omitempty"`
	Deps      []NormalizedPlanDep `json:"deps"`
}

type PlanError struct {
	Code    string
	Message string
	TaskID  string
	Index   *int
	Dep     string
	Parent  string
	Field   string
	Cycle   []string
}

func (e PlanError) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	add := func(key string, value any) error {
		if !first {
			b.WriteByte(',')
		}
		first = false
		keyJSON, _ := jscompat.Stringify(key)
		valueJSON, err := jscompat.Stringify(value)
		if err != nil {
			return err
		}
		b.Write(keyJSON)
		b.WriteByte(':')
		b.Write(valueJSON)
		return nil
	}
	_ = add("code", e.Code)
	switch e.Code {
	case "missing_id":
		if e.Index != nil {
			_ = add("index", *e.Index)
		}
	case "duplicate_id":
		_ = add("taskId", e.TaskID)
	case "missing_title":
		if e.Index != nil {
			_ = add("index", *e.Index)
		}
		if e.TaskID != "" {
			_ = add("taskId", e.TaskID)
		}
	case "field_limit":
		if e.Index != nil {
			_ = add("index", *e.Index)
		}
		_ = add("taskId", e.TaskID)
		_ = add("field", e.Field)
	case "self_dep", "dangling_dep":
		_ = add("taskId", e.TaskID)
		_ = add("dep", e.Dep)
	case "dangling_parent":
		_ = add("taskId", e.TaskID)
		_ = add("parent", e.Parent)
	case "cycle":
		_ = add("cycle", e.Cycle)
	}
	_ = add("message", e.Message)
	b.WriteByte('}')
	return b.Bytes(), nil
}

type PlanValidationResult struct {
	Errors    []PlanError          `json:"errors"`
	Order     []NormalizedPlanTask `json:"order"`
	ByLocalID struct{}             `json:"byLocalId"`
	lookup    map[string]*NormalizedPlanTask
}

// IsIDKeyedPlan detects the new plan document shape.
func IsIDKeyedPlan(tasks []PlanTaskInput) bool {
	for _, task := range tasks {
		if task.ID != "" {
			return true
		}
		for _, dep := range task.Deps {
			if dep.Bare {
				return true
			}
		}
	}
	return false
}

func utf16Length(value string) int { return len(utf16.Encode([]rune(value))) }

func planEdges(tasks []NormalizedPlanTask, lookup map[string]*NormalizedPlanTask) *jscompat.OrderedMap[string, *orderedStrings] {
	succ := jscompat.NewOrderedMap[string, *orderedStrings]()
	for i := range tasks {
		if tasks[i].LocalID != "" {
			succ.Set(tasks[i].LocalID, newOrderedStrings())
		}
	}
	add := func(from, to string) {
		if _, ok := lookup[from]; !ok {
			return
		}
		if _, ok := lookup[to]; !ok {
			return
		}
		set, _ := succ.Get(from)
		set.Add(to)
	}
	for _, task := range tasks {
		if task.LocalID == "" {
			continue
		}
		for _, dep := range task.Deps {
			add(dep.Ref, task.LocalID)
		}
		if task.ParentRef != "" {
			add(task.ParentRef, task.LocalID)
		}
	}
	return succ
}

func detectPlanCycle(tasks []NormalizedPlanTask, lookup map[string]*NormalizedPlanTask) []string {
	succ := planEdges(tasks, lookup)
	state := map[string]int{}
	for _, id := range succ.Keys() {
		state[id] = 0
	}
	type frame struct {
		id    string
		next  int
		items []string
	}
	for _, start := range succ.Keys() {
		if state[start] != 0 {
			continue
		}
		set, _ := succ.Get(start)
		stack := []frame{{id: start, items: set.Values()}}
		path := []string{start}
		state[start] = 1
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next >= len(top.items) {
				state[top.id] = 2
				stack = stack[:len(stack)-1]
				path = path[:len(path)-1]
				continue
			}
			to := top.items[top.next]
			top.next++
			if state[to] == 1 {
				index := 0
				for path[index] != to {
					index++
				}
				cycle := append([]string(nil), path[index:]...)
				return append(cycle, to)
			}
			if state[to] == 0 {
				state[to] = 1
				path = append(path, to)
				next, _ := succ.Get(to)
				stack = append(stack, frame{id: to, items: next.Values()})
			}
		}
	}
	return nil
}

func topoPlanOrder(tasks []NormalizedPlanTask, lookup map[string]*NormalizedPlanTask) []NormalizedPlanTask {
	succ := planEdges(tasks, lookup)
	indegree := map[string]int{}
	for _, id := range succ.Keys() {
		indegree[id] = 0
	}
	for _, set := range succ.Values() {
		for _, to := range set.Values() {
			indegree[to]++
		}
	}
	queue := make([]NormalizedPlanTask, 0, len(tasks))
	for _, task := range tasks {
		if task.LocalID != "" && indegree[task.LocalID] == 0 {
			queue = append(queue, task)
		}
	}
	order := make([]NormalizedPlanTask, 0, len(tasks))
	emitted := map[string]bool{}
	for head := 0; head < len(queue); head++ {
		task := queue[head]
		if emitted[task.LocalID] {
			continue
		}
		emitted[task.LocalID] = true
		order = append(order, task)
		set, _ := succ.Get(task.LocalID)
		for _, to := range set.Values() {
			indegree[to]--
			if indegree[to] == 0 {
				if next := lookup[to]; next != nil {
					queue = append(queue, *next)
				}
			}
		}
	}
	return order
}

// ValidatePlanDocument validates the entire document without writes.
func ValidatePlanDocument(tasks []PlanTaskInput, existingIDs map[string]bool, limits ...PlanLimits) PlanValidationResult {
	limit := DefaultPlanLimits
	if len(limits) > 0 {
		limit = limits[0]
	}
	errorsOut := make([]PlanError, 0)
	result := PlanValidationResult{
		Errors: errorsOut, Order: []NormalizedPlanTask{},
		lookup: map[string]*NormalizedPlanTask{},
	}
	if len(tasks) == 0 {
		result.Errors = append(result.Errors, PlanError{Code: "empty", Message: "plan document contains no tasks"})
		return result
	}
	if len(tasks) > limit.MaxTasks {
		result.Errors = append(result.Errors, PlanError{
			Code: "too_many_tasks", Message: fmt.Sprintf("plan has %d tasks; max is %d", len(tasks), limit.MaxTasks),
		})
	}
	normalized := make([]NormalizedPlanTask, 0, len(tasks))
	idIndexes := jscompat.NewOrderedMap[string, []int]()
	for index, raw := range tasks {
		localID := jscompat.Trim(raw.ID)
		indexCopy := index
		if localID == "" {
			result.Errors = append(result.Errors, PlanError{
				Code: "missing_id", Index: &indexCopy,
				Message: fmt.Sprintf(`task at index %d ("%s") is missing an "id"; id-keyed plans require an explicit id on every task`, index, raw.Title),
			})
		} else {
			seen, _ := idIndexes.Get(localID)
			idIndexes.Set(localID, append(seen, index))
		}
		if raw.Title == "" || jscompat.Trim(raw.Title) == "" {
			result.Errors = append(result.Errors, PlanError{
				Code: "missing_title", Index: &indexCopy, TaskID: localID,
				Message: fmt.Sprintf(`task "%s" is missing a title`, coalesceID(localID, index)),
			})
		} else if utf16Length(raw.Title) > limit.MaxTitleChars {
			result.Errors = append(result.Errors, PlanError{
				Code: "field_limit", Index: &indexCopy, TaskID: localID, Field: "title",
				Message: fmt.Sprintf(`task "%s" title exceeds %d chars`, localID, limit.MaxTitleChars),
			})
		}
		if utf16Length(raw.Description) > limit.MaxDescriptionChars {
			result.Errors = append(result.Errors, PlanError{
				Code: "field_limit", Index: &indexCopy, TaskID: localID, Field: "description",
				Message: fmt.Sprintf(`task "%s" description exceeds %d chars`, localID, limit.MaxDescriptionChars),
			})
		}
		deps := make([]NormalizedPlanDep, 0, len(raw.Deps))
		for _, dep := range raw.Deps {
			deps = append(deps, NormalizedPlanDep{Ref: jscompat.Trim(dep.TaskID), Kind: dep.Kind})
		}
		if len(deps) > limit.MaxDepsPerTask {
			result.Errors = append(result.Errors, PlanError{
				Code: "field_limit", Index: &indexCopy, TaskID: localID, Field: "deps",
				Message: fmt.Sprintf(`task "%s" has %d deps; max is %d`, localID, len(deps), limit.MaxDepsPerTask),
			})
		}
		task := NormalizedPlanTask{
			LocalID: localID, Index: index, Raw: raw,
			ParentRef: jscompat.Trim(raw.Parent), Deps: deps,
		}
		normalized = append(normalized, task)
		if localID != "" {
			if _, exists := result.lookup[localID]; !exists {
				result.lookup[localID] = &normalized[len(normalized)-1]
			}
		}
	}
	for _, entry := range idIndexes.Entries() {
		if len(entry.Val) > 1 {
			indexStrings := make([]string, len(entry.Val))
			for i, index := range entry.Val {
				indexStrings[i] = fmt.Sprint(index)
			}
			result.Errors = append(result.Errors, PlanError{
				Code: "duplicate_id", TaskID: entry.Key,
				Message: fmt.Sprintf(`id "%s" is declared by %d tasks (indexes %s)`, entry.Key, len(entry.Val), strings.Join(indexStrings, ", ")),
			})
		}
	}
	resolvable := func(ref string) bool { return result.lookup[ref] != nil || existingIDs[ref] }
	for _, task := range normalized {
		if task.LocalID == "" {
			continue
		}
		for _, dep := range task.Deps {
			if dep.Ref == "" {
				continue
			}
			if dep.Ref == task.LocalID {
				result.Errors = append(result.Errors, PlanError{
					Code: "self_dep", TaskID: task.LocalID, Dep: dep.Ref,
					Message: fmt.Sprintf(`task "%s" depends on itself`, task.LocalID),
				})
			} else if !resolvable(dep.Ref) {
				result.Errors = append(result.Errors, PlanError{
					Code: "dangling_dep", TaskID: task.LocalID, Dep: dep.Ref,
					Message: fmt.Sprintf(`task "%s" depends on "%s", which is neither a task id in this document nor an existing PlanDB task`, task.LocalID, dep.Ref),
				})
			}
		}
		if task.ParentRef != "" && !resolvable(task.ParentRef) {
			result.Errors = append(result.Errors, PlanError{
				Code: "dangling_parent", TaskID: task.LocalID, Parent: task.ParentRef,
				Message: fmt.Sprintf(`task "%s" names parent "%s", which is neither a task id in this document nor an existing PlanDB task`, task.LocalID, task.ParentRef),
			})
		}
	}
	if cycle := detectPlanCycle(normalized, result.lookup); len(cycle) > 0 {
		result.Errors = append(result.Errors, PlanError{
			Code: "cycle", Cycle: cycle,
			Message: "dependency cycle detected: " + strings.Join(cycle, " -> "),
		})
	}
	if len(result.Errors) > 0 {
		result.Order = []NormalizedPlanTask{}
		return result
	}
	result.Order = topoPlanOrder(normalized, result.lookup)
	return result
}

func coalesceID(id string, index int) string {
	if id != "" {
		return id
	}
	return fmt.Sprint(index)
}

type PlanApplyBase struct {
	Project        string   `json:"project,omitempty"`
	Parent         string   `json:"parent,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	Priority       *float64 `json:"priority,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Access         string   `json:"access,omitempty"`
	Parallel       string   `json:"parallel,omitempty"`
	Worktree       string   `json:"worktree,omitempty"`
	FileScope      string   `json:"fileScope,omitempty"`
	TaskRole       string   `json:"taskRole,omitempty"`
	ContextInputs  []string `json:"contextInputs,omitempty"`
	Outputs        []string `json:"outputs,omitempty"`
	SuggestedAgent string   `json:"suggestedAgent,omitempty"`
	Acceptance     string   `json:"acceptance,omitempty"`
}

type PlanAppliedTask struct {
	LocalID string `json:"localId"`
	TaskID  string `json:"taskId"`
	Title   string `json:"title"`
}

type PlanApplyFailure struct {
	LocalID string   `json:"localId"`
	Title   string   `json:"title"`
	Command []string `json:"command"`
	Error   string   `json:"error"`
}

type PlanIDMap struct {
	values *jscompat.OrderedMap[string, string]
}

func newPlanIDMap() PlanIDMap { return PlanIDMap{values: jscompat.NewOrderedMap[string, string]()} }
func (m PlanIDMap) Get(key string) string {
	value, _ := m.values.Get(key)
	return value
}
func (m PlanIDMap) Set(key, value string) { m.values.Set(key, value) }
func (m PlanIDMap) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, entry := range m.values.Entries() {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := jscompat.Stringify(entry.Key)
		value, _ := jscompat.Stringify(entry.Val)
		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type PlanApplyResult struct {
	OK        bool              `json:"ok"`
	Applied   []PlanAppliedTask `json:"applied"`
	IDMap     PlanIDMap         `json:"idMap"`
	Failed    *PlanApplyFailure `json:"failed,omitempty"`
	Remaining []string          `json:"remaining,omitempty"`
}

type PlanRunFunc func([]string) plandb.RunResult

// ApplyPlanDocument applies a validated topological order until first failure.
func ApplyPlanDocument(order []NormalizedPlanTask, base PlanApplyBase, existingIDs map[string]bool, run PlanRunFunc, chunkSizes ...int) PlanApplyResult {
	chunkSize := DefaultPlanLimits.ApplyChunkSize
	if len(chunkSizes) > 0 {
		chunkSize = chunkSizes[0]
	}
	idMap := newPlanIDMap()
	applied := make([]PlanAppliedTask, 0)
	resolve := func(ref string) string {
		if id := idMap.Get(ref); id != "" {
			return id
		}
		if existingIDs[ref] {
			return ref
		}
		return ""
	}
	for start := 0; start < len(order); start += chunkSize {
		end := min(start+chunkSize, len(order))
		chunk := order[start:end]
		for offset, task := range chunk {
			deps := make([]PlanDepRef, 0, len(task.Deps))
			for _, dep := range task.Deps {
				if id := resolve(dep.Ref); id != "" {
					deps = append(deps, PlanDepRef{TaskID: id, Kind: dep.Kind})
				}
			}
			parent := base.Parent
			if task.ParentRef != "" {
				parent = resolve(task.ParentRef)
			}
			params := PlanDBParams{
				Project: base.Project, Parent: parent, Kind: base.Kind,
				Priority: base.Priority, Tags: base.Tags, Access: base.Access,
				Parallel: base.Parallel, Worktree: base.Worktree,
				FileScope: base.FileScope, TaskRole: base.TaskRole,
				ContextInputs: base.ContextInputs, Outputs: base.Outputs,
				SuggestedAgent: base.SuggestedAgent, Acceptance: base.Acceptance,
			}
			overlayPlanTask(&params, task.Raw)
			params.Deps, params.Parent, params.Project = deps, parent, base.Project
			command := BuildPlanDBAddCommand(params)
			runResult := run(command)
			created := parsePlanJSON(runResult.Stdout)
			id, _ := created["id"].(string)
			if runResult.Code != 0 || id == "" {
				errorText := jscompat.Trim(string(runResult.Stderr))
				if errorText == "" {
					errorText = fmt.Sprintf("exit %d", runResult.Code)
				}
				remaining := make([]string, 0, len(order)-(start+offset))
				for _, pending := range order[start+offset:] {
					remaining = append(remaining, pending.LocalID)
				}
				return PlanApplyResult{
					OK: false, Applied: applied, IDMap: idMap,
					Failed: &PlanApplyFailure{
						LocalID: task.LocalID, Title: task.Raw.Title,
						Command: command, Error: errorText,
					},
					Remaining: remaining,
				}
			}
			title, _ := created["title"].(string)
			if title == "" {
				title = task.Raw.Title
			}
			idMap.Set(task.LocalID, id)
			applied = append(applied, PlanAppliedTask{LocalID: task.LocalID, TaskID: id, Title: title})
		}
	}
	return PlanApplyResult{OK: true, Applied: applied, IDMap: idMap}
}

func overlayPlanTask(params *PlanDBParams, task PlanTaskInput) {
	params.Title = task.Title
	params.Description = task.Description
	if task.Kind != "" {
		params.Kind = task.Kind
	}
	if task.Priority != nil {
		params.Priority = task.Priority
	}
	if task.Parent != "" {
		params.Parent = task.Parent
	}
	if task.Deps != nil {
		params.Deps = task.Deps
	}
	if task.Tags != nil {
		params.Tags = task.Tags
	}
	if task.Access != "" {
		params.Access = task.Access
	}
	if task.Parallel != "" {
		params.Parallel = task.Parallel
	}
	if task.Worktree != "" {
		params.Worktree = task.Worktree
	}
	if task.FileScope != "" {
		params.FileScope = task.FileScope
	}
	if task.TaskRole != "" {
		params.TaskRole = task.TaskRole
	}
	if task.ContextInputs != nil {
		params.ContextInputs = task.ContextInputs
	}
	if task.Outputs != nil {
		params.Outputs = task.Outputs
	}
	if task.SuggestedAgent != "" {
		params.SuggestedAgent = task.SuggestedAgent
	}
	if task.Acceptance != "" {
		params.Acceptance = task.Acceptance
	}
}

func parsePlanJSON(data []byte) map[string]any {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	var value map[string]any
	if json.Unmarshal(trimmed, &value) == nil {
		return value
	}
	firstObject := bytes.IndexAny(trimmed, "[{")
	if firstObject >= 0 {
		_ = json.Unmarshal(trimmed[firstObject:], &value)
	}
	return value
}

type PlanIngestResult struct {
	Title    string `json:"title"`
	ExitCode int    `json:"exitCode"`
	Parsed   any    `json:"parsed"`
}

type planValidationParsed struct {
	OK         bool        `json:"ok"`
	Phase      string      `json:"phase"`
	Created    int         `json:"created"`
	Total      int         `json:"total"`
	ErrorCount int         `json:"errorCount"`
	Errors     []PlanError `json:"errors"`
	Hint       string      `json:"hint"`
}

type planIngestParsed struct {
	OK        bool              `json:"ok"`
	Phase     string            `json:"phase"`
	Created   int               `json:"created"`
	Total     int               `json:"total"`
	IDMap     PlanIDMap         `json:"idMap"`
	Tasks     []PlanAppliedTask `json:"tasks"`
	Failed    *PlanApplyFailure `json:"failed,omitempty"`
	Remaining []string          `json:"remaining,omitempty"`
	Warning   string            `json:"warning,omitempty"`
}

// IngestIDKeyedPlan drives validation then apply.
func IngestIDKeyedPlan(tasks []PlanTaskInput, existingIDs map[string]bool, base PlanApplyBase, run PlanRunFunc) PlanIngestResult {
	validation := ValidatePlanDocument(tasks, existingIDs)
	if len(validation.Errors) > 0 {
		return PlanIngestResult{
			Title: "plandb add_many rejected", ExitCode: 1,
			Parsed: planValidationParsed{
				OK: false, Phase: "validate", Created: 0, Total: len(tasks),
				ErrorCount: len(validation.Errors), Errors: validation.Errors,
				Hint: "No tasks were created. Fix every listed violation and resend the whole document.",
			},
		}
	}
	result := ApplyPlanDocument(validation.Order, base, existingIDs, run)
	parsed := planIngestParsed{
		OK: result.OK, Created: len(result.Applied), Total: len(tasks),
		IDMap: result.IDMap, Tasks: result.Applied,
	}
	title, exitCode := "plandb add_many", 0
	if result.OK {
		parsed.Phase = "done"
	} else {
		title, exitCode, parsed.Phase = "plandb add_many partial", 1, "apply"
		parsed.Failed, parsed.Remaining = result.Failed, result.Remaining
		parsed.Warning = "PARTIAL APPLY: some tasks were created before an unexpected error. The `idMap`/`tasks` above list exactly what landed; `remaining` lists what did not. Re-send add_many with only the remaining tasks (their deps to already-created tasks can now use the real ids in `idMap`)."
	}
	return PlanIngestResult{Title: title, Parsed: parsed, ExitCode: exitCode}
}

// PlanDBToolResult is the CLI-shaped result exposed to an integrating registry.
type PlanDBToolResult struct {
	Title    string
	Output   string
	Command  []string
	Parsed   any
	ExitCode int
}

// ExecutePlanDB runs the simple surface and the id-keyed add_many path.
func ExecutePlanDB(params PlanDBParams, harnessParentTaskID string, run PlanRunFunc) (PlanDBToolResult, error) {
	if run == nil {
		run = plandb.RunPlanDB
	}
	if params.Op == "add_many" {
		if len(params.Tasks) == 0 {
			return PlanDBToolResult{}, fmt.Errorf("plandb add_many requires tasks")
		}
		if !IsIDKeyedPlan(params.Tasks) {
			return executeLegacyAddMany(params, harnessParentTaskID, run), nil
		}
		listArgs := []string{"plandb", "list", "--json"}
		if params.Project != "" {
			listArgs = append(listArgs, "--project", params.Project)
		}
		existing := planTaskIDs(run(listArgs).Stdout)
		parent := params.Parent
		if parent == "" {
			parent = harnessParentTaskID
		}
		ingested := IngestIDKeyedPlan(params.Tasks, existing, PlanApplyBase{
			Project: params.Project, Parent: parent, Kind: params.Kind,
			Priority: params.Priority, Tags: params.Tags, Access: params.Access,
			Parallel: params.Parallel, Worktree: params.Worktree,
			FileScope: params.FileScope, TaskRole: params.TaskRole,
			ContextInputs: params.ContextInputs, Outputs: params.Outputs,
			SuggestedAgent: params.SuggestedAgent, Acceptance: params.Acceptance,
		}, run)
		output, _ := jscompat.StringifyIndent(ingested.Parsed)
		return PlanDBToolResult{
			Title: ingested.Title, Output: string(output),
			Command: []string{"plandb", "add_many"}, Parsed: ingested.Parsed,
			ExitCode: ingested.ExitCode,
		}, nil
	}
	if params.Op == "add" && params.Parent == "" {
		params.Parent = harnessParentTaskID
	}
	command, err := BuildPlanDBCommand(params)
	if err != nil {
		return PlanDBToolResult{}, err
	}
	result := run(command)
	parsed := parsePlanJSONAny(result.Stdout)
	output := ""
	if parsed != nil {
		data, _ := jscompat.StringifyIndent(parsed)
		output = string(data)
	} else {
		parts := []string{}
		if stdout := jscompat.Trim(string(result.Stdout)); stdout != "" {
			parts = append(parts, stdout)
		}
		if stderr := jscompat.Trim(string(result.Stderr)); stderr != "" {
			parts = append(parts, stderr)
		}
		output = strings.Join(parts, "\n\n")
		if output == "" {
			output = "(no output)"
		}
	}
	return PlanDBToolResult{
		Title: "plandb " + params.Op, Output: output, Command: command,
		Parsed: parsed, ExitCode: result.Code,
	}, nil
}

func executeLegacyAddMany(params PlanDBParams, harnessParent string, run PlanRunFunc) PlanDBToolResult {
	type item struct {
		Command  []string `json:"command"`
		ExitCode int      `json:"exitCode"`
		Task     any      `json:"task,omitempty"`
		Output   string   `json:"output"`
	}
	results := make([]item, 0, len(params.Tasks))
	exit := 0
	for _, task := range params.Tasks {
		effective := PlanDBParams{
			Project: params.Project, Parent: params.Parent, Kind: params.Kind,
			Priority: params.Priority, Deps: params.Deps, Tags: params.Tags,
			Access: params.Access, Parallel: params.Parallel, Worktree: params.Worktree,
			FileScope: params.FileScope, TaskRole: params.TaskRole,
			ContextInputs: params.ContextInputs, Outputs: params.Outputs,
			SuggestedAgent: params.SuggestedAgent, Acceptance: params.Acceptance,
		}
		if effective.Parent == "" {
			effective.Parent = harnessParent
		}
		overlayPlanTask(&effective, task)
		command := BuildPlanDBAddCommand(effective)
		runResult := run(command)
		if runResult.Code != 0 {
			exit = 1
		}
		taskValue := parsePlanJSONAny(runResult.Stdout)
		outputParts := []string{}
		if stdout := jscompat.Trim(string(runResult.Stdout)); stdout != "" {
			outputParts = append(outputParts, stdout)
		}
		if stderr := jscompat.Trim(string(runResult.Stderr)); stderr != "" {
			outputParts = append(outputParts, stderr)
		}
		results = append(results, item{
			Command: command, ExitCode: runResult.Code, Task: taskValue,
			Output: strings.Join(outputParts, "\n\n"),
		})
	}
	data, _ := jscompat.StringifyIndent(results)
	return PlanDBToolResult{
		Title: "plandb add_many", Output: string(data),
		Command: []string{"plandb", "add_many"}, Parsed: results, ExitCode: exit,
	}
}

func parsePlanJSONAny(data []byte) any {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(trimmed, &value) == nil {
		return value
	}
	if index := bytes.IndexAny(trimmed, "[{"); index >= 0 {
		_ = json.Unmarshal(trimmed[index:], &value)
	}
	return value
}

func planTaskIDs(data []byte) map[string]bool {
	value := parsePlanJSONAny(data)
	list, _ := value.([]any)
	out := map[string]bool{}
	for _, item := range list {
		object, _ := item.(map[string]any)
		if id, ok := object["id"].(string); ok {
			out[id] = true
		}
	}
	return out
}
