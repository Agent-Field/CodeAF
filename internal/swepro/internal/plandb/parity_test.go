// Port of src/plandb/parity.test.ts — locked-in invariants between the
// in-memory PlanDB and the standalone Rust plandb CLI. Each test names the
// specific Rust source line or SQL that defines the expected behavior. If you
// change behavior here without changing it in TS/Rust too, you've broken
// parity.
//
// Subtest names are the verbatim TS describe/test strings so coverage can be
// diffed 1:1 against parity.test.ts.

package plandb

import (
	"regexp"
	"strconv"
	"testing"
)

// parityNewDB mirrors the TS `beforeEach(() => { db = new PlanDB() })`.
func parityNewDB(t *testing.T) *PlanDB {
	t.Helper()
	ResetPlanDBForTesting()
	return NewPlanDB()
}

func parityDepKind(k DepKind) *DepKind { return &k }

// parityAdd mirrors `db.addTask(...)` in the TS test, where addTask throwing
// would fail the test outright.
func parityAdd(t *testing.T, db *PlanDB, input AddTaskInput) *Task {
	t.Helper()
	task, err := db.AddTask(input)
	if err != nil {
		t.Fatalf("addTask(%q) returned unexpected error: %v", input.Title, err)
	}
	if task == nil {
		t.Fatalf("addTask(%q) returned nil task", input.Title)
	}
	return task
}

// parityDone mirrors `db.doneTask(id)`, which in TS throws on failure.
func parityDone(t *testing.T, db *PlanDB, tid TaskID) *Task {
	t.Helper()
	task, err := db.DoneTask(tid, DoneOpts{})
	if err != nil {
		t.Fatalf("doneTask(%s) returned unexpected error: %v", tid, err)
	}
	return task
}

func parityMustGet(t *testing.T, db *PlanDB, tid TaskID) *Task {
	t.Helper()
	task := db.GetTask(tid)
	if task == nil {
		t.Fatalf("getTask(%s) returned nil (TS `!` non-null assertion)", tid)
	}
	return task
}

// Ref: src/db/tasks.rs CLAIM_TASK: `WHERE id=?3 AND status='ready'`
func TestParityClaimOnlyFiresOnStatusReady(t *testing.T) {
	t.Run("parity: claim only fires on status='ready'", func(t *testing.T) {
		t.Run("pending task (blocked by upstream) refuses claim", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			a := parityAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			b := parityAdd(t, db, AddTaskInput{
				Title:   "b",
				Project: "demo",
				Deps:    []DepSpec{{TaskID: a.ID}},
			})
			if b.Status != StatusPending {
				t.Errorf("b.status = %q, want %q", b.Status, StatusPending)
			}
			if got := db.ClaimTask(b.ID, "agent"); got != nil {
				t.Errorf("claimTask(b, \"agent\") = %+v, want null", got)
			}
		})

		t.Run("ready task accepts claim", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			task := parityAdd(t, db, AddTaskInput{Title: "t", Project: "demo"})
			if task.Status != StatusReady {
				t.Errorf("t.status = %q, want %q", task.Status, StatusReady)
			}
			if got := db.ClaimTask(task.ID, "agent"); got == nil {
				t.Error("claimTask(t, \"agent\") = null, want non-null")
			}
		})
	})
}

// Ref: src/db/schema.rs task_readiness view —
//
//	effective_unmet walks ancestor_chain, counts deps where kind IN
//	('blocks','feeds_into') AND upstream.status NOT IN ('done','done_partial').
func TestParityReadinessUsesAncestorChain(t *testing.T) {
	t.Run("parity: readiness uses ancestor chain", func(t *testing.T) {
		t.Run("child of a composite parent stays pending while parent has open dep", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			upstream := parityAdd(t, db, AddTaskInput{Title: "upstream", Project: "demo"})
			parent := parityAdd(t, db, AddTaskInput{
				Title:   "parent",
				Project: "demo",
				Deps:    []DepSpec{{TaskID: upstream.ID}},
			})
			child := parityAdd(t, db, AddTaskInput{Title: "child", Project: "demo", Parent: parent.ID})
			// Even though child has no direct deps, the ancestor's unmet dep keeps it pending.
			if child.Status != StatusPending {
				t.Errorf("child.status = %q, want %q", child.Status, StatusPending)
			}
			if parent.Status != StatusPending {
				t.Errorf("parent.status = %q, want %q", parent.Status, StatusPending)
			}
			// Once upstream is done, both parent and child become ready.
			db.ClaimTask(upstream.ID, "w")
			parityDone(t, db, upstream.ID)
			if got := parityMustGet(t, db, child.ID).Status; got != StatusReady {
				t.Errorf("getTask(child).status = %q, want %q", got, StatusReady)
			}
			if got := parityMustGet(t, db, parent.ID).Status; got != StatusReady {
				t.Errorf("getTask(parent).status = %q, want %q", got, StatusReady)
			}
		})

		t.Run("grandchild waits on grandparent's dep (depth>1)", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			upstream := parityAdd(t, db, AddTaskInput{Title: "up", Project: "demo"})
			gp := parityAdd(t, db, AddTaskInput{
				Title:   "gp",
				Project: "demo",
				Deps:    []DepSpec{{TaskID: upstream.ID}},
			})
			p := parityAdd(t, db, AddTaskInput{Title: "p", Project: "demo", Parent: gp.ID})
			c := parityAdd(t, db, AddTaskInput{Title: "c", Project: "demo", Parent: p.ID})
			if c.Status != StatusPending {
				t.Errorf("c.status = %q, want %q", c.Status, StatusPending)
			}
			db.ClaimTask(upstream.ID, "w")
			parityDone(t, db, upstream.ID)
			if got := parityMustGet(t, db, c.ID).Status; got != StatusReady {
				t.Errorf("getTask(c).status = %q, want %q", got, StatusReady)
			}
		})

		t.Run("suggests dep does NOT block (only feeds_into and blocks count)", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			a := parityAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			b := parityAdd(t, db, AddTaskInput{
				Title:   "b",
				Project: "demo",
				Deps:    []DepSpec{{TaskID: a.ID, Kind: parityDepKind(DepSuggests)}},
			})
			if b.Status != StatusReady {
				t.Errorf("b.status = %q, want %q", b.Status, StatusReady)
			}
		})
	})
}

