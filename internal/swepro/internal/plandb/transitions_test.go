package plandb

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 1:1 port of src/plandb/transitions.test.ts. describe/test names are
// preserved verbatim in t.Run names so coverage can be diffed against the TS
// suite line by line.

// ── releaseTask / reopenToPending: pure transition legality ──────────────────

// describe("PlanDB.releaseTask")
func TestTransitionsPlanDBReleaseTask(t *testing.T) {
	// beforeEach: db = new PlanDB(); db.init("demo")
	newDB := func(t *testing.T) *PlanDB {
		t.Helper()
		db := NewPlanDB()
		db.Init("demo")
		return db
	}

	t.Run("claimed → ready, clearing execution stamps but keeping history", func(t *testing.T) {
		db := newDB(t)

		a := mustAddTaskTransitions(t, db, AddTaskInput{Title: "a", Project: "demo"})
		db.ClaimTask(a.ID, "worker-1")
		before := db.GetTask(a.ID)
		if before == nil {
			t.Fatalf("getTask(a.id) = nil, want task")
		}
		if before.Status != StatusClaimed {
			t.Errorf("before.status = %q, want %q", before.Status, StatusClaimed)
		}
		if before.AgentID == nil || *before.AgentID != "worker-1" {
			t.Errorf("before.agent_id = %v, want %q", ptrStrTransitions(before.AgentID), "worker-1")
		}

		released := db.ReleaseTask(a.ID)
		if released == nil {
			t.Fatalf("releaseTask(a.id) = nil, want task")
		}
		if released.Status != StatusReady {
			t.Errorf("released.status = %q, want %q", released.Status, StatusReady)
		}
		if released.AgentID != nil {
			t.Errorf("released.agent_id = %q, want null", *released.AgentID)
		}
		if released.ClaimedAt != nil {
			t.Errorf("released.claimed_at = %d, want null", *released.ClaimedAt)
		}
		if released.StartedAt != nil {
			t.Errorf("released.started_at = %d, want null", *released.StartedAt)
		}
		// result/error history is preserved (not part of the reset set)
		if !reflect.DeepEqual(released.Result, before.Result) {
			t.Errorf("released.result = %v, want %v", released.Result, before.Result)
		}
		if !ptrEq(released.Error, before.Error) {
			t.Errorf("released.error = %v, want %v", ptrStrTransitions(released.Error), ptrStrTransitions(before.Error))
		}
	})

	t.Run("running → ready", func(t *testing.T) {
		db := newDB(t)

		a := mustAddTaskTransitions(t, db, AddTaskInput{Title: "a", Project: "demo"})
		db.ClaimTask(a.ID, "w")
		db.StartTask(a.ID)
		if got := db.GetTask(a.ID); got == nil || got.Status != StatusRunning {
			t.Fatalf("getTask(a.id).status = %v, want %q", statusOfTransitions(got), StatusRunning)
		}
		released := db.ReleaseTask(a.ID)
		if released == nil || released.Status != StatusReady {
			t.Fatalf("releaseTask(a.id).status = %v, want %q", statusOfTransitions(released), StatusReady)
		}
	})

	t.Run("a released task with an unmet dep lands pending, not ready", func(t *testing.T) {
		db := newDB(t)

		a := mustAddTaskTransitions(t, db, AddTaskInput{Title: "a", Project: "demo"})
		b := mustAddTaskTransitions(t, db, AddTaskInput{
			Title:   "b",
			Project: "demo",
			Deps:    []DepSpec{{TaskID: a.ID}},
		})
		// b is pending (a not done). Force b through claim by first making it ready:
		// instead, claim a, done a → b ready, claim b, then RE-block b by failing a's
		// equivalent. Simpler: claim a-done path.
		db.ClaimTask(a.ID, "w")
		db.StartTask(a.ID)
		if _, err := db.DoneTask(a.ID, DoneOpts{}); err != nil {
			t.Fatalf("doneTask(a.id) errored: %v", err)
		}
		if got := db.GetTask(b.ID); got == nil || got.Status != StatusReady {
			t.Fatalf("getTask(b.id).status = %v, want %q", statusOfTransitions(got), StatusReady)
		}
		db.ClaimTask(b.ID, "w")
		// Now re-introduce an unmet dep on b, then release: it must land pending.
		c := mustAddTaskTransitions(t, db, AddTaskInput{Title: "c", Project: "demo"}) // ready, NOT done
		db.AddDep(c.ID, b.ID, DepFeedsInto)
		released := db.ReleaseTask(b.ID)
		if released == nil {
			t.Fatalf("releaseTask(b.id) = nil, want task")
		}
		if released.Status != StatusPending { // unmet dep on c blocks promotion
			t.Errorf("released.status = %q, want %q", released.Status, StatusPending)
		}
	})

	t.Run("illegal state: releasing a pending/ready/done task returns null", func(t *testing.T) {
		db := newDB(t)

		a := mustAddTaskTransitions(t, db, AddTaskInput{Title: "a", Project: "demo"}) // ready
		if got := db.ReleaseTask(a.ID); got != nil {
			t.Errorf("releaseTask(ready) = %+v, want null", got)
		}
		db.ClaimTask(a.ID, "w")
		db.StartTask(a.ID)
		if _, err := db.DoneTask(a.ID, DoneOpts{}); err != nil {
			t.Fatalf("doneTask(a.id) errored: %v", err)
		}
		if got := db.ReleaseTask(a.ID); got != nil { // done
			t.Errorf("releaseTask(done) = %+v, want null", got)
		}
	})

	t.Run("unknown task returns null", func(t *testing.T) {
		db := newDB(t)

		if got := db.ReleaseTask("t-nope"); got != nil {
			t.Errorf("releaseTask(\"t-nope\") = %+v, want null", got)
		}
	})
}

