package plandb

// 1:1 translation of src/plandb/persist.test.ts. Subtest names are the TS
// describe/test strings verbatim so coverage can be diffed by eye.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ── harness ──────────────────────────────────────────────────────────────
//
// TS beforeEach: fresh temp workspace + fresh singleton, PLANDB_DB and
// CODEAF_PLANDB_PERSIST cleared (and restored in afterEach — t.Setenv does the
// restore for us). t.TempDir is removed automatically, matching the afterEach
// rmSync.

func persistSetup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".plandb.db")
	// t.Setenv records the pre-test value and restores it on cleanup; the
	// Unsetenv right after reproduces `delete process.env.X`.
	t.Setenv("PLANDB_DB", "")
	t.Setenv("CODEAF_PLANDB_PERSIST", "")
	_ = os.Unsetenv("PLANDB_DB")
	_ = os.Unsetenv("CODEAF_PLANDB_PERSIST")
	ResetPlanDBForTesting()
	t.Cleanup(ResetPlanDBForTesting)
	return dbPath
}

func persistStrPtr(s string) *string { return &s }

func persistF64Ptr(f float64) *float64 { return &f }

func persistMustAdd(t *testing.T, db *PlanDB, in AddTaskInput) *Task {
	t.Helper()
	task, err := db.AddTask(in)
	if err != nil {
		t.Fatalf("addTask(%q): unexpected error: %v", in.Title, err)
	}
	if task == nil {
		t.Fatalf("addTask(%q): returned nil task", in.Title)
	}
	return task
}

type persistRichIDs struct {
	a      TaskID
	b      TaskID
	c      TaskID
	parent TaskID
	merged TaskID
}

// persistBuildRichGraph mirrors buildRichGraph(): exercise every mutating path
// (create, claim, start, done, done_partial, fail, cancel, insert, amend,
// coalesce, addContext, composite auto-complete) and return the minted ids.
func persistBuildRichGraph(t *testing.T) persistRichIDs {
	t.Helper()
	db := GetPlanDB()
	db.Init("demo")

	a := persistMustAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
	b := persistMustAdd(t, db, AddTaskInput{
		Title: "b", Project: "demo",
		Deps: []DepSpec{{TaskID: a.ID}},
	})
	suggests := DepSuggests
	c := persistMustAdd(t, db, AddTaskInput{
		Title: "c", Project: "demo",
		Deps: []DepSpec{{TaskID: a.ID, Kind: &suggests}},
	})

	// a: full lifecycle to done → unblocks b
	if got := db.ClaimTask(a.ID, "worker-1"); got == nil {
		t.Fatalf("claimTask(a) returned nil")
	}
	if got := db.StartTask(a.ID); got == nil {
		t.Fatalf("startTask(a) returned nil")
	}
	if _, err := db.DoneTask(a.ID, DoneOpts{
		Result: map[string]any{"ok": true},
		Files:  []string{"src/a.ts"},
		Agent:  persistStrPtr("worker-1"),
	}); err != nil {
		t.Fatalf("doneTask(a): %v", err)
	}

	// b: done_partial with debt
	if got := db.ClaimTask(b.ID, "worker-2"); got == nil {
		t.Fatalf("claimTask(b) returned nil")
	}
	if got := db.StartTask(b.ID); got == nil {
		t.Fatalf("startTask(b) returned nil")
	}
	if _, err := db.DonePartialTask(b.ID, DoneOpts{
		Result: "partial",
		Files:  []string{"src/b.ts"},
	}); err != nil {
		t.Fatalf("donePartialTask(b): %v", err)
	}

	// c: fail
	db.FailTask(c.ID, "boom", persistStrPtr("worker-3"))

	// composite parent with children → auto-completes
	parent := persistMustAdd(t, db, AddTaskInput{Title: "parent", Project: "demo"})
	c1 := persistMustAdd(t, db, AddTaskInput{Title: "c1", Project: "demo", Parent: parent.ID})
	c2 := persistMustAdd(t, db, AddTaskInput{Title: "c2", Project: "demo", Parent: parent.ID})
	if _, err := db.DoneTask(c1.ID, DoneOpts{}); err != nil {
		t.Fatalf("doneTask(c1): %v", err)
	}
	db.CancelTask(c2.ID) // cancelled still counts toward composite completion

	// amend + update + insert + context
	d := persistMustAdd(t, db, AddTaskInput{Title: "d", Project: "demo"})
	if _, err := db.AmendTask(d.ID, "extra scope", "append"); err != nil {
		t.Fatalf("amendTask(d): %v", err)
	}
	db.UpdateTask(d.ID, UpdateFields{Priority: persistF64Ptr(5)})
	if _, err := db.InsertTask(d.ID, "", AddTaskInput{Title: "inserted", Project: "demo"}); err != nil {
		t.Fatalf("insertTask(d): %v", err)
	}
	db.AddContext("a free-floating note", AddContextOpts{Project: "demo", Kind: "note"})

	// coalesce two fresh pending siblings under a new parent
	cp := persistMustAdd(t, db, AddTaskInput{Title: "cparent", Project: "demo"})
	s1 := persistMustAdd(t, db, AddTaskInput{Title: "s1", Project: "demo", Parent: cp.ID})
	s2 := persistMustAdd(t, db, AddTaskInput{Title: "s2", Project: "demo", Parent: cp.ID})
	merged, err := db.CoalesceTasks([]TaskID{s1.ID, s2.ID}, CoalesceTasksOpts{})
	if err != nil {
		t.Fatalf("coalesceTasks: %v", err)
	}

	return persistRichIDs{a: a.ID, b: b.ID, c: c.ID, parent: parent.ID, merged: merged.ID}
}