// DELIBERATE DIVERGENCE from src/db/schema.rs task_readiness (BUGS-KEPT.md):
// TS keeps cancelled out of the satisfaction set, so cancelling an upstream
// never unblocks its dependents. The replanner's documented no-op-leaf
// strategy is cancel-and-amend, which under TS semantics leaves every
// dependent pending forever; TS only survives because its quiet exit slides
// into audit over an open graph — a hole this port closed. Two live runs
// deadlocked on exactly this before the divergence.
func TestCancelledUnblocksDownstream(t *testing.T) {
	t.Run("divergence: cancelled DOES unblock downstream", func(t *testing.T) {
		t.Run("cancelling upstream promotes downstream to ready", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			a := parityAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			b := parityAdd(t, db, AddTaskInput{
				Title:   "b",
				Project: "demo",
				Deps:    []DepSpec{{TaskID: a.ID}},
			})
			db.CancelTask(a.ID)
			if got := parityMustGet(t, db, b.ID).Status; got != StatusReady {
				t.Errorf("getTask(b).status = %q, want %q", got, StatusReady)
			}
		})
		t.Run("a second unmet dep still holds the dependent pending", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			a := parityAdd(t, db, AddTaskInput{Title: "a", Project: "demo"})
			c := parityAdd(t, db, AddTaskInput{Title: "c", Project: "demo"})
			b := parityAdd(t, db, AddTaskInput{
				Title:   "b",
				Project: "demo",
				Deps:    []DepSpec{{TaskID: a.ID}, {TaskID: c.ID}},
			})
			db.CancelTask(a.ID)
			if got := parityMustGet(t, db, b.ID).Status; got != StatusPending {
				t.Errorf("getTask(b).status = %q, want %q", got, StatusPending)
			}
		})
		t.Run("a failed-then-cancelled leaf unblocks the graph (run L shape)", func(t *testing.T) {
			// The exact live deadlock: a no-op leaf fails, the replanner
			// cancels it via the CLI bridge, and the dependents must go ready.
			db := parityNewDB(t)
			db.Init("demo")
			gomod := parityAdd(t, db, AddTaskInput{Title: "go.mod", Project: "demo"})
			impl := parityAdd(t, db, AddTaskInput{
				Title:   "impl",
				Project: "demo",
				Deps:    []DepSpec{{TaskID: gomod.ID}},
			})
			if claimed := db.ClaimTask(gomod.ID, "leaf"); claimed == nil {
				t.Fatal("claim failed")
			}
			db.FailTask(gomod.ID, "spec already satisfied", nil)
			if db.CancelTask(gomod.ID) == nil {
				t.Fatal("cancel failed")
			}
			if got := parityMustGet(t, db, impl.ID).Status; got != StatusReady {
				t.Errorf("getTask(impl).status = %q, want %q", got, StatusReady)
			}
		})
	})
}

// Ref: src/db/tasks.rs COMPLETE_COMPOSITE_IF_CHILDREN_DONE —
//
//	`NOT EXISTS (... status NOT IN ('done','done_partial','cancelled'))`
func TestParityCancelledSatisfiesCompositeCompletion(t *testing.T) {
	t.Run("parity: cancelled DOES satisfy composite-completion", func(t *testing.T) {
		t.Run("composite parent auto-completes when all children are done OR cancelled", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			parent := parityAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			c1 := parityAdd(t, db, AddTaskInput{Title: "c1", Project: "demo", Parent: parent.ID})
			c2 := parityAdd(t, db, AddTaskInput{Title: "c2", Project: "demo", Parent: parent.ID})
			parityDone(t, db, c1.ID)
			db.CancelTask(c2.ID)
			if got := parityMustGet(t, db, parent.ID).Status; got != StatusDone {
				t.Errorf("getTask(parent).status = %q, want %q", got, StatusDone)
			}
		})
	})
}

