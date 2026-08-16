// Port of src/plandb/store.test.ts (bun:test) — 1:1 translation.
//
// Structure mirrors the TS file exactly: each Go top-level func corresponds to
// one `describe(...)` block, whose verbatim name is the outer t.Run, and each
// inner t.Run carries the verbatim `test(...)` name so coverage can be diffed
// against the TS source line by line.
//
// TS `beforeEach(() => { db = new PlanDB() })` becomes a fresh NewPlanDB() at
// the top of every subtest.
package plandb

import (
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ── helpers (prefixed `storeT` to avoid collisions with sibling _test.go files) ──

func storeTAdd(t *testing.T, db *PlanDB, in AddTaskInput) *Task {
	t.Helper()
	task, err := db.AddTask(in)
	if err != nil {
		t.Fatalf("AddTask(%q): unexpected error: %v", in.Title, err)
	}
	if task == nil {
		t.Fatalf("AddTask(%q): returned nil task", in.Title)
	}
	return task
}

func storeTDone(t *testing.T, db *PlanDB, tid TaskID) *Task {
	t.Helper()
	task, err := db.DoneTask(tid, DoneOpts{})
	if err != nil {
		t.Fatalf("DoneTask(%s): unexpected error: %v", tid, err)
	}
	return task
}

func storeTGet(t *testing.T, db *PlanDB, tid TaskID) *Task {
	t.Helper()
	task := db.GetTask(tid)
	if task == nil {
		t.Fatalf("GetTask(%s): returned nil (TS non-null assertion)", tid)
	}
	return task
}

func storeTDepKind(k DepKind) *DepKind { return &k }

func storeTStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func storeTTaskIDs(tasks []*Task) []string {
	out := []string{}
	for _, task := range tasks {
		out = append(out, task.ID)
	}
	return out
}

func storeTTaskTitles(tasks []*Task) []string {
	out := []string{}
	for _, task := range tasks {
		out = append(out, task.Title)
	}
	return out
}

func storeTDepFroms(deps []Dependency) []string {
	out := []string{}
	for _, d := range deps {
		out = append(out, d.FromTask)
	}
	return out
}

func storeTCoalesceCode(t *testing.T, err error) CoalesceErrorCode {
	t.Helper()
	var ce *CoalesceError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *CoalesceError, got %T (%v)", err, err)
	}
	return ce.Code
}

// ── describe("PlanDB.init + projects") ───────────────────────────────────

func TestStorePlanDBInitProjects(t *testing.T) {
	t.Run("PlanDB.init + projects", func(t *testing.T) {
		t.Run("creates a project on first init", func(t *testing.T) {
			db := NewPlanDB()
			p := db.Init("demo")
			if !regexp.MustCompile(`^p-[a-z0-9]+$`).MatchString(p.ID) {
				t.Errorf("project id %q does not match /^p-[a-z0-9]+$/", p.ID)
			}
			if p.Name != "demo" {
				t.Errorf("name = %q, want %q", p.Name, "demo")
			}
			if p.Status != "active" {
				t.Errorf("status = %q, want %q", p.Status, "active")
			}
		})

		t.Run("re-init returns the existing project (idempotent)", func(t *testing.T) {
			db := NewPlanDB()
			a := db.Init("demo")
			b := db.Init("demo")
			if a.ID != b.ID {
				t.Errorf("a.id = %q, b.id = %q; want identical", a.ID, b.ID)
			}
		})

		t.Run("listProjects returns all projects", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("a")
			db.Init("b")
			names := []string{}
			for _, p := range db.ListProjects() {
				names = append(names, p.Name)
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, []string{"a", "b"}) {
				t.Errorf("project names = %v, want [a b]", names)
			}
		})
	})
}

// ── describe("PlanDB.addTask + status progression") ──────────────────────