// describe("PlanDB.reopenToPending")
func TestTransitionsPlanDBReopenToPending(t *testing.T) {
	// beforeEach: db = new PlanDB(); db.init("demo")
	newDB := func(t *testing.T) *PlanDB {
		t.Helper()
		db := NewPlanDB()
		db.Init("demo")
		return db
	}

	t.Run("ready → pending when the task now has an unmet dependency", func(t *testing.T) {
		db := newDB(t)

		b := mustAddTaskTransitions(t, db, AddTaskInput{Title: "b", Project: "demo"}) // ready, no deps
		// Introduce a not-done predecessor AFTER promotion (addDep does not demote),
		// mirroring the cascade case where a ready dependent's predecessor fails.
		a := mustAddTaskTransitions(t, db, AddTaskInput{Title: "a", Project: "demo"}) // ready, NOT done
		db.AddDep(a.ID, b.ID, DepFeedsInto)
		if got := db.GetTask(b.ID); got == nil || got.Status != StatusReady { // still ready
			t.Fatalf("getTask(b.id).status = %v, want %q", statusOfTransitions(got), StatusReady)
		}

		reopened, err := db.ReopenToPending(b.ID)
		if err != nil {
			t.Fatalf("reopenToPending(b.id) errored: %v", err)
		}
		if reopened == nil {
			t.Fatalf("reopenToPending(b.id) = nil, want task")
		}
		if reopened.Status != StatusPending {
			t.Errorf("reopened.status = %q, want %q", reopened.Status, StatusPending)
		}
	})

	t.Run("rejects reopening a genuinely-ready task (all deps met)", func(t *testing.T) {
		db := newDB(t)

		b := mustAddTaskTransitions(t, db, AddTaskInput{Title: "b", Project: "demo"}) // ready, deps all met
		got, err := db.ReopenToPending(b.ID)
		if err == nil {
			t.Fatalf("reopenToPending(b.id) = (%+v, nil), want error matching /all dependencies met/", got)
		}
		if !strings.Contains(err.Error(), "all dependencies met") {
			t.Errorf("error = %q, want match /all dependencies met/", err.Error())
		}
		wantMsg := fmt.Sprintf("task %s has all dependencies met; refusing to reopen a genuinely-ready task to pending", b.ID)
		if err.Error() != wantMsg {
			t.Errorf("error = %q, want %q", err.Error(), wantMsg)
		}
		if cur := db.GetTask(b.ID); cur == nil || cur.Status != StatusReady { // unchanged
			t.Errorf("getTask(b.id).status = %v, want %q", statusOfTransitions(cur), StatusReady)
		}
	})

	t.Run("rejects reopening a non-ready task", func(t *testing.T) {
		db := newDB(t)

		a := mustAddTaskTransitions(t, db, AddTaskInput{Title: "a", Project: "demo"})
		c := mustAddTaskTransitions(t, db, AddTaskInput{
			Title:   "c",
			Project: "demo",
			Deps:    []DepSpec{{TaskID: a.ID}},
		})
		if got := db.GetTask(c.ID); got == nil || got.Status != StatusPending {
			t.Fatalf("getTask(c.id).status = %v, want %q", statusOfTransitions(got), StatusPending)
		}
		got, err := db.ReopenToPending(c.ID)
		if err == nil {
			t.Fatalf("reopenToPending(c.id) = (%+v, nil), want error matching /must be ready/", got)
		}
		if !strings.Contains(err.Error(), "must be ready") {
			t.Errorf("error = %q, want match /must be ready/", err.Error())
		}
		wantMsg := fmt.Sprintf("task %s must be ready to reopen to pending (status=%s)", c.ID, StatusPending)
		if err.Error() != wantMsg {
			t.Errorf("error = %q, want %q", err.Error(), wantMsg)
		}
	})

	t.Run("unknown task returns null", func(t *testing.T) {
		db := newDB(t)

		got, err := db.ReopenToPending("t-nope")
		if err != nil {
			t.Fatalf("reopenToPending(\"t-nope\") errored: %v", err)
		}
		if got != nil {
			t.Errorf("reopenToPending(\"t-nope\") = %+v, want null", got)
		}
	})
}