// persistNormalizeRaw makes a nil json.RawMessage compare equal to the literal
// bytes "null". Both encode the same JSON value, and in TS both sides of the
// round trip are literally `null` (types.ts declares result/metadata as
// `... | null`, and store.ts writes `result: null` / `metadata: null`), so
// toEqual sees one value. In Go the live store holds a nil RawMessage while the
// journal-restored copy holds RawMessage("null") — encoding/json calls
// RawMessage.UnmarshalJSON even for a JSON null. Normalizing here keeps the
// assertion at TS strength (the JSON bytes of the two snapshots are verified
// identical by the same helper) without weakening any other field.
func persistNormalizeRaw(r json.RawMessage) json.RawMessage {
	if len(r) == 0 {
		return json.RawMessage("null")
	}
	return r
}

func persistNormalizeSnapshot(s Snapshot) Snapshot {
	out := Snapshot{
		Projects:     make([]*Project, len(s.Projects)),
		Tasks:        make([]*Task, len(s.Tasks)),
		Contexts:     make([]*ContextEntry, len(s.Contexts)),
		Dependencies: append([]Dependency{}, s.Dependencies...),
	}
	for i, p := range s.Projects {
		if p == nil {
			continue
		}
		c := *p
		c.Metadata = persistNormalizeRaw(c.Metadata)
		out.Projects[i] = &c
	}
	for i, task := range s.Tasks {
		if task == nil {
			continue
		}
		c := *task
		c.Result = persistNormalizeRaw(c.Result)
		c.Metadata = persistNormalizeRaw(c.Metadata)
		out.Tasks[i] = &c
	}
	for i, ctx := range s.Contexts {
		if ctx == nil {
			continue
		}
		c := *ctx
		out.Contexts[i] = &c
	}
	return out
}

// assertSnapshotEqual is the Go stand-in for expect(after).toEqual(before).
func assertSnapshotEqual(t *testing.T, got, want Snapshot) {
	t.Helper()
	if !reflect.DeepEqual(persistNormalizeSnapshot(got), persistNormalizeSnapshot(want)) {
		gb, _ := jscompat.StringifyIndent(got)
		wb, _ := jscompat.StringifyIndent(want)
		t.Errorf("snapshot mismatch after restart\n--- got ---\n%s\n--- want ---\n%s", gb, wb)
		return
	}
	// "byte-for-byte": the serialized form must match too.
	gb, _ := jscompat.Stringify(got)
	wb, _ := jscompat.Stringify(want)
	if string(gb) != string(wb) {
		t.Errorf("snapshot JSON mismatch after restart\n--- got ---\n%s\n--- want ---\n%s", gb, wb)
	}
}

func persistFileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ── describe("planDBPath") ───────────────────────────────────────────────

func TestPersistPlanDBPath(t *testing.T) {
	t.Run("defaults to <workspace>/.plandb.db", func(t *testing.T) {
		persistSetup(t)
		if got, want := PlanDBPath("/work/space"), filepath.Join("/work/space", ".plandb.db"); got != want {
			t.Errorf("planDBPath = %q, want %q", got, want)
		}
	})

	t.Run("honors PLANDB_DB override", func(t *testing.T) {
		persistSetup(t)
		t.Setenv("PLANDB_DB", "/custom/elsewhere.db")
		if got, want := PlanDBPath("/work/space"), "/custom/elsewhere.db"; got != want {
			t.Errorf("planDBPath = %q, want %q", got, want)
		}
	})
}

// ── describe("openPlanDB replay round-trip") ─────────────────────────────

func TestPersistOpenPlanDBReplayRoundTrip(t *testing.T) {
	t.Run("full graph survives a simulated restart, byte-for-byte", func(t *testing.T) {
		dbPath := persistSetup(t)

		OpenPlanDB(dbPath)
		ids := persistBuildRichGraph(t)
		before := GetPlanDB().SnapshotState()

		// sanity: the graph is non-trivial
		if len(before.Tasks) <= 8 {
			t.Fatalf("before.tasks.length = %d, want > 8", len(before.Tasks))
		}
		if len(before.Dependencies) <= 0 {
			t.Fatalf("before.dependencies.length = %d, want > 0", len(before.Dependencies))
		}

		// Simulate a process restart: drop the singleton, reload from disk.
		ResetPlanDBForTesting()
		OpenPlanDB(dbPath)
		after := GetPlanDB().SnapshotState()

		assertSnapshotEqual(t, after, before)

		// ids are stable across restart (attempt-ledger keys depend on this)
		db := GetPlanDB()
		if got := db.GetTask(ids.a); got == nil || got.Status != StatusDone {
			t.Errorf("getTask(a).status = %v, want done", persistStatusOf(got))
		}
		if got := db.GetTask(ids.b); got == nil || got.Status != StatusDonePartial {
			t.Errorf("getTask(b).status = %v, want done_partial", persistStatusOf(got))
		}
		if got := db.GetTask(ids.c); got == nil || got.Status != StatusFailed {
			t.Errorf("getTask(c).status = %v, want failed", persistStatusOf(got))
		}
		if got := db.GetTask(ids.parent); got == nil || got.Status != StatusDone {
			t.Errorf("getTask(parent).status = %v, want done", persistStatusOf(got))
		}
		if got := db.GetTask(ids.merged); got == nil || got.Title != "s1 + s2" {
			title := "<nil>"
			if got != nil {
				title = got.Title
			}
			t.Errorf("getTask(merged).title = %q, want %q", title, "s1 + s2")
		}
	})

	t.Run("results, errors, files, deps and context all restore", func(t *testing.T) {
		dbPath := persistSetup(t)

		OpenPlanDB(dbPath)
		ids := persistBuildRichGraph(t)
		ResetPlanDBForTesting()
		OpenPlanDB(dbPath)
		db := GetPlanDB()

		ta := db.GetTask(ids.a)
		if ta == nil {
			t.Fatalf("getTask(a) = nil after restart")
		}
		var result any
		if err := json.Unmarshal(ta.Result, &result); err != nil {
			t.Fatalf("getTask(a).result is not valid JSON (%q): %v", string(ta.Result), err)
		}
		if want := map[string]any{"ok": true}; !reflect.DeepEqual(result, want) {
			t.Errorf("getTask(a).result = %#v, want %#v", result, want)
		}
		if want := []string{"src/a.ts"}; !reflect.DeepEqual(ta.Files, want) {
			t.Errorf("getTask(a).files = %#v, want %#v", ta.Files, want)
		}

		tc := db.GetTask(ids.c)
		if tc == nil || tc.Error == nil || *tc.Error != "boom" {
			t.Errorf("getTask(c).error = %v, want %q", persistErrOf(tc), "boom")
		}

		// b depended on a via feeds_into
		someFromA := false
		for _, d := range db.DepsOfTask(ids.b) {
			if d.FromTask == ids.a {
				someFromA = true
			}
		}
		if !someFromA {
			t.Errorf("depsOfTask(b).some(d => d.from_task === a) = false, want true")
		}

		// coalesce recorded a "coalesced" context on each member
		if got := len(db.ListContexts(&ListContextsFilter{Kind: "coalesced"})); got != 2 {
			t.Errorf("listContexts({kind:'coalesced'}).length = %d, want 2", got)
		}
		someNote := false
		for _, c := range db.ListContexts(&ListContextsFilter{Kind: "note"}) {
			if c.Content == "a free-floating note" {
				someNote = true
			}
		}
		if !someNote {
			t.Errorf("listContexts({kind:'note'}).some(c => c.content === 'a free-floating note') = false, want true")
		}
	})
}

