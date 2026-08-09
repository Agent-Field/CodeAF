// CLI-bridge — port of src/plandb/cli-bridge.ts.
//
// Interprets the `plandb <op> ...` argv the tool layer builds and returns a
// RunResult shaped like a subprocess run of the retired Rust CLI: exit code,
// stdout = JSON.stringify(value, null, 2), stderr = message + "\n". Error
// message TEXT is load-bearing (callers string-match on some of it); every
// message below is byte-identical to the TS original, including the JS
// `undefined` interpolation for missing positionals.
//
// Bug-compat preserved deliberately (see PORT-ASSESSMENT.md):
//   - `contexts` DROPS the --task filter (TS never forwards it to the store)
//   - parseFlags: `--json` vanishes; a flag followed by another `--token` is
//     valueless; repeated flags accumulate; values starting with `--` cannot
//     be expressed.
package plandb

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type RunResult struct {
	Code   int
	Stdout []byte
	Stderr []byte
}

func okResult(value any) RunResult {
	text, err := jscompat.StringifyIndent(value)
	if err != nil {
		return failResult(err.Error())
	}
	return RunResult{Code: 0, Stdout: text, Stderr: []byte{}}
}

func failResult(message string) RunResult {
	return RunResult{Code: 1, Stdout: []byte{}, Stderr: []byte(message + "\n")}
}

type flagMap struct {
	m *jscompat.OrderedMap[string, []string]
}

// parseFlags mirrors parseFlags, quirks included.
func parseFlags(args []string) (positional []string, flags flagMap) {
	flags = flagMap{m: jscompat.NewOrderedMap[string, []string]()}
	positional = []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--json" {
			continue
		}
		if strings.HasPrefix(a, "--") {
			key := a[2:]
			hasVal := i+1 < len(args) && !strings.HasPrefix(args[i+1], "--")
			if hasVal {
				existing, _ := flags.m.Get(key)
				flags.m.Set(key, append(existing, args[i+1]))
				i++
			} else {
				if !flags.m.Has(key) {
					flags.m.Set(key, []string{})
				}
			}
		} else {
			positional = append(positional, a)
		}
	}
	return positional, flags
}

// first mirrors first(): (value, defined). A flag present with no value is
// defined-in-the-map but first() is undefined — callers that need the
// distinction use has().
func (f flagMap) first(key string) (string, bool) {
	vals, ok := f.m.Get(key)
	if !ok || len(vals) == 0 {
		return "", false
	}
	return vals[0], true
}

func (f flagMap) firstOr(key, fallback string) string {
	if v, ok := f.first(key); ok {
		return v
	}
	return fallback
}

func (f flagMap) many(key string) []string {
	vals, _ := f.m.Get(key)
	return vals
}

func (f flagMap) has(key string) bool {
	return f.m.Has(key)
}

// pos returns (value, text-for-interpolation): a missing positional prints as
// the JS `undefined`.
func pos(args []string, i int) (string, string) {
	if i >= len(args) {
		return "", "undefined"
	}
	return args[i], args[i]
}

// argvToAddInput mirrors argvToAddInput.
func argvToAddInput(args []string) AddTaskInput {
	title := ""
	if len(args) > 1 {
		title = args[1]
	}
	var rest []string
	if len(args) > 2 {
		rest = args[2:]
	}
	_, flags := parseFlags(rest)
	tags := flags.many("tag")
	var deps []DepSpec
	for _, spec := range flags.many("dep") {
		parts := strings.Split(spec, ":")
		kind := DepFeedsInto
		if len(parts) > 1 {
			kind = DepKind(parts[1])
		}
		deps = append(deps, DepSpec{TaskID: parts[0], Kind: &kind})
	}
	var desc *string
	if v, ok := flags.first("description"); ok {
		desc = &v
	}
	var priority *float64
	if v, ok := flags.first("priority"); ok && v != "" {
		p := jscompat.ToNumber(v)
		priority = &p
	}
	// TS quirk not reproduced: `--as ""` creates a task with the empty-string
	// id in the original (`"" ?? id("t")` keeps ""); here it falls through to
	// id generation. No sane caller passes it.
	return AddTaskInput{
		Title:       title,
		Description: desc,
		Kind:        TaskKind(flags.firstOr("kind", "")),
		Priority:    priority,
		Parent:      flags.firstOr("parent", ""),
		Project:     flags.firstOr("project", ""),
		Deps:        deps,
		Tags:        tags,
		CustomID:    flags.firstOr("as", ""),
	}
}

