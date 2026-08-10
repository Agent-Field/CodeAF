package plandb

import (
	"reflect"
	"testing"
)

// Validation contract for the failed-upstream deadlock, derived from two
// benchmark runs (urfave/cli #2263, node-semver #775) that each delivered an
// empty diff while a correct patch sat quarantined:
//
//   - a dependent whose only unmet hard edges point at terminally failed
//     upstreams must be recoverable, not stranded pending forever;
//   - a dependent that still has an upstream capable of completing must NOT be
//     recoverable — waiting is still meaningful;
//   - recovery must be explicit, never a side effect of the normal promotion
//     pass, so a failed upstream keeps blocking until someone decides otherwise.
func TestFailedUpstreamBlockers(t *testing.T) {
	t.Run("PlanDB.failedUpstreamBlockers", func(t *testing.T) {
		t.Run("a failed upstream strands its dependent under normal promotion", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			fix := storeTAdd(t, db, AddTaskInput{Title: "fix errors.go", Project: "demo"})
			test := storeTAdd(t, db, AddTaskInput{
				Title: "add test", Project: "demo",
				Deps: []DepSpec{{TaskID: fix.ID, Kind: storeTDepKind(DepFeedsInto)}},
			})
			db.FailTask(fix.ID, "adaptive leaf guard: gave up", nil)

			// The pre-existing semantics: failed does not satisfy the edge.
			if got := storeTGet(t, db, test.ID).Status; got != StatusPending {
				t.Fatalf("dependent status = %q, want %q (failed must not auto-satisfy)", got, StatusPending)
			}
			if got := db.FailedUpstreamBlockers(test.ID); !reflect.DeepEqual(got, []TaskID{fix.ID}) {
				t.Errorf("FailedUpstreamBlockers = %v, want [%s]", got, fix.ID)
			}
		})

		t.Run("an upstream that can still complete is not a failed blocker", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			failed := storeTAdd(t, db, AddTaskInput{Title: "failed", Project: "demo"})
			pending := storeTAdd(t, db, AddTaskInput{Title: "still open", Project: "demo"})
			test := storeTAdd(t, db, AddTaskInput{
				Title: "dependent", Project: "demo",
				Deps: []DepSpec{
					{TaskID: failed.ID, Kind: storeTDepKind(DepFeedsInto)},
					{TaskID: pending.ID, Kind: storeTDepKind(DepFeedsInto)},
				},
			})
			db.FailTask(failed.ID, "boom", nil)

			if got := db.FailedUpstreamBlockers(test.ID); got != nil {
				t.Errorf("FailedUpstreamBlockers = %v, want nil while %s can still complete", got, pending.ID)
			}
			if got := db.ReleaseFailedUpstreamBlock(test.ID); got != nil {
				t.Errorf("ReleaseFailedUpstreamBlock = %v, want nil (must not jump a live dependency)", got)
			}
			if got := storeTGet(t, db, test.ID).Status; got != StatusPending {
				t.Errorf("dependent status = %q, want %q", got, StatusPending)
			}
		})

		t.Run("release promotes the stranded dependent and reports the bypassed upstreams", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			fix := storeTAdd(t, db, AddTaskInput{Title: "fix", Project: "demo"})
			test := storeTAdd(t, db, AddTaskInput{
				Title: "add test", Project: "demo",
				Deps: []DepSpec{{TaskID: fix.ID, Kind: storeTDepKind(DepFeedsInto)}},
			})
			db.FailTask(fix.ID, "adaptive leaf guard: gave up", nil)

			if got := db.ReleaseFailedUpstreamBlock(test.ID); !reflect.DeepEqual(got, []TaskID{fix.ID}) {
				t.Fatalf("ReleaseFailedUpstreamBlock = %v, want [%s]", got, fix.ID)
			}
			if got := storeTGet(t, db, test.ID).Status; got != StatusReady {
				t.Errorf("dependent status = %q, want %q — the graph must be dispatchable again", got, StatusReady)
			}
			// Idempotent: the task is no longer pending, so a second sweep pass
			// must not report it again.
			if got := db.ReleaseFailedUpstreamBlock(test.ID); got != nil {
				t.Errorf("second ReleaseFailedUpstreamBlock = %v, want nil", got)
			}
		})

		t.Run("a blocks edge deadlocks the same way and releases the same way", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			first := storeTAdd(t, db, AddTaskInput{Title: "first", Project: "demo"})
			second := storeTAdd(t, db, AddTaskInput{
				Title: "second", Project: "demo",
				Deps: []DepSpec{{TaskID: first.ID, Kind: storeTDepKind(DepBlocks)}},
			})
			db.FailTask(first.ID, "boom", nil)

			if got := db.ReleaseFailedUpstreamBlock(second.ID); !reflect.DeepEqual(got, []TaskID{first.ID}) {
				t.Errorf("ReleaseFailedUpstreamBlock = %v, want [%s]", got, first.ID)
			}
		})

		t.Run("a suggests edge never blocks, so there is nothing to release", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			hint := storeTAdd(t, db, AddTaskInput{Title: "hint", Project: "demo"})
			task := storeTAdd(t, db, AddTaskInput{
				Title: "task", Project: "demo",
				Deps: []DepSpec{{TaskID: hint.ID, Kind: storeTDepKind(DepSuggests)}},
			})
			db.FailTask(hint.ID, "boom", nil)

			if got := storeTGet(t, db, task.ID).Status; got != StatusReady {
				t.Fatalf("dependent status = %q, want %q (suggests is soft)", got, StatusReady)
			}
			if got := db.FailedUpstreamBlockers(task.ID); got != nil {
				t.Errorf("FailedUpstreamBlockers = %v, want nil", got)
			}
		})

		t.Run("a cancelled upstream already satisfies the edge and is not reported", func(t *testing.T) {
			db := NewPlanDB()
			db.Init("demo")
			noop := storeTAdd(t, db, AddTaskInput{Title: "no-op", Project: "demo"})
			task := storeTAdd(t, db, AddTaskInput{
				Title: "task", Project: "demo",
				Deps: []DepSpec{{TaskID: noop.ID, Kind: storeTDepKind(DepFeedsInto)}},
			})
			db.CancelTask(noop.ID)

			if got := storeTGet(t, db, task.ID).Status; got != StatusReady {
				t.Fatalf("dependent status = %q, want %q", got, StatusReady)
			}
			if got := db.FailedUpstreamBlockers(task.ID); got != nil {
				t.Errorf("FailedUpstreamBlockers = %v, want nil", got)
			}
		})
	})
}