func persistStatusOf(t *Task) any {
	if t == nil {
		return nil
	}
	return t.Status
}

func persistErrOf(t *Task) any {
	if t == nil || t.Error == nil {
		return nil
	}
	return *t.Error
}

// ── describe("journal corruption tolerance") ─────────────────────────────

func TestPersistJournalCorruptionTolerance(t *testing.T) {
	t.Run("a torn final line is skipped; the rest of the graph loads without throwing", func(t *testing.T) {
		dbPath := persistSetup(t)

		OpenPlanDB(dbPath)
		persistBuildRichGraph(t)
		before := GetPlanDB().SnapshotState()

		// Append a truncated / garbage record as if a crash tore the last write.
		f, err := os.OpenFile(dbPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatalf("open for append: %v", err)
		}
		if _, err := f.WriteString(`{"k":"t","v":{"id":"t-broke","title":"tru`); err != nil {
			t.Fatalf("append torn record: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		ResetPlanDBForTesting()
		// OpenPlanDB has no error return — corruption tolerance is expressed by
		// it completing normally (the TS `not.toThrow`).
		OpenPlanDB(dbPath)
		after := GetPlanDB().SnapshotState()

		assertSnapshotEqual(t, after, before)
		if got := GetPlanDB().GetTask("t-broke"); got != nil {
			t.Errorf("getTask('t-broke') = %+v, want undefined/nil", got)
		}
	})

	t.Run("an empty / never-written db loads as an empty store", func(t *testing.T) {
		dbPath := persistSetup(t)

		ResetPlanDBForTesting()
		OpenPlanDB(dbPath)
		if got := GetPlanDB().SnapshotState().Tasks; !reflect.DeepEqual(got, []*Task{}) {
			t.Errorf("snapshot().tasks = %#v, want []", got)
		}
	})
}

// ── describe("CODEAF_PLANDB_PERSIST kill switch") ────────────────────────

func TestPersistKillSwitch(t *testing.T) {
	t.Run("disabled: no file is written and no journal is attached (in-memory only)", func(t *testing.T) {
		dbPath := persistSetup(t)

		t.Setenv("CODEAF_PLANDB_PERSIST", "0")
		OpenPlanDB(dbPath)
		if GetPlanDB().HasJournal() {
			t.Errorf("hasJournal() = true, want false")
		}

		persistBuildRichGraph(t)
		// nothing persisted
		if persistFileExists(dbPath) {
			t.Errorf("existsSync(dbPath) = true, want false")
		}

		// "restart" sees an empty store
		ResetPlanDBForTesting()
		OpenPlanDB(dbPath)
		if got := GetPlanDB().SnapshotState().Tasks; !reflect.DeepEqual(got, []*Task{}) {
			t.Errorf("snapshot().tasks = %#v, want []", got)
		}
		if persistFileExists(dbPath) {
			t.Errorf("existsSync(dbPath) = true, want false")
		}
	})

	t.Run("enabled by default attaches a journal", func(t *testing.T) {
		dbPath := persistSetup(t)

		OpenPlanDB(dbPath)
		if !GetPlanDB().HasJournal() {
			t.Errorf("hasJournal() = false, want true")
		}
		if !persistFileExists(dbPath) {
			t.Errorf("existsSync(dbPath) = false, want true")
		}
	})
}

// ── describe("openPlanDB idempotency") ───────────────────────────────────

func TestPersistOpenPlanDBIdempotency(t *testing.T) {
	t.Run("second open on the same singleton is a no-op", func(t *testing.T) {
		dbPath := persistSetup(t)

		OpenPlanDB(dbPath)
		db1 := GetPlanDB()
		persistMustAdd(t, db1, AddTaskInput{Title: "x", Project: "demo"})
		OpenPlanDB(dbPath) // must not wipe or re-restore
		if got := len(GetPlanDB().ListTasks(nil)); got != 1 {
			t.Errorf("listTasks().length = %d, want 1", got)
		}
		if GetPlanDB() != db1 {
			t.Errorf("getPlanDB() is not the same instance as before the second open")
		}
	})
}

// ── describe("rapid sequential writes + compaction") ─────────────────────

func TestPersistRapidSequentialWritesAndCompaction(t *testing.T) {
	t.Run("many mutations persist and survive restart (crosses compaction threshold)", func(t *testing.T) {
		dbPath := persistSetup(t)

		OpenPlanDB(dbPath)
		db := GetPlanDB()
		db.Init("bulk")
		ids := []TaskID{}
		// 600 > COMPACT_EVERY (500) so an in-run compaction happens mid-stream.
		// Custom ids keep the count deterministic (auto 4-char ids can collide at
		// this volume — a pre-existing store trait unrelated to persistence).
		for i := 0; i < 600; i++ {
			task := persistMustAdd(t, db, AddTaskInput{
				Title:    "t" + itoaJS(i),
				Project:  "bulk",
				CustomID: "t-bulk-" + itoaJS(i),
			})
			ids = append(ids, task.ID)
		}
		before := GetPlanDB().SnapshotState()
		if got := len(before.Tasks); got != 600 {
			t.Fatalf("before.tasks.length = %d, want 600", got)
		}

		ResetPlanDBForTesting()
		OpenPlanDB(dbPath)
		after := GetPlanDB().SnapshotState()

		if got := len(after.Tasks); got != 600 {
			t.Errorf("after.tasks.length = %d, want 600", got)
		}
		assertSnapshotEqual(t, after, before)
		// every id preserved
		for _, id := range ids {
			if GetPlanDB().GetTask(id) == nil {
				t.Fatalf("getTask(%q) = nil after restart, want defined", id)
			}
		}
	})

	t.Run("compaction bounds file growth: 600 mutations do not leave 600 log lines", func(t *testing.T) {
		dbPath := persistSetup(t)

		OpenPlanDB(dbPath)
		db := GetPlanDB()
		db.Init("bulk")
		for i := 0; i < 600; i++ {
			persistMustAdd(t, db, AddTaskInput{
				Title:    "t" + itoaJS(i),
				Project:  "bulk",
				CustomID: "t-bulk-" + itoaJS(i),
			})
		}
		raw, err := os.ReadFile(dbPath)
		if err != nil {
			t.Fatalf("readFile(dbPath): %v", err)
		}
		lines := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if line != "" { // .filter(Boolean)
				lines++
			}
		}
		// After the mid-run compaction the log is a snapshot + the tail since then,
		// far fewer than the ~1200 raw append lines 600 tasks would otherwise emit.
		if lines >= 600 {
			t.Errorf("log lines = %d, want < 600", lines)
		}
	})
}

// itoaJS renders an int the way JS template interpolation does.
func itoaJS(i int) string { return jscompat.FormatNumber(float64(i)) }