// ── cli-bridge argv routing ──────────────────────────────────────────────────

// describe("cli-bridge: task release / task reopen")
func TestTransitionsCLIBridgeTaskReleaseReopen(t *testing.T) {
	// beforeEach / afterEach: _resetPlanDBForTesting()
	setup := func(t *testing.T) {
		t.Helper()
		ResetPlanDBForTesting()
		t.Cleanup(ResetPlanDBForTesting)
	}

	t.Run(`runPlanDB(["plandb","task","release",id]) requeues a claimed task`, func(t *testing.T) {
		setup(t)

		db := GetPlanDB()
		db.Init("demo")
		a := mustAddTaskTransitions(t, db, AddTaskInput{Title: "a", Project: "demo"})
		db.ClaimTask(a.ID, "worker-1")
		res := RunPlanDB([]string{"plandb", "task", "release", a.ID})
		if res.Code != 0 {
			t.Fatalf("code = %d, want 0 (stderr=%q)", res.Code, string(res.Stderr))
		}
		if got := jsonFieldTransitions(t, res.Stdout, "status"); got != "ready" {
			t.Errorf("JSON.parse(stdout).status = %v, want %q", got, "ready")
		}
		cur := db.GetTask(a.ID)
		if cur == nil {
			t.Fatalf("getTask(a.id) = nil, want task")
		}
		if cur.AgentID != nil {
			t.Errorf("getTask(a.id).agent_id = %q, want null", *cur.AgentID)
		}
	})

	t.Run("release on a non-claimed task exits nonzero", func(t *testing.T) {
		setup(t)

		db := GetPlanDB()
		db.Init("demo")
		a := mustAddTaskTransitions(t, db, AddTaskInput{Title: "a", Project: "demo"}) // ready
		res := RunPlanDB([]string{"plandb", "task", "release", a.ID})
		if res.Code != 1 {
			t.Errorf("code = %d, want 1 (stdout=%q)", res.Code, string(res.Stdout))
		}
	})

	t.Run(`runPlanDB(["plandb","task","reopen",id]) demotes a now-blocked ready task`, func(t *testing.T) {
		setup(t)

		db := GetPlanDB()
		db.Init("demo")
		b := mustAddTaskTransitions(t, db, AddTaskInput{Title: "b", Project: "demo"})
		a := mustAddTaskTransitions(t, db, AddTaskInput{Title: "a", Project: "demo"})
		db.AddDep(a.ID, b.ID, DepFeedsInto)
		res := RunPlanDB([]string{"plandb", "task", "reopen", b.ID})
		if res.Code != 0 {
			t.Fatalf("code = %d, want 0 (stderr=%q)", res.Code, string(res.Stderr))
		}
		if got := jsonFieldTransitions(t, res.Stdout, "status"); got != "pending" {
			t.Errorf("JSON.parse(stdout).status = %v, want %q", got, "pending")
		}
	})

	t.Run("reopen on a genuinely-ready task exits nonzero (guard)", func(t *testing.T) {
		setup(t)

		db := GetPlanDB()
		db.Init("demo")
		b := mustAddTaskTransitions(t, db, AddTaskInput{Title: "b", Project: "demo"})
		res := RunPlanDB([]string{"plandb", "task", "reopen", b.ID})
		if res.Code != 1 {
			t.Errorf("code = %d, want 1 (stdout=%q)", res.Code, string(res.Stdout))
		}
	})
}