func splitCSV(raw string) []string {
	out := []string{}
	for _, s := range strings.Split(raw, ",") {
		t := jscompat.Trim(s)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func agentPtr(flags flagMap) *string {
	if v, ok := flags.first("agent"); ok {
		return &v
	}
	return nil
}

// RunPlanDB mirrors runPlanDB: dispatch a `plandb ...` argv to the native
// store singleton.
func RunPlanDB(argv []string) RunResult {
	_, argv0 := pos(argv, 0)
	if argv0 != "plandb" {
		return failResult("runPlanDB expects argv[0]='plandb', got " + argv0)
	}
	_, op := pos(argv, 1)
	var rest []string
	if len(argv) > 2 {
		rest = argv[2:]
	}
	db := GetPlanDB()

	switch op {
	case "init":
		name, _ := pos(rest, 0)
		if name == "" {
			return failResult("init requires a project name")
		}
		return okResult(db.Init(name))

	case "add":
		input := argvToAddInput(append([]string{"add"}, rest...))
		if input.Title == "" {
			return failResult("add requires a title")
		}
		task, err := db.AddTask(input)
		if err != nil {
			return failResult(err.Error())
		}
		return okResult(task)

	case "list":
		_, flags := parseFlags(rest)
		return okResult(db.ListTasks(&ListTasksFilter{
			Project: flags.firstOr("project", ""),
			Status:  flags.firstOr("status", ""),
			Kind:    flags.firstOr("kind", ""),
			Tag:     flags.firstOr("tag", ""),
			Parent:  flags.firstOr("parent", ""),
		}))

	case "show":
		taskID, taskText := pos(rest, 0)
		task := db.GetTask(taskID)
		if task == nil {
			return failResult("task not found: " + taskText)
		}
		return okResult(task)

	case "task":
		subop, subopText := pos(rest, 0)
		var subargs []string
		if len(rest) > 1 {
			subargs = rest[1:]
		}
		switch subop {
		case "claim":
			taskID, taskText := pos(subargs, 0)
			_, flags := parseFlags(sliceFrom(subargs, 1))
			agent := flags.firstOr("agent", "codeaf")
			task := db.ClaimTask(taskID, agent)
			if task == nil {
				return failResult(fmt.Sprintf("could not claim task %s (not ready, or already claimed)", taskText))
			}
			return okResult(task)
		case "start":
			taskID, taskText := pos(subargs, 0)
			task := db.StartTask(taskID)
			if task == nil {
				return failResult(fmt.Sprintf("could not start task %s (must be claimed first)", taskText))
			}
			return okResult(task)
		case "release":
			taskID, taskText := pos(subargs, 0)
			if taskID == "" {
				return failResult("task release requires id")
			}
			task := db.ReleaseTask(taskID)
			if task == nil {
				return failResult(fmt.Sprintf("could not release task %s (not claimed or running)", taskText))
			}
			return okResult(task)
		case "reopen":
			taskID, taskText := pos(subargs, 0)
			if taskID == "" {
				return failResult("task reopen requires id")
			}
			task, err := db.ReopenToPending(taskID)
			if err != nil {
				return failResult(err.Error())
			}
			if task == nil {
				return failResult("task not found: " + taskText)
			}
			return okResult(task)
		case "amend":
			taskID, taskText := pos(subargs, 0)
			_, flags := parseFlags(sliceFrom(subargs, 1))
			content, hasContent := flags.first("prepend")
			if !hasContent {
				content, _ = flags.first("append")
			}
			position := "append"
			if flags.has("prepend") {
				position = "prepend"
			}
			if content == "" {
				return failResult("amend requires --prepend or --append content")
			}
			task, err := db.AmendTask(taskID, content, position)
			if err != nil {
				return failResult(err.Error())
			}
			if task == nil {
				return failResult("task not found: " + taskText)
			}
			return okResult(task)
		case "insert":
			_, flags := parseFlags(subargs)
			after, _ := flags.first("after")
			before, _ := flags.first("before")
			title, _ := flags.first("title")
			if after == "" || title == "" {
				return failResult("insert requires --after and --title")
			}
			var desc *string
			if v, ok := flags.first("description"); ok {
				desc = &v
			}
			task, err := db.InsertTask(after, before, AddTaskInput{
				Title:       title,
				Description: desc,
				Project:     flags.firstOr("project", ""),
			})
			if err != nil {
				return failResult(err.Error())
			}
			return okResult(task)
		case "update":
			taskID, taskText := pos(subargs, 0)
			_, flags := parseFlags(sliceFrom(subargs, 1))
			fields := UpdateFields{}
			if v, ok := flags.first("title"); ok {
				fields.Title = &v
			}
			if v, ok := flags.first("description"); ok {
				fields.Description = &v
			}
			if v, ok := flags.first("kind"); ok {
				k := TaskKind(v)
				fields.Kind = &k
			}
			if v, ok := flags.first("priority"); ok {
				p := jscompat.ToNumber(v)
				fields.Priority = &p
			}
			task := db.UpdateTask(taskID, fields)
			if task == nil {
				return failResult("task not found: " + taskText)
			}
			return okResult(task)
		case "overview":
			_, flags := parseFlags(subargs)
			return okResult(db.Overview(flags.firstOr("project", "")))
		case "fail":
			taskID, taskText := pos(subargs, 0)
			_, flags := parseFlags(sliceFrom(subargs, 1))
			errMsg := flags.firstOr("error", "task failed")
			task := db.FailTask(taskID, errMsg, agentPtr(flags))
			if task == nil {
				return failResult("task not found: " + taskText)
			}
			return okResult(task)
		case "cancel":
			taskID, taskText := pos(subargs, 0)
			task := db.CancelTask(taskID)
			if task == nil {
				return failResult("task not found: " + taskText)
			}
			return okResult(task)
		case "partial":
			taskID, taskText := pos(subargs, 0)
			_, flags := parseFlags(sliceFrom(subargs, 1))
			if taskID == "" {
				return failResult("task partial requires id")
			}
			opts := DoneOpts{Agent: agentPtr(flags)}
			if v, ok := flags.first("result"); ok {
				opts.Result = v
			}
			if filesRaw, ok := flags.first("files"); ok {
				opts.Files = splitCSV(filesRaw)
			}
			task, err := db.DonePartialTask(taskID, opts)
			if err != nil {
				return failResult(err.Error())
			}
			if task == nil {
				return failResult("task not found: " + taskText)
			}
			return okResult(task)
		case "add-many":
			return runAddMany(db, subargs)
		}
		return failResult("unknown task subop: " + subopText)

	case "done":
		taskID, taskText := pos(rest, 0)
		_, flags := parseFlags(sliceFrom(rest, 1))
		opts := DoneOpts{Agent: agentPtr(flags)}
		if v, ok := flags.first("result"); ok {
			opts.Result = v
		}
		if filesCsv, ok := flags.first("files"); ok {
			opts.Files = splitCSV(filesCsv)
		}
		task, err := db.DoneTask(taskID, opts)
		if err != nil {
			return failResult(err.Error())
		}
		if task == nil {
			return failResult("task not found: " + taskText)
		}
		return okResult(task)

	case "context":
		content, _ := pos(rest, 0)
		_, flags := parseFlags(sliceFrom(rest, 1))
		if content == "" {
			return failResult("context requires content")
		}
		return okResult(db.AddContext(content, AddContextOpts{
			Kind:    flags.firstOr("kind", ""),
			TaskID:  flags.firstOr("task", ""),
			Project: flags.firstOr("project", ""),
		}))

	case "contexts":
		_, flags := parseFlags(rest)
		limit := 0.0
		if v, ok := flags.first("limit"); ok && v != "" {
			limit = jscompat.ToNumber(v)
		}
		// Bug-compat: the --task filter is DROPPED, exactly like the TS bridge
		// (fix-generator's readFrozenLeaves depends on the project-wide view).
		return okResult(db.ListContexts(&ListContextsFilter{
			Project: flags.firstOr("project", ""),
			Kind:    flags.firstOr("kind", ""),
			Limit:   limit,
		}))

	case "search":
		query, _ := pos(rest, 0)
		_, flags := parseFlags(sliceFrom(rest, 1))
		if query == "" {
			return failResult("search requires a query")
		}
		limit := 0.0
		if v, ok := flags.first("limit"); ok && v != "" {
			limit = jscompat.ToNumber(v)
		}
		return okResult(db.Search(query, flags.firstOr("project", ""), limit))

	case "split":
		positional, flags := parseFlags(rest)
		into, _ := flags.first("into")
		taskID := ""
		if len(positional) > 0 {
			taskID = positional[0]
		}
		if taskID == "" || into == "" {
			return failResult("split requires taskId and --into spec")
		}
		tasks, err := db.SplitTask(taskID, into)
		if err != nil {
			return failResult(err.Error())
		}
		return okResult(tasks)

	case "status":
		_, flags := parseFlags(rest)
		project := flags.firstOr("project", "")
		return okResult(struct {
			Project *Project `json:"project"`
			Status  Status   `json:"status"`
			Tasks   []*Task  `json:"tasks"`
		}{
			Project: db.ResolveProject(project),
			Status:  db.Status(project),
			Tasks:   db.ListTasks(&ListTasksFilter{Project: project}),
		})

	case "project":
		subop, subopText := pos(rest, 0)
		if subop == "dag" {
			name := ""
			if len(rest) > 1 && !strings.HasPrefix(rest[1], "--") {
				name = rest[1]
			}
			return okResult(db.ProjectDag(name))
		}
		if subop == "list" {
			return okResult(db.ListProjects())
		}
		return failResult("unknown project subop: " + subopText)

	case "critical-path":
		_, flags := parseFlags(rest)
		return okResult(db.CriticalPath(flags.firstOr("project", "")))

	case "bottlenecks":
		_, flags := parseFlags(rest)
		return okResult(db.Bottlenecks(flags.firstOr("project", "")))

	case "what-unlocks":
		taskID, _ := pos(rest, 0)
		if taskID == "" {
			return failResult("what-unlocks requires taskId")
		}
		return okResult(db.WhatUnlocks(taskID))

	case "ahead":
		_, flags := parseFlags(rest)
		depth := 3.0
		if v, ok := flags.first("depth"); ok && v != "" {
			depth = jscompat.ToNumber(v)
		}
		return okResult(db.Ahead(depth, flags.firstOr("project", "")))
	}

	return failResult("unknown op: " + op)
}

func sliceFrom(args []string, i int) []string {
	if i >= len(args) {
		return nil
	}
	return args[i:]
}

// runAddMany mirrors the "task add-many" handler: topo-sorted transactional
// ingestion of a planner-translate payload.
func runAddMany(db *PlanDB, subargs []string) RunResult {
	_, flags := parseFlags(subargs)
	payloadRaw, ok := flags.first("payload")
	if !ok || payloadRaw == "" {
		return failResult("add-many requires --payload <json>")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		return failResult(fmt.Sprintf("add-many payload is not valid JSON: %v", err))
	}
	parent := ""
	if p, ok := payload["parent"].(string); ok {
		parent = p
	}
	tasksAny, _ := payload["tasks"].([]any)
	if len(tasksAny) == 0 {
		return failResult("add-many requires payload.tasks: non-empty array")
	}
	type rawTask = map[string]any
	tasks := make([]rawTask, 0, len(tasksAny))
	for _, t := range tasksAny {
		m, _ := t.(rawTask)
		tasks = append(tasks, m)
	}
	str := func(m rawTask, k string) string {
		if m == nil {
			return ""
		}
		s, _ := m[k].(string)
		return s
	}
	for _, t := range tasks {
		if str(t, "taskKey") == "" || str(t, "title") == "" {
			b, _ := jscompat.Stringify(t)
			s := string(b)
			if len(s) > 120 {
				s = s[:120]
			}
			return failResult("add-many task missing taskKey or title: " + s)
		}
	}

	// Topo-sort via Kahn's algorithm. from_task → consumer task edges.
	inDeg := jscompat.NewOrderedMap[string, int]()
	adj := jscompat.NewOrderedMap[string, []string]()
	byKey := jscompat.NewOrderedMap[string, rawTask]()
	for _, t := range tasks {
		k := str(t, "taskKey")
		byKey.Set(k, t)
		inDeg.Set(k, 0)
		adj.Set(k, []string{})
	}
	depsOf := func(t rawTask) []rawTask {
		arr, _ := t["deps"].([]any)
		out := make([]rawTask, 0, len(arr))
		for _, d := range arr {
			m, _ := d.(rawTask)
			out = append(out, m)
		}
		return out
	}
	for _, t := range tasks {
		key := str(t, "taskKey")
		for _, d := range depsOf(t) {
			upstream := str(d, "from_task")
			upstreamText := upstream
			if upstream == "" {
				upstreamText = "undefined"
			}
			if upstream == "" || !byKey.Has(upstream) {
				// Dangling edge — fail loud rather than silently skip.
				return failResult(fmt.Sprintf("task %s references unknown from_task: %s", key, upstreamText))
			}
			existing, _ := adj.Get(upstream)
			adj.Set(upstream, append(existing, key))
			deg, _ := inDeg.Get(key)
			inDeg.Set(key, deg+1)
		}
	}
	queue := []string{}
	for _, e := range inDeg.Entries() {
		if e.Val == 0 {
			queue = append(queue, e.Key)
		}
	}
	order := []rawTask{}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		if t, ok := byKey.Get(k); ok {
			order = append(order, t)
		}
		next, _ := adj.Get(k)
		for _, n := range next {
			deg, _ := inDeg.Get(n)
			inDeg.Set(n, deg-1)
			if deg-1 == 0 {
				queue = append(queue, n)
			}
		}
	}
	if len(order) != len(tasks) {
		return failResult(fmt.Sprintf("add-many: cycle detected in deps (resolved %d/%d tasks)", len(order), len(tasks)))
	}

	// Insert in topo order. Each task's inline deps resolve via keyToID.
	keyToID := map[string]string{}
	inserted := []*Task{}
	for _, t := range order {
		var resolved []DepSpec
		for _, d := range depsOf(t) {
			realID, ok := keyToID[str(d, "from_task")]
			if !ok {
				// Should never hit — topo-sort guarantees upstream is inserted.
				return failResult(fmt.Sprintf("internal: from_task %s not yet inserted for %s", str(d, "from_task"), str(t, "taskKey")))
			}
			kind := DepFeedsInto
			if k, isStr := d["kind"].(string); isStr {
				// "" is preserved, mirroring the nullish-only `?? "feeds_into"`.
				kind = DepKind(k)
			}
			resolved = append(resolved, DepSpec{TaskID: realID, Kind: &kind})
		}
		input := AddTaskInput{
			Title:  str(t, "title"),
			Kind:   TaskKind(str(t, "kind")),
			Parent: parent,
			Deps:   resolved,
		}
		if desc, ok := t["description"].(string); ok {
			input.Description = &desc
		}
		if tagsArr, ok := t["tags"].([]any); ok {
			tags := []string{}
			for _, tag := range tagsArr {
				if s, isStr := tag.(string); isStr {
					tags = append(tags, s)
				}
			}
			input.Tags = tags
		}
		created, err := db.AddTask(input)
		if err != nil {
			return failResult(err.Error())
		}
		keyToID[str(t, "taskKey")] = created.ID
		inserted = append(inserted, created)
	}
	return okResult(inserted)
}