// Ref: src/db/tasks.rs COMPLETE_COMPOSITE_IF_CHILDREN_DONE —
//
//	`WHERE ... AND status IN ('pending','ready')`
func TestParityCompositeCompleteOnlyWhenParentPendingOrReady(t *testing.T) {
	t.Run("parity: composite-complete only fires when parent is pending/ready", func(t *testing.T) {
		t.Run("running parent is NOT auto-completed when children finish", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			parent := parityAdd(t, db, AddTaskInput{Title: "p", Project: "demo"})
			c := parityAdd(t, db, AddTaskInput{Title: "c", Project: "demo", Parent: parent.ID})
			db.ClaimTask(parent.ID, "w")
			db.StartTask(parent.ID)
			if got := parityMustGet(t, db, parent.ID).Status; got != StatusRunning {
				t.Errorf("getTask(parent).status = %q, want %q", got, StatusRunning)
			}
			parityDone(t, db, c.ID)
			// Parent must NOT auto-complete to done while in running state.
			if got := parityMustGet(t, db, parent.ID).Status; got != StatusRunning {
				t.Errorf("getTask(parent).status = %q, want %q", got, StatusRunning)
			}
		})
	})
}

// Ref: src/db/tasks.rs amend_task_description —
//
//	`if !matches!(task.status, TaskStatus::Pending | TaskStatus::Ready)`
//	format: `format!("{text}\n---\n{description}")`
func TestParityAmendOnlyOnPendingReady(t *testing.T) {
	// The TS test calls db.amendTask(id, text) with the default position
	// argument, which is "prepend".
	const prepend = "prepend"

	t.Run("parity: amend only on pending/ready, uses --- separator", func(t *testing.T) {
		t.Run("amending a claimed task throws", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			original := "original"
			task := parityAdd(t, db, AddTaskInput{Title: "t", Project: "demo", Description: &original})
			db.ClaimTask(task.ID, "w")
			_, err := db.AmendTask(task.ID, "NEW NOTE", prepend)
			if err == nil {
				t.Fatal("amendTask on a claimed task returned no error, want throw matching /pending or ready/")
			}
			if !regexp.MustCompile(`pending or ready`).MatchString(err.Error()) {
				t.Errorf("amendTask error = %q, want match /pending or ready/", err.Error())
			}
		})

		t.Run("amend prepends with --- separator", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			original := "original"
			task := parityAdd(t, db, AddTaskInput{Title: "t", Project: "demo", Description: &original})
			amended, err := db.AmendTask(task.ID, "NEW NOTE", prepend)
			if err != nil {
				t.Fatalf("amendTask returned unexpected error: %v", err)
			}
			if amended == nil {
				t.Fatal("amendTask returned null (TS `!` non-null assertion)")
			}
			if amended.Description == nil {
				t.Fatal("amended.description = null, want \"NEW NOTE\\n---\\noriginal\"")
			}
			if got := *amended.Description; got != "NEW NOTE\n---\noriginal" {
				t.Errorf("amended.description = %q, want %q", got, "NEW NOTE\n---\noriginal")
			}
		})

		t.Run("amend on empty description omits the separator", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			task := parityAdd(t, db, AddTaskInput{Title: "t", Project: "demo"})
			amended, err := db.AmendTask(task.ID, "first content", prepend)
			if err != nil {
				t.Fatalf("amendTask returned unexpected error: %v", err)
			}
			if amended == nil {
				t.Fatal("amendTask returned null (TS `!` non-null assertion)")
			}
			if amended.Description == nil {
				t.Fatal("amended.description = null, want \"first content\"")
			}
			if got := *amended.Description; got != "first content" {
				t.Errorf("amended.description = %q, want %q", got, "first content")
			}
		})
	})
}

// Ref: src/main.rs help text: "Otherwise auto-generated (t-k3m9)"
func TestParityIDFormatMatchesRustRegex(t *testing.T) {
	t.Run("parity: ID format matches Rust regex /t-[a-z0-9]+/", func(t *testing.T) {
		t.Run("task IDs match t- prefix + lowercase alphanumerics", func(t *testing.T) {
			db := parityNewDB(t)
			db.Init("demo")
			taskIDRe := regexp.MustCompile(`^t-[a-z0-9]+$`)
			for i := 0; i < 20; i++ {
				task := parityAdd(t, db, AddTaskInput{Title: "t" + strconv.Itoa(i), Project: "demo"})
				if !taskIDRe.MatchString(task.ID) {
					t.Errorf("t.id = %q, want match /^t-[a-z0-9]+$/", task.ID)
				}
				if len(task.ID) < 5 { // 't-' + at least 3 chars
					t.Errorf("len(t.id) = %d for %q, want >= 5", len(task.ID), task.ID)
				}
			}
		})

		t.Run("project IDs match p- prefix + lowercase alphanumerics", func(t *testing.T) {
			db := parityNewDB(t)
			p := db.Init("demo")
			if !regexp.MustCompile(`^p-[a-z0-9]+$`).MatchString(p.ID) {
				t.Errorf("p.id = %q, want match /^p-[a-z0-9]+$/", p.ID)
			}
		})
	})
}