func TestStorePlanDBAddTaskStatusProgression(t *testing.T) {
	t.Run("PlanDB.addTask + status progression", func(t *testing.T) {
		t.Run("orphan task becomes ready immediately", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			task := storeTAdd(t, db, AddTaskInput{Title: "first", Project: "demo"})
			if task.Status != StatusReady {
				t.Errorf("status = %q, want %q", task.Status, StatusReady)
			}
			projects := db.ListProjects()
			if len(projects) == 0 {
				t.Fatal("listProjects() is empty")
			}
			if task.ProjectID != projects[0].ID {
				t.Errorf("project_id = %q, want %q", task.ProjectID, projects[0].ID)
			}
			if !regexp.MustCompile(`^t-[a-z0-9]+$`).MatchString(task.ID) {
				t.Errorf("task id %q does not match /^t-[a-z0-9]+$/", task.ID)
			}
		})

		t.Run("task with unmet dep stays pending until upstream is done", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Deps: []DepSpec{{TaskID: a.ID}}})
			if b.Status != StatusPending {
				t.Errorf("b.status = %q, want %q", b.Status, StatusPending)
			}

			db.ClaimTask(a.ID, "worker-1")
			storeTDone(t, db, a.ID)
			if got := db.GetTask(b.ID); got == nil || got.Status != StatusReady {
				t.Errorf("b.status after a done = %v, want %q", got, StatusReady)
			}
		})

		t.Run("dep with kind=suggests doesn't block downstream", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			b := storeTAdd(t, db, AddTaskInput{
				Title:   "b",
				Project: "demo",
				Deps:    []DepSpec{{TaskID: a.ID, Kind: storeTDepKind(DepSuggests)}},
			})
			if b.Status != StatusReady { // suggests is soft
				t.Errorf("b.status = %q, want %q", b.Status, StatusReady)
			}
		})

		t.Run("addTask with unknown parent throws", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			_, err := db.AddTask(AddTaskInput{Title: "x", Project: "demo", Parent: "t-nope"})
			if err == nil {
				t.Fatal("expected AddTask with unknown parent to return an error")
			}
		})

		t.Run("adding a child marks parent as composite", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "parent", Project: "demo"})
			if parent.IsComposite {
				t.Errorf("parent.is_composite = true, want false")
			}
			storeTAdd(t, db, AddTaskInput{Title: "child", Project: "demo", Parent: parent.ID})
			if !storeTGet(t, db, parent.ID).IsComposite {
				t.Errorf("parent.is_composite after child = false, want true")
			}
		})

		t.Run("composite parent auto-completes when all children done", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "parent", Project: "demo"})
			c1 := storeTAdd(t, db, AddTaskInput{Title: "c1", Project: "demo", Parent: parent.ID})
			c2 := storeTAdd(t, db, AddTaskInput{Title: "c2", Project: "demo", Parent: parent.ID})
			storeTDone(t, db, c1.ID)
			if got := storeTGet(t, db, parent.ID).Status; got == StatusDone {
				t.Errorf("parent.status = %q, want anything but %q", got, StatusDone)
			}
			storeTDone(t, db, c2.ID)
			if got := storeTGet(t, db, parent.ID).Status; got != StatusDone {
				t.Errorf("parent.status = %q, want %q", got, StatusDone)
			}
		})
	})
}

// ── describe("PlanDB.claimTask atomicity") ───────────────────────────────

