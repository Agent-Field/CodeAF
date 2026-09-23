package run_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// A WORKER CANNOT WRITE INTO ANOTHER RUN'S STORE. The worker's run opened its
// store at one path; while it works, the store at that path is set aside and a
// different run's store is made in its place, which is what a second hand-off
// racing the first used to do. The worker's `plandb add` must not land in the
// store that is not its run's: its binding names the run, not only the path.
func TestBashWorkerCannotWriteIntoAnotherRunsStore(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", realPlandbDoor(t))
	store := runOpenStore(t)
	path := store.Path()
	if _, err := store.AddMany([]plandb.TaskSpec{leafDone("mine")}); err != nil {
		t.Fatalf("add the leaf: %v", err)
	}
	if _, err := store.Claim("mine", "mine", "test-owner"); err != nil {
		t.Fatalf("claim the leaf: %v", err)
	}
	const title = "Work filed into the wrong run"
	seat := &seat{script: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			// ANOTHER RUN TAKES THE PATH. The worker's own store is moved aside
			// whole (its handle keeps working on the moved file) and a store with
			// a different root is created where it was.
			for _, suffix := range []string{"", "-wal", "-shm"} {
				if err := os.Rename(path+suffix, path+".1"+suffix); err != nil && !os.IsNotExist(err) {
					t.Errorf("set the worker's store aside: %v", err)
				}
			}
			other, err := plandb.Open(path, "the other run", "other", "the other run", "")
			if err != nil {
				t.Errorf("open the other run's store: %v", err)
			} else {
				_ = other.Close()
			}
			return toolReply(bashArguments(t, `plandb add '`+title+`' --description 'not this run'`)), nil
		},
	}}
	worker := run.NewBashWorker(store, t.TempDir(), "test/model", seat)
	_, _ = worker.Run(run.WithStepsPerTask(runContext(t), 3), *store.Task("mine"))

	other, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("re-open the store at the path: %v", err)
	}
	defer other.Close()
	if other.RootID() != "other" {
		t.Fatalf("the store at the path is run %q, want the other run", other.RootID())
	}
	for _, task := range other.Tasks() {
		if task.Title == title {
			t.Fatalf("the worker filed %q into the other run's store at %s", title, filepath.Base(path))
		}
	}
}
