package session

// THE RUN ROAD'S OWN HALF OF THE SEAT LAW. [NewBeltWorker] builds the worker
// the run engine hosts each claimed store task in (internal/run's BashWorker),
// and unlike the /task road's spawn — which reads the belt switch and sets the
// seat beside it (task_run.go's workerSeat) — it had no posture of its own: a
// run worker's every call went out with no reasoning field at all.
//
// The seat it now takes is [effort.RoleWork], the same one the /task road
// chooses with its belt on. What these tests pin is the RESOLUTION and not a
// field: the constructor sets the role, and [effort.Resolve]'s own order —
// turn beats conversation beats task beats role beats the install's default —
// decides the rung, so nothing here special-cases a scope.

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// TestBeltWorkerBuiltByNewBeltWorkerThinksFromTheWorkSeat is the run road's
// seat, read at the constructor: a worker built through [NewBeltWorker] answers
// low when nothing above the role spoke, and answers the rung the work carries
// when the work set one — the task scope, which [effort.Resolve] reads before
// the role and therefore returns ahead of the floor.
func TestBeltWorkerBuiltByNewBeltWorkerThinksFromTheWorkSeat(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	// The plandb override wins unprobed (plandb_plan.go's resolvePlanCLI), so
	// the constructor's shim arms against any path and no CLI is run.
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))

	cases := []struct {
		name string
		rung effort.Rung
		want string
	}{
		{"nothing above the seat", effort.None, "low"},
		{"a rung on the work", effort.High, "high"},
		{"the seat is a floor, not a cap", effort.Max, "max"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := newRunBeltWorker(t, tc.rung)
			if got := agent.ResolvedEffort(); got != tc.want {
				t.Fatalf("the run worker resolves to %q, want %q", got, tc.want)
			}
		})
	}
}

// newRunBeltWorker builds one run worker through [NewBeltWorker] against a real
// store at a real path, the way internal/run's BashWorker does — the root task
// of a fresh store, the seat's model, and the rung handed in as the work's own
// when the case set one. A scripted completer stands in for the seat's
// provider, because the resolution under test is read off the agent and no call
// is made.
func newRunBeltWorker(t *testing.T, taskRung effort.Rung) *Agent {
	t.Helper()
	dir := t.TempDir()
	store, err := plandb.Open(filepath.Join(dir, planStoreFilename), "the work", planRootID, "", "")
	if err != nil {
		t.Fatalf("open the plan store: %v", err)
	}
	task := store.Task(planRootID)
	if task == nil {
		t.Fatal("the fresh store carries no root task")
	}
	config := Config{Workspace: t.TempDir(), Model: "test/model"}
	if taskRung.Valid() {
		config.Effort = taskRung
	}
	agent, err := NewBeltWorker(config, &scriptedCompleter{}, task, store.Path())
	if err != nil {
		t.Fatalf("NewBeltWorker: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}