func TestStorePlanDBClaimTaskAtomicity(t *testing.T) {
	t.Run("PlanDB.claimTask atomicity", func(t *testing.T) {
		t.Run("two claims on the same task — only one wins", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			task := storeTAdd(t, db, AddTaskInput{Title: "race", Project: "demo"})
			a := db.ClaimTask(task.ID, "agent-A")
			b := db.ClaimTask(task.ID, "agent-B")
			if a == nil {
				t.Error("first claim = nil, want non-nil")
			}
			if b != nil { // already claimed by A
				t.Error("second claim = non-nil, want nil")
			}
			if got := storeTStr(storeTGet(t, db, task.ID).AgentID); got != "agent-A" {
				t.Errorf("agent_id = %q, want %q", got, "agent-A")
			}
		})

		t.Run("claim respects ready/pending only", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			task := storeTAdd(t, db, AddTaskInput{Title: "x", Project: "demo"})
			db.ClaimTask(task.ID, "agent-A")
			storeTDone(t, db, task.ID)
			// Cannot claim a done task.
			if got := db.ClaimTask(task.ID, "agent-B"); got != nil {
				t.Errorf("claim on done task = %+v, want nil", got)
			}
		})

		t.Run("same agent re-claiming the same task is a no-op success", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			task := storeTAdd(t, db, AddTaskInput{Title: "x", Project: "demo"})
			db.ClaimTask(task.ID, "agent-A")
			// Re-claim by the same agent is rejected (status no longer ready/pending).
			// This is intentional: prevent double-claim attempts from triggering work.
			if got := db.ClaimTask(task.ID, "agent-A"); got != nil {
				t.Errorf("re-claim = %+v, want nil", got)
			}
		})
	})
}

// ── describe("PlanDB.doneTask blocked by open descendants") ──────────────

func TestStorePlanDBDoneTaskBlockedByOpenDescendants(t *testing.T) {
	t.Run("PlanDB.doneTask blocked by open descendants", func(t *testing.T) {
		t.Run("done on composite with open child throws", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			storeTAdd(t, db, AddTaskInput{Title: "c", Project: "demo", Parent: parent.ID})
			_, err := db.DoneTask(parent.ID, DoneOpts{})
			if err == nil {
				t.Fatal("expected DoneTask on composite with open child to return an error")
			}
			if !strings.Contains(err.Error(), "descendant") {
				t.Errorf("error = %q, want it to contain %q", err.Error(), "descendant")
			}
		})

		t.Run("done on composite after all children done succeeds", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			c := storeTAdd(t, db, AddTaskInput{Title: "c", Project: "demo", Parent: parent.ID})
			storeTDone(t, db, c.ID)
			// Parent auto-completes via maybeCompleteComposite, so explicit done() is a no-op.
			if got := storeTGet(t, db, parent.ID).Status; got != StatusDone {
				t.Errorf("parent.status = %q, want %q", got, StatusDone)
			}
		})
	})
}

// ── describe("PlanDB.listTasks filters") ─────────────────────────────────

func TestStorePlanDBListTasksFilters(t *testing.T) {
	t.Run("PlanDB.listTasks filters", func(t *testing.T) {
		t.Run("by status", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Deps: []DepSpec{{TaskID: a.ID}}})
			if got := storeTTaskIDs(db.ListTasks(&ListTasksFilter{Project: "demo", Status: "ready"})); !reflect.DeepEqual(got, []string{a.ID}) {
				t.Errorf("ready ids = %v, want [%s]", got, a.ID)
			}
			if got := storeTTaskIDs(db.ListTasks(&ListTasksFilter{Project: "demo", Status: "pending"})); !reflect.DeepEqual(got, []string{b.ID}) {
				t.Errorf("pending ids = %v, want [%s]", got, b.ID)
			}
		})

		t.Run("by kind", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Kind: "code"})
			storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Kind: "test"})
			if got := storeTTaskTitles(db.ListTasks(&ListTasksFilter{Project: "demo", Kind: "test"})); !reflect.DeepEqual(got, []string{"b"}) {
				t.Errorf("kind=test titles = %v, want [b]", got)
			}
		})

		t.Run("by tag (composed from access/parallel/role flags)", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Access: "write", TaskRole: "implementation"})
			storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Access: "read"})
			if got := storeTTaskTitles(db.ListTasks(&ListTasksFilter{Project: "demo", Tag: "access:write"})); !reflect.DeepEqual(got, []string{"a"}) {
				t.Errorf("tag=access:write titles = %v, want [a]", got)
			}
			if got := storeTTaskTitles(db.ListTasks(&ListTasksFilter{Project: "demo", Tag: "role:implementation"})); !reflect.DeepEqual(got, []string{"a"}) {
				t.Errorf("tag=role:implementation titles = %v, want [a]", got)
			}
		})
	})
}