// ── durability: transitions survive a simulated restart ──────────────────────

// describe("release / reopen journal round-trip")
func TestTransitionsReleaseReopenJournalRoundTrip(t *testing.T) {
	t.Run("released + reopened statuses persist across restart", func(t *testing.T) {
		// beforeEach: fresh temp dir, delete PLANDB_DB / CODEAF_PLANDB_PERSIST,
		// _resetPlanDBForTesting(). afterEach restores env (t.Setenv) and removes
		// the dir (t.TempDir).
		dir := t.TempDir()
		dbPath := filepath.Join(dir, ".plandb.db")
		t.Setenv("PLANDB_DB", "")
		t.Setenv("CODEAF_PLANDB_PERSIST", "")
		ResetPlanDBForTesting()
		t.Cleanup(ResetPlanDBForTesting)

		OpenPlanDB(dbPath)
		db := GetPlanDB()
		db.Init("demo")

		claimed := mustAddTaskTransitions(t, db, AddTaskInput{Title: "claimed", Project: "demo"})
		db.ClaimTask(claimed.ID, "dead-worker")
		db.ReleaseTask(claimed.ID) // claimed → ready, stamps cleared

		dep := mustAddTaskTransitions(t, db, AddTaskInput{Title: "dep-target", Project: "demo"})
		ready := mustAddTaskTransitions(t, db, AddTaskInput{Title: "ready", Project: "demo"})
		db.AddDep(dep.ID, ready.ID, DepFeedsInto)
		if _, err := db.ReopenToPending(ready.ID); err != nil { // ready → pending
			t.Fatalf("reopenToPending(ready.id) errored: %v", err)
		}

		before := GetPlanDB().SnapshotState()
		if got := db.GetTask(claimed.ID); got == nil || got.Status != StatusReady {
			t.Fatalf("getTask(claimed.id).status = %v, want %q", statusOfTransitions(got), StatusReady)
		}
		if got := db.GetTask(ready.ID); got == nil || got.Status != StatusPending {
			t.Fatalf("getTask(ready.id).status = %v, want %q", statusOfTransitions(got), StatusPending)
		}

		// Simulate process death + resume.
		ResetPlanDBForTesting()
		OpenPlanDB(dbPath)
		after := GetPlanDB().SnapshotState()

		// TS: expect(after).toEqual(before)
		//
		// NOTE (suspected port bug, asserted as TS behaviour per the porting
		// contract): this currently FAILS even though both snapshots serialize
		// to byte-identical JSON. persist.go's fold unmarshals a journalled
		// `"result": null` / `"metadata": null` back into a NON-nil
		// json.RawMessage holding the 4 bytes `null`, whereas the live store
		// holds a nil RawMessage. TS has a single `null` for both, so toEqual
		// holds there. See the report accompanying this file.
		if !reflect.DeepEqual(after, before) {
			t.Errorf("snapshot after restart != snapshot before\n%s\n after = %s\nbefore = %s",
				strings.Join(snapshotDiffTransitions(before, after), "\n"),
				mustJSONTransitions(t, after), mustJSONTransitions(t, before))
		}
		rdb := GetPlanDB()
		rc := rdb.GetTask(claimed.ID)
		if rc == nil {
			t.Fatalf("restored getTask(claimed.id) = nil, want task")
		}
		if rc.Status != StatusReady {
			t.Errorf("restored claimed.status = %q, want %q", rc.Status, StatusReady)
		}
		if rc.AgentID != nil {
			t.Errorf("restored claimed.agent_id = %q, want null", *rc.AgentID)
		}
		if rc.ClaimedAt != nil {
			t.Errorf("restored claimed.claimed_at = %d, want null", *rc.ClaimedAt)
		}
		rr := rdb.GetTask(ready.ID)
		if rr == nil {
			t.Fatalf("restored getTask(ready.id) = nil, want task")
		}
		if rr.Status != StatusPending {
			t.Errorf("restored ready.status = %q, want %q", rr.Status, StatusPending)
		}
	})
}

