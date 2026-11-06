package run

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

func boundedRunStore(t *testing.T, title string) *plandb.Store {
	t.Helper()
	s, err := plandb.Open(filepath.Join(t.TempDir(), "plan.db"), "bounded-run", "root", title, "the assigned work")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// L-LOOP C3e: replay the recursive red/green/blue adds through the real CLI.
func TestRepeatedTaskHandoffsCannotFanOutTheRun(t *testing.T) {
	store := boundedRunStore(t, "Create green.txt")
	t.Setenv(plandb.RunEnv, store.RootID())
	t.Setenv("PLANDB_DB", store.Path())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var accepted, refused, launches atomic.Int32
	factory := func(task plandb.Task) Worker {
		launches.Add(1)
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			if task.Role == plandb.RoleCheck {
				return Report{Result: "holds: the work was read", Steps: 1}, nil
			}
			for _, title := range []string{"Create red.txt", "Create green.txt", "Create blue.txt"} {
				if code := plandb.Main([]string{"add", title, "--parent", "t-" + task.ID}); code == 0 {
					accepted.Add(1)
				} else {
					refused.Add(1)
				}
			}
			return Report{Result: "did my own work", Steps: 1}, nil
		})
	}
	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: factory, Limits: Limits{ReviewRound: true}})
	if outcome != OutcomeDone || summary.Limit != "" || ctx.Err() != nil {
		t.Fatalf("bounded replay did not end normally: %s, %+v, %v", outcome, summary, ctx.Err())
	}
	seen := map[string]bool{}
	counted := 0
	for _, task := range store.Tasks() {
		if task.Status == plandb.StatusCancelled {
			continue
		}
		key := strings.Join(strings.FieldsFunc(strings.ToLower(task.Title), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}), " ")
		if seen[key] {
			t.Fatalf("duplicate title in the finished store: %s", task.Title)
		}
		seen[key] = true
		if task.ID != store.RootID() && task.Role != plandb.RoleCheck {
			counted++
		}
		depth := 0
		for parent := task; parent.ID != store.RootID(); parent = store.Task(parent.ParentID) {
			depth++
			if depth > plandb.TreeDepthLimit {
				t.Fatalf("task %s exceeds the depth limit", task.ID)
			}
		}
	}
	// Two accepted children, their checks, and the root's initial/waking turns.
	if accepted.Load() != 2 || refused.Load() < 6 || counted != 2 || counted > plandb.TreeTaskLimit || launches.Load() != 6 {
		t.Fatalf("replay admitted refused work: accepted=%d refused=%d counted=%d launches=%d", accepted.Load(), refused.Load(), counted, launches.Load())
	}
}

// L-LOOP C4b, C4c: a typed pre-call stop ends peers and never launches the
// next ready task, even though the run's metered total is below its ceiling.
func TestTaskSpendRefusalStopsTheWholeRun(t *testing.T) {
	store := boundedRunStore(t, "Root work")
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "peer", Title: "Peer work"}}); err != nil {
		t.Fatal(err)
	}
	peerStarted := make(chan struct{})
	var launches, peersEnded atomic.Int32
	factory := func(task plandb.Task) Worker {
		launches.Add(1)
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			switch task.ID {
			case "root":
				select {
				case <-peerStarted:
				case <-ctx.Done():
					return Report{}, ctx.Err()
				}
				if _, err := store.AddMany([]plandb.TaskSpec{{ID: "later", Title: "Later work"}}); err != nil {
					return Report{}, err
				}
				return Report{}, fmt.Errorf("seat ended: %w", session.ErrSpendStopped{Action: "task cap", TaskLimit: true})
			case "peer":
				close(peerStarted)
				<-ctx.Done()
				peersEnded.Add(1)
				return Report{}, ctx.Err()
			default:
				return Report{Result: "must never start"}, nil
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 2, Factory: factory, Limits: Limits{CostUSD: 5}})
	if outcome != OutcomeLimit || summary.Limit != LimitCost || summary.USD != 0 || launches.Load() != 2 || peersEnded.Load() != 1 || ctx.Err() != nil {
		t.Fatalf("task-cap refusal did not stop the tree: %s %+v launches=%d peers ended=%d context=%v", outcome, summary, launches.Load(), peersEnded.Load(), ctx.Err())
	}
}