// ── describe("PlanDB.status counts") ─────────────────────────────────────

func TestStorePlanDBStatusCounts(t *testing.T) {
	t.Run("PlanDB.status counts", func(t *testing.T) {
		t.Run("aggregates by status", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Deps: []DepSpec{{TaskID: a.ID}}})
			storeTAdd(t, db, AddTaskInput{Title: "c", Project: "demo"})
			db.ClaimTask(a.ID, "x")
			s := db.Status("demo")
			if s.Total != 3 {
				t.Errorf("total = %d, want 3", s.Total)
			}
			if s.Claimed != 1 {
				t.Errorf("claimed = %d, want 1", s.Claimed)
			}
			if s.Ready != 1 { // c
				t.Errorf("ready = %d, want 1", s.Ready)
			}
			if s.Pending != 1 { // b
				t.Errorf("pending = %d, want 1", s.Pending)
			}
			if s.Done != 0 {
				t.Errorf("done = %d, want 0", s.Done)
			}
		})
	})
}

// ── describe("PlanDB.splitTask") ─────────────────────────────────────────

func TestStorePlanDBSplitTask(t *testing.T) {
	t.Run("PlanDB.splitTask", func(t *testing.T) {
		t.Run("parallel split (comma-separated) creates independent siblings", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			children, err := db.SplitTask(parent.ID, "A, B, C")
			if err != nil {
				t.Fatalf("SplitTask: unexpected error: %v", err)
			}
			if len(children) != 3 {
				t.Fatalf("len(children) = %d, want 3", len(children))
			}
			for _, c := range children {
				if c.ParentTaskID == nil || *c.ParentTaskID != parent.ID {
					t.Errorf("child %s parent_task_id = %v, want %s", c.ID, c.ParentTaskID, parent.ID)
				}
			}
			// No deps between siblings → all ready.
			for _, c := range children {
				if c.Status != StatusReady {
					t.Errorf("child %s status = %q, want %q", c.ID, c.Status, StatusReady)
				}
			}
		})

		t.Run("sequential split (>) creates a chain", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			children, err := db.SplitTask(parent.ID, "A > B > C")
			if err != nil {
				t.Fatalf("SplitTask: unexpected error: %v", err)
			}
			if len(children) != 3 {
				t.Fatalf("len(children) = %d, want 3", len(children))
			}
			if children[0].Status != StatusReady {
				t.Errorf("children[0].status = %q, want %q", children[0].Status, StatusReady)
			}
			if children[1].Status != StatusPending {
				t.Errorf("children[1].status = %q, want %q", children[1].Status, StatusPending)
			}
			if children[2].Status != StatusPending {
				t.Errorf("children[2].status = %q, want %q", children[2].Status, StatusPending)
			}
		})

		t.Run("mechanical concurrency split preserves per-child descriptions and scopes", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			children, err := db.SplitTaskWithSpecs(parent.ID, []SplitSpec{
				{Title: "p [a]", Description: "file_scope: src/a.ts\nImplement A", Tags: []string{"auto:scheduler"}},
				{Title: "p [b]", Description: "file_scope: src/b.ts\nImplement B", Tags: []string{"auto:scheduler"}},
			})
			if err != nil {
				t.Fatalf("SplitTaskWithSpecs: unexpected error: %v", err)
			}
			if len(children) != 2 {
				t.Fatalf("len(children) = %d, want 2", len(children))
			}
			for _, c := range children {
				if c.Status != StatusReady {
					t.Errorf("child %s status = %q, want %q", c.ID, c.Status, StatusReady)
				}
			}
			descs := []string{}
			for _, c := range children {
				descs = append(descs, storeTStr(c.Description))
			}
			want := []string{
				"file_scope: src/a.ts\nImplement A",
				"file_scope: src/b.ts\nImplement B",
			}
			if !reflect.DeepEqual(descs, want) {
				t.Errorf("descriptions = %q, want %q", descs, want)
			}
			for _, c := range children {
				if !contains(c.Tags, "auto:scheduler") {
					t.Errorf("child %s tags = %v, want it to include auto:scheduler", c.ID, c.Tags)
				}
			}
		})
	})
}