// ── helpers ──────────────────────────────────────────────────────────────────

func mustAddTaskTransitions(t *testing.T, db *PlanDB, input AddTaskInput) *Task {
	t.Helper()
	task, err := db.AddTask(input)
	if err != nil {
		t.Fatalf("addTask(%q) errored: %v", input.Title, err)
	}
	if task == nil {
		t.Fatalf("addTask(%q) = nil", input.Title)
	}
	return task
}

func ptrStrTransitions(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func statusOfTransitions(task *Task) any {
	if task == nil {
		return nil
	}
	return task.Status
}

// jsonFieldTransitions mirrors JSON.parse(res.stdout.toString())[key].
func jsonFieldTransitions(t *testing.T, stdout []byte, key string) any {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(stdout, &parsed); err != nil {
		t.Fatalf("JSON.parse(stdout) failed: %v (stdout=%q)", err, string(stdout))
	}
	return parsed[key]
}

// snapshotDiffTransitions reports the Go-value-level field differences between
// two snapshots, including the raw byte form of json.RawMessage fields so a
// nil-vs-`null` divergence is visible (JSON rendering alone hides it).
func snapshotDiffTransitions(before, after Snapshot) []string {
	var out []string
	note := func(format string, args ...any) { out = append(out, "  diff: "+fmt.Sprintf(format, args...)) }

	rawDiff := func(where string, b, a json.RawMessage) {
		if !reflect.DeepEqual(b, a) {
			note("%s: before=%#v after=%#v", where, []byte(b), []byte(a))
		}
	}

	if len(before.Projects) != len(after.Projects) {
		note("projects length: before=%d after=%d", len(before.Projects), len(after.Projects))
	} else {
		for i := range before.Projects {
			rawDiff(fmt.Sprintf("projects[%d].metadata", i), before.Projects[i].Metadata, after.Projects[i].Metadata)
			if !reflect.DeepEqual(before.Projects[i], after.Projects[i]) {
				note("projects[%d] differs", i)
			}
		}
	}
	if len(before.Tasks) != len(after.Tasks) {
		note("tasks length: before=%d after=%d", len(before.Tasks), len(after.Tasks))
	} else {
		for i := range before.Tasks {
			rawDiff(fmt.Sprintf("tasks[%d].result", i), before.Tasks[i].Result, after.Tasks[i].Result)
			rawDiff(fmt.Sprintf("tasks[%d].metadata", i), before.Tasks[i].Metadata, after.Tasks[i].Metadata)
			if !reflect.DeepEqual(before.Tasks[i].Files, after.Tasks[i].Files) {
				note("tasks[%d].files: before=%#v after=%#v", i, before.Tasks[i].Files, after.Tasks[i].Files)
			}
			if !reflect.DeepEqual(before.Tasks[i].Tags, after.Tasks[i].Tags) {
				note("tasks[%d].tags: before=%#v after=%#v", i, before.Tasks[i].Tags, after.Tasks[i].Tags)
			}
			if !reflect.DeepEqual(before.Tasks[i], after.Tasks[i]) {
				note("tasks[%d] differs", i)
			}
		}
	}
	if !reflect.DeepEqual(before.Contexts, after.Contexts) {
		note("contexts: before=%#v after=%#v", before.Contexts, after.Contexts)
	}
	if !reflect.DeepEqual(before.Dependencies, after.Dependencies) {
		note("dependencies: before=%#v after=%#v", before.Dependencies, after.Dependencies)
	}
	if len(out) == 0 {
		out = append(out, "  diff: (no field-level difference located)")
	}
	return out
}

func mustJSONTransitions(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}
	return string(b)
}