// L-LOOP C4c: a checker's own ceiling fails that task and leaves work going.
func TestSeatSpendRefusalLeavesOtherTasksRunning(t *testing.T) {
	store := boundedRunStore(t, "Root work")
	if _, err := store.AddMany([]plandb.TaskSpec{{ID: "check", Title: "Check", Role: plandb.RoleCheck}, {ID: "work", Title: "Work"}}); err != nil {
		t.Fatal(err)
	}
	var workStarted atomic.Bool
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(context.Context, plandb.Task) (Report, error) {
			if task.ID == "check" {
				return Report{}, fmt.Errorf("seat ended: %w", session.ErrSpendStopped{Action: "checker ceiling"})
			}
			if task.ID == "work" {
				workStarted.Store(true)
			}
			return Report{Result: "did my work", Steps: 1}, nil
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: factory, Limits: Limits{CostUSD: 5}})
	if outcome == OutcomeLimit || summary.Limit != "" || !workStarted.Load() || store.Task("check").Status != plandb.StatusFailed || ctx.Err() != nil {
		t.Fatalf("seat ceiling stopped other work: %s %+v work started=%v check=%+v context=%v", outcome, summary, workStarted.Load(), store.Task("check"), ctx.Err())
	}
}

// L-LOOP C4c: the tree's cap can refuse a review call after the work landed.
func TestTaskSpendRefusalInTheReviewRoundStillEndsOnCost(t *testing.T) {
	store := boundedRunStore(t, "Single task")
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(context.Context, plandb.Task) (Report, error) {
			if task.Role == plandb.RoleCheck {
				return Report{}, session.ErrSpendStopped{Action: "task cap", TaskLimit: true}
			}
			return Report{Result: "finished the work", Steps: 1}, nil
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: factory, Limits: Limits{CostUSD: 5, ReviewRound: true}})
	if outcome != OutcomeLimit || summary.Limit != LimitCost || ctx.Err() != nil {
		t.Fatalf("review's task-cap refusal was lost: %s %+v context=%v", outcome, summary, ctx.Err())
	}
}

// L-LOOP C6: an ordinary single task still lands after its review, under cap.
func TestSingleTaskUnderTreeCeilingStillRunsAndChecks(t *testing.T) {
	store := boundedRunStore(t, "Single task")
	var checks atomic.Int32
	factory := func(task plandb.Task) Worker {
		return workerFunc(func(context.Context, plandb.Task) (Report, error) {
			if task.Role == plandb.RoleCheck {
				checks.Add(1)
				return Report{Result: "holds: read the result", Steps: 1, USD: .01}, nil
			}
			return Report{Result: "finished the work", Steps: 1, USD: .02}, nil
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: factory, Limits: Limits{CostUSD: 5, ReviewRound: true}})
	if outcome != OutcomeDone || summary.Limit != "" || summary.Result != "finished the work" || checks.Load() != 1 || ctx.Err() != nil {
		t.Fatalf("ordinary task changed: %s %+v checks=%d context=%v", outcome, summary, checks.Load(), ctx.Err())
	}
}

// L-LOOP C6: a final successful receipt retains the existing completion
// precedence even when that receipt reaches the ordinary cost limit.
func TestFinishedChildlessRunAtCostLimitStillEndsDone(t *testing.T) {
	store := boundedRunStore(t, "Single task")
	factory := func(plandb.Task) Worker {
		return workerFunc(func(context.Context, plandb.Task) (Report, error) {
			return Report{Result: "finished", USD: 1}, nil
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: factory, Limits: Limits{CostUSD: 1}})
	if outcome != OutcomeDone || summary.Result != "finished" || summary.USD != 1 || summary.Limit != LimitCost || store.Task(store.RootID()).Status != plandb.StatusDone || ctx.Err() != nil {
		t.Fatalf("finished work lost to its final receipt: %s %+v root=%+v context=%v", outcome, summary, store.Task(store.RootID()), ctx.Err())
	}
}

// L-LOOP C6: the original pass accepts a completed root after the elapsed
// signal drains a successful worker return. Drive that ordering explicitly.
func TestCompletedRootStillWinsOverTheElapsedSignal(t *testing.T) {
	store := boundedRunStore(t, "Single task")
	elapsed := make(chan time.Time)
	factory := func(plandb.Task) Worker {
		return workerFunc(func(ctx context.Context, _ plandb.Task) (Report, error) {
			if err := store.CompleteRoot("finished"); err != nil {
				return Report{}, err
			}
			close(elapsed)
			<-ctx.Done()
			return Report{Result: "finished"}, nil
		})
	}
	supervisor := NewSupervisor(store, t.TempDir(), 1, Limits{Elapsed: time.Hour}, factory)
	supervisor.after = func(time.Duration) <-chan time.Time { return elapsed }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome := supervisor.Run(ctx)
	if outcome != OutcomeDone || supervisor.limitHit != LimitTime || supervisor.rootResult != "finished" || store.Task(store.RootID()).Status != plandb.StatusDone || ctx.Err() != nil {
		t.Fatalf("completed root lost to elapsed signal: %s limit=%s result=%q root=%+v context=%v", outcome, supervisor.limitHit, supervisor.rootResult, store.Task(store.RootID()), ctx.Err())
	}
}