// ── describe("PlanDB.coalesceTasks") ─────────────────────────────────────

func TestStorePlanDBCoalesceTasks(t *testing.T) {
	t.Run("PlanDB.coalesceTasks", func(t *testing.T) {
		t.Run("happy path: merges siblings into one new task, cancels members", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			a := storeTAdd(t, db, AddTaskInput{Title: "add helper", Project: "demo", Parent: parent.ID, Tags: []string{"scope:small"}})
			b := storeTAdd(t, db, AddTaskInput{Title: "add test", Project: "demo", Parent: parent.ID, Tags: []string{"scope:small"}})
			merged, err := db.CoalesceTasks([]TaskID{a.ID, b.ID}, CoalesceTasksOpts{})
			if err != nil {
				t.Fatalf("CoalesceTasks: unexpected error: %v", err)
			}

			if merged.ParentTaskID == nil || *merged.ParentTaskID != parent.ID {
				t.Errorf("merged.parent_task_id = %v, want %s", merged.ParentTaskID, parent.ID)
			}
			if merged.Status != StatusReady { // no external deps
				t.Errorf("merged.status = %q, want %q", merged.Status, StatusReady)
			}
			if merged.Title != "add helper + add test" {
				t.Errorf("merged.title = %q, want %q", merged.Title, "add helper + add test")
			}
			if !strings.Contains(storeTStr(merged.Description), "Coalesced from 2 sibling tasks") {
				t.Errorf("merged.description = %q, want it to contain %q", storeTStr(merged.Description), "Coalesced from 2 sibling tasks")
			}
			// members are cancelled with a coalesced context entry
			if got := storeTGet(t, db, a.ID).Status; got != StatusCancelled {
				t.Errorf("a.status = %q, want %q", got, StatusCancelled)
			}
			if got := storeTGet(t, db, b.ID).Status; got != StatusCancelled {
				t.Errorf("b.status = %q, want %q", got, StatusCancelled)
			}
			ctxs := db.ListContexts(&ListContextsFilter{Project: "demo", Kind: "coalesced"})
			if len(ctxs) != 2 {
				t.Fatalf("len(coalesced contexts) = %d, want 2", len(ctxs))
			}
			for _, c := range ctxs {
				if !strings.Contains(c.Content, merged.ID) {
					t.Errorf("context content %q does not contain merged id %s", c.Content, merged.ID)
				}
			}
			// stale scope tags dropped
			for _, tag := range merged.Tags {
				if strings.HasPrefix(tag, "scope:") {
					t.Errorf("merged.tags = %v, want no scope:* tag", merged.Tags)
					break
				}
			}
		})

		t.Run("tags = union of members (minus stale scope:*), scopeTagFor recomputes scope", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Parent: parent.ID, Access: "write", Tags: []string{"scope:small"}})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Parent: parent.ID, TaskRole: "implementation", Tags: []string{"scope:small"}})
			merged, err := db.CoalesceTasks([]TaskID{a.ID, b.ID}, CoalesceTasksOpts{
				FileScope:   []string{"src/x.ts", "src/y.ts"},
				ScopeTagFor: func(string) string { return "scope:medium" },
			})
			if err != nil {
				t.Fatalf("CoalesceTasks: unexpected error: %v", err)
			}
			for _, want := range []string{"access:write", "role:implementation", "scope:medium"} {
				if !contains(merged.Tags, want) {
					t.Errorf("merged.tags = %v, want it to contain %q", merged.Tags, want)
				}
			}
			scopeTags := []string{}
			for _, tag := range merged.Tags {
				if strings.HasPrefix(tag, "scope:") {
					scopeTags = append(scopeTags, tag)
				}
			}
			if !reflect.DeepEqual(scopeTags, []string{"scope:medium"}) {
				t.Errorf("scope:* tags = %v, want [scope:medium]", scopeTags)
			}
			if !strings.Contains(storeTStr(merged.Description), "file_scope: src/x.ts, src/y.ts") {
				t.Errorf("merged.description = %q, want it to contain %q", storeTStr(merged.Description), "file_scope: src/x.ts, src/y.ts")
			}
		})

		t.Run("external deps become the merged task's deps (union), internal deps dropped", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			up := storeTAdd(t, db, AddTaskInput{Title: "upstream", Project: "demo"})
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			// a depends on external `up`; b depends on a (internal to the group).
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Parent: parent.ID, Deps: []DepSpec{{TaskID: up.ID}}})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Parent: parent.ID, Deps: []DepSpec{{TaskID: a.ID}}})
			merged, err := db.CoalesceTasks([]TaskID{a.ID, b.ID}, CoalesceTasksOpts{})
			if err != nil {
				t.Fatalf("CoalesceTasks: unexpected error: %v", err)
			}
			// merged inherits the external dep on `up` → still pending until up is done.
			if merged.Status != StatusPending {
				t.Errorf("merged.status = %q, want %q", merged.Status, StatusPending)
			}
			if got := storeTDepFroms(db.DepsOfTask(merged.ID)); !reflect.DeepEqual(got, []string{up.ID}) {
				t.Errorf("depsOfTask(merged) from_task = %v, want [%s]", got, up.ID)
			}
			db.ClaimTask(up.ID, "w")
			storeTDone(t, db, up.ID)
			if got := storeTGet(t, db, merged.ID).Status; got != StatusReady {
				t.Errorf("merged.status after up done = %q, want %q", got, StatusReady)
			}
		})

		t.Run("dependents rewire: a downstream depending on any member now depends on the merged task", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Parent: parent.ID})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Parent: parent.ID})
			// `down` depends on BOTH members — must end up depending on merged ONCE.
			down := storeTAdd(t, db, AddTaskInput{
				Title:   "down",
				Project: "demo",
				Deps:    []DepSpec{{TaskID: a.ID}, {TaskID: b.ID}},
			})
			if down.Status != StatusPending {
				t.Errorf("down.status = %q, want %q", down.Status, StatusPending)
			}
			merged, err := db.CoalesceTasks([]TaskID{a.ID, b.ID}, CoalesceTasksOpts{})
			if err != nil {
				t.Fatalf("CoalesceTasks: unexpected error: %v", err)
			}
			downDeps := storeTDepFroms(db.DepsOfTask(down.ID))
			if !reflect.DeepEqual(downDeps, []string{merged.ID}) { // deduped to the single merged task
				t.Errorf("depsOfTask(down) from_task = %v, want [%s]", downDeps, merged.ID)
			}
			// completing the merged task unblocks down
			db.ClaimTask(merged.ID, "w")
			storeTDone(t, db, merged.ID)
			if got := storeTGet(t, db, down.ID).Status; got != StatusReady {
				t.Errorf("down.status = %q, want %q", got, StatusReady)
			}
		})

		t.Run("rejects fewer than two tasks", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			_, err := db.CoalesceTasks([]TaskID{a.ID}, CoalesceTasksOpts{})
			if err == nil {
				t.Fatal("expected a CoalesceError")
			}
			var ce *CoalesceError
			if !errors.As(err, &ce) {
				t.Fatalf("expected *CoalesceError, got %T (%v)", err, err)
			}
			_, err2 := db.CoalesceTasks([]TaskID{a.ID}, CoalesceTasksOpts{})
			if got := storeTCoalesceCode(t, err2); got != CoalesceTooFew {
				t.Errorf("code = %q, want %q", got, CoalesceTooFew)
			}
		})

		t.Run("rejects duplicate ids", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Parent: parent.ID})
			_, err := db.CoalesceTasks([]TaskID{a.ID, a.ID}, CoalesceTasksOpts{})
			if err == nil {
				t.Fatal("should have thrown")
			}
			if got := storeTCoalesceCode(t, err); got != CoalesceDuplicateID {
				t.Errorf("code = %q, want %q", got, CoalesceDuplicateID)
			}
		})

		t.Run("rejects unknown ids", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Parent: parent.ID})
			_, err := db.CoalesceTasks([]TaskID{a.ID, "t-nope"}, CoalesceTasksOpts{})
			if err == nil {
				t.Fatal("should have thrown")
			}
			if got := storeTCoalesceCode(t, err); got != CoalesceNotFound {
				t.Errorf("code = %q, want %q", got, CoalesceNotFound)
			}
		})

		t.Run("rejects tasks with different parents", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			p1 := storeTAdd(t, db, AddTaskInput{Title: "p1", Project: "demo"})
			p2 := storeTAdd(t, db, AddTaskInput{Title: "p2", Project: "demo"})
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Parent: p1.ID})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Parent: p2.ID})
			_, err := db.CoalesceTasks([]TaskID{a.ID, b.ID}, CoalesceTasksOpts{})
			if err == nil {
				t.Fatal("should have thrown")
			}
			if got := storeTCoalesceCode(t, err); got != CoalesceMixedParents {
				t.Errorf("code = %q, want %q", got, CoalesceMixedParents)
			}
		})

		t.Run("rejects a task that is not pending/ready (e.g. claimed)", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Parent: parent.ID})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Parent: parent.ID})
			db.ClaimTask(a.ID, "w")
			_, err := db.CoalesceTasks([]TaskID{a.ID, b.ID}, CoalesceTasksOpts{})
			if err == nil {
				t.Fatal("should have thrown")
			}
			if got := storeTCoalesceCode(t, err); got != CoalesceNotMergeableStatus {
				t.Errorf("code = %q, want %q", got, CoalesceNotMergeableStatus)
			}
		})

		t.Run("coalescing all children does not auto-complete the parent (merged is an open child)", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			parent := storeTAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo", Parent: parent.ID})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Parent: parent.ID})
			if _, err := db.CoalesceTasks([]TaskID{a.ID, b.ID}, CoalesceTasksOpts{}); err != nil {
				t.Fatalf("CoalesceTasks: unexpected error: %v", err)
			}
			if got := storeTGet(t, db, parent.ID).Status; got == StatusDone {
				t.Errorf("parent.status = %q, want anything but %q", got, StatusDone)
			}
		})
	})
}

// ── describe("PlanDB.insertTask") ────────────────────────────────────────

func TestStorePlanDBInsertTask(t *testing.T) {
	t.Run("PlanDB.insertTask", func(t *testing.T) {
		t.Run("inserts between two existing tasks and rewires", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Deps: []DepSpec{{TaskID: a.ID}}})
			if b.Status != StatusPending {
				t.Errorf("b.status = %q, want %q", b.Status, StatusPending)
			}

			mid, err := db.InsertTask(a.ID, b.ID, AddTaskInput{Title: "mid", Project: "demo"})
			if err != nil {
				t.Fatalf("InsertTask: unexpected error: %v", err)
			}
			// After insert: a -> mid -> b. b's only blocker is now mid.
			if mid.Status != StatusPending { // depends on a
				t.Errorf("mid.status = %q, want %q", mid.Status, StatusPending)
			}
			// a is still ready
			if got := storeTGet(t, db, a.ID).Status; got != StatusReady {
				t.Errorf("a.status = %q, want %q", got, StatusReady)
			}
			// b depends transitively now; should remain pending
			if got := storeTGet(t, db, b.ID).Status; got != StatusPending {
				t.Errorf("b.status = %q, want %q", got, StatusPending)
			}

			// Complete a, then mid; b becomes ready.
			storeTDone(t, db, a.ID)
			if got := storeTGet(t, db, mid.ID).Status; got != StatusReady {
				t.Errorf("mid.status = %q, want %q", got, StatusReady)
			}
			storeTDone(t, db, mid.ID)
			if got := storeTGet(t, db, b.ID).Status; got != StatusReady {
				t.Errorf("b.status = %q, want %q", got, StatusReady)
			}
		})
	})
}

// ── describe("PlanDB.contexts + search") ─────────────────────────────────

func TestStorePlanDBContextsSearch(t *testing.T) {
	t.Run("PlanDB.contexts + search", func(t *testing.T) {
		t.Run("addContext + listContexts ordered by created_at desc", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			db.AddContext("first note", AddContextOpts{Project: "demo"})
			db.AddContext("second note", AddContextOpts{Project: "demo", Kind: "decision"})
			all := db.ListContexts(&ListContextsFilter{Project: "demo"})
			contents := []string{}
			for _, c := range all {
				contents = append(contents, c.Content)
			}
			if !reflect.DeepEqual(contents, []string{"second note", "first note"}) {
				t.Errorf("contents = %v, want [second note first note]", contents)
			}
			decisions := db.ListContexts(&ListContextsFilter{Project: "demo", Kind: "decision"})
			if len(decisions) != 1 {
				t.Errorf("len(decisions) = %d, want 1", len(decisions))
			}
		})

		t.Run("search matches title, description, and contexts", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			desc := "in tokio runtime"
			storeTAdd(t, db, AddTaskInput{Title: "fix race condition", Project: "demo", Description: &desc})
			db.AddContext("tokio is the rust async runtime", AddContextOpts{Project: "demo"})
			hits := db.Search("tokio", "demo", 0)
			if len(hits) != 2 {
				t.Errorf("len(hits) = %d, want 2", len(hits))
			}
		})
	})
}

// ── describe("PlanDB.criticalPath + bottlenecks + whatUnlocks") ──────────

func TestStorePlanDBCriticalPathBottlenecksWhatUnlocks(t *testing.T) {
	t.Run("PlanDB.criticalPath + bottlenecks + whatUnlocks", func(t *testing.T) {
		t.Run("criticalPath returns longest dep chain", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Deps: []DepSpec{{TaskID: a.ID}}})
			c := storeTAdd(t, db, AddTaskInput{Title: "c", Project: "demo", Deps: []DepSpec{{TaskID: b.ID}}})
			path := db.CriticalPath("demo")
			if got := storeTTaskIDs(path); !reflect.DeepEqual(got, []string{a.ID, b.ID, c.ID}) {
				t.Errorf("criticalPath ids = %v, want [%s %s %s]", got, a.ID, b.ID, c.ID)
			}
		})

		t.Run("bottlenecks ranks by downstream count", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			root := storeTAdd(t, db, AddTaskInput{Title: "root", Project: "demo"})
			storeTAdd(t, db, AddTaskInput{Title: "x", Project: "demo", Deps: []DepSpec{{TaskID: root.ID}}})
			storeTAdd(t, db, AddTaskInput{Title: "y", Project: "demo", Deps: []DepSpec{{TaskID: root.ID}}})
			storeTAdd(t, db, AddTaskInput{Title: "z", Project: "demo", Deps: []DepSpec{{TaskID: root.ID}}})
			top := db.Bottlenecks("demo")
			if len(top) == 0 {
				t.Fatal("bottlenecks() is empty")
			}
			if top[0].Task.ID != root.ID {
				t.Errorf("top[0].task.id = %q, want %q", top[0].Task.ID, root.ID)
			}
			if top[0].Downstream != 3 {
				t.Errorf("top[0].downstream = %v, want 3", float64(top[0].Downstream))
			}
		})

		t.Run("whatUnlocks returns downstream that become ready when this task is done", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			a := storeTAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			b := storeTAdd(t, db, AddTaskInput{Title: "b", Project: "demo", Deps: []DepSpec{{TaskID: a.ID}}})
			unlocked := db.WhatUnlocks(a.ID)
			if got := storeTTaskIDs(unlocked); !reflect.DeepEqual(got, []string{b.ID}) {
				t.Errorf("whatUnlocks ids = %v, want [%s]", got, b.ID)
			}
		})
	})
}
